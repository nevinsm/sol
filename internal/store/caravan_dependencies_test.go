package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddCaravanDependency(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	idA, _ := s.CreateCaravan("caravan-a", "autarch")
	idB, _ := s.CreateCaravan("caravan-b", "autarch")

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
	t.Parallel()
	s := setupSphere(t)
	idA, _ := s.CreateCaravan("caravan-a", "autarch")

	err := s.AddCaravanDependency(idA, idA)
	if err == nil {
		t.Fatal("expected error for self-dependency")
	}
}

func TestAddCaravanDependencyNonexistent(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)
	idA, _ := s.CreateCaravan("caravan-a", "autarch")

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
	t.Parallel()
	s := setupSphere(t)

	idA, _ := s.CreateCaravan("caravan-a", "autarch")
	idB, _ := s.CreateCaravan("caravan-b", "autarch")
	idC, _ := s.CreateCaravan("caravan-c", "autarch")

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
	t.Parallel()
	s := setupSphere(t)

	idA, _ := s.CreateCaravan("caravan-a", "autarch")
	idB, _ := s.CreateCaravan("caravan-b", "autarch")

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
	t.Parallel()
	s := setupSphere(t)

	idA, _ := s.CreateCaravan("caravan-a", "autarch")
	idB, _ := s.CreateCaravan("caravan-b", "autarch")

	s.AddCaravanDependency(idA, idB)
	s.RemoveCaravanDependency(idA, idB)

	deps, _ := s.GetCaravanDependencies(idA)
	if len(deps) != 0 {
		t.Fatalf("expected 0 dependencies after remove, got %d", len(deps))
	}
}

func TestAreCaravanDependenciesSatisfied(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	idA, _ := s.CreateCaravan("caravan-a", "autarch")
	idB, _ := s.CreateCaravan("caravan-b", "autarch")

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
	t.Parallel()
	s := setupSphere(t)

	idA, _ := s.CreateCaravan("caravan-a", "autarch")

	satisfied, err := s.AreCaravanDependenciesSatisfied(idA)
	if err != nil {
		t.Fatal(err)
	}
	if !satisfied {
		t.Fatal("expected dependencies satisfied (no deps)")
	}
}

func TestUnsatisfiedCaravanDependencies(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	idA, _ := s.CreateCaravan("caravan-a", "autarch")
	idB, _ := s.CreateCaravan("caravan-b", "autarch")
	idC, _ := s.CreateCaravan("caravan-c", "autarch")

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
	t.Parallel()
	s := setupSphere(t)

	idA, _ := s.CreateCaravan("caravan-a", "autarch")
	idB, _ := s.CreateCaravan("caravan-b", "autarch")
	idC, _ := s.CreateCaravan("caravan-c", "autarch")

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
	t.Parallel()
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".store")
	os.MkdirAll(storeDir, 0o755)

	openWorldByName := makeWorldOpener(t, storeDir)
	sphereStore := openSphereAt(t, filepath.Join(storeDir, "sphere.db"))

	// Create writs in world.
	worldStore := openWorldAt(t, filepath.Join(storeDir, "ember.db"))
	idA, _ := worldStore.CreateWrit("Item A", "", "autarch", 2, nil)
	idB, _ := worldStore.CreateWrit("Item B", "", "autarch", 2, nil)
	worldStore.Close()

	// Create two caravans.
	prereqID, _ := sphereStore.CreateCaravan("prereq-caravan", "autarch")
	dependentID, _ := sphereStore.CreateCaravan("dependent-caravan", "autarch")

	// Add items.
	sphereStore.CreateCaravanItem(prereqID, idA, "ember", 0)
	sphereStore.CreateCaravanItem(dependentID, idB, "ember", 0)

	// Dependent depends on prereq.
	sphereStore.AddCaravanDependency(dependentID, prereqID)

	// Item B should NOT be ready (prereq caravan is open).
	statuses, err := sphereStore.CheckCaravanReadiness(dependentID, openWorldByName)
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
	statuses, err = sphereStore.CheckCaravanReadiness(prereqID, openWorldByName)
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

	statuses, err = sphereStore.CheckCaravanReadiness(dependentID, openWorldByName)
	if err != nil {
		t.Fatal(err)
	}
	if !statuses[0].Ready {
		t.Fatal("expected item B ready after prereq caravan closed")
	}
}

func TestCheckCaravanReadinessCaravanDepsPartialClose(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".store")
	os.MkdirAll(storeDir, 0o755)

	openWorldByName := makeWorldOpener(t, storeDir)
	sphereStore := openSphereAt(t, filepath.Join(storeDir, "sphere.db"))

	// Create writs.
	worldStore := openWorldAt(t, filepath.Join(storeDir, "ember.db"))
	idA, _ := worldStore.CreateWrit("Item A", "", "autarch", 2, nil)
	worldStore.Close()

	// Two prerequisite caravans.
	prereq1, _ := sphereStore.CreateCaravan("prereq-1", "autarch")
	prereq2, _ := sphereStore.CreateCaravan("prereq-2", "autarch")
	dependent, _ := sphereStore.CreateCaravan("dependent", "autarch")

	sphereStore.CreateCaravanItem(dependent, idA, "ember", 0)
	sphereStore.AddCaravanDependency(dependent, prereq1)
	sphereStore.AddCaravanDependency(dependent, prereq2)

	// Close only prereq1 → dependent still blocked.
	sphereStore.UpdateCaravanStatus(prereq1, "closed")
	statuses, _ := sphereStore.CheckCaravanReadiness(dependent, openWorldByName)
	if statuses[0].Ready {
		t.Fatal("expected item NOT ready (prereq-2 still open)")
	}

	// Close prereq2 → dependent unblocked.
	sphereStore.UpdateCaravanStatus(prereq2, "closed")
	statuses, _ = sphereStore.CheckCaravanReadiness(dependent, openWorldByName)
	if !statuses[0].Ready {
		t.Fatal("expected item ready (both prereq caravans closed)")
	}
}

func TestIsWritBlockedByCaravanDeps(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	prereq, _ := s.CreateCaravan("prereq", "autarch")
	dependent, _ := s.CreateCaravan("dependent", "autarch")

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
	t.Parallel()
	s := setupSphere(t)

	// Writ not in any caravan → not blocked.
	blocked, _, err := s.IsWritBlockedByCaravanDeps("sol-99999999")
	if err != nil {
		t.Fatal(err)
	}
	if blocked {
		t.Fatal("expected writ NOT blocked (not in any caravan)")
	}
}

func TestIsWritBlockedByCaravanMultiWorld(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".store")
	os.MkdirAll(storeDir, 0o755)

	openWorldByName := makeWorldOpener(t, storeDir)
	sphereStore := openSphereAt(t, filepath.Join(storeDir, "sphere.db"))

	// Create writs in two different worlds.
	worldAlpha := openWorldAt(t, filepath.Join(storeDir, "alpha.db"))
	alphaWrit, _ := worldAlpha.CreateWrit("Alpha item", "", "autarch", 2, nil)
	worldAlpha.Close()

	worldBeta := openWorldAt(t, filepath.Join(storeDir, "beta.db"))
	betaWrit, _ := worldBeta.CreateWrit("Beta item", "", "autarch", 2, nil)
	worldBeta.Close()

	worldGamma := openWorldAt(t, filepath.Join(storeDir, "gamma.db"))
	gammaWrit, _ := worldGamma.CreateWrit("Gamma item", "", "autarch", 2, nil)
	worldGamma.Close()

	// Create a caravan with phase 0 items in alpha and beta, phase 1 item in gamma.
	caravanID, _ := sphereStore.CreateCaravan("multi-world-caravan", "autarch")
	sphereStore.CreateCaravanItem(caravanID, alphaWrit, "alpha", 0)
	sphereStore.CreateCaravanItem(caravanID, betaWrit, "beta", 0)
	sphereStore.CreateCaravanItem(caravanID, gammaWrit, "gamma", 1)

	// gammaWrit (phase 1) should be blocked — alpha and beta items are not closed.
	blocked, err := sphereStore.IsWritBlockedByCaravan(gammaWrit, "gamma", openWorldByName)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan error: %v", err)
	}
	if !blocked {
		t.Fatal("expected gamma writ blocked (alpha and beta items open)")
	}

	// Close only the alpha item — still blocked (beta is open).
	worldAlpha = openWorldAt(t, filepath.Join(storeDir, "alpha.db"))
	worldAlpha.CloseWrit(alphaWrit)
	worldAlpha.Close()

	blocked, err = sphereStore.IsWritBlockedByCaravan(gammaWrit, "gamma", openWorldByName)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan error: %v", err)
	}
	if !blocked {
		t.Fatal("expected gamma writ still blocked (beta item open)")
	}

	// Close the beta item — gamma should be unblocked.
	worldBeta = openWorldAt(t, filepath.Join(storeDir, "beta.db"))
	worldBeta.CloseWrit(betaWrit)
	worldBeta.Close()

	blocked, err = sphereStore.IsWritBlockedByCaravan(gammaWrit, "gamma", openWorldByName)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan error: %v", err)
	}
	if blocked {
		t.Fatal("expected gamma writ NOT blocked (all lower phase items closed)")
	}

	// Phase 0 items should never be blocked by phase gating.
	blocked, err = sphereStore.IsWritBlockedByCaravan(alphaWrit, "alpha", openWorldByName)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan error: %v", err)
	}
	if blocked {
		t.Fatal("expected alpha writ NOT blocked (phase 0)")
	}
}

func TestIsWritBlockedByCaravanNotInCaravan(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".store")
	os.MkdirAll(storeDir, 0o755)

	openWorldByName := makeWorldOpener(t, storeDir)
	sphereStore := openSphereAt(t, filepath.Join(storeDir, "sphere.db"))

	// Writ not in any caravan → not blocked.
	blocked, err := sphereStore.IsWritBlockedByCaravan("sol-99999999", "ember", openWorldByName)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan error: %v", err)
	}
	if blocked {
		t.Fatal("expected writ NOT blocked (not in any caravan)")
	}
}
