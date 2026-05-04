package cmd

import (
	"fmt"
	"time"

	"github.com/nevinsm/sol/internal/cliapi/agents"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	activateWorld string
	activateAgent string
	activateJSON  bool
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
		// L-M4: when only the session restart failed, ActivateWrit returns
		// both a non-nil result (DB and resume_state.json are persisted) and
		// a non-nil err. Surface the failure to the operator with exit 1 so
		// scripted callers see the degraded outcome instead of a silent
		// "success" while the agent's session is gone.
		if err != nil && result != nil && result.SessionRestartErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"sol writ activate: active_writ updated to %s but session restart failed: %v\n",
				result.WritID, result.SessionRestartErr)
			return result.SessionRestartErr
		}
		if err != nil {
			return err
		}

		if activateJSON {
			// Re-read the agent post-activation to reflect the updated active_writ.
			agentID := world + "/" + agent
			agentRec, err := sphereStore.GetAgent(agentID)
			if err != nil {
				return fmt.Errorf("failed to read agent %q after activation: %w", agentID, err)
			}

			// Resolve model and account for the JSON response.
			var model, account string
			if cfg, err := config.LoadWorldConfig(world); err == nil {
				runtime := cfg.ResolveRuntime(agentRec.Role)
				model = cfg.ResolveModel(agentRec.Role, runtime)
			}
			account = readAgentAccountBinding(agentRec.World, agentRec.Role, agentRec.Name)

			var lastSeen *time.Time
			if !agentRec.UpdatedAt.IsZero() {
				t := agentRec.UpdatedAt.UTC()
				lastSeen = &t
			}

			return printJSON(agents.FromStoreAgent(*agentRec, model, account, lastSeen))
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
	writActivateCmd.Flags().BoolVar(&activateJSON, "json", false, "output as JSON")
}
