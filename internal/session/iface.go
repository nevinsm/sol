package session

import "time"

// SessionManager is the canonical interface for session operations.
// All consumers (dispatch, handoff, etc.) should use this interface
// rather than defining package-local subsets.
type SessionManager interface {
	Start(name, workdir, cmd string, env map[string]string, role, world string) error
	Stop(name string, force bool) error
	Exists(name string) bool
	Inject(name string, text string, submit bool) error
	Capture(name string, lines int) (string, error)
	Cycle(name, workdir, cmd string, env map[string]string, role, world string) error
	NudgeSession(name string, message string) error
	WaitForIdle(name string, timeout time.Duration) error
	CountSessions(prefix string) (int, error)
}

// Compile-time check: *Manager implements SessionManager.
var _ SessionManager = (*Manager)(nil)
