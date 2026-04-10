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
			{
				World: "sol-dev",
				Services: []ServiceStartResult{
					{Name: "forge", Started: true},
					{Name: "sentinel", Started: true},
				},
			},
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

	ws := services[0].(map[string]any)
	if ws["world"] != "sol-dev" {
		t.Errorf("world = %v, want sol-dev", ws["world"])
	}
	svcList := ws["services"].([]any)
	if len(svcList) != 2 {
		t.Errorf("services len = %d, want 2", len(svcList))
	}
}

func TestUpResultDaemonError(t *testing.T) {
	r := UpResult{
		SphereDaemons: []DaemonStartResult{
			{Name: "prefect", Started: false, Error: "port in use"},
		},
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

	d := m["sphere_daemons"].([]any)[0].(map[string]any)
	if d["error"] != "port in use" {
		t.Errorf("error = %v, want 'port in use'", d["error"])
	}
	if d["started"] != false {
		t.Errorf("started = %v, want false", d["started"])
	}
}

func TestUpResultOmitsEmptyError(t *testing.T) {
	r := DaemonStartResult{Name: "prefect", Started: true, PID: 123}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if _, ok := m["error"]; ok {
		t.Error("error field should be omitted when empty")
	}
}

func TestDownResultJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	r := DownResult{
		SphereDaemons: []DaemonStopResult{
			{Name: "prefect", Stopped: true, WasRunning: true},
		},
		WorldServices: []WorldServicesStopResult{
			{
				World: "sol-dev",
				Services: []ServiceStopResult{
					{Name: "forge", Stopped: true, WasRunning: true},
					{Name: "sentinel", Stopped: true, WasRunning: true},
				},
			},
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

	ws := m["world_services"].([]any)[0].(map[string]any)
	svcList := ws["services"].([]any)
	if len(svcList) != 2 {
		t.Errorf("services len = %d, want 2", len(svcList))
	}
}

func TestDownResultNotRunning(t *testing.T) {
	r := DownResult{
		SphereDaemons: []DaemonStopResult{
			{Name: "prefect", Stopped: false, WasRunning: false},
		},
		WorldServices: []WorldServicesStopResult{},
		StoppedAt:     time.Now().UTC(),
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	d := m["sphere_daemons"].([]any)[0].(map[string]any)
	if d["stopped"] != false {
		t.Errorf("stopped = %v, want false", d["stopped"])
	}
	if d["was_running"] != false {
		t.Errorf("was_running = %v, want false", d["was_running"])
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

func TestDownResultEmptyArrays(t *testing.T) {
	r := DownResult{
		SphereDaemons: []DaemonStopResult{},
		WorldServices: []WorldServicesStopResult{},
		StoppedAt:     time.Now().UTC(),
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if _, ok := m["sphere_daemons"].([]any); !ok {
		t.Error("sphere_daemons should be an empty JSON array")
	}
	if _, ok := m["world_services"].([]any); !ok {
		t.Error("world_services should be an empty JSON array")
	}
}
