package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/nevinsm/sol/internal/cliformat"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/envoy"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/worldsync"
	"github.com/spf13/cobra"
)

var envoyCmd = &cobra.Command{
	Use:     "envoy",
	Short:   "Manage persistent envoy agents",
	GroupID: groupAgents,
}

// --- sol envoy create ---

var (
	envoyCreateWorld   string
	envoyCreatePersona string
)

var envoyCreateCmd = &cobra.Command{
	Use:          "create <name>",
	Short:        "Create an envoy agent",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		world, err := config.ResolveWorld(envoyCreateWorld)
		if err != nil {
			return err
		}

		worldCfg, err := config.LoadWorldConfig(world)
		if err != nil {
			return fmt.Errorf("failed to load world config: %w", err)
		}
		sourceRepo, err := dispatch.ResolveSourceRepo(world, worldCfg)
		if err != nil {
			return fmt.Errorf("failed to resolve source repo: %w", err)
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		if err := envoy.Create(envoy.CreateOpts{
			World:      world,
			Name:       name,
			SourceRepo: sourceRepo,
			Persona:    envoyCreatePersona,
		}, sphereStore); err != nil {
			return fmt.Errorf("failed to create envoy: %w", err)
		}

		fmt.Printf("Created envoy %q in world %q\n", name, world)
		return nil
	},
}

// --- sol envoy start ---

var envoyStartWorld string

var envoyStartCmd = &cobra.Command{
	Use:          "start <name>",
	Short:        "Start an envoy session",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		world, err := config.ResolveWorld(envoyStartWorld)
		if err != nil {
			return err
		}

		sessName, err := startup.Launch(envoy.RoleConfig(), world, name, startup.LaunchOpts{})
		if err != nil {
			return fmt.Errorf("failed to start envoy: %w", err)
		}

		fmt.Printf("Started envoy %q in world %q\n", name, world)
		fmt.Printf("  Session: %s\n", sessName)
		fmt.Printf("  Attach:  sol envoy attach %s --world=%s\n", name, world)
		return nil
	},
}

// --- sol envoy stop ---

var envoyStopWorld string

var envoyStopCmd = &cobra.Command{
	Use:          "stop <name>",
	Short:        "Stop an envoy session",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		world, err := config.ResolveWorld(envoyStopWorld)
		if err != nil {
			return err
		}

		// Hold the agent lock to prevent a concurrent Prefect respawn from racing
		// with the Exists→stop sequence inside envoy.Stop (TOCTOU guard).
		agentID := world + "/" + name
		agentLock, err := dispatch.AcquireAgentLock(agentID)
		if err != nil {
			return fmt.Errorf("failed to stop envoy: %w", err)
		}
		defer agentLock.Release()

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		mgr := session.New()

		if err := envoy.Stop(world, name, sphereStore, mgr); err != nil {
			return err
		}

		fmt.Printf("Stopped envoy %q in world %q\n", name, world)
		return nil
	},
}

// --- sol envoy restart ---

var envoyRestartWorld string

var envoyRestartCmd = &cobra.Command{
	Use:          "restart <name>",
	Short:        "Restart an envoy session (stop then start)",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		world, err := config.ResolveWorld(envoyRestartWorld)
		if err != nil {
			return err
		}

		// Hold the agent lock for the stop phase to prevent a concurrent Prefect
		// respawn from racing with the Exists→stop sequence (TOCTOU guard).
		agentID := world + "/" + name
		agentLock, err := dispatch.AcquireAgentLock(agentID)
		if err != nil {
			return fmt.Errorf("failed to restart envoy: %w", err)
		}
		// Hold the lock through the entire stop→start cycle so prefect
		// cannot respawn between stop and start.
		defer agentLock.Release()

		sessName := config.SessionName(world, name)
		mgr := session.New()

		envoyStartWorld = world
		return restartSession(mgr, sessName, "envoy",
			fmt.Sprintf("Stopped envoy %q in world %q", name, world),
			func() error {
				sphereStore, err := store.OpenSphere()
				if err != nil {
					return err
				}
				defer sphereStore.Close()
				return envoy.Stop(world, name, sphereStore, mgr)
			}, nil, envoyStartCmd, args)
	},
}

// --- sol envoy attach ---

var envoyAttachWorld string

var envoyAttachCmd = &cobra.Command{
	Use:          "attach <name>",
	Short:        "Attach to an envoy's tmux session",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		world, err := config.ResolveWorld(envoyAttachWorld)
		if err != nil {
			return err
		}

		sessName := config.SessionName(world, name)
		mgr := session.New()

		if !mgr.Exists(sessName) {
			return fmt.Errorf("no envoy session for %q in world %q (run 'sol envoy start %s --world=%s' first)",
				name, world, name, world)
		}

		return mgr.Attach(sessName)
	},
}

// --- sol envoy list ---

var (
	envoyListWorld string
	envoyListAll   bool
	envoyListJSON  bool
)

var envoyListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List envoy agents",
	Long: `List envoy agents.

By default, lists envoys scoped to the current world — determined from
--world, $SOL_WORLD, or the current working directory (if inside a world
worktree). If no world can be detected, falls back to listing envoys across
all worlds. Pass --all to explicitly list across every world regardless of
the current directory.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Determine which world to scope the listing to.
		//
		//   --all            -> list across every world (empty string).
		//   explicit --world -> ResolveWorld validates and returns it.
		//   env/cwd detect   -> ResolveWorld returns detected world on success.
		//   no detection     -> fall back to listing across every world.
		world := ""
		if !envoyListAll {
			resolved, err := config.ResolveWorld(envoyListWorld)
			if err == nil {
				world = resolved
			}
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		envoys, err := envoy.List(world, sphereStore)
		if err != nil {
			return fmt.Errorf("failed to list envoys: %w", err)
		}

		if envoyListJSON {
			return printJSON(envoys)
		}

		if len(envoys) == 0 {
			fmt.Println("No envoys found.")
			return nil
		}

		mgr := session.New()
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "NAME\tWORLD\tSTATE\tSESSION\n")
		for _, e := range envoys {
			sessName := config.SessionName(e.World, e.Name)
			sessStatus := "stopped"
			if mgr.Exists(sessName) {
				sessStatus = "running"
			}
			state := string(e.State)
			if state == "" {
				state = cliformat.EmptyMarker
			}
			name := e.Name
			if name == "" {
				name = cliformat.EmptyMarker
			}
			worldCell := e.World
			if worldCell == "" {
				worldCell = cliformat.EmptyMarker
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", name, worldCell, state, sessStatus)
		}
		tw.Flush()
		fmt.Fprintln(os.Stdout, cliformat.FormatCount(len(envoys), "envoy", "envoys"))
		return nil
	},
}

// --- sol envoy delete ---

var (
	envoyDeleteWorld   string
	envoyDeleteForce   bool
	envoyDeleteConfirm bool
)

var envoyDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete an envoy agent and all associated resources",
	Long: `Remove an envoy agent, its worktree, memory history, and agent record.

Requires --confirm to proceed; without it, prints what would be deleted and exits.

Refuses to delete if the envoy's session is active or tethered unless --force
is specified. With --force, stops the session and clears the tether before
deleting. Both flags may be needed together: sol envoy delete --confirm --force.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if err := config.RequireWorld(envoyDeleteWorld); err != nil {
			return err
		}

		if !envoyDeleteConfirm {
			fmt.Printf("This will permanently delete envoy %q from world %q:\n", name, envoyDeleteWorld)
			fmt.Printf("  - Worktree: %s\n", envoy.WorktreePath(envoyDeleteWorld, name))
			fmt.Printf("  - Envoy directory (memory, persona): %s\n", envoy.EnvoyDir(envoyDeleteWorld, name))
			fmt.Printf("  - Agent record: %s/%s\n", envoyDeleteWorld, name)
			fmt.Println()
			fmt.Println("Run with --confirm to proceed.")
			return &exitError{code: 1}
		}

		worldCfg, err := config.LoadWorldConfig(envoyDeleteWorld)
		if err != nil {
			return fmt.Errorf("failed to load world config: %w", err)
		}
		sourceRepo, err := dispatch.ResolveSourceRepo(envoyDeleteWorld, worldCfg)
		if err != nil {
			return fmt.Errorf("failed to resolve source repo: %w", err)
		}

		// Hold the agent lock to prevent a concurrent Prefect respawn from racing
		// with the Exists→stop sequence inside envoy.Delete (TOCTOU guard).
		agentID := envoyDeleteWorld + "/" + name
		agentLock, err := dispatch.AcquireAgentLock(agentID)
		if err != nil {
			return fmt.Errorf("failed to delete envoy: %w", err)
		}
		defer agentLock.Release()

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		mgr := session.New()

		// Open world store for writ reopening on force-delete.
		var worldStore envoy.WritReopener
		if envoyDeleteForce {
			ws, err := store.OpenWorld(envoyDeleteWorld)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not open world store (tethered writs may be orphaned): %v\n", err)
			} else {
				defer ws.Close()
				worldStore = ws
			}
		}

		if err := envoy.Delete(envoy.DeleteOpts{
			World:      envoyDeleteWorld,
			Name:       name,
			SourceRepo: sourceRepo,
			Force:      envoyDeleteForce,
			WorldStore: worldStore,
		}, sphereStore, mgr); err != nil {
			return fmt.Errorf("failed to delete envoy: %w", err)
		}

		fmt.Printf("Deleted envoy %q from world %q\n", name, envoyDeleteWorld)
		return nil
	},
}

// --- sol envoy sync ---

var envoySyncWorld string

var envoySyncCmd = &cobra.Command{
	Use:          "sync <name>",
	Short:        "Sync managed repo and notify a running envoy session",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		world, err := config.ResolveWorld(envoySyncWorld)
		if err != nil {
			return err
		}

		// Sync managed repo first.
		outcome, err := worldsync.SyncRepo(world)
		if err != nil {
			return fmt.Errorf("failed to sync managed repo: %w", err)
		}

		// Notify envoy session if running.
		mgr := session.New()
		if err := worldsync.SyncEnvoy(world, name, mgr, outcome); err != nil {
			return fmt.Errorf("failed to sync envoy: %w", err)
		}

		fmt.Printf("Synced for envoy %q in world %q\n", name, world)
		return nil
	},
}

// --- sol envoy status ---

var envoyStatusWorld string

type envoyStatusSummary struct {
	World       string `json:"world"`
	Name        string `json:"name"`
	Running     bool   `json:"running"`
	SessionName string `json:"session_name"`
	State       string `json:"state,omitempty"`
	ActiveWrit  string `json:"active_writ,omitempty"`
}

var envoyStatusCmd = &cobra.Command{
	Use:          "status <name>",
	Short:        "Show envoy status",
	Long: `Show envoy session and agent state.

Prints session status, agent state, and active writ.
Use --json for machine-readable output.

Exit codes:
  0 - Envoy session is running
  1 - Envoy session is not running`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		world, err := config.ResolveWorld(envoyStatusWorld)
		if err != nil {
			return err
		}

		sessName := config.SessionName(world, name)
		mgr := session.New()
		running := mgr.Exists(sessName)

		summary := envoyStatusSummary{
			World:       world,
			Name:        name,
			Running:     running,
			SessionName: sessName,
		}

		// Query sphere store for agent state.
		sphereStore, err := store.OpenSphere()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: unable to open sphere store: %v\n", err)
		} else {
			defer sphereStore.Close()
			agentID := world + "/" + name
			agent, err := sphereStore.GetAgent(agentID)
			if err == nil && agent != nil {
				summary.State = string(agent.State)
				summary.ActiveWrit = agent.ActiveWrit
			}
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			if err := printJSON(summary); err != nil {
				return err
			}
		} else {
			printEnvoyStatus(summary)
		}
		if !running {
			return &exitError{code: 1}
		}
		return nil
	},
}

func printEnvoyStatus(s envoyStatusSummary) {
	fmt.Printf("Envoy: %s (%s)\n\n", s.Name, s.World)

	if s.Running {
		fmt.Printf("  Process:  running (%s)\n", s.SessionName)
	} else {
		fmt.Printf("  Process:  stopped\n")
	}

	if s.State != "" {
		fmt.Printf("  State:    %s\n", s.State)
	}

	if s.ActiveWrit != "" {
		fmt.Printf("  Active:   %s\n", s.ActiveWrit)
	}
}

func init() {
	// Register envoy role config for startup.Launch and prefect respawn.
	startup.Register("envoy", envoy.RoleConfig())

	rootCmd.AddCommand(envoyCmd)
	envoyCmd.AddCommand(envoyCreateCmd, envoyStartCmd, envoyStopCmd, envoyRestartCmd,
		envoyAttachCmd, envoyListCmd, envoySyncCmd, envoyDeleteCmd,
		envoyStatusCmd)

	// envoy create flags
	envoyCreateCmd.Flags().StringVar(&envoyCreateWorld, "world", "", "world name")
	envoyCreateCmd.Flags().StringVar(&envoyCreatePersona, "persona", "", "persona template name (e.g. planner, engineer)")

	// envoy start flags
	envoyStartCmd.Flags().StringVar(&envoyStartWorld, "world", "", "world name")

	// envoy stop flags
	envoyStopCmd.Flags().StringVar(&envoyStopWorld, "world", "", "world name")

	// envoy restart flags
	envoyRestartCmd.Flags().StringVar(&envoyRestartWorld, "world", "", "world name")

	// envoy attach flags
	envoyAttachCmd.Flags().StringVar(&envoyAttachWorld, "world", "", "world name")

	// envoy list flags
	envoyListCmd.Flags().StringVar(&envoyListWorld, "world", "",
		"world name (defaults to $SOL_WORLD or detected from current worktree; pass --all to list across all worlds)")
	envoyListCmd.Flags().BoolVar(&envoyListAll, "all", false, "list envoys across all worlds (override directory-detected default)")
	envoyListCmd.Flags().BoolVar(&envoyListJSON, "json", false, "output as JSON")

	// envoy delete flags
	envoyDeleteCmd.Flags().StringVar(&envoyDeleteWorld, "world", "", "world name")
	_ = envoyDeleteCmd.MarkFlagRequired("world")
	envoyDeleteCmd.Flags().BoolVar(&envoyDeleteConfirm, "confirm", false, "confirm destructive action")
	envoyDeleteCmd.Flags().BoolVar(&envoyDeleteForce, "force", false, "force delete even if session is active or tethered")

	// envoy sync flags
	envoySyncCmd.Flags().StringVar(&envoySyncWorld, "world", "", "world name")

	// envoy status flags
	envoyStatusCmd.Flags().StringVar(&envoyStatusWorld, "world", "", "world name")
	envoyStatusCmd.Flags().Bool("json", false, "output as JSON")
}
