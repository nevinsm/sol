package worlds

import (
	"encoding/json"
	"testing"
)

func TestWorldListItemJSON(t *testing.T) {
	item := WorldListItem{
		Name:       "sol-dev",
		State:      "active",
		Health:     "healthy",
		Agents:     3,
		Queue:      5,
		SourceRepo: "https://github.com/nevinsm/sol",
		CreatedAt:  "2025-01-15T10:30:00Z",
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Verify snake_case keys.
	expectedKeys := []string{"name", "state", "health", "agents", "queue", "source_repo", "created_at"}
	for _, k := range expectedKeys {
		if _, ok := got[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}

	if got["name"] != "sol-dev" {
		t.Errorf("name = %v, want %q", got["name"], "sol-dev")
	}
	if got["state"] != "active" {
		t.Errorf("state = %v, want %q", got["state"], "active")
	}
	if got["agents"].(float64) != 3 {
		t.Errorf("agents = %v, want 3", got["agents"])
	}
}

func TestWorldListItemEmptyArray(t *testing.T) {
	items := []WorldListItem{}
	data, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("empty array = %s, want []", string(data))
	}
}
