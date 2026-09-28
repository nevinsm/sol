// Package session manages tmux-based process containers for AI agents — start, stop, inject, capture, and liveness operations.
package session

import "time"

// SessionManager is the canonical interface for session operations.
// All consumers (dispatch, handoff, etc.) should use this interface
// rather than defining package-local subsets.
type SessionManager interface {
	Start(name, workdir, cmd string, env map[string]string, role, world string) error
	Stop(name string, force bool) error
	Exists(name string) bool
	Capture(name string, lines int) (string, error)
	Cycle(name, workdir, cmd string, env map[string]string, role, world string) error
	NudgeSession(name string, message string) error
	WaitForIdle(name string, timeout time.Duration) error
	CountSessions(prefix string) (int, error)
	// ProcessTree returns the pane's root process and all of its descendants,
	// sorted by PID. It is a second, harness-neutral observation channel
	// alongside Capture's pane text — see ADR context in
	// docs/failure-modes.md. Returns an empty (non-nil-error) slice when the
	// pane has no child processes.
	ProcessTree(name string) ([]ProcessInfo, error)
}

// ProcessInfo describes one process in a pane's process tree.
type ProcessInfo struct {
	PID      int
	PPID     int
	Command  string
	CPUTicks uint64
}

// Compile-time check: *Manager implements SessionManager.
var _ SessionManager = (*Manager)(nil)
