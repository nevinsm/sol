package caravans

import (
	"encoding/json"
	"testing"
)

func TestLaunchResponseJSONShape(t *testing.T) {
	resp := LaunchResponse{
		CaravanID: "car-0000000000000001",
		World:     "alpha",
		Dispatched: []LaunchItem{
			{WritID: "sol-0001", AgentName: "Toast", SessionName: "sol-alpha-Toast"},
			{WritID: "sol-0002", AgentName: "Nova", SessionName: "sol-alpha-Nova"},
		},
		Blocked:    3,
		AutoClosed: false,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	for _, key := range []string{"caravan_id", "world", "dispatched", "blocked", "auto_closed"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing key %q", key)
		}
	}
}

func TestLaunchResponseEmptyDispatched(t *testing.T) {
	resp := LaunchResponse{
		CaravanID:  "car-0000000000000001",
		World:      "alpha",
		Dispatched: []LaunchItem{},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// dispatched should be present as empty array, not omitted.
	if _, ok := raw["dispatched"]; !ok {
		t.Error("dispatched should be present even when empty")
	}

	// Verify it's an empty array, not null.
	var items []json.RawMessage
	if err := json.Unmarshal(raw["dispatched"], &items); err != nil {
		t.Fatalf("Unmarshal dispatched failed: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("dispatched len = %d, want 0", len(items))
	}
}

func TestLaunchItemJSONKeys(t *testing.T) {
	item := LaunchItem{
		WritID:      "sol-0001",
		AgentName:   "Toast",
		SessionName: "sol-alpha-Toast",
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	for _, key := range []string{"writ_id", "agent_name", "session_name"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing key %q", key)
		}
	}
}
