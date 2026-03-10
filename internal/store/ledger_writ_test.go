package store

import (
	"testing"
	"time"
)

func TestHistoryForWrit(t *testing.T) {
	s := setupWorld(t)

	writID := "sol-a1b2c3d4e5f6a7b8"
	now := time.Now().UTC()

	// Create a writ.
	_, err := s.CreateWrit("test writ", "", "autarch", 2, nil)
	if err != nil {
		// The random ID won't match, so create with specific ID manually.
		_, err = s.db.Exec(
			`INSERT INTO writs (id, title, status, priority, created_by, created_at, updated_at)
			 VALUES (?, ?, 'open', 2, 'autarch', ?, ?)`,
			writID, "test writ", now.Format(time.RFC3339), now.Format(time.RFC3339),
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Write history entries.
	_, err = s.WriteHistory("Toast", writID, "cast", "", now, nil)
	if err != nil {
		t.Fatal(err)
	}
	endTime := now.Add(1 * time.Hour)
	_, err = s.WriteHistory("Toast", writID, "cast", "completed", now.Add(2*time.Hour), &endTime)
	if err != nil {
		t.Fatal(err)
	}

	// Write unrelated history.
	_, err = s.WriteHistory("Ember", "sol-9999999999999999", "cast", "", now, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Query.
	entries, err := s.HistoryForWrit(writID)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].AgentName != "Toast" {
		t.Errorf("expected agent 'Toast', got %q", entries[0].AgentName)
	}

	// Verify ordering (ASC by started_at).
	if entries[0].StartedAt.After(entries[1].StartedAt) {
		t.Error("expected entries ordered by started_at ASC")
	}
}

