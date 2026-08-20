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
	"github.com/nevinsm/sol/internal/style"
	"github.com/spf13/cobra"
)

// resolveMailIdentity returns the effective mail identity for the current
// caller. Delegates to config.ResolveActorIdentity — see its doc comment for
// the resolution precedence and trust model.
func resolveMailIdentity(flagValue string) string {
	return config.ResolveActorIdentity(flagValue)
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

// isThreadParticipant reports whether identity is the sender or recipient
// of at least one message in msgs. Shared by "mail thread" and "mail
// archive" for their access checks.
func isThreadParticipant(msgs []store.Message, identity string) bool {
	for _, m := range msgs {
		if m.Sender == identity || m.Recipient == identity {
			return true
		}
	}
	return false
}

var mailCmd = &cobra.Command{
	Use:     "mail",
	Short:   "Inter-agent messaging",
	GroupID: groupCommunication,
}

var mailSendCmd = &cobra.Command{
	Use:   "send",
	Short: "Send a message",
	Long: `Send a message to an agent or the autarch.

Wake-on-mail: if the recipient is an envoy with no live session, priority 1
(urgent) or 2 (normal) mail starts one automatically — via the same launch
path as "sol envoy start" — so the message doesn't sit unseen until someone
manually starts the envoy. Priority 3 (low) mail never triggers a wake; it
waits for the envoy's next natural session. Outposts are never auto-started
this way — their lifecycle is exclusively cast/dispatch-owned. --no-notify
suppresses both the nudge notification and this wake.`,
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
	Use:   "inbox",
	Short: "List pending messages",
	Long: `List pending messages for the caller's identity.

Archived threads are excluded by default -- archiving is meant to clear
inbox attention cost while preserving the record (see "sol mail archive").
Pass --all to include archived threads in the listing.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		identityFlag, _ := cmd.Flags().GetString("identity")
		identity := resolveMailIdentity(identityFlag)
		asJSON, _ := cmd.Flags().GetBool("json")
		all, _ := cmd.Flags().GetBool("all")

		s, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer s.Close()

		var msgs []store.Message
		if all {
			msgs, err = s.InboxAll(identity)
		} else {
			msgs, err = s.Inbox(identity)
		}
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
	Use:   "read <message-id>",
	Short: "Read a message (marks as read)",
	Long: `Read a message by ID, printing it and marking it read.

Cross-identity reads are allowed -- debugging another identity's mail is a
legitimate operation -- and print a warning to stderr when the caller
(resolved the same way as "mail ack" -- see --identity) is not the message's
recipient. Unlike a same-identity read, a cross-identity read does NOT mark
the message as read: the actual recipient still sees it as unread in "mail
inbox" and "mail check". This is a pure peek in that case.`,
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

		// Cross-identity reads are allowed (debugging is legit — see the
		// Long help below) but must not consume unread state: peek via
		// GetMessage instead of ReadMessage (which marks read=1) whenever
		// the caller isn't the recipient, so the actual recipient still
		// sees the message as unread.
		peek, err := s.GetMessage(args[0])
		if err != nil {
			return err
		}

		var msg *store.Message
		var readAt *time.Time
		if peek.Recipient != identity {
			fmt.Fprintf(os.Stderr, "warning: message %s belongs to %s, not %s\n", args[0], peek.Recipient, identity)
			msg = peek
		} else {
			msg, err = s.ReadMessage(args[0])
			if err != nil {
				return err
			}
			now := time.Now().UTC().Truncate(time.Second)
			readAt = &now
		}

		if asJSON {
			return printJSON(mail.FromStoreMessage(*msg, readAt))
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

var mailThreadCmd = &cobra.Command{
	Use:   "thread <thread-id>",
	Short: "View a full thread conversation",
	Long: `Print every message in a thread, in chronological order.

Reconstructs the whole conversation, unlike "mail read" (a single message)
or "mail inbox" (unread only). Read status does not filter the output and
is not mutated by this command — this is a pure read.

Access rule: the thread is shown only if the caller identity (resolved the
same way as "mail read" — see --identity) is the sender or recipient of at
least one message in it. Otherwise the command behaves as if the thread
does not exist.

Exit codes:
  0 - thread found and the caller has access to it
  1 - thread not found, or the caller has no access to any message in it`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		identityFlag, _ := cmd.Flags().GetString("identity")
		identity := resolveMailIdentity(identityFlag)
		asJSON, _ := cmd.Flags().GetBool("json")
		threadID := args[0]

		s, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer s.Close()

		msgs, err := s.Thread(threadID)
		if err != nil {
			return err
		}

		if !isThreadParticipant(msgs, identity) {
			return fmt.Errorf("thread %q not found", threadID)
		}

		if asJSON {
			return printJSON(mail.FromStoreMessages(msgs))
		}

		for i, m := range msgs {
			if i > 0 {
				fmt.Println("---")
			}
			fmt.Printf("From:    %s\n", m.Sender)
			fmt.Printf("To:      %s\n", m.Recipient)
			fmt.Printf("Via:     %s\n", m.Via)
			fmt.Printf("Date:    %s\n", m.CreatedAt.Format(time.RFC3339))
			fmt.Printf("Subject: %s\n", m.Subject)
			if m.Body != "" {
				fmt.Printf("\n%s\n", m.Body)
			}
		}
		return nil
	},
}

var mailArchiveCmd = &cobra.Command{
	Use:   "archive",
	Short: "Archive or unarchive a mail thread",
	Long: `Stamp every message in a thread as archived, clearing it from "mail inbox"
and "mail check" unread counts without deleting anything. Pass --unarchive
to reverse it.

Archiving preserves the audit trail: an archived thread remains fully
readable by "mail read <message-id>" and "mail thread <thread-id>" (thread
view always shows archived content -- it is a pure read, not a listing).
Archiving a thread with unread messages is allowed and expected -- that is
often the point, sweeping up dead-weight trickle -- and archived+unread
messages never count toward "mail check" or trigger anything.

Before archiving, distill anything durable (a decision, a fact worth
keeping) to its proper home -- an ADR, a brief, memory, a writ -- since mail
is the working medium, not the archive.

Authorization: the caller (resolved the same way as "mail read" -- see
--identity) must be a sender or recipient of at least one message in the
thread, or the autarch.

Exit codes:
  0 - thread archived (or unarchived)
  1 - --thread missing, thread not found, or the caller has no access to it`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		threadID, _ := cmd.Flags().GetString("thread")
		unarchive, _ := cmd.Flags().GetBool("unarchive")
		identityFlag, _ := cmd.Flags().GetString("identity")
		identity := resolveMailIdentity(identityFlag)
		asJSON, _ := cmd.Flags().GetBool("json")

		if threadID == "" {
			return fmt.Errorf("--thread is required")
		}

		s, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer s.Close()

		msgs, err := s.Thread(threadID)
		if err != nil {
			return err
		}

		// The autarch may archive any thread it can see (operator-level
		// housekeeping), not just ones it is a participant of -- a
		// deliberately broader rule than "mail thread"'s pure-read access
		// check, per the writ's authorization spec.
		hasAccess := identity == config.Autarch || isThreadParticipant(msgs, identity)
		if len(msgs) == 0 || !hasAccess {
			return fmt.Errorf("thread %q not found", threadID)
		}

		var n int64
		if unarchive {
			n, err = s.UnarchiveThread(threadID)
		} else {
			n, err = s.ArchiveThread(threadID)
		}
		if err != nil {
			return err
		}

		if asJSON {
			return printJSON(map[string]any{
				"thread_id": threadID,
				"archived":  !unarchive,
				"messages":  n,
			})
		}

		verb := "Archived"
		if unarchive {
			verb = "Unarchived"
		}
		fmt.Printf("%s thread %s (%d message(s)).\n", verb, threadID, n)
		return nil
	},
}

var mailAckCmd = &cobra.Command{
	Use:   "ack <message-id>",
	Short: "Acknowledge a message",
	Long: `Acknowledge a message, marking it delivery='acked'.

Ownership is enforced: a caller (resolved the same way as "mail read" --
see --identity) may only ack a message addressed to it. Acking a message
belonging to a different identity is refused -- pass --identity=<recipient>
to explicitly act on that identity's behalf. The autarch identity is the
one exception and may ack any message, mirroring the universal-access
precedent used by "mail archive".

Exit codes:
  0 - message acknowledged
  1 - message not found, or the caller does not own the message and is not
      the autarch`,
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

		// Peek (no side effect) before deciding whether to proceed -- a
		// refusal must not mark the message read out from under its
		// actual recipient.
		peek, err := s.GetMessage(args[0])
		if err != nil {
			return err
		}
		if peek.Recipient != identity && identity != config.Autarch {
			return fmt.Errorf("message %s belongs to %s, not %s; pass --identity=%s to act on its behalf", args[0], peek.Recipient, identity, peek.Recipient)
		}

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
	Short: "Delete messages from the sphere mailbox",
	Long: `Delete messages from the sphere mailbox. At least one selector is required:

  --all-acked               Acknowledged messages, regardless of age.
  --before=<duration>       Acknowledged messages with acked_at older than
                             duration (e.g. 7d, 24h). Ignored if --all-acked
                             is also set.
  --archived                Every message belonging to an archived thread
                             (see "sol mail archive"), regardless of ack or
                             read state.
  --older-than=<duration>   Narrows --archived to threads archived more than
                             duration ago. Requires --archived.
  --dismissed               Every message with delivery='dismissed' (see
                             the inbox TUI's dismiss action), regardless of
                             ack or read state.

--archived and --dismissed each compose with the other selectors by
intersection: passing more than one deletes only messages matching every
selection given (e.g. "--all-acked --archived" deletes messages that are
acknowledged AND archived). Used alone, --archived does not require the
messages to be acknowledged -- archiving a thread is itself a "done with
this" signal (see the mail skill's promotion norm: distill anything
durable, then archive), so an archived thread's unread stragglers are
eligible for purge too. --dismissed is the same kind of signal for a single
message the recipient chose not to engage with: dismissing it from the
inbox already means "done with this," so a dismissed message is eligible
for purge regardless of ack/read state, and it is otherwise invisible and
unpurgeable forever (no listing command surfaces dismissed mail).

Purge never touches messages outside the selectors above -- a message that
is neither acknowledged, archived, nor dismissed is never deleted.

Requires --confirm to proceed; without it, previews what would be deleted and exits 1.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		allAcked, _ := cmd.Flags().GetBool("all-acked")
		before, _ := cmd.Flags().GetString("before")
		archived, _ := cmd.Flags().GetBool("archived")
		olderThan, _ := cmd.Flags().GetString("older-than")
		dismissed, _ := cmd.Flags().GetBool("dismissed")
		confirm, _ := cmd.Flags().GetBool("confirm")

		if olderThan != "" && !archived {
			return fmt.Errorf("--older-than requires --archived")
		}
		if !allAcked && before == "" && !archived && !dismissed {
			return fmt.Errorf("must specify --before=<duration>, --all-acked, --archived, or --dismissed")
		}

		var filter store.PurgeFilter
		var desc []string
		if allAcked || before != "" {
			filter.RequireAcked = true
			if allAcked {
				desc = append(desc, "acknowledged")
			} else {
				dur, err := parseHumanDuration(before)
				if err != nil {
					return fmt.Errorf("invalid --before duration %q: %w", before, err)
				}
				cutoff := time.Now().UTC().Add(-dur)
				filter.AckedBefore = &cutoff
				desc = append(desc, fmt.Sprintf("acknowledged more than %s ago", before))
			}
		}
		if archived {
			filter.RequireArchived = true
			if olderThan != "" {
				dur, err := parseHumanDuration(olderThan)
				if err != nil {
					return fmt.Errorf("invalid --older-than duration %q: %w", olderThan, err)
				}
				cutoff := time.Now().UTC().Add(-dur)
				filter.ArchivedBefore = &cutoff
				desc = append(desc, fmt.Sprintf("archived more than %s ago", olderThan))
			} else {
				desc = append(desc, "archived")
			}
		}
		if dismissed {
			filter.RequireDismissed = true
			desc = append(desc, "dismissed")
		}

		s, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer s.Close()

		description := strings.Join(desc, " and ")

		if !confirm {
			n, err := s.CountPurgeCandidates(filter)
			if err != nil {
				return err
			}
			fmt.Printf("Would delete %d message(s) (%s).\n", n, description)
			fmt.Println("Run with --confirm to proceed.")
			return &exitError{code: 1}
		}

		count, err := s.PurgeMessages(filter)
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

// nudgeBodyMaxBytes bounds the mail body preview embedded in a nudge
// message.
const nudgeBodyMaxBytes = 500

// truncateNudgeBody trims body to the nudge preview budget. Rune-boundary
// safe — mail bodies are free-form user content and may contain multi-byte
// UTF-8 characters.
func truncateNudgeBody(body string) string {
	return style.TruncateBytes(body, nudgeBodyMaxBytes)
}

// bridgeMailToNudge resolves the recipient to a session and delivers a nudge notification.
// Best-effort: failures are logged to stderr but do not affect mail delivery.
//
// Wake-on-mail (operator-approved design, 2026-08-19 phone-steering arc): when
// no session is live, mail to an envoy (never an outpost — see
// envoyWakeEligible) at priority 1-2 starts one via the same path as `sol
// envoy start`, so a phone-initiated (or terminal) mail conversation never
// lands in dead air. Callers gate --no-notify by not calling this function at
// all (see mailSendCmd's RunE), which also suppresses the wake — one flag,
// one meaning.
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
	wake := false
	if !mgr.Exists(sessName) {
		wake = envoyWakeEligible(world, agent, priority)
		if !wake {
			// No active session and not eligible for wake — sphere mail is
			// the durable record.
			return
		}
	}

	// Map mail priority to nudge priority
	nudgePriority := "normal"
	if priority == 1 {
		nudgePriority = "urgent"
	}

	nudgeBody := truncateNudgeBody(body)

	// Enqueue the nudge FIRST — the per-agent nudge queue is drained on
	// session start, so if we're about to wake the envoy below, it sees this
	// MAIL notification on its first turn. If the wake fails, the nudge
	// stays queued (harmless) and the mail row remains the durable record
	// either way.
	if err := nudge.Deliver(sessName, nudge.Message{
		Sender:   config.Autarch,
		Type:     "MAIL",
		Subject:  subject,
		Body:     nudgeBody,
		Priority: nudgePriority,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "mail: warning: nudge delivery failed: %v\n", err)
	}

	if !wake {
		return
	}

	// Start the envoy session via the exact `sol envoy start` code path
	// (startEnvoySession, cmd/envoy.go) so guards — already-running check,
	// per-agent lock — stay uniform between manual and automatic starts.
	//
	// Soft-fail by design: mail delivery must never fail or block on a wake
	// problem. A slow session start adds at most a few seconds to `sol mail
	// send`'s CLI latency, which is acceptable — courier and scripts
	// invoking mail send already tolerate multi-second CLI round trips.
	// startEnvoySession bottoms out in startup.Launch, whose steps are
	// bounded filesystem/tmux operations (no network calls, no unbounded
	// waits) and the agent lock it acquires is non-blocking, so a busy or
	// stuck concurrent operation fails fast here rather than hanging `sol
	// mail send`. On failure we just log and return; the nudge enqueued
	// above is already durable and the mail itself was already sent.
	if _, err := startEnvoySession(world, agent); err != nil {
		fmt.Fprintf(os.Stderr, "mail: warning: envoy wake failed: %v\n", err)
		return
	}

	// Emit the existing session-start event type so the wake is visible in
	// `sol feed` (dash/cmd feed rendering already know EventSessionStart —
	// no new event type needed).
	events.NewLogger(config.Home()).Emit(events.EventSessionStart, "sol", world+"/"+agent, "both", map[string]string{
		"agent":  agent,
		"world":  world,
		"role":   "envoy",
		"reason": "mail_wake",
	})
}

// envoyWakeEligible reports whether a mail-triggered envoy wake should fire
// for recipient world/agent at the given mail priority. Design (operator-
// approved 2026-08-19, phone-steering arc):
//   - role must be "envoy" — outposts must never be auto-started; cast/
//     dispatch exclusively own outpost lifecycle, and the no-work launch
//     guard in startup.Launch exists specifically to keep an outpost from
//     spinning up with no bound writ.
//   - priority must be 1 (urgent) or 2 (normal) — priority 3 (e.g. the
//     durable-lessons trickle) must not burn a session/tokens on low-value
//     mail; it waits for the recipient's next natural session.
//
// Any lookup failure (unknown recipient, sphere store unavailable) is
// treated as "do not wake" — this preserves the prior silent-no-op behavior
// for recipients sol cannot positively identify as a live envoy.
func envoyWakeEligible(world, agent string, priority int) bool {
	if priority > 2 {
		return false
	}

	sphereStore, err := store.OpenSphere()
	if err != nil {
		fmt.Fprintf(os.Stderr, "mail: warning: wake eligibility check failed to open sphere store: %v\n", err)
		return false
	}
	defer sphereStore.Close()

	rec, err := sphereStore.GetAgent(world + "/" + agent)
	if err != nil {
		return false
	}
	return rec.Role == "envoy"
}

func init() {
	rootCmd.AddCommand(mailCmd)

	mailSendCmd.Flags().String("to", "", "Recipient agent ID or \"autarch\"")
	mailSendCmd.Flags().String("subject", "", "Message subject")
	mailSendCmd.Flags().String("body", "", "Message body")
	mailSendCmd.Flags().String("body-file", "", "Read message body from file (\"-\" for stdin); mutually exclusive with --body")
	mailSendCmd.Flags().Int("priority", 2, "Priority (1=urgent, 2=normal, 3=low)")
	mailSendCmd.Flags().Bool("no-notify", false, "Suppress nudge notification to recipient (also suppresses envoy wake-on-mail)")
	mailSendCmd.Flags().String("world", "", "world name")
	mailSendCmd.Flags().Bool("json", false, "Output as JSON")
	mailSendCmd.Flags().String("via", "", "Origin channel for external automation (default: SOL_VIA env var, then unset); rejects \"/\" and other agent-name-unsafe characters")
	mailSendCmd.Flags().String("thread", "", "Thread ID to group related messages (default: a fresh thread rooted at this message's own ID)")
	_ = mailSendCmd.MarkFlagRequired("to")
	_ = mailSendCmd.MarkFlagRequired("subject")

	mailInboxCmd.Flags().String("identity", "", "Recipient identity (default: auto-detected from SOL_WORLD/SOL_AGENT, or autarch)")
	mailInboxCmd.Flags().Bool("json", false, "Output as JSON")
	mailInboxCmd.Flags().Bool("all", false, "Include archived threads")

	mailCheckCmd.Flags().String("identity", "", "Recipient identity (default: auto-detected from SOL_WORLD/SOL_AGENT, or autarch)")

	mailReadCmd.Flags().String("identity", "", "Caller identity for recipient verification (default: auto-detected from SOL_WORLD/SOL_AGENT, or autarch)")
	mailReadCmd.Flags().Bool("json", false, "Output as JSON")

	mailThreadCmd.Flags().String("identity", "", "Caller identity for access verification (default: auto-detected from SOL_WORLD/SOL_AGENT, or autarch)")
	mailThreadCmd.Flags().Bool("json", false, "Output as JSON")

	mailArchiveCmd.Flags().String("thread", "", "Thread ID to archive (or unarchive)")
	mailArchiveCmd.Flags().Bool("unarchive", false, "Reverse a previous archive instead of archiving")
	mailArchiveCmd.Flags().String("identity", "", "Caller identity for access verification (default: auto-detected from SOL_WORLD/SOL_AGENT, or autarch)")
	mailArchiveCmd.Flags().Bool("json", false, "Output as JSON")
	_ = mailArchiveCmd.MarkFlagRequired("thread")

	mailAckCmd.Flags().String("identity", "", "Caller identity for recipient verification (default: auto-detected from SOL_WORLD/SOL_AGENT, or autarch)")
	mailAckCmd.Flags().Bool("json", false, "Output as JSON")

	mailPurgeCmd.Flags().String("before", "", "Delete acked messages older than duration (e.g., 7d, 24h)")
	mailPurgeCmd.Flags().Bool("all-acked", false, "Delete all acknowledged messages regardless of age")
	mailPurgeCmd.Flags().Bool("archived", false, "Delete messages belonging to archived threads, regardless of ack/read state")
	mailPurgeCmd.Flags().String("older-than", "", "Narrow --archived to threads archived more than duration ago (e.g., 30d); requires --archived")
	mailPurgeCmd.Flags().Bool("dismissed", false, "Delete dismissed messages, regardless of ack/read state")
	mailPurgeCmd.Flags().Bool("confirm", false, "confirm destructive action")

	mailCmd.AddCommand(mailSendCmd)
	mailCmd.AddCommand(mailInboxCmd)
	mailCmd.AddCommand(mailReadCmd)
	mailCmd.AddCommand(mailThreadCmd)
	mailCmd.AddCommand(mailArchiveCmd)
	mailCmd.AddCommand(mailAckCmd)
	mailCmd.AddCommand(mailCheckCmd)
	mailCmd.AddCommand(mailPurgeCmd)
}
