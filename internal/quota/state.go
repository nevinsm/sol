package quota

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/config"
)

// FileLock holds a flock on the quota state file.
type FileLock struct {
	file *os.File
}

// AcquireLock takes an exclusive flock on quota.json for the duration of a
// rotate operation. Returns the lock and loaded state. The lock MUST be
// released by calling Release when done.
func AcquireLock() (*FileLock, *State, error) {
	dir := config.RuntimeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("failed to create runtime dir: %w", err)
	}

	lockFile := filepath.Join(dir, "quota.json.lock")
	f, err := os.OpenFile(lockFile, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open quota lock: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("failed to acquire quota lock: %w", err)
	}

	state, err := Load()
	if err != nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		return nil, nil, err
	}

	return &FileLock{file: f}, state, nil
}

// Release releases the flock.
func (l *FileLock) Release() {
	if l == nil || l.file == nil {
		return
	}
	syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	l.file.Close()
	l.file = nil
}

// LimitedAccounts returns the handles of accounts with status "limited".
func (s *State) LimitedAccounts() []string {
	var limited []string
	for handle, acct := range s.Accounts {
		if acct.Status == Limited {
			limited = append(limited, handle)
		}
	}
	return limited
}

// AvailableAccountsLRU returns account handles with status "available",
// sorted by last_used ascending (least recently used first).
func (s *State) AvailableAccountsLRU() []string {
	type entry struct {
		handle   string
		lastUsed time.Time
	}
	var entries []entry
	for handle, acct := range s.Accounts {
		if acct.Status == Available {
			var lu time.Time
			if acct.LastUsed != nil {
				lu = *acct.LastUsed
			}
			entries = append(entries, entry{handle: handle, lastUsed: lu})
		}
	}

	// Sort by last_used ascending (LRU first). Use SliceStable so equal
	// timestamps preserve their insertion order — LRU is stability-sensitive
	// and matches the codebase's standard sort idiom (OP-L4).
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].lastUsed.Before(entries[j].lastUsed)
	})

	handles := make([]string, len(entries))
	for i, e := range entries {
		handles[i] = e.handle
	}
	return handles
}

// MarkLastUsed updates the last_used timestamp for an account.
func (s *State) MarkLastUsed(handle string) {
	now := time.Now().UTC()
	acct := s.ensureAccount(handle)
	acct.LastUsed = &now
}

// ExpireLimits transitions any limited accounts whose resets_at has passed
// back to "available". Accounts with nil ResetsAt are expired immediately to
// prevent permanent stuck-limited state. Returns the handles that were
// transitioned.
func (s *State) ExpireLimits() []string {
	now := time.Now().UTC()
	var expired []string
	for handle, acct := range s.Accounts {
		if acct.Status != Limited {
			continue
		}
		if acct.ResetsAt == nil {
			// No reset time known — expire immediately rather than staying
			// stuck in limited state forever.
			slog.Warn("quota: expiring account with nil ResetsAt", "account", handle)
			acct.Status = Available
			acct.LimitedAt = nil
			expired = append(expired, handle)
			continue
		}
		if now.After(*acct.ResetsAt) {
			acct.Status = Available
			acct.LimitedAt = nil
			acct.ResetsAt = nil
			expired = append(expired, handle)
		}
	}
	return expired
}
