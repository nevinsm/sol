package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// TrustDirectory marks a directory as trusted in Claude Code's global state
// (~/.claude.json). This prevents the interactive trust prompt that would
// otherwise block automated sessions started in new worktree directories.
func TrustDirectory(dir string) error {
	claudeJSON := filepath.Join(os.Getenv("HOME"), ".claude.json")

	// Read existing state.
	var state map[string]any
	data, err := os.ReadFile(claudeJSON)
	if err != nil {
		if os.IsNotExist(err) {
			state = make(map[string]any)
		} else {
			return fmt.Errorf("failed to read %s: %w", claudeJSON, err)
		}
	} else {
		if err := json.Unmarshal(data, &state); err != nil {
			return fmt.Errorf("failed to parse %s: %w", claudeJSON, err)
		}
	}

	// Get or create the projects map.
	projectsRaw, ok := state["projects"]
	if !ok {
		projectsRaw = make(map[string]any)
		state["projects"] = projectsRaw
	}
	projects, ok := projectsRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("unexpected type for projects in %s", claudeJSON)
	}

	// Resolve to absolute path.
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path for %q: %w", dir, err)
	}

	// Get or create the project entry.
	entryRaw, ok := projects[absDir]
	if ok {
		entry, ok := entryRaw.(map[string]any)
		if ok {
			if trusted, _ := entry["hasTrustDialogAccepted"].(bool); trusted {
				return nil // Already trusted.
			}
			entry["hasTrustDialogAccepted"] = true
		}
	} else {
		projects[absDir] = map[string]any{
			"allowedTools":            []any{},
			"hasTrustDialogAccepted":  true,
			"hasCompletedProjectOnboarding": true,
		}
	}

	// Write back.
	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal %s: %w", claudeJSON, err)
	}
	if err := os.WriteFile(claudeJSON, out, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", claudeJSON, err)
	}

	return nil
}
