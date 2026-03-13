package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/nevinsm/sol/internal/fileutil"
)

// TrustDirectory marks a directory as trusted in Claude Code's global state
// (~/.claude.json). This prevents the interactive trust prompt that would
// otherwise block automated sessions started in new worktree directories.
//
// Uses flock-based locking and atomic writes to prevent corruption when
// multiple sessions call TrustDirectory concurrently.
func TrustDirectory(dir string) error {
	claudeJSON := filepath.Join(os.Getenv("HOME"), ".claude.json")
	return trustDirectoryInFile(dir, claudeJSON)
}

// TrustDirectoryIn marks a directory as trusted in the specified config dir's
// .claude.json. Used when CLAUDE_CONFIG_DIR is set so Claude Code reads trust
// from the agent-specific config dir rather than ~/.claude.json.
//
// Uses the same flock-based locking and atomic writes as TrustDirectory.
func TrustDirectoryIn(dir, configDir string) error {
	claudeJSON := filepath.Join(configDir, ".claude.json")
	return trustDirectoryInFile(dir, claudeJSON)
}

// trustDirectoryInFile is the shared implementation for TrustDirectory and
// TrustDirectoryIn. It marks a directory as trusted in the specified
// .claude.json file using flock-based locking and atomic writes.
func trustDirectoryInFile(dir, claudeJSON string) error {
	// Resolve absolute path outside the lock to reduce lock hold time.
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path for %q: %w", dir, err)
	}

	// Ensure parent directory exists (needed for agent config dirs).
	if err := os.MkdirAll(filepath.Dir(claudeJSON), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", claudeJSON, err)
	}

	return withClaudeJSONLock(claudeJSON, func() error {
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
				"allowedTools":                  []any{},
				"hasTrustDialogAccepted":        true,
				"hasCompletedProjectOnboarding": true,
			}
		}

		// Atomic write back.
		out, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal %s: %w", claudeJSON, err)
		}
		return fileutil.AtomicWrite(claudeJSON, out, 0o600)
	})
}

// withClaudeJSONLock acquires a blocking exclusive flock on claudeJSON+".lock"
// and calls fn while holding the lock. The lock file is not removed after
// release (standard flock practice, avoids TOCTOU races).
func withClaudeJSONLock(claudeJSON string, fn func() error) error {
	lockPath := claudeJSON + ".lock"

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open lock file %s: %w", lockPath, err)
	}
	defer f.Close()

	// Blocking exclusive lock — sessions wait rather than fail.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("failed to acquire lock on %s: %w", lockPath, err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	return fn()
}

