package store

import (
	"strings"
	"testing"
	"time"
)

func TestRegisterWorld(t *testing.T) {
	s := setupSphere(t)

	err := s.RegisterWorld("haven", "/home/user/haven")
	if err != nil {
		t.Fatal(err)
	}

	w, err := s.GetWorld("haven")
	if err != nil {
		t.Fatal(err)
	}
	if w == nil {
		t.Fatal("expected world, got nil")
	}
	if w.Name != "haven" {
		t.Fatalf("expected name 'haven', got %q", w.Name)
	}
	if w.SourceRepo != "/home/user/haven" {
		t.Fatalf("expected source_repo '/home/user/haven', got %q", w.SourceRepo)
	}

	// Verify timestamps are valid RFC3339.
	if w.CreatedAt.IsZero() {
		t.Fatal("expected non-zero created_at")
	}
	if w.UpdatedAt.IsZero() {
		t.Fatal("expected non-zero updated_at")
	}
	// Verify timestamps are recent (within last 5 seconds).
	if time.Since(w.CreatedAt) > 5*time.Second {
		t.Fatalf("created_at is too old: %v", w.CreatedAt)
	}
}

func TestRegisterWorldIdempotent(t *testing.T) {
	s := setupSphere(t)

	err := s.RegisterWorld("haven", "/home/user/haven")
	if err != nil {
		t.Fatal(err)
	}

	// Register again with different source_repo — should be no-op.
	err = s.RegisterWorld("haven", "/different/path")
	if err != nil {
		t.Fatal(err)
	}

	w, err := s.GetWorld("haven")
	if err != nil {
		t.Fatal(err)
	}
	// Original values preserved.
	if w.SourceRepo != "/home/user/haven" {
		t.Fatalf("expected source_repo '/home/user/haven' (original), got %q", w.SourceRepo)
	}
}

func TestGetWorldNotFound(t *testing.T) {
	s := setupSphere(t)

	w, err := s.GetWorld("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if w != nil {
		t.Fatalf("expected nil for nonexistent world, got %v", w)
	}
}

func TestListWorlds(t *testing.T) {
	s := setupSphere(t)

	// Register 3 worlds (in non-alphabetical order).
	for _, name := range []string{"zephyr", "alpha", "haven"} {
		if err := s.RegisterWorld(name, "/path/"+name); err != nil {
			t.Fatal(err)
		}
	}

	worlds, err := s.ListWorlds()
	if err != nil {
		t.Fatal(err)
	}
	if len(worlds) != 3 {
		t.Fatalf("expected 3 worlds, got %d", len(worlds))
	}
	// Verify ordered by name.
	if worlds[0].Name != "alpha" {
		t.Fatalf("expected first world 'alpha', got %q", worlds[0].Name)
	}
	if worlds[1].Name != "haven" {
		t.Fatalf("expected second world 'haven', got %q", worlds[1].Name)
	}
	if worlds[2].Name != "zephyr" {
		t.Fatalf("expected third world 'zephyr', got %q", worlds[2].Name)
	}
}

func TestListWorldsEmpty(t *testing.T) {
	s := setupSphere(t)

	worlds, err := s.ListWorlds()
	if err != nil {
		t.Fatal(err)
	}
	if len(worlds) != 0 {
		t.Fatalf("expected 0 worlds, got %d", len(worlds))
	}
}

func TestUpdateWorldRepo(t *testing.T) {
	s := setupSphere(t)

	err := s.RegisterWorld("haven", "/old/path")
	if err != nil {
		t.Fatal(err)
	}

	// Set updated_at to an old timestamp so we can verify it changes.
	_, err = s.db.Exec(`UPDATE worlds SET updated_at = '2020-01-01T00:00:00Z' WHERE name = 'haven'`)
	if err != nil {
		t.Fatal(err)
	}

	err = s.UpdateWorldRepo("haven", "/new/path")
	if err != nil {
		t.Fatal(err)
	}

	after, err := s.GetWorld("haven")
	if err != nil {
		t.Fatal(err)
	}
	if after.SourceRepo != "/new/path" {
		t.Fatalf("expected source_repo '/new/path', got %q", after.SourceRepo)
	}
	if after.UpdatedAt.Year() == 2020 {
		t.Fatalf("expected updated_at to change from old value, got %v", after.UpdatedAt)
	}
}

func TestUpdateWorldRepoNonexistent(t *testing.T) {
	s := setupSphere(t)

	err := s.UpdateWorldRepo("nonexistent", "/some/path")
	if err == nil {
		t.Fatal("expected error updating nonexistent world")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
}

func TestDeleteWorldData(t *testing.T) {
	s := setupSphere(t)

	err := s.RegisterWorld("haven", "/home/user/haven")
	if err != nil {
		t.Fatal(err)
	}

	// Create an agent, message, and escalation to verify cleanup.
	_, err = s.CreateAgent("Toast", "haven", "agent")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SendMessage("haven/Toast", "haven/Other", "test", "body", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateEscalation("low", "haven/Toast", "test escalation")
	if err != nil {
		t.Fatal(err)
	}

	err = s.DeleteWorldData("haven")
	if err != nil {
		t.Fatal(err)
	}

	w, err := s.GetWorld("haven")
	if err != nil {
		t.Fatal(err)
	}
	if w != nil {
		t.Fatalf("expected nil after deletion, got %v", w)
	}

	// Verify messages were cleaned up.
	msgs, err := s.ListMessages(MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after deletion, got %d", len(msgs))
	}

	// Verify escalations were cleaned up.
	escs, err := s.ListEscalations("")
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 0 {
		t.Fatalf("expected 0 escalations after deletion, got %d", len(escs))
	}
}

func TestDeleteWorldDataDeletesAgents(t *testing.T) {
	s := setupSphere(t)

	err := s.RegisterWorld("haven", "/home/user/haven")
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.CreateAgent("Toast", "haven", "agent")
	if err != nil {
		t.Fatal(err)
	}

	err = s.DeleteWorldData("haven")
	if err != nil {
		t.Fatal(err)
	}

	agents, err := s.ListAgents("haven", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 0 {
		t.Fatalf("expected 0 agents after deletion, got %d", len(agents))
	}

	w, err := s.GetWorld("haven")
	if err != nil {
		t.Fatal(err)
	}
	if w != nil {
		t.Fatalf("expected nil world after deletion, got %v", w)
	}
}

func TestDeleteWorldDataDeletesCaravanItems(t *testing.T) {
	s := setupSphere(t)

	err := s.RegisterWorld("haven", "/home/user/haven")
	if err != nil {
		t.Fatal(err)
	}

	caravanID, err := s.CreateCaravan("test-caravan", "operator")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddCaravanItem(caravanID, "sol-11111111", "haven"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddCaravanItem(caravanID, "sol-22222222", "haven"); err != nil {
		t.Fatal(err)
	}

	err = s.DeleteWorldData("haven")
	if err != nil {
		t.Fatal(err)
	}

	// Caravan itself should still exist (cross-world).
	c, err := s.GetCaravan(caravanID)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("expected caravan to survive world deletion")
	}

	// But items for the deleted world should be gone.
	items, err := s.ListCaravanItems(caravanID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 caravan items after deletion, got %d", len(items))
	}
}

func TestDeleteWorldDataDeletesMessagesAndEscalations(t *testing.T) {
	s := setupSphere(t)

	err := s.RegisterWorld("haven", "/home/user/haven")
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.CreateAgent("Toast", "haven", "agent")
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.SendMessage("haven/Toast", "haven/Other", "test", "body", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateEscalation("low", "haven/Toast", "test escalation")
	if err != nil {
		t.Fatal(err)
	}

	err = s.DeleteWorldData("haven")
	if err != nil {
		t.Fatal(err)
	}

	msgs, err := s.ListMessages(MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after deletion, got %d", len(msgs))
	}

	escs, err := s.ListEscalations("")
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 0 {
		t.Fatalf("expected 0 escalations after deletion, got %d", len(escs))
	}
}

func TestDeleteWorldDataPreservesOtherWorlds(t *testing.T) {
	s := setupSphere(t)

	// Register two worlds with agents.
	if err := s.RegisterWorld("alpha", "/path/alpha"); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterWorld("beta", "/path/beta"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAgent("Toast", "alpha", "agent"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAgent("Sage", "beta", "agent"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendMessage("beta/Sage", "beta/Other", "test", "body", 2, "notification"); err != nil {
		t.Fatal(err)
	}

	// Delete alpha.
	if err := s.DeleteWorldData("alpha"); err != nil {
		t.Fatal(err)
	}

	// Beta's data should be untouched.
	w, err := s.GetWorld("beta")
	if err != nil {
		t.Fatal(err)
	}
	if w == nil {
		t.Fatal("expected beta world to survive alpha deletion")
	}

	agents, err := s.ListAgents("beta", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 beta agent, got %d", len(agents))
	}
	if agents[0].Name != "Sage" {
		t.Fatalf("expected agent 'Sage', got %q", agents[0].Name)
	}

	msgs, err := s.ListMessages(MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (beta's), got %d", len(msgs))
	}
}

func TestDeleteWorldDataNonexistent(t *testing.T) {
	s := setupSphere(t)

	// Deleting a nonexistent world should not error.
	err := s.DeleteWorldData("nonexistent")
	if err != nil {
		t.Fatalf("expected no error deleting nonexistent world data, got %v", err)
	}
}

func TestSchemaV5Migration(t *testing.T) {
	s := setupSphere(t)

	// Verify worlds table exists.
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='worlds'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected worlds table, got count=%d", count)
	}

	// Verify schema_version is 6.
	var version int
	err = s.db.QueryRow(`SELECT version FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version != 6 {
		t.Fatalf("expected schema version 6, got %d", version)
	}
}
