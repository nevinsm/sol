package cost

import (
	"encoding/json"
	"testing"
)

func TestCaravanCostResponseJSON(t *testing.T) {
	costVal := 8.50
	resp := CaravanCostResponse{
		CaravanID:   "cv-abc123",
		CaravanName: "test-caravan",
		Writs: []CaravanWritCost{
			{
				WritID:       "sol-abc",
				World:        "sol-dev",
				Phase:        1,
				Kind:         "code",
				Status:       "resolved",
				InputTokens:  20000,
				OutputTokens: 10000,
				CacheTokens:  5000,
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

	if m["caravan_id"] != "cv-abc123" {
		t.Errorf("caravan_id = %v, want %q", m["caravan_id"], "cv-abc123")
	}
	if m["caravan_name"] != "test-caravan" {
		t.Errorf("caravan_name = %v, want %q", m["caravan_name"], "test-caravan")
	}

	writs, ok := m["writs"].([]any)
	if !ok || len(writs) != 1 {
		t.Fatalf("writs should be array with 1 element, got %v", m["writs"])
	}

	writ := writs[0].(map[string]any)
	if writ["writ_id"] != "sol-abc" {
		t.Errorf("writ_id = %v, want %q", writ["writ_id"], "sol-abc")
	}
	if writ["phase"].(float64) != 1 {
		t.Errorf("phase = %v, want 1", writ["phase"])
	}
	if writ["world"] != "sol-dev" {
		t.Errorf("world = %v, want %q", writ["world"], "sol-dev")
	}
}

func TestCaravanCostResponseEmptyWrits(t *testing.T) {
	resp := CaravanCostResponse{
		CaravanID:   "cv-xyz",
		CaravanName: "empty-caravan",
		Writs:       []CaravanWritCost{},
		Period:      "since 2024-01-01",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	writs, ok := m["writs"].([]any)
	if !ok {
		t.Fatal("writs should be a JSON array")
	}
	if len(writs) != 0 {
		t.Errorf("writs len = %d, want 0", len(writs))
	}

	// total_cost should be null when nil (not omitted — matches existing JSON shape).
	if _, ok := m["total_cost"]; !ok {
		t.Error("total_cost should be present (as null) when nil")
	} else if m["total_cost"] != nil {
		t.Errorf("total_cost should be null, got %v", m["total_cost"])
	}
}
