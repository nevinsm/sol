package broker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadProviderHealthNil(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// No heartbeat — returns nil.
	info, err := ReadProviderHealth()
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Error("expected nil when no heartbeat")
	}
}

func TestReadProviderHealthAllOK(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	runtimeDir := filepath.Join(solHome, ".runtime")
	os.MkdirAll(runtimeDir, 0o755)

	now := time.Now().UTC()
	hb := Heartbeat{
		Timestamp:   now,
		PatrolCount: 3,
		Status:      "running",
		Runtimes: []RuntimeLiveness{
			{Runtime: "claude", OK: true, LastProbe: now},
		},
	}
	data, _ := json.MarshalIndent(hb, "", "  ")
	os.WriteFile(filepath.Join(runtimeDir, "broker-heartbeat.json"), append(data, '\n'), 0o644)

	info, err := ReadProviderHealth()
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected non-nil health info")
	}
	if info.Health != HealthHealthy {
		t.Errorf("expected healthy, got %s", info.Health)
	}
	if info.Stale {
		t.Error("heartbeat should not be stale")
	}
}

func TestReadProviderHealthRuntimeDown(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	runtimeDir := filepath.Join(solHome, ".runtime")
	os.MkdirAll(runtimeDir, 0o755)

	now := time.Now().UTC()
	hb := Heartbeat{
		Timestamp:   now,
		PatrolCount: 5,
		Status:      "running",
		Runtimes: []RuntimeLiveness{
			{Runtime: "claude", OK: true, LastProbe: now},
			{Runtime: "codex", OK: false, LastProbe: now},
		},
	}
	data, _ := json.MarshalIndent(hb, "", "  ")
	os.WriteFile(filepath.Join(runtimeDir, "broker-heartbeat.json"), append(data, '\n'), 0o644)

	info, err := ReadProviderHealth()
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected non-nil health info")
	}
	if info.Health != HealthDown {
		t.Errorf("expected down (any failing runtime), got %s", info.Health)
	}
}

func TestReadProviderHealthStale(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	runtimeDir := filepath.Join(solHome, ".runtime")
	os.MkdirAll(runtimeDir, 0o755)

	// Write a stale heartbeat (15 min old).
	old := time.Now().UTC().Add(-15 * time.Minute)
	hb := map[string]any{
		"timestamp":    old.Format(time.RFC3339),
		"patrol_count": 1,
		"status":       "running",
	}
	data, _ := json.Marshal(hb)
	os.WriteFile(filepath.Join(runtimeDir, "broker-heartbeat.json"), data, 0o644)

	info, err := ReadProviderHealth()
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected non-nil health info")
	}
	if !info.Stale {
		t.Error("expected stale=true for 15-min-old heartbeat with 10-min threshold")
	}
}
