package store

import "testing"

func TestHasOpenTransitiveDependents_NoDependents(t *testing.T) {
	s := setupWorld(t)
	id, _ := s.CreateWrit("Solo writ", "", "operator", 2, nil)

	has, err := s.HasOpenTransitiveDependents(id)
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("expected no open transitive dependents for writ with no dependents")
	}
}

func TestHasOpenTransitiveDependents_DirectOpenDependent(t *testing.T) {
	s := setupWorld(t)
	idA, _ := s.CreateWrit("Writ A", "", "operator", 2, nil)
	idB, _ := s.CreateWrit("Writ B", "", "operator", 2, nil)

	// B depends on A → A has dependent B.
	s.AddDependency(idB, idA)

	// B is open → A should have open transitive dependents.
	has, err := s.HasOpenTransitiveDependents(idA)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected open transitive dependents when direct dependent is open")
	}
}

func TestHasOpenTransitiveDependents_AllDependentsClosed(t *testing.T) {
	s := setupWorld(t)
	idA, _ := s.CreateWrit("Writ A", "", "operator", 2, nil)
	idB, _ := s.CreateWrit("Writ B", "", "operator", 2, nil)

	// B depends on A.
	s.AddDependency(idB, idA)

	// Close B.
	s.CloseWrit(idB)

	has, err := s.HasOpenTransitiveDependents(idA)
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("expected no open transitive dependents when all dependents are closed")
	}
}

func TestHasOpenTransitiveDependents_TransitiveOpenDependent(t *testing.T) {
	s := setupWorld(t)
	idA, _ := s.CreateWrit("Writ A", "", "operator", 2, nil)
	idB, _ := s.CreateWrit("Writ B", "", "operator", 2, nil)
	idC, _ := s.CreateWrit("Writ C", "", "operator", 2, nil)

	// B depends on A, C depends on B → A → B → C.
	s.AddDependency(idB, idA)
	s.AddDependency(idC, idB)

	// Close B but leave C open → A has open transitive dependent (C).
	s.CloseWrit(idB)

	has, err := s.HasOpenTransitiveDependents(idA)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected open transitive dependent when indirect dependent C is open")
	}

	// Close C → A should have no open transitive dependents.
	s.CloseWrit(idC)

	has, err = s.HasOpenTransitiveDependents(idA)
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("expected no open transitive dependents when all are closed")
	}
}
