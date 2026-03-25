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
	resolveWorld string
	resolveAgent string
)

var resolveCmd = &cobra.Command{
	Use:   "resolve",
	Short: "Signal work completion — code writs push branch and create MR; non-code writs close directly",
	Long: `Mark the current writ as done and clean up the agent's tether.

For code writs: pushes the worktree branch, creates a merge request in the
forge queue, and sets the writ to "done" (awaiting merge).

For non-code writs: closes the writ directly with no branch push.

In both cases, clears the agent's tether and returns it to idle (unless the
session is configured to stay alive for further dispatch).

Typically called from within an agent session. Uses SOL_WORLD and SOL_AGENT
environment variables when --world and --agent are not provided.`,
	GroupID:      groupDispatch,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(resolveWorld)
		if err != nil {
			return err
		}
		agent, err := config.ResolveAgent(resolveAgent)
		if err != nil {
			return err
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return fmt.Errorf("failed to open world store: %w", err)
		}
		defer worldStore.Close()

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		mgr := dispatch.NewSessionManager()
		logger := events.NewLogger(config.Home())

		result, err := dispatch.Resolve(cmd.Context(), dispatch.ResolveOpts{
			World:     world,
			AgentName: agent,
		}, worldStore, sphereStore, mgr, logger)
		if err != nil {
			return fmt.Errorf("failed to resolve writ: %w", err)
		}

		fmt.Printf("Done: %s (%s)\n", result.WritID, result.Title)
		if result.BranchName != "" {
			fmt.Printf("  Branch: %s\n", result.BranchName)
		}
		if result.MergeRequestID != "" {
			fmt.Printf("  Merge request: %s (queued)\n", result.MergeRequestID)
		}
		if result.SessionKept {
			fmt.Printf("  Agent %s resolved %q — session kept alive\n", result.AgentName, result.Title)
		} else {
			fmt.Printf("  Agent %s is now idle.\n", result.AgentName)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(resolveCmd)
	resolveCmd.Flags().StringVar(&resolveWorld, "world", "", "world name")
	resolveCmd.Flags().StringVar(&resolveAgent, "agent", "", "agent name (defaults to SOL_AGENT env)")
}
