package broker

import (
	"encoding/json"
	"testing"
	"time"

	ibroker "github.com/nevinsm/sol/internal/broker"
)

func TestStatusResponse_MinimalFields(t *testing.T) {
	resp := StatusResponse{
		Status:      "running",
		CheckedAt:   time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		PatrolCount: 5,
		Stale:       false,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got["status"] != "running" {
		t.Errorf("status = %v, want running", got["status"])
	}
	if got["checked_at"] != "2025-01-15T10:30:00Z" {
		t.Errorf("checked_at = %v, want 2025-01-15T10:30:00Z", got["checked_at"])
	}
	if got["patrol_count"] != float64(5) {
		t.Errorf("patrol_count = %v, want 5", got["patrol_count"])
	}
	if got["stale"] != false {
		t.Errorf("stale = %v, want false", got["stale"])
	}

	// Optional runtimes field should be omitted when not set.
	if _, ok := got["runtimes"]; ok {
		t.Error("runtimes should be omitted when nil")
	}
}

func TestStatusResponse_WithRuntimes(t *testing.T) {
	probeAt := time.Date(2025, 1, 15, 10, 29, 0, 0, time.UTC)
	resp := StatusResponse{
		Status:      "running",
		CheckedAt:   time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		PatrolCount: 42,
		Stale:       false,
		Runtimes: []RuntimeEntry{
			{Runtime: "claude", OK: true, LastProbe: &probeAt},
			{Runtime: "codex", OK: false},
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

	runtimes, ok := got["runtimes"].([]any)
	if !ok || len(runtimes) != 2 {
		t.Fatalf("runtimes = %v, want 2 entries", got["runtimes"])
	}

	r0 := runtimes[0].(map[string]any)
	if r0["runtime"] != "claude" || r0["ok"] != true {
		t.Errorf("runtimes[0] = %v, want claude ok=true", r0)
	}

	r1 := runtimes[1].(map[string]any)
	if r1["runtime"] != "codex" || r1["ok"] != false {
		t.Errorf("runtimes[1] = %v, want codex ok=false", r1)
	}
}

func TestFromHeartbeat_Empty(t *testing.T) {
	hb := &ibroker.Heartbeat{
		Status:      "running",
		Timestamp:   time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		PatrolCount: 5,
	}

	resp := FromHeartbeat(hb, 10*time.Minute)

	if resp.Status != "running" {
		t.Errorf("Status = %q, want %q", resp.Status, "running")
	}
	want := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	if !resp.CheckedAt.Equal(want) {
		t.Errorf("CheckedAt = %v, want %v", resp.CheckedAt, want)
	}
	if resp.PatrolCount != 5 {
		t.Errorf("PatrolCount = %d, want 5", resp.PatrolCount)
	}
	if resp.Runtimes != nil {
		t.Error("Runtimes should be nil when empty")
	}
}

func TestFromHeartbeat_WithRuntimes(t *testing.T) {
	probeAt := time.Date(2025, 1, 15, 10, 29, 0, 0, time.UTC)
	hb := &ibroker.Heartbeat{
		Status:      "running",
		Timestamp:   time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		PatrolCount: 10,
		Runtimes: []ibroker.RuntimeLiveness{
			{Runtime: "claude", OK: true, LastProbe: probeAt},
			{Runtime: "codex", OK: false},
		},
	}

	resp := FromHeartbeat(hb, 10*time.Minute)

	if len(resp.Runtimes) != 2 {
		t.Fatalf("len(Runtimes) = %d, want 2", len(resp.Runtimes))
	}

	r0 := resp.Runtimes[0]
	if r0.Runtime != "claude" || !r0.OK {
		t.Errorf("Runtimes[0] = %+v, want claude ok=true", r0)
	}
	if r0.LastProbe == nil {
		t.Error("Runtimes[0].LastProbe should be set")
	}

	r1 := resp.Runtimes[1]
	if r1.Runtime != "codex" || r1.OK {
		t.Errorf("Runtimes[1] = %+v, want codex ok=false", r1)
	}
	if r1.LastProbe != nil {
		t.Errorf("Runtimes[1].LastProbe should be nil when zero, got %v", r1.LastProbe)
	}
}

func TestFromHeartbeat_JSONShape(t *testing.T) {
	probeAt := time.Date(2025, 1, 15, 10, 29, 0, 0, time.UTC)
	hb := &ibroker.Heartbeat{
		Status:      "running",
		Timestamp:   time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		PatrolCount: 7,
		Runtimes: []ibroker.RuntimeLiveness{
			{Runtime: "claude", OK: true, LastProbe: probeAt},
		},
	}

	resp := FromHeartbeat(hb, 10*time.Minute)
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Required fields always present.
	for _, key := range []string{"status", "checked_at", "patrol_count", "stale"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing required field %q", key)
		}
	}

	// runtimes present (non-empty).
	if _, ok := got["runtimes"]; !ok {
		t.Error("runtimes should be present when set")
	}
}
