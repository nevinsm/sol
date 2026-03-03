package worldsync

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/governor"
	"github.com/nevinsm/sol/internal/setup"
	"github.com/nevinsm/sol/internal/store"
)

// NotifyManager provides session notification primitives.
type NotifyManager interface {
	Exists(name string) bool
	Inject(name, text string, submit bool) error
}

// AgentLister lists agents from the sphere store.
type AgentLister interface {
	ListAgents(world string, state string) ([]store.Agent, error)
}

// SyncResult records the outcome of syncing a single component.
type SyncResult struct {
	Component string
	Err       error
}

// SyncRepo fetches and fast-forward pulls the managed repo at $SOL_HOME/{world}/repo/.
func SyncRepo(world string) error {
	repoPath := config.RepoPath(world)

	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		return fmt.Errorf("managed repo does not exist for world %q", world)
	}

	fetchCmd := exec.Command("git", "-C", repoPath, "fetch", "origin")
	if out, err := fetchCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to fetch for world %q: %s: %w",
			world, strings.TrimSpace(string(out)), err)
	}

	pullCmd := exec.Command("git", "-C", repoPath, "pull", "--ff-only")
	if out, err := pullCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to pull for world %q: %s: %w",
			world, strings.TrimSpace(string(out)), err)
	}

	// Ensure sol-specific excludes are in place (idempotent).
	if err := setup.InstallExcludes(repoPath); err != nil {
		return fmt.Errorf("failed to install git excludes for world %q: %w", world, err)
	}

	return nil
}

// SyncForge syncs the forge worktree by fetching origin and resetting to the target branch.
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

	// Checkout the forge branch.
	forgeBranch := forge.ForgeBranch(world)
	checkoutCmd := exec.Command("git", "-C", wtPath, "checkout", forgeBranch)
	if out, err := checkoutCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to checkout %s in forge worktree: %s: %w",
			forgeBranch, strings.TrimSpace(string(out)), err)
	}

	// Reset to origin's target branch.
	resetCmd := exec.Command("git", "-C", wtPath, "reset", "--hard", "origin/"+targetBranch)
	if out, err := resetCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to reset forge worktree to origin/%s: %s: %w",
			targetBranch, strings.TrimSpace(string(out)), err)
	}

	return nil
}

// SyncEnvoy notifies a running envoy session that the managed repo has been synced.
// If the session is not running, this is a no-op.
func SyncEnvoy(world, name string, mgr NotifyManager) error {
	sessName := config.SessionName(world, name)
	if !mgr.Exists(sessName) {
		return nil
	}

	msg := fmt.Sprintf("\n[sol] Managed repo synced. Review your branch for any upstream changes.\n")
	if err := mgr.Inject(sessName, msg, true); err != nil {
		return fmt.Errorf("failed to notify envoy %q: %w", name, err)
	}

	return nil
}

// SyncGovernor notifies a running governor session that the managed repo has been synced.
// If the session is not running, this is a no-op.
func SyncGovernor(world string, mgr NotifyManager) error {
	sessName := config.SessionName(world, "governor")
	if !mgr.Exists(sessName) {
		return nil
	}

	msg := fmt.Sprintf("\n[sol] Managed repo synced. Latest code available in ../repo.\n")
	if err := mgr.Inject(sessName, msg, true); err != nil {
		return fmt.Errorf("failed to notify governor: %w", err)
	}

	return nil
}

// SyncAllComponents syncs the forge and notifies all envoys and the governor.
// Called after the managed repo is already synced. Returns results for each component.
func SyncAllComponents(world, targetBranch string, lister AgentLister, mgr NotifyManager) []SyncResult {
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
			err := SyncEnvoy(world, a.Name, mgr)
			results = append(results, SyncResult{Component: "envoy:" + a.Name, Err: err})
		}
	} else {
		results = append(results, SyncResult{Component: "envoys", Err: fmt.Errorf("failed to list agents: %w", err)})
	}

	// Notify governor if its directory exists.
	govDir := governor.GovernorDir(world)
	if _, err := os.Stat(govDir); err == nil {
		err := SyncGovernor(world, mgr)
		results = append(results, SyncResult{Component: "governor", Err: err})
	}

	return results
}
