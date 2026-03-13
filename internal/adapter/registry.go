package adapter

var adapters = map[string]RuntimeAdapter{}

// Register adds an adapter to the registry under the given name.
// Adapters call this from their init() function.
func Register(name string, a RuntimeAdapter) { adapters[name] = a }

// Get retrieves an adapter by name. Returns false if not found.
func Get(name string) (RuntimeAdapter, bool) { a, ok := adapters[name]; return a, ok }

// Default returns the default adapter ("claude").
// Panics if the claude adapter has not been registered.
func Default() RuntimeAdapter { return adapters["claude"] }
