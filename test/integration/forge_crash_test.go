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

// TestForgeReclaimAfterTTLRelease exercises the crash-recovery path end-to-end:
//
//  1. Cast a writ and resolve it (creates a branch + MR in "ready" state).
//  2. Claim the MR to simulate "forge crashed mid-merge".
//  3. Backdate claimed_at by 31 minutes to make the claim stale (> TTL).
//  4. Run ReleaseStaleClaims — verifies the stale claim is released back to "ready".
//  5. Run one forge patrol cycle — forge re-claims and merges the MR.
//  6. Verify the MR is marked merged and the writ is closed.
//
// This tests the highest-risk path in the system:
//
//	claim MR → crash mid-merge → restart → TTL expires → stale claim released
//	→ re-claim → merge succeeds
func TestForgeReclaimAfterTTLRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnvWithRepo(t)

	// Set up git repos: bare origin + working clone with "origin" configured.
	_, sourceClone := createSourceRepo(t, gtHome)

	// Open stores. openStores creates the world DB in SOL_HOME.
	worldStore, sphereStore := openStores(t, "crashtest")
	mgr := session.New()

	ctx := context.Background()

	// --- Phase 1: create a writ, cast an agent, write a file, resolve ---

	writID, err := worldStore.CreateWrit(
		"Crash recovery test feature",
		"Integration test for forge crash-recovery re-claim path",
		"test", 2, nil,
	)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Cast: auto-provisions an agent and creates a worktree on a branch.
	castResult, err := dispatch.Cast(ctx, dispatch.CastOpts{
		WritID:     writID,
		World:      "crashtest",
		SourceRepo: sourceClone,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Cast: %v", err)
	}

	// Simulate agent writing a file in the worktree.
	if err := os.WriteFile(
		castResult.WorktreeDir+"/crash_feature.go",
		[]byte("package main\n\nfunc crashFeature() {}\n"),
		0o644,
	); err != nil {
		t.Fatalf("WriteFile crash_feature.go: %v", err)
	}

	// Resolve: commits changes, pushes the branch to origin, creates MR in "ready".
	_, err = dispatch.Resolve(ctx, dispatch.ResolveOpts{
		World:     "crashtest",
		AgentName: castResult.AgentName,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Sanity check: one ready MR exists after resolve.
	mrs, err := worldStore.ListMergeRequests(store.MRReady)
	if err != nil {
		t.Fatalf("ListMergeRequests: %v", err)
	}
	if len(mrs) != 1 {
		t.Fatalf("expected 1 ready MR after resolve, got %d", len(mrs))
	}
	mrID := mrs[0].ID

	// --- Phase 2: simulate a forge crash mid-merge ---
	// Claim the MR (as a forge process would), then immediately backdate
	// claimed_at to simulate "forge claimed but crashed before completing merge".

	claimed, err := worldStore.ClaimMergeRequest("forge/crashtest")
	if err != nil {
		t.Fatalf("ClaimMergeRequest (simulate crash): %v", err)
	}
	if claimed == nil || claimed.ID != mrID {
		t.Fatalf("expected to claim MR %s, got %v", mrID, claimed)
	}

	// Verify it's claimed.
	mr, err := worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("GetMergeRequest after claim: %v", err)
	}
	if mr.Phase != store.MRClaimed {
		t.Fatalf("MR phase = %q before backdate, want %q", mr.Phase, store.MRClaimed)
	}

	// Backdate claimed_at by 31 minutes to exceed the 30-minute TTL.
	staleTime := time.Now().UTC().Add(-31 * time.Minute).Format(time.RFC3339)
	if _, err := worldStore.DB().Exec(
		`UPDATE merge_requests SET claimed_at = ? WHERE id = ?`, staleTime, mrID,
	); err != nil {
		t.Fatalf("backdate claimed_at: %v", err)
	}

	// --- Phase 3: TTL recovery — release the stale claim ---

	released, err := worldStore.ReleaseStaleClaims(30 * time.Minute)
	if err != nil {
		t.Fatalf("ReleaseStaleClaims: %v", err)
	}
	if released != 1 {
		t.Errorf("ReleaseStaleClaims returned %d, want 1", released)
	}

	// Verify the MR is back to "ready" with cleared claim fields.
	mr, err = worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("GetMergeRequest after TTL release: %v", err)
	}
	if mr.Phase != store.MRReady {
		t.Errorf("MR phase after TTL release = %q, want %q", mr.Phase, store.MRReady)
	}
	if mr.ClaimedBy != "" {
		t.Errorf("MR claimed_by after TTL release = %q, want empty", mr.ClaimedBy)
	}
	if mr.ClaimedAt != nil {
		t.Errorf("MR claimed_at after TTL release = %v, want nil", mr.ClaimedAt)
	}

	// --- Phase 4: forge restarts, re-claims, and merges ---

	forgeCfg := forge.DefaultConfig()
	forgeCfg.TargetBranch = "main"
	forgeCfg.QualityGates = []string{"echo ok"}
	forgeCfg.GateTimeout = 30 * time.Second
	forgeCfg.MaxAttempts = 3
	forgeCfg.ClaimTTL = 30 * time.Minute

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	f := forge.New("crashtest", sourceClone, worldStore, sphereStore, forgeCfg, logger)

	// Set up the forge worktree (creates a linked git worktree at origin/main).
	if err := f.EnsureWorktree(); err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}

	// Run one patrol cycle — the forge should re-claim the released MR and merge it.
	pcfg := forge.DefaultPatrolConfig()
	if err := f.RunPatrol(ctx, pcfg); err != nil {
		t.Fatalf("RunPatrol (re-claim after TTL release): %v", err)
	}

	// Verify the MR is now merged.
	mr, err = worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("GetMergeRequest after re-claim patrol: %v", err)
	}
	if mr.Phase != store.MRMerged {
		t.Errorf("MR phase after re-claim = %q, want %q", mr.Phase, store.MRMerged)
	}

	// Verify the writ is closed.
	writ, err := worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("GetWrit after re-claim patrol: %v", err)
	}
	if writ.Status != "closed" {
		t.Errorf("writ status = %q, want %q", writ.Status, "closed")
	}
}
