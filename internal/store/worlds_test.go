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

func TestRemoveWorld(t *testing.T) {
	s := setupSphere(t)

	err := s.RegisterWorld("haven", "/home/user/haven")
	if err != nil {
		t.Fatal(err)
	}

	err = s.RemoveWorld("haven")
	if err != nil {
		t.Fatal(err)
	}

	w, err := s.GetWorld("haven")
	if err != nil {
		t.Fatal(err)
	}
	if w != nil {
		t.Fatalf("expected nil after removal, got %v", w)
	}
}

func TestRemoveWorldNonexistent(t *testing.T) {
	s := setupSphere(t)

	err := s.RemoveWorld("nonexistent")
	if err != nil {
		t.Fatalf("expected no error removing nonexistent world, got %v", err)
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

	// Verify schema_version is 5.
	var version int
	err = s.db.QueryRow(`SELECT version FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version != 5 {
		t.Fatalf("expected schema version 5, got %d", version)
	}
}
