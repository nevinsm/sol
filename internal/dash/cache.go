package dash

import (
	"sync"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// defaultStoreTTL is how long a cached world store stays open after its last use.
const defaultStoreTTL = 60 * time.Second

// worldStoreCache keeps world stores open across dashboard refresh cycles.
// Entries are evicted when they haven't been used within the TTL.
type worldStoreCache struct {
	mu      sync.Mutex
	opener  func(string) (*store.WorldStore, error)
	entries map[string]*cacheEntry
	ttl     time.Duration
}

type cacheEntry struct {
	store    *store.WorldStore
	lastUsed time.Time
}

// newWorldStoreCache creates a new cache wrapping the given WorldOpener.
func newWorldStoreCache(opener func(string) (*store.WorldStore, error)) *worldStoreCache {
	return &worldStoreCache{
		opener:  opener,
		entries: make(map[string]*cacheEntry),
		ttl:     defaultStoreTTL,
	}
}

// Get returns a cached world store or opens a new one.
// The caller must NOT close the returned store — the cache owns its lifecycle.
func (c *worldStoreCache) Get(world string) (*store.WorldStore, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.entries[world]; ok {
		entry.lastUsed = time.Now()
		return entry.store, nil
	}

	ws, err := c.opener(world)
	if err != nil {
		return nil, err
	}

	c.entries[world] = &cacheEntry{
		store:    ws,
		lastUsed: time.Now(),
	}
	return ws, nil
}

// Opener returns a WorldOpener function that uses the cache.
// This can be passed to functions that expect func(string) (*store.WorldStore, error).
// Important: callers receiving stores via this opener must NOT close them.
func (c *worldStoreCache) Opener() func(string) (*store.WorldStore, error) {
	return c.Get
}

// Prune closes and removes entries that haven't been used within the TTL.
func (c *worldStoreCache) Prune() {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().Add(-c.ttl)
	for world, entry := range c.entries {
		if entry.lastUsed.Before(cutoff) {
			entry.store.Close()
			delete(c.entries, world)
		}
	}
}

// CloseAll closes all cached stores and clears the cache.
func (c *worldStoreCache) CloseAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for world, entry := range c.entries {
		entry.store.Close()
		delete(c.entries, world)
	}
}
