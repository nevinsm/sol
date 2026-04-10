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
	if got.Timestamp != "2025-03-15T10:30:00Z" {
		t.Errorf("Timestamp = %q, want %q", got.Timestamp, "2025-03-15T10:30:00Z")
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

// TestFromHeartbeat_JSONShape verifies that the struct produces byte-equivalent
// JSON to the pre-migration map[string]any output.
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

	stale := true
	pidGone := false
	wedged := true

	// Build the pre-migration map[string]any output.
	legacy := map[string]any{
		"status":        hb.Status,
		"timestamp":     hb.Timestamp.Format(time.RFC3339),
		"patrol_count":  hb.PatrolCount,
		"stale_tethers": hb.StaleTethers,
		"caravan_feeds": hb.CaravanFeeds,
		"escalations":   hb.Escalations,
		"stale":         stale,
		"pid_gone":      pidGone,
		"wedged":        wedged,
	}

	resp := FromHeartbeat(hb, stale, pidGone, wedged)

	gotBytes, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal StatusResponse: %v", err)
	}
	wantBytes, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("failed to marshal legacy map: %v", err)
	}

	if string(gotBytes) != string(wantBytes) {
		t.Errorf("JSON mismatch:\n  got:  %s\n  want: %s", gotBytes, wantBytes)
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
