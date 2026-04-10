package nudge

import (
	"encoding/json"
	"testing"
	"time"

	internalnudge "github.com/nevinsm/sol/internal/nudge"
)

func TestFromMessage(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)

	msg := internalnudge.Message{
		Sender:    "autarch",
		Type:      "escalation",
		Subject:   "Please check esc-abc123",
		Body:      "Escalation needs attention",
		Priority:  "normal",
		CreatedAt: now,
		TTL:       "30m",
	}

	n := FromMessage(msg, "sol-dev-Nova")

	if n.ID != "1775822400000" {
		t.Errorf("ID = %q, want %q", n.ID, "1775822400000")
	}
	if n.Target != "sol-dev-Nova" {
		t.Errorf("Target = %q, want %q", n.Target, "sol-dev-Nova")
	}
	if n.Body != "Escalation needs attention" {
		t.Errorf("Body = %q, want %q", n.Body, "Escalation needs attention")
	}
	if n.Source != "autarch" {
		t.Errorf("Source = %q, want %q", n.Source, "autarch")
	}
	if !n.QueuedAt.Equal(now) {
		t.Errorf("QueuedAt = %v, want %v", n.QueuedAt, now)
	}

	// Verify JSON shape.
	data, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Confirm snake_case keys.
	for _, key := range []string{"id", "target", "body", "source", "queued_at"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON key %q", key)
		}
	}
}

func TestFromMessages(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)

	msgs := []internalnudge.Message{
		{Sender: "autarch", Body: "first", CreatedAt: now},
		{Sender: "sentinel", Body: "second", CreatedAt: now.Add(time.Second)},
	}

	nudges := FromMessages(msgs, "sol-dev-Nova")
	if len(nudges) != 2 {
		t.Fatalf("len = %d, want 2", len(nudges))
	}
	if nudges[0].Source != "autarch" {
		t.Errorf("nudges[0].Source = %q, want %q", nudges[0].Source, "autarch")
	}
	if nudges[1].Source != "sentinel" {
		t.Errorf("nudges[1].Source = %q, want %q", nudges[1].Source, "sentinel")
	}
}

func TestFromMessagesEmpty(t *testing.T) {
	nudges := FromMessages([]internalnudge.Message{}, "sol-dev-Nova")
	if nudges == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(nudges) != 0 {
		t.Fatalf("len = %d, want 0", len(nudges))
	}

	// Empty arrays should marshal as [] not null.
	data, err := json.Marshal(nudges)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("JSON = %s, want []", data)
	}
}

func TestFromMessagesNil(t *testing.T) {
	nudges := FromMessages(nil, "sol-dev-Nova")
	if nudges == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(nudges) != 0 {
		t.Fatalf("len = %d, want 0", len(nudges))
	}
}
