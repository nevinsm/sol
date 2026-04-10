package cost

import (
	"encoding/json"
	"testing"
)

func TestCostSummaryJSON(t *testing.T) {
	costVal := 12.50
	cs := CostSummary{
		Worlds: []WorldCost{
			{
				World:        "sol-dev",
				Agents:       3,
				Writs:        10,
				InputTokens:  50000,
				OutputTokens: 25000,
				CacheTokens:  10000,
				Cost:         &costVal,
			},
		},
		TotalCost: &costVal,
		Period:    "24h",
	}

	data, err := json.Marshal(cs)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	worlds, ok := m["worlds"].([]any)
	if !ok || len(worlds) != 1 {
		t.Fatalf("worlds should be array with 1 element, got %v", m["worlds"])
	}

	if m["period"] != "24h" {
		t.Errorf("period = %v, want %q", m["period"], "24h")
	}
}

func TestModelCostJSON(t *testing.T) {
	costVal := 3.14
	mc := ModelCost{
		Model:               "opus",
		InputTokens:         1000,
		OutputTokens:        500,
		CacheReadTokens:     200,
		CacheCreationTokens: 100,
		Cost:                &costVal,
	}

	data, err := json.Marshal(mc)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if m["model"] != "opus" {
		t.Errorf("model = %v, want %q", m["model"], "opus")
	}
	if m["cache_read_tokens"].(float64) != 200 {
		t.Errorf("cache_read_tokens = %v, want 200", m["cache_read_tokens"])
	}
}

func TestCostSummaryEmptyWorlds(t *testing.T) {
	cs := CostSummary{
		Worlds: []WorldCost{},
		Period: "7d",
	}

	data, err := json.Marshal(cs)
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
