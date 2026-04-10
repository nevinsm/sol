package migrations

import (
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

func TestFromStoreMigration(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	sm := store.AppliedMigration{
		Name:      "add-labels-column",
		Version:   "v1.2.0",
		AppliedAt: now,
		Summary:   "Added labels column to writs table",
		Details:   map[string]any{"rows_affected": 42},
	}

	m := FromStoreMigration(sm)

	if m.Name != "add-labels-column" {
		t.Errorf("Name = %q, want %q", m.Name, "add-labels-column")
	}
	if m.Version != "v1.2.0" {
		t.Errorf("Version = %q, want %q", m.Version, "v1.2.0")
	}
	if !m.AppliedAt.Equal(now) {
		t.Errorf("AppliedAt = %v, want %v", m.AppliedAt, now)
	}
	if m.Summary != "Added labels column to writs table" {
		t.Errorf("Summary = %q, want %q", m.Summary, "Added labels column to writs table")
	}
}
