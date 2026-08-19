package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/sol/internal/cliapi/mail"
	"github.com/nevinsm/sol/internal/cliflag"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

// resolveMailIdentity returns the effective mail identity for the current caller.
// If flagValue is non-empty (explicitly set), it is returned as-is.
// If SOL_AGENT and SOL_WORLD are both set, returns "world/agent" canonical form.
// Otherwise returns config.Autarch (operator default).
func resolveMailIdentity(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	agent := os.Getenv("SOL_AGENT")
	world := os.Getenv("SOL_WORLD")
	if agent != "" && world != "" {
		return world + "/" + agent
	}
	return config.Autarch
}

// resolveVia returns the effective SOL_VIA origin channel for the current
// caller (ADR-0043 decision 1). If flagValue is non-empty (explicitly set
// via --via), it is returned as-is. Otherwise falls back to the SOL_VIA
// environment variable, sibling of SOL_WORLD/SOL_AGENT. Empty string means
// no origin channel is recorded — sol's own internal callers never set one.
func resolveVia(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv("SOL_VIA")
}

// validateVia checks that a non-empty via value contains only safe
// characters. Reuses the same restrictive charset as agent names
// (config.ValidateAgentName) — per ADR-0043, via is a channel label, not a
// compound identity, so "/" and any other agent-name-unsafe character are
// rejected. An empty via is valid (no origin channel recorded).
func validateVia(via string) error {
	if via == "" {
		return nil
	}
	if err := config.ValidateAgentName(via); err != nil {
		return fmt.Errorf("invalid --via %q: %w", via, err)
	}
	return nil
}

// canonicalizeRecipient ensures the recipient is in "world/agent" format for agents,
// or plain "autarch" for the operator. If to is already "world/agent" or "autarch",
// it is returned as-is. If it is a plain agent name, worldHint is used to prefix it.
func canonicalizeRecipient(to, worldHint string) string {
	if to == config.Autarch {
		return to
	}
	if strings.Contains(to, "/") {
		return to
	}
	// Plain agent name — prepend world
	if worldHint != "" {
		return worldHint + "/" + to
	}
	// No world hint available; return as-is
	return to
}

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
		bodyInline, _ := cmd.Flags().GetString("body")
		bodyFile, _ := cmd.Flags().GetString("body-file")
		priority, _ := cmd.Flags().GetInt("priority")
		noNotify, _ := cmd.Flags().GetBool("no-notify")
		worldFlag, _ := cmd.Flags().GetString("world")
		asJSON, _ := cmd.Flags().GetBool("json")
		viaFlag, _ := cmd.Flags().GetString("via")
		threadFlag, _ := cmd.Flags().GetString("thread")
		if priority < 1 || priority > 3 {
			return fmt.Errorf("priority must be 1 (urgent), 2 (normal), or 3 (low)")
		}

		via := resolveVia(viaFlag)
		if err := validateVia(via); err != nil {
			return err
		}

		body, err := cliflag.ResolveText(bodyInline, bodyFile, "body", "body-file")
		if err != nil {
			return err
		}

		// Auto-detect sender: use world/agent if env vars set, otherwise autarch.
		sender := resolveMailIdentity("")

		// Canonicalize recipient to world/agent format. Resolve world using
		// the same flag -> SOL_WORLD -> cwd-detection precedence every other
		// world-scoped command uses, via ResolveWorldHint — this is a hint
		// only (no existence validation): world here is optional context
		// used solely for recipient canonicalization below, and an
		// unresolvable or nonexistent world just leaves the recipient
		// un-prefixed, which the non-canonical-recipient check right after
		// this still catches.
		resolvedWorld := config.ResolveWorldHint(worldFlag)
		storedTo := canonicalizeRecipient(to, resolvedWorld)

		// Refuse to persist a non-canonical recipient: if the stored form is
		// not "autarch" and lacks a "world/" prefix, delivery is impossible
		// and bridgeMailToNudge cannot resolve a session. Error out before
		// any DB write so we don't leave an orphaned mail row.
		if storedTo != config.Autarch && !strings.Contains(storedTo, "/") {
			return fmt.Errorf("recipient %q has no world prefix; pass --world or set SOL_WORLD, or use \"world/agent\" form", to)
		}

		s, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer s.Close()

		id, err := s.SendMessageWithOrigin(sender, storedTo, subject, body, priority, "notification", via, threadFlag)
		if err != nil {
			return err
		}

		// Resolve the thread actually used: an empty --thread means
		// SendMessageWithOrigin self-assigned the message's own id (see
		// its doc comment) — mirror that here so output and the event
		// payload reflect the real stored value.
		threadID := threadFlag
		if threadID == "" {
			threadID = id
		}

		// Emit mail_sent to the event log (ADR-0043 decision 3) — best
		// effort, matches the DEGRADE principle used throughout events.Logger.
		logger := events.NewLogger(config.Home())
		logger.Emit(events.EventMailSent, sender, "sol", "both", map[string]string{
			"id":        id,
			"sender":    sender,
			"recipient": storedTo,
			"subject":   subject,
			"via":       via,
			"thread_id": threadID,
		})

		// Bridge to nudge queue for agent delivery
		if !noNotify && storedTo != config.Autarch {
			bridgeMailToNudge(storedTo, subject, body, priority)
		}

		if asJSON {
			now := time.Now().UTC().Truncate(time.Second)
			msg := mail.Message{
				ID:        id,
				Sender:    sender,
				Recipient: storedTo,
				Subject:   subject,
				Body:      body,
				Priority:  priority,
				CreatedAt: now,
				Via:       via,
				ThreadID:  threadID,
			}
			return printJSON(msg)
		}

		fmt.Printf("Sent: %s → %s\n", id, storedTo)
		return nil
	},
}

var mailInboxCmd = &cobra.Command{
	Use:          "inbox",
	Short:        "List pending messages",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		identityFlag, _ := cmd.Flags().GetString("identity")
		identity := resolveMailIdentity(identityFlag)
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
			return printJSON(mail.FromStoreMessages(msgs))
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
		identityFlag, _ := cmd.Flags().GetString("identity")
		identity := resolveMailIdentity(identityFlag)
		asJSON, _ := cmd.Flags().GetBool("json")

		s, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer s.Close()

		msg, err := s.ReadMessage(args[0])
		if err != nil {
			return err
		}

		if msg.Recipient != identity {
			fmt.Fprintf(os.Stderr, "warning: message %s belongs to %s, not %s\n", args[0], msg.Recipient, identity)
		}

		if asJSON {
			now := time.Now().UTC().Truncate(time.Second)
			return printJSON(mail.FromStoreMessage(*msg, &now))
		}

		fmt.Printf("From:    %s\n", msg.Sender)
		fmt.Printf("To:      %s\n", msg.Recipient)
		fmt.Printf("Via:     %s\n", msg.Via)
		fmt.Printf("Subject: %s\n", msg.Subject)
		fmt.Printf("Thread:  %s\n", msg.ThreadID)
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
		identityFlag, _ := cmd.Flags().GetString("identity")
		identity := resolveMailIdentity(identityFlag)
		asJSON, _ := cmd.Flags().GetBool("json")

		s, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer s.Close()

		// Fetch the message to check recipient before acking.
		// ReadMessage marks it as read, which is acceptable since we're acknowledging it anyway.
		msg, err := s.ReadMessage(args[0])
		if err != nil {
			return err
		}
		if msg.Recipient != identity {
			fmt.Fprintf(os.Stderr, "warning: message %s belongs to %s, not %s\n", args[0], msg.Recipient, identity)
		}

		if err := s.AckMessage(args[0]); err != nil {
			return err
		}

		if asJSON {
			now := time.Now().UTC().Truncate(time.Second)
			readAt := now // ReadMessage marked it as read
			apiMsg := mail.FromStoreMessage(*msg, &readAt)
			apiMsg.AcknowledgedAt = &now
			return printJSON(apiMsg)
		}

		fmt.Printf("Acknowledged: %s\n", args[0])
		return nil
	},
}

var mailCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Count unread messages",
	Long: `Check for unread messages and print the count.

Useful in scripts to conditionally process mail.

Exit codes:
  0 - Unread messages exist
  1 - No unread messages`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		identityFlag, _ := cmd.Flags().GetString("identity")
		identity := resolveMailIdentity(identityFlag)

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

Requires --confirm to proceed; without it, previews what would be deleted and exits 1.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		allAcked, _ := cmd.Flags().GetBool("all-acked")
		before, _ := cmd.Flags().GetString("before")
		confirm, _ := cmd.Flags().GetBool("confirm")

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

		fmt.Printf("Purged %d message(s).\n", count)
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
func bridgeMailToNudge(to, subject, body string, priority int) {
	// Defensive: callers are expected to pass canonicalized "world/agent" form,
	// but we never want a malformed recipient to panic the CLI.
	parts := strings.SplitN(to, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		fmt.Fprintf(os.Stderr, "mail bridge: skipping non-canonical recipient %q (expected world/agent format)\n", to)
		return
	}
	world, agent := parts[0], parts[1]

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
		Sender:   config.Autarch,
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

	mailSendCmd.Flags().String("to", "", "Recipient agent ID or \"autarch\"")
	mailSendCmd.Flags().String("subject", "", "Message subject")
	mailSendCmd.Flags().String("body", "", "Message body")
	mailSendCmd.Flags().String("body-file", "", "Read message body from file (\"-\" for stdin); mutually exclusive with --body")
	mailSendCmd.Flags().Int("priority", 2, "Priority (1=urgent, 2=normal, 3=low)")
	mailSendCmd.Flags().Bool("no-notify", false, "Suppress nudge notification to recipient")
	mailSendCmd.Flags().String("world", "", "world name")
	mailSendCmd.Flags().Bool("json", false, "Output as JSON")
	mailSendCmd.Flags().String("via", "", "Origin channel for external automation (default: SOL_VIA env var, then unset); rejects \"/\" and other agent-name-unsafe characters")
	mailSendCmd.Flags().String("thread", "", "Thread ID to group related messages (default: a fresh thread rooted at this message's own ID)")
	_ = mailSendCmd.MarkFlagRequired("to")
	_ = mailSendCmd.MarkFlagRequired("subject")

	mailInboxCmd.Flags().String("identity", "", "Recipient identity (default: auto-detected from SOL_WORLD/SOL_AGENT, or autarch)")
	mailInboxCmd.Flags().Bool("json", false, "Output as JSON")

	mailCheckCmd.Flags().String("identity", "", "Recipient identity (default: auto-detected from SOL_WORLD/SOL_AGENT, or autarch)")

	mailReadCmd.Flags().String("identity", "", "Caller identity for recipient verification (default: auto-detected from SOL_WORLD/SOL_AGENT, or autarch)")
	mailReadCmd.Flags().Bool("json", false, "Output as JSON")

	mailAckCmd.Flags().String("identity", "", "Caller identity for recipient verification (default: auto-detected from SOL_WORLD/SOL_AGENT, or autarch)")
	mailAckCmd.Flags().Bool("json", false, "Output as JSON")

	mailPurgeCmd.Flags().String("before", "", "Delete acked messages older than duration (e.g., 7d, 24h)")
	mailPurgeCmd.Flags().Bool("all-acked", false, "Delete all acknowledged messages regardless of age")
	mailPurgeCmd.Flags().Bool("confirm", false, "confirm destructive action")

	mailCmd.AddCommand(mailSendCmd)
	mailCmd.AddCommand(mailInboxCmd)
	mailCmd.AddCommand(mailReadCmd)
	mailCmd.AddCommand(mailAckCmd)
	mailCmd.AddCommand(mailCheckCmd)
	mailCmd.AddCommand(mailPurgeCmd)
}
