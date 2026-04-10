package dispatch

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHandoffResult_JSON(t *testing.T) {
	ts := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	result := HandoffResult{
		WritID:      "sol-a1b2c3d4e5f6a7b8",
		Agent:       "Lyra",
		OldSession:  "sol-dev-Lyra",
		NewSession:  "sol-dev-Lyra",
		HandedOffAt: ts,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify snake_case keys.
	for _, key := range []string{"writ_id", "agent", "old_session", "new_session", "handed_off_at"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing expected key %q", key)
		}
	}

	// Verify field count — no extra keys.
	if len(m) != 5 {
		t.Errorf("expected 5 keys, got %d: %v", len(m), m)
	}

	// Verify values.
	if m["writ_id"] != "sol-a1b2c3d4e5f6a7b8" {
		t.Errorf("writ_id = %v, want sol-a1b2c3d4e5f6a7b8", m["writ_id"])
	}
	if m["agent"] != "Lyra" {
		t.Errorf("agent = %v, want Lyra", m["agent"])
	}
	if m["old_session"] != "sol-dev-Lyra" {
		t.Errorf("old_session = %v, want sol-dev-Lyra", m["old_session"])
	}
	if m["new_session"] != "sol-dev-Lyra" {
		t.Errorf("new_session = %v, want sol-dev-Lyra", m["new_session"])
	}
}

func TestHandoffResult_Roundtrip(t *testing.T) {
	ts := time.Date(2026, 4, 10, 14, 30, 0, 0, time.UTC)
	original := HandoffResult{
		WritID:      "sol-1234567890abcdef",
		Agent:       "Toast",
		OldSession:  "sol-prod-Toast",
		NewSession:  "sol-prod-Toast",
		HandedOffAt: ts,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded HandoffResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.WritID != original.WritID {
		t.Errorf("WritID = %q, want %q", decoded.WritID, original.WritID)
	}
	if decoded.Agent != original.Agent {
		t.Errorf("Agent = %q, want %q", decoded.Agent, original.Agent)
	}
	if decoded.OldSession != original.OldSession {
		t.Errorf("OldSession = %q, want %q", decoded.OldSession, original.OldSession)
	}
	if decoded.NewSession != original.NewSession {
		t.Errorf("NewSession = %q, want %q", decoded.NewSession, original.NewSession)
	}
	if !decoded.HandedOffAt.Equal(original.HandedOffAt) {
		t.Errorf("HandedOffAt = %v, want %v", decoded.HandedOffAt, original.HandedOffAt)
	}
}
