package chronicle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHeartbeatPath(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	got := HeartbeatPath()
	want := filepath.Join(solHome, ".runtime", "chronicle-heartbeat.json")
	if got != want {
		t.Errorf("HeartbeatPath() = %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, "chronicle-heartbeat.json") {
		t.Errorf("HeartbeatPath() should end with chronicle-heartbeat.json, got %q", got)
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
	if err := os.MkdirAll(filepath.Join(solHome, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create .runtime dir: %v", err)
	}

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

func TestWriteHeartbeatCreatesDir(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Ensure .runtime dir doesn't exist yet.
	runtimeDir := filepath.Join(solHome, ".runtime")
	if _, err := os.Stat(runtimeDir); err == nil {
		t.Fatal(".runtime dir should not exist yet")
	}

	hb := &Heartbeat{
		Timestamp:        time.Now().UTC(),
		Status:           "running",
		EventsProcessed:  1,
		CheckpointOffset: 0,
	}
	if err := WriteHeartbeat(hb); err != nil {
		t.Fatalf("WriteHeartbeat failed: %v", err)
	}

	// Verify dir was created.
	if _, err := os.Stat(runtimeDir); err != nil {
		t.Errorf(".runtime dir should exist: %v", err)
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
