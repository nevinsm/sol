package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	cliworlds "github.com/nevinsm/sol/internal/cliapi/worlds"
	"github.com/nevinsm/sol/internal/cliformat"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/sentinel"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/setup"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/nevinsm/sol/internal/worldexport"
	"github.com/nevinsm/sol/internal/worldsync"
	"github.com/spf13/cobra"
)

var (
	worldInitSourceRepo      string
	worldInitJSON            bool
	worldListJSON            bool
	worldStatusJSON          bool
	worldDeleteWorld         string
	worldDeleteConfirm       bool
	worldDeleteJSON          bool
	worldSyncWorld           string
	worldSyncAll             bool
	worldSyncJSON            bool
	worldCloneIncludeHistory bool
	worldCloneJSON           bool
	worldImportName          string
	worldImportJSON          bool
	worldSleepForce          bool
	worldSleepJSON           bool
	worldWakeJSON            bool
)

var worldCmd = &cobra.Command{
	Use:     "world",
	Short:   "Manage worlds",
	GroupID: groupSetup,
}

// positionalOrDeprecatedFlag returns the world name from a positional
// argument, falling back to the deprecated --world flag value. If both
// are provided it returns an error. If --world is used (without a
// positional), a deprecation notice is printed to stderr.
//
// The returned value is a "flag value" suitable for config.ResolveWorld:
// an empty string means "no explicit world specified", and ResolveWorld
// will fall back to env/path detection.
func positionalOrDeprecatedFlag(args []string, deprecatedFlag, subcmd string) (string, error) {
	if len(args) == 1 && deprecatedFlag != "" {
		return "", fmt.Errorf("cannot use both positional name and --world flag")
	}
	if len(args) == 1 {
		return args[0], nil
	}
	if deprecatedFlag != "" {
		fmt.Fprintf(os.Stderr,
			"warning: --world is deprecated for 'sol world %s'; use positional argument: sol world %s <name>\n",
			subcmd, subcmd)
		return deprecatedFlag, nil
	}
	return "", nil
}

var worldInitCmd = &cobra.Command{
	Use:   "init <name>",
	Short: "Initialize a new world",
	Long: `Create a new world with directory structure, database, and configuration.

Creates:
  - World directory at $SOL_HOME/<name>/ with outposts/ subdirectory
  - World database (<name>.db) with schema migrations
  - Default world.toml configuration
  - Managed repo clone (if --source-repo is provided)

Registers the world in sphere.db. If a pre-Arc1 database exists (DB without
world.toml), migrates legacy quality gates and name pool settings.

world.toml configuration reference:

  [world]
  source_repo = "/path/to/repo"   # persistent source repo binding
  branch = "main"                 # primary branch (used for merges and guard protection)
  protected_branches = []         # additional protected branches (glob patterns OK)

  [agents]
  max_active = 10                 # max concurrent agents (0 = unlimited)
  name_pool_path = ""             # custom name pool file (empty = built-in)
  model = "sonnet"                # default model for all roles (passthrough to runtime)

  [agents.models.claude]          # per-runtime, per-role model overrides
  outpost = "sonnet"              # overrides agents.model for outpost agents
  envoy = "opus"                  # overrides agents.model for envoy agents
  forge = "sonnet"                # overrides agents.model for forge

  [forge]
  quality_gates = ["make test"]   # commands that must pass before merge
  gate_timeout = "5m"             # per-gate timeout

Resolution order for model: agents.models.<runtime>.<role> → agents.model → adapter.DefaultModel().
Any non-empty string is valid (passed through to the runtime).`,
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
				return fmt.Errorf("failed to clone source repo: %w", err)
			}
		}

		// Ensure .store/ directory exists.
		if err := config.EnsureDirs(); err != nil {
			return fmt.Errorf("failed to create store directory: %w", err)
		}

		// Create world database (triggers schema migration).
		worldStore, err := store.OpenWorld(name)
		if err != nil {
			return fmt.Errorf("failed to open world store: %w", err)
		}
		defer worldStore.Close()

		// Register in sphere.db.
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		if err := sphereStore.RegisterWorld(name, sourceRepo); err != nil {
			return fmt.Errorf("failed to register world: %w", err)
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
			return fmt.Errorf("failed to write world config: %w", err)
		}

		if worldInitJSON {
			w, err := sphereStore.GetWorld(name)
			if err != nil {
				return fmt.Errorf("failed to get world record: %w", err)
			}
			state := "active"
			if cfg.World.Sleeping {
				state = "sleeping"
			}
			out := cliworlds.FromStoreWorld(*w, cliworlds.WorldInfo{
				Branch:   cfg.World.Branch,
				State:    state,
				Health:   "unknown",
				Sleeping: cfg.World.Sleeping,
			})
			return printJSON(out)
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
	Use:   "list",
	Short: "List all worlds",
	Long: `List all initialized worlds with operational state.

Columns:
  NAME         world name
  STATE        active or sleeping (from world.toml)
  HEALTH       healthy / unhealthy / degraded / unknown — same logic as 'sol status'
  AGENTS       count of working outpost agents
  QUEUE        pending merge requests (ready + claimed + failed)
  SOURCE_REPO  source repository for the managed clone
  CREATED      world creation time

Sleeping worlds report '-' for HEALTH because their daemons are stopped.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		worlds, err := sphereStore.ListWorlds()
		if err != nil {
			return fmt.Errorf("failed to list worlds: %w", err)
		}

		// Reuse the per-world health/agent computation that 'sol status'
		// uses. GatherSphere returns one WorldSummary per world; we index
		// by name and look up below.
		mgr := session.New()
		summaryByName := make(map[string]status.WorldSummary, len(worlds))
		claimedByName := make(map[string]int, len(worlds))
		if len(worlds) > 0 {
			sphereStatus := status.GatherSphere(sphereStore, sphereStore, mgr,
				gatedWorldOpener, sphereStore, sphereStore)
			for _, ws := range sphereStatus.Worlds {
				summaryByName[ws.Name] = ws
			}

			// WorldSummary exposes ready/failed counts but not claimed.
			// Pull claimed counts directly from each world store so QUEUE
			// reflects the full pending pipeline.
			for _, w := range worlds {
				sum := summaryByName[w.Name]
				if sum.Sleeping {
					continue
				}
				wsStore, err := gatedWorldOpener(w.Name)
				if err != nil {
					continue
				}
				if mrs, err := wsStore.ListMergeRequests("claimed"); err == nil {
					claimedByName[w.Name] = len(mrs)
				}
				wsStore.Close()
			}
		}

		now := time.Now()

		// stateOf returns the operational state cell for a world.
		stateOf := func(sum status.WorldSummary) string {
			if sum.Sleeping {
				return "sleeping"
			}
			return "active"
		}
		// healthOf normalises the WorldSummary.Health string to one of
		// healthy/unhealthy/degraded/unknown. Sleeping worlds report
		// 'unknown' because their daemons are stopped.
		healthOf := func(sum status.WorldSummary) string {
			if sum.Sleeping {
				return "unknown"
			}
			switch sum.Health {
			case "healthy", "unhealthy", "degraded":
				return sum.Health
			default:
				return "unknown"
			}
		}
		queueOf := func(name string, sum status.WorldSummary) int {
			return sum.MRReady + claimedByName[name] + sum.MRFailed
		}

		if worldListJSON {
			if len(worlds) == 0 {
				fmt.Println("[]")
				return nil
			}
			items := make([]cliworlds.WorldListItem, 0, len(worlds))
			for _, w := range worlds {
				sum := summaryByName[w.Name]
				items = append(items, cliworlds.WorldListItem{
					Name:       w.Name,
					State:      stateOf(sum),
					Health:     healthOf(sum),
					Agents:     sum.Working,
					Queue:      queueOf(w.Name, sum),
					SourceRepo: w.SourceRepo,
					CreatedAt:  w.CreatedAt.Format("2006-01-02T15:04:05Z"),
				})
			}
			return printJSON(items)
		}

		if len(worlds) == 0 {
			fmt.Println("No worlds initialized.")
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "NAME\tSTATE\tHEALTH\tAGENTS\tQUEUE\tSOURCE REPO\tCREATED\n")
		for _, w := range worlds {
			sum := summaryByName[w.Name]
			health := healthOf(sum)
			// In the table, sleeping worlds show '-' rather than the
			// 'unknown' literal — STATE already conveys why.
			if sum.Sleeping {
				health = cliformat.EmptyMarker
			}
			sourceRepo := w.SourceRepo
			if sourceRepo == "" {
				sourceRepo = cliformat.EmptyMarker
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
				w.Name,
				stateOf(sum),
				health,
				sum.Working,
				queueOf(w.Name, sum),
				sourceRepo,
				cliformat.FormatTimestampOrRelative(w.CreatedAt, now),
			)
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
			return fmt.Errorf("failed to load world config: %w", err)
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		worldStore, err := store.OpenWorld(name)
		if err != nil {
			return fmt.Errorf("failed to open world store: %w", err)
		}
		defer worldStore.Close()

		mgr := session.New()

		result, err := status.Gather(name, sphereStore, worldStore, worldStore, mgr)
		if err != nil {
			return fmt.Errorf("failed to gather world status: %w", err)
		}

		status.GatherCaravans(result, sphereStore, gatedWorldOpener)

		if worldStatusJSON {
			out := cliworlds.StatusResponse{
				WorldStatus: result,
				Config:      cfg,
			}
			return printJSON(out)
		}

		fmt.Print(status.RenderWorldConfig(name, cfg))
		fmt.Print(status.RenderWorld(result))

		return nil
	},
}

var worldDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a world",
	Long: `Permanently delete a world and all associated data:
  - World database (writs, merge requests, dependencies)
  - World directory (repo, outposts, worktrees, config)
  - Agent records for the world in sphere.db

Refuses to delete if any agent sessions are still running — stop them first.
Requires --confirm to proceed; without it, prints what would be deleted and exits.

The world name is provided as a positional argument:

  sol world delete <name> --confirm

The legacy --world flag is accepted for backward compatibility but
prints a deprecation notice on stderr.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		flagValue, err := positionalOrDeprecatedFlag(args, worldDeleteWorld, "delete")
		if err != nil {
			return err
		}
		name, err := config.ResolveWorld(flagValue)
		if err != nil {
			return err
		}

		if !worldDeleteConfirm {
			if !worldDeleteJSON {
				fmt.Printf("This will permanently delete world %q:\n", name)
				fmt.Printf("  - World database: %s\n", filepath.Join(config.StoreDir(), name+".db"))
				fmt.Printf("  - World directory: %s\n", config.WorldDir(name))
				fmt.Printf("  - Agent records for world %q\n", name)
				fmt.Println()
				fmt.Println("Run with --confirm to proceed.")
			}
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

		// Stop per-world daemons (best-effort — they may not be running).
		if err := forge.StopProcess(name, 5*time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "note: forge not stopped: %v\n", err)
		}
		if err := sentinel.StopProcess(name, 5*time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "note: sentinel not stopped: %v\n", err)
		}

		// Open sphere store.
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		// Remove all sphere-level data for this world in a single transaction.
		if err := sphereStore.DeleteWorldData(name); err != nil {
			return fmt.Errorf("failed to delete world data: %w", err)
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

		if worldDeleteJSON {
			return printJSON(cliworlds.DeleteResponse{
				Name:    name,
				Deleted: true,
			})
		}

		fmt.Printf("World %q deleted.\n", name)
		return nil
	},
}

var worldSyncCmd = &cobra.Command{
	Use:   "sync [name]",
	Short: "Sync the managed repo with its remote",
	Long: `Fetch and pull latest changes from the source repo's origin.
If the managed repo doesn't exist yet but source_repo is configured
in world.toml, clones it first.

With --all, also syncs forge worktree and notifies running envoy sessions.

The world name is provided as a positional argument:

  sol world sync <name> [--all]

If omitted, the world is detected from $SOL_WORLD or the current directory.
The legacy --world flag is accepted for backward compatibility but prints
a deprecation notice on stderr.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		flagValue, err := positionalOrDeprecatedFlag(args, worldSyncWorld, "sync")
		if err != nil {
			return err
		}
		name, err := config.ResolveWorld(flagValue)
		if err != nil {
			return err
		}

		repoPath := config.RepoPath(name)

		// If managed repo doesn't exist, try to clone from config.
		if _, err := os.Stat(repoPath); os.IsNotExist(err) {
			worldCfg, err := config.LoadWorldConfig(name)
			if err != nil {
				return fmt.Errorf("failed to load world config: %w", err)
			}
			if worldCfg.World.SourceRepo == "" {
				return fmt.Errorf("no managed repo and no source_repo configured for world %q", name)
			}
			if !worldSyncJSON {
				fmt.Printf("Cloning %s into managed repo...\n", worldCfg.World.SourceRepo)
			}
			if err := setup.CloneRepo(name, worldCfg.World.SourceRepo); err != nil {
				return fmt.Errorf("failed to clone source repo: %w", err)
			}
			if worldSyncJSON {
				headCommit := getRepoHead(repoPath)
				return printJSON(cliworlds.SyncResponse{
					Name:       name,
					Fetched:    true,
					HeadCommit: headCommit,
				})
			}
			fmt.Printf("Managed repo created for world %q\n", name)
			return nil
		}

		// Sync managed repo.
		outcome, err := worldsync.SyncRepo(name)
		if err != nil {
			return fmt.Errorf("failed to sync managed repo: %w", err)
		}

		// If --all, propagate to components.
		if worldSyncAll {
			worldCfg, err := config.LoadWorldConfig(name)
			if err != nil {
				return fmt.Errorf("failed to load world config: %w", err)
			}

			cfg, err := resolveForgeConfig(name, worldCfg)
			if err != nil {
				return fmt.Errorf("failed to resolve forge config: %w", err)
			}

			sphereStore, err := store.OpenSphere()
			if err != nil {
				return fmt.Errorf("failed to open sphere store: %w", err)
			}
			defer sphereStore.Close()

			mgr := session.New()
			results := worldsync.SyncAllComponents(name, cfg.TargetBranch, sphereStore, mgr, outcome)

			if !worldSyncJSON {
				for _, r := range results {
					if r.Err != nil {
						fmt.Printf("[fail] %s: %v\n", r.Component, r.Err)
					} else {
						fmt.Printf("[ok] %s\n", r.Component)
					}
				}
			}
		}

		if worldSyncJSON {
			return printJSON(cliworlds.SyncResponse{
				Name:       name,
				Fetched:    outcome.Advanced,
				HeadCommit: outcome.NewHead,
			})
		}

		fmt.Printf("Synced managed repo for world %q\n", name)
		return nil
	},
}

var worldCloneCmd = &cobra.Command{
	Use:   "clone <source> <target>",
	Short: "Clone a world",
	Long: `Duplicate a world with copied configuration, database state (writs,
dependencies), and directory structure. Credentials and tethers are NOT copied.
The new world gets a fresh agent pool.

Agent state (history, token usage) is excluded by default. Use --include-history
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
			return fmt.Errorf("failed to open target world store: %w", err)
		}
		defer worldStore.Close()

		// Copy database state from source.
		if err := store.CloneWorldData(source, target, worldCloneIncludeHistory); err != nil {
			return fmt.Errorf("failed to clone world data: %w", err)
		}

		// Register in sphere.db.
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		if err := sphereStore.RegisterWorld(target, sourceRepo); err != nil {
			return fmt.Errorf("failed to register cloned world: %w", err)
		}

		// Copy raw source world.toml to target — avoids freezing sol.toml values.
		// Clear transient/local state that shouldn't carry over.
		if err := config.CopyWorldConfigRaw(source, target, func(raw map[string]interface{}) {
			if worldSec, ok := raw["world"].(map[string]interface{}); ok {
				delete(worldSec, "sleeping")
				delete(worldSec, "default_account")
			}
		}); err != nil {
			return fmt.Errorf("failed to write world config: %w", err)
		}

		// Load the target config (resolved) for JSON output and confirmation.
		targetCfg, err := config.LoadWorldConfig(target)
		if err != nil {
			return fmt.Errorf("failed to load target world config: %w", err)
		}

		if worldCloneJSON {
			w, err := sphereStore.GetWorld(target)
			if err != nil {
				return fmt.Errorf("failed to get world record: %w", err)
			}
			state := "active"
			if targetCfg.World.Sleeping {
				state = "sleeping"
			}
			out := cliworlds.FromStoreWorld(*w, cliworlds.WorldInfo{
				Branch:   targetCfg.World.Branch,
				State:    state,
				Health:   "unknown",
				Sleeping: targetCfg.World.Sleeping,
			})
			return printJSON(out)
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

// forceStopStabilityTimeout is the maximum time to wait for an outpost agent
// to stabilize (reach idle prompt) before killing the session. Brief save is
// best-effort, not a gate.
const forceStopStabilityTimeout = 30 * time.Second

var worldSleepCmd = &cobra.Command{
	Use:   "sleep <name>",
	Short: "Mark a world as sleeping and stop its services",
	Long: `Mark a world as sleeping, which stops world services (sentinel, forge)
and activates dispatch gates that prevent new work from being cast.

With --force, also stops all outpost agent sessions immediately:
  - Waits up to 30 seconds for session stability before killing
  - Kills sessions that don't stabilize in time
  - Returns writs to "open" status, sets agents to "idle", clears tethers
  - Warns envoy sessions but does not stop them (human-directed)`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		if err := config.RequireWorld(name); err != nil {
			return err
		}

		cfg, err := config.LoadWorldConfig(name)
		if err != nil {
			return fmt.Errorf("failed to load world config: %w", err)
		}

		if cfg.World.Sleeping {
			if worldSleepJSON {
				return printWorldJSON(name, cfg)
			}
			fmt.Printf("World %q is already sleeping.\n", name)
			return nil
		}

		// Mark sleeping in config FIRST — this activates dispatch gates.
		// If we crash after this point, gates are active and running agents
		// finish naturally (soft sleep behavior).
		// Use PatchWorldConfig to avoid freezing sol.toml values into world.toml.
		cfg.World.Sleeping = true
		if err := config.PatchWorldConfig(name, func(raw map[string]interface{}) {
			sec := raw["world"]
			worldSec, ok := sec.(map[string]interface{})
			if !ok {
				worldSec = make(map[string]interface{})
				raw["world"] = worldSec
			}
			worldSec["sleeping"] = true
		}); err != nil {
			return fmt.Errorf("failed to write world config: %w", err)
		}

		// Stop world services.
		mgr := session.New()
		servicesStopped := 0

		// Stop sentinel via PID file (it's a daemon process, not a tmux session).
		if pid := sentinel.ReadPID(name); pid > 0 && sentinel.IsRunning(pid) {
			proc, err := os.FindProcess(pid)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  warning: failed to find sentinel process: %v\n", err)
			} else if err := proc.Signal(syscall.SIGTERM); err != nil {
				fmt.Fprintf(os.Stderr, "  warning: failed to stop sentinel: %v\n", err)
			} else {
				fmt.Printf("  stopped sentinel\n")
				servicesStopped++
			}
		}

		// Stop forge via PID file (it's a daemon process, not a tmux session).
		if err := forge.StopProcess(name, 5*time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: failed to stop forge: %v\n", err)
		} else {
			fmt.Printf("  stopped forge\n")
			servicesStopped++
		}

		if !worldSleepForce {
			// Soft sleep: count running outpost agents for reporting.
			agentsRunning := 0
			sphereStore, err := store.OpenSphere()
			if err == nil {
				agents, listErr := sphereStore.ListAgents(name, "")
				if listErr == nil {
					for _, a := range agents {
						if a.Role == "outpost" && (a.State == "working" || a.State == "stalled") {
							agentsRunning++
						}
					}
				}
				sphereStore.Close()
			}

			if worldSleepJSON {
				return printWorldJSON(name, cfg)
			}
			if servicesStopped == 0 && agentsRunning == 0 {
				fmt.Printf("World %q is now sleeping (no services were running).\n", name)
			} else if agentsRunning > 0 {
				fmt.Printf("World %q is now sleeping (%d service(s) stopped, %d agent(s) still running).\n", name, servicesStopped, agentsRunning)
			} else {
				fmt.Printf("World %q is now sleeping (%d service(s) stopped).\n", name, servicesStopped)
			}
			return nil
		}

		// --- Hard sleep: stop outpost agents, warn envoys ---

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		worldStore, err := store.OpenWorld(name)
		if err != nil {
			return fmt.Errorf("failed to open world store for %q: %w", name, err)
		}
		defer worldStore.Close()

		agents, err := sphereStore.ListAgents(name, "")
		if err != nil {
			return fmt.Errorf("failed to list agents for world %q: %w", name, err)
		}

		agentsStopped := 0
		envoysWarned := 0

		for _, agent := range agents {
			if agent.Role == "envoy" {
				// Warn envoy sessions but do not stop them.
				sessName := config.SessionName(name, agent.Name)
				if mgr.Exists(sessName) {
					warnMsg := "World is sleeping. Your session will continue but no new work will be dispatched."
					if err := mgr.NudgeSession(sessName, warnMsg); err != nil {
						fmt.Fprintf(os.Stderr, "  warning: failed to warn envoy %s: %v\n", agent.Name, err)
					} else {
						fmt.Printf("  warned envoy %s\n", agent.Name)
						envoysWarned++
					}
				}
				continue
			}

			if agent.Role != "outpost" {
				continue // skip non-outpost, non-envoy roles (forge already stopped)
			}

			if agent.State != "working" && agent.State != "stalled" {
				continue // skip idle agents
			}

			sessName := config.SessionName(name, agent.Name)

			if mgr.Exists(sessName) {
				// Graceful stop: nudge with a save-your-work prompt, wait for stability, then kill.
				_ = mgr.NudgeSession(sessName, "World is going to sleep. Please save your progress immediately by committing your work, then run: sol escalate \"world sleeping\"")

				// Wait up to 30 seconds for the agent to stabilize (reach idle prompt).
				// This is best-effort — if the agent doesn't stabilize, we kill it anyway.
				_ = mgr.WaitForIdle(sessName, forceStopStabilityTimeout)

				if err := mgr.Stop(sessName, true); err != nil && !errors.Is(err, session.ErrNotFound) {
					fmt.Fprintf(os.Stderr, "  warning: failed to stop agent %s session: %v\n", agent.Name, err)
				}
			}

			// Return writ to "open" status, clear assignee.
			if agent.ActiveWrit != "" {
				if err := worldStore.UpdateWrit(agent.ActiveWrit, store.WritUpdates{
					Status:   "open",
					Assignee: "-", // "-" clears assignee
				}); err != nil {
					fmt.Fprintf(os.Stderr, "  warning: failed to return writ %s to open: %v\n", agent.ActiveWrit, err)
				}

				// Auto-resolve linked escalations (best-effort) — writ is abandoned.
				if escs, escErr := sphereStore.ListEscalationsBySourceRef("writ:" + agent.ActiveWrit); escErr == nil {
					for _, esc := range escs {
						_ = sphereStore.ResolveEscalation(esc.ID)
					}
				}
			}

			// Set agent to idle, clear active_writ.
			if err := sphereStore.UpdateAgentState(agent.ID, store.AgentIdle, ""); err != nil {
				fmt.Fprintf(os.Stderr, "  warning: failed to set agent %s to idle: %v\n", agent.Name, err)
			}

			// Clear tether.
			if err := tether.Clear(name, agent.Name, agent.Role); err != nil {
				fmt.Fprintf(os.Stderr, "  warning: failed to clear tether for %s: %v\n", agent.Name, err)
			}

			fmt.Printf("  stopped agent %s\n", agent.Name)
			agentsStopped++
		}

		// Report.
		parts := []string{}
		if servicesStopped > 0 {
			parts = append(parts, fmt.Sprintf("%d service(s) stopped", servicesStopped))
		}
		if agentsStopped > 0 {
			parts = append(parts, fmt.Sprintf("%d agent(s) stopped", agentsStopped))
		}
		if envoysWarned > 0 {
			parts = append(parts, fmt.Sprintf("%d envoy(s) warned", envoysWarned))
		}
		if worldSleepJSON {
			return printWorldJSON(name, cfg)
		}
		if len(parts) == 0 {
			fmt.Printf("World %q is now sleeping.\n", name)
		} else {
			fmt.Printf("World %q is now sleeping (%s).\n", name, strings.Join(parts, ", "))
		}
		return nil
	},
}

var worldWakeCmd = &cobra.Command{
	Use:   "wake <name>",
	Short: "Mark a world as active and start its services",
	Long: `Clear the sleeping flag in world.toml and restart world services
(sentinel, forge). This reverses sol world sleep — dispatch gates are
deactivated and new work can be cast again.

Does not restart outpost agent sessions that were stopped by sleep --force;
those must be re-dispatched manually.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		if err := config.RequireWorld(name); err != nil {
			return err
		}

		cfg, err := config.LoadWorldConfig(name)
		if err != nil {
			return fmt.Errorf("failed to load world config: %w", err)
		}

		if !cfg.World.Sleeping {
			if worldWakeJSON {
				return printWorldJSON(name, cfg)
			}
			fmt.Printf("World %q is already active.\n", name)
			return nil
		}

		// Mark active in config.
		// Use PatchWorldConfig to avoid freezing sol.toml values into world.toml.
		cfg.World.Sleeping = false
		if err := config.PatchWorldConfig(name, func(raw map[string]interface{}) {
			if worldSec, ok := raw["world"].(map[string]interface{}); ok {
				delete(worldSec, "sleeping")
			}
		}); err != nil {
			return fmt.Errorf("failed to write world config: %w", err)
		}

		// Start services via subcommands.
		solBin, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: cannot find sol binary to start services: %v\n", err)
			fmt.Printf("World %q is now active.\n", name)
			return nil
		}

		mgr := session.New()

		type serviceResult struct {
			name    string
			started bool
			err     string
		}
		var results []serviceResult

		// Start sentinel (daemon process, not a tmux session — check via PID file).
		if pid := sentinel.ReadPID(name); pid > 0 && sentinel.IsRunning(pid) {
			results = append(results, serviceResult{name: "sentinel", started: true})
		} else {
			out, err := exec.Command(solBin, "sentinel", "start", "--world="+name).CombinedOutput()
			if err != nil {
				results = append(results, serviceResult{name: "sentinel", started: false, err: strings.TrimSpace(string(out))})
			} else {
				// Verify sentinel is running via PID file after start.
				pid := sentinel.ReadPID(name)
				if pid > 0 && sentinel.IsRunning(pid) {
					results = append(results, serviceResult{name: "sentinel", started: true})
				} else {
					results = append(results, serviceResult{name: "sentinel", started: false, err: "process not found after start"})
				}
			}
		}

		// Start forge.
		forgeSess := config.SessionName(name, "forge")
		if !mgr.Exists(forgeSess) {
			out, err := exec.Command(solBin, "forge", "start", "--world="+name).CombinedOutput()
			if err != nil {
				results = append(results, serviceResult{name: "forge", started: false, err: strings.TrimSpace(string(out))})
			} else {
				// Verify session exists after start.
				if mgr.Exists(forgeSess) {
					results = append(results, serviceResult{name: "forge", started: true})
				} else {
					results = append(results, serviceResult{name: "forge", started: false, err: "session not found after start"})
				}
			}
		} else {
			results = append(results, serviceResult{name: "forge", started: true})
		}

		if worldWakeJSON {
			return printWorldJSON(name, cfg)
		}

		// Report.
		fmt.Printf("World %q is now active.\n", name)
		for _, r := range results {
			if r.started {
				fmt.Printf("  ✓ %-10s started\n", r.name)
			} else {
				fmt.Printf("  ✗ %-10s failed: %s\n", r.name, r.err)
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
			return fmt.Errorf("failed to import world: %w", err)
		}

		if worldImportJSON {
			importedCfg, cfgErr := config.LoadWorldConfig(result.World)
			if cfgErr != nil {
				return fmt.Errorf("failed to load imported world config: %w", cfgErr)
			}
			return printWorldJSON(result.World, importedCfg)
		}

		fmt.Printf("World %q imported.\n", result.World)
		fmt.Printf("  Config:   %s\n", config.WorldConfigPath(result.World))
		fmt.Printf("  Database: %s\n", filepath.Join(config.StoreDir(), result.World+".db"))
		fmt.Println()

		if result.SourceRepo != "" {
			fmt.Println("Next steps:")
			fmt.Printf("  sol world sync %s   # clone the managed repo\n", result.World)
		}

		return nil
	},
}

// printWorldJSON builds a cliapi World from the config and sphere store, then prints it as JSON.
func printWorldJSON(name string, cfg config.WorldConfig) error {
	sphereStore, err := store.OpenSphere()
	if err != nil {
		return fmt.Errorf("failed to open sphere store: %w", err)
	}
	defer sphereStore.Close()

	w, err := sphereStore.GetWorld(name)
	if err != nil {
		return fmt.Errorf("failed to get world record: %w", err)
	}
	state := "active"
	if cfg.World.Sleeping {
		state = "sleeping"
	}
	out := cliworlds.FromStoreWorld(*w, cliworlds.WorldInfo{
		Branch:         cfg.World.Branch,
		State:          state,
		Health:         "unknown",
		Sleeping:       cfg.World.Sleeping,
		DefaultAccount: cfg.World.DefaultAccount,
	})
	return printJSON(out)
}

// getRepoHead returns the short HEAD commit SHA for a repo path, or "" on error.
func getRepoHead(repoPath string) string {
	out, err := exec.Command("git", "-C", repoPath, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func init() {
	rootCmd.AddCommand(worldCmd)
	worldCmd.AddCommand(worldInitCmd)
	worldCmd.AddCommand(worldListCmd)
	worldCmd.AddCommand(worldStatusCmd)
	worldCmd.AddCommand(worldDeleteCmd)
	worldCmd.AddCommand(worldCloneCmd)
	worldCmd.AddCommand(worldSyncCmd)
	worldCmd.AddCommand(worldSleepCmd)
	worldCmd.AddCommand(worldWakeCmd)
	worldCmd.AddCommand(worldImportCmd)

	worldInitCmd.Flags().StringVar(&worldInitSourceRepo, "source-repo",
		"", "git URL or local path to source repository")
	worldInitCmd.Flags().BoolVar(&worldInitJSON, "json", false,
		"output as JSON")
	worldListCmd.Flags().BoolVar(&worldListJSON, "json", false,
		"output as JSON")
	worldStatusCmd.Flags().BoolVar(&worldStatusJSON, "json", false,
		"output as JSON")
	worldDeleteCmd.Flags().StringVar(&worldDeleteWorld, "world", "",
		"world name (deprecated: use positional argument)")
	_ = worldDeleteCmd.Flags().MarkHidden("world")
	worldDeleteCmd.Flags().BoolVar(&worldDeleteConfirm, "confirm", false,
		"confirm deletion")
	worldDeleteCmd.Flags().BoolVar(&worldDeleteJSON, "json", false,
		"output as JSON")
	worldCloneCmd.Flags().BoolVar(&worldCloneIncludeHistory, "include-history", false,
		"include agent history and token usage in clone")
	worldCloneCmd.Flags().BoolVar(&worldCloneJSON, "json", false,
		"output as JSON")
	worldSyncCmd.Flags().StringVar(&worldSyncWorld, "world", "",
		"world name (deprecated: use positional argument)")
	_ = worldSyncCmd.Flags().MarkHidden("world")
	worldSyncCmd.Flags().BoolVar(&worldSyncAll, "all", false,
		"also sync forge and envoys")
	worldSyncCmd.Flags().BoolVar(&worldSyncJSON, "json", false,
		"output as JSON")
	worldImportCmd.Flags().StringVar(&worldImportName, "name", "",
		"import under a different name (rewrites agent IDs and references)")
	worldImportCmd.Flags().BoolVar(&worldImportJSON, "json", false,
		"output as JSON")
	worldSleepCmd.Flags().BoolVar(&worldSleepForce, "force", false,
		"stop all outpost agent sessions and return their writs to the open pool")
	worldSleepCmd.Flags().BoolVar(&worldSleepJSON, "json", false,
		"output as JSON")
	worldWakeCmd.Flags().BoolVar(&worldWakeJSON, "json", false,
		"output as JSON")
}
