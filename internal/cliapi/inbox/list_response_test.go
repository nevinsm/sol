package inbox

import (
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/inbox"
)

func TestFromInboxItem(t *testing.T) {
	ts := time.Date(2026, 3, 15, 10, 30, 0, 0, time.UTC)

	item := inbox.InboxItem{
		ID:          "esc-0000000000000001",
		Type:        inbox.ItemEscalation,
		Priority:    1,
		Source:      "forge",
		Description: "merge conflict unresolvable",
		CreatedAt:   ts,
	}

	got := FromInboxItem(item)

	if got.ID != "esc-0000000000000001" {
		t.Errorf("ID = %q, want %q", got.ID, "esc-0000000000000001")
	}
	if got.Type != "escalation" {
		t.Errorf("Type = %q, want %q", got.Type, "escalation")
	}
	if got.Priority != 1 {
		t.Errorf("Priority = %d, want %d", got.Priority, 1)
	}
	if got.Source != "forge" {
		t.Errorf("Source = %q, want %q", got.Source, "forge")
	}
	if got.Description != "merge conflict unresolvable" {
		t.Errorf("Description = %q, want %q", got.Description, "merge conflict unresolvable")
	}
	if got.CreatedAt != "2026-03-15T10:30:00Z" {
		t.Errorf("CreatedAt = %q, want %q", got.CreatedAt, "2026-03-15T10:30:00Z")
	}
	// Age is computed from time.Since — just verify it's non-empty.
	if got.Age == "" {
		t.Error("Age should be non-empty")
	}
}

func TestFromInboxItem_MailType(t *testing.T) {
	ts := time.Date(2026, 3, 15, 10, 30, 0, 0, time.UTC)

	item := inbox.InboxItem{
		ID:          "msg-0000000000000001",
		Type:        inbox.ItemMail,
		Priority:    3,
		Source:      "Toast",
		Description: "build complete",
		CreatedAt:   ts,
	}

	got := FromInboxItem(item)

	if got.Type != "mail" {
		t.Errorf("Type = %q, want %q", got.Type, "mail")
	}
}

func TestFromInboxItems(t *testing.T) {
	ts := time.Date(2026, 3, 15, 10, 30, 0, 0, time.UTC)

	items := []inbox.InboxItem{
		{
			ID:          "esc-0000000000000001",
			Type:        inbox.ItemEscalation,
			Priority:    1,
			Source:      "forge",
			Description: "conflict",
			CreatedAt:   ts,
		},
		{
			ID:          "msg-0000000000000001",
			Type:        inbox.ItemMail,
			Priority:    3,
			Source:      "Toast",
			Description: "done",
			CreatedAt:   ts,
		},
	}

	got := FromInboxItems(items)

	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != "esc-0000000000000001" {
		t.Errorf("got[0].ID = %q, want %q", got[0].ID, "esc-0000000000000001")
	}
	if got[1].ID != "msg-0000000000000001" {
		t.Errorf("got[1].ID = %q, want %q", got[1].ID, "msg-0000000000000001")
	}
}

func TestFromInboxItems_Empty(t *testing.T) {
	got := FromInboxItems([]inbox.InboxItem{})

	if got == nil {
		t.Fatal("expected non-nil slice for empty input")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}
