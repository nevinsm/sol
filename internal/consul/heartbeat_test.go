package consul

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
		StaleTethers: 3,
		CaravanFeeds: 1,
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
	if got.StaleTethers != hb.StaleTethers {
		t.Errorf("StaleTethers = %d, want %d", got.StaleTethers, hb.StaleTethers)
	}
	if got.CaravanFeeds != hb.CaravanFeeds {
		t.Errorf("CaravanFeeds = %d, want %d", got.CaravanFeeds, hb.CaravanFeeds)
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

	// Ensure consul dir doesn't exist yet.
	consulDir := filepath.Join(gtHome, "consul")
	if _, err := os.Stat(consulDir); err == nil {
		t.Fatal("consul dir should not exist yet")
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
	if _, err := os.Stat(consulDir); err != nil {
		t.Errorf("consul dir should exist: %v", err)
	}
}
