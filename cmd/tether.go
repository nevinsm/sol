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
	Use:          "tether <agent-name> <work-item-id>",
	Short:        "Bind a work item to an agent (any role)",
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		agentName := args[0]
		workItemID := args[1]
		world := tetherWorld

		if world == "" {
			return fmt.Errorf("--world is required")
		}
		if err := config.RequireWorld(world); err != nil {
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
			WorkItemID: workItemID,
			World:      world,
		}, worldStore, sphereStore, logger)
		if err != nil {
			return err
		}

		fmt.Printf("Tethered %s (%s) -> %s\n", result.AgentName, result.AgentRole, result.WorkItemID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(tetherCmd)
	tetherCmd.Flags().StringVar(&tetherWorld, "world", "", "world name")
}
