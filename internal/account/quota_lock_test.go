package account

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/fileutil"
)

// simulateQuotaRotation mimics what quota.AcquireLock + Save does:
// it takes an exclusive flock on quota.json.lock, reads quota.json,
// adds a marker account, and atomically writes it back.
func simulateQuotaRotation(t *testing.T, marker string) {
	t.Helper()

	dir := config.RuntimeDir()
	lockPath := filepath.Join(dir, "quota.json.lock")
	statePath := filepath.Join(dir, "quota.json")

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Errorf("simulateQuotaRotation: open lock: %v", err)
		return
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Errorf("simulateQuotaRotation: flock: %v", err)
		return
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Errorf("simulateQuotaRotation: read: %v", err)
		return
	}

	var state quotaState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Errorf("simulateQuotaRotation: unmarshal: %v", err)
		return
	}

	if state.Accounts == nil {
		state.Accounts = make(map[string]json.RawMessage)
	}
	state.Accounts[marker] = json.RawMessage(`{"status":"available"}`)

	updated, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Errorf("simulateQuotaRotation: marshal: %v", err)
		return
	}
	if err := fileutil.AtomicWrite(statePath, append(updated, '\n'), 0o644); err != nil {
		t.Errorf("simulateQuotaRotation: write: %v", err)
	}
}

// TestRemoveFromQuotaStateConcurrentNoLostWrites verifies that concurrent
// calls to removeFromQuotaState and simulated quota rotation (which use the
// same quota.json.lock) do not produce a lost-write: the persistent account
// must survive and the removed account must not be present.
func TestRemoveFromQuotaStateConcurrentNoLostWrites(t *testing.T) {
	setupTestHome(t)

	dir := config.RuntimeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write initial quota.json with two accounts.
	initial := quotaState{
		Accounts: map[string]json.RawMessage{
			"persistent": json.RawMessage(`{"status":"available"}`),
			"to-remove":  json.RawMessage(`{"status":"available"}`),
		},
	}
	data, err := json.MarshalIndent(initial, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "quota.json")
	if err := os.WriteFile(statePath, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n * 2)

	// Half the goroutines remove "to-remove" from quota state.
	for range n {
		go func() {
			defer wg.Done()
			removeFromQuotaState("to-remove")
		}()
	}

	// The other half simulate quota rotation: they add a marker account.
	for range n {
		go func() {
			defer wg.Done()
			simulateQuotaRotation(t, "rotation-marker")
		}()
	}

	wg.Wait()

	// Read final state and verify consistency.
	finalData, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var final quotaState
	if err := json.Unmarshal(finalData, &final); err != nil {
		t.Fatalf("final state is not valid JSON: %v", err)
	}

	// "to-remove" must not be present.
	if _, exists := final.Accounts["to-remove"]; exists {
		t.Error("account \"to-remove\" still present after concurrent removeFromQuotaState")
	}

	// "persistent" must still be present (not lost by a rotation write).
	if _, exists := final.Accounts["persistent"]; !exists {
		t.Error("account \"persistent\" was lost by a concurrent quota rotation write")
	}
}
