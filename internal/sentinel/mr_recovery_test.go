package sentinel

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// --- Recast tests ---

// createFailedMR creates a writ and a failed MR for it.
// Transitions through the valid path: ready → claimed → failed.
func createFailedMR(t *testing.T, worldStore *store.WorldStore, writID, title, branch string) string {
	t.Helper()
	createWrit(t, worldStore, writID, title)
	mrID, err := worldStore.CreateMergeRequest(writID, branch, 3)
	if err != nil {
		t.Fatalf("failed to create MR: %v", err)
	}
	if _, err := worldStore.ClaimMergeRequest("test/forge", 0); err != nil {
		t.Fatalf("failed to claim MR: %v", err)
	}
	if err := worldStore.UpdateMergeRequestPhase(mrID, store.MRFailed); err != nil {
		t.Fatalf("failed to set MR phase to failed: %v", err)
	}
	return mrID
}

func TestReleaseStaleClaims(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.ClaimTTL = 30 * time.Minute

	// Create a writ and MR, then claim it.
	createWrit(t, worldStore, "sol-stale001", "Stale claim test")
	mrID, err := worldStore.CreateMergeRequest("sol-stale001", "outpost/A/sol-stale001", 3)
	if err != nil {
		t.Fatalf("failed to create MR: %v", err)
	}
	claimed, err := worldStore.ClaimMergeRequest("forge-1", 0)
	if err != nil {
		t.Fatalf("failed to claim MR: %v", err)
	}
	if claimed == nil || claimed.ID != mrID {
		t.Fatal("expected to claim the MR")
	}

	// Backdate the claimed_at to make it stale (> 30 min ago).
	staleTime := time.Now().UTC().Add(-45 * time.Minute).Format(time.RFC3339)
	_, err = worldStore.DB().Exec(
		`UPDATE merge_requests SET claimed_at = ? WHERE id = ?`, staleTime, mrID)
	if err != nil {
		t.Fatalf("failed to backdate claimed_at: %v", err)
	}

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// The MR should be back to "ready" phase.
	mr, err := worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("failed to get MR: %v", err)
	}
	if mr.Phase != store.MRReady {
		t.Errorf("MR phase = %q, want %q", mr.Phase, store.MRReady)
	}
	if mr.ClaimedBy != "" {
		t.Errorf("MR claimed_by = %q, want empty", mr.ClaimedBy)
	}
}

func TestReleaseStaleClaims_SkipsFresh(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.ClaimTTL = 30 * time.Minute

	// Create a writ and MR, then claim it (claimed_at = now, so fresh).
	createWrit(t, worldStore, "sol-fresh001", "Fresh claim test")
	mrID, err := worldStore.CreateMergeRequest("sol-fresh001", "outpost/A/sol-fresh001", 3)
	if err != nil {
		t.Fatalf("failed to create MR: %v", err)
	}
	claimed, err := worldStore.ClaimMergeRequest("forge-1", 0)
	if err != nil {
		t.Fatalf("failed to claim MR: %v", err)
	}
	if claimed == nil || claimed.ID != mrID {
		t.Fatal("expected to claim the MR")
	}

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// The MR should still be claimed — it's fresh.
	mr, err := worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("failed to get MR: %v", err)
	}
	if mr.Phase != store.MRClaimed {
		t.Errorf("MR phase = %q, want %q (claim is fresh, should not be released)", mr.Phase, store.MRClaimed)
	}
	if mr.ClaimedBy != "forge-1" {
		t.Errorf("MR claimed_by = %q, want %q", mr.ClaimedBy, "forge-1")
	}
}

// recastNowFunc returns a time function that skips ahead by the given duration,
// sufficient to bypass all cooldown/backoff checks in recast tests.
func recastNowFunc(skip time.Duration) func() time.Time {
	return func() time.Time { return time.Now().Add(skip) }
}

// assertRecastMetadata checks that a writ's metadata has the expected recast count.
func assertRecastMetadata(t *testing.T, worldStore *store.WorldStore, writID string, wantCount int) {
	t.Helper()
	item, err := worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("GetWrit(%q) error: %v", writID, err)
	}
	got := recastCountFromMetadata(item)
	if got != wantCount {
		t.Errorf("recast-count metadata for %q = %d, want %d", writID, got, wantCount)
	}
}

func TestRecastFailedMR(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR with an open writ.
	createFailedMR(t, worldStore, "sol-fail1111", "Failing task", "outpost/Toast/sol-fail1111")

	castCalled := false
	var castWritID string

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(15 * time.Minute)) // skip past cooldown
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		castWritID = writID
		return &CastResult{
			WritID:      writID,
			AgentName:   "Sage",
			SessionName: "sol-ember-Sage",
		}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if !castCalled {
		t.Fatal("expected castFn to be called for failed MR")
	}
	if castWritID != "sol-fail1111" {
		t.Errorf("castFn called with %q, want %q", castWritID, "sol-fail1111")
	}

	// Recast count should be 1 (persisted in metadata).
	assertRecastMetadata(t, worldStore, "sol-fail1111", 1)
}

func TestRecastSkipsNonOpenWrit(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a failed MR but set the writ to "tethered" (already re-dispatched).
	mrID := createFailedMR(t, worldStore, "sol-teth2222", "Already tethered", "outpost/X/sol-teth2222")
	_ = mrID
	worldStore.UpdateWrit("sol-teth2222", store.WritUpdates{Status: store.WritTethered, Assignee: "ember/Toast"})

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when writ is not open")
	}
}

func TestRecastMaxAttemptsEscalates(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 2

	// Create a failed MR with an open writ.
	createFailedMR(t, worldStore, "sol-maxr3333", "Max retries task", "outpost/Toast/sol-maxr3333")

	// Pre-set recast count to max via writ metadata.
	worldStore.SetWritMetadata("sol-maxr3333", map[string]any{
		"recast-count": float64(2),
		"recast-last":  time.Now().UTC().Format(time.RFC3339),
	})

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(2 * time.Hour)) // skip past all backoff
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when max recast attempts reached")
	}

	// Should have sent RECOVERY_NEEDED to operator.
	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("expected RECOVERY_NEEDED protocol message after max recast attempts")
	}

	// Recast count should be incremented past max to prevent re-escalation.
	assertRecastMetadata(t, worldStore, "sol-maxr3333", 3)
}

func TestRecastMaxAttemptsEscalatesOnlyOnce(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 2

	// Create a failed MR with an open writ.
	createFailedMR(t, worldStore, "sol-once4444", "Escalate once", "outpost/Toast/sol-once4444")

	// Pre-set recast count past max via metadata (already escalated).
	worldStore.SetWritMetadata("sol-once4444", map[string]any{
		"recast-count": float64(3),
	})

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(2 * time.Hour))
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No new RECOVERY_NEEDED message.
	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) != 0 {
		t.Error("should not send RECOVERY_NEEDED again after already escalated")
	}
}

func TestRecastNoCastFuncSkips(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a failed MR.
	createFailedMR(t, worldStore, "sol-nocast55", "No cast func", "outpost/X/sol-nocast55")

	// No castFn set.
	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Should complete without error and without panic.
}

func TestRecastDeduplicatesByWrit(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a writ with TWO failed MRs (e.g., two merge attempts).
	createWrit(t, worldStore, "sol-dedup666", "Dedup task")
	mr1, _ := worldStore.CreateMergeRequest("sol-dedup666", "outpost/A/sol-dedup666", 3)
	worldStore.ClaimMergeRequest("test/forge", 0)
	worldStore.UpdateMergeRequestPhase(mr1, store.MRFailed)
	mr2, _ := worldStore.CreateMergeRequest("sol-dedup666", "outpost/B/sol-dedup666", 3)
	worldStore.ClaimMergeRequest("test/forge", 0)
	worldStore.UpdateMergeRequestPhase(mr2, store.MRFailed)

	castCount := 0
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(15 * time.Minute)) // skip past cooldown
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCount++
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Cast should only be called once despite two failed MRs.
	if castCount != 1 {
		t.Errorf("castFn called %d times, want 1 (deduplication)", castCount)
	}
}

func TestRecastPrunesDedupOnHandledItem(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a failed MR with a "tethered" writ (already re-dispatched).
	createFailedMR(t, worldStore, "sol-prune777", "Already tethered", "outpost/X/sol-prune777")
	worldStore.UpdateWrit("sol-prune777", store.WritUpdates{Status: store.WritTethered, Assignee: "ember/Toast"})

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		return &CastResult{AgentName: "Sage"}, nil
	})

	// Pre-set a dedup guard entry (old enough to pass the dedup check).
	w.lastCastTime["sol-prune777"] = time.Now().Add(-time.Minute)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Dedup guard should be pruned since writ is tethered (handled elsewhere).
	if _, exists := w.lastCastTime["sol-prune777"]; exists {
		t.Error("expected lastCastTime to be pruned for tethered writ")
	}
}

func TestRecastDoneWritNoAssigneeTransitionsToOpen(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR with a "done" writ and no assignee (orphaned).
	createFailedMR(t, worldStore, "sol-done1111", "Orphaned done", "outpost/X/sol-done1111")
	worldStore.UpdateWrit("sol-done1111", store.WritUpdates{Status: store.WritDone})

	castCalled := false
	var castWritID string

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(15 * time.Minute)) // skip past cooldown
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		castWritID = writID
		return &CastResult{
			WritID:    writID,
			AgentName: "Sage",
		}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if !castCalled {
		t.Fatal("expected castFn to be called for done writ with no assignee")
	}
	if castWritID != "sol-done1111" {
		t.Errorf("castFn called with %q, want %q", castWritID, "sol-done1111")
	}

	// Verify writ was transitioned to "open".
	item, err := worldStore.GetWrit("sol-done1111")
	if err != nil {
		t.Fatalf("GetWrit() error: %v", err)
	}
	// After castFn succeeds, dispatch sets the writ status; here we verify
	// sentinel at least transitioned it from "done" (it's now "open" or
	// whatever castFn/dispatch set it to).
	if item.Status == store.WritDone {
		t.Error("writ should no longer be in done status after recast")
	}

	// Recast count should be 1 (persisted in metadata).
	assertRecastMetadata(t, worldStore, "sol-done1111", 1)
}

func TestRecastDoneWritWithAssigneeSkipped(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a failed MR with a "done" writ that has an assignee.
	createFailedMR(t, worldStore, "sol-dassn222", "Done with agent", "outpost/X/sol-dassn222")
	worldStore.UpdateWrit("sol-dassn222", store.WritUpdates{Status: store.WritDone, Assignee: "ember/Toast"})

	// Register the agent so it exists in the sphere store — sentinel should skip
	// recasting when the assignee is still active.
	sphereStore.CreateAgent("Toast", "ember", "outpost")

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called for done writ with active assignee")
	}
}

func TestRecastDoneWritWithReapedAssigneeRecasts(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR with a "done" writ whose assignee agent no longer exists
	// (simulates the agent being reaped after the writ was resolved).
	createFailedMR(t, worldStore, "sol-reaped11", "Reaped agent done", "outpost/X/sol-reaped11")
	worldStore.UpdateWrit("sol-reaped11", store.WritUpdates{Status: store.WritDone, Assignee: "ember/ReapedAgent"})
	// Deliberately do NOT register "ember/ReapedAgent" — it has been reaped.

	castCalled := false
	var castWritID string

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(15 * time.Minute)) // skip past cooldown
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		castWritID = writID
		return &CastResult{
			WritID:    writID,
			AgentName: "Sage",
		}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if !castCalled {
		t.Fatal("expected castFn to be called for done writ with reaped assignee")
	}
	if castWritID != "sol-reaped11" {
		t.Errorf("castFn called with %q, want %q", castWritID, "sol-reaped11")
	}

	// Recast count should be 1 (persisted in metadata).
	assertRecastMetadata(t, worldStore, "sol-reaped11", 1)
}

func TestRecastSkipsDuplicateMR(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a writ with a failed MR AND a non-failed MR (e.g., "ready").
	createWrit(t, worldStore, "sol-dupmr333", "Dup MR task")
	failedMR, _ := worldStore.CreateMergeRequest("sol-dupmr333", "outpost/A/sol-dupmr333", 3)
	worldStore.ClaimMergeRequest("test/forge", 0)
	worldStore.UpdateMergeRequestPhase(failedMR, store.MRFailed)
	readyMR, _ := worldStore.CreateMergeRequest("sol-dupmr333", "outpost/B/sol-dupmr333", 3)
	_ = readyMR

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(15 * time.Minute)) // skip past cooldown
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when a non-failed MR exists (duplicate prevention)")
	}
}

func TestRecastCastFailureNonBlocking(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a failed MR.
	createFailedMR(t, worldStore, "sol-cfail888", "Cast failure", "outpost/X/sol-cfail888")

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(15 * time.Minute)) // skip past cooldown
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		return nil, fmt.Errorf("no idle agents available")
	})

	// Should not error — cast failure is non-blocking.
	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Recast count should NOT be incremented on failure (metadata unchanged).
	assertRecastMetadata(t, worldStore, "sol-cfail888", 0)
}

// --- Cooldown, backoff, and dedup guard tests ---

func TestRecastCooldownSkipsRecentFailure(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR with an open writ (MR failure is "now").
	createFailedMR(t, worldStore, "sol-cool1111", "Recent failure", "outpost/X/sol-cool1111")

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	// Do NOT set nowFn — default is time.Now, so MR failure is <10 min old.
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when MR failure is less than 10 minutes old")
	}
}

func TestRecastCooldownAllowsOldFailure(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR with an open writ.
	createFailedMR(t, worldStore, "sol-cool2222", "Old failure", "outpost/X/sol-cool2222")

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	// Jump 11 minutes into the future — past the 10-minute cooldown.
	w.SetNowFunc(recastNowFunc(11 * time.Minute))
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if !castCalled {
		t.Error("castFn should be called when MR failure is older than 10 minutes")
	}
}

func TestRecastBackoffDelaysSecondRecast(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR with an open writ.
	createFailedMR(t, worldStore, "sol-back1111", "Backoff test", "outpost/X/sol-back1111")

	// Pre-set: 1 recast already done, last recast 15 min ago.
	recastTime := time.Now().Add(-15 * time.Minute).UTC().Format(time.RFC3339)
	worldStore.SetWritMetadata("sol-back1111", map[string]any{
		"recast-count": float64(1),
		"recast-last":  recastTime,
	})

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	// nowFn not needed — the last recast was 15 min ago but the 2nd recast
	// requires 30 min backoff, so it should be skipped.
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when 30-min backoff has not elapsed")
	}
}

func TestRecastBackoffAllowsAfterElapsed(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR with an open writ.
	createFailedMR(t, worldStore, "sol-back2222", "Backoff elapsed", "outpost/X/sol-back2222")

	// Pre-set: 1 recast already done, last recast 35 min ago (>30 min backoff).
	recastTime := time.Now().Add(-35 * time.Minute).UTC().Format(time.RFC3339)
	worldStore.SetWritMetadata("sol-back2222", map[string]any{
		"recast-count": float64(1),
		"recast-last":  recastTime,
	})

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	// MR failure is "now" but the cooldown check uses mr.UpdatedAt only for attempt 0.
	// For attempt 1+, the backoff check uses recast-last. Since recast-last is 35 min ago
	// and we need 30 min for the 2nd recast, this should pass. But we still need the
	// initial cooldown check to pass (which it does because attempts > 0 uses recast-last).
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if !castCalled {
		t.Error("castFn should be called when 30-min backoff has elapsed")
	}
	assertRecastMetadata(t, worldStore, "sol-back2222", 2)
}

func TestRecastThirdAttemptBackoff60Min(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR.
	createFailedMR(t, worldStore, "sol-back3333", "60m backoff", "outpost/X/sol-back3333")

	// Pre-set: 2 recasts done, last recast 45 min ago (<60 min backoff).
	recastTime := time.Now().Add(-45 * time.Minute).UTC().Format(time.RFC3339)
	worldStore.SetWritMetadata("sol-back3333", map[string]any{
		"recast-count": float64(2),
		"recast-last":  recastTime,
	})

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when 60-min backoff has not elapsed")
	}
}

func TestRecastDeduplicationGuard(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.PatrolInterval = 3 * time.Minute // realistic interval for dedup test

	// Create a failed MR with an open writ.
	createFailedMR(t, worldStore, "sol-dedup111", "Dedup guard", "outpost/X/sol-dedup111")

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(15 * time.Minute)) // skip past cooldown

	// Pre-set dedup guard: writ was cast very recently (within 2× patrol interval).
	w.lastCastTime["sol-dedup111"] = w.now().Add(-time.Minute) // 1 min ago, within 6 min window

	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when writ was recently cast (dedup guard)")
	}
}

func TestRecastPersistentCountSurvivesRestart(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR with an open writ.
	createFailedMR(t, worldStore, "sol-pers1111", "Persistent count", "outpost/X/sol-pers1111")

	// First sentinel instance: recast once.
	w1 := New(cfg, sphereStore, worldStore, mock, nil)
	w1.SetNowFunc(recastNowFunc(15 * time.Minute))
	w1.SetCastFunc(func(writID string) (*CastResult, error) {
		return &CastResult{AgentName: "Sage"}, nil
	})
	if err := w1.patrol(context.Background()); err != nil {
		t.Fatalf("w1.patrol() error: %v", err)
	}
	assertRecastMetadata(t, worldStore, "sol-pers1111", 1)

	// Simulate sentinel restart — create a new Sentinel (no in-memory state).
	// Reset writ to open (simulate MR failure cycle).
	worldStore.UpdateWrit("sol-pers1111", store.WritUpdates{Status: store.WritOpen, Assignee: "-"})

	w2 := New(cfg, sphereStore, worldStore, mock, nil)
	// Jump 35 min to pass the 30-min backoff for the 2nd recast.
	w2.SetNowFunc(recastNowFunc(50 * time.Minute))
	w2.SetCastFunc(func(writID string) (*CastResult, error) {
		return &CastResult{AgentName: "Sage"}, nil
	})
	if err := w2.patrol(context.Background()); err != nil {
		t.Fatalf("w2.patrol() error: %v", err)
	}

	// Count should be 2 (persisted across sentinel restarts).
	assertRecastMetadata(t, worldStore, "sol-pers1111", 2)
}

// --- Orphaned resolution dispatch tests ---

// createBlockedMR creates an original writ with MR, a blocker (resolution) writ,
// and blocks the MR with the blocker writ. Returns the MR ID.
// The blocker writ is created with the given age (time before now).
func createBlockedMR(t *testing.T, worldStore *store.WorldStore, writID, blockerWritID, title, branch string, blockerAge time.Duration) string {
	t.Helper()
	createWrit(t, worldStore, writID, title)
	mrID, err := worldStore.CreateMergeRequest(writID, branch, 3)
	if err != nil {
		t.Fatalf("failed to create MR: %v", err)
	}

	// Create blocker (resolution) writ with a backdated created_at.
	createdAt := time.Now().UTC().Add(-blockerAge).Format(time.RFC3339)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = worldStore.DB().Exec(
		`INSERT INTO writs (id, title, description, status, priority, created_by, created_at, updated_at)
		 VALUES (?, ?, '', 'open', 1, 'forge', ?, ?)`,
		blockerWritID, "Resolve conflict for "+title, createdAt, now,
	)
	if err != nil {
		t.Fatalf("failed to create blocker writ %q: %v", blockerWritID, err)
	}

	if err := worldStore.BlockMergeRequest(mrID, blockerWritID); err != nil {
		t.Fatalf("failed to block MR: %v", err)
	}
	return mrID
}

func TestDispatchOrphanedResolution_HappyPath(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Blocked MR with open+unassigned resolution writ older than 5 min.
	mrID := createBlockedMR(t, worldStore, "sol-orig1111", "sol-res-1111", "Feature A", "outpost/Toast/sol-orig1111", 10*time.Minute)
	_ = mrID

	castCalled := false
	var castWritID string

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		castWritID = writID
		return &CastResult{
			WritID:      writID,
			AgentName:   "Sage",
			SessionName: "sol-ember-Sage",
		}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if !castCalled {
		t.Fatal("expected castFn to be called for orphaned resolution writ")
	}
	if castWritID != "sol-res-1111" {
		t.Errorf("castFn called with %q, want %q", castWritID, "sol-res-1111")
	}

	// Dispatch count should be 1.
	if w.resolutionDispatchCounts["sol-res-1111"] != 1 {
		t.Errorf("resolution dispatch count = %d, want 1", w.resolutionDispatchCounts["sol-res-1111"])
	}
}

func TestDispatchOrphanedResolution_SkipAssigned(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create blocked MR with old resolution writ.
	createBlockedMR(t, worldStore, "sol-orig2222", "sol-res-2222", "Feature B", "outpost/Toast/sol-orig2222", 10*time.Minute)

	// Assign the resolution writ (already dispatched).
	worldStore.UpdateWrit("sol-res-2222", store.WritUpdates{Assignee: "ember/Toast"})

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when blocker writ has assignee")
	}
}

func TestDispatchOrphanedResolution_SkipClosed(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create blocked MR with old resolution writ.
	createBlockedMR(t, worldStore, "sol-orig3333", "sol-res-3333", "Feature C", "outpost/Toast/sol-orig3333", 10*time.Minute)

	// Close the resolution writ (already handled).
	worldStore.UpdateWrit("sol-res-3333", store.WritUpdates{Status: store.WritClosed})

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when blocker writ is closed")
	}
}

func TestDispatchOrphanedResolution_SkipYoung(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create blocked MR with resolution writ only 2 min old (within grace period).
	createBlockedMR(t, worldStore, "sol-orig4444", "sol-res-4444", "Feature D", "outpost/Toast/sol-orig4444", 2*time.Minute)

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when blocker writ is younger than grace period")
	}
}

func TestDispatchOrphanedResolution_AttemptCapEscalates(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 2

	// Create blocked MR with old resolution writ.
	createBlockedMR(t, worldStore, "sol-orig5555", "sol-res-5555", "Feature E", "outpost/Toast/sol-orig5555", 10*time.Minute)

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	// Pre-set dispatch count to max.
	w.resolutionDispatchCounts["sol-res-5555"] = 2

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when max dispatch attempts reached")
	}

	// Should have sent RECOVERY_NEEDED to operator.
	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("expected RECOVERY_NEEDED protocol message after max dispatch attempts")
	}

	// Dispatch count should be incremented past max to prevent re-escalation.
	if w.resolutionDispatchCounts["sol-res-5555"] != 3 {
		t.Errorf("resolution dispatch count = %d, want %d (max+1)", w.resolutionDispatchCounts["sol-res-5555"], 3)
	}
}

func TestDispatchOrphanedResolution_CastFailureDoesNotIncrementCount(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create blocked MR with resolution writ older than the grace period.
	createBlockedMR(t, worldStore, "sol-cf-orig6", "sol-cf-res-6", "Feature F", "outpost/Toast/sol-cf-orig6", 10*time.Minute)

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		return nil, fmt.Errorf("no available agents")
	})

	// Pre-set dispatch count to 1 so we can observe it stays unchanged.
	w.resolutionDispatchCounts["sol-cf-res-6"] = 1

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Count should still be 1 — cast failure must NOT increment the counter.
	if w.resolutionDispatchCounts["sol-cf-res-6"] != 1 {
		t.Errorf("resolution dispatch count = %d, want 1 (cast failure should not increment count)",
			w.resolutionDispatchCounts["sol-cf-res-6"])
	}
}

func TestDispatchOrphanedResolution_EscalatesExactlyOnce(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 2

	// Create blocked MR with resolution writ older than the grace period.
	createBlockedMR(t, worldStore, "sol-eo-orig7", "sol-eo-res-7", "Feature G", "outpost/Toast/sol-eo-orig7", 10*time.Minute)

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	// Pre-set dispatch count to max so the first patrol fires the escalation.
	w.resolutionDispatchCounts["sol-eo-res-7"] = 2

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("first patrol() error: %v", err)
	}

	msgs1, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs1) == 0 {
		t.Fatal("expected RECOVERY_NEEDED message after first patrol at max attempts")
	}
	countAfterFirst := len(msgs1)

	// Second patrol — count is now maxAttempts+1; escalation should NOT fire again.
	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("second patrol() error: %v", err)
	}

	msgs2, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs2) != countAfterFirst {
		t.Errorf("RECOVERY_NEEDED count went from %d to %d after second patrol — escalation should fire exactly once",
			countAfterFirst, len(msgs2))
	}

	if castCalled {
		t.Error("castFn should not be called once max attempts reached")
	}
}

func TestRecastMaxAttemptsCreatesEscalation(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 2

	createFailedMR(t, worldStore, "sol-escr1111", "Escalation recast task", "outpost/Toast/sol-escr1111")

	// Pre-set recast count to max via writ metadata.
	worldStore.SetWritMetadata("sol-escr1111", map[string]any{
		"recast-count": float64(2),
		"recast-last":  time.Now().UTC().Format(time.RFC3339),
	})

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(2 * time.Hour))
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Should have created a formal escalation.
	escs, err := sphereStore.ListEscalations("")
	if err != nil {
		t.Fatalf("ListEscalations() error: %v", err)
	}

	var found *store.Escalation
	for i := range escs {
		if escs[i].Source == "ember/sentinel" && escs[i].Severity == "high" &&
			strings.Contains(escs[i].Description, "sol-escr1111") &&
			strings.Contains(escs[i].Description, "recast limit") {
			found = &escs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected a high-severity escalation from ember/sentinel for failed recast")
	}
	if found.SourceRef != "writ:sol-escr1111" {
		t.Errorf("escalation source_ref = %q, want %q", found.SourceRef, "writ:sol-escr1111")
	}

	// Protocol message should still be sent alongside.
	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("expected RECOVERY_NEEDED protocol message alongside escalation")
	}
}

func TestOrphanedResolutionCreatesEscalation(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 2

	// Create blocked MR with old resolution writ.
	createBlockedMR(t, worldStore, "sol-escorph1", "sol-res-escorph1", "Feature orphan esc", "outpost/Toast/sol-escorph1", 10*time.Minute)

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		return &CastResult{AgentName: "Sage"}, nil
	})

	// Pre-set dispatch count to max.
	w.resolutionDispatchCounts["sol-res-escorph1"] = 2

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Should have created a formal escalation with medium severity.
	escs, err := sphereStore.ListEscalations("")
	if err != nil {
		t.Fatalf("ListEscalations() error: %v", err)
	}

	var found *store.Escalation
	for i := range escs {
		if escs[i].Source == "ember/sentinel" && escs[i].Severity == "medium" &&
			strings.Contains(escs[i].Description, "sol-res-escorph1") &&
			strings.Contains(escs[i].Description, "dispatch limit") {
			found = &escs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected a medium-severity escalation from ember/sentinel for orphaned resolution")
	}
	if found.SourceRef != "writ:sol-res-escorph1" {
		t.Errorf("escalation source_ref = %q, want %q", found.SourceRef, "writ:sol-res-escorph1")
	}

	// Protocol message should still be sent alongside.
	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("expected RECOVERY_NEEDED protocol message alongside escalation")
	}
}

func TestRecoverOrphanedTetheredWrits_MissingAgent(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a tethered writ with an assignee that does not exist in the sphere store.
	createWrit(t, worldStore, "sol-teth000000000001", "Orphaned tethered task")
	worldStore.UpdateWrit("sol-teth000000000001", store.WritUpdates{
		Status:   "tethered",
		Assignee: "ember/Ghost",
	})
	// Make it old enough to pass the grace period.
	worldStore.DB().Exec(`UPDATE writs SET updated_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-10*time.Minute).Format(time.RFC3339), "sol-teth000000000001")

	w := New(cfg, sphereStore, worldStore, mock, nil)

	recovered := w.recoverOrphanedTetheredWrits()
	if recovered != 1 {
		t.Fatalf("recoverOrphanedTetheredWrits() = %d, want 1", recovered)
	}

	// Writ should be reopened.
	writ, err := worldStore.GetWrit("sol-teth000000000001")
	if err != nil {
		t.Fatalf("GetWrit() error: %v", err)
	}
	if writ.Status != store.WritOpen {
		t.Errorf("writ status = %q, want %q", writ.Status, store.WritOpen)
	}
	if writ.Assignee != "" {
		t.Errorf("writ assignee = %q, want empty", writ.Assignee)
	}
}

func TestRecoverOrphanedTetheredWrits_WorkingAgentSkipped(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working agent.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-teth000000000002")

	// Create a tethered writ assigned to the working agent.
	createWrit(t, worldStore, "sol-teth000000000002", "Active tethered task")
	worldStore.UpdateWrit("sol-teth000000000002", store.WritUpdates{
		Status:   "tethered",
		Assignee: "ember/Toast",
	})
	// Make it old enough to pass the grace period.
	worldStore.DB().Exec(`UPDATE writs SET updated_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-10*time.Minute).Format(time.RFC3339), "sol-teth000000000002")

	w := New(cfg, sphereStore, worldStore, mock, nil)

	recovered := w.recoverOrphanedTetheredWrits()
	if recovered != 0 {
		t.Fatalf("recoverOrphanedTetheredWrits() = %d, want 0 (working agent should be skipped)", recovered)
	}

	// Writ should still be tethered.
	writ, err := worldStore.GetWrit("sol-teth000000000002")
	if err != nil {
		t.Fatalf("GetWrit() error: %v", err)
	}
	if writ.Status != "tethered" {
		t.Errorf("writ status = %q, want %q", writ.Status, "tethered")
	}
}

func TestRecoverOrphanedTetheredWrits_GracePeriod(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a tethered writ with a missing agent, but recently updated (within grace period).
	createWrit(t, worldStore, "sol-teth000000000003", "Recent tethered task")
	worldStore.UpdateWrit("sol-teth000000000003", store.WritUpdates{
		Status:   "tethered",
		Assignee: "ember/Ghost",
	})
	// UpdatedAt is now (recent) — within the 5-minute grace period.

	w := New(cfg, sphereStore, worldStore, mock, nil)

	recovered := w.recoverOrphanedTetheredWrits()
	if recovered != 0 {
		t.Fatalf("recoverOrphanedTetheredWrits() = %d, want 0 (grace period should prevent recovery)", recovered)
	}

	// Writ should still be tethered.
	writ, err := worldStore.GetWrit("sol-teth000000000003")
	if err != nil {
		t.Fatalf("GetWrit() error: %v", err)
	}
	if writ.Status != "tethered" {
		t.Errorf("writ status = %q, want %q", writ.Status, "tethered")
	}
}

func TestEscalateFailedRecast_SkipsWhenOpenEscalationExists(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 2

	createFailedMR(t, worldStore, "sol-dedupesc001", "Dedup escalation task", "outpost/Toast/sol-dedupesc001")

	// Pre-create an open escalation for this writ.
	_, err := sphereStore.CreateEscalation("high", "ember/sentinel",
		"existing escalation for sol-dedupesc001", "writ:sol-dedupesc001")
	if err != nil {
		t.Fatalf("CreateEscalation() error: %v", err)
	}

	// Pre-set recast count to max via writ metadata.
	worldStore.SetWritMetadata("sol-dedupesc001", map[string]any{
		"recast-count": float64(2),
		"recast-last":  time.Now().UTC().Format(time.RFC3339),
	})

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(2 * time.Hour))
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Should NOT have created a duplicate escalation — only the original should exist.
	escs, err := sphereStore.ListEscalations("")
	if err != nil {
		t.Fatalf("ListEscalations() error: %v", err)
	}

	var matchCount int
	for _, esc := range escs {
		if esc.SourceRef == "writ:sol-dedupesc001" && esc.Severity == "high" {
			matchCount++
		}
	}
	if matchCount != 1 {
		t.Errorf("expected exactly 1 escalation for writ:sol-dedupesc001, got %d", matchCount)
	}
}

func TestEscalateOrphanedResolution_SkipsWhenOpenEscalationExists(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 2

	// Create blocked MR with old resolution writ.
	createBlockedMR(t, worldStore, "sol-dedupres001", "sol-res-dedupres001", "Dedup resolution task", "outpost/Toast/sol-dedupres001", 10*time.Minute)

	// Pre-create an open escalation for the resolution writ.
	_, err := sphereStore.CreateEscalation("medium", "ember/sentinel",
		"existing escalation for sol-res-dedupres001", "writ:sol-res-dedupres001")
	if err != nil {
		t.Fatalf("CreateEscalation() error: %v", err)
	}

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		return &CastResult{AgentName: "Sage"}, nil
	})

	// Pre-set dispatch count to max.
	w.resolutionDispatchCounts["sol-res-dedupres001"] = 2

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Should NOT have created a duplicate escalation — only the original should exist.
	escs, err := sphereStore.ListEscalations("")
	if err != nil {
		t.Fatalf("ListEscalations() error: %v", err)
	}

	var matchCount int
	for _, esc := range escs {
		if esc.SourceRef == "writ:sol-res-dedupres001" {
			matchCount++
		}
	}
	if matchCount != 1 {
		t.Errorf("expected exactly 1 escalation for writ:sol-res-dedupres001, got %d", matchCount)
	}
}

func TestResolutionDispatchCount_PersistedInMetadata(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create blocked MR with old resolution writ.
	createBlockedMR(t, worldStore, "sol-disppers001", "sol-res-disppers001", "Persisted dispatch task", "outpost/Toast/sol-disppers001", 10*time.Minute)

	castCount := 0
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCount++
		return &CastResult{AgentName: "Sage"}, nil
	})

	// First dispatch.
	dispatched := w.dispatchOrphanedResolutions()
	if dispatched != 1 {
		t.Fatalf("first dispatch: got %d, want 1", dispatched)
	}

	// Verify count was persisted in writ metadata.
	writ, err := worldStore.GetWrit("sol-res-disppers001")
	if err != nil {
		t.Fatalf("GetWrit() error: %v", err)
	}
	count := resolutionDispatchCountFromMetadata(writ)
	if count != 1 {
		t.Errorf("persisted dispatch count = %d, want 1", count)
	}

	// Simulate sentinel restart: create a fresh sentinel (in-memory counts reset).
	w2 := New(cfg, sphereStore, worldStore, mock, nil)
	w2.SetCastFunc(func(writID string) (*CastResult, error) {
		castCount++
		return &CastResult{AgentName: "Sage"}, nil
	})

	// Reopen the resolution writ (simulate it being returned to open after agent failure).
	worldStore.UpdateWrit("sol-res-disppers001", store.WritUpdates{
		Status:   "open",
		Assignee: "-",
	})
	// Backdate it past grace period again.
	worldStore.DB().Exec(`UPDATE writs SET created_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-10*time.Minute).Format(time.RFC3339), "sol-res-disppers001")

	// Second dispatch should read persisted count (1) and increment to 2.
	dispatched = w2.dispatchOrphanedResolutions()
	if dispatched != 1 {
		t.Fatalf("second dispatch (after restart): got %d, want 1", dispatched)
	}

	writ, err = worldStore.GetWrit("sol-res-disppers001")
	if err != nil {
		t.Fatalf("GetWrit() after second dispatch: %v", err)
	}
	count = resolutionDispatchCountFromMetadata(writ)
	if count != 2 {
		t.Errorf("persisted dispatch count after restart = %d, want 2", count)
	}
}
