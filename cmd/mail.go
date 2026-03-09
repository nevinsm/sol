package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var mailCmd = &cobra.Command{
	Use:     "mail",
	Short:   "Inter-agent messaging",
	GroupID: groupCommunication,
}

var mailSendCmd = &cobra.Command{
	Use:          "send",
	Short:        "Send a message",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		to, _ := cmd.Flags().GetString("to")
		subject, _ := cmd.Flags().GetString("subject")
		body, _ := cmd.Flags().GetString("body")
		priority, _ := cmd.Flags().GetInt("priority")
		noNotify, _ := cmd.Flags().GetBool("no-notify")
		worldFlag, _ := cmd.Flags().GetString("world")
		if priority < 1 || priority > 3 {
			return fmt.Errorf("priority must be 1 (urgent), 2 (normal), or 3 (low)")
		}

		s, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer s.Close()

		id, err := s.SendMessage("operator", to, subject, body, priority, "notification")
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Sent: %s → %s\n", id, to)

		// Bridge to nudge queue for agent delivery
		if !noNotify && to != "operator" {
			bridgeMailToNudge(to, worldFlag, subject, body, priority)
		}

		return nil
	},
}

var mailInboxCmd = &cobra.Command{
	Use:          "inbox",
	Short:        "List pending messages",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		identity, _ := cmd.Flags().GetString("identity")
		asJSON, _ := cmd.Flags().GetBool("json")

		s, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer s.Close()

		msgs, err := s.Inbox(identity)
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
		fmt.Fprintln(w, "ID\tFROM\tPRIORITY\tSUBJECT\tAGE")
		for _, m := range msgs {
			age := time.Since(m.CreatedAt).Truncate(time.Second)
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", m.ID, m.Sender, m.Priority, m.Subject, age)
		}
		return w.Flush()
	},
}

var mailReadCmd = &cobra.Command{
	Use:          "read <message-id>",
	Short:        "Read a message (marks as read)",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer s.Close()

		msg, err := s.ReadMessage(args[0])
		if err != nil {
			return err
		}

		fmt.Printf("From:    %s\n", msg.Sender)
		fmt.Printf("To:      %s\n", msg.Recipient)
		fmt.Printf("Subject: %s\n", msg.Subject)
		fmt.Printf("Date:    %s\n", msg.CreatedAt.Format(time.RFC3339))
		if msg.Body != "" {
			fmt.Printf("\n%s\n", msg.Body)
		}
		return nil
	},
}

var mailAckCmd = &cobra.Command{
	Use:          "ack <message-id>",
	Short:        "Acknowledge a message",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer s.Close()

		if err := s.AckMessage(args[0]); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Acknowledged: %s\n", args[0])
		return nil
	},
}

var mailCheckCmd = &cobra.Command{
	Use:          "check",
	Short:        "Count unread messages",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		identity, _ := cmd.Flags().GetString("identity")

		s, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer s.Close()

		count, err := s.CountPending(identity)
		if err != nil {
			return err
		}

		if count == 0 {
			fmt.Println("No unread messages.")
			return &exitError{code: 1}
		}
		fmt.Printf("%d unread messages\n", count)
		return nil
	},
}

var mailPurgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Delete acknowledged messages",
	Long: `Delete acknowledged messages from the sphere mailbox.

Requires --confirm to proceed; without it, previews what would be deleted and exits.
The --force flag is accepted as an alias for --confirm for backward compatibility.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		allAcked, _ := cmd.Flags().GetBool("all-acked")
		before, _ := cmd.Flags().GetString("before")
		confirm, _ := cmd.Flags().GetBool("confirm")
		force, _ := cmd.Flags().GetBool("force")

		// --force is an alias for --confirm on this command (backward compat).
		if force {
			confirm = true
		}

		if !allAcked && before == "" {
			return fmt.Errorf("must specify --before=<duration> or --all-acked")
		}

		s, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer s.Close()

		var count int64
		if allAcked {
			if !confirm {
				n, err := s.CountAcked()
				if err != nil {
					return err
				}
				fmt.Printf("Would delete %d acknowledged message(s).\n", n)
				fmt.Println("Run with --confirm to proceed.")
				return &exitError{code: 1}
			}
			count, err = s.PurgeAllAcked()
		} else {
			dur, parseErr := parseHumanDuration(before)
			if parseErr != nil {
				return fmt.Errorf("invalid --before duration %q: %w", before, parseErr)
			}
			cutoff := time.Now().UTC().Add(-dur)

			if !confirm {
				n, err := s.CountAckedBefore(cutoff)
				if err != nil {
					return err
				}
				fmt.Printf("Would delete %d acknowledged message(s) older than %s.\n", n, before)
				fmt.Println("Run with --confirm to proceed.")
				return &exitError{code: 1}
			}
			count, err = s.PurgeAckedMessages(cutoff)
		}
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "Purged %d message(s).\n", count)
		return nil
	},
}

// parseHumanDuration parses a duration string with support for "d" (days)
// in addition to the standard Go duration units.
// Examples: "7d", "24h", "30m", "7d12h".
func parseHumanDuration(s string) (time.Duration, error) {
	// Try standard Go duration first.
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Handle "d" suffix by converting days to hours.
	if strings.Contains(s, "d") {
		parts := strings.SplitN(s, "d", 2)
		days, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, fmt.Errorf("invalid day count in %q", s)
		}
		total := time.Duration(days) * 24 * time.Hour
		if parts[1] != "" {
			remainder, err := time.ParseDuration(parts[1])
			if err != nil {
				return 0, fmt.Errorf("invalid duration suffix in %q: %w", s, err)
			}
			total += remainder
		}
		return total, nil
	}

	return 0, fmt.Errorf("invalid duration %q", s)
}

// bridgeMailToNudge resolves the recipient to a session and delivers a nudge notification.
// Best-effort: failures are logged to stderr but do not affect mail delivery.
func bridgeMailToNudge(to, worldFlag, subject, body string, priority int) {
	var world, agent string

	if strings.Contains(to, "/") {
		// "world/agent" format
		parts := strings.SplitN(to, "/", 2)
		world, agent = parts[0], parts[1]
	} else {
		// Plain agent name — resolve world from flag/env/cwd
		agent = to
		var err error
		world, err = config.ResolveWorld(worldFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mail: skipping nudge: cannot resolve world: %v\n", err)
			return
		}
	}

	sessName := config.SessionName(world, agent)

	mgr := session.New()
	if !mgr.Exists(sessName) {
		// No active session — sphere mail is the durable record
		return
	}

	// Map mail priority to nudge priority
	nudgePriority := "normal"
	if priority == 1 {
		nudgePriority = "urgent"
	}

	// Truncate body for nudge preview (max 500 chars)
	nudgeBody := body
	if len(nudgeBody) > 500 {
		nudgeBody = nudgeBody[:497] + "..."
	}

	if err := nudge.Deliver(sessName, nudge.Message{
		Sender:   "operator",
		Type:     "MAIL",
		Subject:  subject,
		Body:     nudgeBody,
		Priority: nudgePriority,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "mail: warning: nudge delivery failed: %v\n", err)
	}
}

func init() {
	rootCmd.AddCommand(mailCmd)

	mailSendCmd.Flags().String("to", "", "Recipient agent ID or \"operator\"")
	mailSendCmd.Flags().String("subject", "", "Message subject")
	mailSendCmd.Flags().String("body", "", "Message body")
	mailSendCmd.Flags().Int("priority", 2, "Priority (1=urgent, 2=normal, 3=low)")
	mailSendCmd.Flags().Bool("no-notify", false, "Suppress nudge notification to recipient")
	mailSendCmd.Flags().String("world", "", "world name")
	mailSendCmd.MarkFlagRequired("to")
	mailSendCmd.MarkFlagRequired("subject")

	mailInboxCmd.Flags().String("identity", "operator", "Recipient to check")
	mailInboxCmd.Flags().Bool("json", false, "Output as JSON")

	mailCheckCmd.Flags().String("identity", "operator", "Recipient to check")

	mailPurgeCmd.Flags().String("before", "", "Delete acked messages older than duration (e.g., 7d, 24h)")
	mailPurgeCmd.Flags().Bool("all-acked", false, "Delete all acknowledged messages regardless of age")
	mailPurgeCmd.Flags().Bool("confirm", false, "confirm destructive action")
	mailPurgeCmd.Flags().Bool("force", false, "alias for --confirm (backward compatibility)")

	mailCmd.AddCommand(mailSendCmd)
	mailCmd.AddCommand(mailInboxCmd)
	mailCmd.AddCommand(mailReadCmd)
	mailCmd.AddCommand(mailAckCmd)
	mailCmd.AddCommand(mailCheckCmd)
	mailCmd.AddCommand(mailPurgeCmd)
}
