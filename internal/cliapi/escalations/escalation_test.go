package escalations

import (
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

func TestFromStoreEscalation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	acked := now.Add(5 * time.Minute)

	se := store.Escalation{
		ID:          "esc-0000000000000001",
		Severity:    "high",
		Source:      "forge",
		Description: "merge conflict unresolvable",
		SourceRef:   "mr:mr-abc123",
		Status:      "acknowledged",
		CreatedAt:   now,
	}

	e := FromStoreEscalation(se, "sol-dev", &acked, nil)

	if e.ID != se.ID {
		t.Errorf("ID = %q, want %q", e.ID, se.ID)
	}
	if e.Severity != "high" {
		t.Errorf("Severity = %q, want %q", e.Severity, "high")
	}
	if e.Status != "acknowledged" {
		t.Errorf("Status = %q, want %q", e.Status, "acknowledged")
	}
	if e.World != "sol-dev" {
		t.Errorf("World = %q, want %q", e.World, "sol-dev")
	}
	if e.Component != "forge" {
		t.Errorf("Component = %q, want %q", e.Component, "forge")
	}
	if e.SourceRef != "mr:mr-abc123" {
		t.Errorf("SourceRef = %q, want %q", e.SourceRef, "mr:mr-abc123")
	}
	if e.Message != "merge conflict unresolvable" {
		t.Errorf("Message = %q, want %q", e.Message, "merge conflict unresolvable")
	}
	if e.AcknowledgedAt == nil || !e.AcknowledgedAt.Equal(acked) {
		t.Errorf("AcknowledgedAt = %v, want %v", e.AcknowledgedAt, acked)
	}
	if e.ResolvedAt != nil {
		t.Errorf("ResolvedAt = %v, want nil", e.ResolvedAt)
	}
}
