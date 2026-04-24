package dispatch

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nevinsm/sol/internal/config"
)

// WritLock holds an advisory flock on a writ.
type WritLock struct {
	file *os.File
	path string
}

// AcquireWritLock takes an exclusive advisory lock on the given work
// item ID. The lock file is created at $SOL_HOME/.runtime/locks/{itemID}.lock.
// Returns an error if the lock cannot be acquired (EAGAIN = already held).
// Uses LOCK_EX | LOCK_NB (non-blocking exclusive lock).
func AcquireWritLock(itemID string) (*WritLock, error) {
	lockDir := filepath.Join(config.RuntimeDir(), "locks")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to acquire lock for writ %q: %w", itemID, err)
	}

	lockPath := filepath.Join(lockDir, itemID+".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock for writ %q: %w", itemID, err)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return nil, fmt.Errorf("writ %q is being dispatched by another process", itemID)
		}
		return nil, fmt.Errorf("failed to acquire lock for writ %q: %w", itemID, err)
	}

	return &WritLock{file: f, path: lockPath}, nil
}

// Release releases the advisory lock. The lock file is intentionally
// preserved to maintain mutual exclusion — deleting it would allow a new
// process to create a fresh inode and acquire a separate lock concurrently.
func (l *WritLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if err := errors.Join(unlockErr, closeErr); err != nil {
		slog.Warn("lock release failed", "lock", "WritLock", "path", l.path, "error", err)
		return err
	}
	return nil
}

// AgentLock holds an advisory flock on an agent.
type AgentLock struct {
	file *os.File
	path string
}

// AcquireAgentLock takes an exclusive advisory lock on the given agent.
// Lock file: $SOL_HOME/.runtime/locks/agent-{sanitizedID}.lock.
// Uses LOCK_EX | LOCK_NB (non-blocking exclusive lock).
func AcquireAgentLock(agentID string) (*AgentLock, error) {
	lockDir := filepath.Join(config.RuntimeDir(), "locks")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to acquire lock for agent %q: %w", agentID, err)
	}

	// Replace "/" in agent IDs (e.g., "ember/Toast") with "--" for safe filenames.
	safe := strings.ReplaceAll(agentID, "/", "--")
	lockPath := filepath.Join(lockDir, "agent-"+safe+".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock for agent %q: %w", agentID, err)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return nil, fmt.Errorf("agent %q is being dispatched by another process", agentID)
		}
		return nil, fmt.Errorf("failed to acquire lock for agent %q: %w", agentID, err)
	}

	return &AgentLock{file: f, path: lockPath}, nil
}

// Release releases the agent lock. It is idempotent — calling Release on an
// already-released lock is a no-op. This is important for the restart path
// where both restartSession's unlockFn and a defer may call Release.
func (l *AgentLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if err := errors.Join(unlockErr, closeErr); err != nil {
		slog.Warn("lock release failed", "lock", "AgentLock", "path", l.path, "error", err)
		return err
	}
	return nil
}

// MergeSlotLock holds an advisory flock on a world's merge slot.
type MergeSlotLock struct {
	file *os.File
	path string
}

// AcquireMergeSlotLock takes an exclusive advisory lock on the merge slot
// for the given world. Only one merge may be in progress per world at a time.
// Lock file: $SOL_HOME/.runtime/locks/{world}-merge-slot.lock.
// Uses LOCK_EX | LOCK_NB (non-blocking exclusive lock).
func AcquireMergeSlotLock(world string) (*MergeSlotLock, error) {
	lockDir := filepath.Join(config.RuntimeDir(), "locks")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to acquire merge slot for world %q: %w", world, err)
	}

	lockPath := filepath.Join(lockDir, world+"-merge-slot.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire merge slot for world %q: %w", world, err)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return nil, fmt.Errorf("merge slot busy for world %q", world)
		}
		return nil, fmt.Errorf("failed to acquire merge slot for world %q: %w", world, err)
	}

	return &MergeSlotLock{file: f, path: lockPath}, nil
}

// Release releases the merge slot lock. The lock file is preserved to
// maintain mutual exclusion across concurrent processes.
func (l *MergeSlotLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if err := errors.Join(unlockErr, closeErr); err != nil {
		slog.Warn("lock release failed", "lock", "MergeSlotLock", "path", l.path, "error", err)
		return err
	}
	return nil
}

// SphereSessionLock holds an advisory flock that serializes session-start
// operations across all worlds. This prevents concurrent Cast calls in
// different worlds from both passing the sphere-wide max_sessions check
// before either starts a session.
//
// Lock ordering: acquire the per-world ProvisionLock FIRST, then the
// SphereSessionLock. This consistent ordering avoids deadlocks.
//
// Lock file: $SOL_HOME/.runtime/locks/sphere-session.lock
// Uses LOCK_EX (blocking) so callers wait rather than error on contention.
// The lock is held from the capacity check through session creation — the
// caller (Cast) releases it after startup.Launch completes.
type SphereSessionLock struct {
	file *os.File
	path string
}

// AcquireSphereSessionLock takes a blocking exclusive advisory lock on the
// sphere-wide session slot. This must be acquired AFTER any per-world
// ProvisionLock to maintain consistent lock ordering and avoid deadlocks.
func AcquireSphereSessionLock() (*SphereSessionLock, error) {
	lockDir := filepath.Join(config.RuntimeDir(), "locks")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to acquire sphere session lock: %w", err)
	}

	lockPath := filepath.Join(lockDir, "sphere-session.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire sphere session lock: %w", err)
	}

	// Blocking lock: wait for any concurrent session-start to finish.
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to acquire sphere session lock: %w", err)
	}

	return &SphereSessionLock{file: f, path: lockPath}, nil
}

// Release releases the sphere session lock. The lock file is preserved to
// maintain mutual exclusion across concurrent processes.
// It is idempotent — calling Release on an already-released lock is a no-op.
func (l *SphereSessionLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if err := errors.Join(unlockErr, closeErr); err != nil {
		slog.Warn("lock release failed", "lock", "SphereSessionLock", "path", l.path, "error", err)
		return err
	}
	return nil
}

// ProvisionLock holds an advisory flock that serializes autoProvision calls
// for a given world. This prevents concurrent Cast calls from both passing
// the capacity check and both creating an agent, exceeding the world limit.
type ProvisionLock struct {
	file *os.File
	path string
}

// AcquireProvisionLock takes a blocking exclusive advisory lock on the
// provision slot for the given world.
// Lock file: $SOL_HOME/.runtime/locks/{world}-provision.lock.
// Uses LOCK_EX (blocking) so callers wait rather than error on contention.
func AcquireProvisionLock(world string) (*ProvisionLock, error) {
	lockDir := filepath.Join(config.RuntimeDir(), "locks")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to acquire provision lock for world %q: %w", world, err)
	}

	lockPath := filepath.Join(lockDir, world+"-provision.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire provision lock for world %q: %w", world, err)
	}

	// Blocking lock: wait for any concurrent autoProvision to finish.
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to acquire provision lock for world %q: %w", world, err)
	}

	return &ProvisionLock{file: f, path: lockPath}, nil
}

// Release releases the provision lock. The lock file is preserved to
// maintain mutual exclusion across concurrent processes.
func (l *ProvisionLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if err := errors.Join(unlockErr, closeErr); err != nil {
		slog.Warn("lock release failed", "lock", "ProvisionLock", "path", l.path, "error", err)
		return err
	}
	return nil
}
