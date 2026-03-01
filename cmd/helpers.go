package cmd

import (
	"fmt"
	"strings"
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
