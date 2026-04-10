package agents

import (
	"encoding/json"
	"testing"
)

func TestAgentListRowJSON(t *testing.T) {
	row := AgentListRow{
		ID:         "sol-dev/Nova",
		Name:       "Nova",
		World:      "sol-dev",
		Role:       "outpost",
		State:      "working",
		ActiveWrit: "sol-a1b2c3d4e5f6a7b8",
		Model:      "opus",
		Account:    "primary",
		LastSeen:   "2s ago",
	}

	data, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Verify the JSON field names match the pre-migration shape exactly.
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	expectedFields := []string{
		"id", "name", "world", "role", "state",
		"active_writ", "model", "account", "last_seen",
	}
	for _, f := range expectedFields {
		if _, ok := m[f]; !ok {
			t.Errorf("missing expected JSON field %q", f)
		}
	}
	if len(m) != len(expectedFields) {
		t.Errorf("got %d fields, want %d", len(m), len(expectedFields))
	}
}

func TestAgentListRowEmptyFields(t *testing.T) {
	// Even empty strings should be present (not omitted) — the list DTO
	// uses empty markers, not nil pointers.
	row := AgentListRow{
		ID:    "test/Agent1",
		Name:  "Agent1",
		World: "test",
		Role:  "outpost",
		State: "idle",
	}

	data, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// All fields should be present even when empty.
	for _, f := range []string{"active_writ", "model", "account", "last_seen"} {
		if _, ok := m[f]; !ok {
			t.Errorf("field %q should be present even when empty", f)
		}
	}
}
