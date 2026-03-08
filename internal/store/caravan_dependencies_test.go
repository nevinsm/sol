package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddCaravanDependency(t *testing.T) {
	s := setupSphere(t)

	idA, _ := s.CreateCaravan("caravan-a", "operator")
	idB, _ := s.CreateCaravan("caravan-b", "operator")

	if err := s.AddCaravanDependency(idA, idB); err != nil {
		t.Fatalf("AddCaravanDependency() error: %v", err)
	}

	deps, err := s.GetCaravanDependencies(idA)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || deps[0] != idB {
		t.Fatalf("expected [%s], got %v", idB, deps)
	}

	dependents, err := s.GetCaravanDependents(idB)
	if err != nil {
		t.Fatal(err)
	}
	if len(dependents) != 1 || dependents[0] != idA {
		t.Fatalf("expected [%s], got %v", idA, dependents)
	}
}

func TestAddCaravanDependencySelfRef(t *testing.T) {
	s := setupSphere(t)
	idA, _ := s.CreateCaravan("caravan-a", "operator")

	err := s.AddCaravanDependency(idA, idA)
	if err == nil {
		t.Fatal("expected error for self-dependency")
	}
}

func TestAddCaravanDependencyNonexistent(t *testing.T) {
	s := setupSphere(t)
	idA, _ := s.CreateCaravan("caravan-a", "operator")

	err := s.AddCaravanDependency(idA, "car-nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent caravan")
	}

	err = s.AddCaravanDependency("car-nonexistent", idA)
	if err == nil {
		t.Fatal("expected error for nonexistent caravan")
	}
}

func TestAddCaravanDependencyCycleDetection(t *testing.T) {
	s := setupSphere(t)

	idA, _ := s.CreateCaravan("caravan-a", "operator")
	idB, _ := s.CreateCaravan("caravan-b", "operator")
	idC, _ := s.CreateCaravan("caravan-c", "operator")

	// A → B → C.
	s.AddCaravanDependency(idA, idB)
	s.AddCaravanDependency(idB, idC)

	// C → A would create a cycle.
	err := s.AddCaravanDependency(idC, idA)
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
}

func TestAddCaravanDependencyIdempotent(t *testing.T) {
	s := setupSphere(t)

	idA, _ := s.CreateCaravan("caravan-a", "operator")
	idB, _ := s.CreateCaravan("caravan-b", "operator")

	// Adding twice should not error (INSERT OR IGNORE).
	s.AddCaravanDependency(idA, idB)
	if err := s.AddCaravanDependency(idA, idB); err != nil {
		t.Fatalf("expected idempotent add, got error: %v", err)
	}

	deps, _ := s.GetCaravanDependencies(idA)
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(deps))
	}
}

func TestRemoveCaravanDependency(t *testing.T) {
	s := setupSphere(t)

	idA, _ := s.CreateCaravan("caravan-a", "operator")
	idB, _ := s.CreateCaravan("caravan-b", "operator")

	s.AddCaravanDependency(idA, idB)
	s.RemoveCaravanDependency(idA, idB)

	deps, _ := s.GetCaravanDependencies(idA)
	if len(deps) != 0 {
		t.Fatalf("expected 0 dependencies after remove, got %d", len(deps))
	}
}

func TestAreCaravanDependenciesSatisfied(t *testing.T) {
	s := setupSphere(t)

	idA, _ := s.CreateCaravan("caravan-a", "operator")
	idB, _ := s.CreateCaravan("caravan-b", "operator")

	s.AddCaravanDependency(idA, idB)

	// B is open → A's deps not satisfied.
	satisfied, err := s.AreCaravanDependenciesSatisfied(idA)
	if err != nil {
		t.Fatal(err)
	}
	if satisfied {
		t.Fatal("expected dependencies NOT satisfied (B is open)")
	}

	// Close B → A's deps satisfied.
	s.UpdateCaravanStatus(idB, "closed")

	satisfied, err = s.AreCaravanDependenciesSatisfied(idA)
	if err != nil {
		t.Fatal(err)
	}
	if !satisfied {
		t.Fatal("expected dependencies satisfied (B is closed)")
	}
}

func TestAreCaravanDependenciesSatisfiedNoDeps(t *testing.T) {
	s := setupSphere(t)

	idA, _ := s.CreateCaravan("caravan-a", "operator")

	satisfied, err := s.AreCaravanDependenciesSatisfied(idA)
	if err != nil {
		t.Fatal(err)
	}
	if !satisfied {
		t.Fatal("expected dependencies satisfied (no deps)")
	}
}

func TestUnsatisfiedCaravanDependencies(t *testing.T) {
	s := setupSphere(t)

	idA, _ := s.CreateCaravan("caravan-a", "operator")
	idB, _ := s.CreateCaravan("caravan-b", "operator")
	idC, _ := s.CreateCaravan("caravan-c", "operator")

	s.AddCaravanDependency(idA, idB)
	s.AddCaravanDependency(idA, idC)

	// Both open → both unsatisfied.
	unsatisfied, err := s.UnsatisfiedCaravanDependencies(idA)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsatisfied) != 2 {
		t.Fatalf("expected 2 unsatisfied, got %d", len(unsatisfied))
	}

	// Close B → only C unsatisfied.
	s.UpdateCaravanStatus(idB, "closed")
	unsatisfied, _ = s.UnsatisfiedCaravanDependencies(idA)
	if len(unsatisfied) != 1 || unsatisfied[0] != idC {
		t.Fatalf("expected [%s], got %v", idC, unsatisfied)
	}

	// Close C → none unsatisfied.
	s.UpdateCaravanStatus(idC, "closed")
	unsatisfied, _ = s.UnsatisfiedCaravanDependencies(idA)
	if len(unsatisfied) != 0 {
		t.Fatalf("expected 0 unsatisfied, got %d", len(unsatisfied))
	}
}

func TestDeleteCaravanDependencies(t *testing.T) {
	s := setupSphere(t)

	idA, _ := s.CreateCaravan("caravan-a", "operator")
	idB, _ := s.CreateCaravan("caravan-b", "operator")
	idC, _ := s.CreateCaravan("caravan-c", "operator")

	s.AddCaravanDependency(idA, idB)
	s.AddCaravanDependency(idC, idA) // C depends on A

	s.DeleteCaravanDependencies(idA)

	// A should have no deps.
	deps, _ := s.GetCaravanDependencies(idA)
	if len(deps) != 0 {
		t.Fatalf("expected 0 deps for A, got %d", len(deps))
	}
	// C should no longer depend on A.
	deps, _ = s.GetCaravanDependencies(idC)
	if len(deps) != 0 {
		t.Fatalf("expected 0 deps for C, got %d", len(deps))
	}
}

func TestCheckCaravanReadinessWithCaravanDeps(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	sphereStore, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer sphereStore.Close()

	// Create writs in world.
	worldStore, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	idA, _ := worldStore.CreateWrit("Item A", "", "operator", 2, nil)
	idB, _ := worldStore.CreateWrit("Item B", "", "operator", 2, nil)
	worldStore.Close()

	// Create two caravans.
	prereqID, _ := sphereStore.CreateCaravan("prereq-caravan", "operator")
	dependentID, _ := sphereStore.CreateCaravan("dependent-caravan", "operator")

	// Add items.
	sphereStore.CreateCaravanItem(prereqID, idA, "ember", 0)
	sphereStore.CreateCaravanItem(dependentID, idB, "ember", 0)

	// Dependent depends on prereq.
	sphereStore.AddCaravanDependency(dependentID, prereqID)

	// Item B should NOT be ready (prereq caravan is open).
	statuses, err := sphereStore.CheckCaravanReadiness(dependentID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].Ready {
		t.Fatal("expected item B NOT ready (prereq caravan is open)")
	}

	// Item A (in prereq caravan) should still be ready — no deps on it.
	statuses, err = sphereStore.CheckCaravanReadiness(prereqID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if !statuses[0].Ready {
		t.Fatal("expected item A ready (prereq caravan has no deps)")
	}

	// Close prereq caravan → item B should become ready.
	sphereStore.UpdateCaravanStatus(prereqID, "closed")

	statuses, err = sphereStore.CheckCaravanReadiness(dependentID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	if !statuses[0].Ready {
		t.Fatal("expected item B ready after prereq caravan closed")
	}
}

func TestCheckCaravanReadinessCaravanDepsPartialClose(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	sphereStore, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer sphereStore.Close()

	// Create writs.
	worldStore, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	idA, _ := worldStore.CreateWrit("Item A", "", "operator", 2, nil)
	worldStore.Close()

	// Two prerequisite caravans.
	prereq1, _ := sphereStore.CreateCaravan("prereq-1", "operator")
	prereq2, _ := sphereStore.CreateCaravan("prereq-2", "operator")
	dependent, _ := sphereStore.CreateCaravan("dependent", "operator")

	sphereStore.CreateCaravanItem(dependent, idA, "ember", 0)
	sphereStore.AddCaravanDependency(dependent, prereq1)
	sphereStore.AddCaravanDependency(dependent, prereq2)

	// Close only prereq1 → dependent still blocked.
	sphereStore.UpdateCaravanStatus(prereq1, "closed")
	statuses, _ := sphereStore.CheckCaravanReadiness(dependent, OpenWorld)
	if statuses[0].Ready {
		t.Fatal("expected item NOT ready (prereq-2 still open)")
	}

	// Close prereq2 → dependent unblocked.
	sphereStore.UpdateCaravanStatus(prereq2, "closed")
	statuses, _ = sphereStore.CheckCaravanReadiness(dependent, OpenWorld)
	if !statuses[0].Ready {
		t.Fatal("expected item ready (both prereq caravans closed)")
	}
}

func TestIsWritBlockedByCaravanDeps(t *testing.T) {
	s := setupSphere(t)

	prereq, _ := s.CreateCaravan("prereq", "operator")
	dependent, _ := s.CreateCaravan("dependent", "operator")

	s.CreateCaravanItem(dependent, "sol-11111111", "ember", 0)
	s.AddCaravanDependency(dependent, prereq)

	// sol-11111111 should be blocked.
	blocked, blockers, err := s.IsWritBlockedByCaravanDeps("sol-11111111")
	if err != nil {
		t.Fatal(err)
	}
	if !blocked {
		t.Fatal("expected writ blocked by caravan deps")
	}
	if len(blockers) != 1 || blockers[0] != prereq {
		t.Fatalf("expected blocker [%s], got %v", prereq, blockers)
	}

	// Close prereq → no longer blocked.
	s.UpdateCaravanStatus(prereq, "closed")
	blocked, _, err = s.IsWritBlockedByCaravanDeps("sol-11111111")
	if err != nil {
		t.Fatal(err)
	}
	if blocked {
		t.Fatal("expected writ NOT blocked after prereq closed")
	}
}

func TestIsWritBlockedByCaravanDepsNoCaravan(t *testing.T) {
	s := setupSphere(t)

	// Work item not in any caravan → not blocked.
	blocked, _, err := s.IsWritBlockedByCaravanDeps("sol-99999999")
	if err != nil {
		t.Fatal(err)
	}
	if blocked {
		t.Fatal("expected writ NOT blocked (not in any caravan)")
	}
}

func TestIsWritBlockedByCaravanMultiWorld(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	sphereStore, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer sphereStore.Close()

	// Create writs in two different worlds.
	worldAlpha, err := OpenWorld("alpha")
	if err != nil {
		t.Fatal(err)
	}
	alphaWrit, _ := worldAlpha.CreateWrit("Alpha item", "", "operator", 2, nil)
	worldAlpha.Close()

	worldBeta, err := OpenWorld("beta")
	if err != nil {
		t.Fatal(err)
	}
	betaWrit, _ := worldBeta.CreateWrit("Beta item", "", "operator", 2, nil)
	worldBeta.Close()

	worldGamma, err := OpenWorld("gamma")
	if err != nil {
		t.Fatal(err)
	}
	gammaWrit, _ := worldGamma.CreateWrit("Gamma item", "", "operator", 2, nil)
	worldGamma.Close()

	// Create a caravan with phase 0 items in alpha and beta, phase 1 item in gamma.
	caravanID, _ := sphereStore.CreateCaravan("multi-world-caravan", "operator")
	sphereStore.CreateCaravanItem(caravanID, alphaWrit, "alpha", 0)
	sphereStore.CreateCaravanItem(caravanID, betaWrit, "beta", 0)
	sphereStore.CreateCaravanItem(caravanID, gammaWrit, "gamma", 1)

	// gammaWrit (phase 1) should be blocked — alpha and beta items are not closed.
	blocked, err := sphereStore.IsWritBlockedByCaravan(gammaWrit, "gamma", OpenWorld)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan error: %v", err)
	}
	if !blocked {
		t.Fatal("expected gamma writ blocked (alpha and beta items open)")
	}

	// Close only the alpha item — still blocked (beta is open).
	worldAlpha, err = OpenWorld("alpha")
	if err != nil {
		t.Fatal(err)
	}
	worldAlpha.CloseWrit(alphaWrit)
	worldAlpha.Close()

	blocked, err = sphereStore.IsWritBlockedByCaravan(gammaWrit, "gamma", OpenWorld)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan error: %v", err)
	}
	if !blocked {
		t.Fatal("expected gamma writ still blocked (beta item open)")
	}

	// Close the beta item — gamma should be unblocked.
	worldBeta, err = OpenWorld("beta")
	if err != nil {
		t.Fatal(err)
	}
	worldBeta.CloseWrit(betaWrit)
	worldBeta.Close()

	blocked, err = sphereStore.IsWritBlockedByCaravan(gammaWrit, "gamma", OpenWorld)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan error: %v", err)
	}
	if blocked {
		t.Fatal("expected gamma writ NOT blocked (all lower phase items closed)")
	}

	// Phase 0 items should never be blocked by phase gating.
	blocked, err = sphereStore.IsWritBlockedByCaravan(alphaWrit, "alpha", OpenWorld)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan error: %v", err)
	}
	if blocked {
		t.Fatal("expected alpha writ NOT blocked (phase 0)")
	}
}

func TestIsWritBlockedByCaravanNotInCaravan(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	sphereStore, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer sphereStore.Close()

	// Writ not in any caravan → not blocked.
	blocked, err := sphereStore.IsWritBlockedByCaravan("sol-99999999", "ember", OpenWorld)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan error: %v", err)
	}
	if blocked {
		t.Fatal("expected writ NOT blocked (not in any caravan)")
	}
}
