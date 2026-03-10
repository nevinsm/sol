package cmd

import (
	"fmt"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/envoy"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/governor"
	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	handoffWorld   string
	handoffAgent   string
	handoffSummary string
	handoffReason  string
)

var handoffCmd = &cobra.Command{
	Use:   "handoff",
	Short: "Hand off to a fresh session with context preservation",
	Long: `Stop the current agent session and start a new one for the same writ.

The agent's tether, worktree, and writ assignment are preserved. Committed
code and the git history carry over as the primary context for the successor
session. Use --summary to pass additional context.

Common reasons: context exhaustion (compact), autarch-initiated (manual),
or health-check triggered restart. Uses SOL_WORLD and SOL_AGENT environment
variables when flags are not provided.`,
	GroupID:      groupDispatch,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(handoffWorld)
		if err != nil {
			return err
		}
		agent, err := config.ResolveAgent(handoffAgent)
		if err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		// Look up agent to determine role and worktree path.
		agentID := world + "/" + agent
		agentRecord, err := sphereStore.GetAgent(agentID)
		if err != nil {
			return fmt.Errorf("failed to get agent %q: %w", agentID, err)
		}

		role := agentRecord.Role
		worktreeDir := worktreeDirForRole(world, agent, role)

		mgr := session.New()
		logger := events.NewLogger(config.Home())

		reason := handoffReason
		if reason == "" {
			reason = "unknown"
		}

		if err := handoff.Exec(handoff.ExecOpts{
			World:       world,
			AgentName:   agent,
			Summary:     handoffSummary,
			Role:        role,
			WorktreeDir: worktreeDir,
			Reason:      reason,
		}, mgr, sphereStore, logger); err != nil {
			return err
		}

		fmt.Println("Handoff complete. New session starting.")
		return nil
	},
}

// worktreeDirForRole returns the worktree path for an agent based on its role.
func worktreeDirForRole(world, agentName, role string) string {
	switch role {
	case "envoy":
		return envoy.WorktreePath(world, agentName)
	case "governor":
		return governor.GovernorDir(world)
	case "forge":
		return forge.WorktreePath(world)
	default:
		return config.WorktreePath(world, agentName)
	}
}

func init() {
	rootCmd.AddCommand(handoffCmd)
	handoffCmd.Flags().StringVar(&handoffWorld, "world", "", "world name")
	handoffCmd.Flags().StringVar(&handoffAgent, "agent", "", "agent name (defaults to SOL_AGENT env)")
	handoffCmd.Flags().StringVar(&handoffSummary, "summary", "", "summary of current progress")
	handoffCmd.Flags().StringVar(&handoffReason, "reason", "", "handoff reason (compact, manual, health-check)")
}
