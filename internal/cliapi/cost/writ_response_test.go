package cost

import (
	"encoding/json"
	"testing"
)

func TestWritCostResponseJSON(t *testing.T) {
	costVal := 1.75
	resp := WritCostResponse{
		WritID: "sol-abc123",
		Title:  "Test writ",
		Kind:   "code",
		Status: "open",
		Models: []ModelCost{
			{
				Model:               "claude-sonnet-4-6",
				InputTokens:         5000,
				OutputTokens:        2500,
				CacheReadTokens:     1000,
				CacheCreationTokens: 500,
				Cost:                &costVal,
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

	if m["writ_id"] != "sol-abc123" {
		t.Errorf("writ_id = %v, want %q", m["writ_id"], "sol-abc123")
	}
	if m["title"] != "Test writ" {
		t.Errorf("title = %v, want %q", m["title"], "Test writ")
	}

	models, ok := m["models"].([]any)
	if !ok || len(models) != 1 {
		t.Fatalf("models should be array with 1 element, got %v", m["models"])
	}

	model := models[0].(map[string]any)
	if model["model"] != "claude-sonnet-4-6" {
		t.Errorf("model = %v, want %q", model["model"], "claude-sonnet-4-6")
	}
}

func TestWritCostResponseOmitsEmptyOptionalFields(t *testing.T) {
	resp := WritCostResponse{
		WritID: "sol-xyz",
		Models: []ModelCost{},
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

	// title, kind, status should be omitted when empty.
	for _, field := range []string{"title", "kind", "status"} {
		if _, ok := m[field]; ok {
			t.Errorf("%s should be omitted when empty", field)
		}
	}

	// models should be present as empty array.
	models, ok := m["models"].([]any)
	if !ok {
		t.Fatal("models should be a JSON array")
	}
	if len(models) != 0 {
		t.Errorf("models len = %d, want 0", len(models))
	}
}
