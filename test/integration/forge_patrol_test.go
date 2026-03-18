package integration

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/session"
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

// TestForgeMergeEndToEnd runs the complete forge merge pipeline:
//  1. Cast a writ (creates agent worktree on a branch)
//  2. Simulate agent work (write a file)
//  3. Resolve (commits, pushes branch, creates MR)
//  4. Run one forge patrol cycle with a test-only quality gate ("echo ok")
//  5. Verify the MR is marked merged and the writ is closed
//
// This covers the critical path documented in LOOP2_ACCEPTANCE.md §3–4 and
// the crash-recovery paths described in ADR-0027 (rebase + quality gates +
// squash merge + mark-merged).
func TestForgeMergeEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnvWithRepo(t)

	// Set up git repos: bare origin + working clone with "origin" configured.
	_, sourceClone := createSourceRepo(t, gtHome)

	// Open stores. openStores creates the world DB in SOL_HOME.
	worldStore, sphereStore := openStores(t, "forgetest")
	mgr := session.New()

	ctx := context.Background()

	// Create a writ.
	writID, err := worldStore.CreateWrit(
		"Test merge feature",
		"Integration test for end-to-end forge merge pipeline",
		"test", 2, nil,
	)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Cast: auto-provisions an agent, creates a worktree on a branch.
	castResult, err := dispatch.Cast(ctx, dispatch.CastOpts{
		WritID:     writID,
		World:      "forgetest",
		SourceRepo: sourceClone,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Cast: %v", err)
	}

	// Simulate agent writing a file in the worktree.
	if err := os.WriteFile(
		castResult.WorktreeDir+"/feature.go",
		[]byte("package main\n\nfunc feature() {}\n"),
		0o644,
	); err != nil {
		t.Fatalf("WriteFile feature.go: %v", err)
	}

	// Resolve: commits changes, pushes branch to origin, creates MR.
	_, err = dispatch.Resolve(ctx, dispatch.ResolveOpts{
		World:     "forgetest",
		AgentName: castResult.AgentName,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Verify an MR was created in ready state.
	mrs, err := worldStore.ListMergeRequests(store.MRReady)
	if err != nil {
		t.Fatalf("ListMergeRequests: %v", err)
	}
	if len(mrs) != 1 {
		t.Fatalf("expected 1 ready MR after resolve, got %d", len(mrs))
	}
	mrID := mrs[0].ID

	// Create forge in legacy mode (no session manager), with a test-only gate.
	forgeCfg := forge.DefaultConfig()
	forgeCfg.TargetBranch = "main"
	forgeCfg.QualityGates = []string{"echo ok"}
	forgeCfg.GateTimeout = 30 * time.Second
	forgeCfg.MaxAttempts = 3
	// Use a short ClaimTTL so the test doesn't wait.
	forgeCfg.ClaimTTL = 30 * time.Minute

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	f := forge.New("forgetest", sourceClone, worldStore, sphereStore, forgeCfg, logger)

	// Set up the forge worktree (creates a linked git worktree at origin/main).
	if err := f.EnsureWorktree(); err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}

	// Run exactly one patrol cycle.
	pcfg := forge.DefaultPatrolConfig()
	if err := f.RunPatrol(ctx, pcfg); err != nil {
		t.Fatalf("RunPatrol: %v", err)
	}

	// Verify the MR is now merged.
	mr, err := worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("GetMergeRequest after patrol: %v", err)
	}
	if mr.Phase != "merged" {
		t.Errorf("MR phase = %q, want %q", mr.Phase, "merged")
	}

	// Verify the writ is closed.
	writ, err := worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("GetWrit after patrol: %v", err)
	}
	if writ.Status != "closed" {
		t.Errorf("writ status = %q, want %q", writ.Status, "closed")
	}
}
