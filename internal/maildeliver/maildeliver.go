// Package maildeliver implements the mail delivery-signal stack shared by
// every call site that inserts a durable mail row and wants the recipient
// to actually notice it: `sol mail send` (cmd/mail.go), caravan
// completion mail (internal/consul's patrol and cmd/caravan.go's close
// paths), and forge's writ-merged/writ-failed notifications
// (internal/forge/toolbox.go).
//
// Durable storage and delivery signaling are deliberately layered apart:
// internal/store's message-insert methods (SendMessage and its
// SendMessageWithThread* siblings) only ever write the messages table —
// they must stay free of session/nudge dependencies (see
// docs/conventions and internal/store/CONVENTIONS.md-style layering).
// This package is the delivery leg: it never writes to the mailbox
// itself, it only signals that a message a caller already inserted
// durably has now arrived — a pane doorbell nudge for a live session, or
// a wake-on-mail session start for a stopped envoy.
//
// Before this package existed, this whole stack (nudge enqueue with body
// preview, doorbell, wake-on-mail with its priority/role gates, the
// autarch-recipient exemption, --no-notify suppression) lived only
// inline in cmd/mail.go's `mail send` command — every other insert path
// (caravan close, forge merge/failure notices) wrote mail durably but
// produced no delivery signal at all. Deliver is the single
// implementation now; every caller above routes through it.
package maildeliver

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/envoy"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/flock"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/style"
)

// previewMaxBytes bounds the mail body preview embedded in a nudge
// message.
const previewMaxBytes = 500

// TruncatePreview trims body to the nudge preview budget. Rune-boundary
// safe — mail bodies are free-form user content and may contain
// multi-byte UTF-8 characters.
func TruncatePreview(body string) string {
	return style.TruncateBytes(body, previewMaxBytes)
}

// Opts is the input to Deliver: one mail message a caller has already
// durably inserted into the sphere mailbox.
type Opts struct {
	// Recipient is the canonical "world/agent" identity, or
	// config.Autarch. Any other shape is treated as a caller bug and
	// reported via the returned error rather than risking a panic.
	Recipient string
	// MessageID is the durable message's ID (e.g. "msg-xxxx"). Deliver
	// does not use it to look anything up; it exists for callers that
	// want it available in future diagnostics without a second insert
	// round trip.
	MessageID string
	// Subject and Body are copied from the durable insert. Deliver
	// truncates its own preview of Body for the nudge queue — callers
	// should pass the full body.
	Subject string
	Body    string
	// Priority is the mail priority: 1 (urgent), 2 (normal), 3 (low).
	Priority int
	// Suppress mirrors `sol mail send --no-notify`: when true, Deliver is
	// a complete no-op. Present so every caller can route through one
	// call site rather than branching around it.
	Suppress bool
}

// Deliver signals delivery of a mail message the caller already inserted
// durably. It applies, in order, every gate `sol mail send` has always
// applied before this package existed:
//
//  1. Suppress (--no-notify) — no-op.
//  2. Autarch recipient — no-op; the autarch has no session to nudge or
//     wake, sphere mail is itself the record.
//  3. A live session for the recipient — enqueue+doorbell nudge (see
//     nudge.Deliver).
//  4. No live session — wake-on-mail (operator-approved design,
//     2026-08-19 phone-steering arc): an envoy recipient (never an
//     outpost — see envoyWakeEligible) at priority 1 (urgent) or 2
//     (normal) gets a session started via the same path as `sol envoy
//     start`, so a mail conversation never lands in dead air. Priority 3
//     (low) mail never triggers a wake; it waits for the recipient's
//     next natural session. The nudge is enqueued FIRST, before the wake
//     is attempted, so the queue holds the durable record regardless of
//     whether the wake itself succeeds.
//
// Best-effort throughout: Deliver's own failures (a malformed recipient,
// a nudge enqueue error, a wake failure) are folded into the returned
// error for the caller to log through its own channel — they must never
// fail the caller's insert/close/merge. A nil return does not guarantee
// delivery, only that Deliver observed no problem signaling it.
func Deliver(opts Opts) error {
	if opts.Suppress {
		return nil
	}
	if opts.Recipient == config.Autarch {
		return nil
	}

	// Defensive: callers are expected to pass canonicalized "world/agent"
	// form, but with multiple call sites now (CLI, caravan close, forge)
	// a malformed recipient must never panic its caller.
	parts := strings.SplitN(opts.Recipient, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("maildeliver: non-canonical recipient %q (expected world/agent format)", opts.Recipient)
	}
	world, agent := parts[0], parts[1]

	sessName := config.SessionName(world, agent)
	mgr := session.New()

	var errs []error

	wake := false
	if !mgr.Exists(sessName) {
		var wakeErr error
		wake, wakeErr = envoyWakeEligible(world, agent, opts.Priority)
		if wakeErr != nil {
			errs = append(errs, wakeErr)
		}
		if !wake {
			// No active session and not eligible for wake — sphere mail
			// is the durable record.
			return errors.Join(errs...)
		}
	}

	nudgePriority := "normal"
	if opts.Priority == 1 {
		nudgePriority = "urgent"
	}

	// Enqueue the nudge FIRST — the per-agent nudge queue is drained on
	// session start, so if we're about to wake the envoy below, it sees
	// this MAIL notification on its first turn. If the wake fails, the
	// nudge stays queued (harmless) and the mail row remains the durable
	// record either way.
	if err := nudge.Deliver(sessName, nudge.Message{
		Sender:   config.Autarch,
		Type:     "MAIL",
		Subject:  opts.Subject,
		Body:     TruncatePreview(opts.Body),
		Priority: nudgePriority,
	}); err != nil {
		errs = append(errs, fmt.Errorf("nudge delivery failed: %w", err))
	}

	if !wake {
		return errors.Join(errs...)
	}

	// Start the envoy session via the exact `sol envoy start` code path
	// (startEnvoySession below, mirroring cmd/envoy.go's) so guards —
	// already-running check, per-agent lock — stay uniform between
	// manual and automatic starts.
	//
	// Soft-fail by design: mail delivery must never fail or block on a
	// wake problem. startEnvoySession bottoms out in startup.Launch,
	// whose steps are bounded filesystem/tmux operations (no network
	// calls, no unbounded waits) and the agent lock it acquires is
	// non-blocking, so a busy or stuck concurrent operation fails fast
	// here rather than hanging the caller.
	if _, err := startEnvoySession(world, agent); err != nil {
		errs = append(errs, fmt.Errorf("envoy wake failed: %w", err))
		return errors.Join(errs...)
	}

	// Emit the existing session-start event type so the wake is visible
	// in `sol feed` (dash/cmd feed rendering already know
	// EventSessionStart — no new event type needed).
	events.NewLogger(config.Home()).Emit(events.EventSessionStart, "sol", world+"/"+agent, "both", map[string]string{
		"agent":  agent,
		"world":  world,
		"role":   "envoy",
		"reason": "mail_wake",
	})

	return errors.Join(errs...)
}

// envoyWakeEligible reports whether a mail-triggered envoy wake should
// fire for recipient world/agent at the given mail priority. Design
// (operator-approved 2026-08-19, phone-steering arc):
//   - role must be "envoy" — outposts must never be auto-started;
//     cast/dispatch exclusively own outpost lifecycle, and the no-work
//     launch guard in startup.Launch exists specifically to keep an
//     outpost from spinning up with no bound writ.
//   - priority must be 1 (urgent) or 2 (normal) — priority 3 (e.g. the
//     durable-lessons trickle) must not burn a session/tokens on
//     low-value mail; it waits for the recipient's next natural session.
//
// A lookup failure for an unknown recipient is treated as "do not wake"
// with no error — this preserves the prior silent-no-op behavior for
// recipients sol cannot positively identify as a live envoy. A sphere
// store open failure IS reported via the error return (distinct from
// "recipient not found") since that is an operational problem worth a
// caller logging, not a routine "recipient isn't an envoy" outcome.
func envoyWakeEligible(world, agent string, priority int) (bool, error) {
	if priority > 2 {
		return false, nil
	}

	sphereStore, err := store.OpenSphere()
	if err != nil {
		return false, fmt.Errorf("wake eligibility check failed to open sphere store: %w", err)
	}
	defer sphereStore.Close()

	rec, err := sphereStore.GetAgent(world + "/" + agent)
	if err != nil {
		return false, nil
	}
	return rec.Role == "envoy", nil
}

// startEnvoySession starts an envoy tmux session, holding the per-agent
// lock across the launch so it can't race a concurrent manual start/stop.
// Mirrors cmd/envoy.go's startEnvoySession exactly (same underlying
// startup.Launch call with the same RoleConfig) — internal/maildeliver
// cannot import the cmd package (cmd imports internal packages, not the
// other way around), so the wake path calls the same lower-level
// primitives cmd/envoy.go's wrapper does rather than sharing that
// wrapper directly.
func startEnvoySession(world, agent string) (string, error) {
	agentID := world + "/" + agent
	agentLock, err := flock.AcquireAgentLock(agentID)
	if err != nil {
		return "", fmt.Errorf("failed to start envoy: %w", err)
	}
	defer agentLock.Release()

	sessName, err := startup.Launch(envoy.RoleConfig(), world, agent, startup.LaunchOpts{})
	if err != nil {
		return "", fmt.Errorf("failed to start envoy: %w", err)
	}
	return sessName, nil
}
