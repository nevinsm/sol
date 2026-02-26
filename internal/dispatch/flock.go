package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/nevinsm/gt/internal/config"
)

// WorkItemLock holds an advisory flock on a work item.
type WorkItemLock struct {
	file *os.File
	path string
}

// AcquireWorkItemLock takes an exclusive advisory lock on the given work
// item ID. The lock file is created at $GT_HOME/.runtime/locks/{itemID}.lock.
// Returns an error if the lock cannot be acquired (EAGAIN = already held).
// Uses LOCK_EX | LOCK_NB (non-blocking exclusive lock).
func AcquireWorkItemLock(itemID string) (*WorkItemLock, error) {
	lockDir := filepath.Join(config.RuntimeDir(), "locks")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to acquire lock for work item %s: %w", itemID, err)
	}

	lockPath := filepath.Join(lockDir, itemID+".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock for work item %s: %w", itemID, err)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return nil, fmt.Errorf("work item %s is being dispatched by another process", itemID)
		}
		return nil, fmt.Errorf("failed to acquire lock for work item %s: %w", itemID, err)
	}

	return &WorkItemLock{file: f, path: lockPath}, nil
}

// Release releases the advisory lock and removes the lock file.
func (l *WorkItemLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	l.file.Close()
	os.Remove(l.path)
	l.file = nil
	return nil
}

// MergeSlotLock holds an advisory flock on a rig's merge slot.
type MergeSlotLock struct {
	file *os.File
	path string
}

// AcquireMergeSlotLock takes an exclusive advisory lock on the merge slot
// for the given rig. Only one merge may be in progress per rig at a time.
// Lock file: $GT_HOME/.runtime/locks/{rig}-merge-slot.lock.
// Uses LOCK_EX | LOCK_NB (non-blocking exclusive lock).
func AcquireMergeSlotLock(rig string) (*MergeSlotLock, error) {
	lockDir := filepath.Join(config.RuntimeDir(), "locks")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to acquire merge slot for rig %s: %w", rig, err)
	}

	lockPath := filepath.Join(lockDir, rig+"-merge-slot.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire merge slot for rig %s: %w", rig, err)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return nil, fmt.Errorf("merge slot busy for rig %q", rig)
		}
		return nil, fmt.Errorf("failed to acquire merge slot for rig %s: %w", rig, err)
	}

	return &MergeSlotLock{file: f, path: lockPath}, nil
}

// Release releases the merge slot lock and removes the lock file.
func (l *MergeSlotLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	l.file.Close()
	os.Remove(l.path)
	l.file = nil
	return nil
}
