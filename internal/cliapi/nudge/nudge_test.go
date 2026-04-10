package nudge

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNudgeJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	n := Nudge{
		ID:       "nudge-001",
		Target:   "sol-dev-Nova",
		Body:     "Please check escalation esc-abc123",
		Source:   "autarch",
		QueuedAt: now,
	}

	data, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if m["id"] != "nudge-001" {
		t.Errorf("id = %v, want %q", m["id"], "nudge-001")
	}
	if m["target"] != "sol-dev-Nova" {
		t.Errorf("target = %v, want %q", m["target"], "sol-dev-Nova")
	}
	if m["source"] != "autarch" {
		t.Errorf("source = %v, want %q", m["source"], "autarch")
	}
}

func TestNudgeQueueSummaryJSON(t *testing.T) {
	s := NudgeQueueSummary{
		Agent:        "Nova",
		World:        "sol-dev",
		PendingCount: 3,
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if m["agent"] != "Nova" {
		t.Errorf("agent = %v, want %q", m["agent"], "Nova")
	}
	if m["pending_count"].(float64) != 3 {
		t.Errorf("pending_count = %v, want 3", m["pending_count"])
	}
}
