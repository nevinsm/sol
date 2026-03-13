package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var schemaCmd = &cobra.Command{
	Use:     "schema",
	Short:   "Schema version and migration management",
	GroupID: groupSetup,
}

var schemaStatusJSON bool

func init() {
	rootCmd.AddCommand(schemaCmd)

	schemaCmd.AddCommand(schemaStatusCmd)
	schemaStatusCmd.Flags().BoolVar(&schemaStatusJSON, "json", false, "output as JSON")

	schemaCmd.AddCommand(schemaMigrateCmd)
	schemaMigrateCmd.Flags().Bool("dry-run", false, "Preview migrations without applying them")
	schemaMigrateCmd.Flags().Bool("backup", false, "Create a backup of each database before migrating")
}

// --- sol schema status ---

// schemaEntry is a JSON-friendly representation of a database's schema info.
type schemaEntry struct {
	Database string `json:"database"`
	Type     string `json:"type"` // "sphere" or "world"
	Version  int    `json:"version"`
	Target   int    `json:"target"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

var schemaStatusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show schema version information for all databases",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		storeDir := config.StoreDir()

		if schemaStatusJSON {
			return schemaStatusJSONOutput(storeDir)
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)

		// Sphere database.
		spherePath := filepath.Join(storeDir, "sphere.db")
		sphereVersion, sphereErr := readSchemaVersion(spherePath)
		if sphereErr != nil {
			fmt.Fprintf(w, "Sphere database:\t(error: %v)\n", sphereErr)
		} else {
			fmt.Fprintf(w, "Sphere database:\tv%d\t%s\n", sphereVersion, versionStatus(sphereVersion, store.CurrentSphereSchema))
		}

		// World databases.
		entries, err := os.ReadDir(storeDir)
		if err != nil {
			w.Flush()
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("failed to read store directory: %w", err)
		}

		var worldDBs []string
		for _, e := range entries {
			name := e.Name()
			if name == "sphere.db" || !strings.HasSuffix(name, ".db") {
				continue
			}
			// Skip WAL and journal files.
			if strings.HasSuffix(name, "-wal") || strings.HasSuffix(name, "-shm") || strings.HasSuffix(name, "-journal") {
				continue
			}
			worldDBs = append(worldDBs, name)
		}

		if len(worldDBs) > 0 {
			fmt.Fprintln(w, "World databases:")
			for _, dbFile := range worldDBs {
				worldName := strings.TrimSuffix(dbFile, ".db")
				dbPath := filepath.Join(storeDir, dbFile)
				v, err := readSchemaVersion(dbPath)
				if err != nil {
					fmt.Fprintf(w, "  %s:\t(error: %v)\n", worldName, err)
				} else {
					fmt.Fprintf(w, "  %s:\tv%d\t%s\n", worldName, v, versionStatus(v, store.CurrentWorldSchema))
				}
			}
		}

		return w.Flush()
	},
}

func schemaStatusJSONOutput(storeDir string) error {
	var results []schemaEntry

	// Sphere database.
	spherePath := filepath.Join(storeDir, "sphere.db")
	sphereVersion, sphereErr := readSchemaVersion(spherePath)
	entry := schemaEntry{
		Database: "sphere",
		Type:     "sphere",
		Target:   store.CurrentSphereSchema,
	}
	if sphereErr != nil {
		entry.Error = sphereErr.Error()
	} else {
		entry.Version = sphereVersion
		entry.Status = versionStatus(sphereVersion, store.CurrentSphereSchema)
	}
	results = append(results, entry)

	// World databases.
	entries, err := os.ReadDir(storeDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read store directory: %w", err)
	}

	for _, e := range entries {
		name := e.Name()
		if name == "sphere.db" || !strings.HasSuffix(name, ".db") {
			continue
		}
		if strings.HasSuffix(name, "-wal") || strings.HasSuffix(name, "-shm") || strings.HasSuffix(name, "-journal") {
			continue
		}
		worldName := strings.TrimSuffix(name, ".db")
		dbPath := filepath.Join(storeDir, name)
		we := schemaEntry{
			Database: worldName,
			Type:     "world",
			Target:   store.CurrentWorldSchema,
		}
		v, err := readSchemaVersion(dbPath)
		if err != nil {
			we.Error = err.Error()
		} else {
			we.Version = v
			we.Status = versionStatus(v, store.CurrentWorldSchema)
		}
		results = append(results, we)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Type != results[j].Type {
			return results[i].Type == "sphere" // sphere first
		}
		return results[i].Database < results[j].Database
	})

	return printJSON(results)
}

// --- sol schema migrate ---

var schemaMigrateCmd = &cobra.Command{
	Use:          "migrate",
	Short:        "Run schema migrations on all databases",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		backup, _ := cmd.Flags().GetBool("backup")

		storeDir := config.StoreDir()

		// Sphere database.
		spherePath := filepath.Join(storeDir, "sphere.db")
		if err := migrateDatabase(spherePath, "sphere", store.CurrentSphereSchema, dryRun, backup, func() (storeCloser, error) {
			return store.OpenSphere()
		}); err != nil {
			return err
		}

		// World databases.
		entries, err := os.ReadDir(storeDir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("failed to read store directory: %w", err)
		}

		for _, e := range entries {
			name := e.Name()
			if name == "sphere.db" || !strings.HasSuffix(name, ".db") {
				continue
			}
			if strings.HasSuffix(name, "-wal") || strings.HasSuffix(name, "-shm") || strings.HasSuffix(name, "-journal") {
				continue
			}
			worldName := strings.TrimSuffix(name, ".db")
			dbPath := filepath.Join(storeDir, name)
			if err := migrateDatabase(dbPath, worldName, store.CurrentWorldSchema, dryRun, backup, func() (storeCloser, error) {
				return store.OpenWorld(worldName)
			}); err != nil {
				return err
			}
		}

		return nil
	},
}

// readSchemaVersion opens a database without migrating and returns its schema version.
func readSchemaVersion(path string) (int, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return 0, fmt.Errorf("database not found")
	}
	s, err := store.OpenNoMigrate(path)
	if err != nil {
		return 0, err
	}
	defer s.Close()
	return s.SchemaVersion()
}

// versionStatus returns a human-readable status string for a schema version.
func versionStatus(current, target int) string {
	if current >= target {
		return "(current)"
	}
	if current == 0 {
		return "(uninitialized)"
	}
	return fmt.Sprintf("(needs migration to v%d)", target)
}

// migrateDatabase handles migration of a single database with optional dry-run and backup.
// storeCloser is the minimal interface needed by migrateDatabase for the opened store.
type storeCloser interface {
	Close() error
}

func migrateDatabase(path, label string, targetVersion int, dryRun, backup bool, openFn func() (storeCloser, error)) error {
	currentVersion, err := readSchemaVersion(path)
	if err != nil {
		// Database doesn't exist yet — migration will create it.
		if os.IsNotExist(err) {
			if dryRun {
				fmt.Printf("%s: would create database at v%d\n", label, targetVersion)
				return nil
			}
			fmt.Printf("%s: creating database at v%d\n", label, targetVersion)
			s, err := openFn()
			if err != nil {
				return fmt.Errorf("failed to migrate %s: %w", label, err)
			}
			s.Close()
			return nil
		}
		return fmt.Errorf("failed to read version for %s: %w", label, err)
	}

	if currentVersion >= targetVersion {
		fmt.Printf("%s: v%d (current)\n", label, currentVersion)
		return nil
	}

	if dryRun {
		fmt.Printf("%s: v%d → v%d (dry-run, no changes applied)\n", label, currentVersion, targetVersion)
		return nil
	}

	if backup {
		backupPath, err := store.BackupDatabase(path)
		if err != nil {
			return fmt.Errorf("failed to backup %s: %w", label, err)
		}
		fmt.Printf("%s: backed up to %s\n", label, backupPath)
	}

	s, err := openFn()
	if err != nil {
		return fmt.Errorf("failed to migrate %s: %w", label, err)
	}
	s.Close()
	fmt.Printf("%s: v%d → v%d (migrated)\n", label, currentVersion, targetVersion)
	return nil
}
