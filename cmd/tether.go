package cmd

import (
	"fmt"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var tetherWorld string

var tetherCmd = &cobra.Command{
	Use:          "tether <agent-name> <writ-id>",
	Short:        "Bind a writ to an agent (any role)",
	GroupID:      groupAgents,
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		agentName := args[0]
		writID := args[1]
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
			AgentName:  agentName,
			WritID: writID,
			World:      world,
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
	tetherCmd.Flags().StringVar(&tetherWorld, "world", "", "world name")
}
