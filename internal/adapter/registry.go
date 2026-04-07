package adapter

import "maps"

var adapters = map[string]RuntimeAdapter{}

// Register adds an adapter to the registry under the given name.
// Adapters call this from their init() function.
func Register(name string, a RuntimeAdapter) { adapters[name] = a }

// Get retrieves an adapter by name. Returns false if not found.
func Get(name string) (RuntimeAdapter, bool) { a, ok := adapters[name]; return a, ok }

// All returns a copy of the registered adapters keyed by name.
// Used by cleanup paths that must invoke runtime-specific teardown for an
// agent whose runtime is no longer recorded (e.g. orphan sweeps).
func All() map[string]RuntimeAdapter {
	out := make(map[string]RuntimeAdapter, len(adapters))
	maps.Copy(out, adapters)
	return out
}
