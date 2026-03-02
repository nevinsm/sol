package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	worldInitSourceRepo string
	worldListJSON       bool
	worldStatusJSON     bool
	worldDeleteConfirm  bool
)

var worldCmd = &cobra.Command{
	Use:   "world",
	Short: "Manage worlds",
}

var worldInitCmd = &cobra.Command{
	Use:   "init <name>",
	Short: "Initialize a new world",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		if err := config.ValidateWorldName(name); err != nil {
			return err
		}

		// Check if world.toml already exists.
		tomlPath := config.WorldConfigPath(name)
		if _, err := os.Stat(tomlPath); err == nil {
			return fmt.Errorf("world %q is already initialized", name)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to check world config %q: %w", tomlPath, err)
		}

		// Detect pre-Arc1 world (DB exists but no world.toml).
		dbPath := filepath.Join(config.StoreDir(), name+".db")
		preArc1 := false
		if _, err := os.Stat(dbPath); err == nil {
			preArc1 = true
		}

		// Determine source repo.
		sourceRepo := worldInitSourceRepo
		if sourceRepo == "" {
			repo, err := dispatch.ResolveSourceRepo(config.WorldConfig{})
			if err == nil {
				sourceRepo = repo
			}
		}

		if sourceRepo != "" {
			info, err := os.Stat(sourceRepo)
			if err != nil {
				return fmt.Errorf("source repo path %q: %w", sourceRepo, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("source repo path %q is not a directory", sourceRepo)
			}
		}

		// Create directory tree.
		worldDir := config.WorldDir(name)
		if err := os.MkdirAll(filepath.Join(worldDir, "outposts"), 0o755); err != nil {
			return fmt.Errorf("failed to create world directory: %w", err)
		}

		// Ensure .store/ directory exists.
		if err := config.EnsureDirs(); err != nil {
			return fmt.Errorf("failed to create store directory: %w", err)
		}

		// Create world database (triggers schema migration).
		worldStore, err := store.OpenWorld(name)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		// Register in sphere.db.
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		if err := sphereStore.RegisterWorld(name, sourceRepo); err != nil {
			return err
		}

		// Build config from defaults.
		cfg := config.DefaultWorldConfig()
		cfg.World.SourceRepo = sourceRepo

		// Pre-Arc1 migration: adopt legacy config files.
		if preArc1 {
			// Migrate quality gates.
			gatesPath := filepath.Join(config.WorldDir(name), "forge", "quality-gates.txt")
			gates, err := forge.LoadQualityGates(gatesPath, nil)
			if err == nil && len(gates) > 0 {
				cfg.Forge.QualityGates = gates
			}

			// Migrate names.txt.
			namesPath := filepath.Join(config.WorldDir(name), "names.txt")
			if _, err := os.Stat(namesPath); err == nil {
				absPath, err := filepath.Abs(namesPath)
				if err == nil {
					cfg.Agents.NamePoolPath = absPath
				}
			}
		}

		// Write world.toml.
		if err := config.WriteWorldConfig(name, cfg); err != nil {
			return err
		}

		// Print confirmation.
		sourceDisplay := sourceRepo
		if sourceDisplay == "" {
			sourceDisplay = "none"
		}
		fmt.Printf("World %q initialized.\n", name)
		fmt.Printf("  Config:   %s\n", config.WorldConfigPath(name))
		fmt.Printf("  Database: %s\n", filepath.Join(config.StoreDir(), name+".db"))
		fmt.Printf("  Source:   %s\n", sourceDisplay)
		fmt.Println()
		fmt.Println("Next steps:")
		fmt.Printf("  sol store create --world=%s --title=\"First task\"\n", name)
		fmt.Printf("  sol cast <work-item-id> %s\n", name)
		return nil
	},
}

var worldListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all worlds",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		worlds, err := sphereStore.ListWorlds()
		if err != nil {
			return err
		}

		if worldListJSON {
			if len(worlds) == 0 {
				fmt.Println("[]")
				return nil
			}
			// Local struct intentionally omits updated_at and formats
			// created_at as a fixed-layout string for stable CLI output.
			type worldJSON struct {
				Name       string `json:"name"`
				SourceRepo string `json:"source_repo"`
				CreatedAt  string `json:"created_at"`
			}
			var items []worldJSON
			for _, w := range worlds {
				items = append(items, worldJSON{
					Name:       w.Name,
					SourceRepo: w.SourceRepo,
					CreatedAt:  w.CreatedAt.Format("2006-01-02T15:04:05Z"),
				})
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(items)
		}

		if len(worlds) == 0 {
			fmt.Println("No worlds initialized.")
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "NAME\tSOURCE REPO\tCREATED\n")
		for _, w := range worlds {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", w.Name, w.SourceRepo, w.CreatedAt.Format("2006-01-02T15:04:05Z"))
		}
		tw.Flush()
		fmt.Printf("\n%d world(s)\n", len(worlds))
		return nil
	},
}

var worldStatusCmd = &cobra.Command{
	Use:   "status <name>",
	Short: "Show world status with config",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		if err := config.RequireWorld(name); err != nil {
			return err
		}

		cfg, err := config.LoadWorldConfig(name)
		if err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		worldStore, err := store.OpenWorld(name)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		mgr := session.New()

		result, err := status.Gather(name, sphereStore, worldStore, worldStore, mgr)
		if err != nil {
			return err
		}

		status.GatherCaravans(result, sphereStore, gatedWorldOpener)

		if worldStatusJSON {
			type worldStatusOutput struct {
				*status.WorldStatus
				Config config.WorldConfig `json:"config"`
			}
			out := worldStatusOutput{
				WorldStatus: result,
				Config:      cfg,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		// Print config section.
		fmt.Printf("World: %s\n\n", name)
		fmt.Println("Config:")

		sourceDisplay := cfg.World.SourceRepo
		if sourceDisplay == "" {
			sourceDisplay = "(none)"
		}
		fmt.Printf("  Source repo:    %s\n", sourceDisplay)

		if cfg.Agents.Capacity == 0 {
			fmt.Printf("  Agent capacity: unlimited\n")
		} else {
			fmt.Printf("  Agent capacity: %d\n", cfg.Agents.Capacity)
		}
		fmt.Printf("  Model tier:    %s\n", cfg.Agents.ModelTier)
		fmt.Printf("  Quality gates: %d\n", len(cfg.Forge.QualityGates))

		namePool := "(default)"
		if cfg.Agents.NamePoolPath != "" {
			namePool = cfg.Agents.NamePoolPath
		}
		fmt.Printf("  Name pool:     %s\n", namePool)
		fmt.Println()

		// Print rest of status (shared with sol status).
		printWorldStatus(result)

		return nil
	},
}

var worldDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a world",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		if err := config.RequireWorld(name); err != nil {
			return err
		}

		if !worldDeleteConfirm {
			fmt.Printf("This will permanently delete world %q:\n", name)
			fmt.Printf("  - World database: %s\n", filepath.Join(config.StoreDir(), name+".db"))
			fmt.Printf("  - World directory: %s\n", config.WorldDir(name))
			fmt.Printf("  - Agent records for world %q\n", name)
			fmt.Println()
			fmt.Println("Run with --confirm to proceed.")
			return nil
		}

		// Check for active sessions.
		mgr := session.New()
		sessions, err := mgr.List()
		if err != nil {
			return fmt.Errorf("failed to list sessions: %w", err)
		}
		prefix := fmt.Sprintf("sol-%s-", name)
		var activeSessions []string
		for _, s := range sessions {
			if strings.HasPrefix(s.Name, prefix) && s.Alive {
				activeSessions = append(activeSessions, s.Name)
			}
		}
		if len(activeSessions) > 0 {
			fmt.Fprintf(os.Stderr, "Active sessions:\n")
			for _, s := range activeSessions {
				fmt.Fprintf(os.Stderr, "  %s\n", s)
			}
			fmt.Fprintf(os.Stderr, "\nStop sessions first, e.g.: sol session stop %s\n", activeSessions[0])
			return fmt.Errorf("cannot delete world %q: %d active session(s)", name, len(activeSessions))
		}

		// Open sphere store.
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}

		// Remove all sphere-level data for this world in a single transaction.
		if err := sphereStore.DeleteWorldData(name); err != nil {
			sphereStore.Close()
			return err
		}
		sphereStore.Close()

		// Delete world database file and SQLite sidecar files.
		dbPath := filepath.Join(config.StoreDir(), name+".db")
		if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove world database: %w", err)
		}
		os.Remove(dbPath + "-wal")
		os.Remove(dbPath + "-shm")

		// Delete world directory tree.
		worldDir := config.WorldDir(name)
		if err := os.RemoveAll(worldDir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove world directory: %w", err)
		}

		fmt.Printf("World %q deleted.\n", name)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(worldCmd)
	worldCmd.AddCommand(worldInitCmd)
	worldCmd.AddCommand(worldListCmd)
	worldCmd.AddCommand(worldStatusCmd)
	worldCmd.AddCommand(worldDeleteCmd)

	worldInitCmd.Flags().StringVar(&worldInitSourceRepo, "source-repo",
		"", "path to source git repository")
	worldListCmd.Flags().BoolVar(&worldListJSON, "json", false,
		"output as JSON")
	worldStatusCmd.Flags().BoolVar(&worldStatusJSON, "json", false,
		"output as JSON")
	worldDeleteCmd.Flags().BoolVar(&worldDeleteConfirm, "confirm", false,
		"confirm deletion")
}
