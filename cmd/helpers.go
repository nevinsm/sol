package cmd

import (
	"fmt"
	"strings"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// parseVarFlags parses key=value flag entries. Returns an error if any
// entry does not contain "=".
func parseVarFlags(vars []string) (map[string]string, error) {
	m := make(map[string]string, len(vars))
	for _, v := range vars {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid --var %q: must be key=value", v)
		}
		m[parts[0]] = parts[1]
	}
	return m, nil
}

// gatedWorldOpener opens a world store after verifying the world exists.
func gatedWorldOpener(world string) (*store.Store, error) {
	if err := config.RequireWorld(world); err != nil {
		return nil, err
	}
	return store.OpenWorld(world)
}
