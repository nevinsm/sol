package store

import "testing"

func TestAddDependency(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	id1, _ := s.CreateWrit("Item A", "", "autarch", 2, nil)
	id2, _ := s.CreateWrit("Item B", "", "autarch", 2, nil)

	// Add dependency: A depends on B.
	if err := s.AddDependency(id1, id2); err != nil {
		t.Fatalf("AddDependency() error: %v", err)
	}

	// Verify with GetDependencies.
	deps, err := s.GetDependencies(id1)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || deps[0] != id2 {
		t.Fatalf("expected deps [%s], got %v", id2, deps)
	}
}

func TestAddDependencySelfRef(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	id1, _ := s.CreateWrit("Item A", "", "autarch", 2, nil)

	err := s.AddDependency(id1, id1)
	if err == nil {
		t.Fatal("expected error for self-dependency")
	}
}

func TestAddDependencyNonexistentItem(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	id1, _ := s.CreateWrit("Item A", "", "autarch", 2, nil)

	err := s.AddDependency(id1, "sol-nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent writ")
	}

	err = s.AddDependency("sol-nonexist", id1)
	if err == nil {
		t.Fatal("expected error for nonexistent writ")
	}
}

func TestAddDependencyCycleDetection(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	idA, _ := s.CreateWrit("Item A", "", "autarch", 2, nil)
	idB, _ := s.CreateWrit("Item B", "", "autarch", 2, nil)
	idC, _ := s.CreateWrit("Item C", "", "autarch", 2, nil)

	// A depends on B.
	if err := s.AddDependency(idA, idB); err != nil {
		t.Fatal(err)
	}

	// B depends on A → direct cycle → error.
	err := s.AddDependency(idB, idA)
	if err == nil {
		t.Fatal("expected error for direct cycle A↔B")
	}

	// B depends on C.
	if err := s.AddDependency(idB, idC); err != nil {
		t.Fatal(err)
	}

	// C depends on A → transitive cycle (A→B→C→A) → error.
	err = s.AddDependency(idC, idA)
	if err == nil {
		t.Fatal("expected error for transitive cycle A→B→C→A")
	}
}

func TestRemoveDependency(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	id1, _ := s.CreateWrit("Item A", "", "autarch", 2, nil)
	id2, _ := s.CreateWrit("Item B", "", "autarch", 2, nil)

	// Add then remove.
	s.AddDependency(id1, id2)
	if err := s.RemoveDependency(id1, id2); err != nil {
		t.Fatalf("RemoveDependency() error: %v", err)
	}

	deps, err := s.GetDependencies(id1)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 0 {
		t.Fatalf("expected empty deps after remove, got %v", deps)
	}
}

func TestGetDependencies(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	idA, _ := s.CreateWrit("Item A", "", "autarch", 2, nil)
	idB, _ := s.CreateWrit("Item B", "", "autarch", 2, nil)
	idC, _ := s.CreateWrit("Item C", "", "autarch", 2, nil)
	idD, _ := s.CreateWrit("Item D", "", "autarch", 2, nil)

	// A depends on B, C, D.
	s.AddDependency(idA, idB)
	s.AddDependency(idA, idC)
	s.AddDependency(idA, idD)

	deps, err := s.GetDependencies(idA)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 3 {
		t.Fatalf("expected 3 deps, got %d", len(deps))
	}

	// Item with no deps → empty.
	deps, err = s.GetDependencies(idB)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 0 {
		t.Fatalf("expected 0 deps for B, got %d", len(deps))
	}
}

func TestGetDependents(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	idX, _ := s.CreateWrit("Item X", "", "autarch", 2, nil)
	idA, _ := s.CreateWrit("Item A", "", "autarch", 2, nil)
	idB, _ := s.CreateWrit("Item B", "", "autarch", 2, nil)
	idC, _ := s.CreateWrit("Item C", "", "autarch", 2, nil)

	// A, B, C all depend on X.
	s.AddDependency(idA, idX)
	s.AddDependency(idB, idX)
	s.AddDependency(idC, idX)

	dependents, err := s.GetDependents(idX)
	if err != nil {
		t.Fatal(err)
	}
	if len(dependents) != 3 {
		t.Fatalf("expected 3 dependents, got %d", len(dependents))
	}
}

func TestIsReady(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	idA, _ := s.CreateWrit("Item A", "", "autarch", 2, nil)
	idB, _ := s.CreateWrit("Item B", "", "autarch", 2, nil)
	idC, _ := s.CreateWrit("Item C", "", "autarch", 2, nil)

	// Item with no deps → ready.
	ready, err := s.IsReady(idA)
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatal("expected item with no deps to be ready")
	}

	// A depends on B (open) → not ready.
	s.AddDependency(idA, idB)
	ready, err = s.IsReady(idA)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("expected item with open dep to be not ready")
	}

	// B → done → A still NOT ready (done != merged).
	s.UpdateWrit(idB, WritUpdates{Status: "done"})
	ready, err = s.IsReady(idA)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("expected item with done (not closed) dep to NOT be ready")
	}

	// B → closed (merged) → A is ready.
	s.CloseWrit(idB)
	ready, err = s.IsReady(idA)
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatal("expected item with closed dep to be ready")
	}

	// Add dep on C (open) → A not ready again (mixed).
	s.AddDependency(idA, idC)
	ready, err = s.IsReady(idA)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("expected item with mixed deps (one closed, one open) to not be ready")
	}

	// Close C → A is ready.
	s.CloseWrit(idC)
	ready, err = s.IsReady(idA)
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatal("expected item with all closed deps to be ready")
	}
}

func TestV4Migration(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	// Verify the schema version.
	var v int
	if err := s.DB().QueryRow("SELECT version FROM schema_version").Scan(&v); err != nil {
		t.Fatalf("failed to get schema version: %v", err)
	}
	if v != CurrentWorldSchema {
		t.Errorf("schema version = %d, want %d", v, CurrentWorldSchema)
	}

	// Verify dependencies table exists.
	var count int
	err := s.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='dependencies'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected dependencies table, got count=%d", count)
	}
}
