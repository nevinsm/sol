package agents

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

func TestFromStoreHistoryEntry(t *testing.T) {
	started := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	ended := time.Date(2026, 4, 10, 12, 30, 0, 0, time.UTC)

	entry := store.HistoryEntry{
		ID:        "h-001",
		AgentName: "Nova",
		WritID:    "sol-a1b2c3d4e5f6a7b8",
		Action:    "cast",
		StartedAt: started,
		EndedAt:   &ended,
		Summary:   "completed migration",
	}

	he := FromStoreHistoryEntry(entry, "30m", "done")

	if he.ID != "h-001" {
		t.Errorf("ID = %q, want %q", he.ID, "h-001")
	}
	if he.AgentName != "Nova" {
		t.Errorf("AgentName = %q, want %q", he.AgentName, "Nova")
	}
	if he.WritID != "sol-a1b2c3d4e5f6a7b8" {
		t.Errorf("WritID = %q, want %q", he.WritID, "sol-a1b2c3d4e5f6a7b8")
	}
	if he.Action != "cast" {
		t.Errorf("Action = %q, want %q", he.Action, "cast")
	}
	if !he.StartedAt.Equal(started) {
		t.Errorf("StartedAt = %v, want %v", he.StartedAt, started)
	}
	if he.EndedAt == nil || !he.EndedAt.Equal(ended) {
		t.Errorf("EndedAt = %v, want %v", he.EndedAt, ended)
	}
	if he.Duration != "30m" {
		t.Errorf("Duration = %q, want %q", he.Duration, "30m")
	}
	if he.Outcome != "done" {
		t.Errorf("Outcome = %q, want %q", he.Outcome, "done")
	}
	if he.Summary != "completed migration" {
		t.Errorf("Summary = %q, want %q", he.Summary, "completed migration")
	}
}

func TestFromStoreHistoryEntryMinimal(t *testing.T) {
	entry := store.HistoryEntry{
		ID:        "h-002",
		AgentName: "Toast",
		Action:    "resolve",
		StartedAt: time.Date(2026, 4, 10, 14, 0, 0, 0, time.UTC),
	}

	he := FromStoreHistoryEntry(entry, "", "running")

	if he.WritID != "" {
		t.Errorf("WritID = %q, want empty", he.WritID)
	}
	if he.EndedAt != nil {
		t.Errorf("EndedAt = %v, want nil", he.EndedAt)
	}
	if he.Duration != "" {
		t.Errorf("Duration = %q, want empty", he.Duration)
	}
	if he.Outcome != "running" {
		t.Errorf("Outcome = %q, want %q", he.Outcome, "running")
	}
	if he.Summary != "" {
		t.Errorf("Summary = %q, want empty", he.Summary)
	}
}

func TestHistoryEntryJSONShape(t *testing.T) {
	started := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	ended := time.Date(2026, 4, 10, 12, 30, 0, 0, time.UTC)

	he := HistoryEntry{
		ID:        "h-001",
		AgentName: "Nova",
		WritID:    "sol-a1b2c3d4e5f6a7b8",
		Action:    "cast",
		StartedAt: started,
		EndedAt:   &ended,
		Duration:  "30m",
		Outcome:   "done",
		Summary:   "completed migration",
	}

	data, err := json.Marshal(he)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	// Verify expected keys are present.
	expectedKeys := []string{"id", "agent_name", "writ_id", "action", "started_at", "ended_at", "duration", "outcome", "summary"}
	for _, k := range expectedKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("missing expected key %q in JSON output", k)
		}
	}

	// Verify no unexpected keys.
	if len(m) != len(expectedKeys) {
		t.Errorf("JSON has %d keys, want %d; keys: %v", len(m), len(expectedKeys), m)
	}
}

func TestHistoryEntryJSONOmitsEmpty(t *testing.T) {
	he := HistoryEntry{
		ID:        "h-002",
		AgentName: "Toast",
		Action:    "resolve",
		StartedAt: time.Date(2026, 4, 10, 14, 0, 0, 0, time.UTC),
		Outcome:   "running",
	}

	data, err := json.Marshal(he)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	// These should be omitted when empty/nil.
	for _, k := range []string{"writ_id", "ended_at", "duration", "summary"} {
		if _, ok := m[k]; ok {
			t.Errorf("key %q should be omitted when empty, but is present", k)
		}
	}

	// These should always be present.
	for _, k := range []string{"id", "agent_name", "action", "started_at", "outcome"} {
		if _, ok := m[k]; !ok {
			t.Errorf("key %q should always be present, but is missing", k)
		}
	}
}
