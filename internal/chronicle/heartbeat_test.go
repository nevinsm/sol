package chronicle

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHeartbeatPath(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	got := HeartbeatPath()
	want := filepath.Join(solHome, "chronicle.heartbeat")
	if got != want {
		t.Errorf("HeartbeatPath() = %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, "chronicle.heartbeat") {
		t.Errorf("HeartbeatPath() should end with chronicle.heartbeat, got %q", got)
	}
}

func TestReadHeartbeat_NotFound(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	hb, err := ReadHeartbeat()
	if err != nil {
		t.Fatalf("ReadHeartbeat() with no file should return nil error, got %v", err)
	}
	if hb != nil {
		t.Error("ReadHeartbeat() with no file should return nil heartbeat")
	}
}

func TestWriteAndReadHeartbeat(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	now := time.Now().UTC().Round(time.Second)
	hb := &Heartbeat{
		Timestamp:        now,
		Status:           "running",
		EventsProcessed:  42,
		CheckpointOffset: 1024,
	}

	if err := WriteHeartbeat(hb); err != nil {
		t.Fatalf("WriteHeartbeat() error: %v", err)
	}

	got, err := ReadHeartbeat()
	if err != nil {
		t.Fatalf("ReadHeartbeat() error: %v", err)
	}
	if got == nil {
		t.Fatal("ReadHeartbeat() returned nil after write")
	}
	if got.Status != "running" {
		t.Errorf("Status: got %q, want %q", got.Status, "running")
	}
	if got.EventsProcessed != 42 {
		t.Errorf("EventsProcessed: got %d, want 42", got.EventsProcessed)
	}
	if got.CheckpointOffset != 1024 {
		t.Errorf("CheckpointOffset: got %d, want 1024", got.CheckpointOffset)
	}
	if !got.Timestamp.Equal(now) {
		t.Errorf("Timestamp: got %v, want %v", got.Timestamp, now)
	}
}

func TestHeartbeatIsStale(t *testing.T) {
	hb := &Heartbeat{
		Timestamp: time.Now().Add(-15 * time.Minute),
	}
	if !hb.IsStale(10 * time.Minute) {
		t.Error("heartbeat 15m old should be stale with 10m threshold")
	}

	hb.Timestamp = time.Now().Add(-5 * time.Minute)
	if hb.IsStale(10 * time.Minute) {
		t.Error("heartbeat 5m old should not be stale with 10m threshold")
	}
}
