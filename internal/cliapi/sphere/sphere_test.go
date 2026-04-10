package sphere

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSphereStatusJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	s := SphereStatus{
		SOLHome: "/home/user/sol",
		Health:  "healthy",
		Prefect: ProcessInfo{Running: true, PID: 1234},
		Consul:  ProcessInfo{Running: true, PID: 1235},
		Worlds: []WorldStatus{
			{
				Name:   "sol-dev",
				Health: "healthy",
				Agents: 3,
				Working: 2,
				Idle:    1,
				Forge:  true,
			},
		},
		Caravans: []CaravanSummary{
			{
				ID:          "car-001",
				Name:        "test-caravan",
				Status:      "open",
				ItemsTotal:  5,
				ItemsMerged: 2,
				CreatedAt:   now,
			},
		},
		Escalations: &EscalationCount{
			Total:      3,
			BySeverity: map[string]int{"high": 1, "medium": 2},
		},
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if m["sol_home"] != "/home/user/sol" {
		t.Errorf("sol_home = %v, want %q", m["sol_home"], "/home/user/sol")
	}
	if m["health"] != "healthy" {
		t.Errorf("health = %v, want %q", m["health"], "healthy")
	}

	prefect := m["prefect"].(map[string]any)
	if prefect["running"] != true {
		t.Error("prefect.running = false, want true")
	}

	worlds := m["worlds"].([]any)
	if len(worlds) != 1 {
		t.Errorf("worlds len = %d, want 1", len(worlds))
	}
}

func TestWorldStatusJSON(t *testing.T) {
	ws := WorldStatus{
		Name:     "sol-dev",
		Health:   "degraded",
		Agents:   5,
		Working:  3,
		Idle:     1,
		Stalled:  1,
		Forge:    true,
		Sentinel: true,
		MRReady:  2,
		MRFailed: 1,
	}

	data, err := json.Marshal(ws)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if m["health"] != "degraded" {
		t.Errorf("health = %v, want %q", m["health"], "degraded")
	}
	if m["mr_ready"].(float64) != 2 {
		t.Errorf("mr_ready = %v, want 2", m["mr_ready"])
	}
}

func TestSphereStatusEmptyWorldsArray(t *testing.T) {
	s := SphereStatus{
		Worlds: []WorldStatus{},
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	worlds, ok := m["worlds"].([]any)
	if !ok {
		t.Fatal("worlds should be a JSON array")
	}
	if len(worlds) != 0 {
		t.Errorf("worlds len = %d, want 0", len(worlds))
	}
}
