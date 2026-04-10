package caravans

import (
	"encoding/json"
	"testing"
)

func TestListEntryJSONShape(t *testing.T) {
	entry := ListEntry{
		ID:     "car-0000000000000001",
		Name:   "test",
		Status: "open",
		Owner:  "autarch",
		Items:  5,
		Merged: 2,
		Worlds: []string{"alpha", "beta"},
		PhaseProgress: map[string]ListPhaseStats{
			"0": {Total: 3, Merged: 2, InProgress: 1},
			"1": {Total: 2, Ready: 1, Blocked: 1},
		},
		CreatedAt: "2024-01-01T00:00:00Z",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	expectedKeys := []string{"id", "name", "status", "owner", "items", "merged", "worlds", "phase_progress", "created_at"}
	for _, key := range expectedKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing key %q in JSON output", key)
		}
	}

	// closed_at should be omitted when nil.
	if _, ok := raw["closed_at"]; ok {
		t.Error("closed_at should be omitted when nil")
	}
}

func TestListEntryClosedAt(t *testing.T) {
	closedAt := "2024-06-01T12:00:00Z"
	entry := ListEntry{
		ID:            "car-0000000000000001",
		Name:          "done",
		Status:        "closed",
		Owner:         "autarch",
		Worlds:        []string{},
		PhaseProgress: map[string]ListPhaseStats{},
		CreatedAt:     "2024-01-01T00:00:00Z",
		ClosedAt:      &closedAt,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if _, ok := raw["closed_at"]; !ok {
		t.Error("closed_at should be present when non-nil")
	}
}

func TestListPhaseStatsJSONKeys(t *testing.T) {
	ps := ListPhaseStats{
		Total:      5,
		Merged:     2,
		InProgress: 1,
		Ready:      1,
		Blocked:    1,
	}

	data, err := json.Marshal(ps)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	for _, key := range []string{"total", "merged", "in_progress", "ready", "blocked"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing key %q in ListPhaseStats JSON", key)
		}
	}
}
