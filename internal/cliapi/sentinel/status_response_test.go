package sentinel

import (
	"encoding/json"
	"testing"
)

func TestStatusResponse_Stopped(t *testing.T) {
	resp := StatusResponse{
		World:   "myworld",
		Running: false,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `{"world":"myworld","running":false}`
	if string(data) != want {
		t.Errorf("got %s, want %s", data, want)
	}
}

func TestStatusResponse_Running(t *testing.T) {
	resp := StatusResponse{
		World:         "myworld",
		Running:       true,
		PID:           1234,
		PatrolCount:   42,
		AgentsChecked: 3,
		StalledCount:  1,
		ReapedCount:   2,
		HeartbeatAge:  "5s",
		Status:        "healthy",
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got["world"] != "myworld" {
		t.Errorf("world = %v, want myworld", got["world"])
	}
	if got["running"] != true {
		t.Errorf("running = %v, want true", got["running"])
	}
	if got["pid"] != float64(1234) {
		t.Errorf("pid = %v, want 1234", got["pid"])
	}
	if got["patrol_count"] != float64(42) {
		t.Errorf("patrol_count = %v, want 42", got["patrol_count"])
	}
	if got["agents_checked"] != float64(3) {
		t.Errorf("agents_checked = %v, want 3", got["agents_checked"])
	}
	if got["stalled_count"] != float64(1) {
		t.Errorf("stalled_count = %v, want 1", got["stalled_count"])
	}
	if got["reaped_count"] != float64(2) {
		t.Errorf("reaped_count = %v, want 2", got["reaped_count"])
	}
	if got["heartbeat_age"] != "5s" {
		t.Errorf("heartbeat_age = %v, want 5s", got["heartbeat_age"])
	}
	if got["status"] != "healthy" {
		t.Errorf("status = %v, want healthy", got["status"])
	}
}

func TestStatusResponse_StoppedOmitsOptionalFields(t *testing.T) {
	resp := StatusResponse{
		World:   "myworld",
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

	for _, field := range []string{"pid", "patrol_count", "agents_checked", "stalled_count", "reaped_count", "heartbeat_age", "status"} {
		if _, ok := got[field]; ok {
			t.Errorf("%s should be omitted when zero/empty", field)
		}
	}
}

func TestStatusResponse_RunningNoHeartbeat(t *testing.T) {
	resp := StatusResponse{
		World:   "myworld",
		Running: true,
		PID:     5678,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got["pid"] != float64(5678) {
		t.Errorf("pid = %v, want 5678", got["pid"])
	}
	// Heartbeat fields should be omitted when no heartbeat data.
	for _, field := range []string{"patrol_count", "agents_checked", "stalled_count", "reaped_count", "heartbeat_age", "status"} {
		if _, ok := got[field]; ok {
			t.Errorf("%s should be omitted when no heartbeat data", field)
		}
	}
}
