package forge

import (
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// mkMR is a tiny helper for constructing MergeRequest rows in tests.
func mkMR(id, phase string, created time.Time, blockedBy string, merged *time.Time) store.MergeRequest {
	return store.MergeRequest{
		ID:        id,
		WritID:    "sol-" + id,
		Branch:    "writs/" + id,
		Phase:     phase,
		BlockedBy: blockedBy,
		CreatedAt: created,
		UpdatedAt: created,
		MergedAt:  merged,
	}
}

func TestFilterQueueByStatusDefault(t *testing.T) {
	now := time.Now()
	mrs := []store.MergeRequest{
		mkMR("a", store.MRReady, now, "", nil),
		mkMR("b", store.MRClaimed, now, "", nil),
		mkMR("c", store.MRFailed, now, "", nil),
		mkMR("d", store.MRMerged, now, "", &now),
		mkMR("e", store.MRSuperseded, now, "", nil),
	}

	// Default filter: active statuses only (no merged, no superseded).
	out := FilterQueueByStatus(mrs, false, "")
	if len(out) != 3 {
		t.Fatalf("expected 3 active MRs, got %d", len(out))
	}
	for _, mr := range out {
		if mr.Phase == store.MRMerged || mr.Phase == store.MRSuperseded {
			t.Errorf("default filter should not include phase %q", mr.Phase)
		}
	}
}

func TestFilterQueueByStatusAll(t *testing.T) {
	now := time.Now()
	mrs := []store.MergeRequest{
		mkMR("a", store.MRReady, now, "", nil),
		mkMR("b", store.MRMerged, now, "", &now),
	}

	out := FilterQueueByStatus(mrs, true, "")
	if len(out) != 2 {
		t.Fatalf("--all should include every MR, got %d", len(out))
	}
}

func TestFilterQueueByStatusExplicit(t *testing.T) {
	now := time.Now()
	mrs := []store.MergeRequest{
		mkMR("a", store.MRReady, now, "", nil),
		mkMR("b", store.MRClaimed, now, "", nil),
		mkMR("c", store.MRMerged, now, "", &now),
	}

	// Explicit --status should override the default and ignore other phases.
	out := FilterQueueByStatus(mrs, false, "merged")
	if len(out) != 1 || out[0].Phase != store.MRMerged {
		t.Fatalf("expected only merged MR, got %+v", out)
	}

	// Comma-separated list with whitespace.
	out = FilterQueueByStatus(mrs, false, "ready, claimed")
	if len(out) != 2 {
		t.Fatalf("expected 2 MRs, got %d", len(out))
	}
}

func TestFilterHistorySortsNewestFirstAndLimits(t *testing.T) {
	base := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	t1 := base.Add(-72 * time.Hour)
	t2 := base.Add(-48 * time.Hour)
	t3 := base.Add(-24 * time.Hour)
	mrs := []store.MergeRequest{
		mkMR("old", store.MRMerged, t1, "", &t1),
		mkMR("mid", store.MRMerged, t2, "", &t2),
		mkMR("new", store.MRMerged, t3, "", &t3),
	}

	out := FilterHistory(mrs, nil, nil, 0)
	if len(out) != 3 {
		t.Fatalf("expected 3 MRs, got %d", len(out))
	}
	if out[0].ID != "new" || out[1].ID != "mid" || out[2].ID != "old" {
		t.Errorf("expected newest-first order, got %s,%s,%s", out[0].ID, out[1].ID, out[2].ID)
	}

	// Limit=2 should keep the two newest.
	out = FilterHistory(mrs, nil, nil, 2)
	if len(out) != 2 {
		t.Fatalf("expected limit=2, got %d", len(out))
	}
	if out[0].ID != "new" || out[1].ID != "mid" {
		t.Errorf("expected newest two, got %s,%s", out[0].ID, out[1].ID)
	}
}

func TestFilterHistorySinceUntil(t *testing.T) {
	base := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	t1 := base.Add(-7 * 24 * time.Hour)
	t2 := base.Add(-3 * 24 * time.Hour)
	t3 := base.Add(-1 * time.Hour)
	mrs := []store.MergeRequest{
		mkMR("wk", store.MRMerged, t1, "", &t1),
		mkMR("3d", store.MRMerged, t2, "", &t2),
		mkMR("hr", store.MRMerged, t3, "", &t3),
	}

	since := base.Add(-4 * 24 * time.Hour)
	out := FilterHistory(mrs, &since, nil, 0)
	if len(out) != 2 {
		t.Fatalf("expected 2 MRs within last 4 days, got %d", len(out))
	}
	for _, mr := range out {
		if mr.ID == "wk" {
			t.Errorf("MR older than --since should be excluded")
		}
	}

	until := base.Add(-2 * 24 * time.Hour)
	out = FilterHistory(mrs, nil, &until, 0)
	// until=2d ago — only MRs merged on/before that should remain (wk, 3d).
	if len(out) != 2 {
		t.Fatalf("expected 2 MRs older than --until, got %d", len(out))
	}
	for _, mr := range out {
		if mr.ID == "hr" {
			t.Errorf("MR newer than --until should be excluded")
		}
	}
}

func TestHistoryTimestampPrefersMergedAt(t *testing.T) {
	updated := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	merged := time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC)

	mr := store.MergeRequest{UpdatedAt: updated, MergedAt: &merged}
	if got := HistoryTimestamp(mr); !got.Equal(merged) {
		t.Errorf("expected merged_at, got %v", got)
	}

	// Fallback to updated_at when MergedAt is nil (e.g. failed MR).
	mr = store.MergeRequest{UpdatedAt: updated}
	if got := HistoryTimestamp(mr); !got.Equal(updated) {
		t.Errorf("expected updated_at fallback, got %v", got)
	}
}
