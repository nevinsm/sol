package cmd

import (
	"fmt"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	activateWorld string
	activateAgent string
)

var writActivateCmd = &cobra.Command{
	Use:          "activate <writ-id>",
	Short:        "Switch active writ for a persistent agent",
	Long:         "Switch the active writ with lightweight session handoff. The writ must be tethered to the agent. If the writ is already active, this is a no-op.",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		writID := args[0]
		if err := config.ValidateWritID(writID); err != nil {
			return err
		}

		world, err := config.ResolveWorld(activateWorld)
		if err != nil {
			return err
		}
		agent, err := config.ResolveAgent(activateAgent)
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

		result, err := dispatch.ActivateWrit(dispatch.ActivateOpts{
			World:     world,
			AgentName: agent,
			WritID:    writID,
		}, worldStore, sphereStore, mgr, logger)
		if err != nil {
			return err
		}

		if result.AlreadyActive {
			fmt.Printf("Writ %s is already active for %s — no-op.\n", result.WritID, agent)
		} else {
			fmt.Printf("Activated %s for %s", result.WritID, agent)
			if result.PreviousWrit != "" {
				fmt.Printf(" (was %s)", result.PreviousWrit)
			}
			fmt.Println()
		}

		return nil
	},
}

func init() {
	writActivateCmd.Flags().StringVar(&activateWorld, "world", "", "world name")
	writActivateCmd.Flags().StringVar(&activateAgent, "agent", "", "agent name (defaults to SOL_AGENT env)")
}
