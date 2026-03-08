package cmd

import (
	"fmt"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	primeWorld string
	primeAgent string
)

var primeCmd = &cobra.Command{
	Use:          "prime",
	Short:        "Assemble and print execution context for an agent",
	GroupID:      groupPlumbing,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(primeWorld)
		if err != nil {
			return err
		}
		agent, err := config.ResolveAgent(primeAgent)
		if err != nil {
			return err
		}

		// Look up agent to determine role.
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		agentID := world + "/" + agent
		agentRecord, err := sphereStore.GetAgent(agentID)
		if err != nil {
			return fmt.Errorf("failed to get agent %q: %w", agentID, err)
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		result, err := dispatch.Prime(world, agent, agentRecord.Role, worldStore)
		if err != nil {
			return err
		}

		fmt.Println(result.Output)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(primeCmd)
	primeCmd.Flags().StringVar(&primeWorld, "world", "", "world name")
	primeCmd.Flags().StringVar(&primeAgent, "agent", "", "agent name")
}
