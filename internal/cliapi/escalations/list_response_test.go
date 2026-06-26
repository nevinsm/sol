package escalations

import (
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

func TestListEscalationsFromStore(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	notified := time.Date(2025, 6, 15, 11, 0, 0, 0, time.UTC)

	escs := []store.Escalation{
		{
			ID:             "esc-0000000000000001",
			Severity:       "high",
			Status:         "open",
			Source:         "forge",
			SourceRef:      "mr:mr-abc123",
			Description:    "merge conflict",
			CreatedAt:      now,
			UpdatedAt:      now,
			LastNotifiedAt: &notified,
		},
		{
			ID:          "esc-0000000000000002",
			Severity:    "low",
			Status:      "acknowledged",
			Source:      "sentinel",
			Description: "stale tether",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}

	out := ListEscalationsFromStore(escs)

	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}

	// First escalation — has LastNotifiedAt.
	e := out[0]
	if e.ID != "esc-0000000000000001" {
		t.Errorf("ID = %q, want %q", e.ID, "esc-0000000000000001")
	}
	if e.Severity != "high" {
		t.Errorf("Severity = %q, want %q", e.Severity, "high")
	}
	if e.Status != "open" {
		t.Errorf("Status = %q, want %q", e.Status, "open")
	}
	if e.Source != "forge" {
		t.Errorf("Source = %q, want %q", e.Source, "forge")
	}
	if e.SourceRef != "mr:mr-abc123" {
		t.Errorf("SourceRef = %q, want %q", e.SourceRef, "mr:mr-abc123")
	}
	if e.Description != "merge conflict" {
		t.Errorf("Description = %q, want %q", e.Description, "merge conflict")
	}
	if !e.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", e.CreatedAt, now)
	}
	if !e.UpdatedAt.Equal(now) {
		t.Errorf("UpdatedAt = %v, want %v", e.UpdatedAt, now)
	}
	if e.LastNotifiedAt == nil || !e.LastNotifiedAt.Equal(notified) {
		t.Errorf("LastNotifiedAt = %v, want %v", e.LastNotifiedAt, notified)
	}

	// Second escalation — no LastNotifiedAt.
	e2 := out[1]
	if e2.ID != "esc-0000000000000002" {
		t.Errorf("ID = %q, want %q", e2.ID, "esc-0000000000000002")
	}
	if e2.LastNotifiedAt != nil {
		t.Errorf("LastNotifiedAt = %v, want nil", e2.LastNotifiedAt)
	}
}

func TestListEscalationsFromStore_Empty(t *testing.T) {
	out := ListEscalationsFromStore([]store.Escalation{})
	if out == nil {
		t.Fatal("expected non-nil slice for empty input")
	}
	if len(out) != 0 {
		t.Errorf("len = %d, want 0", len(out))
	}
}
