package integration

import (
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// TestForgeTTLClaimRelease verifies that stale MR claims (>30 min old) are
// automatically released back to "ready" by ReleaseStaleClaims — the TTL
// recovery mechanism documented in LOOP2_ACCEPTANCE.md §7.
//
// This is an integration test because it exercises the real SQLite store
// end-to-end: the timestamp backdating uses raw SQL and the release
// uses the production code path.
func TestForgeTTLClaimRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnvWithRepo(t)
	_ = gtHome

	worldStore, _ := openStores(t, "ttltest")

	// Create a writ.
	writID, err := worldStore.CreateWrit("TTL test writ", "TTL recovery test", "test", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Create an MR.
	mrID, err := worldStore.CreateMergeRequest(writID, "outpost/Agent/"+writID, 2)
	if err != nil {
		t.Fatalf("CreateMergeRequest: %v", err)
	}

	// Claim the MR.
	claimed, err := worldStore.ClaimMergeRequest("forge/ttltest")
	if err != nil {
		t.Fatalf("ClaimMergeRequest: %v", err)
	}
	if claimed == nil || claimed.ID != mrID {
		t.Fatalf("expected to claim MR %s, got %v", mrID, claimed)
	}

	// Verify it's in claimed state.
	mr, err := worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("GetMergeRequest: %v", err)
	}
	if mr.Phase != store.MRClaimed {
		t.Fatalf("MR phase = %q before backdate, want %q", mr.Phase, store.MRClaimed)
	}

	// Backdate claimed_at by 31 minutes to simulate a stale claim.
	staleTime := time.Now().UTC().Add(-31 * time.Minute).Format(time.RFC3339)
	if _, err := worldStore.DB().Exec(
		`UPDATE merge_requests SET claimed_at = ? WHERE id = ?`, staleTime, mrID,
	); err != nil {
		t.Fatalf("backdate claimed_at: %v", err)
	}

	// Run ReleaseStaleClaims with a 30-minute TTL — should release the backdated MR.
	released, err := worldStore.ReleaseStaleClaims(30 * time.Minute)
	if err != nil {
		t.Fatalf("ReleaseStaleClaims: %v", err)
	}
	if released != 1 {
		t.Errorf("ReleaseStaleClaims returned %d, want 1", released)
	}

	// Verify the MR is back to ready phase with cleared claim fields.
	mr, err = worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("GetMergeRequest after release: %v", err)
	}
	if mr.Phase != store.MRReady {
		t.Errorf("MR phase = %q, want %q", mr.Phase, store.MRReady)
	}
	if mr.ClaimedBy != "" {
		t.Errorf("MR claimed_by = %q, want empty", mr.ClaimedBy)
	}
	if mr.ClaimedAt != nil {
		t.Errorf("MR claimed_at = %v, want nil", mr.ClaimedAt)
	}
}

// TestForgeTTLClaimRelease_FreshClaimsNotReleased verifies that fresh claims
// (claimed within the TTL window) are not released by ReleaseStaleClaims.
func TestForgeTTLClaimRelease_FreshClaimsNotReleased(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, _ = setupTestEnvWithRepo(t)

	worldStore, _ := openStores(t, "ttlfreshtest")

	// Create writ and MR.
	writID, err := worldStore.CreateWrit("Fresh claim writ", "Fresh claim test", "test", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	mrID, err := worldStore.CreateMergeRequest(writID, "outpost/Agent/"+writID, 2)
	if err != nil {
		t.Fatalf("CreateMergeRequest: %v", err)
	}

	// Claim now (claimed_at = now, so it is fresh).
	claimed, err := worldStore.ClaimMergeRequest("forge/ttlfreshtest")
	if err != nil {
		t.Fatalf("ClaimMergeRequest: %v", err)
	}
	if claimed == nil || claimed.ID != mrID {
		t.Fatalf("expected to claim MR %s", mrID)
	}

	// ReleaseStaleClaims with 30-min TTL — fresh claim should NOT be released.
	released, err := worldStore.ReleaseStaleClaims(30 * time.Minute)
	if err != nil {
		t.Fatalf("ReleaseStaleClaims: %v", err)
	}
	if released != 0 {
		t.Errorf("ReleaseStaleClaims returned %d, want 0 (claim is fresh)", released)
	}

	// Verify MR is still claimed.
	mr, err := worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("GetMergeRequest: %v", err)
	}
	if mr.Phase != store.MRClaimed {
		t.Errorf("MR phase = %q, want %q (claim is fresh, should not be released)", mr.Phase, store.MRClaimed)
	}
}

