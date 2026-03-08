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
	tetherWorld string
	tetherAgent string
)

var tetherCmd = &cobra.Command{
	Use:          "tether <writ-id>",
	Short:        "Bind a writ to a persistent agent (envoy, governor, forge)",
	Long:         "Bind a writ to a persistent agent without creating a worktree or launching a session.\nOutpost agents must use sol cast instead.",
	GroupID:      groupAgents,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		writID := args[0]
		world, err := config.ResolveWorld(tetherWorld)
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

		logger := events.NewLogger(config.Home())

		result, err := dispatch.Tether(dispatch.TetherOpts{
			AgentName: tetherAgent,
			WritID:    writID,
			World:     world,
		}, worldStore, sphereStore, logger)
		if err != nil {
			return err
		}

		fmt.Printf("Tethered %s (%s) -> %s\n", result.AgentName, result.AgentRole, result.WritID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(tetherCmd)
	tetherCmd.Flags().StringVar(&tetherAgent, "agent", "", "agent name (required)")
	tetherCmd.Flags().StringVar(&tetherWorld, "world", "", "world name")
	tetherCmd.MarkFlagRequired("agent")
}
