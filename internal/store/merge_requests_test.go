package store

import (
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"
)

func TestCreateMergeRequest(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	// Create a writ first (FK dependency).
	itemID, err := s.CreateWrit("Test item", "A test writ", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create a merge request.
	mrID, err := s.CreateMergeRequest(itemID, "outpost/Toast/"+itemID, 2)
	if err != nil {
		t.Fatal(err)
	}

	// Verify ID format.
	pattern := regexp.MustCompile(`^mr-[0-9a-f]{16}$`)
	if !pattern.MatchString(mrID) {
		t.Fatalf("MR ID %q does not match pattern mr-[0-9a-f]{16}", mrID)
	}

	// Get it back and verify all fields.
	mr, err := s.GetMergeRequest(mrID)
	if err != nil {
		t.Fatal(err)
	}
	if mr.ID != mrID {
		t.Fatalf("expected ID %q, got %q", mrID, mr.ID)
	}
	if mr.WritID != itemID {
		t.Fatalf("expected writ_id %q, got %q", itemID, mr.WritID)
	}
	if mr.Branch != "outpost/Toast/"+itemID {
		t.Fatalf("expected branch 'outpost/Toast/%s', got %q", itemID, mr.Branch)
	}
	if mr.Phase != MRReady {
		t.Fatalf("expected phase 'ready', got %q", mr.Phase)
	}
	if mr.ClaimedBy != "" {
		t.Fatalf("expected empty claimed_by, got %q", mr.ClaimedBy)
	}
	if mr.ClaimedAt != nil {
		t.Fatalf("expected nil claimed_at, got %v", mr.ClaimedAt)
	}
	if mr.Attempts != 0 {
		t.Fatalf("expected 0 attempts, got %d", mr.Attempts)
	}
	if mr.Priority != 2 {
		t.Fatalf("expected priority 2, got %d", mr.Priority)
	}
	if mr.MergedAt != nil {
		t.Fatalf("expected nil merged_at, got %v", mr.MergedAt)
	}
}

func TestListMergeRequests(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	// Create writs (FK dependency).
	id1, _ := s.CreateWrit("Item 1", "", "autarch", 1, nil)
	id2, _ := s.CreateWrit("Item 2", "", "autarch", 2, nil)
	id3, _ := s.CreateWrit("Item 3", "", "autarch", 3, nil)

	// Create 3 MRs with different priorities.
	s.CreateMergeRequest(id1, "branch1", 1)
	s.CreateMergeRequest(id2, "branch2", 2)
	s.CreateMergeRequest(id3, "branch3", 3)

	// List all.
	all, err := s.ListMergeRequests(MRPhase(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 MRs, got %d", len(all))
	}
	// Verify ordered by priority.
	if all[0].Priority != 1 {
		t.Fatalf("expected first MR priority 1, got %d", all[0].Priority)
	}
	if all[2].Priority != 3 {
		t.Fatalf("expected last MR priority 3, got %d", all[2].Priority)
	}

	// List by phase.
	ready, err := s.ListMergeRequests(MRReady)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 3 {
		t.Fatalf("expected 3 ready MRs, got %d", len(ready))
	}

	claimed, err := s.ListMergeRequests(MRClaimed)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 0 {
		t.Fatalf("expected 0 claimed MRs, got %d", len(claimed))
	}
}

func TestClaimMergeRequest(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	id1, _ := s.CreateWrit("Item 1", "", "autarch", 1, nil)
	id2, _ := s.CreateWrit("Item 2", "", "autarch", 3, nil)

	// Create 2 MRs: priority 1 and priority 3.
	s.CreateMergeRequest(id1, "branch1", 1)
	s.CreateMergeRequest(id2, "branch2", 3)

	// Claim -> should get priority 1 first.
	mr, err := s.ClaimMergeRequest("forge/Forge")
	if err != nil {
		t.Fatal(err)
	}
	if mr == nil {
		t.Fatal("expected a claimed MR, got nil")
	}
	if mr.Priority != 1 {
		t.Fatalf("expected priority 1 MR, got priority %d", mr.Priority)
	}
	if mr.Phase != MRClaimed {
		t.Fatalf("expected phase 'claimed', got %q", mr.Phase)
	}
	if mr.ClaimedBy != "forge/Forge" {
		t.Fatalf("expected claimed_by 'forge/Forge', got %q", mr.ClaimedBy)
	}
	if mr.Attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", mr.Attempts)
	}
	if mr.ClaimedAt == nil {
		t.Fatal("expected claimed_at to be set")
	}

	// Claim again -> should get priority 3.
	mr2, err := s.ClaimMergeRequest("forge/Forge")
	if err != nil {
		t.Fatal(err)
	}
	if mr2 == nil {
		t.Fatal("expected a second claimed MR, got nil")
	}
	if mr2.Priority != 3 {
		t.Fatalf("expected priority 3 MR, got priority %d", mr2.Priority)
	}

	// Claim again -> nil (no more ready MRs).
	mr3, err := s.ClaimMergeRequest("forge/Forge")
	if err != nil {
		t.Fatal(err)
	}
	if mr3 != nil {
		t.Fatalf("expected nil when no ready MRs, got %+v", mr3)
	}
}

func TestClaimMergeRequestOrdering(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	// Create 3 writs.
	id1, _ := s.CreateWrit("Item 1", "", "autarch", 2, nil)
	id2, _ := s.CreateWrit("Item 2", "", "autarch", 2, nil)
	id3, _ := s.CreateWrit("Item 3", "", "autarch", 2, nil)

	// Create 3 MRs with same priority — claim order should be FIFO.
	mr1ID, _ := s.CreateMergeRequest(id1, "branch1", 2)
	time.Sleep(10 * time.Millisecond) // ensure different created_at
	s.CreateMergeRequest(id2, "branch2", 2)
	time.Sleep(10 * time.Millisecond)
	s.CreateMergeRequest(id3, "branch3", 2)

	// Claim -> should get oldest first.
	mr, err := s.ClaimMergeRequest("forge/Forge")
	if err != nil {
		t.Fatal(err)
	}
	if mr.ID != mr1ID {
		t.Fatalf("expected oldest MR %q first, got %q", mr1ID, mr.ID)
	}
}

func TestUpdateMergeRequestPhase(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	// Create and claim a MR, then update to "merged".
	itemID, _ := s.CreateWrit("Item 1", "", "autarch", 2, nil)
	mrID, _ := s.CreateMergeRequest(itemID, "branch1", 2)
	s.ClaimMergeRequest("forge/Forge")

	err := s.UpdateMergeRequestPhase(mrID, MRMerged)
	if err != nil {
		t.Fatal(err)
	}
	mr, err := s.GetMergeRequest(mrID)
	if err != nil {
		t.Fatal(err)
	}
	if mr.Phase != MRMerged {
		t.Fatalf("expected phase 'merged', got %q", mr.Phase)
	}
	if mr.MergedAt == nil {
		t.Fatal("expected merged_at to be set")
	}

	// Create another, claim, update to "failed" -> verify merged_at is nil.
	itemID2, _ := s.CreateWrit("Item 2", "", "autarch", 2, nil)
	mrID2, _ := s.CreateMergeRequest(itemID2, "branch2", 2)
	s.ClaimMergeRequest("forge/Forge")

	err = s.UpdateMergeRequestPhase(mrID2, MRFailed)
	if err != nil {
		t.Fatal(err)
	}
	mr2, err := s.GetMergeRequest(mrID2)
	if err != nil {
		t.Fatal(err)
	}
	if mr2.Phase != MRFailed {
		t.Fatalf("expected phase 'failed', got %q", mr2.Phase)
	}
	if mr2.MergedAt != nil {
		t.Fatalf("expected nil merged_at for failed MR, got %v", mr2.MergedAt)
	}

	// Create another, claim, update to "ready" -> verify claimed_by cleared.
	itemID3, _ := s.CreateWrit("Item 3", "", "autarch", 2, nil)
	mrID3, _ := s.CreateMergeRequest(itemID3, "branch3", 2)
	s.ClaimMergeRequest("forge/Forge")

	err = s.UpdateMergeRequestPhase(mrID3, MRReady)
	if err != nil {
		t.Fatal(err)
	}
	mr3, err := s.GetMergeRequest(mrID3)
	if err != nil {
		t.Fatal(err)
	}
	if mr3.Phase != MRReady {
		t.Fatalf("expected phase 'ready', got %q", mr3.Phase)
	}
	if mr3.ClaimedBy != "" {
		t.Fatalf("expected empty claimed_by after release, got %q", mr3.ClaimedBy)
	}
	if mr3.ClaimedAt != nil {
		t.Fatalf("expected nil claimed_at after release, got %v", mr3.ClaimedAt)
	}

	// Test invalid phase.
	err = s.UpdateMergeRequestPhase(mrID, "invalid")
	if err == nil {
		t.Fatal("expected error for invalid phase")
	}
}

func TestReleaseStaleClaims(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	itemID, _ := s.CreateWrit("Item 1", "", "autarch", 2, nil)
	mrID, _ := s.CreateMergeRequest(itemID, "branch1", 2)
	s.ClaimMergeRequest("forge/Forge")

	// ReleaseStaleClaims with 1-hour TTL -> 0 released (claim is fresh).
	released, err := s.ReleaseStaleClaims(1 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if released != 0 {
		t.Fatalf("expected 0 released, got %d", released)
	}

	// Manually set claimed_at to 31 minutes ago.
	staleTime := time.Now().UTC().Add(-31 * time.Minute).Format(time.RFC3339)
	_, err = s.db.Exec(`UPDATE merge_requests SET claimed_at = ? WHERE id = ?`, staleTime, mrID)
	if err != nil {
		t.Fatal(err)
	}

	// ReleaseStaleClaims with 30-minute TTL -> 1 released.
	released, err = s.ReleaseStaleClaims(30 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if released != 1 {
		t.Fatalf("expected 1 released, got %d", released)
	}

	// Verify MR is back to ready.
	mr, err := s.GetMergeRequest(mrID)
	if err != nil {
		t.Fatal(err)
	}
	if mr.Phase != MRReady {
		t.Fatalf("expected phase 'ready' after release, got %q", mr.Phase)
	}
	if mr.ClaimedBy != "" {
		t.Fatalf("expected empty claimed_by after release, got %q", mr.ClaimedBy)
	}
	if mr.ClaimedAt != nil {
		t.Fatalf("expected nil claimed_at after release, got %v", mr.ClaimedAt)
	}
}

func TestReleaseStaleLeavesRecentClaims(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	itemID1, _ := s.CreateWrit("Item 1", "", "autarch", 2, nil)
	itemID2, _ := s.CreateWrit("Item 2", "", "autarch", 2, nil)
	mrID1, _ := s.CreateMergeRequest(itemID1, "branch1", 2)
	s.CreateMergeRequest(itemID2, "branch2", 2)

	// Claim both.
	s.ClaimMergeRequest("forge/Forge")
	s.ClaimMergeRequest("forge/Forge")

	// Set one claimed_at to 31 minutes ago, leave other fresh.
	staleTime := time.Now().UTC().Add(-31 * time.Minute).Format(time.RFC3339)
	_, err := s.db.Exec(`UPDATE merge_requests SET claimed_at = ? WHERE id = ?`, staleTime, mrID1)
	if err != nil {
		t.Fatal(err)
	}

	// ReleaseStaleClaims(30min) -> 1 released.
	released, err := s.ReleaseStaleClaims(30 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if released != 1 {
		t.Fatalf("expected 1 released, got %d", released)
	}

	// Verify only the stale one was released.
	mr1, _ := s.GetMergeRequest(mrID1)
	if mr1.Phase != MRReady {
		t.Fatalf("expected stale MR to be ready, got %q", mr1.Phase)
	}

	// The fresh one should still be claimed.
	claimed, _ := s.ListMergeRequests(MRClaimed)
	if len(claimed) != 1 {
		t.Fatalf("expected 1 still-claimed MR, got %d", len(claimed))
	}
}

func TestListMergeRequestsByWrit(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	id1, _ := s.CreateWrit("Item 1", "", "autarch", 2, nil)
	id2, _ := s.CreateWrit("Item 2", "", "autarch", 2, nil)

	// Create MRs: 2 for id1, 1 for id2.
	mr1ID, _ := s.CreateMergeRequest(id1, "branch1a", 2)
	mr2ID, _ := s.CreateMergeRequest(id1, "branch1b", 2)
	s.CreateMergeRequest(id2, "branch2", 2)

	// Mark one as failed.
	s.ClaimMergeRequest("forge/Forge")
	s.UpdateMergeRequestPhase(mr1ID, MRFailed)

	// List all for id1.
	mrs, err := s.ListMergeRequestsByWrit(id1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(mrs) != 2 {
		t.Fatalf("expected 2 MRs for writ, got %d", len(mrs))
	}

	// List only failed for id1.
	failed, err := s.ListMergeRequestsByWrit(id1, MRFailed)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 {
		t.Fatalf("expected 1 failed MR, got %d", len(failed))
	}
	if failed[0].ID != mr1ID {
		t.Errorf("failed MR ID = %q, want %q", failed[0].ID, mr1ID)
	}

	// List only ready for id1.
	ready, err := s.ListMergeRequestsByWrit(id1, MRReady)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready MR, got %d", len(ready))
	}
	if ready[0].ID != mr2ID {
		t.Errorf("ready MR ID = %q, want %q", ready[0].ID, mr2ID)
	}

	// List for id2 — should only get 1.
	mrs2, err := s.ListMergeRequestsByWrit(id2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(mrs2) != 1 {
		t.Fatalf("expected 1 MR for writ 2, got %d", len(mrs2))
	}
}

func TestSupersededPhase(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	itemID, _ := s.CreateWrit("Item 1", "", "autarch", 2, nil)
	mrID, _ := s.CreateMergeRequest(itemID, "branch1", 2)

	// Must go through valid path: ready → claimed → failed → superseded.
	s.ClaimMergeRequest("forge/Forge")
	if err := s.UpdateMergeRequestPhase(mrID, MRFailed); err != nil {
		t.Fatalf("UpdateMergeRequestPhase(failed) error: %v", err)
	}
	if err := s.UpdateMergeRequestPhase(mrID, MRSuperseded); err != nil {
		t.Fatalf("UpdateMergeRequestPhase(superseded) error: %v", err)
	}

	mr, err := s.GetMergeRequest(mrID)
	if err != nil {
		t.Fatal(err)
	}
	if mr.Phase != MRSuperseded {
		t.Errorf("phase = %q, want 'superseded'", mr.Phase)
	}
}

func TestGetMergeRequestNotFound(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	_, err := s.GetMergeRequest("mr-nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent merge request")
	}
	expected := `merge request "mr-nonexist": not found`
	if err.Error() != expected {
		t.Fatalf("expected error %q, got %q", expected, err.Error())
	}
}

func TestBlockAndUnblockMergeRequest(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	itemID, _ := s.CreateWrit("Item 1", "", "autarch", 2, nil)
	mrID, _ := s.CreateMergeRequest(itemID, "branch1", 2)

	// Block the MR.
	if err := s.BlockMergeRequest(mrID, "sol-blocker1"); err != nil {
		t.Fatalf("BlockMergeRequest() error: %v", err)
	}

	// Verify blocked_by is set and phase is ready.
	mr, err := s.GetMergeRequest(mrID)
	if err != nil {
		t.Fatal(err)
	}
	if mr.BlockedBy != "sol-blocker1" {
		t.Errorf("blocked_by = %q, want %q", mr.BlockedBy, "sol-blocker1")
	}
	if mr.Phase != MRReady {
		t.Errorf("phase = %q, want %q", mr.Phase, MRReady)
	}

	// Unblock the MR.
	if err := s.UnblockMergeRequest(mrID); err != nil {
		t.Fatalf("UnblockMergeRequest() error: %v", err)
	}

	// Verify blocked_by is cleared.
	mr, err = s.GetMergeRequest(mrID)
	if err != nil {
		t.Fatal(err)
	}
	if mr.BlockedBy != "" {
		t.Errorf("blocked_by after unblock = %q, want empty", mr.BlockedBy)
	}
	if mr.Phase != MRReady {
		t.Errorf("phase after unblock = %q, want %q", mr.Phase, MRReady)
	}
}

func TestClaimSkipsBlockedMRs(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	id1, _ := s.CreateWrit("Item 1", "", "autarch", 1, nil)
	id2, _ := s.CreateWrit("Item 2", "", "autarch", 2, nil)
	mr1ID, _ := s.CreateMergeRequest(id1, "branch1", 1) // Higher priority.
	mr2ID, _ := s.CreateMergeRequest(id2, "branch2", 2)

	// Block the higher-priority MR.
	s.BlockMergeRequest(mr1ID, "sol-blocker1")

	// Claim should skip the blocked MR and get the second one.
	mr, err := s.ClaimMergeRequest("forge/Forge")
	if err != nil {
		t.Fatal(err)
	}
	if mr == nil {
		t.Fatal("expected a claimed MR, got nil")
	}
	if mr.ID != mr2ID {
		t.Errorf("claimed MR = %q, want %q (should skip blocked)", mr.ID, mr2ID)
	}
}

func TestFindMergeRequestByBlocker(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	itemID, _ := s.CreateWrit("Item 1", "", "autarch", 2, nil)
	mrID, _ := s.CreateMergeRequest(itemID, "branch1", 2)

	// No blocker yet.
	result, err := s.FindMergeRequestByBlocker("sol-blocker1")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatalf("expected nil, got %+v", result)
	}

	// Block the MR.
	s.BlockMergeRequest(mrID, "sol-blocker1")

	// Now find it.
	result, err = s.FindMergeRequestByBlocker("sol-blocker1")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected a MR, got nil")
	}
	if result.ID != mrID {
		t.Errorf("found MR = %q, want %q", result.ID, mrID)
	}
	if result.BlockedBy != "sol-blocker1" {
		t.Errorf("blocked_by = %q, want %q", result.BlockedBy, "sol-blocker1")
	}
}

func TestV3Migration(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	// Verify the schema version is 5 (V3 migration was applied as part of V5).
	var v int
	if err := s.DB().QueryRow("SELECT version FROM schema_version").Scan(&v); err != nil {
		t.Fatalf("failed to get schema version: %v", err)
	}
	if v != CurrentWorldSchema {
		t.Errorf("schema version = %d, want %d", v, CurrentWorldSchema)
	}

	// Verify blocked_by column exists.
	itemID, _ := s.CreateWrit("Test", "", "autarch", 2, nil)
	mrID, _ := s.CreateMergeRequest(itemID, "branch1", 2)

	// Should be able to block/unblock without error.
	if err := s.BlockMergeRequest(mrID, "sol-test"); err != nil {
		t.Fatalf("BlockMergeRequest failed (blocked_by column missing?): %v", err)
	}
	if err := s.UnblockMergeRequest(mrID); err != nil {
		t.Fatalf("UnblockMergeRequest failed: %v", err)
	}
}

func TestResetMergeRequestForRetry(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	itemID, _ := s.CreateWrit("Item 1", "", "autarch", 2, nil)
	mrID, _ := s.CreateMergeRequest(itemID, "branch1", 2)

	// Claim the MR to bump attempts and set claimed_by/claimed_at.
	s.ClaimMergeRequest("forge/Forge")

	// Mark it as failed (simulates forge giving up after max attempts).
	if err := s.UpdateMergeRequestPhase(mrID, MRFailed); err != nil {
		t.Fatal(err)
	}

	// Block it (simulates conflict-resolution blocker being set before failure).
	// Use raw SQL since BlockMergeRequest also resets phase.
	if _, err := s.db.Exec(`UPDATE merge_requests SET blocked_by = 'sol-blocker1' WHERE id = ?`, mrID); err != nil {
		t.Fatal(err)
	}

	// Verify pre-conditions.
	mr, _ := s.GetMergeRequest(mrID)
	if mr.Phase != MRFailed {
		t.Fatalf("expected phase 'failed', got %q", mr.Phase)
	}
	if mr.Attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", mr.Attempts)
	}

	// Reset for retry.
	if err := s.ResetMergeRequestForRetry(mrID); err != nil {
		t.Fatalf("ResetMergeRequestForRetry() error: %v", err)
	}

	// Verify all fields are reset.
	mr, err := s.GetMergeRequest(mrID)
	if err != nil {
		t.Fatal(err)
	}
	if mr.Phase != MRReady {
		t.Errorf("phase = %q, want 'ready'", mr.Phase)
	}
	if mr.Attempts != 0 {
		t.Errorf("attempts = %d, want 0", mr.Attempts)
	}
	if mr.BlockedBy != "" {
		t.Errorf("blocked_by = %q, want empty", mr.BlockedBy)
	}
	if mr.ClaimedBy != "" {
		t.Errorf("claimed_by = %q, want empty", mr.ClaimedBy)
	}
	if mr.ClaimedAt != nil {
		t.Errorf("claimed_at = %v, want nil", mr.ClaimedAt)
	}
}

func TestListBlockedMergeRequests(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	// Create writs for our MRs.
	blockerID, err := s.CreateWrit("Resolve conflict", "Fix the conflict", "forge", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	writ1ID, err := s.CreateWrit("Feature A", "Implement A", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	writ2ID, err := s.CreateWrit("Feature B", "Implement B", "autarch", 3, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create MRs — one blocked, two not.
	mr1ID, err := s.CreateMergeRequest(writ1ID, "outpost/Toast/"+writ1ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.BlockMergeRequest(mr1ID, blockerID); err != nil {
		t.Fatal(err)
	}

	// Unblocked MR.
	_, err = s.CreateMergeRequest(writ2ID, "outpost/Sage/"+writ2ID, 3)
	if err != nil {
		t.Fatal(err)
	}

	// List blocked — should only return the blocked one.
	blocked, err := s.ListBlockedMergeRequests()
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked) != 1 {
		t.Fatalf("expected 1 blocked MR, got %d", len(blocked))
	}
	if blocked[0].ID != mr1ID {
		t.Errorf("blocked MR ID = %q, want %q", blocked[0].ID, mr1ID)
	}
	if blocked[0].BlockedBy != blockerID {
		t.Errorf("blocked_by = %q, want %q", blocked[0].BlockedBy, blockerID)
	}
}

func TestListBlockedMergeRequests_Empty(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	// Create an unblocked MR.
	writID, err := s.CreateWrit("Feature", "Do something", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateMergeRequest(writID, "outpost/Toast/"+writID, 2)
	if err != nil {
		t.Fatal(err)
	}

	// List blocked — should be empty.
	blocked, err := s.ListBlockedMergeRequests()
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked) != 0 {
		t.Fatalf("expected 0 blocked MRs, got %d", len(blocked))
	}
}

func TestResetMergeRequestForRetryNotFound(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	err := s.ResetMergeRequestForRetry("mr-nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent MR")
	}
	expected := `merge request "mr-nonexist": not found`
	if err.Error() != expected {
		t.Fatalf("error = %q, want %q", err.Error(), expected)
	}
}

func TestPhaseTransitionGuards(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	t.Run("valid transitions", func(t *testing.T) {
		// ready → claimed → merged
		itemID, _ := s.CreateWrit("Merge path", "", "autarch", 2, nil)
		mrID, _ := s.CreateMergeRequest(itemID, "branch-merge", 2)
		s.ClaimMergeRequest("forge/Forge")
		if err := s.UpdateMergeRequestPhase(mrID, MRMerged); err != nil {
			t.Fatalf("claimed→merged: unexpected error: %v", err)
		}

		// ready → claimed → failed → superseded
		itemID2, _ := s.CreateWrit("Fail path", "", "autarch", 2, nil)
		mrID2, _ := s.CreateMergeRequest(itemID2, "branch-fail", 2)
		s.ClaimMergeRequest("forge/Forge")
		if err := s.UpdateMergeRequestPhase(mrID2, MRFailed); err != nil {
			t.Fatalf("claimed→failed: unexpected error: %v", err)
		}
		if err := s.UpdateMergeRequestPhase(mrID2, MRSuperseded); err != nil {
			t.Fatalf("failed→superseded: unexpected error: %v", err)
		}

		// ready → claimed → ready (release)
		itemID3, _ := s.CreateWrit("Release path", "", "autarch", 2, nil)
		mrID3, _ := s.CreateMergeRequest(itemID3, "branch-release", 2)
		s.ClaimMergeRequest("forge/Forge")
		if err := s.UpdateMergeRequestPhase(mrID3, MRReady); err != nil {
			t.Fatalf("claimed→ready: unexpected error: %v", err)
		}
	})

	t.Run("idempotent transitions", func(t *testing.T) {
		itemID, _ := s.CreateWrit("Idempotent", "", "autarch", 2, nil)
		mrID, _ := s.CreateMergeRequest(itemID, "branch-idem", 2)

		// ready → ready (no-op)
		if err := s.UpdateMergeRequestPhase(mrID, MRReady); err != nil {
			t.Fatalf("ready→ready: unexpected error: %v", err)
		}

		// claimed → claimed (no-op)
		s.ClaimMergeRequest("forge/Forge")
		if err := s.UpdateMergeRequestPhase(mrID, MRClaimed); err != nil {
			t.Fatalf("claimed→claimed: unexpected error: %v", err)
		}

		// failed → failed (no-op)
		if err := s.UpdateMergeRequestPhase(mrID, MRFailed); err != nil {
			t.Fatalf("claimed→failed: unexpected error: %v", err)
		}
		if err := s.UpdateMergeRequestPhase(mrID, MRFailed); err != nil {
			t.Fatalf("failed→failed: unexpected error: %v", err)
		}

		// merged → merged (no-op)
		itemID2, _ := s.CreateWrit("Idempotent2", "", "autarch", 2, nil)
		mrID2, _ := s.CreateMergeRequest(itemID2, "branch-idem2", 2)
		s.ClaimMergeRequest("forge/Forge")
		s.UpdateMergeRequestPhase(mrID2, MRMerged)
		if err := s.UpdateMergeRequestPhase(mrID2, MRMerged); err != nil {
			t.Fatalf("merged→merged: unexpected error: %v", err)
		}
	})

	t.Run("invalid transitions from terminal states", func(t *testing.T) {
		// merged → ready
		itemID, _ := s.CreateWrit("Terminal merged", "", "autarch", 2, nil)
		mrID, _ := s.CreateMergeRequest(itemID, "branch-term-m", 2)
		s.ClaimMergeRequest("forge/Forge")
		s.UpdateMergeRequestPhase(mrID, MRMerged)

		for _, target := range []MRPhase{MRReady, MRClaimed, MRFailed, MRSuperseded} {
			err := s.UpdateMergeRequestPhase(mrID, target)
			if err == nil {
				t.Fatalf("merged→%s: expected error, got nil", target)
			}
			if !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("merged→%s: expected ErrInvalidTransition, got: %v", target, err)
			}
		}

		// superseded → anything
		itemID2, _ := s.CreateWrit("Terminal superseded", "", "autarch", 2, nil)
		mrID2, _ := s.CreateMergeRequest(itemID2, "branch-term-s", 2)
		s.ClaimMergeRequest("forge/Forge")
		s.UpdateMergeRequestPhase(mrID2, MRFailed)
		s.UpdateMergeRequestPhase(mrID2, MRSuperseded)

		for _, target := range []MRPhase{MRReady, MRClaimed, MRMerged, MRFailed} {
			err := s.UpdateMergeRequestPhase(mrID2, target)
			if err == nil {
				t.Fatalf("superseded→%s: expected error, got nil", target)
			}
			if !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("superseded→%s: expected ErrInvalidTransition, got: %v", target, err)
			}
		}
	})

	t.Run("invalid transitions skip steps", func(t *testing.T) {
		// ready → merged (skips claimed)
		itemID, _ := s.CreateWrit("Skip merged", "", "autarch", 2, nil)
		mrID, _ := s.CreateMergeRequest(itemID, "branch-skip-m", 2)
		err := s.UpdateMergeRequestPhase(mrID, MRMerged)
		if err == nil {
			t.Fatal("ready→merged: expected error, got nil")
		}
		if !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("ready→merged: expected ErrInvalidTransition, got: %v", err)
		}

		// ready → failed (skips claimed)
		err = s.UpdateMergeRequestPhase(mrID, MRFailed)
		if err == nil {
			t.Fatal("ready→failed: expected error, got nil")
		}
		if !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("ready→failed: expected ErrInvalidTransition, got: %v", err)
		}

		// ready → superseded (not allowed)
		err = s.UpdateMergeRequestPhase(mrID, MRSuperseded)
		if err == nil {
			t.Fatal("ready→superseded: expected error, got nil")
		}
		if !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("ready→superseded: expected ErrInvalidTransition, got: %v", err)
		}

		// failed → ready (must use ResetMergeRequestForRetry)
		itemID2, _ := s.CreateWrit("Skip ready", "", "autarch", 2, nil)
		mrID2, _ := s.CreateMergeRequest(itemID2, "branch-skip-r", 2)
		// Claim this specific MR by ID to ensure we're operating on the right one.
		if err := s.UpdateMergeRequestPhase(mrID2, "claimed"); err != nil {
			t.Fatalf("ready→claimed for mrID2: %v", err)
		}
		if err := s.UpdateMergeRequestPhase(mrID2, MRFailed); err != nil {
			t.Fatalf("claimed→failed for mrID2: %v", err)
		}

		err = s.UpdateMergeRequestPhase(mrID2, "ready")
		if err == nil {
			t.Fatal("failed→ready: expected error, got nil")
		}
		if !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("failed→ready: expected ErrInvalidTransition, got: %v", err)
		}

		// failed → claimed (must go through ready first)
		err = s.UpdateMergeRequestPhase(mrID2, "claimed")
		if err == nil {
			t.Fatal("failed→claimed: expected error, got nil")
		}
		if !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("failed→claimed: expected ErrInvalidTransition, got: %v", err)
		}
	})
}

func TestClaimSkipsCaravanBlockedMRs(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	id1, _ := s.CreateWrit("Item 1", "", "autarch", 1, nil)
	id2, _ := s.CreateWrit("Item 2", "", "autarch", 2, nil)
	mr1ID, _ := s.CreateMergeRequest(id1, "branch1", 1) // Higher priority
	mr2ID, _ := s.CreateMergeRequest(id2, "branch2", 2)

	// Block the higher-priority MR with the caravan sentinel.
	if err := s.BlockMergeRequest(mr1ID, CaravanBlockedSentinel); err != nil {
		t.Fatalf("BlockMergeRequest(caravan-blocked) error: %v", err)
	}

	// Claim should skip the caravan-blocked MR and get the second one.
	mr, err := s.ClaimMergeRequest("forge/Forge")
	if err != nil {
		t.Fatal(err)
	}
	if mr == nil {
		t.Fatal("expected a claimed MR, got nil")
	}
	if mr.ID != mr2ID {
		t.Errorf("claimed MR = %q, want %q (should skip caravan-blocked)", mr.ID, mr2ID)
	}

	// Verify the caravan-blocked MR is still blocked.
	blocked, err := s.GetMergeRequest(mr1ID)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.BlockedBy != CaravanBlockedSentinel {
		t.Errorf("blocked_by = %q, want %q", blocked.BlockedBy, CaravanBlockedSentinel)
	}
	if blocked.Phase != "ready" {
		t.Errorf("phase = %q, want 'ready'", blocked.Phase)
	}

	// Try to claim again — nothing left, caravan-blocked MR should NOT be claimed.
	mr3, err := s.ClaimMergeRequest("forge/Forge")
	if err != nil {
		t.Fatal(err)
	}
	if mr3 != nil {
		t.Fatalf("expected nil when only caravan-blocked MR remains, got %+v", mr3)
	}
}

func TestPhaseTransitionNotFound(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	err := s.UpdateMergeRequestPhase("mr-nonexist", "ready")
	if err == nil {
		t.Fatal("expected error for nonexistent MR")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestSupersedeFailedMRsForWrit(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	writID, _ := s.CreateWrit("Item 1", "", "autarch", 2, nil)

	// Create 3 MRs for the same writ.
	mr1ID, _ := s.CreateMergeRequest(writID, "branch1", 2)
	mr2ID, _ := s.CreateMergeRequest(writID, "branch2", 2)
	mr3ID, _ := s.CreateMergeRequest(writID, "branch3", 2)

	// Move mr1 and mr2 to failed (ready → claimed → failed).
	s.ClaimMergeRequest("forge/Forge")
	s.UpdateMergeRequestPhase(mr1ID, MRFailed)
	s.ClaimMergeRequest("forge/Forge")
	s.UpdateMergeRequestPhase(mr2ID, "failed")
	// mr3 stays ready.

	// Supersede failed MRs.
	ids, err := s.SupersedeFailedMRsForWrit(writID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 superseded IDs, got %d", len(ids))
	}

	// Verify both are superseded.
	mr1, _ := s.GetMergeRequest(mr1ID)
	if mr1.Phase != "superseded" {
		t.Fatalf("expected mr1 phase 'superseded', got %q", mr1.Phase)
	}
	mr2, _ := s.GetMergeRequest(mr2ID)
	if mr2.Phase != "superseded" {
		t.Fatalf("expected mr2 phase 'superseded', got %q", mr2.Phase)
	}

	// mr3 should still be ready.
	mr3, _ := s.GetMergeRequest(mr3ID)
	if mr3.Phase != MRReady {
		t.Fatalf("expected mr3 phase 'ready', got %q", mr3.Phase)
	}
}

func TestSupersedeFailedMRsForWritNoFailed(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	writID, _ := s.CreateWrit("Item 1", "", "autarch", 2, nil)
	s.CreateMergeRequest(writID, "branch1", 2)

	// No failed MRs — should return nil.
	ids, err := s.SupersedeFailedMRsForWrit(writID)
	if err != nil {
		t.Fatal(err)
	}
	if ids != nil {
		t.Fatalf("expected nil IDs for no failed MRs, got %v", ids)
	}
}

func TestSupersedeFailedMRsForWritNonexistentWrit(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	// Nonexistent writ — should return nil (no failed MRs found).
	ids, err := s.SupersedeFailedMRsForWrit("sol-nonexist")
	if err != nil {
		t.Fatal(err)
	}
	if ids != nil {
		t.Fatalf("expected nil IDs for nonexistent writ, got %v", ids)
	}
}

func TestBlockMergeRequestNotFound(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	err := s.BlockMergeRequest("mr-nonexist", "sol-blocker1")
	if err == nil {
		t.Fatal("expected error for nonexistent merge request")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestUnblockMergeRequestNotFound(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	err := s.UnblockMergeRequest("mr-nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent merge request")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestAttemptsIncrementAcrossRetries(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	writID, _ := s.CreateWrit("Item 1", "", "autarch", 2, nil)
	mrID, _ := s.CreateMergeRequest(writID, "branch1", 2)

	// First claim → attempts = 1.
	mr, err := s.ClaimMergeRequest("forge/Forge")
	if err != nil {
		t.Fatal(err)
	}
	if mr == nil {
		t.Fatal("expected a claimed MR, got nil")
	}
	if mr.Attempts != 1 {
		t.Fatalf("after 1st claim: attempts = %d, want 1", mr.Attempts)
	}

	// Release back to ready (simulating forge returning it for retry).
	if err := s.UpdateMergeRequestPhase(mrID, MRReady); err != nil {
		t.Fatal(err)
	}

	// Second claim → attempts = 2 (counter preserved across release).
	mr2, err := s.ClaimMergeRequest("forge/Forge")
	if err != nil {
		t.Fatal(err)
	}
	if mr2 == nil {
		t.Fatal("expected second claim to succeed")
	}
	if mr2.ID != mrID {
		t.Fatalf("expected same MR %q, got %q", mrID, mr2.ID)
	}
	if mr2.Attempts != 2 {
		t.Fatalf("after 2nd claim: attempts = %d, want 2", mr2.Attempts)
	}

	// Release and claim a third time → attempts = 3.
	if err := s.UpdateMergeRequestPhase(mrID, MRReady); err != nil {
		t.Fatal(err)
	}
	mr3, err := s.ClaimMergeRequest("forge/Forge")
	if err != nil {
		t.Fatal(err)
	}
	if mr3 == nil {
		t.Fatal("expected third claim to succeed")
	}
	if mr3.Attempts != 3 {
		t.Fatalf("after 3rd claim: attempts = %d, want 3", mr3.Attempts)
	}

	// ResetMergeRequestForRetry clears attempts back to 0.
	if err := s.UpdateMergeRequestPhase(mrID, MRFailed); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetMergeRequestForRetry(mrID); err != nil {
		t.Fatal(err)
	}
	mr4, err := s.GetMergeRequest(mrID)
	if err != nil {
		t.Fatal(err)
	}
	if mr4.Attempts != 0 {
		t.Fatalf("after reset: attempts = %d, want 0", mr4.Attempts)
	}
}

// TestPhaseTransitionMatrix is a table-driven test covering all valid and
// invalid phase transitions for merge requests.
func TestPhaseTransitionMatrix(t *testing.T) {
	t.Parallel()

	type testCase struct {
		from    MRPhase
		to      MRPhase
		wantErr bool
	}

	cases := []testCase{
		// Idempotent (same → same): always valid.
		{MRReady, MRReady, false},
		{MRClaimed, MRClaimed, false},
		{MRMerged, MRMerged, false},
		{MRFailed, MRFailed, false},
		{MRSuperseded, MRSuperseded, false},

		// Valid forward transitions.
		{MRReady, MRClaimed, false},
		{MRClaimed, MRReady, false},
		{MRClaimed, MRMerged, false},
		{MRClaimed, MRFailed, false},
		{MRFailed, MRSuperseded, false},

		// Invalid: skip steps.
		{MRReady, MRMerged, true},
		{MRReady, MRFailed, true},
		{MRReady, MRSuperseded, true},
		{MRClaimed, MRSuperseded, true},

		// Invalid: transitions from terminal state merged.
		{MRMerged, MRReady, true},
		{MRMerged, MRClaimed, true},
		{MRMerged, MRFailed, true},
		{MRMerged, MRSuperseded, true},

		// Invalid: transitions from terminal state superseded.
		{MRSuperseded, MRReady, true},
		{MRSuperseded, MRClaimed, true},
		{MRSuperseded, MRMerged, true},
		{MRSuperseded, MRFailed, true},

		// Invalid: backward from failed (must use ResetMergeRequestForRetry).
		{MRFailed, MRReady, true},
		{MRFailed, MRClaimed, true},
		{MRFailed, MRMerged, true},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s to %s", tc.from, tc.to), func(t *testing.T) {
			t.Parallel()
			s := setupWorld(t)

			writID, _ := s.CreateWrit("item", "", "autarch", 2, nil)
			mrID, _ := s.CreateMergeRequest(writID, "branch", 2)

			// Advance MR to the desired starting phase.
			switch tc.from {
			case MRReady:
				// Initial state — no action needed.
			case MRClaimed:
				if _, err := s.ClaimMergeRequest("forge/Forge"); err != nil {
					t.Fatalf("setup: ClaimMergeRequest failed: %v", err)
				}
			case MRMerged:
				if _, err := s.ClaimMergeRequest("forge/Forge"); err != nil {
					t.Fatalf("setup: ClaimMergeRequest failed: %v", err)
				}
				if err := s.UpdateMergeRequestPhase(mrID, MRMerged); err != nil {
					t.Fatalf("setup: UpdateMergeRequestPhase(merged) failed: %v", err)
				}
			case MRFailed:
				if _, err := s.ClaimMergeRequest("forge/Forge"); err != nil {
					t.Fatalf("setup: ClaimMergeRequest failed: %v", err)
				}
				if err := s.UpdateMergeRequestPhase(mrID, MRFailed); err != nil {
					t.Fatalf("setup: UpdateMergeRequestPhase(failed) failed: %v", err)
				}
			case MRSuperseded:
				if _, err := s.ClaimMergeRequest("forge/Forge"); err != nil {
					t.Fatalf("setup: ClaimMergeRequest failed: %v", err)
				}
				if err := s.UpdateMergeRequestPhase(mrID, MRFailed); err != nil {
					t.Fatalf("setup: UpdateMergeRequestPhase(failed) failed: %v", err)
				}
				if err := s.UpdateMergeRequestPhase(mrID, MRSuperseded); err != nil {
					t.Fatalf("setup: UpdateMergeRequestPhase(superseded) failed: %v", err)
				}
			}

			err := s.UpdateMergeRequestPhase(mrID, tc.to)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %s → %s, got nil", tc.from, tc.to)
				}
				if !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("expected ErrInvalidTransition for %s → %s, got: %v", tc.from, tc.to, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for %s → %s: %v", tc.from, tc.to, err)
				}
			}
		})
	}
}
