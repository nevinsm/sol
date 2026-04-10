package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	clischema "github.com/nevinsm/sol/internal/cliapi/schema"
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
var schemaMigrateJSON bool

func init() {
	rootCmd.AddCommand(schemaCmd)

	schemaCmd.AddCommand(schemaStatusCmd)
	schemaStatusCmd.Flags().BoolVar(&schemaStatusJSON, "json", false, "output as JSON")

	schemaCmd.AddCommand(schemaMigrateCmd)
	schemaMigrateCmd.Flags().Bool("confirm", false, "Execute migrations (default is preview-only)")
	schemaMigrateCmd.Flags().Bool("backup", false, "Create a backup of each database before migrating")
	schemaMigrateCmd.Flags().BoolVar(&schemaMigrateJSON, "json", false, "output as JSON")

	// Deprecated --dry-run flag (no-op since dry-run is the default; kept for backward compatibility).
	schemaMigrateCmd.Flags().Bool("dry-run", false, "deprecated: dry-run is now the default; use --confirm to execute")
	schemaMigrateCmd.Flags().MarkDeprecated("dry-run", "dry-run is now the default behavior; use --confirm to execute")
}

// --- sol schema status ---

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
	var results []clischema.StatusEntry

	// Sphere database.
	spherePath := filepath.Join(storeDir, "sphere.db")
	sphereVersion, sphereErr := readSchemaVersion(spherePath)
	entry := clischema.StatusEntry{
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
		we := clischema.StatusEntry{
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
	Use:   "migrate",
	Short: "Run schema migrations on all databases",
	Long: `Run schema migrations on the sphere database and every world
database in the store directory.

By default this is a preview only — pass --confirm to actually apply
migrations. Pass --backup to snapshot each database before migrating.

Exit codes:
  0 - Migrations applied successfully (--confirm), or all databases
      already at current schema version
  1 - Preview mode (--confirm not provided), or an error occurred`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		confirm, _ := cmd.Flags().GetBool("confirm")
		backup, _ := cmd.Flags().GetBool("backup")
		dryRun := !confirm

		storeDir := config.StoreDir()

		var jsonResults []clischema.MigratedDatabase

		// Sphere database.
		spherePath := filepath.Join(storeDir, "sphere.db")
		result, err := migrateDatabase(spherePath, "sphere", "sphere", store.CurrentSphereSchema, dryRun, backup, schemaMigrateJSON, func() (storeCloser, error) {
			return store.OpenSphere()
		})
		if err != nil {
			return err
		}
		if schemaMigrateJSON {
			jsonResults = append(jsonResults, result)
		}

		// World databases.
		entries, err := os.ReadDir(storeDir)
		if err != nil {
			if os.IsNotExist(err) {
				if schemaMigrateJSON {
					return printJSON(clischema.MigrateResponse{AppliedMigrations: jsonResults})
				}
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
			result, err := migrateDatabase(dbPath, worldName, "world", store.CurrentWorldSchema, dryRun, backup, schemaMigrateJSON, func() (storeCloser, error) {
				return store.OpenWorld(worldName)
			})
			if err != nil {
				return err
			}
			if schemaMigrateJSON {
				jsonResults = append(jsonResults, result)
			}
		}

		if schemaMigrateJSON {
			return printJSON(clischema.MigrateResponse{AppliedMigrations: jsonResults})
		}

		if dryRun {
			fmt.Println("\nRun with --confirm to apply migrations.")
			return &exitError{code: 1}
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

func migrateDatabase(path, label, dbType string, targetVersion int, dryRun, backup, jsonMode bool, openFn func() (storeCloser, error)) (clischema.MigratedDatabase, error) {
	currentVersion, err := readSchemaVersion(path)
	if err != nil {
		// Database doesn't exist yet — migration will create it.
		if os.IsNotExist(err) {
			if dryRun {
				if !jsonMode {
					fmt.Printf("%s: would create database at v%d\n", label, targetVersion)
				}
				return clischema.MigratedDatabase{
					Database:    label,
					Type:        dbType,
					FromVersion: 0,
					ToVersion:   targetVersion,
					Status:      "preview",
				}, nil
			}
			if !jsonMode {
				fmt.Printf("%s: creating database at v%d\n", label, targetVersion)
			}
			s, err := openFn()
			if err != nil {
				return clischema.MigratedDatabase{}, fmt.Errorf("failed to migrate %s: %w", label, err)
			}
			s.Close()
			return clischema.MigratedDatabase{
				Database:    label,
				Type:        dbType,
				FromVersion: 0,
				ToVersion:   targetVersion,
				Status:      "created",
			}, nil
		}
		return clischema.MigratedDatabase{}, fmt.Errorf("failed to read version for %s: %w", label, err)
	}

	if currentVersion >= targetVersion {
		if !jsonMode {
			fmt.Printf("%s: v%d (current)\n", label, currentVersion)
		}
		return clischema.MigratedDatabase{
			Database:    label,
			Type:        dbType,
			FromVersion: currentVersion,
			ToVersion:   targetVersion,
			Status:      "current",
		}, nil
	}

	if dryRun {
		if !jsonMode {
			fmt.Printf("%s: v%d → v%d (dry-run, no changes applied)\n", label, currentVersion, targetVersion)
		}
		return clischema.MigratedDatabase{
			Database:    label,
			Type:        dbType,
			FromVersion: currentVersion,
			ToVersion:   targetVersion,
			Status:      "preview",
		}, nil
	}

	if backup {
		backupPath, err := store.BackupDatabase(path)
		if err != nil {
			return clischema.MigratedDatabase{}, fmt.Errorf("failed to backup %s: %w", label, err)
		}
		if !jsonMode {
			fmt.Printf("%s: backed up to %s\n", label, backupPath)
		}
	}

	s, err := openFn()
	if err != nil {
		return clischema.MigratedDatabase{}, fmt.Errorf("failed to migrate %s: %w", label, err)
	}
	s.Close()
	if !jsonMode {
		fmt.Printf("%s: v%d → v%d (migrated)\n", label, currentVersion, targetVersion)
	}
	return clischema.MigratedDatabase{
		Database:    label,
		Type:        dbType,
		FromVersion: currentVersion,
		ToVersion:   targetVersion,
		Status:      "migrated",
	}, nil
}
