package quota

import (
	"testing"
	"time"
)

// TestApplyScanResultPreservesAssigned verifies that a scan with no rate-limit
// signal does not overwrite an Assigned account back to Available. Without
// this guard, ScanWorld would clear AssignedTo on an in-flight rotation,
// enabling double-assignment of the same credentials to two agents.
func TestApplyScanResultPreservesAssigned(t *testing.T) {
	state := &State{Accounts: make(map[string]*AccountState)}
	state.MarkAssigned("acct-1", "world/agent-1")

	applyScanResult(state, "acct-1", false, nil)

	got := state.Accounts["acct-1"]
	if got == nil {
		t.Fatal("account missing after scan")
	}
	if got.Status != Assigned {
		t.Errorf("status = %q, want %q", got.Status, Assigned)
	}
	if got.AssignedTo != "world/agent-1" {
		t.Errorf("assigned_to = %q, want %q", got.AssignedTo, "world/agent-1")
	}
}

// TestApplyScanResultPreservesLimited verifies the existing guard against
// overwriting a Limited account.
func TestApplyScanResultPreservesLimited(t *testing.T) {
	state := &State{Accounts: make(map[string]*AccountState)}
	resets := time.Now().Add(time.Hour).UTC()
	state.MarkLimited("acct-1", &resets)

	applyScanResult(state, "acct-1", false, nil)

	if state.Accounts["acct-1"].Status != Limited {
		t.Errorf("status = %q, want %q", state.Accounts["acct-1"].Status, Limited)
	}
}

// TestApplyScanResultMarksAvailable verifies that a previously-unknown or
// already-Available account is marked Available with last-used touched.
func TestApplyScanResultMarksAvailable(t *testing.T) {
	state := &State{Accounts: make(map[string]*AccountState)}

	applyScanResult(state, "acct-1", false, nil)

	got := state.Accounts["acct-1"]
	if got == nil {
		t.Fatal("account missing after scan")
	}
	if got.Status != Available {
		t.Errorf("status = %q, want %q", got.Status, Available)
	}
	if got.LastUsed == nil {
		t.Error("last_used not touched")
	}
}

// TestApplyScanResultLimitedOverridesAssigned verifies that detection of a
// rate limit always wins, even over an Assigned account: a hard rate limit
// must move the account to Limited so Rotate stops handing it out.
func TestApplyScanResultLimitedOverridesAssigned(t *testing.T) {
	state := &State{Accounts: make(map[string]*AccountState)}
	state.MarkAssigned("acct-1", "world/agent-1")

	resets := time.Now().Add(time.Hour).UTC()
	applyScanResult(state, "acct-1", true, &resets)

	got := state.Accounts["acct-1"]
	if got.Status != Limited {
		t.Errorf("status = %q, want %q", got.Status, Limited)
	}
	if got.AssignedTo != "" {
		t.Errorf("assigned_to = %q, want empty (cleared by MarkLimited)", got.AssignedTo)
	}
}

// TestApplyScanResultEmptyHandle verifies the no-op case.
func TestApplyScanResultEmptyHandle(t *testing.T) {
	state := &State{Accounts: make(map[string]*AccountState)}
	applyScanResult(state, "", false, nil)
	if len(state.Accounts) != 0 {
		t.Errorf("expected no accounts, got %d", len(state.Accounts))
	}
}
