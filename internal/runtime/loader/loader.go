// Package loader provides runtime registry helpers for the runtime package.
// It imports each concrete runtime implementation to construct them on demand,
// breaking the import cycle that would result from putting Get/All in
// internal/runtime itself (since claude and codex both import internal/runtime).
//
// Callers should import this package to resolve runtimes by name:
//
//	import "github.com/nevinsm/sol/internal/runtime/loader"
//	rt, err := loader.Get("claude")
package loader

import (
	"fmt"

	"github.com/nevinsm/sol/internal/runtime"
	clauderuntime "github.com/nevinsm/sol/internal/runtime/claude"
	codexruntime "github.com/nevinsm/sol/internal/runtime/codex"
)

// Get returns a new Runtime instance for the given name.
// Returns an error for unknown runtime names.
func Get(name string) (runtime.Runtime, error) {
	switch name {
	case "claude":
		return clauderuntime.New(), nil
	case "codex":
		return codexruntime.New(), nil
	default:
		return nil, fmt.Errorf("unknown runtime %q", name)
	}
}

// All returns a fresh map of all registered runtimes keyed by name.
// Each call constructs new runtime instances; runtimes are stateless so
// this is safe for concurrent use.
func All() map[string]runtime.Runtime {
	return map[string]runtime.Runtime{
		"claude": clauderuntime.New(),
		"codex":  codexruntime.New(),
	}
}
