package namepool

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
)

//go:embed names.txt
var defaultNames string

// Pool manages a set of agent names.
type Pool struct {
	names []string
}

// Load returns a Pool. If overridePath is non-empty and the file exists,
// it reads names from that file instead of the embedded default. If the
// override file does not exist, it falls back to the embedded default
// (no error). Lines starting with "#" and blank lines are skipped.
func Load(overridePath string) (*Pool, error) {
	source := defaultNames

	if overridePath != "" {
		data, err := os.ReadFile(overridePath)
		if err == nil {
			source = string(data)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read name pool override %q: %w", overridePath, err)
		}
		// If file doesn't exist, fall back to embedded default (no error).
	}

	names := parseNames(source)
	return &Pool{names: names}, nil
}

// Names returns a copy of the available name list.
func (p *Pool) Names() []string {
	out := make([]string, len(p.names))
	copy(out, p.names)
	return out
}

// AllocateName returns the first name in the pool that is not already
// used by an agent in the given rig. usedNames is the set of names
// already taken (typically from store.ListAgents). Returns an error if
// all names are exhausted.
func (p *Pool) AllocateName(usedNames []string) (string, error) {
	used := make(map[string]bool, len(usedNames))
	for _, n := range usedNames {
		used[n] = true
	}
	for _, name := range p.names {
		if !used[name] {
			return name, nil
		}
	}
	return "", fmt.Errorf("name pool exhausted: all %d names are in use", len(p.names))
}

// parseNames splits text into names, skipping blank lines and comments.
func parseNames(text string) []string {
	var names []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		names = append(names, line)
	}
	return names
}
