package dash

import (
	"fmt"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// mockOpener tracks open calls and returns real (but temporary) WorldStores.
type mockOpener struct {
	calls   int
	failFor map[string]bool
	tmpDir  string
}

func newMockOpener(t *testing.T) *mockOpener {
	t.Helper()
	tmpDir := t.TempDir()
	// Set SOL_HOME so store.OpenWorld can find the store directory.
	t.Setenv("SOL_HOME", tmpDir)
	return &mockOpener{failFor: make(map[string]bool), tmpDir: tmpDir}
}

func (m *mockOpener) open(world string) (*store.WorldStore, error) {
	m.calls++
	if m.failFor[world] {
		return nil, fmt.Errorf("mock error opening %s", world)
	}
	return store.OpenWorld(world)
}

func TestWorldStoreCacheReusesStores(t *testing.T) {
	opener := newMockOpener(t)
	cache := newWorldStoreCache(opener.open)
	defer cache.CloseAll()

	// First call opens a new store.
	ws1, err := cache.Get("alpha")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opener.calls != 1 {
		t.Fatalf("expected 1 open call, got %d", opener.calls)
	}

	// Second call returns the cached store.
	ws2, err := cache.Get("alpha")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opener.calls != 1 {
		t.Fatalf("expected 1 open call (cached), got %d", opener.calls)
	}
	if ws1 != ws2 {
		t.Fatal("expected same store pointer from cache")
	}

	// Different world opens a new store.
	_, err = cache.Get("beta")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opener.calls != 2 {
		t.Fatalf("expected 2 open calls, got %d", opener.calls)
	}
}

func TestWorldStoreCacheOpenerError(t *testing.T) {
	opener := newMockOpener(t)
	opener.failFor["broken"] = true
	cache := newWorldStoreCache(opener.open)
	defer cache.CloseAll()

	_, err := cache.Get("broken")
	if err == nil {
		t.Fatal("expected error for broken world")
	}

	// Failed opens are not cached — retry should call opener again.
	_, err = cache.Get("broken")
	if err == nil {
		t.Fatal("expected error on retry")
	}
	if opener.calls != 2 {
		t.Fatalf("expected 2 open calls for failed world, got %d", opener.calls)
	}
}

func TestWorldStoreCachePrune(t *testing.T) {
	opener := newMockOpener(t)
	cache := newWorldStoreCache(opener.open)
	cache.ttl = 50 * time.Millisecond

	_, err := cache.Get("alpha")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for TTL to expire.
	time.Sleep(60 * time.Millisecond)

	cache.Prune()

	// Entry should be evicted — next Get opens a new store.
	_, err = cache.Get("alpha")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opener.calls != 2 {
		t.Fatalf("expected 2 open calls after prune, got %d", opener.calls)
	}
	cache.CloseAll()
}

func TestWorldStoreCachePruneKeepsFresh(t *testing.T) {
	opener := newMockOpener(t)
	cache := newWorldStoreCache(opener.open)
	cache.ttl = 200 * time.Millisecond
	defer cache.CloseAll()

	_, err := cache.Get("alpha")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Prune immediately — entry should survive (still fresh).
	cache.Prune()

	_, err = cache.Get("alpha")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opener.calls != 1 {
		t.Fatalf("expected 1 open call (fresh entry survived prune), got %d", opener.calls)
	}
}

func TestWorldStoreCacheCloseAll(t *testing.T) {
	opener := newMockOpener(t)
	cache := newWorldStoreCache(opener.open)

	_, _ = cache.Get("alpha")
	_, _ = cache.Get("beta")

	cache.CloseAll()

	// After CloseAll, entries are cleared — next Get opens new stores.
	_, _ = cache.Get("alpha")
	if opener.calls != 3 {
		t.Fatalf("expected 3 open calls after CloseAll, got %d", opener.calls)
	}
	cache.CloseAll()
}

func TestWorldStoreCacheOpenerFunc(t *testing.T) {
	opener := newMockOpener(t)
	cache := newWorldStoreCache(opener.open)
	defer cache.CloseAll()

	openerFn := cache.Opener()

	// Opener function should use the cache.
	_, _ = openerFn("alpha")
	_, _ = openerFn("alpha")
	if opener.calls != 1 {
		t.Fatalf("expected 1 open call via Opener(), got %d", opener.calls)
	}
}
