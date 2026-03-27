package brief

import (
	"crypto/sha256"
	"fmt"
	"os"
	"time"
)

const captureLines = 50

// StopPrompt is the message injected into a session before graceful shutdown.
const StopPrompt = "[sol] Session is shutting down. Update your brief (.brief/memory.md) now with current state, decisions, and next steps."

// GracefulStopManager abstracts session operations needed for graceful stop.
type GracefulStopManager interface {
	Exists(name string) bool
	Inject(name string, text string, submit bool) error
	Capture(name string, lines int) (string, error)
	Stop(name string, force bool) error
}

// GracefulStop injects a brief-update prompt into the session, polls for
// output stability, then kills the session. Gives the agent time to save
// state to .brief/memory.md before termination.
//
// If briefDir doesn't exist, the session is killed immediately (outpost
// behavior). Polls pane content every 10s; after 5 consecutive unchanged
// captures (50s stable), kills the session. Falls back to force-kill after
// 90s max timeout.
//
// When SOL_SESSION_COMMAND is set (test stub sessions), uses aggressive
// timeouts (100ms poll, 2 stable checks, 1s max) since the stub process
// will never produce meaningful output.
func GracefulStop(sessName, briefDir string, mgr GracefulStopManager) error {
	if os.Getenv("SOL_SESSION_COMMAND") != "" {
		return gracefulStop(sessName, briefDir, mgr, 100*time.Millisecond, 2, time.Second)
	}
	return gracefulStop(sessName, briefDir, mgr, 10*time.Second, 4, 90*time.Second)
}

func gracefulStop(sessName, briefDir string, mgr GracefulStopManager,
	pollInterval time.Duration, stableThreshold int, maxTimeout time.Duration) error {

	// No brief directory -> immediate kill (outpost behavior).
	if _, err := os.Stat(briefDir); os.IsNotExist(err) {
		return mgr.Stop(sessName, true)
	}

	// Inject the stop prompt.
	fmt.Fprintf(os.Stderr, "Requesting brief update before shutdown...\n")
	if err := mgr.Inject(sessName, StopPrompt, true); err != nil {
		// Session may have died between Exists check and Inject.
		if mgr.Exists(sessName) {
			return mgr.Stop(sessName, true)
		}
		return nil
	}

	// Poll for output stability.
	deadline := time.Now().Add(maxTimeout)
	var lastHash string
	unchangedCount := 0

	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)

		if !mgr.Exists(sessName) {
			return nil // Session exited on its own.
		}

		content, err := mgr.Capture(sessName, captureLines)
		if err != nil {
			continue
		}

		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))

		if lastHash != "" && hash == lastHash {
			unchangedCount++
		} else {
			unchangedCount = 0
		}
		lastHash = hash

		if unchangedCount >= stableThreshold {
			break
		}
	}

	// Kill the session.
	if mgr.Exists(sessName) {
		return mgr.Stop(sessName, true)
	}

	return nil
}
