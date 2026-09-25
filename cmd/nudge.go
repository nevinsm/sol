package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	cliapinudge "github.com/nevinsm/sol/internal/cliapi/nudge"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/spf13/cobra"
)

var (
	nudgeWorld string
	nudgeAgent string
)

var nudgeCmd = &cobra.Command{
	Use:     "nudge",
	Short:   "Nudge queue operations",
	Hidden:  true,
	GroupID: groupCommunication,
}

var nudgeListCmd = &cobra.Command{
	Use:          "list",
	Short:        "View pending nudge queue messages",
	Hidden:       true,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		session, err := resolveNudgeSession(nudgeWorld, nudgeAgent)
		if err != nil {
			return err
		}

		asJSON, _ := cmd.Flags().GetBool("json")

		msgs, err := nudge.List(session)
		if err != nil {
			return err
		}

		if asJSON {
			return printJSON(cliapinudge.FromMessages(msgs, session))
		}

		if len(msgs) == 0 {
			fmt.Println("No pending messages.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SENDER\tTYPE\tSUBJECT\tPRIORITY\tAGE")
		for _, m := range msgs {
			age := time.Since(m.CreatedAt).Truncate(time.Second)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", m.Sender, m.Type, m.Subject, m.Priority, age)
		}
		return w.Flush()
	},
}

var nudgeCountCmd = &cobra.Command{
	Use:          "count",
	Short:        "Print count of pending nudge messages",
	Hidden:       true,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(nudgeWorld)
		if err != nil {
			return err
		}
		agent, err := config.ResolveAgent(nudgeAgent)
		if err != nil {
			return err
		}
		session := config.SessionName(world, agent)

		count, err := nudge.Peek(session)
		if err != nil {
			return err
		}

		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			return printJSON(cliapinudge.NudgeQueueSummary{
				Agent:        agent,
				World:        world,
				PendingCount: count,
			})
		}

		fmt.Println(count)
		return nil
	},
}

var nudgeDrainCmd = &cobra.Command{
	Use:   "drain",
	Short: "Drain pending nudge messages for an agent session",
	Long: `Drain pending nudge messages for an agent session.

Claims all pending messages, prints them, and clears the queue (with the
same crash-safe receipt protocol as the automatic turn-boundary drain — see
internal/nudge). Safe to run at any point mid-session, not just at a turn
boundary: an agent that sees the fixed "[sol] pending messages: run sol
nudge drain, then continue your current work" doorbell line in its pane
should run this command immediately to retrieve the actual message content,
which never rides the pane itself.

An empty queue is a silent no-op in text mode (nothing printed, exit 0);
--json always prints a JSON array, empty ([]) when there is nothing pending.

Exit codes:
  0 - success (messages drained, or queue was empty)
  1 - failed to read or clean up the nudge queue`,
	Hidden:       true,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		session, err := resolveNudgeSession(nudgeWorld, nudgeAgent)
		if err != nil {
			return err
		}

		asJSON, _ := cmd.Flags().GetBool("json")

		// Drain pending messages.
		messages, err := nudge.Drain(session)
		if err != nil {
			return fmt.Errorf("failed to drain nudge queue: %w", err)
		}

		// Run cleanup (requeue orphaned claims, delete expired).
		if err := nudge.Cleanup(session); err != nil {
			return fmt.Errorf("failed to cleanup nudge queue: %w", err)
		}

		// Silent no-op if no messages.
		if len(messages) == 0 {
			if asJSON {
				return printJSON(cliapinudge.FromMessages([]nudge.Message{}, session))
			}
			return nil
		}

		if asJSON {
			return printJSON(cliapinudge.FromMessages(messages, session))
		}

		// Render through the same formatter channelserve uses for its channel
		// push, so text-mode drain and channel delivery never drift apart
		// again (see nudge.FormatNotification's doc comment).
		for _, msg := range messages {
			fmt.Println(nudge.FormatNotification(msg))
		}

		// Batch-level framing, printed once per non-empty drain (never per
		// message): without it, messages landing mid-task read as feedback
		// on whatever the agent was just doing. See nudge.DrainFraming.
		fmt.Println(nudge.DrainFraming)

		return nil
	},
}

// resolveNudgeSession resolves the session name from world/agent flags, env vars, or cwd.
func resolveNudgeSession(worldFlag, agentFlag string) (string, error) {
	world, err := config.ResolveWorld(worldFlag)
	if err != nil {
		return "", err
	}
	agent, err := config.ResolveAgent(agentFlag)
	if err != nil {
		return "", err
	}
	return config.SessionName(world, agent), nil
}

func init() {
	rootCmd.AddCommand(nudgeCmd)

	nudgeCmd.PersistentFlags().StringVar(&nudgeWorld, "world", "", "world name")
	nudgeCmd.PersistentFlags().StringVar(&nudgeAgent, "agent", "", "agent name (defaults to SOL_AGENT env)")

	nudgeCmd.AddCommand(nudgeListCmd)
	nudgeListCmd.Flags().Bool("json", false, "output as JSON")

	nudgeCmd.AddCommand(nudgeCountCmd)
	nudgeCountCmd.Flags().Bool("json", false, "output as JSON")

	nudgeCmd.AddCommand(nudgeDrainCmd)
	nudgeDrainCmd.Flags().Bool("json", false, "output as JSON")
}
