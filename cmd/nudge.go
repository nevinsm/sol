package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/spf13/cobra"
)

var (
	nudgeDrainWorld string
	nudgeDrainAgent string
	nudgeListWorld  string
	nudgeListAgent  string
)

var nudgeCmd = &cobra.Command{
	Use:     "nudge",
	Short:   "Nudge queue operations",
	GroupID: groupCommunication,
}

var nudgeListCmd = &cobra.Command{
	Use:          "list",
	Short:        "View pending nudge queue messages",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		session, err := resolveNudgeSession(nudgeListWorld, nudgeListAgent)
		if err != nil {
			return err
		}

		asJSON, _ := cmd.Flags().GetBool("json")

		msgs, err := nudge.List(session)
		if err != nil {
			return err
		}

		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(msgs)
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
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		session, err := resolveNudgeSession(nudgeListWorld, nudgeListAgent)
		if err != nil {
			return err
		}

		count, err := nudge.Peek(session)
		if err != nil {
			return err
		}

		fmt.Println(count)
		return nil
	},
}

var nudgeDrainCmd = &cobra.Command{
	Use:          "drain",
	Short:        "Drain pending nudge messages for an agent session",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(nudgeDrainWorld)
		if err != nil {
			return err
		}
		agent, err := config.ResolveAgent(nudgeDrainAgent)
		if err != nil {
			return err
		}
		session := config.SessionName(world, agent)

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
			return nil
		}

		// Format and print messages as structured block.
		for _, msg := range messages {
			fmt.Printf("[NOTIFICATION] %s: %s", msg.Type, msg.Subject)
			if msg.Body != "" {
				fmt.Printf(" — %s", msg.Body)
			}
			fmt.Println()
		}

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

	nudgeCmd.PersistentFlags().StringVar(&nudgeListWorld, "world", "", "world name")
	nudgeCmd.PersistentFlags().StringVar(&nudgeListAgent, "agent", "", "agent name (defaults to SOL_AGENT env)")

	nudgeCmd.AddCommand(nudgeListCmd)
	nudgeListCmd.Flags().Bool("json", false, "output as JSON")

	nudgeCmd.AddCommand(nudgeCountCmd)

	nudgeCmd.AddCommand(nudgeDrainCmd)
	nudgeDrainCmd.Flags().StringVar(&nudgeDrainWorld, "world", "", "world name")
	nudgeDrainCmd.Flags().StringVar(&nudgeDrainAgent, "agent", "", "agent name (defaults to SOL_AGENT env)")
	nudgeDrainCmd.Flags().Bool("json", false, "output as JSON")
}
