package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

// fakeWorldStore is an in-memory implementation of MaterializeWorldStore
// that supports injecting faults at specific call counts. It records every
// successfully created writ so tests can assert on rollback behavior.
type fakeWorldStore struct {
	writs map[string]*store.Writ // id → writ (deleted writs are removed)

	createCalls int
	failCreateAfter int // 0 = never; N = the Nth call returns an error

	closeCalls int
	failCloseAfter int

	addDepCalls    int
	failAddDepAfter int

	target *store.Writ // optional fixture returned by GetWrit

	idCounter int
}

func newFakeWorldStore() *fakeWorldStore {
	return &fakeWorldStore{writs: map[string]*store.Writ{}}
}

func (f *fakeWorldStore) GetWrit(id string) (*store.Writ, error) {
	if f.target != nil && f.target.ID == id {
		return f.target, nil
	}
	w, ok := f.writs[id]
	if !ok {
		return nil, fmt.Errorf("writ %q not found", id)
	}
	return w, nil
}

func (f *fakeWorldStore) CreateWritWithOpts(opts store.CreateWritOpts) (string, error) {
	f.createCalls++
	if f.failCreateAfter > 0 && f.createCalls == f.failCreateAfter {
		return "", errors.New("injected: CreateWritWithOpts failed")
	}
	f.idCounter++
	id := fmt.Sprintf("sol-fake%016d", f.idCounter)
	f.writs[id] = &store.Writ{
		ID:          id,
		Title:       opts.Title,
		Description: opts.Description,
		Status:      "open",
		CreatedBy:   opts.CreatedBy,
		ParentID:    opts.ParentID,
		Labels:      opts.Labels,
		Kind:        opts.Kind,
	}
	return id, nil
}

func (f *fakeWorldStore) AddDependency(fromID, toID string) error {
	f.addDepCalls++
	if f.failAddDepAfter > 0 && f.addDepCalls == f.failAddDepAfter {
		return errors.New("injected: AddDependency failed")
	}
	return nil
}

func (f *fakeWorldStore) CloseWrit(id string, closeReason ...string) ([]string, error) {
	f.closeCalls++
	if f.failCloseAfter > 0 && f.closeCalls == f.failCloseAfter {
		return nil, errors.New("injected: CloseWrit failed")
	}
	w, ok := f.writs[id]
	if !ok {
		return nil, fmt.Errorf("writ %q not found", id)
	}
	w.Status = "closed"
	if len(closeReason) > 0 {
		w.CloseReason = closeReason[0]
	}
	return nil, nil
}

// listManifestChildren returns IDs of every non-closed writ labeled "manifest-child".
// This mirrors what worldStore.ListWrits would return for the rollback assertion.
func (f *fakeWorldStore) listManifestChildren() []string {
	var ids []string
	for id, w := range f.writs {
		if w.Status == "closed" {
			continue
		}
		for _, l := range w.Labels {
			if l == "manifest-child" {
				ids = append(ids, id)
				break
			}
		}
	}
	return ids
}

// fakeSphereStore is an in-memory implementation of MaterializeSphereStore.
type fakeSphereStore struct {
	caravans map[string]string // id → name
	items    map[string][]store.CaravanItem

	createCaravanCalls       int
	failCreateCaravanAfter   int
	createCaravanItemCalls   int
	failCreateCaravanItemAt  int

	deleteCaravanCalls       int
	failDeleteCaravanAfter   int

	idCounter int
}

func newFakeSphereStore() *fakeSphereStore {
	return &fakeSphereStore{
		caravans: map[string]string{},
		items:    map[string][]store.CaravanItem{},
	}
}

func (f *fakeSphereStore) CreateCaravan(name, owner string) (string, error) {
	f.createCaravanCalls++
	if f.failCreateCaravanAfter > 0 && f.createCaravanCalls == f.failCreateCaravanAfter {
		return "", errors.New("injected: CreateCaravan failed")
	}
	f.idCounter++
	id := fmt.Sprintf("car-fake%016d", f.idCounter)
	f.caravans[id] = name
	return id, nil
}

func (f *fakeSphereStore) CreateCaravanItem(caravanID, writID, world string, phase int) error {
	f.createCaravanItemCalls++
	if f.failCreateCaravanItemAt > 0 && f.createCaravanItemCalls == f.failCreateCaravanItemAt {
		return errors.New("injected: CreateCaravanItem failed")
	}
	f.items[caravanID] = append(f.items[caravanID], store.CaravanItem{
		CaravanID: caravanID,
		WritID:    writID,
		World:     world,
		Phase:     phase,
	})
	return nil
}

func (f *fakeSphereStore) DeleteCaravan(id string) error {
	f.deleteCaravanCalls++
	if f.failDeleteCaravanAfter > 0 && f.deleteCaravanCalls == f.failDeleteCaravanAfter {
		return errors.New("injected: DeleteCaravan failed")
	}
	delete(f.caravans, id)
	delete(f.items, id)
	return nil
}

// writeRollbackWorkflow lays a 6-step linear workflow on disk and returns its name.
// The workflow has the shape required by the rollback test: 6 children, no
// required variables, mode = "manifest". The instruction file is trivially
// rendered (no unresolved {{var}} tokens).
func writeRollbackWorkflow(t *testing.T, name string, count int) {
	t.Helper()
	solHome := os.Getenv("SOL_HOME")
	dir := filepath.Join(solHome, "workflows", name)
	if err := os.MkdirAll(filepath.Join(dir, "steps"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	steps := make([]StepDef, count)
	for i := range count {
		steps[i] = StepDef{
			ID:           fmt.Sprintf("s%d", i+1),
			Title:        fmt.Sprintf("Step %d", i+1),
			Instructions: fmt.Sprintf("steps/s%d.md", i+1),
		}
		if i > 0 {
			steps[i].Needs = []string{fmt.Sprintf("s%d", i)}
		}
	}
	writeTOMLManifestWithFlag(t, dir, name, steps, nil, true)
	for _, s := range steps {
		if err := os.WriteFile(filepath.Join(dir, s.Instructions),
			[]byte("# "+s.Title+"\n\nbody\n"), 0o644); err != nil {
			t.Fatalf("write step: %v", err)
		}
	}
}

// TestMaterializeRollbackOnWritCreateFailure verifies that a fault on the
// 4th CreateWritWithOpts call leaves zero manifest-child writs and zero
// caravan rows behind.
func TestMaterializeRollbackOnWritCreateFailure(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())
	writeRollbackWorkflow(t, "rollback-wf", 6)

	ws := newFakeWorldStore()
	ws.failCreateAfter = 4 // 4th CreateWritWithOpts call returns an error
	ss := newFakeSphereStore()

	_, err := Materialize(ws, ss, ManifestOpts{
		Name:      "rollback-wf",
		World:     "test-world",
		CreatedBy: "autarch",
	})
	if err == nil {
		t.Fatal("Materialize() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to create child writ") {
		t.Errorf("error: got %q, want one wrapping CreateWritWithOpts failure", err.Error())
	}

	// Acceptance: zero leftover manifest-child writs after rollback.
	if leftovers := ws.listManifestChildren(); len(leftovers) != 0 {
		t.Errorf("expected zero manifest-child writs after rollback, got %d: %v",
			len(leftovers), leftovers)
	}

	// Acceptance: rollback closed exactly the 3 successfully-created writs.
	// (The 4th call failed before any write happened, so 3 writs exist.)
	if ws.closeCalls != 3 {
		t.Errorf("CloseWrit call count: got %d, want 3", ws.closeCalls)
	}

	// Acceptance: zero leftover caravans (caravan was never created in this path).
	if len(ss.caravans) != 0 {
		t.Errorf("expected zero caravans after rollback, got %d", len(ss.caravans))
	}
	if ss.createCaravanCalls != 0 {
		t.Errorf("CreateCaravan should not have been called when writ creation failed first; got %d calls",
			ss.createCaravanCalls)
	}
}

// TestMaterializeRollbackOnCaravanItemFailure verifies that a fault during
// CreateCaravanItem rolls back both the writs and the caravan, satisfying
// the second half of the acceptance criteria.
func TestMaterializeRollbackOnCaravanItemFailure(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())
	writeRollbackWorkflow(t, "rollback-cv", 4)

	ws := newFakeWorldStore()
	ss := newFakeSphereStore()
	ss.failCreateCaravanItemAt = 2 // fail mid-loop

	_, err := Materialize(ws, ss, ManifestOpts{
		Name:      "rollback-cv",
		World:     "test-world",
		CreatedBy: "autarch",
	})
	if err == nil {
		t.Fatal("Materialize() expected error, got nil")
	}

	if leftovers := ws.listManifestChildren(); len(leftovers) != 0 {
		t.Errorf("expected zero manifest-child writs after rollback, got %d: %v",
			len(leftovers), leftovers)
	}
	if len(ss.caravans) != 0 {
		t.Errorf("expected zero caravans after rollback, got %d", len(ss.caravans))
	}
	if ws.closeCalls != 4 {
		t.Errorf("CloseWrit call count: got %d, want 4", ws.closeCalls)
	}
	if ss.deleteCaravanCalls != 1 {
		t.Errorf("DeleteCaravan call count: got %d, want 1", ss.deleteCaravanCalls)
	}
}

// TestMaterializeRollbackOnCreateCaravanFailure verifies that a fault on
// CreateCaravan rolls back all writs that were created before the caravan.
func TestMaterializeRollbackOnCreateCaravanFailure(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())
	writeRollbackWorkflow(t, "rollback-cc", 3)

	ws := newFakeWorldStore()
	ss := newFakeSphereStore()
	ss.failCreateCaravanAfter = 1

	_, err := Materialize(ws, ss, ManifestOpts{
		Name:      "rollback-cc",
		World:     "test-world",
		CreatedBy: "autarch",
	})
	if err == nil {
		t.Fatal("Materialize() expected error, got nil")
	}

	if leftovers := ws.listManifestChildren(); len(leftovers) != 0 {
		t.Errorf("expected zero manifest-child writs after rollback, got %d: %v",
			len(leftovers), leftovers)
	}
	if len(ss.caravans) != 0 {
		t.Errorf("expected zero caravans after rollback, got %d", len(ss.caravans))
	}
	// All 3 writs were successfully created before CreateCaravan failed.
	if ws.closeCalls != 3 {
		t.Errorf("CloseWrit call count: got %d, want 3", ws.closeCalls)
	}
}

// TestMaterializeNoRollbackOnSuccess verifies that the deferred rollback
// is a no-op when Materialize succeeds: no writs are closed, the caravan
// remains, and all caravan items are present.
func TestMaterializeNoRollbackOnSuccess(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())
	writeRollbackWorkflow(t, "rollback-ok", 3)

	ws := newFakeWorldStore()
	ss := newFakeSphereStore()

	result, err := Materialize(ws, ss, ManifestOpts{
		Name:      "rollback-ok",
		World:     "test-world",
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	if ws.closeCalls != 0 {
		t.Errorf("CloseWrit should not be called on success; got %d calls", ws.closeCalls)
	}
	if ss.deleteCaravanCalls != 0 {
		t.Errorf("DeleteCaravan should not be called on success; got %d calls", ss.deleteCaravanCalls)
	}
	if len(ws.listManifestChildren()) != 3 {
		t.Errorf("expected 3 manifest-child writs after success, got %d",
			len(ws.listManifestChildren()))
	}
	if _, ok := ss.caravans[result.CaravanID]; !ok {
		t.Errorf("caravan %q missing after successful materialize", result.CaravanID)
	}
}
