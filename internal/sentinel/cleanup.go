package sentinel

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/runtime"
	"github.com/nevinsm/sol/internal/runtime/loader"
	"github.com/nevinsm/sol/internal/softfail"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// cleanupResources runs branch pruning, orphaned resource cleanup, and map pruning.
// agents is the full agent list for cleanupOrphanedResources (all roles).
// activeAgents is the active outpost subset (excluding reaped agents) for map pruning.
// Returns counts for each operation for patrol event telemetry.
func (w *Sentinel) cleanupResources(agents []store.Agent, activeAgents []store.Agent) (branchesPruned, orphansCleaned int) {
	// Prune local branches whose remote tracking branch is gone.
	branchesPruned = w.pruneOrphanedBranches()
	// Clean up orphaned resources (worktrees, session metadata, tethers).
	// Uses the full agent list so envoy and forge directory sweeps check agents of every role.
	orphansCleaned = w.cleanupOrphanedResources(agents)
	// Prune stale entries for agents no longer in the active outpost set.
	activeOutpostIDs := make(map[string]bool, len(activeAgents))
	for _, a := range activeAgents {
		activeOutpostIDs[a.ID] = true
	}
	w.pruneCaptures(activeOutpostIDs)
	w.pruneRespawnCounts(activeOutpostIDs)
	w.pruneWaitingCounts(activeOutpostIDs)
	return
}

// pruneCaptures removes hash entries for agents that are no longer working.
func (w *Sentinel) pruneCaptures(workingAgentIDs map[string]bool) {
	for key := range w.lastCaptures {
		if !workingAgentIDs[key] {
			delete(w.lastCaptures, key)
		}
	}
}

// pruneWaitingCounts removes waiting_on_background streak counters for
// agents that are no longer active, mirroring pruneCaptures/pruneRespawnCounts.
func (w *Sentinel) pruneWaitingCounts(activeAgentIDs map[string]bool) {
	for key := range w.waitingCounts {
		if !activeAgentIDs[key] {
			delete(w.waitingCounts, key)
		}
	}
}

// cleanupAgentResources removes all disk resources for an agent: worktree,
// session metadata, tether file, handoff file, and workflow directory.
// Best-effort: logs errors but does not fail.
//
// The role parameter selects the role-scoped paths used by tether.Clear,
// handoff.Remove, and runtime.CleanupConfigDir. Passing the wrong role
// silently corrupts state for the other role's tethers/handoffs, so
// callers must pass the agent's actual role rather than hardwiring "outpost".
func (w *Sentinel) cleanupAgentResources(agentName, role string) {
	sessionName := config.SessionName(w.config.World, agentName)

	// Stop session if still alive.
	if w.sessions.Exists(sessionName) {
		if err := w.sessions.Stop(sessionName, true); err != nil {
			if w.logger != nil {
				w.logger.Emit("sentinel_warn", w.agentID(), w.agentID(), "audit", map[string]any{
					"action":  "cleanup_stop_session",
					"session": sessionName,
					"error":   err.Error(),
				})
			} else {
				slog.Warn("sentinel: failed to stop session", "session", sessionName, "error", err)
			}
		}
	}

	// Remove nudge queue directory for the dead session. The agent is not
	// coming back, so there is nothing to requeue — RemoveQueueDir is a
	// wholesale reap of the queue dir. Best-effort: the directory may not
	// exist for agents that never received nudges.
	if err := nudge.RemoveQueueDir(sessionName); err != nil {
		softfail.Log(nil, "sentinel.nudge_queue_remove", err)
	}

	// Remove worktree via git.
	worktreeDir := dispatch.WorktreePath(w.config.World, agentName)
	if _, err := os.Stat(worktreeDir); err == nil {
		repoPath := config.RepoPath(w.config.World)
		rmCmd := exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", worktreeDir)
		if out, err := rmCmd.CombinedOutput(); err != nil {
			if w.logger != nil {
				w.logger.Emit("sentinel_warn", w.agentID(), w.agentID(), "audit", map[string]any{
					"action":  "cleanup_worktree_remove",
					"output":  strings.TrimSpace(string(out)),
					"error":   err.Error(),
				})
			} else {
				slog.Warn("sentinel: worktree remove failed", "output", strings.TrimSpace(string(out)), "error", err)
			}
			// Fallback: remove directory directly.
			os.RemoveAll(worktreeDir)
		}
		pruneCmd := exec.Command("git", "-C", repoPath, "worktree", "prune")
		pruneCmd.Run() // best-effort
	}

	// Remove session metadata files.
	metaPath := filepath.Join(config.RuntimeDir(), "sessions", sessionName+".json")
	os.Remove(metaPath) // best-effort
	hashPath := filepath.Join(config.RuntimeDir(), "sessions", sessionName+".last-capture-hash")
	os.Remove(hashPath) // best-effort

	// Clear tether file. Tether storage is role-scoped — passing the wrong
	// role would leave the real tether in place and silently corrupt state.
	if err := tether.Clear(w.config.World, agentName, role); err != nil {
		slog.Warn("sentinel: failed to clear tether", "agent", agentName, "role", role, "error", err)
	}

	// Remove handoff file (also role-scoped).
	if err := handoff.Remove(w.config.World, agentName, role); err != nil {
		slog.Warn("sentinel: failed to remove handoff", "agent", agentName, "role", role, "error", err)
	}

	// Remove runtime config dirs for the terminated agent. We don't know which
	// runtime owned the agent (the record may already be gone), so we invoke
	// every known runtime — CleanupConfigDir is idempotent.
	worldDir := config.WorldDir(w.config.World)
	for name, r := range loader.All() {
		if err := runtime.CleanupConfigDir(r.Descriptor(), worldDir, role, agentName); err != nil {
			slog.Warn("sentinel: failed to clean up runtime config dir",
				"agent", agentName, "role", role, "runtime", name, "error", err)
		}
	}

	// Remove the outpost directory itself if empty.
	outpostDir := filepath.Join(config.Home(), w.config.World, "outposts", agentName)
	os.Remove(outpostDir) // only succeeds if empty, which is fine
}

// cleanupOrphanedResources scans for resources on disk that have no matching
// agent record and cleans them up. Returns the number of resources cleaned.
func (w *Sentinel) cleanupOrphanedResources(agents []store.Agent) int {
	agentNames := make(map[string]bool, len(agents))
	for _, a := range agents {
		agentNames[a.Name] = true
	}

	// Build set of working agents for tether checks.
	workingAgents := make(map[string]bool)
	for _, a := range agents {
		if a.State == "working" {
			workingAgents[a.Name] = true
		}
	}

	var cleaned int
	cleaned += w.cleanupOrphanedOutpostDirs(agentNames)
	cleaned += w.cleanupOrphanedEnvoyDirs(agentNames)
	cleaned += w.cleanupOrphanedSessionMeta(agentNames)
	cleaned += w.cleanupOrphanedTethers(agentNames, workingAgents)
	return cleaned
}

// cleanupOrphanedOutpostDirs removes outpost directories that have no matching agent record.
func (w *Sentinel) cleanupOrphanedOutpostDirs(agentNames map[string]bool) int {
	outpostsDir := filepath.Join(config.Home(), w.config.World, "outposts")
	entries, err := os.ReadDir(outpostsDir)
	if err != nil {
		return 0 // directory may not exist
	}

	var cleaned int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if agentNames[name] {
			continue // agent exists, not orphaned
		}

		// Orphaned outpost directory — clean it up regardless of contents.
		// The directory may contain a worktree, stale .resume_state.json,
		// empty .tether/ dirs, or other remnants. All are safe to remove
		// since there is no matching agent record in sphere.db. The orphan
		// scanner only walks the outposts/ tree, so the role is "outpost".
		w.cleanupAgentResources(name, "outpost")

		// Force-remove any remaining files. cleanupAgentResources uses
		// os.Remove (empty-only) for the directory, but orphan cleanup
		// needs full removal of stale remnants.
		outpostDir := filepath.Join(outpostsDir, name)
		os.RemoveAll(outpostDir) // best-effort

		cleaned++

		if w.logger != nil {
			w.logger.Emit(events.EventOrphanCleanup, w.agentID(), w.agentID(), "audit",
				map[string]any{
					"type":  "outpost-dir",
					"agent": name,
					"world": w.config.World,
				})
		}
	}
	return cleaned
}

// cleanupOrphanedEnvoyDirs removes envoy directories that have no matching
// agent record in sphere.db. Mirrors cleanupOrphanedOutpostDirs but is scoped
// to $SOL_HOME/<world>/envoys/.
//
// Without this sweep, an envoy.Delete that fails midway (e.g. DB lock during
// DeleteAgent after the worktree was removed) leaves the envoy directory and
// any runtime config state on disk forever — sentinel was previously hard-
// scoped to outposts/ and could not see envoy orphans.
//
// Cleanup steps mirror what envoy.Delete does: stop any live session, remove
// the git worktree (so .git/worktrees doesn't accumulate stale entries),
// clear tether and handoff files (role=envoy), invoke every registered
// runtime's CleanupConfigDir to reap .claude-config/.codex-home leaks, then
// finally os.RemoveAll on the envoy directory itself.
func (w *Sentinel) cleanupOrphanedEnvoyDirs(agentNames map[string]bool) int {
	envoysDir := filepath.Join(config.Home(), w.config.World, "envoys")
	entries, err := os.ReadDir(envoysDir)
	if err != nil {
		return 0 // directory may not exist
	}

	var cleaned int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if agentNames[name] {
			continue // agent exists, not orphaned
		}

		// Orphaned envoy directory — clean up associated state.
		envoyDir := filepath.Join(envoysDir, name)

		// Stop session if still alive.
		sessionName := config.SessionName(w.config.World, name)
		if w.sessions.Exists(sessionName) {
			if err := w.sessions.Stop(sessionName, true); err != nil && w.logger != nil {
				w.logger.Emit("sentinel_warn", w.agentID(), w.agentID(), "audit", map[string]any{
					"action":  "orphan_envoy_stop_session",
					"session": sessionName,
					"error":   err.Error(),
				})
			}
		}

		// Remove envoy worktree via git so the source repo's worktree
		// administrative entry (.git/worktrees/<name>) is dropped too.
		// Path matches envoy.WorktreePath; hardcoded here to avoid an
		// envoy package import (sentinel sits below envoy in the import
		// graph everywhere else).
		worktreeDir := filepath.Join(envoyDir, "worktree")
		if _, err := os.Stat(worktreeDir); err == nil {
			repoPath := config.RepoPath(w.config.World)
			rmCmd := exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", worktreeDir)
			if out, err := rmCmd.CombinedOutput(); err != nil && w.logger != nil {
				w.logger.Emit("sentinel_warn", w.agentID(), w.agentID(), "audit", map[string]any{
					"action": "orphan_envoy_worktree_remove",
					"output": strings.TrimSpace(string(out)),
					"error":  err.Error(),
				})
			}
			pruneCmd := exec.Command("git", "-C", repoPath, "worktree", "prune")
			pruneCmd.Run() // best-effort
		}

		// Remove session metadata.
		metaPath := filepath.Join(config.RuntimeDir(), "sessions", sessionName+".json")
		os.Remove(metaPath) // best-effort
		hashPath := filepath.Join(config.RuntimeDir(), "sessions", sessionName+".last-capture-hash")
		os.Remove(hashPath) // best-effort

		// Clear tether file (role-scoped — must pass "envoy").
		if err := tether.Clear(w.config.World, name, "envoy"); err != nil {
			slog.Warn("sentinel: failed to clear orphan envoy tether", "agent", name, "error", err)
		}

		// Remove handoff file (role-scoped).
		if err := handoff.Remove(w.config.World, name, "envoy"); err != nil {
			slog.Warn("sentinel: failed to remove orphan envoy handoff", "agent", name, "error", err)
		}

		// Remove runtime config dirs. Mirrors cleanupAgentResources:
		// invoke every known runtime so we catch the runtime that
		// EnsureConfigDir was called against, even if the world config has
		// since been swapped. CleanupConfigDir is idempotent.
		worldDir := config.WorldDir(w.config.World)
		for runtimeName, r := range loader.All() {
			if err := runtime.CleanupConfigDir(r.Descriptor(), worldDir, "envoy", name); err != nil {
				slog.Warn("sentinel: failed to clean up orphan envoy runtime config dir",
					"agent", name, "runtime", runtimeName, "error", err)
			}
		}

		// Finally, remove the envoy directory entirely.
		os.RemoveAll(envoyDir) // best-effort

		cleaned++

		if w.logger != nil {
			w.logger.Emit(events.EventOrphanCleanup, w.agentID(), w.agentID(), "audit",
				map[string]any{
					"type":  "envoy-dir",
					"agent": name,
					"world": w.config.World,
				})
		}
	}
	return cleaned
}

// cleanupOrphanedSessionMeta removes session metadata files for dead outpost
// sessions that have no matching agent record.
func (w *Sentinel) cleanupOrphanedSessionMeta(agentNames map[string]bool) int {
	sessDir := filepath.Join(config.RuntimeDir(), "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		return 0
	}

	prefix := "sol-" + w.config.World + "-"
	var cleaned int
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		fileName := entry.Name()
		sessName := strings.TrimSuffix(fileName, ".json")
		if !strings.HasPrefix(sessName, prefix) {
			continue // not for this world
		}

		agentName := strings.TrimPrefix(sessName, prefix)
		if agentNames[agentName] {
			continue // agent exists, not orphaned
		}

		// Skip if session is still alive in tmux.
		if w.sessions.Exists(sessName) {
			continue
		}

		// Orphaned session metadata — remove it.
		os.Remove(filepath.Join(sessDir, fileName))
		hashFile := sessName + ".last-capture-hash"
		os.Remove(filepath.Join(sessDir, hashFile))
		cleaned++

		if w.logger != nil {
			w.logger.Emit(events.EventOrphanCleanup, w.agentID(), w.agentID(), "audit",
				map[string]any{
					"type":    "session_metadata",
					"session": sessName,
					"world":   w.config.World,
				})
		}
	}
	return cleaned
}

// cleanupOrphanedTethers scans tether directories for agents that are not working
// and clears all tether files within.
//
// IMPORTANT: Before clearing, re-reads agent state from DB (not the stale snapshot)
// to avoid a race with Cast(), which writes the tether before updating agent state.
func (w *Sentinel) cleanupOrphanedTethers(agentNames, workingAgents map[string]bool) int {
	outpostsDir := filepath.Join(config.Home(), w.config.World, "outposts")
	entries, err := os.ReadDir(outpostsDir)
	if err != nil {
		return 0
	}

	var cleaned int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()

		// If agent exists in DB at all (any state), skip it.
		// Only clear tethers for agents with NO record in the sphere DB
		// (truly orphaned — the agent was deleted but its tether directory
		// wasn't cleaned up). Idle agents with tethers are handled by
		// consul's stale-tether recovery with proper context.
		if agentNames[name] {
			continue
		}

		// Check if the tether directory has any files.
		if !tether.IsTethered(w.config.World, name, "outpost") {
			continue
		}

		// Tether directory non-empty for agent with no DB record — truly orphaned.
		if err := tether.Clear(w.config.World, name, "outpost"); err != nil && w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit", map[string]any{
				"error": fmt.Sprintf("failed to clear orphaned tether (best-effort): agent=%s: %v", name, err),
			})
		}
		cleaned++

		if w.logger != nil {
			w.logger.Emit(events.EventOrphanCleanup, w.agentID(), w.agentID(), "audit",
				map[string]any{
					"type":  "tether",
					"agent": name,
					"world": w.config.World,
				})
		}
	}
	return cleaned
}

// pruneOrphanedBranches deletes local branches whose remote tracking branch
// has been deleted (i.e., marked as "gone" by git). Active worktree branches
// are protected. Returns the number of branches pruned.
func (w *Sentinel) pruneOrphanedBranches() int {
	repoPath := w.config.SourceRepo
	if repoPath == "" {
		return 0
	}

	// Prune remote tracking refs for deleted remote branches.
	exec.Command("git", "-C", repoPath, "fetch", "--prune").Run()

	// List local branches with their upstream tracking status.
	// Format: %(refname:short) %(upstream:track)
	// Branches whose remote is gone show "[gone]" in the track field.
	out, err := exec.Command("git", "-C", repoPath, "for-each-ref",
		"--format=%(refname:short) %(upstream:track)",
		"refs/heads/").CombinedOutput()
	if err != nil {
		return 0
	}

	// Resolve the world's primary branch from config (default "main").
	worldBranch := "main"
	if worldCfg, cfgErr := config.LoadWorldConfig(w.config.World); cfgErr == nil {
		worldBranch = worldCfg.World.Branch
	}

	// Build set of branches used by active worktrees.
	worktreeBranches := w.listWorktreeBranches(repoPath)

	var pruned int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		if !strings.Contains(line, "[gone]") {
			continue
		}

		branch := strings.Fields(line)[0]

		// Never delete the world's primary branch.
		if branch == worldBranch {
			continue
		}

		// Protect branches that have an active worktree.
		if worktreeBranches[branch] {
			continue
		}

		// Delete the orphaned local branch.
		if err := exec.Command("git", "-C", repoPath, "branch", "-D", branch).Run(); err != nil {
			continue
		}
		pruned++

		if w.logger != nil {
			w.logger.Emit("sentinel_action", w.agentID(), w.agentID(), "audit",
				map[string]any{
					"action": "pruned_branch",
					"branch": branch,
					"world":  w.config.World,
				})
		}
	}
	return pruned
}

// listWorktreeBranches returns a set of branch names currently checked out
// in git worktrees.
func (w *Sentinel) listWorktreeBranches(repoPath string) map[string]bool {
	out, err := exec.Command("git", "-C", repoPath, "worktree", "list",
		"--porcelain").CombinedOutput()
	if err != nil {
		return nil
	}

	branches := make(map[string]bool)
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "branch ") {
			ref := strings.TrimPrefix(line, "branch ")
			// Convert refs/heads/foo to foo.
			branch := strings.TrimPrefix(ref, "refs/heads/")
			branches[branch] = true
		}
	}
	return branches
}
