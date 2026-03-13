package status

import (
	"fmt"
	"os"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

// --- Caravan mock implementations ---

type mockCaravanStore struct {
	caravans []store.Caravan
	items    map[string][]store.CaravanItem   // keyed by caravan ID
	statuses map[string][]store.CaravanItemStatus // keyed by caravan ID
}

func (m *mockCaravanStore) ListCaravans(status store.CaravanStatus) ([]store.Caravan, error) {
	if status == "" {
		return m.caravans, nil
	}
	var result []store.Caravan
	for _, c := range m.caravans {
		if c.Status == status {
			result = append(result, c)
		}
	}
	return result, nil
}

func (m *mockCaravanStore) ListCaravanItems(caravanID string) ([]store.CaravanItem, error) {
	return m.items[caravanID], nil
}

func (m *mockCaravanStore) CheckCaravanReadiness(caravanID string, _ func(string) (*store.Store, error)) ([]store.CaravanItemStatus, error) {
	return m.statuses[caravanID], nil
}

func (m *mockCaravanStore) GetCaravan(id string) (*store.Caravan, error) {
	return nil, fmt.Errorf("mockCaravanStore.GetCaravan not implemented")
}

func (m *mockCaravanStore) GetCaravanItemsForWrit(writID string) ([]store.CaravanItem, error) {
	return nil, fmt.Errorf("mockCaravanStore.GetCaravanItemsForWrit not implemented")
}

// --- Tests for Bug 1: GatherCaravans done/closed split ---

func TestGatherCaravansSplitsDoneClosed(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	result := &WorldStatus{World: "haven"}

	cs := &mockCaravanStore{
		caravans: []store.Caravan{
			{ID: "car-1111", Name: "batch-1", Status: store.CaravanOpen},
		},
		items: map[string][]store.CaravanItem{
			"car-1111": {
				{CaravanID: "car-1111", WritID: "sol-aaa", World: "haven", Phase: 0},
				{CaravanID: "car-1111", WritID: "sol-bbb", World: "haven", Phase: 0},
				{CaravanID: "car-1111", WritID: "sol-ccc", World: "haven", Phase: 0},
				{CaravanID: "car-1111", WritID: "sol-ddd", World: "haven", Phase: 0},
			},
		},
		statuses: map[string][]store.CaravanItemStatus{
			"car-1111": {
				{WritID: "sol-aaa", World: "haven", WritStatus: store.WritClosed},  // merged
				{WritID: "sol-bbb", World: "haven", WritStatus: store.WritDone},    // awaiting merge
				{WritID: "sol-ccc", World: "haven", WritStatus: store.WritWorking}, // in progress
				{WritID: "sol-ddd", World: "haven", WritStatus: store.WritOpen, Ready: true},
			},
		},
	}

	GatherCaravans(result, cs, failingWorldOpener)

	if len(result.Caravans) != 1 {
		t.Fatalf("len(Caravans) = %d, want 1", len(result.Caravans))
	}
	c := result.Caravans[0]
	if c.ClosedItems != 1 {
		t.Errorf("ClosedItems = %d, want 1", c.ClosedItems)
	}
	if c.DoneItems != 1 {
		t.Errorf("DoneItems = %d, want 1", c.DoneItems)
	}
	if c.DispatchedItems != 1 {
		t.Errorf("DispatchedItems = %d, want 1", c.DispatchedItems)
	}
	if c.ReadyItems != 1 {
		t.Errorf("ReadyItems = %d, want 1", c.ReadyItems)
	}
}

// --- Tests for Bug 2: computePhaseProgress done/closed split ---

func TestComputePhaseProgressSplitsDoneClosed(t *testing.T) {
	items := []store.CaravanItem{
		{CaravanID: "car-1", WritID: "sol-aaa", World: "haven", Phase: 0},
		{CaravanID: "car-1", WritID: "sol-bbb", World: "haven", Phase: 0},
		{CaravanID: "car-1", WritID: "sol-ccc", World: "haven", Phase: 1},
		{CaravanID: "car-1", WritID: "sol-ddd", World: "haven", Phase: 1},
	}
	statuses := []store.CaravanItemStatus{
		{WritID: "sol-aaa", World: "haven", WritStatus: store.WritClosed},
		{WritID: "sol-bbb", World: "haven", WritStatus: store.WritDone},
		{WritID: "sol-ccc", World: "haven", WritStatus: store.WritClosed},
		{WritID: "sol-ddd", World: "haven", WritStatus: store.WritTethered},
	}

	phases := computePhaseProgress(items, statuses)
	if len(phases) != 2 {
		t.Fatalf("len(phases) = %d, want 2", len(phases))
	}

	// Phase 0: 1 closed, 1 done.
	p0 := phases[0]
	if p0.Closed != 1 {
		t.Errorf("phase 0 Closed = %d, want 1", p0.Closed)
	}
	if p0.Done != 1 {
		t.Errorf("phase 0 Done = %d, want 1", p0.Done)
	}

	// Phase 1: 1 closed, 1 dispatched.
	p1 := phases[1]
	if p1.Closed != 1 {
		t.Errorf("phase 1 Closed = %d, want 1", p1.Closed)
	}
	if p1.Dispatched != 1 {
		t.Errorf("phase 1 Dispatched = %d, want 1", p1.Dispatched)
	}
}

// --- Tests for Bug 3: GatherSphere caravan done/closed split + IsDispatched ---

func TestGatherSphereCaravanSplitsDoneClosed(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	lister := &mockWorldLister{}
	sphere := &mockSphereStore{}
	checker := &mockChecker{alive: map[string]bool{}}

	cs := &mockCaravanStore{
		caravans: []store.Caravan{
			{ID: "car-2222", Name: "batch-2", Status: store.CaravanOpen},
		},
		items: map[string][]store.CaravanItem{
			"car-2222": {
				{CaravanID: "car-2222", WritID: "sol-aaa", World: "haven", Phase: 0},
				{CaravanID: "car-2222", WritID: "sol-bbb", World: "haven", Phase: 0},
				{CaravanID: "car-2222", WritID: "sol-ccc", World: "haven", Phase: 0},
				{CaravanID: "car-2222", WritID: "sol-ddd", World: "haven", Phase: 0},
			},
		},
		statuses: map[string][]store.CaravanItemStatus{
			"car-2222": {
				{WritID: "sol-aaa", World: "haven", WritStatus: store.WritClosed},
				{WritID: "sol-bbb", World: "haven", WritStatus: store.WritDone},
				{WritID: "sol-ccc", World: "haven", WritStatus: store.WritTethered}, // dispatched
				{WritID: "sol-ddd", World: "haven", WritStatus: store.WritOpen, Ready: true},
			},
		},
	}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, cs)

	if len(result.Caravans) != 1 {
		t.Fatalf("len(Caravans) = %d, want 1", len(result.Caravans))
	}
	c := result.Caravans[0]
	if c.ClosedItems != 1 {
		t.Errorf("ClosedItems = %d, want 1", c.ClosedItems)
	}
	if c.DoneItems != 1 {
		t.Errorf("DoneItems = %d, want 1", c.DoneItems)
	}
	if c.DispatchedItems != 1 {
		t.Errorf("DispatchedItems = %d, want 1 (IsDispatched case)", c.DispatchedItems)
	}
	if c.ReadyItems != 1 {
		t.Errorf("ReadyItems = %d, want 1", c.ReadyItems)
	}
}

// --- Tests for Bug 4: Stale failed MR exclusion ---

func TestGatherExcludesStaleFailedMRs(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	sphere := &mockSphereStore{agents: nil}
	world := &mockWorldStore{
		items: map[string]*store.Writ{
			"sol-aaa": {ID: "sol-aaa", Status: store.WritClosed}, // re-cast and merged
			"sol-bbb": {ID: "sol-bbb", Status: store.WritOpen},   // still open (genuine failure)
		},
	}
	checker := &mockChecker{alive: nil}

	mqStore := &mockMergeQueueStore{
		mrs: []store.MergeRequest{
			{ID: "mr-1", WritID: "sol-aaa", Phase: store.MRFailed}, // stale — writ closed
			{ID: "mr-2", WritID: "sol-bbb", Phase: store.MRFailed}, // genuine failure
			{ID: "mr-3", WritID: "sol-ccc", Phase: store.MRReady},
		},
	}

	result, err := Gather("haven", sphere, world, mqStore, checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.MergeQueue.Failed != 1 {
		t.Errorf("MergeQueue.Failed = %d, want 1 (stale failure excluded)", result.MergeQueue.Failed)
	}
	if result.MergeQueue.Ready != 1 {
		t.Errorf("MergeQueue.Ready = %d, want 1", result.MergeQueue.Ready)
	}
	if result.MergeQueue.Total != 3 {
		t.Errorf("MergeQueue.Total = %d, want 3", result.MergeQueue.Total)
	}
}

func TestGatherKeepsFailedMRsWithOpenWrits(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	sphere := &mockSphereStore{agents: nil}
	world := &mockWorldStore{
		items: map[string]*store.Writ{
			"sol-aaa": {ID: "sol-aaa", Status: store.WritOpen},
		},
	}
	checker := &mockChecker{alive: nil}

	mqStore := &mockMergeQueueStore{
		mrs: []store.MergeRequest{
			{ID: "mr-1", WritID: "sol-aaa", Phase: store.MRFailed},
		},
	}

	result, err := Gather("haven", sphere, world, mqStore, checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.MergeQueue.Failed != 1 {
		t.Errorf("MergeQueue.Failed = %d, want 1 (open writ still counts)", result.MergeQueue.Failed)
	}
}
