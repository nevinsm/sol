package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nevinsm/sol/internal/fileutil"
	"github.com/nevinsm/sol/internal/softfail"
)

// TrustDirectory marks a directory as trusted in Claude Code's global state
// (~/.claude.json). This prevents the interactive trust prompt that would
// otherwise block automated sessions started in new worktree directories.
//
// Uses flock-based locking and atomic writes to prevent corruption when
// multiple sessions call TrustDirectory concurrently.
func TrustDirectory(dir string) error {
	claudeJSON := filepath.Join(os.Getenv("HOME"), ".claude.json")
	return trustDirectoryInFile([]string{dir}, claudeJSON)
}

// TrustDirectoryIn marks dir as trusted in the specified config dir's
// .claude.json. Used when CLAUDE_CONFIG_DIR is set so Claude Code reads trust
// from the agent-specific config dir rather than ~/.claude.json.
//
// Starting with Claude Code v2.1.211, workspace trust for a git worktree is
// keyed on the MAIN CHECKOUT's root, not the worktree's own path (see
// https://code.claude.com/docs/en/permissions.md, "Where Claude Code keys
// the trust": "In a repository, Claude Code keys the trust on the git
// repository root ... In a worktree, it uses the main checkout's root.").
// When dir is a linked git worktree, TrustDirectoryIn therefore also trusts
// the main checkout's root, so Claude Code finds a trusted entry regardless
// of which path it looks up. If dir is not a worktree, or not a git
// repository at all, only dir is trusted (git resolution failures are
// soft-failed, never propagated — pre-trust must not block Seed).
//
// Uses the same flock-based locking and atomic writes as TrustDirectory.
func TrustDirectoryIn(dir, configDir string) error {
	claudeJSON := filepath.Join(configDir, ".claude.json")
	return trustDirectoryInFile(trustTargets(dir), claudeJSON)
}

// trustTargets resolves the set of directories that should be trusted for
// dir: dir itself and, when dir is a linked git worktree, the main
// checkout's root (see TrustDirectoryIn's doc comment for why).
//
// The worktree relationship is resolved via git, not by assuming sol's own
// layout: "git -C <dir> rev-parse --git-common-dir" returns the main
// checkout's .git directory for a linked worktree, and dir's own .git
// directory otherwise. Both dir and the derived root are resolved with
// filepath.EvalSymlinks so the keys written match what Claude Code itself
// computes.
//
// Any failure (dir is not a git repository, git is unavailable, a path
// fails to resolve) falls back to trusting dir alone, matching the
// pre-existing behavior. Failures are logged via softfail rather than
// returned — this is best-effort discovery, not a hard requirement.
func trustTargets(dir string) []string {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		softfail.Log(nil, "trust.resolve_targets", fmt.Errorf("resolve absolute path for %q: %w", dir, err))
		return []string{dir}
	}

	out, err := exec.Command("git", "-C", absDir, "rev-parse", "--git-common-dir").Output()
	if err != nil {
		// Most commonly: dir is not inside a git repository at all. Not
		// worth logging as a soft failure — this is an expected shape for
		// non-repo dirs, not an anomaly.
		return []string{absDir}
	}

	commonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(absDir, commonDir)
	}
	commonDir, err = filepath.EvalSymlinks(commonDir)
	if err != nil {
		softfail.Log(nil, "trust.resolve_targets", fmt.Errorf("resolve git common dir for %q: %w", absDir, err))
		return []string{absDir}
	}
	root := filepath.Dir(commonDir)

	resolvedDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		softfail.Log(nil, "trust.resolve_targets", fmt.Errorf("resolve %q: %w", absDir, err))
		return []string{absDir}
	}

	if root == resolvedDir {
		// dir is itself the main checkout (git-common-dir's parent is dir
		// itself) — not a linked worktree. Trust only dir, unchanged.
		return []string{resolvedDir}
	}
	return []string{resolvedDir, root}
}

// trustDirectoryInFile is the shared implementation for TrustDirectory and
// TrustDirectoryIn. It marks each directory in dirs as trusted in the
// specified .claude.json file using flock-based locking and a single atomic
// write for the whole batch.
func trustDirectoryInFile(dirs []string, claudeJSON string) error {
	// Resolve absolute paths outside the lock to reduce lock hold time.
	absDirs := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("failed to resolve absolute path for %q: %w", dir, err)
		}
		absDirs = append(absDirs, absDir)
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

		changed := false
		for _, absDir := range absDirs {
			if trustProjectEntry(projects, absDir) {
				changed = true
			}
		}
		if !changed {
			return nil // All entries already trusted; nothing to write.
		}

		// Atomic write back.
		out, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal %s: %w", claudeJSON, err)
		}
		return fileutil.AtomicWrite(claudeJSON, out, 0o600)
	})
}

// trustProjectEntry ensures projects[absDir] is a well-formed, trusted
// project entry. It reports the anomaly via softfail and overwrites the
// entry when it exists but has an unexpected shape (e.g., a hand-edit
// replaced the object with a string) rather than silently leaving the
// directory untrusted — otherwise the next session start would block on the
// trust prompt.
//
// Returns true if it changed projects (added a new entry or flipped an
// existing entry to trusted).
func trustProjectEntry(projects map[string]any, absDir string) bool {
	entryRaw, ok := projects[absDir]
	if !ok {
		projects[absDir] = map[string]any{
			"allowedTools":                  []any{},
			"hasTrustDialogAccepted":        true,
			"hasCompletedProjectOnboarding": true,
		}
		return true
	}

	entry, ok := entryRaw.(map[string]any)
	if !ok {
		softfail.Log(nil, "trust.Update", fmt.Errorf("projects[%q] has type %T, expected map[string]any", absDir, entryRaw))
		projects[absDir] = map[string]any{
			"allowedTools":                  []any{},
			"hasTrustDialogAccepted":        true,
			"hasCompletedProjectOnboarding": true,
		}
		return true
	}

	if trusted, _ := entry["hasTrustDialogAccepted"].(bool); trusted {
		return false // Already trusted.
	}
	entry["hasTrustDialogAccepted"] = true
	return true
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
