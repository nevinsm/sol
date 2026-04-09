// Package sessionsave provides a best-effort "prompt the agent to save state,
// then wait for output stability" primitive used before destructive session
// operations (stop, cycle). It replaces the retired internal/brief/graceful.go
// for the Claude Code auto-memory era.
//
// The auto-memory feature is good but not great: in practice, an explicit
// "you are about to be killed, write your MEMORY.md now" prompt produces
// noticeably higher-quality memory than letting the natural shutdown
// behavior take its course. Prompt() injects such a prompt into the target
// session's tmux pane and then polls the pane until output goes idle (or a
// hard timeout fires), so the agent has a real chance to write before the
// caller proceeds with the destructive operation.
//
// All polling is best-effort: capture errors are tolerated and the function
// returns nil after the timeout regardless of stability. The only hard error
// is failure to inject the initial prompt — callers can log it and continue.
package sessionsave

import (
	"log/slog"
	"time"
)

// Prompts injected into the session before destructive operations. Kept as
// exported constants so callers wire them in by name (and so tests can assert
// the exact text was sent).
const (
	EnvoyStopPrompt    = "Your session is about to stop. If you have any important state not yet saved, update MEMORY.md now."
	HandoffCyclePrompt = "Your session is about to cycle (handoff). If you have any important state not yet saved, update MEMORY.md now so your successor has it."
)

// captureLines is the number of pane lines sampled per poll. Enough to catch
// the busy "esc to interrupt" status bar plus a few lines of recent output;
// large enough that minor cursor movement does not register as "still
// changing" forever.
const captureLines = 40

// Sender is the narrow set of session-manager methods sessionsave depends on.
// session.SessionManager from internal/session satisfies this interface, and
// tests can supply their own fake without pulling in the full manager surface.
type Sender interface {
	Inject(name string, text string, submit bool) error
	Capture(name string, lines int) (string, error)
}

// Options tunes the polling behavior. Zero values are replaced with sensible
// defaults; pass Options{} to take all defaults.
type Options struct {
	// PollInterval is how often the pane is sampled. Default: 500ms.
	PollInterval time.Duration
	// StabilityWindow is how long the sampled output must remain unchanged
	// before the session is considered idle. Default: 3s.
	StabilityWindow time.Duration
	// Timeout is the hard upper bound on the entire wait. After this, the
	// function returns nil regardless of whether the session ever went idle.
	// Default: 30s.
	Timeout time.Duration
}

func (o *Options) applyDefaults() {
	if o.PollInterval <= 0 {
		o.PollInterval = 500 * time.Millisecond
	}
	if o.StabilityWindow <= 0 {
		o.StabilityWindow = 3 * time.Second
	}
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}
}

// Prompt injects promptText into the named session and polls the pane until
// the captured output stops changing for opts.StabilityWindow, or until
// opts.Timeout elapses.
//
// Returns nil on stable idle OR on timeout — both are treated as success
// because the operation is best-effort. Returns an error only when the
// initial Inject call fails; in that case the caller may log it and proceed
// with the destructive operation anyway.
//
// Capture errors during polling are tolerated: they are logged at warn level
// via slog.Default and polling continues. The rationale is that a transient
// tmux hiccup (or the session being torn down by something else) should not
// short-circuit the wait — the timeout is the real backstop.
func Prompt(mgr Sender, sessionName, promptText string, opts Options) error {
	opts.applyDefaults()

	if err := mgr.Inject(sessionName, promptText, true); err != nil {
		return err
	}

	deadline := time.Now().Add(opts.Timeout)
	var (
		lastSample  string
		haveSample  bool
		stableSince time.Time
	)

	for {
		if !time.Now().Before(deadline) {
			return nil
		}
		time.Sleep(opts.PollInterval)

		sample, err := mgr.Capture(sessionName, captureLines)
		if err != nil {
			slog.Default().Warn("sessionsave: capture failed",
				"session", sessionName, "error", err)
			continue
		}

		if !haveSample || sample != lastSample {
			lastSample = sample
			haveSample = true
			stableSince = time.Now()
			continue
		}

		if time.Since(stableSince) >= opts.StabilityWindow {
			return nil
		}
	}
}
