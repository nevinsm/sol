package dispatch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/adapter"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/envoy"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// cleanupOutpostConfigDir invokes the runtime adapter's CleanupConfigDir for
// an outpost agent. Best-effort: logs warnings but never fails resolve.
//
// We resolve the runtime adapter via the world config (the agent record has
// no Runtime field). If the configured runtime is unknown, we fall back to
// invoking every registered adapter — this catches the case where an outpost
// was dispatched under a previous runtime that has since been swapped.
// CleanupConfigDir is idempotent so the fallback is safe.
func cleanupOutpostConfigDir(world, role, agentName string) {
	worldDir := config.WorldDir(world)
	worldCfg, err := config.LoadWorldConfig(world)
	if err == nil {
		runtime := worldCfg.ResolveRuntime(role)
		if a, ok := adapter.Get(runtime); ok {
			if cleanupErr := a.CleanupConfigDir(worldDir, role, agentName); cleanupErr != nil {
				slog.Warn("resolve: failed to clean up adapter config dir",
					"agent", agentName, "runtime", runtime, "error", cleanupErr)
			}
			return
		}
	}
	// Fallback: clean up via every registered adapter (idempotent).
	for name, a := range adapter.All() {
		if cleanupErr := a.CleanupConfigDir(worldDir, role, agentName); cleanupErr != nil {
			slog.Warn("resolve: failed to clean up adapter config dir (fallback)",
				"agent", agentName, "runtime", name, "error", cleanupErr)
		}
	}
}

// cleanupWorktree removes a git worktree and prunes stale references.
// Best-effort: logs what was cleaned up but does not fail.
// Uses its own background context since it may run in a goroutine after
// the parent context has been cancelled.
func cleanupWorktree(world, worktreeDir string) {
	repoPath := config.RepoPath(world)

	rmCtx, rmCancel := context.WithTimeout(context.Background(), GitWorktreeRemoveTimeout)
	defer rmCancel()
	rmCmd := exec.CommandContext(rmCtx, "git", "-C", repoPath, "worktree", "remove", "--force", worktreeDir)
	if out, err := rmCmd.CombinedOutput(); err != nil {
		slog.Warn("resolve: worktree remove failed", "output", strings.TrimSpace(string(out)), "error", err)
		// Fallback: remove directory directly (matches cast cleanup pattern).
		if removeErr := os.RemoveAll(worktreeDir); removeErr != nil {
			slog.Warn("resolve: failed to remove worktree dir", "dir", worktreeDir, "error", removeErr)
			return
		}
	}
	slog.Warn("resolve: cleaned up worktree", "dir", worktreeDir)

	pruneCtx, pruneCancel := context.WithTimeout(context.Background(), GitLocalOpTimeout)
	defer pruneCancel()
	pruneCmd := exec.CommandContext(pruneCtx, "git", "-C", repoPath, "worktree", "prune")
	if out, err := pruneCmd.CombinedOutput(); err != nil {
		slog.Warn("resolve: worktree prune failed", "output", strings.TrimSpace(string(out)), "error", err)
	}
}

// ResolveResult holds the output of a resolve operation.
type ResolveResult struct {
	WritID     string
	Title          string
	AgentName      string
	BranchName     string
	MergeRequestID string
	SessionKept    bool // true if session was not killed (envoy resolve)
}

// ResolveOpts holds the inputs for a resolve operation.
type ResolveOpts struct {
	World     string
	AgentName string
	WritID    string // Optional: specific writ to resolve (persistent agents only; ignored for outpost agents)
}

// ResolveLockPath returns the path to the shared resolve-in-progress lock file (used for outpost agents).
func ResolveLockPath(world, agentName, role string) string {
	return filepath.Join(config.AgentDir(world, agentName, role), ".resolve_in_progress")
}

// ResolveWritLockPath returns the path to the per-writ resolve-in-progress lock file.
// Used for persistent agents to avoid concurrent resolves sharing the same lock file.
func ResolveWritLockPath(world, agentName, role, writID string) string {
	return filepath.Join(config.AgentDir(world, agentName, role), ".resolve_in_progress."+writID)
}

// IsResolveInProgress returns true if any resolve lock file exists for this agent.
// Checks both the shared lock file (outpost agents) and per-writ lock files (persistent agents).
func IsResolveInProgress(world, agentName, role string) bool {
	if _, err := os.Stat(ResolveLockPath(world, agentName, role)); err == nil {
		return true
	}
	agentDir := config.AgentDir(world, agentName, role)
	matches, err := filepath.Glob(filepath.Join(agentDir, ".resolve_in_progress.*"))
	return err == nil && len(matches) > 0
}

// ClearResolveLocksForAgent removes all resolve lock files for an agent (shared and per-writ).
func ClearResolveLocksForAgent(world, agentName, role string) {
	os.Remove(ResolveLockPath(world, agentName, role))
	agentDir := config.AgentDir(world, agentName, role)
	if matches, err := filepath.Glob(filepath.Join(agentDir, ".resolve_in_progress.*")); err == nil {
		for _, f := range matches {
			os.Remove(f)
		}
	}
}

// Resolve signals work completion: git operations, state updates, tether clear.
// The logger parameter is optional — if nil, no events are emitted.
func Resolve(ctx context.Context, opts ResolveOpts, worldStore WorldStore, sphereStore SphereStore, mgr SessionManager, logger *events.Logger) (*ResolveResult, error) {
	agentID := opts.World + "/" + opts.AgentName
	sessName := config.SessionName(opts.World, opts.AgentName)

	// Look up agent first to determine role (needed for role-aware tether path).
	agent, err := sphereStore.GetAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", agentID, err)
	}

	// 1. Determine which writ to resolve.
	var writID string
	if agent.Role == "outpost" {
		// Outpost: read tether (single writ, unchanged behavior).
		writID, err = tether.Read(opts.World, opts.AgentName, agent.Role)
		if err != nil {
			return nil, fmt.Errorf("failed to read tether: %w", err)
		}
		if writID == "" {
			return nil, fmt.Errorf("no work tethered for agent %q in world %q", opts.AgentName, opts.World)
		}
	} else {
		// Persistent agent: use explicit WritID, fall back to active_writ from DB.
		if opts.WritID != "" {
			writID = opts.WritID
			// Validate the writ is actually tethered.
			if !tether.IsTetheredTo(opts.World, opts.AgentName, writID, agent.Role) {
				return nil, fmt.Errorf("writ %q is not tethered to agent %q", writID, opts.AgentName)
			}
		} else {
			writID = agent.ActiveWrit
			if writID == "" {
				return nil, fmt.Errorf("no active writ for agent %q in world %q", opts.AgentName, opts.World)
			}
		}
	}

	// Create resolve lock to prevent handoff from interrupting.
	// Persistent agents use a per-writ lock file so concurrent resolves don't
	// interfere: when Resolve-A finishes and removes its lock, Resolve-B's lock
	// remains visible and the handoff guard still sees a resolve in progress.
	var lockPath string
	if agent.Role == "outpost" {
		lockPath = ResolveLockPath(opts.World, opts.AgentName, agent.Role)
	} else {
		lockPath = ResolveWritLockPath(opts.World, opts.AgentName, agent.Role, writID)
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}
	// Write resolve lock with writ ID (enables crash recovery detection).
	if err := os.WriteFile(lockPath, []byte(writID), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write resolve lock: %w", err)
	}
	defer os.Remove(lockPath)

	// Acquire locks: writ first, then agent (consistent ordering with Cast).
	lock, err := AcquireWritLock(writID)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	agentLock, err := AcquireAgentLock(agentID)
	if err != nil {
		return nil, err
	}
	defer agentLock.Release()

	// Compute worktree path and branch name based on role.
	var worktreeDir string
	var branchName string
	switch agent.Role {
	case "envoy":
		worktreeDir = envoy.WorktreePath(opts.World, opts.AgentName)
		branchName = fmt.Sprintf("envoy/%s/%s/%s", opts.World, opts.AgentName, writID)
	case "forge":
		worktreeDir = filepath.Join(config.Home(), opts.World, "forge", "worktree")
		branchName = "forge/" + opts.World
	default:
		worktreeDir = WorktreePath(opts.World, opts.AgentName)
		branchName = fmt.Sprintf("outpost/%s/%s", opts.AgentName, writID)
	}

	// Get the writ for output and conflict-resolution detection.
	item, err := worldStore.GetWrit(writID)
	if err != nil {
		return nil, fmt.Errorf("failed to get writ %q: %w", writID, err)
	}

	// Reject resolve on already-closed writs — no git ops, no MR creation.
	if item.Status == "closed" {
		return nil, fmt.Errorf("writ %s is already closed — cannot resolve", writID)
	}

	// Detect conflict-resolution tasks and handle separately.
	if item.HasLabel("conflict-resolution") {
		return resolveConflictResolution(ctx, opts, item, branchName, worktreeDir,
			agentID, sessName, agent.Role, worldStore, sphereStore, mgr, logger)
	}

	// Determine if this is a code writ. Non-code writs (analysis, etc.) skip
	// git operations, MR creation, and forge nudges entirely.
	isCodeWrit := item.Kind == "" || item.Kind == "code"

	var mrID string
	var pushFailed bool

	if isCodeWrit {
		// 2. Git operations in the worktree (code writs only).
		// git add -A
		addCtx, addCancel := context.WithTimeout(ctx, GitLocalOpTimeout)
		defer addCancel()
		addCmd := exec.CommandContext(addCtx, "git", "-C", worktreeDir, "add", "-A")
		if out, err := addCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git add failed: %s: %w", strings.TrimSpace(string(out)), err)
		}

		// git commit (skip if nothing to commit)
		commitMsg := fmt.Sprintf("sol resolve: %s", item.Title)
		commitCtx, commitCancel := context.WithTimeout(ctx, GitLocalOpTimeout)
		defer commitCancel()
		commitCmd := exec.CommandContext(commitCtx, "git", "-C", worktreeDir, "commit", "-m", commitMsg)
		commitCmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME="+agent.Name,
			"GIT_AUTHOR_EMAIL="+strings.ToLower(agent.Role+"."+agent.Name)+"@sol.local",
		)
		commitCmd.CombinedOutput() // ignore error — nothing to commit is OK

		// git push: envoy pushes HEAD to a per-writ remote ref via refspec;
		// other roles push HEAD (which tracks the pre-created branch).
		pushCtx, pushCancel := context.WithTimeout(ctx, GitPushTimeout)
		defer pushCancel()
		var pushCmd *exec.Cmd
		if agent.Role == "envoy" {
			// Push HEAD to the per-writ remote ref without creating a local branch.
			// (A local branch cannot coexist with the persistent envoy branch because
			// git stores refs as a filesystem hierarchy and the envoy branch name is a
			// prefix of the per-writ branch name.)
			pushCmd = exec.CommandContext(pushCtx, "git", "-C", worktreeDir,
				"push", "origin", "HEAD:refs/heads/"+branchName)
		} else {
			pushCmd = exec.CommandContext(pushCtx, "git", "-C", worktreeDir, "push", "origin", "HEAD")
		}
		if out, err := pushCmd.CombinedOutput(); err != nil {
			slog.Warn("resolve: git push failed", "output", strings.TrimSpace(string(out)))
			pushFailed = true
		}
	}

	// Track what has been done so we can undo on failure.
	var writUpdated bool

	rollback := func() {
		if writUpdated {
			if err := worldStore.UpdateWrit(writID, store.WritUpdates{Status: "tethered"}); err != nil {
				slog.Warn("resolve rollback: failed to reset writ", "writ", writID, "error", err)
			}
		}
	}

	// 3. Update writ status.
	if isCodeWrit {
		// Code writs: status → done (idempotent — skip if already done).
		if item.Status != "done" {
			if err := worldStore.UpdateWrit(writID, store.WritUpdates{Status: "done"}); err != nil {
				return nil, fmt.Errorf("failed to update writ status: %w", err)
			}
			writUpdated = true
		}
	} else {
		// Non-code writs: close directly with close_reason "completed".
		if item.Status != "closed" {
			if _, err := worldStore.CloseWrit(writID, "completed"); err != nil {
				return nil, fmt.Errorf("failed to close non-code writ: %w", err)
			}
			writUpdated = true
		}
	}

	if isCodeWrit {
		// 4. Create merge request (idempotent — skip if one already exists for this writ).
		// Filter out failed MRs so a new resolve after a failed MR creates a fresh
		// MR with the current branch instead of reusing the stale failed one.
		existingMRs, err := worldStore.ListMergeRequestsByWrit(writID, "")
		if err != nil {
			rollback()
			return nil, fmt.Errorf("failed to check existing merge requests: %w", err)
		}
		var activeMRs []store.MergeRequest
		for _, mr := range existingMRs {
			if mr.Phase != "failed" {
				activeMRs = append(activeMRs, mr)
			}
		}
		if len(activeMRs) > 0 {
			mrID = activeMRs[0].ID
		} else if !pushFailed {
			// Only create an MR when the branch was successfully pushed.
			// If push failed, the remote branch doesn't exist yet, so creating
			// an MR would let forge attempt to merge a non-existent branch —
			// causing an infinite recast loop. The writ stays in "done" state;
			// the next resolve (after a successful push) will create the MR.
			mrID, err = worldStore.CreateMergeRequest(writID, branchName, item.Priority)
			if err != nil {
				rollback()
				return nil, fmt.Errorf("failed to create merge request for %q: %w", writID, err)
			}
		}
	}

	// Auto-resolve writ-linked escalations (best-effort).
	escalations, escErr := sphereStore.ListEscalationsBySourceRef("writ:" + writID)
	if escErr != nil {
		slog.Warn("resolve: failed to check escalations", "error", escErr)
	} else {
		for _, esc := range escalations {
			if err := sphereStore.ResolveEscalation(esc.ID); err != nil {
				slog.Warn("resolve: failed to auto-resolve escalation", "escalation", esc.ID, "error", err)
			}
		}
	}

	// 5. Clear tether BEFORE updating agent state.
	// If tether clear fails after work is already done (writ status updated,
	// MR created), don't roll back the writ — the work is complete and only
	// cleanup failed. Log the error and let consul handle orphaned tethers.
	if agent.Role == "outpost" {
		// Outpost: clear entire tether directory.
		if err := tether.Clear(opts.World, opts.AgentName, agent.Role); err != nil {
			slog.Warn("resolve: failed to clear tether (work complete, consul will clean up)",
				"agent", opts.AgentName, "writ", writID, "error", err)
		}
	} else {
		// Persistent: remove only the resolved writ's tether file.
		if err := tether.ClearOne(opts.World, opts.AgentName, writID, agent.Role); err != nil {
			slog.Warn("resolve: failed to clear tether (work complete, consul will clean up)",
				"agent", opts.AgentName, "writ", writID, "error", err)
		}
	}

	// 6. Update agent state.
	// Outpost agents are ephemeral — delete the record to reclaim the name.
	// Persistent roles (envoy) keep their record and update state
	// based on remaining tethers.
	// Note: At this point the writ is already done/closed and the tether clear
	// was attempted. Agent state failures are logged but don't roll back the
	// writ — the work is complete.
	if agent.Role == "outpost" {
		// Re-read agent to check if already deleted (idempotent re-run).
		if _, getErr := sphereStore.GetAgent(agentID); getErr == nil {
			if err := sphereStore.DeleteAgent(agentID); err != nil {
				slog.Warn("resolve: failed to delete agent (work complete)",
					"agent", agentID, "error", err)
			}
		}
	} else {
		// Persistent agent: determine remaining tethers after this resolve.
		// Tether for this writ was already cleared above, so List returns only remaining ones.
		currentTethers, listErr := tether.List(opts.World, opts.AgentName, agent.Role)
		if listErr != nil {
			slog.Warn("resolve: failed to list tethers (work complete)",
				"agent", opts.AgentName, "error", listErr)
		} else if len(currentTethers) > 0 {
			// More tethers remain: stay working.
			if agent.ActiveWrit == writID {
				// Resolving the active writ: clear active_writ but stay working.
				if err := sphereStore.UpdateAgentState(agentID, "working", ""); err != nil {
					slog.Warn("resolve: failed to update agent state (work complete)",
						"agent", agentID, "error", err)
				}
			}
			// If resolving a non-active writ, no state update needed.
		} else {
			// No remaining tethers: set to idle, clear active_writ.
			if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
				slog.Warn("resolve: failed to update agent state (work complete)",
					"agent", agentID, "error", err)
			}
		}
	}

	// 6b. Stop session after a brief delay to allow final output.
	// Envoys keep their session alive — they are human-supervised and persistent.
	// We wait for completion to prevent worktree cleanup from racing with a re-cast
	// that reuses the same agent name.
	sessionKept := false
	if agent.Role != "envoy" && agent.Role != "forge" {
		time.Sleep(1 * time.Second)
		if err := mgr.Stop(sessName, true); err != nil {
			slog.Warn("resolve: failed to stop session", "session", sessName, "error", err)
		}
		// 7b. Remove worktree for outpost agents (ephemeral worktrees only).
		if agent.Role == "outpost" {
			cleanupWorktree(opts.World, worktreeDir)
			// 7c. Remove the runtime adapter's config dir for the terminated
			// outpost. Closes the lifecycle opened by EnsureConfigDir; without
			// this, every dispatch leaks .claude-config or .codex-home (the
			// latter contains auth.json with credentials).
			cleanupOutpostConfigDir(opts.World, agent.Role, opts.AgentName)
		}
	} else {
		sessionKept = true
	}

	// 8. Emit event and nudge downstream agents (code writs only for nudges).
	if isCodeWrit {
		if logger != nil {
			logger.Emit(events.EventResolve, "sol", opts.AgentName, "both", map[string]string{
				"writ_id":      writID,
				"agent":        opts.AgentName,
				"branch":       branchName,
				"merge_request": mrID,
			})
		}

		// Nudge forge that a new MR is ready (best-effort, smart delivery).
		// Only send when an MR was actually created — an empty mrID means push
		// failed and no MR exists yet, so waking forge would be a no-op.
		if mrID != "" {
			forgeSession := config.SessionName(opts.World, "forge")
			if err := nudge.Deliver(forgeSession, nudge.Message{
				Sender:   opts.AgentName,
				Type:     "MR_READY",
				Subject:  fmt.Sprintf("MR %s ready for merge", mrID),
				Body:     fmt.Sprintf(`{"writ_id":%q,"merge_request_id":%q,"branch":%q,"title":%q}`, writID, mrID, branchName, item.Title),
				Priority: "normal",
			}); err != nil {
				slog.Warn("resolve: failed to nudge forge", "error", err)
			}
		}
	} else {
		// Non-code writs: emit event without branch/MR fields.
		if logger != nil {
			logger.Emit(events.EventResolve, "sol", opts.AgentName, "both", map[string]string{
				"writ_id": writID,
				"agent":   opts.AgentName,
				"kind":    item.Kind,
			})
		}

	}

	// 9. Close history record for cycle-time tracking.
	if _, err := worldStore.EndHistory(writID); err != nil {
		slog.Warn("resolve: failed to end history", "writ", writID, "error", err)
	}

	// For non-code writs, BranchName and MergeRequestID are empty strings.
	resultBranch := ""
	if isCodeWrit {
		resultBranch = branchName
	}

	return &ResolveResult{
		WritID:         writID,
		Title:          item.Title,
		AgentName:      opts.AgentName,
		BranchName:     resultBranch,
		MergeRequestID: mrID,
		SessionKept:    sessionKept,
	}, nil
}

// resolveConflictResolution handles the resolve flow for conflict-resolution tasks.
// Differences from normal resolve:
// 1. Uses --force-with-lease for push (branch was rebased)
// 2. Does NOT create a new merge request (original MR already exists)
// 3. Unblocks the original MR
// 4. Closes the resolution writ
func resolveConflictResolution(ctx context.Context, opts ResolveOpts, item *store.Writ, branchName, worktreeDir,
	agentID, sessName, role string, worldStore WorldStore, sphereStore SphereStore, mgr SessionManager, logger *events.Logger) (*ResolveResult, error) {

	// 1. Git operations: add, commit, force-push (branch was rebased).
	addCtx, addCancel := context.WithTimeout(ctx, GitLocalOpTimeout)
	defer addCancel()
	addCmd := exec.CommandContext(addCtx, "git", "-C", worktreeDir, "add", "-A")
	if out, err := addCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git add failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	commitMsg := fmt.Sprintf("sol resolve: %s", item.Title)
	commitCtx, commitCancel := context.WithTimeout(ctx, GitLocalOpTimeout)
	defer commitCancel()
	commitCmd := exec.CommandContext(commitCtx, "git", "-C", worktreeDir, "commit", "-m", commitMsg)
	commitCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME="+opts.AgentName,
		"GIT_AUTHOR_EMAIL="+strings.ToLower(role+"."+opts.AgentName)+"@sol.local",
	)
	commitCmd.CombinedOutput() // ignore error — nothing to commit is OK

	// Force push with lease — branch was rebased, needs force push.
	pushCtx, pushCancel := context.WithTimeout(ctx, GitPushTimeout)
	defer pushCancel()
	pushCmd := exec.CommandContext(pushCtx, "git", "-C", worktreeDir, "push", "--force-with-lease", "origin", "HEAD")
	pushFailed := false
	if out, err := pushCmd.CombinedOutput(); err != nil {
		slog.Warn("resolve: git push --force-with-lease failed", "output", strings.TrimSpace(string(out)))
		pushFailed = true
	}

	// 2. Reset the parent's MR for retry (only if push succeeded).
	// Two complementary strategies:
	//   a) Find MR blocked by this resolution task and reset it.
	//   b) Look up parent writ's MR by parent_id — covers the case where
	//      the MR ended up in 'failed' phase independently.
	if !pushFailed {
		resetMRs := map[string]bool{} // track already-reset MR IDs

		// 2a. Find MR blocked by this resolution task.
		blockedMR, err := worldStore.FindMergeRequestByBlocker(item.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to find blocked MR for %q: %w", item.ID, err)
		}
		if blockedMR != nil {
			if err := worldStore.ResetMergeRequestForRetry(blockedMR.ID); err != nil {
				return nil, fmt.Errorf("failed to reset MR %q for retry: %w", blockedMR.ID, err)
			}
			resetMRs[blockedMR.ID] = true
		}

		// 2b. Check parent writ's MRs for any stuck in 'failed' phase.
		if item.ParentID != "" {
			parentMRs, err := worldStore.ListMergeRequestsByWrit(item.ParentID, "failed")
			if err != nil {
				slog.Warn("resolve: failed to list parent MRs", "parent", item.ParentID, "error", err)
			} else {
				for _, mr := range parentMRs {
					if resetMRs[mr.ID] {
						continue
					}
					if err := worldStore.ResetMergeRequestForRetry(mr.ID); err != nil {
						slog.Warn("resolve: failed to reset parent MR", "mr", mr.ID, "error", err)
					}
				}
			}
		}
	}

	// 3. Close the resolution writ.
	if _, err := worldStore.CloseWrit(item.ID); err != nil {
		return nil, fmt.Errorf("failed to close resolution writ: %w", err)
	}

	// 4. Clear tether BEFORE updating agent state.
	// If tether clear fails after work is already done (writ closed, MR unblocked),
	// don't roll back the writ — the work is complete and only cleanup failed.
	// Log the error and let consul handle orphaned tethers.
	if role == "outpost" {
		// Outpost: clear entire tether directory.
		if err := tether.Clear(opts.World, opts.AgentName, role); err != nil {
			slog.Warn("resolve: failed to clear tether (work complete, consul will clean up)",
				"agent", opts.AgentName, "writ", item.ID, "error", err)
		}
	} else {
		// Persistent: remove only the resolved writ's tether file.
		if err := tether.ClearOne(opts.World, opts.AgentName, item.ID, role); err != nil {
			slog.Warn("resolve: failed to clear tether (work complete, consul will clean up)",
				"agent", opts.AgentName, "writ", item.ID, "error", err)
		}
	}

	// 5. Update agent state.
	// Outpost agents are ephemeral — delete the record to reclaim the name.
	// Persistent agents update state based on remaining tethers.
	// Note: At this point tether has been cleared and writ is closed.
	// Agent state failures are logged but don't roll back the writ — the
	// work is complete and tether is already gone.
	if role == "outpost" {
		if _, getErr := sphereStore.GetAgent(agentID); getErr == nil {
			if err := sphereStore.DeleteAgent(agentID); err != nil {
				slog.Warn("resolve: failed to delete agent (work complete)",
					"agent", agentID, "error", err)
			}
		}
	} else {
		// Persistent agent: determine remaining tethers after this resolve.
		// Tether for this writ was already cleared above, so List returns only remaining ones.
		currentAgent, _ := sphereStore.GetAgent(agentID)
		currentTethers, listErr := tether.List(opts.World, opts.AgentName, role)
		if listErr != nil {
			slog.Warn("resolve: failed to list tethers (work complete)",
				"agent", opts.AgentName, "error", listErr)
		} else if len(currentTethers) > 0 {
			// More tethers remain: stay working.
			if currentAgent != nil && currentAgent.ActiveWrit == item.ID {
				if err := sphereStore.UpdateAgentState(agentID, "working", ""); err != nil {
					slog.Warn("resolve: failed to update agent state (work complete)",
						"agent", agentID, "error", err)
				}
			}
		} else {
			// No remaining tethers: set to idle.
			if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
				slog.Warn("resolve: failed to update agent state (work complete)",
					"agent", agentID, "error", err)
			}
		}
	}

	// 5b. Stop session after a brief delay to allow final output.
	// Envoys keep their session alive — they are human-supervised and persistent.
	// We wait for completion to prevent worktree cleanup from racing with a re-cast
	// that reuses the same agent name.
	sessionKept := false
	if role != "envoy" && role != "forge" {
		time.Sleep(1 * time.Second)
		if err := mgr.Stop(sessName, true); err != nil {
			slog.Warn("resolve: failed to stop session", "session", sessName, "error", err)
		}
		// 6b. Remove worktree for outpost agents (ephemeral worktrees only).
		if role == "outpost" {
			cleanupWorktree(opts.World, worktreeDir)
			// 6c. Remove runtime adapter config dir (see Resolve for rationale).
			cleanupOutpostConfigDir(opts.World, role, opts.AgentName)
		}
	} else {
		sessionKept = true
	}

	// 7. Close history record for cycle-time tracking.
	if _, err := worldStore.EndHistory(item.ID); err != nil {
		slog.Warn("resolve: failed to end history", "writ", item.ID, "error", err)
	}

	if logger != nil {
		logger.Emit(events.EventResolve, "sol", opts.AgentName, "both", map[string]string{
			"writ_id": item.ID,
			"agent":        opts.AgentName,
			"branch":       branchName,
		})
	}

	return &ResolveResult{
		WritID:     item.ID,
		Title:          item.Title,
		AgentName:      opts.AgentName,
		BranchName:     branchName,
		MergeRequestID: "", // No new MR for conflict resolution.
		SessionKept:    sessionKept,
	}, nil
}
