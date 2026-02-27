package deacon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteAndReadHeartbeat(t *testing.T) {
	gtHome := t.TempDir()

	hb := &Heartbeat{
		Timestamp:   time.Now().UTC().Truncate(time.Second),
		PatrolCount: 42,
		Status:      "running",
		StaleHooks:  3,
		ConvoyFeeds: 1,
		Escalations: 2,
	}

	if err := WriteHeartbeat(gtHome, hb); err != nil {
		t.Fatalf("WriteHeartbeat failed: %v", err)
	}

	// Verify file exists on disk.
	data, err := os.ReadFile(HeartbeatPath(gtHome))
	if err != nil {
		t.Fatalf("heartbeat file not found: %v", err)
	}

	var diskHB Heartbeat
	if err := json.Unmarshal(data, &diskHB); err != nil {
		t.Fatalf("failed to parse heartbeat JSON: %v", err)
	}
	if diskHB.PatrolCount != 42 {
		t.Errorf("patrol_count = %d, want 42", diskHB.PatrolCount)
	}

	// Read it back.
	got, err := ReadHeartbeat(gtHome)
	if err != nil {
		t.Fatalf("ReadHeartbeat failed: %v", err)
	}
	if got.PatrolCount != hb.PatrolCount {
		t.Errorf("PatrolCount = %d, want %d", got.PatrolCount, hb.PatrolCount)
	}
	if got.Status != hb.Status {
		t.Errorf("Status = %q, want %q", got.Status, hb.Status)
	}
	if got.StaleHooks != hb.StaleHooks {
		t.Errorf("StaleHooks = %d, want %d", got.StaleHooks, hb.StaleHooks)
	}
	if got.ConvoyFeeds != hb.ConvoyFeeds {
		t.Errorf("ConvoyFeeds = %d, want %d", got.ConvoyFeeds, hb.ConvoyFeeds)
	}
	if got.Escalations != hb.Escalations {
		t.Errorf("Escalations = %d, want %d", got.Escalations, hb.Escalations)
	}
}

func TestReadHeartbeatMissing(t *testing.T) {
	gtHome := t.TempDir()

	hb, err := ReadHeartbeat(gtHome)
	if err != nil {
		t.Fatalf("ReadHeartbeat should not error for missing file: %v", err)
	}
	if hb != nil {
		t.Errorf("expected nil heartbeat, got %+v", hb)
	}
}

func TestHeartbeatIsStale(t *testing.T) {
	threshold := 5 * time.Minute

	// Fresh heartbeat (1 minute ago) — not stale.
	fresh := &Heartbeat{Timestamp: time.Now().Add(-1 * time.Minute)}
	if fresh.IsStale(threshold) {
		t.Error("fresh heartbeat should not be stale")
	}

	// Old heartbeat (10 minutes ago) — stale.
	old := &Heartbeat{Timestamp: time.Now().Add(-10 * time.Minute)}
	if !old.IsStale(threshold) {
		t.Error("old heartbeat should be stale")
	}
}

func TestWriteHeartbeatCreatesDir(t *testing.T) {
	gtHome := t.TempDir()

	// Ensure deacon dir doesn't exist yet.
	deaconDir := filepath.Join(gtHome, "deacon")
	if _, err := os.Stat(deaconDir); err == nil {
		t.Fatal("deacon dir should not exist yet")
	}

	hb := &Heartbeat{
		Timestamp:   time.Now().UTC(),
		PatrolCount: 1,
		Status:      "running",
	}
	if err := WriteHeartbeat(gtHome, hb); err != nil {
		t.Fatalf("WriteHeartbeat failed: %v", err)
	}

	// Verify dir was created.
	if _, err := os.Stat(deaconDir); err != nil {
		t.Errorf("deacon dir should exist: %v", err)
	}
}
