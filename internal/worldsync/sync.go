package worldsync

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/setup"
	"github.com/nevinsm/sol/internal/store"
)

// NotifyManager provides session notification primitives.
type NotifyManager interface {
	Exists(name string) bool
	Inject(name, text string, submit bool) error
}

// SyncResult records the outcome of syncing a single component.
type SyncResult struct {
	Component string
	Err       error
}

// SyncOutcome records whether the managed repo advanced during sync.
type SyncOutcome struct {
	Advanced bool   // true if HEAD moved forward
	OldHead  string // short SHA before sync
	NewHead  string // short SHA after sync
}

// SyncRepo fetches origin and hard-resets the managed repo to origin/{branch}.
// The managed repo is a read-only research copy — there is never local state
// worth preserving, so reset --hard is safe and handles dirty trees or diverged
// branches that would cause pull --ff-only to fail.
func SyncRepo(world string) (*SyncOutcome, error) {
	repoPath := config.RepoPath(world)

	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("managed repo does not exist for world %q", world)
	}

	// Capture HEAD before sync.
	oldHeadCmd := exec.Command("git", "-C", repoPath, "rev-parse", "--short", "HEAD")
	oldHeadOut, err := oldHeadCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD for world %q: %s: %w",
			world, strings.TrimSpace(string(oldHeadOut)), err)
	}
	oldHead := strings.TrimSpace(string(oldHeadOut))

	fetchCmd := exec.Command("git", "-C", repoPath, "fetch", "origin")
	if out, err := fetchCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to fetch for world %q: %s: %w",
			world, strings.TrimSpace(string(out)), err)
	}

	// Install excludes BEFORE reset so sol-managed files are excluded
	// when git evaluates the working tree.
	if err := setup.InstallExcludes(repoPath); err != nil {
		return nil, fmt.Errorf("failed to install git excludes for world %q: %w", world, err)
	}

	// Determine the current branch.
	branchCmd := exec.Command("git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	branchOut, err := branchCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to determine branch for world %q: %s: %w",
			world, strings.TrimSpace(string(branchOut)), err)
	}
	branch := strings.TrimSpace(string(branchOut))

	// Hard reset to origin/{branch} — safe because managed repo is read-only.
	resetCmd := exec.Command("git", "-C", repoPath, "reset", "--hard", "origin/"+branch)
	if out, err := resetCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to reset for world %q: %s: %w",
			world, strings.TrimSpace(string(out)), err)
	}

	// Remove untracked files that might cause issues.
	cleanCmd := exec.Command("git", "-C", repoPath, "clean", "-fd")
	if out, err := cleanCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to clean for world %q: %s: %w",
			world, strings.TrimSpace(string(out)), err)
	}

	// Capture HEAD after sync.
	newHeadCmd := exec.Command("git", "-C", repoPath, "rev-parse", "--short", "HEAD")
	newHeadOut, err := newHeadCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD after sync for world %q: %s: %w",
			world, strings.TrimSpace(string(newHeadOut)), err)
	}
	newHead := strings.TrimSpace(string(newHeadOut))

	return &SyncOutcome{
		Advanced: oldHead != newHead,
		OldHead:  oldHead,
		NewHead:  newHead,
	}, nil
}

// SyncForge syncs the forge worktree by fetching origin and resetting to the target branch.
// The forge worktree operates in detached HEAD mode — no branch checkout needed.
// Returns nil if the forge worktree doesn't exist (nothing to sync).
func SyncForge(world, targetBranch string) error {
	wtPath := forge.WorktreePath(world)
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		return nil
	}

	// Fetch origin in forge worktree.
	fetchCmd := exec.Command("git", "-C", wtPath, "fetch", "origin")
	if out, err := fetchCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to fetch in forge worktree for world %q: %s: %w",
			world, strings.TrimSpace(string(out)), err)
	}

	// Abort any in-progress rebase (best-effort).
	abortCmd := exec.Command("git", "-C", wtPath, "rebase", "--abort")
	_ = abortCmd.Run()

	// Reset to origin's target branch (works in detached HEAD).
	resetCmd := exec.Command("git", "-C", wtPath, "reset", "--hard", "origin/"+targetBranch)
	if out, err := resetCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to reset forge worktree to origin/%s: %s: %w",
			targetBranch, strings.TrimSpace(string(out)), err)
	}

	// Remove untracked files. -fd (without -x) respects .git/info/exclude,
	// so sol-managed files like CLAUDE.local.md are preserved as ignored.
	cleanCmd := exec.Command("git", "-C", wtPath, "clean", "-fd")
	if out, err := cleanCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to clean forge worktree for world %q: %s: %w",
			world, strings.TrimSpace(string(out)), err)
	}

	return nil
}

// SyncEnvoy notifies a running envoy session that the managed repo has been synced.
// If the repo did not advance or the session is not running, this is a no-op.
func SyncEnvoy(world, name string, mgr NotifyManager, outcome *SyncOutcome) error {
	if outcome == nil || !outcome.Advanced {
		return nil
	}

	sessName := config.SessionName(world, name)
	if !mgr.Exists(sessName) {
		return nil
	}

	msg := fmt.Sprintf("\n[sol] Managed repo synced (world: %s, %s..%s). Review your branch for any upstream changes.\n",
		world, outcome.OldHead, outcome.NewHead)
	if err := mgr.Inject(sessName, msg, true); err != nil {
		return fmt.Errorf("failed to notify envoy %q: %w", name, err)
	}

	return nil
}

// SyncAllComponents syncs the forge and notifies all envoys.
// Called after the managed repo is already synced. Returns results for each component.
func SyncAllComponents(world, targetBranch string, lister store.AgentReader, mgr NotifyManager, outcome *SyncOutcome) []SyncResult {
	var results []SyncResult

	// Sync forge if worktree exists.
	forgeWT := forge.WorktreePath(world)
	if _, err := os.Stat(forgeWT); err == nil {
		err := SyncForge(world, targetBranch)
		results = append(results, SyncResult{Component: "forge", Err: err})
	}

	// Notify envoys.
	agents, err := lister.ListAgents(world, "")
	if err == nil {
		for _, a := range agents {
			if a.Role != "envoy" {
				continue
			}
			err := SyncEnvoy(world, a.Name, mgr, outcome)
			results = append(results, SyncResult{Component: "envoy:" + a.Name, Err: err})
		}
	} else {
		results = append(results, SyncResult{Component: "envoys", Err: fmt.Errorf("failed to list agents: %w", err)})
	}

	return results
}
