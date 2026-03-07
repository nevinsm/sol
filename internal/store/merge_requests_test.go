package store

import (
	"regexp"
	"testing"
	"time"
)

func TestCreateMergeRequest(t *testing.T) {
	s := setupWorld(t)

	// Create a writ first (FK dependency).
	itemID, err := s.CreateWrit("Test item", "A test writ", "operator", 2, nil)
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
	if mr.Phase != "ready" {
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
	s := setupWorld(t)

	// Create writs (FK dependency).
	id1, _ := s.CreateWrit("Item 1", "", "operator", 1, nil)
	id2, _ := s.CreateWrit("Item 2", "", "operator", 2, nil)
	id3, _ := s.CreateWrit("Item 3", "", "operator", 3, nil)

	// Create 3 MRs with different priorities.
	s.CreateMergeRequest(id1, "branch1", 1)
	s.CreateMergeRequest(id2, "branch2", 2)
	s.CreateMergeRequest(id3, "branch3", 3)

	// List all.
	all, err := s.ListMergeRequests("")
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
	ready, err := s.ListMergeRequests("ready")
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 3 {
		t.Fatalf("expected 3 ready MRs, got %d", len(ready))
	}

	claimed, err := s.ListMergeRequests("claimed")
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 0 {
		t.Fatalf("expected 0 claimed MRs, got %d", len(claimed))
	}
}

func TestClaimMergeRequest(t *testing.T) {
	s := setupWorld(t)

	id1, _ := s.CreateWrit("Item 1", "", "operator", 1, nil)
	id2, _ := s.CreateWrit("Item 2", "", "operator", 3, nil)

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
	if mr.Phase != "claimed" {
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
	s := setupWorld(t)

	// Create 3 writs.
	id1, _ := s.CreateWrit("Item 1", "", "operator", 2, nil)
	id2, _ := s.CreateWrit("Item 2", "", "operator", 2, nil)
	id3, _ := s.CreateWrit("Item 3", "", "operator", 2, nil)

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
	s := setupWorld(t)

	// Create and claim a MR, then update to "merged".
	itemID, _ := s.CreateWrit("Item 1", "", "operator", 2, nil)
	mrID, _ := s.CreateMergeRequest(itemID, "branch1", 2)
	s.ClaimMergeRequest("forge/Forge")

	err := s.UpdateMergeRequestPhase(mrID, "merged")
	if err != nil {
		t.Fatal(err)
	}
	mr, err := s.GetMergeRequest(mrID)
	if err != nil {
		t.Fatal(err)
	}
	if mr.Phase != "merged" {
		t.Fatalf("expected phase 'merged', got %q", mr.Phase)
	}
	if mr.MergedAt == nil {
		t.Fatal("expected merged_at to be set")
	}

	// Create another, claim, update to "failed" -> verify merged_at is nil.
	itemID2, _ := s.CreateWrit("Item 2", "", "operator", 2, nil)
	mrID2, _ := s.CreateMergeRequest(itemID2, "branch2", 2)
	s.ClaimMergeRequest("forge/Forge")

	err = s.UpdateMergeRequestPhase(mrID2, "failed")
	if err != nil {
		t.Fatal(err)
	}
	mr2, err := s.GetMergeRequest(mrID2)
	if err != nil {
		t.Fatal(err)
	}
	if mr2.Phase != "failed" {
		t.Fatalf("expected phase 'failed', got %q", mr2.Phase)
	}
	if mr2.MergedAt != nil {
		t.Fatalf("expected nil merged_at for failed MR, got %v", mr2.MergedAt)
	}

	// Create another, claim, update to "ready" -> verify claimed_by cleared.
	itemID3, _ := s.CreateWrit("Item 3", "", "operator", 2, nil)
	mrID3, _ := s.CreateMergeRequest(itemID3, "branch3", 2)
	s.ClaimMergeRequest("forge/Forge")

	err = s.UpdateMergeRequestPhase(mrID3, "ready")
	if err != nil {
		t.Fatal(err)
	}
	mr3, err := s.GetMergeRequest(mrID3)
	if err != nil {
		t.Fatal(err)
	}
	if mr3.Phase != "ready" {
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
	s := setupWorld(t)

	itemID, _ := s.CreateWrit("Item 1", "", "operator", 2, nil)
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
	if mr.Phase != "ready" {
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
	s := setupWorld(t)

	itemID1, _ := s.CreateWrit("Item 1", "", "operator", 2, nil)
	itemID2, _ := s.CreateWrit("Item 2", "", "operator", 2, nil)
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
	if mr1.Phase != "ready" {
		t.Fatalf("expected stale MR to be ready, got %q", mr1.Phase)
	}

	// The fresh one should still be claimed.
	claimed, _ := s.ListMergeRequests("claimed")
	if len(claimed) != 1 {
		t.Fatalf("expected 1 still-claimed MR, got %d", len(claimed))
	}
}

func TestListMergeRequestsByWrit(t *testing.T) {
	s := setupWorld(t)

	id1, _ := s.CreateWrit("Item 1", "", "operator", 2, nil)
	id2, _ := s.CreateWrit("Item 2", "", "operator", 2, nil)

	// Create MRs: 2 for id1, 1 for id2.
	mr1ID, _ := s.CreateMergeRequest(id1, "branch1a", 2)
	mr2ID, _ := s.CreateMergeRequest(id1, "branch1b", 2)
	s.CreateMergeRequest(id2, "branch2", 2)

	// Mark one as failed.
	s.ClaimMergeRequest("forge/Forge")
	s.UpdateMergeRequestPhase(mr1ID, "failed")

	// List all for id1.
	mrs, err := s.ListMergeRequestsByWrit(id1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(mrs) != 2 {
		t.Fatalf("expected 2 MRs for writ, got %d", len(mrs))
	}

	// List only failed for id1.
	failed, err := s.ListMergeRequestsByWrit(id1, "failed")
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
	ready, err := s.ListMergeRequestsByWrit(id1, "ready")
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
	s := setupWorld(t)

	itemID, _ := s.CreateWrit("Item 1", "", "operator", 2, nil)
	mrID, _ := s.CreateMergeRequest(itemID, "branch1", 2)

	// Transition to superseded.
	if err := s.UpdateMergeRequestPhase(mrID, "superseded"); err != nil {
		t.Fatalf("UpdateMergeRequestPhase(superseded) error: %v", err)
	}

	mr, err := s.GetMergeRequest(mrID)
	if err != nil {
		t.Fatal(err)
	}
	if mr.Phase != "superseded" {
		t.Errorf("phase = %q, want 'superseded'", mr.Phase)
	}
}

func TestGetMergeRequestNotFound(t *testing.T) {
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
	s := setupWorld(t)

	itemID, _ := s.CreateWrit("Item 1", "", "operator", 2, nil)
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
	if mr.Phase != "ready" {
		t.Errorf("phase = %q, want %q", mr.Phase, "ready")
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
	if mr.Phase != "ready" {
		t.Errorf("phase after unblock = %q, want %q", mr.Phase, "ready")
	}
}

func TestClaimSkipsBlockedMRs(t *testing.T) {
	s := setupWorld(t)

	id1, _ := s.CreateWrit("Item 1", "", "operator", 1, nil)
	id2, _ := s.CreateWrit("Item 2", "", "operator", 2, nil)
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
	s := setupWorld(t)

	itemID, _ := s.CreateWrit("Item 1", "", "operator", 2, nil)
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
	itemID, _ := s.CreateWrit("Test", "", "operator", 2, nil)
	mrID, _ := s.CreateMergeRequest(itemID, "branch1", 2)

	// Should be able to block/unblock without error.
	if err := s.BlockMergeRequest(mrID, "sol-test"); err != nil {
		t.Fatalf("BlockMergeRequest failed (blocked_by column missing?): %v", err)
	}
	if err := s.UnblockMergeRequest(mrID); err != nil {
		t.Fatalf("UnblockMergeRequest failed: %v", err)
	}
}
