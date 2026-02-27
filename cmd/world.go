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

		// Check if world.toml already exists.
		tomlPath := config.WorldConfigPath(name)
		if _, err := os.Stat(tomlPath); err == nil {
			return fmt.Errorf("world %q is already initialized", name)
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
			repo, err := dispatch.DiscoverSourceRepo()
			if err == nil {
				sourceRepo = repo
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
		worldStore.Close()

		// Register in sphere.db.
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		if err := sphereStore.RegisterWorld(name, sourceRepo); err != nil {
			sphereStore.Close()
			return err
		}
		sphereStore.Close()

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
		home := config.Home()
		sourceDisplay := sourceRepo
		if sourceDisplay == "" {
			sourceDisplay = "none"
		}
		fmt.Printf("World %q initialized.\n", name)
		fmt.Printf("  Config:   %s/%s/world.toml\n", home, name)
		fmt.Printf("  Database: %s/.store/%s.db\n", home, name)
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

		if len(worlds) == 0 {
			fmt.Println("No worlds initialized.")
			return nil
		}

		if worldListJSON {
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

		status.GatherCaravans(result, sphereStore, store.OpenWorld)

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

		// Print rest of status (prefect, forge, etc.).
		if result.Prefect.Running {
			fmt.Printf("Prefect: running (pid %d)\n", result.Prefect.PID)
		} else {
			fmt.Println("Prefect: not running")
		}

		if result.Forge.Running {
			fmt.Printf("Forge: running (%s)\n", result.Forge.SessionName)
		} else {
			fmt.Println("Forge: not running")
		}

		if result.Chronicle.Running {
			fmt.Printf("Chronicle: running (%s)\n", result.Chronicle.SessionName)
		} else {
			fmt.Println("Chronicle: not running")
		}

		if result.Sentinel.Running {
			fmt.Printf("Sentinel: running (%s)\n", result.Sentinel.SessionName)
		} else {
			fmt.Println("Sentinel: not running")
		}

		fmt.Println()

		if len(result.Agents) == 0 {
			fmt.Println("No agents registered.")
		} else {
			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(tw, "AGENT\tSTATE\tSESSION\tWORK\n")
			for _, a := range result.Agents {
				sess := "-"
				if a.State == "working" || a.State == "stalled" {
					if a.SessionAlive {
						sess = "alive"
					} else {
						sess = "dead!"
					}
				}
				work := "-"
				if a.TetherItem != "" {
					work = fmt.Sprintf("%s: %s", a.TetherItem, a.WorkTitle)
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", a.Name, a.State, sess, work)
			}
			tw.Flush()
			fmt.Println()
		}

		// Merge queue line.
		mq := result.MergeQueue
		if mq.Total == 0 {
			fmt.Println("Merge Queue: empty")
		} else {
			fmt.Printf("Merge Queue: %d ready, %d in progress, %d failed\n",
				mq.Ready, mq.Claimed, mq.Failed)
		}

		// Summary line.
		parts := fmt.Sprintf("%d working, %d idle", result.Summary.Working, result.Summary.Idle)
		if result.Summary.Stalled > 0 {
			parts += fmt.Sprintf(", %d stalled", result.Summary.Stalled)
		}
		if result.Summary.Dead > 0 {
			parts += fmt.Sprintf(", %d dead session", result.Summary.Dead)
			if result.Summary.Dead > 1 {
				parts += "s"
			}
		}
		fmt.Printf("Summary: %d agents (%s)\n", result.Summary.Total, parts)
		fmt.Printf("Health: %s\n", result.HealthString())

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

		home := config.Home()

		if !worldDeleteConfirm {
			fmt.Printf("This will permanently delete world %q:\n", name)
			fmt.Printf("  - World database: %s/.store/%s.db\n", home, name)
			fmt.Printf("  - World directory: %s/%s/\n", home, name)
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
			return fmt.Errorf("cannot delete world %q: %d active session(s)\n"+
				"Stop sessions first: sol session stop %s",
				name, len(activeSessions), activeSessions[0])
		}

		// Open sphere store.
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}

		// Remove agents for this world.
		if err := sphereStore.DeleteAgentsForWorld(name); err != nil {
			sphereStore.Close()
			return err
		}

		// Remove world record.
		if err := sphereStore.RemoveWorld(name); err != nil {
			sphereStore.Close()
			return err
		}
		sphereStore.Close()

		// Delete world database file.
		dbPath := filepath.Join(config.StoreDir(), name+".db")
		if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove world database: %w", err)
		}

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
