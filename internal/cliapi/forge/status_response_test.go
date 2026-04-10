package forge

import (
	"encoding/json"
	"testing"
	"time"
)

func TestForgeStatusResponse_JSONShape(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	resp := ForgeStatusResponse{
		World:   "sol-dev",
		Running: true,
		Paused:  false,
		PID:     1234,
		Merging: true,
		Ready:   3,
		Blocked: 1,
		InProgress: 1,
		Failed:  0,
		Merged:  5,
		Total:   10,
		ClaimedMR: &ForgeStatusMR{
			ID:     "mr-0000000000000001",
			WritID: "sol-a1b2c3d4e5f6a7b8",
			Title:  "Add feature X",
			Branch: "outpost/Nova/sol-a1b2c3d4e5f6a7b8",
			Age:    "2m30s",
		},
		LastMerge: &ForgeStatusEvent{
			MRID:      "mr-0000000000000002",
			Title:     "Fix bug Y",
			Branch:    "outpost/Toast/sol-b2c3d4e5f6a7b8c9",
			Timestamp: now,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify top-level fields.
	if got["world"] != "sol-dev" {
		t.Errorf("world = %v, want sol-dev", got["world"])
	}
	if got["running"] != true {
		t.Errorf("running = %v, want true", got["running"])
	}
	if got["pid"] != float64(1234) {
		t.Errorf("pid = %v, want 1234", got["pid"])
	}
	if got["ready"] != float64(3) {
		t.Errorf("ready = %v, want 3", got["ready"])
	}
	if got["in_progress"] != float64(1) {
		t.Errorf("in_progress = %v, want 1", got["in_progress"])
	}
	if got["total"] != float64(10) {
		t.Errorf("total = %v, want 10", got["total"])
	}

	// Verify nested claimed_mr.
	claimed, ok := got["claimed_mr"].(map[string]any)
	if !ok {
		t.Fatal("claimed_mr not present or wrong type")
	}
	if claimed["id"] != "mr-0000000000000001" {
		t.Errorf("claimed_mr.id = %v, want mr-0000000000000001", claimed["id"])
	}
	if claimed["writ_id"] != "sol-a1b2c3d4e5f6a7b8" {
		t.Errorf("claimed_mr.writ_id = %v, want sol-a1b2c3d4e5f6a7b8", claimed["writ_id"])
	}

	// Verify nested last_merge.
	lastMerge, ok := got["last_merge"].(map[string]any)
	if !ok {
		t.Fatal("last_merge not present or wrong type")
	}
	if lastMerge["mr_id"] != "mr-0000000000000002" {
		t.Errorf("last_merge.mr_id = %v, want mr-0000000000000002", lastMerge["mr_id"])
	}
}

func TestForgeStatusResponse_OmitsNullOptionals(t *testing.T) {
	resp := ForgeStatusResponse{
		World:   "sol-dev",
		Running: false,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// pid, merging, claimed_mr, last_merge, last_failure should be omitted.
	for _, key := range []string{"pid", "merging", "claimed_mr", "last_merge", "last_failure"} {
		if _, ok := got[key]; ok {
			t.Errorf("%s should be omitted when zero/nil", key)
		}
	}

	// Zero-value ints (ready, blocked, etc.) should be present.
	for _, key := range []string{"ready", "blocked", "in_progress", "failed", "merged", "total"} {
		if _, ok := got[key]; !ok {
			t.Errorf("%s should be present even when zero", key)
		}
	}
}
