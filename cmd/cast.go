package cmd

import (
	"fmt"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	castWorld   string
	castAgent   string
	castWorkflow string
	castVars    []string
	castAccount string
)

var castCmd = &cobra.Command{
	Use:          "cast <writ-id>",
	Short:        "Assign a writ to an agent and start its session",
	GroupID:      groupDispatch,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		writID := args[0]

		world, err := config.ResolveWorld(castWorld)
		if err != nil {
			return err
		}

		// Config-first source repo discovery.
		worldCfg, err := config.LoadWorldConfig(world)
		if err != nil {
			return err
		}

		if worldCfg.World.Sleeping {
			return fmt.Errorf("world %q is sleeping (wake it with 'sol world wake %s')", world, world)
		}

		sourceRepo, err := dispatch.ResolveSourceRepo(world, worldCfg)
		if err != nil {
			return err
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		mgr := dispatch.NewSessionManager()
		logger := events.NewLogger(config.Home())

		// Parse --var flags into a map.
		vars, err := parseVarFlags(castVars)
		if err != nil {
			return err
		}

		result, err := dispatch.Cast(cmd.Context(), dispatch.CastOpts{
			WritID:  writID,
			World:       world,
			AgentName:   castAgent,
			SourceRepo:  sourceRepo,
			Workflow:    castWorkflow,
			Variables:   vars,
			WorldConfig: &worldCfg,
			Account:     castAccount,
		}, worldStore, sphereStore, mgr, logger)
		if err != nil {
			return err
		}

		fmt.Printf("Cast %s -> %s (%s)\n", result.WritID, result.AgentName, result.SessionName)
		fmt.Printf("  Worktree: %s\n", result.WorktreeDir)
		fmt.Printf("  Session:  %s\n", result.SessionName)
		if result.Workflow != "" {
			fmt.Printf("  Workflow: %s\n", result.Workflow)
		}
		fmt.Printf("  Attach:   sol session attach %s\n", result.SessionName)
		return nil
	},
}

func init() {
	// Register outpost role config for startup.Launch and prefect respawn.
	startup.Register("agent", dispatch.OutpostRoleConfig())

	rootCmd.AddCommand(castCmd)
	castCmd.Flags().StringVar(&castWorld, "world", "", "world name")
	castCmd.Flags().StringVar(&castAgent, "agent", "", "agent name (auto-selects idle agent if omitted)")
	castCmd.Flags().StringVar(&castWorkflow, "workflow", "", "workflow to instantiate")
	castCmd.Flags().StringSliceVar(&castVars, "var", nil, "workflow variable (key=val, repeatable)")
	castCmd.Flags().StringVar(&castAccount, "account", "", "account to use for credentials (overrides world.toml default_account)")
}
