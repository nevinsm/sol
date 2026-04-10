package ledger

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
	reqTotal := int64(100)
	tokensProc := int64(5000)
	worldsW := 3
	resp := StatusResponse{
		Status:          "running",
		PID:             1234,
		Port:            4318,
		HeartbeatAge:    "5s",
		RequestsTotal:   &reqTotal,
		TokensProcessed: &tokensProc,
		WorldsWritten:   &worldsW,
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
	if got["port"] != float64(4318) {
		t.Errorf("port = %v, want 4318", got["port"])
	}
	if got["heartbeat_age"] != "5s" {
		t.Errorf("heartbeat_age = %v, want 5s", got["heartbeat_age"])
	}
	if got["requests_total"] != float64(100) {
		t.Errorf("requests_total = %v, want 100", got["requests_total"])
	}
	if got["tokens_processed"] != float64(5000) {
		t.Errorf("tokens_processed = %v, want 5000", got["tokens_processed"])
	}
	if got["worlds_written"] != float64(3) {
		t.Errorf("worlds_written = %v, want 3", got["worlds_written"])
	}
}

func TestStatusResponse_RunningNoHeartbeat(t *testing.T) {
	resp := StatusResponse{
		Status: "running",
		PID:    5678,
		Port:   4318,
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
	// Heartbeat fields should be omitted when nil.
	for _, key := range []string{"heartbeat_age", "requests_total", "tokens_processed", "worlds_written"} {
		if _, ok := got[key]; ok {
			t.Errorf("%s should be omitted when heartbeat is absent", key)
		}
	}
}

func TestStatusResponse_StoppedOmitsPIDAndPort(t *testing.T) {
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
	if _, ok := got["port"]; ok {
		t.Error("port should be omitted when stopped")
	}
}

func TestStatusResponse_HeartbeatZeroValues(t *testing.T) {
	// When heartbeat is present but counters are zero, they should still appear.
	reqTotal := int64(0)
	tokensProc := int64(0)
	worldsW := 0
	resp := StatusResponse{
		Status:          "running",
		PID:             1234,
		Port:            4318,
		HeartbeatAge:    "0s",
		RequestsTotal:   &reqTotal,
		TokensProcessed: &tokensProc,
		WorldsWritten:   &worldsW,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Zero-value pointers should still be serialized (not omitted).
	for _, key := range []string{"requests_total", "tokens_processed", "worlds_written"} {
		if _, ok := got[key]; !ok {
			t.Errorf("%s should be present even when zero", key)
		}
	}
}
