package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadyWritsNoDeps(t *testing.T) {
	s := setupWorld(t)

	// Create open writs with no dependencies — all should be ready.
	id1, _ := s.CreateWrit("Item A", "", "autarch", 1, nil)
	id2, _ := s.CreateWrit("Item B", "", "autarch", 2, nil)
	id3, _ := s.CreateWrit("Item C", "", "autarch", 3, nil)

	ready, err := s.ReadyWrits()
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 3 {
		t.Fatalf("expected 3 ready writs, got %d", len(ready))
	}
	// Should be sorted by priority ASC.
	if ready[0].ID != id1 {
		t.Errorf("expected first ready writ to be %s (pri 1), got %s", id1, ready[0].ID)
	}
	if ready[1].ID != id2 {
		t.Errorf("expected second ready writ to be %s (pri 2), got %s", id2, ready[1].ID)
	}
	if ready[2].ID != id3 {
		t.Errorf("expected third ready writ to be %s (pri 3), got %s", id3, ready[2].ID)
	}
}

func TestReadyWritsBlockedByOpenWrit(t *testing.T) {
	s := setupWorld(t)

	idA, _ := s.CreateWrit("Item A", "", "autarch", 2, nil)
	idB, _ := s.CreateWrit("Item B", "", "autarch", 2, nil)

	// A depends on B (B is open) → A is NOT ready, B IS ready.
	s.AddDependency(idA, idB)

	ready, err := s.ReadyWrits()
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready writ, got %d", len(ready))
	}
	if ready[0].ID != idB {
		t.Fatalf("expected ready writ to be %s (B), got %s", idB, ready[0].ID)
	}
}

func TestReadyWritsBlockedByClosedWrit(t *testing.T) {
	s := setupWorld(t)

	idA, _ := s.CreateWrit("Item A", "", "autarch", 2, nil)
	idB, _ := s.CreateWrit("Item B", "", "autarch", 2, nil)

	// A depends on B, close B → A is ready.
	s.AddDependency(idA, idB)
	s.CloseWrit(idB)

	ready, err := s.ReadyWrits()
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready writ, got %d", len(ready))
	}
	if ready[0].ID != idA {
		t.Fatalf("expected ready writ to be %s (A), got %s", idA, ready[0].ID)
	}
}

func TestReadyWritsTransitiveBlocking(t *testing.T) {
	s := setupWorld(t)

	idA, _ := s.CreateWrit("Item A", "", "autarch", 2, nil)
	idB, _ := s.CreateWrit("Item B", "", "autarch", 2, nil)
	idC, _ := s.CreateWrit("Item C", "", "autarch", 2, nil)

	// C depends on B, B depends on A. A is open.
	// → Only A is ready (B is blocked by A, C is blocked by B).
	s.AddDependency(idC, idB)
	s.AddDependency(idB, idA)

	ready, err := s.ReadyWrits()
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready writ, got %d", len(ready))
	}
	if ready[0].ID != idA {
		t.Fatalf("expected ready writ to be %s (A), got %s", idA, ready[0].ID)
	}
}

func TestReadyWritsTransitiveBlockingPartialClose(t *testing.T) {
	s := setupWorld(t)

	idA, _ := s.CreateWrit("Item A", "", "autarch", 2, nil)
	idB, _ := s.CreateWrit("Item B", "", "autarch", 2, nil)
	idC, _ := s.CreateWrit("Item C", "", "autarch", 2, nil)

	// C → B → A. Close A → B becomes ready, C still blocked by B.
	s.AddDependency(idC, idB)
	s.AddDependency(idB, idA)
	s.CloseWrit(idA)

	ready, err := s.ReadyWrits()
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready writ, got %d", len(ready))
	}
	if ready[0].ID != idB {
		t.Fatalf("expected ready writ to be %s (B), got %s", idB, ready[0].ID)
	}

	// Close B → C becomes ready.
	s.CloseWrit(idB)
	ready, err = s.ReadyWrits()
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready writ, got %d", len(ready))
	}
	if ready[0].ID != idC {
		t.Fatalf("expected ready writ to be %s (C), got %s", idC, ready[0].ID)
	}
}

func TestReadyWritsExcludesNonOpenStatuses(t *testing.T) {
	s := setupWorld(t)

	s.CreateWrit("Open writ", "", "autarch", 2, nil)
	tethered, _ := s.CreateWrit("Tethered writ", "", "autarch", 2, nil)
	done, _ := s.CreateWrit("Done writ", "", "autarch", 2, nil)
	closed, _ := s.CreateWrit("Closed writ", "", "autarch", 2, nil)

	s.UpdateWrit(tethered, WritUpdates{Status: "tethered"})
	s.UpdateWrit(done, WritUpdates{Status: "done"})
	s.CloseWrit(closed)

	ready, err := s.ReadyWrits()
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready writ (only open), got %d", len(ready))
	}
	if ready[0].Title != "Open writ" {
		t.Fatalf("expected 'Open writ', got %q", ready[0].Title)
	}
}

func TestReadyWritsWithLabels(t *testing.T) {
	s := setupWorld(t)

	id1, _ := s.CreateWrit("Labeled writ", "", "autarch", 2, []string{"infra", "urgent"})
	s.CreateWrit("Unlabeled writ", "", "autarch", 2, nil)

	ready, err := s.ReadyWrits()
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready writs, got %d", len(ready))
	}

	// Find the labeled writ and check labels.
	for _, w := range ready {
		if w.ID == id1 {
			if len(w.Labels) != 2 {
				t.Fatalf("expected 2 labels, got %d", len(w.Labels))
			}
			return
		}
	}
	t.Fatal("labeled writ not found in results")
}

func TestReadyWritsDoneDepNotReady(t *testing.T) {
	s := setupWorld(t)

	idA, _ := s.CreateWrit("Item A", "", "autarch", 2, nil)
	idB, _ := s.CreateWrit("Item B", "", "autarch", 2, nil)

	// A depends on B, B is "done" (not closed) → A is NOT ready.
	s.AddDependency(idA, idB)
	s.UpdateWrit(idB, WritUpdates{Status: "done"})

	ready, err := s.ReadyWrits()
	if err != nil {
		t.Fatal(err)
	}
	// Neither A nor B should be ready: A's dep (B) is not closed, B is not "open".
	if len(ready) != 0 {
		t.Fatalf("expected 0 ready writs (B is done not closed), got %d", len(ready))
	}
}

func TestIsWritBlockedByCaravanDepsBlocking(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	sphereStore, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer sphereStore.Close()

	worldStore, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	idA, _ := worldStore.CreateWrit("Item A", "", "autarch", 2, nil)
	worldStore.Close()

	prereq, _ := sphereStore.CreateCaravan("prereq", "autarch")
	dependent, _ := sphereStore.CreateCaravan("dependent", "autarch")

	sphereStore.CreateCaravanItem(dependent, idA, "ember", 0)
	sphereStore.AddCaravanDependency(dependent, prereq)

	// Writ should be blocked (prereq caravan is open).
	blocked, err := sphereStore.IsWritBlockedByCaravan(idA, "ember", OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	if !blocked {
		t.Fatal("expected writ blocked by caravan deps")
	}

	// Close prereq → no longer blocked.
	sphereStore.UpdateCaravanStatus(prereq, "closed")
	blocked, err = sphereStore.IsWritBlockedByCaravan(idA, "ember", OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	if blocked {
		t.Fatal("expected writ NOT blocked after prereq closed")
	}
}

func TestIsWritBlockedByCaravanPhaseGating(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	sphereStore, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer sphereStore.Close()

	worldStore, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	idA, _ := worldStore.CreateWrit("Phase 0 item", "", "autarch", 2, nil)
	idB, _ := worldStore.CreateWrit("Phase 1 item", "", "autarch", 2, nil)
	worldStore.Close()

	caravan, _ := sphereStore.CreateCaravan("phased", "autarch")
	sphereStore.CreateCaravanItem(caravan, idA, "ember", 0)
	sphereStore.CreateCaravanItem(caravan, idB, "ember", 1)

	// Phase 0 item should NOT be blocked (phase 0 has no phase gating).
	blocked, err := sphereStore.IsWritBlockedByCaravan(idA, "ember", OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	if blocked {
		t.Fatal("expected phase 0 item NOT blocked")
	}

	// Phase 1 item should be blocked (phase 0 item is still open).
	blocked, err = sphereStore.IsWritBlockedByCaravan(idB, "ember", OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	if blocked == false {
		t.Fatal("expected phase 1 item blocked (phase 0 item not closed)")
	}

	// Close phase 0 item → phase 1 item should be unblocked.
	worldStore2, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	worldStore2.CloseWrit(idA)
	worldStore2.Close()

	blocked, err = sphereStore.IsWritBlockedByCaravan(idB, "ember", OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	if blocked {
		t.Fatal("expected phase 1 item NOT blocked after phase 0 closed")
	}
}

func TestIsWritBlockedByCaravanNoCaravan(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	sphereStore, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer sphereStore.Close()

	// Writ not in any caravan → not blocked.
	blocked, err := sphereStore.IsWritBlockedByCaravan("sol-nonexist", "ember", OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	if blocked {
		t.Fatal("expected writ NOT blocked (not in any caravan)")
	}
}
