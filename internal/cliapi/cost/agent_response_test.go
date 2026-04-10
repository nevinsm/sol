package cost

import (
	"encoding/json"
	"testing"
)

func TestAgentCostResponseJSON(t *testing.T) {
	costVal := 2.50
	resp := AgentCostResponse{
		World: "sol-dev",
		Agent: "Toast",
		Writs: []WritCost{
			{
				WritID:       "sol-abc123",
				Kind:         "code",
				Status:       "resolved",
				InputTokens:  8000,
				OutputTokens: 4000,
				CacheTokens:  1500,
				Cost:         &costVal,
			},
		},
		TotalCost: &costVal,
		Period:    "since 2024-01-01",
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
	if m["agent"] != "Toast" {
		t.Errorf("agent = %v, want %q", m["agent"], "Toast")
	}

	writs, ok := m["writs"].([]any)
	if !ok || len(writs) != 1 {
		t.Fatalf("writs should be array with 1 element, got %v", m["writs"])
	}

	writ := writs[0].(map[string]any)
	if writ["writ_id"] != "sol-abc123" {
		t.Errorf("writ_id = %v, want %q", writ["writ_id"], "sol-abc123")
	}
	if writ["kind"] != "code" {
		t.Errorf("kind = %v, want %q", writ["kind"], "code")
	}
	if writ["status"] != "resolved" {
		t.Errorf("status = %v, want %q", writ["status"], "resolved")
	}
}

func TestWritCostNilCost(t *testing.T) {
	wc := WritCost{
		WritID:       "sol-xyz",
		InputTokens:  500,
		OutputTokens: 250,
		Cost:         nil,
	}

	data, err := json.Marshal(wc)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// nil cost should serialize as null, not be omitted.
	if m["cost"] != nil {
		t.Errorf("cost should be null, got %v", m["cost"])
	}
}
