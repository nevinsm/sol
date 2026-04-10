package prefect

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

func TestStatusResponse_Running(t *testing.T) {
	resp := StatusResponse{
		Status:        "running",
		PID:           1234,
		UptimeSeconds: 3600,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Unmarshal back to verify fields.
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
	if got["uptime_seconds"] != float64(3600) {
		t.Errorf("uptime_seconds = %v, want 3600", got["uptime_seconds"])
	}
}

func TestStatusResponse_RunningNoUptime(t *testing.T) {
	resp := StatusResponse{
		Status: "running",
		PID:    5678,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// uptime_seconds should be omitted when zero.
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := got["uptime_seconds"]; ok {
		t.Error("uptime_seconds should be omitted when zero")
	}
}

func TestStatusResponse_StoppedOmitsPIDAndUptime(t *testing.T) {
	resp := StatusResponse{Status: "stopped"}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := got["pid"]; ok {
		t.Error("pid should be omitted when stopped")
	}
	if _, ok := got["uptime_seconds"]; ok {
		t.Error("uptime_seconds should be omitted when stopped")
	}
}
