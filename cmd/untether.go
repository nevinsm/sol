package cmd

import (
	"fmt"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var untetherWorld string

var untetherCmd = &cobra.Command{
	Use:          "untether <agent-name>",
	Short:        "Unbind a work item from an agent (any role)",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		agentName := args[0]
		world, err := config.ResolveWorld(untetherWorld)
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

		result, err := dispatch.Untether(dispatch.UntetherOpts{
			AgentName: agentName,
			World:     world,
		}, worldStore, sphereStore, logger)
		if err != nil {
			return err
		}

		fmt.Printf("Untethered %s (%s) from %s\n", result.AgentName, result.AgentRole, result.WorkItemID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(untetherCmd)
	untetherCmd.Flags().StringVar(&untetherWorld, "world", "", "world name")
}
