package cmd

import (
	"fmt"

	"github.com/nevinsm/sol/internal/cliapi/agents"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	untetherWorld string
	untetherAgent string
	untetherJSON  bool
)

var untetherCmd = &cobra.Command{
	Use:     "untether <writ-id>",
	Short:   "Unbind a writ from a persistent agent",
	GroupID: groupAgents,
	Long: `Unbind a specific writ from an agent without stopping the session.
If no tethers remain, the agent goes idle.

Typically called from within an agent session. Uses SOL_AGENT environment
variable when --agent is not provided.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		writID := args[0]
		world, err := config.ResolveWorld(untetherWorld)
		if err != nil {
			return err
		}
		agent, err := config.ResolveAgent(untetherAgent)
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
			AgentName: agent,
			WritID:    writID,
			World:     world,
		}, worldStore, sphereStore, logger)
		if err != nil {
			return err
		}

		if untetherJSON {
			agentID := world + "/" + result.AgentName
			agent, err := sphereStore.GetAgent(agentID)
			if err != nil {
				return fmt.Errorf("failed to get agent %q: %w", agentID, err)
			}
			return printJSON(agents.FromStoreAgent(*agent, "", "", nil))
		}

		fmt.Printf("Untethered %s (%s) from %s\n", result.AgentName, result.AgentRole, result.WritID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(untetherCmd)
	untetherCmd.Flags().StringVar(&untetherAgent, "agent", "", "agent name (defaults to SOL_AGENT env)")
	untetherCmd.Flags().StringVar(&untetherWorld, "world", "", "world name")
	untetherCmd.Flags().BoolVar(&untetherJSON, "json", false, "output as JSON")
}
