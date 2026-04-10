package chronicle

import (
	"encoding/json"
	"testing"
)

func TestStatusResponse_Stopped(t *testing.T) {
	resp := StatusResponse{Status: "stopped"}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `{"status":"stopped"}`
	if string(data) != want {
		t.Errorf("got %s, want %s", data, want)
	}
}

func TestStatusResponse_StoppedWithCheckpoint(t *testing.T) {
	offset := int64(42)
	resp := StatusResponse{
		Status:           "stopped",
		CheckpointOffset: &offset,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got["status"] != "stopped" {
		t.Errorf("status = %v, want stopped", got["status"])
	}
	if got["checkpoint_offset"] != float64(42) {
		t.Errorf("checkpoint_offset = %v, want 42", got["checkpoint_offset"])
	}
	// pid, events_processed, heartbeat_age should be absent.
	for _, key := range []string{"pid", "events_processed", "heartbeat_age"} {
		if _, ok := got[key]; ok {
			t.Errorf("%s should be omitted when stopped", key)
		}
	}
}

func TestStatusResponse_Running(t *testing.T) {
	pid := 1234
	offset := int64(100)
	evts := int64(50)
	resp := StatusResponse{
		Status:           "running",
		PID:              &pid,
		CheckpointOffset: &offset,
		EventsProcessed:  &evts,
		HeartbeatAge:     "5s",
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
	if got["pid"] != float64(1234) {
		t.Errorf("pid = %v, want 1234", got["pid"])
	}
	if got["checkpoint_offset"] != float64(100) {
		t.Errorf("checkpoint_offset = %v, want 100", got["checkpoint_offset"])
	}
	if got["events_processed"] != float64(50) {
		t.Errorf("events_processed = %v, want 50", got["events_processed"])
	}
	if got["heartbeat_age"] != "5s" {
		t.Errorf("heartbeat_age = %v, want 5s", got["heartbeat_age"])
	}
}

func TestStatusResponse_RunningNoHeartbeat(t *testing.T) {
	pid := 5678
	resp := StatusResponse{
		Status: "running",
		PID:    &pid,
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
	if got["pid"] != float64(5678) {
		t.Errorf("pid = %v, want 5678", got["pid"])
	}
	// Optional fields should be absent.
	for _, key := range []string{"checkpoint_offset", "events_processed", "heartbeat_age"} {
		if _, ok := got[key]; ok {
			t.Errorf("%s should be omitted when not set", key)
		}
	}
}

func TestStatusResponse_ZeroCheckpointOffset(t *testing.T) {
	// A checkpoint offset of 0 is valid and should be present in JSON.
	offset := int64(0)
	resp := StatusResponse{
		Status:           "stopped",
		CheckpointOffset: &offset,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got["checkpoint_offset"] != float64(0) {
		t.Errorf("checkpoint_offset = %v, want 0", got["checkpoint_offset"])
	}
}

func TestStatusResponse_ZeroEventsProcessed(t *testing.T) {
	// events_processed of 0 is valid (heartbeat exists but no events yet).
	pid := 100
	evts := int64(0)
	resp := StatusResponse{
		Status:          "running",
		PID:             &pid,
		EventsProcessed: &evts,
		HeartbeatAge:    "1s",
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got["events_processed"] != float64(0) {
		t.Errorf("events_processed = %v, want 0", got["events_processed"])
	}
}
