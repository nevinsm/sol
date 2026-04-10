package cost

import (
	"encoding/json"
	"testing"
)

func TestWorldCostResponseJSON(t *testing.T) {
	costVal := 5.25
	resp := WorldCostResponse{
		World: "sol-dev",
		Agents: []AgentCost{
			{
				Agent:        "Toast",
				Writs:        3,
				InputTokens:  10000,
				OutputTokens: 5000,
				CacheTokens:  2000,
				Cost:         &costVal,
			},
		},
		TotalCost: &costVal,
		Period:    "all time",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if m["world"] != "sol-dev" {
		t.Errorf("world = %v, want %q", m["world"], "sol-dev")
	}

	agents, ok := m["agents"].([]any)
	if !ok || len(agents) != 1 {
		t.Fatalf("agents should be array with 1 element, got %v", m["agents"])
	}

	agent := agents[0].(map[string]any)
	if agent["agent"] != "Toast" {
		t.Errorf("agent = %v, want %q", agent["agent"], "Toast")
	}
	if agent["writs"].(float64) != 3 {
		t.Errorf("writs = %v, want 3", agent["writs"])
	}
}

func TestWorldCostResponseEmptyAgents(t *testing.T) {
	resp := WorldCostResponse{
		World:  "empty-world",
		Agents: []AgentCost{},
		Period: "7d",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	agents, ok := m["agents"].([]any)
	if !ok {
		t.Fatal("agents should be a JSON array")
	}
	if len(agents) != 0 {
		t.Errorf("agents len = %d, want 0", len(agents))
	}

	// total_cost should be null when nil (not omitted — matches existing JSON shape).
	if _, ok := m["total_cost"]; !ok {
		t.Error("total_cost should be present (as null) when nil")
	} else if m["total_cost"] != nil {
		t.Errorf("total_cost should be null, got %v", m["total_cost"])
	}
}
