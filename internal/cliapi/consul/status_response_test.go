package consul

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/consul"
)

func TestFromHeartbeat(t *testing.T) {
	ts := time.Date(2025, 3, 15, 10, 30, 0, 0, time.UTC)
	hb := &consul.Heartbeat{
		Timestamp:    ts,
		PatrolCount:  42,
		Status:       "running",
		StaleTethers: 3,
		CaravanFeeds: 7,
		Escalations:  2,
	}

	got := FromHeartbeat(hb, true, false, true)

	if got.Status != "running" {
		t.Errorf("Status = %q, want %q", got.Status, "running")
	}
	wantCheckedAt := time.Date(2025, 3, 15, 10, 30, 0, 0, time.UTC)
	if !got.CheckedAt.Equal(wantCheckedAt) {
		t.Errorf("CheckedAt = %v, want %v", got.CheckedAt, wantCheckedAt)
	}
	if got.PatrolCount != 42 {
		t.Errorf("PatrolCount = %d, want %d", got.PatrolCount, 42)
	}
	if got.StaleTethers != 3 {
		t.Errorf("StaleTethers = %d, want %d", got.StaleTethers, 3)
	}
	if got.CaravanFeeds != 7 {
		t.Errorf("CaravanFeeds = %d, want %d", got.CaravanFeeds, 7)
	}
	if got.Escalations != 2 {
		t.Errorf("Escalations = %d, want %d", got.Escalations, 2)
	}
	if got.Stale != true {
		t.Errorf("Stale = %v, want %v", got.Stale, true)
	}
	if got.PIDGone != false {
		t.Errorf("PIDGone = %v, want %v", got.PIDGone, false)
	}
	if got.Wedged != true {
		t.Errorf("Wedged = %v, want %v", got.Wedged, true)
	}
}

// TestFromHeartbeat_JSONShape verifies that the struct produces normalized
// snake_case JSON keys including the renamed "checked_at" field.
func TestFromHeartbeat_JSONShape(t *testing.T) {
	ts := time.Date(2025, 3, 15, 10, 30, 0, 0, time.UTC)
	hb := &consul.Heartbeat{
		Timestamp:    ts,
		PatrolCount:  42,
		Status:       "running",
		StaleTethers: 3,
		CaravanFeeds: 7,
		Escalations:  2,
	}

	resp := FromHeartbeat(hb, true, false, true)

	gotBytes, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal StatusResponse: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(gotBytes, &got); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify normalized key names are present.
	for _, key := range []string{"status", "checked_at", "patrol_count", "stale_tethers", "caravan_feeds", "escalations", "stale", "pid_gone", "wedged"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing expected JSON key %q", key)
		}
	}
	// The old "timestamp" key must not appear.
	if _, ok := got["timestamp"]; ok {
		t.Error("old key \"timestamp\" should not be present; expected \"checked_at\"")
	}
	// Verify the value is RFC3339 UTC.
	if got["checked_at"] != "2025-03-15T10:30:00Z" {
		t.Errorf("checked_at = %v, want 2025-03-15T10:30:00Z", got["checked_at"])
	}
}

// TestFromHeartbeat_AllFalseFlags verifies zero-value bools are not omitted.
func TestFromHeartbeat_AllFalseFlags(t *testing.T) {
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	hb := &consul.Heartbeat{
		Timestamp: ts,
		Status:    "running",
	}

	resp := FromHeartbeat(hb, false, false, false)

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Verify all bool fields are present (not omitted).
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	for _, key := range []string{"stale", "pid_gone", "wedged"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected key %q in JSON output, but it was missing", key)
		}
	}
}
