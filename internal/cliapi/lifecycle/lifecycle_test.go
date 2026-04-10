package lifecycle

import (
	"encoding/json"
	"testing"
	"time"
)

func TestUpResultJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	r := UpResult{
		SphereDaemons: []DaemonStartResult{
			{Name: "prefect", Started: true, PID: 1234},
			{Name: "consul", Started: false, AlreadyRunning: true},
		},
		WorldServices: []WorldServicesResult{
			{World: "sol-dev", Forge: true, Sentinel: true},
		},
		StartedAt: now,
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	daemons := m["sphere_daemons"].([]any)
	if len(daemons) != 2 {
		t.Errorf("sphere_daemons len = %d, want 2", len(daemons))
	}

	services := m["world_services"].([]any)
	if len(services) != 1 {
		t.Errorf("world_services len = %d, want 1", len(services))
	}
}

func TestDownResultJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	r := DownResult{
		SphereDaemons: []DaemonStopResult{
			{Name: "prefect", Stopped: true, WasRunning: true},
		},
		WorldServices: []WorldServicesStopResult{
			{World: "sol-dev", Forge: true, Sentinel: true},
		},
		StoppedAt: now,
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	daemons := m["sphere_daemons"].([]any)
	if len(daemons) != 1 {
		t.Errorf("sphere_daemons len = %d, want 1", len(daemons))
	}

	d := daemons[0].(map[string]any)
	if d["stopped"] != true {
		t.Errorf("stopped = %v, want true", d["stopped"])
	}
	if d["was_running"] != true {
		t.Errorf("was_running = %v, want true", d["was_running"])
	}
}

func TestUpResultEmptyArrays(t *testing.T) {
	r := UpResult{
		SphereDaemons: []DaemonStartResult{},
		WorldServices: []WorldServicesResult{},
		StartedAt:     time.Now().UTC(),
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Empty arrays should be present, not null.
	if _, ok := m["sphere_daemons"].([]any); !ok {
		t.Error("sphere_daemons should be an empty JSON array")
	}
	if _, ok := m["world_services"].([]any); !ok {
		t.Error("world_services should be an empty JSON array")
	}
}
