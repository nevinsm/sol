package quota

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadStateMissing(t *testing.T) {
	setupTestDir(t)

	state, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Accounts) != 0 {
		t.Errorf("expected empty accounts, got %d", len(state.Accounts))
	}
	if len(state.PausedSessions) != 0 {
		t.Errorf("expected empty paused sessions, got %d", len(state.PausedSessions))
	}
}

func TestSaveAndLoadViaLock(t *testing.T) {
	setupTestDir(t)

	now := time.Now().UTC().Truncate(time.Second)
	state := &State{
		Accounts: map[string]*AccountState{
			"personal": {
				Status:   Available,
				LastUsed: &now,
			},
			"work": {
				Status:    Limited,
				LimitedAt: &now,
				ResetsAt:  timePtr(now.Add(time.Hour)),
			},
		},
		PausedSessions: make(map[string]PausedSession),
	}

	lock, _, err := AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(state); err != nil {
		lock.Release()
		t.Fatal(err)
	}
	lock.Release()

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if loaded.Accounts["personal"].Status != Available {
		t.Errorf("expected personal=available, got %q", loaded.Accounts["personal"].Status)
	}
	if loaded.Accounts["work"].Status != Limited {
		t.Errorf("expected work=limited, got %q", loaded.Accounts["work"].Status)
	}
}

func TestLimitedAccounts(t *testing.T) {
	state := &State{
		Accounts: map[string]*AccountState{
			"a": {Status: Available},
			"b": {Status: Limited},
			"c": {Status: Available},
			"d": {Status: Limited},
		},
	}

	limited := state.LimitedAccounts()
	if len(limited) != 2 {
		t.Fatalf("expected 2 limited accounts, got %d", len(limited))
	}

	// Both b and d should be present (order not guaranteed).
	set := make(map[string]bool)
	for _, h := range limited {
		set[h] = true
	}
	if !set["b"] || !set["d"] {
		t.Errorf("expected b and d to be limited, got %v", limited)
	}
}

func TestAvailableAccountsLRU(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)

	state := &State{
		Accounts: map[string]*AccountState{
			"newest":  {Status: Available, LastUsed: &t3},
			"oldest":  {Status: Available, LastUsed: &t1},
			"middle":  {Status: Available, LastUsed: &t2},
			"limited": {Status: Limited, LastUsed: &t1},
		},
	}

	lru := state.AvailableAccountsLRU()
	if len(lru) != 3 {
		t.Fatalf("expected 3 available accounts, got %d", len(lru))
	}
	if lru[0] != "oldest" {
		t.Errorf("expected LRU first=oldest, got %q", lru[0])
	}
	if lru[1] != "middle" {
		t.Errorf("expected LRU second=middle, got %q", lru[1])
	}
	if lru[2] != "newest" {
		t.Errorf("expected LRU third=newest, got %q", lru[2])
	}
}

func TestAvailableAccountsLRUNilLastUsed(t *testing.T) {
	t2 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	state := &State{
		Accounts: map[string]*AccountState{
			"used":  {Status: Available, LastUsed: &t2},
			"never": {Status: Available, LastUsed: nil},
		},
	}

	lru := state.AvailableAccountsLRU()
	if len(lru) != 2 {
		t.Fatalf("expected 2, got %d", len(lru))
	}
	// nil LastUsed (zero time) should sort first.
	if lru[0] != "never" {
		t.Errorf("expected never-used first, got %q", lru[0])
	}
}

func TestExpireLimits(t *testing.T) {
	past := time.Now().UTC().Add(-time.Hour)
	future := time.Now().UTC().Add(time.Hour)

	state := &State{
		Accounts: map[string]*AccountState{
			"expired": {
				Status:    Limited,
				LimitedAt: &past,
				ResetsAt:  &past,
			},
			"still_limited": {
				Status:    Limited,
				LimitedAt: &past,
				ResetsAt:  &future,
			},
			"available": {
				Status: Available,
			},
		},
	}

	expired := state.ExpireLimits()
	if len(expired) != 1 || expired[0] != "expired" {
		t.Errorf("expected [expired], got %v", expired)
	}
	if state.Accounts["expired"].Status != Available {
		t.Errorf("expected expired account to be available, got %q", state.Accounts["expired"].Status)
	}
	if state.Accounts["still_limited"].Status != Limited {
		t.Errorf("expected still_limited to remain limited, got %q", state.Accounts["still_limited"].Status)
	}
}

func TestAcquireLockAndRelease(t *testing.T) {
	setupTestDir(t)

	lock, state, err := AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	if state == nil {
		t.Fatal("expected non-nil state")
	}

	// Modify and save.
	now := time.Now().UTC()
	state.Accounts["test"] = &AccountState{
		Status:   Available,
		LastUsed: &now,
	}
	if err := Save(state); err != nil {
		t.Fatal(err)
	}

	lock.Release()

	// Re-acquire and verify.
	lock2, state2, err := AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock2.Release()

	if state2.Accounts["test"].Status != Available {
		t.Errorf("expected test=available, got %q", state2.Accounts["test"].Status)
	}
}

func TestMarkLastUsed(t *testing.T) {
	state := &State{
		Accounts: map[string]*AccountState{
			"test": {Status: Available},
		},
	}

	if state.Accounts["test"].LastUsed != nil {
		t.Fatal("expected nil LastUsed initially")
	}

	state.MarkLastUsed("test")

	if state.Accounts["test"].LastUsed == nil {
		t.Fatal("expected non-nil LastUsed after MarkLastUsed")
	}
}

// TestConcurrentAcquireLockNoLostWrites verifies that concurrent
// AcquireLock+Load+modify+Save+Release cycles never lose writes.
// Each goroutine adds a unique account; all accounts must survive.
func TestConcurrentAcquireLockNoLostWrites(t *testing.T) {
	setupTestDir(t)

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		handle := fmt.Sprintf("account-%d", i)
		go func(h string) {
			defer wg.Done()
			lock, state, err := AcquireLock()
			if err != nil {
				t.Errorf("AcquireLock failed: %v", err)
				return
			}
			state.Accounts[h] = &AccountState{Status: Available}
			if err := Save(state); err != nil {
				lock.Release()
				t.Errorf("Save failed: %v", err)
				return
			}
			lock.Release()
		}(handle)
	}

	wg.Wait()

	// All accounts must be present — no lost writes.
	lock, state, err := AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	for i := 0; i < n; i++ {
		handle := fmt.Sprintf("account-%d", i)
		if state.Accounts[handle] == nil {
			t.Errorf("account %q was lost in concurrent writes", handle)
		}
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
