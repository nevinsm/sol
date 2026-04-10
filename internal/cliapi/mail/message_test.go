package mail

import (
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

func TestFromStoreMessage(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	readTime := now.Add(10 * time.Minute)
	ackedTime := now.Add(20 * time.Minute)

	sm := store.Message{
		ID:        "msg-0000000000000001",
		Sender:    "Nova",
		Recipient: "autarch",
		Subject:   "Work complete",
		Body:      "Writ sol-0001 has been resolved.",
		Priority:  1,
		CreatedAt: now,
		AckedAt:   &ackedTime,
	}

	m := FromStoreMessage(sm, &readTime)

	if m.ID != sm.ID {
		t.Errorf("ID = %q, want %q", m.ID, sm.ID)
	}
	if m.Sender != "Nova" {
		t.Errorf("Sender = %q, want %q", m.Sender, "Nova")
	}
	if m.Recipient != "autarch" {
		t.Errorf("Recipient = %q, want %q", m.Recipient, "autarch")
	}
	if m.Priority != 1 {
		t.Errorf("Priority = %d, want 1", m.Priority)
	}
	if m.ReadAt == nil || !m.ReadAt.Equal(readTime) {
		t.Errorf("ReadAt = %v, want %v", m.ReadAt, readTime)
	}
	if m.AcknowledgedAt == nil || !m.AcknowledgedAt.Equal(ackedTime) {
		t.Errorf("AcknowledgedAt = %v, want %v", m.AcknowledgedAt, ackedTime)
	}
}

func TestFromStoreMessageUnread(t *testing.T) {
	sm := store.Message{
		ID:        "msg-0000000000000002",
		Sender:    "Toast",
		Recipient: "Nova",
		Subject:   "Hello",
		Body:      "Hi there!",
		CreatedAt: time.Now().UTC(),
	}

	m := FromStoreMessage(sm, nil)

	if m.ReadAt != nil {
		t.Errorf("ReadAt = %v, want nil", m.ReadAt)
	}
	if m.AcknowledgedAt != nil {
		t.Errorf("AcknowledgedAt = %v, want nil", m.AcknowledgedAt)
	}
}
