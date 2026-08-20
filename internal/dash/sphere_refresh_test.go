package dash

import (
	"fmt"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

// mockRefreshSphereStore is a minimal fake satisfying dash's sphereStore
// interface (store.AgentReader + store.WorldReader + store.CaravanReader +
// CountPending), enough to drive a real GatherSphere call from
// Model.refresh() in the sphere view.
type mockRefreshSphereStore struct {
	worlds []store.World
}

func (m *mockRefreshSphereStore) GetAgent(id string) (*store.Agent, error) {
	return nil, fmt.Errorf("mockRefreshSphereStore.GetAgent not implemented")
}

func (m *mockRefreshSphereStore) ListAgents(world string, state store.AgentState) ([]store.Agent, error) {
	return nil, nil
}

func (m *mockRefreshSphereStore) FindIdleAgent(world string) (*store.Agent, error) {
	return nil, fmt.Errorf("mockRefreshSphereStore.FindIdleAgent not implemented")
}

func (m *mockRefreshSphereStore) ListWorlds() ([]store.World, error) {
	return m.worlds, nil
}

func (m *mockRefreshSphereStore) GetCaravan(id string) (*store.Caravan, error) {
	return nil, fmt.Errorf("mockRefreshSphereStore.GetCaravan not implemented")
}

func (m *mockRefreshSphereStore) ListCaravans(status store.CaravanStatus) ([]store.Caravan, error) {
	return nil, nil
}

func (m *mockRefreshSphereStore) ListCaravanItems(caravanID string) ([]store.CaravanItem, error) {
	return nil, nil
}

func (m *mockRefreshSphereStore) GetCaravanItemsForWrit(writID string) ([]store.CaravanItem, error) {
	return nil, nil
}

func (m *mockRefreshSphereStore) CheckCaravanReadiness(caravanID string, worldOpener func(world string) (*store.WorldStore, error)) ([]store.CaravanItemStatus, error) {
	return nil, nil
}

func (m *mockRefreshSphereStore) CountPending(recipient string) (int, error) {
	return 0, nil
}

// mockRefreshChecker is a minimal status.SessionChecker fake.
type mockRefreshChecker struct{}

func (mockRefreshChecker) Exists(name string) bool { return false }

// TestSphereViewRefreshReusesCacheAcrossTicks verifies the fix's headline
// behavior: the sphere-view refresh path (Model.refresh(), viewSphere case)
// passes the dash store cache — not the raw opener — into GatherSphere, so
// consecutive refresh ticks reuse the same cached world stores instead of
// re-opening each world's database every 3 seconds.
func TestSphereViewRefreshReusesCacheAcrossTicks(t *testing.T) {
	opener := newMockOpener(t)

	m := NewModel(Config{
		SphereStore: &mockRefreshSphereStore{
			worlds: []store.World{{Name: "alpha"}, {Name: "beta"}},
		},
		SessionCheck: mockRefreshChecker{},
		WorldOpener:  opener.open,
	})

	// First refresh tick: each world is opened once (via the cache).
	msg1, ok := m.refresh()().(dataMsg)
	if !ok {
		t.Fatalf("refresh() #1 did not return a dataMsg")
	}
	if msg1.sphere == nil || len(msg1.sphere.Worlds) != 2 {
		t.Fatalf("refresh() #1: sphere = %+v, want 2 worlds", msg1.sphere)
	}
	if opener.calls != 2 {
		t.Fatalf("opener.calls after tick #1 = %d, want 2 (alpha + beta opened once each)", opener.calls)
	}

	// Second refresh tick: the cache (60s TTL, well within test runtime)
	// still holds both worlds — no additional opens.
	msg2, ok := m.refresh()().(dataMsg)
	if !ok {
		t.Fatalf("refresh() #2 did not return a dataMsg")
	}
	if msg2.sphere == nil || len(msg2.sphere.Worlds) != 2 {
		t.Fatalf("refresh() #2: sphere = %+v, want 2 worlds", msg2.sphere)
	}
	if opener.calls != 2 {
		t.Fatalf("opener.calls after tick #2 = %d, want 2 (ticks must reuse the cache, not reopen)", opener.calls)
	}

	m.storeCache.CloseAll()
}
