package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/governor"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/setup"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/worldexport"
	"github.com/nevinsm/sol/internal/worldsync"
	"github.com/spf13/cobra"
)

var (
	worldInitSourceRepo      string
	worldListJSON            bool
	worldStatusJSON          bool
	worldDeleteWorld         string
	worldDeleteConfirm       bool
	worldSyncWorld           string
	worldSyncAll             bool
	worldQueryTimeout        int
	worldCloneIncludeHistory bool
	worldImportName          string
)

var worldCmd = &cobra.Command{
	Use:     "world",
	Short:   "Manage worlds",
	GroupID: groupSetup,
}

var worldInitCmd = &cobra.Command{
	Use:          "init <name>",
	Short:        "Initialize a new world",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
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
			repo, err := dispatch.ResolveSourceRepo(name, config.WorldConfig{})
			if err == nil {
				sourceRepo = repo
			}
		}

		// Create directory tree.
		worldDir := config.WorldDir(name)
		if err := os.MkdirAll(filepath.Join(worldDir, "outposts"), 0o755); err != nil {
			return fmt.Errorf("failed to create world directory: %w", err)
		}

		// Clone source repo into managed repo directory.
		if sourceRepo != "" {
			if err := setup.CloneRepo(name, sourceRepo); err != nil {
				return err
			}
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
		fmt.Printf("  sol writ create --world=%s --title=\"First task\"\n", name)
		fmt.Printf("  sol cast <writ-id> --world=%s\n", name)
		return nil
	},
}

var worldListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List all worlds",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
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
	Use:          "status <name>",
	Short:        "Show world status with config",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
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

		fmt.Print(status.RenderWorldConfig(name, cfg))
		fmt.Print(status.RenderWorld(result))

		return nil
	},
}

var worldDeleteCmd = &cobra.Command{
	Use:          "delete",
	Short:        "Delete a world",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name, err := config.ResolveWorld(worldDeleteWorld)
		if err != nil {
			return err
		}

		if !worldDeleteConfirm {
			fmt.Printf("This will permanently delete world %q:\n", name)
			fmt.Printf("  - World database: %s\n", filepath.Join(config.StoreDir(), name+".db"))
			fmt.Printf("  - World directory: %s\n", config.WorldDir(name))
			fmt.Printf("  - Agent records for world %q\n", name)
			fmt.Println()
			fmt.Println("Run with --confirm to proceed.")
			return &exitError{code: 1}
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
		defer sphereStore.Close()

		// Remove all sphere-level data for this world in a single transaction.
		if err := sphereStore.DeleteWorldData(name); err != nil {
			return err
		}

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

var worldSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync the managed repo with its remote",
	Long: `Fetch and pull latest changes from the source repo's origin.
If the managed repo doesn't exist yet but source_repo is configured
in world.toml, clones it first.

With --all, also syncs forge worktree and notifies running envoy/governor sessions.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name, err := config.ResolveWorld(worldSyncWorld)
		if err != nil {
			return err
		}

		repoPath := config.RepoPath(name)

		// If managed repo doesn't exist, try to clone from config.
		if _, err := os.Stat(repoPath); os.IsNotExist(err) {
			worldCfg, err := config.LoadWorldConfig(name)
			if err != nil {
				return err
			}
			if worldCfg.World.SourceRepo == "" {
				return fmt.Errorf("no managed repo and no source_repo configured for world %q", name)
			}
			fmt.Printf("Cloning %s into managed repo...\n", worldCfg.World.SourceRepo)
			if err := setup.CloneRepo(name, worldCfg.World.SourceRepo); err != nil {
				return err
			}
			fmt.Printf("Managed repo created for world %q\n", name)
			return nil
		}

		// Sync managed repo.
		if err := worldsync.SyncRepo(name); err != nil {
			return err
		}
		fmt.Printf("Synced managed repo for world %q\n", name)

		// If --all, propagate to components.
		if worldSyncAll {
			worldCfg, err := config.LoadWorldConfig(name)
			if err != nil {
				return err
			}

			cfg, err := resolveForgeConfig(name, worldCfg)
			if err != nil {
				return err
			}

			sphereStore, err := store.OpenSphere()
			if err != nil {
				return err
			}
			defer sphereStore.Close()

			mgr := session.New()
			results := worldsync.SyncAllComponents(name, cfg.TargetBranch, sphereStore, mgr)

			for _, r := range results {
				if r.Err != nil {
					fmt.Printf("[fail] %s: %v\n", r.Component, r.Err)
				} else {
					fmt.Printf("[ok] %s\n", r.Component)
				}
			}
		}

		return nil
	},
}

var worldQueryCmd = &cobra.Command{
	Use:          "query <name> <question>",
	Short:        "Query a world's governor for information",
	Long: `Inject a question into the governor's tmux session and wait for a response.

The governor reads the question from .query/pending.md, writes its answer to
.query/response.md, and the CLI returns the response. If the governor is not
running, returns an error (callers should fall back to the static world summary).`,
	Args:         cobra.MinimumNArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world := args[0]
		question := strings.Join(args[1:], " ")

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		// Check governor session is running.
		mgr := session.New()
		sessName := config.SessionName(world, "governor")
		if !mgr.Exists(sessName) {
			return fmt.Errorf("governor not running for world %q (start with 'sol governor start --world=%s')", world, world)
		}

		// Clear any stale query files.
		governor.ClearQuery(world)

		// Write question to pending.md.
		if err := governor.WritePending(world, question); err != nil {
			return err
		}

		// Inject notification into governor's tmux session.
		notification := fmt.Sprintf("A query has been submitted. Read the question from %s, write your response to %s, then continue your work.",
			governor.PendingPath(world), governor.ResponsePath(world))
		if err := mgr.Inject(sessName, notification, true); err != nil {
			governor.ClearQuery(world)
			return fmt.Errorf("failed to inject query into governor session: %w", err)
		}

		// Poll for response with timeout.
		timeout := time.Duration(worldQueryTimeout) * time.Second
		deadline := time.Now().Add(timeout)
		pollInterval := 2 * time.Second

		for time.Now().Before(deadline) {
			response, found, err := governor.ReadResponse(world)
			if err != nil {
				governor.ClearQuery(world)
				return err
			}
			if found {
				fmt.Print(response)
				governor.ClearQuery(world)
				return nil
			}
			time.Sleep(pollInterval)
		}

		governor.ClearQuery(world)
		return fmt.Errorf("query timed out after %ds waiting for governor response in world %q", worldQueryTimeout, world)
	},
}

var worldSummaryCmd = &cobra.Command{
	Use:          "summary <name>",
	Short:        "Show a world's governor-maintained summary",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world := args[0]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		summaryPath := governor.WorldSummaryPath(world)
		data, err := os.ReadFile(summaryPath)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("No world summary found for world %q\n", world)
				fmt.Printf("Start a governor first: sol governor start --world=%s\n", world)
				return nil
			}
			return fmt.Errorf("failed to read world summary: %w", err)
		}

		fmt.Print(string(data))
		return nil
	},
}

var worldCloneCmd = &cobra.Command{
	Use:          "clone <source> <target>",
	Short:        "Clone a world",
	Long: `Duplicate a world with copied configuration, database state (writs,
dependencies), and directory structure. Credentials and tethers are NOT copied.
The new world gets a fresh agent pool.

Agent state (history, memories) is excluded by default. Use --include-history
to copy it.`,
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		source := args[0]
		target := args[1]

		// Validate target name.
		if err := config.ValidateWorldName(target); err != nil {
			return err
		}

		// Require source exists.
		if err := config.RequireWorld(source); err != nil {
			return err
		}

		// Ensure target does not exist.
		tomlPath := config.WorldConfigPath(target)
		if _, err := os.Stat(tomlPath); err == nil {
			return fmt.Errorf("world %q already exists", target)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to check target world config %q: %w", tomlPath, err)
		}

		// Load source config.
		srcCfg, err := config.LoadWorldConfig(source)
		if err != nil {
			return fmt.Errorf("failed to load source world config: %w", err)
		}

		// Create target directory tree.
		targetDir := config.WorldDir(target)
		if err := os.MkdirAll(filepath.Join(targetDir, "outposts"), 0o755); err != nil {
			return fmt.Errorf("failed to create target world directory: %w", err)
		}

		// Clone the managed repo from source's repo (not the source world's repo/ dir directly).
		sourceRepo := srcCfg.World.SourceRepo
		repoPath := config.RepoPath(source)
		if _, err := os.Stat(repoPath); err == nil {
			// Source has a managed repo — clone from it.
			if err := setup.CloneRepo(target, repoPath); err != nil {
				return fmt.Errorf("failed to clone managed repo: %w", err)
			}
		} else if sourceRepo != "" {
			// No managed repo but source_repo configured — clone from origin.
			if err := setup.CloneRepo(target, sourceRepo); err != nil {
				return fmt.Errorf("failed to clone source repo: %w", err)
			}
		}

		// Ensure .store/ directory exists.
		if err := config.EnsureDirs(); err != nil {
			return fmt.Errorf("failed to create store directory: %w", err)
		}

		// Create target world database (triggers schema migration).
		worldStore, err := store.OpenWorld(target)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		// Copy database state from source.
		if err := store.CloneWorldData(source, target, worldCloneIncludeHistory); err != nil {
			return fmt.Errorf("failed to clone world data: %w", err)
		}

		// Register in sphere.db.
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		if err := sphereStore.RegisterWorld(target, sourceRepo); err != nil {
			return err
		}

		// Build target config from source — clear transient/local state.
		targetCfg := srcCfg
		targetCfg.World.Sleeping = false
		targetCfg.World.DefaultAccount = ""

		if err := config.WriteWorldConfig(target, targetCfg); err != nil {
			return err
		}

		// Print confirmation.
		fmt.Printf("World %q cloned from %q.\n", target, source)
		fmt.Printf("  Config:   %s\n", config.WorldConfigPath(target))
		fmt.Printf("  Database: %s\n", filepath.Join(config.StoreDir(), target+".db"))
		if sourceRepo != "" {
			fmt.Printf("  Source:   %s\n", sourceRepo)
		}
		if worldCloneIncludeHistory {
			fmt.Printf("  History:  included\n")
		}
		return nil
	},
}

var worldSleepCmd = &cobra.Command{
	Use:          "sleep <name>",
	Short:        "Mark a world as sleeping and stop its services",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		if err := config.RequireWorld(name); err != nil {
			return err
		}

		cfg, err := config.LoadWorldConfig(name)
		if err != nil {
			return err
		}

		if cfg.World.Sleeping {
			fmt.Printf("World %q is already sleeping.\n", name)
			return nil
		}

		// Mark sleeping in config.
		cfg.World.Sleeping = true
		if err := config.WriteWorldConfig(name, cfg); err != nil {
			return fmt.Errorf("failed to write world config: %w", err)
		}

		// Stop world services.
		mgr := session.New()
		stopped := 0

		for _, role := range []string{"forge", "sentinel", "governor"} {
			sessName := dispatch.SessionName(name, role)
			if mgr.Exists(sessName) {
				if err := mgr.Stop(sessName, false); err != nil {
					fmt.Fprintf(os.Stderr, "  warning: failed to stop %s: %v\n", role, err)
				} else {
					fmt.Printf("  stopped %s\n", role)
					stopped++
				}
			}
		}

		if stopped == 0 {
			fmt.Printf("World %q is now sleeping (no services were running).\n", name)
		} else {
			fmt.Printf("World %q is now sleeping (%d service(s) stopped).\n", name, stopped)
		}
		return nil
	},
}

var worldWakeCmd = &cobra.Command{
	Use:          "wake <name>",
	Short:        "Mark a world as active and start its services",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		if err := config.RequireWorld(name); err != nil {
			return err
		}

		cfg, err := config.LoadWorldConfig(name)
		if err != nil {
			return err
		}

		if !cfg.World.Sleeping {
			fmt.Printf("World %q is already active.\n", name)
			return nil
		}

		// Mark active in config.
		cfg.World.Sleeping = false
		if err := config.WriteWorldConfig(name, cfg); err != nil {
			return fmt.Errorf("failed to write world config: %w", err)
		}

		fmt.Printf("World %q is now active.\n", name)

		// Start services via subcommands.
		solBin, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: cannot find sol binary to start services: %v\n", err)
			return nil
		}

		mgr := session.New()

		// Start sentinel.
		sentinelSess := dispatch.SessionName(name, "sentinel")
		if !mgr.Exists(sentinelSess) {
			out, err := exec.Command(solBin, "sentinel", "start", "--world="+name).CombinedOutput()
			if err != nil {
				fmt.Fprintf(os.Stderr, "  warning: failed to start sentinel: %s\n", strings.TrimSpace(string(out)))
			} else {
				fmt.Printf("  started sentinel\n")
			}
		}

		// Start forge.
		forgeSess := dispatch.SessionName(name, "forge")
		if !mgr.Exists(forgeSess) {
			out, err := exec.Command(solBin, "forge", "start", "--world="+name).CombinedOutput()
			if err != nil {
				fmt.Fprintf(os.Stderr, "  warning: failed to start forge: %s\n", strings.TrimSpace(string(out)))
			} else {
				fmt.Printf("  started forge\n")
			}
		}

		return nil
	},
}

var worldImportCmd = &cobra.Command{
	Use:   "import <archive>",
	Short: "Import a world from an export archive",
	Long: `Restore a world from a .tar.gz archive produced by sol world export.

Validates the archive manifest and schema compatibility before restoring.
Refuses to import if the world name already exists — delete it first or
use --name to import under a different name.

Agent states are reset to idle on import (no active sessions exist for
imported agents). Ephemeral state (repo, worktrees, sessions) is not
restored — run sol world sync after import to clone the managed repo.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		archivePath := args[0]

		result, err := worldexport.Import(worldexport.ImportOptions{
			ArchivePath: archivePath,
			Name:        worldImportName,
		})
		if err != nil {
			return err
		}

		fmt.Printf("World %q imported.\n", result.World)
		fmt.Printf("  Config:   %s\n", config.WorldConfigPath(result.World))
		fmt.Printf("  Database: %s\n", filepath.Join(config.StoreDir(), result.World+".db"))
		fmt.Println()

		if result.SourceRepo != "" {
			fmt.Println("Next steps:")
			fmt.Printf("  sol world sync --world=%s   # clone the managed repo\n", result.World)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(worldCmd)
	worldCmd.AddCommand(worldInitCmd)
	worldCmd.AddCommand(worldListCmd)
	worldCmd.AddCommand(worldStatusCmd)
	worldCmd.AddCommand(worldDeleteCmd)
	worldCmd.AddCommand(worldCloneCmd)
	worldCmd.AddCommand(worldSyncCmd)
	worldCmd.AddCommand(worldSummaryCmd)
	worldCmd.AddCommand(worldQueryCmd)
	worldCmd.AddCommand(worldSleepCmd)
	worldCmd.AddCommand(worldWakeCmd)
	worldCmd.AddCommand(worldImportCmd)

	worldInitCmd.Flags().StringVar(&worldInitSourceRepo, "source-repo",
		"", "git URL or local path to source repository")
	worldListCmd.Flags().BoolVar(&worldListJSON, "json", false,
		"output as JSON")
	worldStatusCmd.Flags().BoolVar(&worldStatusJSON, "json", false,
		"output as JSON")
	worldDeleteCmd.Flags().StringVar(&worldDeleteWorld, "world", "", "world name")
	worldDeleteCmd.Flags().BoolVar(&worldDeleteConfirm, "confirm", false,
		"confirm deletion")
	worldCloneCmd.Flags().BoolVar(&worldCloneIncludeHistory, "include-history", false,
		"include agent history and memories in clone")
	worldSyncCmd.Flags().StringVar(&worldSyncWorld, "world", "", "world name")
	worldSyncCmd.Flags().BoolVar(&worldSyncAll, "all", false,
		"also sync forge, envoys, and governor")
	worldQueryCmd.Flags().IntVar(&worldQueryTimeout, "timeout", 120,
		"seconds to wait for governor response")
	worldImportCmd.Flags().StringVar(&worldImportName, "name", "",
		"import under a different name (rewrites agent IDs and references)")
}
