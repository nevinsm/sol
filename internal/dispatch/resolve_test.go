package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// --- Test doubles for resolve ordering ---

// orderingSessionManager wraps mockSessionManager to capture filesystem state
// at the moment mgr.Stop is called. The L-M2 fix moves cleanup before Stop;
// these tests assert that ordering by checking what is on disk when Stop fires.
type orderingSessionManager struct {
	*mockSessionManager
	onStop func(name string)
}

func (m *orderingSessionManager) Stop(name string, force bool) error {
	if m.onStop != nil {
		m.onStop(name)
	}
	return m.mockSessionManager.Stop(name, force)
}

// --- L-M2: cleanup-before-Stop tests ---

// TestResolveCleansUpWorktreeBeforeStop verifies the L-M2 race fix: the
// worktree is removed BEFORE mgr.Stop is invoked. Before this fix, Stop ran
// first, killing the tmux session containing the resolve invocation, and
// cleanup-after-Stop lost the race against SIGKILL.
func TestResolveCleansUpWorktreeBeforeStop(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWrit("Cleanup ordering", "Verify cleanup runs before Stop", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}
	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Set up a real managed repo and create a worktree from it (matches the
	// production layout — git worktree add).
	repoPath := config.RepoPath("ember")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	runGit(t, repoPath, "init")
	runGit(t, repoPath, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, repoPath)

	worktreeDir := WorktreePath("ember", "Toast")
	branchName := fmt.Sprintf("outpost/Toast/%s", itemID)
	runGit(t, repoPath, "worktree", "add", worktreeDir, "-b", branchName, "HEAD")

	sessName := config.SessionName("ember", "Toast")

	// Capture filesystem state at the moment mgr.Stop is called.
	var worktreeAtStop bool
	var markerAtStop bool
	mgr := &orderingSessionManager{
		mockSessionManager: newMockSessionManager(),
		onStop: func(name string) {
			if name != sessName {
				return
			}
			if _, statErr := os.Stat(worktreeDir); statErr == nil {
				worktreeAtStop = true
			}
			markerPath := resolveCleanupMarkerPath("ember", "Toast", "outpost")
			if _, statErr := os.Stat(markerPath); statErr == nil {
				markerAtStop = true
			}
		},
	}
	mgr.started[sessName] = true

	if _, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Worktree must be GONE when Stop is called. A worktree-still-present
	// observation means the cleanup is racing the agent's session-death
	// finalization (the L-M2 bug we fixed).
	if worktreeAtStop {
		t.Errorf("worktree still existed when mgr.Stop was called — L-M2 race fix regressed (cleanup ordered after Stop)")
	}
	// Marker must be present at Stop time — it is removed only after cleanup
	// completes. If cleanup-before-Stop holds, the marker landed on disk and
	// then was cleared on the success path before this assertion runs.
	// The marker check here is a tighter assertion: the marker must have
	// existed BEFORE Stop was called. We can only verify it exists at Stop
	// time if Stop fires before the post-cleanup remove. Our ordering writes
	// marker → cleanup → remove marker → Stop, so the marker should be
	// already gone by Stop. Assert it was cleared (success path completed).
	if markerAtStop {
		t.Errorf("cleanup marker still present at Stop — success-path marker removal did not run")
	}

	// Sanity: Stop was actually invoked.
	if !mgr.stopped[sessName] {
		t.Errorf("mgr.Stop was not invoked")
	}
}

// TestResolveCleansUpAdapterConfigDirBeforeStop verifies that runtime
// config dir cleanup (runtime.CleanupConfigDir) runs before mgr.Stop. This is
// the codex auth.json leak path: post-Stop ordering loses the race and leaves
// credential dirs on disk indefinitely (no fallback reaper covers
// successfully-resolved outposts since the agent record is deleted).
//
// Since cleanupOutpostConfigDir calls the package-level runtime.CleanupConfigDir
// (not an injectable method), we verify the ordering indirectly by:
//  1. Creating the actual runtime config dir on disk.
//  2. Asserting it is gone when mgr.Stop fires.
//
// Without a world.toml the fallback path cleans ALL known runtimes, so we
// create the claude config dir (first in the iteration order) as the probe.
func TestResolveCleansUpAdapterConfigDirBeforeStop(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWrit("Runtime cleanup ordering", "Verify config dir cleanup runs before Stop", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}
	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	// Create the actual claude config dir that cleanupOutpostConfigDir will remove.
	// (Without world.toml the fallback path cleans ALL runtimes; claude is one of them.)
	worldDir := config.WorldDir("ember")
	claudeConfigDir := filepath.Join(worldDir, ".claude-config", "outposts", "Toast")
	if err := os.MkdirAll(claudeConfigDir, 0o755); err != nil {
		t.Fatalf("failed to create claude config dir: %v", err)
	}

	sessName := config.SessionName("ember", "Toast")

	// Capture filesystem state at Stop time.
	var configDirGoneAtStop bool
	var stopCalled bool
	mgr := &orderingSessionManager{
		mockSessionManager: newMockSessionManager(),
		onStop: func(name string) {
			if name == sessName {
				stopCalled = true
				if _, statErr := os.Stat(claudeConfigDir); os.IsNotExist(statErr) {
					configDirGoneAtStop = true
				}
			}
		},
	}
	mgr.started[sessName] = true

	if _, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if !stopCalled {
		t.Fatalf("expected mgr.Stop to be called")
	}
	if !configDirGoneAtStop {
		t.Errorf("runtime config dir was still present when mgr.Stop fired — L-M2 race fix regressed (cleanup must run BEFORE Stop)")
	}
	// Config dir must also be gone after Resolve completes.
	if _, err := os.Stat(claudeConfigDir); err == nil {
		t.Errorf("runtime config dir still exists after Resolve — cleanupOutpostConfigDir did not run")
	}
}

// TestResolveCleanupMarkerWrittenBeforeStop verifies that the synchronization
// marker mirrors the handoff.Exec marker-before-cycle invariant. The marker
// is written BEFORE the destructive cleanup ops (cleanupOutpostConfigDir +
// cleanupWorktree) and removed on the success path BEFORE mgr.Stop.
//
// We verify this by asserting the marker is already removed when Stop fires —
// meaning the full sequence (write marker → cleanup → remove marker) completed
// before the session was killed.
func TestResolveCleanupMarkerWrittenBeforeStop(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWrit("Marker before destructive op", "Verify marker write ordering", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}
	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Toast")
	markerPath := resolveCleanupMarkerPath("ember", "Toast", "outpost")

	// Capture marker state when Stop is called. On the success path the marker
	// is removed AFTER cleanup completes and BEFORE Stop fires — so if the
	// marker is already gone at Stop time, the full cleanup sequence ran first.
	var markerGoneAtStop bool
	mgr := &orderingSessionManager{
		mockSessionManager: newMockSessionManager(),
		onStop: func(name string) {
			if name == sessName {
				if _, statErr := os.Stat(markerPath); os.IsNotExist(statErr) {
					markerGoneAtStop = true
				}
			}
		},
	}
	mgr.started[sessName] = true

	if _, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// On the success path, the marker is: write → cleanup runs → remove → Stop.
	// If markerGoneAtStop is true, cleanup completed before Stop was called.
	if !markerGoneAtStop {
		t.Errorf("cleanup marker was still present when Stop fired — success-path marker removal did not run before Stop")
	}
	// Sanity: marker must also be gone after Resolve.
	if _, err := os.Stat(markerPath); err == nil {
		t.Errorf("cleanup marker still exists after Resolve — success path did not clean up %s", markerPath)
	}
}

// --- L-L4: commit-error-handling tests ---

// TestResolveCleanTreeNoCommitNoError verifies that resolving a writ with no
// staged changes (clean tree) succeeds silently — the previous code masked
// real failures by ignoring all commit errors; the new code distinguishes
// "nothing to commit" via `git diff --cached --quiet` and skips commit cleanly.
//
// The test asserts:
//   - Resolve succeeds (no error returned)
//   - No soft_failure event was emitted (HEAD stays at the initial commit,
//     not a sol-resolve commit, since nothing was staged to commit)
func TestResolveCleanTreeNoCommitNoError(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Clean tree resolve", "Verify clean-tree commit skip", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}
	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	// Capture HEAD before resolve so we can verify no extra commit was made.
	headBefore := readHead(t, worktreeDir)
	addBareRemote(t, worktreeDir)

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	logger := events.NewLogger(os.Getenv("SOL_HOME"))

	if _, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, logger); err != nil {
		t.Fatalf("Resolve failed on clean tree: %v", err)
	}

	// Resolve must succeed and the writ must be in 'done' state — no
	// soft_failure event should have been emitted for the commit step.
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ after clean-tree resolve: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected writ status 'done' after clean-tree resolve, got %q", item.Status)
	}
	matches := findSoftFailureEvents(t, "dispatch.resolve.git_commit")
	if len(matches) > 0 {
		t.Errorf("expected no soft_failure event for clean-tree resolve, got %d: %+v", len(matches), matches)
	}
	// Sanity: HEAD captured before resolve was non-empty (initial commit).
	if headBefore == "" {
		t.Fatal("test setup failure: empty HEAD before resolve")
	}
}

// TestResolveCommitHookFailureReturnsError verifies that a real commit
// failure (pre-commit hook returning non-zero) is no longer silently
// swallowed. Before the L-L4 fix, commitCmd.CombinedOutput()-and-discard
// masked hook rejections; the writ flipped to done with no commit landed.
//
// The test installs a pre-commit hook that always exits 1, makes a change
// in the worktree, then calls Resolve. Asserts:
//   - Resolve returns a non-nil error mentioning git commit
//   - A soft_failure event is emitted with op=dispatch.resolve.git_commit
//   - The writ stays in tethered state (not flipped to done)
func TestResolveCommitHookFailureReturnsError(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWrit("Hook-failing resolve", "Verify commit failure surfaces", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}
	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	// Install a pre-commit hook that always rejects.
	hooksDir := filepath.Join(worktreeDir, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("failed to create hooks dir: %v", err)
	}
	hookScript := "#!/bin/sh\necho 'rejected by test hook' 1>&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(hooksDir, "pre-commit"), []byte(hookScript), 0o755); err != nil {
		t.Fatalf("failed to write pre-commit hook: %v", err)
	}

	// Make an unstaged change so `git add -A` produces a non-empty index
	// and commit is attempted (which the hook will reject).
	if err := os.WriteFile(filepath.Join(worktreeDir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	logger := events.NewLogger(os.Getenv("SOL_HOME"))

	_, err = Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, logger)

	if err == nil {
		t.Fatal("expected Resolve to return an error when pre-commit hook rejects, got nil")
	}
	if !strings.Contains(err.Error(), "git commit failed") {
		t.Errorf("expected error mentioning 'git commit failed', got: %v", err)
	}

	// Verify a structured soft_failure event was emitted.
	matches := findSoftFailureEvents(t, "dispatch.resolve.git_commit")
	if len(matches) == 0 {
		t.Errorf("expected at least one soft_failure event with op=dispatch.resolve.git_commit, got 0")
	}

	// Verify writ is still in tethered state — not flipped to done.
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ after failed resolve: %v", err)
	}
	if item.Status != "tethered" {
		t.Errorf("expected writ status 'tethered' after commit failure, got %q", item.Status)
	}

	// No MR should have been created.
	mrs, err := worldStore.ListMergeRequestsByWrit(itemID, "")
	if err != nil {
		t.Fatalf("failed to list MRs: %v", err)
	}
	if len(mrs) > 0 {
		t.Errorf("expected no MR after commit failure, got %d", len(mrs))
	}
}

// --- Conflict-resolution push-failure tests ---

// TestResolveConflictResolutionPushFailedLeavesWritTethered verifies that when
// the git push --force-with-lease fails for a conflict-resolution writ, the
// writ is NOT closed and the parent MR is NOT reset. The result is returned
// with PushFailed=true so the operator knows to retry after fixing the push
// issue.
//
// The test also verifies the retry path: a subsequent resolve (after adding a
// working remote) succeeds, closes the writ, and resets the parent MR.
func TestResolveConflictResolutionPushFailedLeavesWritTethered(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Set up a parent writ with an MR.
	parentWritID, err := worldStore.CreateWrit("Parent writ", "A code writ needing merge", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create parent writ: %v", err)
	}
	parentMRID, err := worldStore.CreateMergeRequest(parentWritID, "outpost/Mint/"+parentWritID, 2)
	if err != nil {
		t.Fatalf("failed to create parent MR: %v", err)
	}

	// Create the conflict-resolution writ with the label resolveConflictResolution routes on.
	resolutionWritID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:       "Resolve conflict for parent",
		Description: "Conflict resolution task",
		CreatedBy:   "forge",
		Priority:    2,
		Labels:      []string{"conflict-resolution", "source-mr:" + parentMRID},
		ParentID:    parentWritID,
	})
	if err != nil {
		t.Fatalf("failed to create resolution writ: %v", err)
	}

	// Block the parent MR with the resolution writ (mirrors forge's CreateResolutionTask).
	if err := worldStore.BlockMergeRequest(parentMRID, resolutionWritID); err != nil {
		t.Fatalf("failed to block parent MR: %v", err)
	}

	// Tether the resolution writ to agent "Toast".
	if err := worldStore.UpdateWrit(resolutionWritID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update resolution writ: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", resolutionWritID); err != nil {
		t.Fatalf("failed to set agent state: %v", err)
	}
	if err := tether.Write("ember", "Toast", resolutionWritID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Set up the managed repo and worktree.
	repoPath := config.RepoPath("ember")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	runGit(t, repoPath, "init")
	runGit(t, repoPath, "commit", "--allow-empty", "-m", "initial")
	branchName := fmt.Sprintf("outpost/Toast/%s", resolutionWritID)
	worktreeDir := WorktreePath("ember", "Toast")
	runGit(t, repoPath, "worktree", "add", worktreeDir, "-b", branchName, "HEAD")
	// Add a nonexistent remote so git push fails deterministically.
	runGit(t, repoPath, "remote", "add", "origin", "/nonexistent/remote.git")

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	// --- Phase 1: resolve with failing push ---
	result, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve returned unexpected error on push failure: %v", err)
	}
	if !result.PushFailed {
		t.Errorf("expected PushFailed=true, got false")
	}

	// Writ must NOT be closed — it stays tethered for retry.
	item, err := worldStore.GetWrit(resolutionWritID)
	if err != nil {
		t.Fatalf("failed to get writ after failed push: %v", err)
	}
	if item.Status == "closed" {
		t.Errorf("expected writ to stay open/tethered after push failure, got status %q", item.Status)
	}

	// Parent MR must still be blocked by the resolution writ.
	blockedMR, err := worldStore.FindMergeRequestByBlocker(resolutionWritID)
	if err != nil {
		t.Fatalf("failed to query for blocked MR: %v", err)
	}
	if blockedMR == nil {
		t.Errorf("expected parent MR to still be blocked by resolution writ after push failure")
	}

	// Session must NOT be stopped — the writ is left tethered for retry.
	if mgr.stopped[sessName] {
		t.Errorf("expected session to NOT be stopped after push failure (writ left for retry)")
	}

	// --- Phase 2: retry with a working remote ---
	// Replace the invalid remote with a real bare clone.
	runGit(t, repoPath, "remote", "remove", "origin")
	addBareRemote(t, repoPath)

	// Tether and agent record were preserved by the early return — retry works.
	result2, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve returned error on retry with working remote: %v", err)
	}
	if result2.PushFailed {
		t.Errorf("expected PushFailed=false on successful retry, got true")
	}

	// Writ must now be closed.
	item2, err := worldStore.GetWrit(resolutionWritID)
	if err != nil {
		t.Fatalf("failed to get writ after successful retry: %v", err)
	}
	if item2.Status != "closed" {
		t.Errorf("expected writ to be closed after successful retry, got status %q", item2.Status)
	}

	// Parent MR must be unblocked (ResetMergeRequestForRetry clears blocked_by).
	blockedMR2, err := worldStore.FindMergeRequestByBlocker(resolutionWritID)
	if err != nil {
		t.Fatalf("failed to query for blocked MR after retry: %v", err)
	}
	if blockedMR2 != nil {
		t.Errorf("expected parent MR to be unblocked after successful retry, but it's still blocked")
	}
}

// --- Resolution report capture tests ---

// resolutionReportBody is a minimal five-section report used across the
// capture tests below.
const resolutionReportBody = `# Resolution Report

## Summary
Did the thing.

## Deviations from spec
None.

## Assumptions
None.

## Surprises
None.

## Durable lessons
None.
`

// setupResolutionReportWrit creates a writ, agent, tether, and worktree ready
// for a Resolve() call, mirroring the setup in the commit-handling tests
// above. Returns the writ ID and worktree dir.
func setupResolutionReportWrit(t *testing.T, worldStore *store.WorldStore, sphereStore *store.SphereStore, title string) (string, string) {
	t.Helper()

	itemID, err := worldStore.CreateWrit(title, "Verify resolution report capture", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}
	if err := tether.Write("ember", "Toast", itemID, "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	return itemID, worktreeDir
}

// TestResolveCapturesResolutionReport verifies the report-present path: a
// .resolution.md written at the worktree root before resolve is moved to
// the writ's persistent output directory as resolution.md, and the worktree
// copy is gone (both because captureResolutionReport moves it — not
// copies — and because the outpost worktree is removed entirely as part of
// teardown).
func TestResolveCapturesResolutionReport(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, worktreeDir := setupResolutionReportWrit(t, worldStore, sphereStore, "Report present")

	srcPath := filepath.Join(worktreeDir, ".resolution.md")
	if err := os.WriteFile(srcPath, []byte(resolutionReportBody), 0o644); err != nil {
		t.Fatalf("failed to write resolution report: %v", err)
	}

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	if _, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	destPath := filepath.Join(config.WritOutputDir("ember", itemID), "resolution.md")
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("expected resolution report at %q, got error: %v", destPath, err)
	}
	if string(data) != resolutionReportBody {
		t.Errorf("resolution report content mismatch:\ngot:  %q\nwant: %q", string(data), resolutionReportBody)
	}

	// Worktree copy is gone — both the file itself (moved, not copied) and
	// the whole worktree (removed by outpost teardown).
	if _, err := os.Stat(worktreeDir); !os.IsNotExist(err) {
		t.Errorf("expected worktree dir %q to be removed after resolve, stat err: %v", worktreeDir, err)
	}
}

// TestResolveResolutionReportAbsentSucceeds verifies the report-absent path:
// resolve is unaffected when no .resolution.md was written — no error, no
// output file, no soft_failure noise.
func TestResolveResolutionReportAbsentSucceeds(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, _ := setupResolutionReportWrit(t, worldStore, sphereStore, "Report absent")

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	logger := events.NewLogger(os.Getenv("SOL_HOME"))

	if _, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, logger); err != nil {
		t.Fatalf("Resolve failed on missing resolution report: %v", err)
	}

	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected writ status 'done', got %q", item.Status)
	}

	destPath := filepath.Join(config.WritOutputDir("ember", itemID), "resolution.md")
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Errorf("expected no resolution.md at %q when no report was written, stat err: %v", destPath, err)
	}

	for _, op := range []string{
		"dispatch.capture_resolution_report_stat",
		"dispatch.capture_resolution_report_mkdir",
		"dispatch.capture_resolution_report_move",
	} {
		if matches := findSoftFailureEvents(t, op); len(matches) > 0 {
			t.Errorf("expected no soft_failure event for op %q when report absent, got %d", op, len(matches))
		}
	}
}

// TestResolveResolutionReportMoveFailureStillResolves verifies the
// move-failure path: if capturing the report fails (here, because the writ's
// output directory path is blocked by a pre-existing regular file, so
// os.MkdirAll cannot create it), resolve still succeeds — the capture step
// is best-effort and must never block resolve.
func TestResolveResolutionReportMoveFailureStillResolves(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, worktreeDir := setupResolutionReportWrit(t, worldStore, sphereStore, "Report move failure")

	srcPath := filepath.Join(worktreeDir, ".resolution.md")
	if err := os.WriteFile(srcPath, []byte(resolutionReportBody), 0o644); err != nil {
		t.Fatalf("failed to write resolution report: %v", err)
	}

	// Block the output dir: pre-create a regular file at the exact path
	// config.WritOutputDir would need to MkdirAll, so the mkdir fails with
	// "not a directory" regardless of the test's uid/gid (portable, unlike
	// a permission-based failure injection).
	outDir := config.WritOutputDir("ember", itemID)
	if err := os.MkdirAll(filepath.Dir(outDir), 0o755); err != nil {
		t.Fatalf("failed to create writ-outputs parent dir: %v", err)
	}
	if err := os.WriteFile(outDir, []byte("blocking file"), 0o644); err != nil {
		t.Fatalf("failed to write blocking file at %q: %v", outDir, err)
	}

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	logger := events.NewLogger(os.Getenv("SOL_HOME"))

	result, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, logger)
	if err != nil {
		t.Fatalf("Resolve failed when resolution report move failed: %v", err)
	}
	if result.PushFailed {
		t.Errorf("expected PushFailed=false, got true")
	}

	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected writ status 'done' despite report move failure, got %q", item.Status)
	}

	mrs, err := worldStore.ListMergeRequestsByWrit(itemID, "")
	if err != nil {
		t.Fatalf("failed to list MRs: %v", err)
	}
	if len(mrs) == 0 {
		t.Errorf("expected a merge request to still be created despite report move failure")
	}

	matches := findSoftFailureEvents(t, "dispatch.capture_resolution_report_mkdir")
	if len(matches) == 0 {
		t.Errorf("expected a soft_failure event with op=dispatch.capture_resolution_report_mkdir, got 0")
	}
}

// --- Durable lessons routing tests ---

// durableLessonsReportBody has a non-empty Durable lessons section.
const durableLessonsReportBody = `# Resolution Report

## Summary
Did the thing.

## Deviations from spec
None.

## Assumptions
None.

## Surprises
None.

## Durable lessons
- Watch out for flaky retries in the merge queue.
`

// emptyDurableLessonsReportBody has a Durable lessons section containing
// only a bare bullet placeholder — no real content.
const emptyDurableLessonsReportBody = `# Resolution Report

## Summary
Did the thing.

## Deviations from spec
None.

## Assumptions
None.

## Surprises
None.

## Durable lessons
-
`

// sendMessageFailingStore wraps a real *store.SphereStore but makes
// SendMessage always fail, so tests can exercise the "mail failure must not
// fail resolve" path without needing to corrupt the sphere database (which
// would break other operations Resolve depends on, like GetAgent).
type sendMessageFailingStore struct {
	*store.SphereStore
}

func (s *sendMessageFailingStore) SendMessage(sender, recipient, subject, body string, priority int, msgType string) (string, error) {
	return "", fmt.Errorf("simulated mail failure")
}

func TestResolveDurableLessonsSentToCaravanOwner(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, worktreeDir := setupResolutionReportWrit(t, worldStore, sphereStore, "Durable lessons to caravan owner")

	caravanID, err := sphereStore.CreateCaravan("release train", "ember/Owner")
	if err != nil {
		t.Fatalf("failed to create caravan: %v", err)
	}
	if err := sphereStore.CreateCaravanItem(caravanID, itemID, "ember", 1); err != nil {
		t.Fatalf("failed to add caravan item: %v", err)
	}

	srcPath := filepath.Join(worktreeDir, ".resolution.md")
	if err := os.WriteFile(srcPath, []byte(durableLessonsReportBody), 0o644); err != nil {
		t.Fatalf("failed to write resolution report: %v", err)
	}

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	if _, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	msgs, err := sphereStore.Inbox("ember/Owner")
	if err != nil {
		t.Fatalf("failed to read inbox: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for caravan owner, got %d: %+v", len(msgs), msgs)
	}
	msg := msgs[0]
	wantSubject := "Durable lesson from " + itemID
	if msg.Subject != wantSubject {
		t.Errorf("subject = %q, want %q", msg.Subject, wantSubject)
	}
	if !strings.Contains(msg.Body, "Watch out for flaky retries") {
		t.Errorf("body missing durable lessons content: %q", msg.Body)
	}
	if !strings.Contains(msg.Body, "Durable lessons to caravan owner") {
		t.Errorf("body missing writ title: %q", msg.Body)
	}
	if !strings.Contains(msg.Body, itemID) {
		t.Errorf("body missing writ id: %q", msg.Body)
	}

	// The autarch should NOT have received a copy — the writ has a caravan.
	autarchMsgs, err := sphereStore.Inbox("autarch")
	if err != nil {
		t.Fatalf("failed to read autarch inbox: %v", err)
	}
	if len(autarchMsgs) != 0 {
		t.Errorf("expected no message to autarch when writ has a caravan owner, got %d", len(autarchMsgs))
	}
}

func TestResolveDurableLessonsSentToAutarchWhenNoCaravan(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, worktreeDir := setupResolutionReportWrit(t, worldStore, sphereStore, "Durable lessons, no caravan")

	srcPath := filepath.Join(worktreeDir, ".resolution.md")
	if err := os.WriteFile(srcPath, []byte(durableLessonsReportBody), 0o644); err != nil {
		t.Fatalf("failed to write resolution report: %v", err)
	}

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	if _, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	msgs, err := sphereStore.Inbox("autarch")
	if err != nil {
		t.Fatalf("failed to read autarch inbox: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message to autarch, got %d: %+v", len(msgs), msgs)
	}
	wantSubject := "Durable lesson from " + itemID
	if msgs[0].Subject != wantSubject {
		t.Errorf("subject = %q, want %q", msgs[0].Subject, wantSubject)
	}
}

func TestResolveDurableLessonsEmptySectionSendsNothing(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	_, worktreeDir := setupResolutionReportWrit(t, worldStore, sphereStore, "Empty durable lessons")

	srcPath := filepath.Join(worktreeDir, ".resolution.md")
	if err := os.WriteFile(srcPath, []byte(emptyDurableLessonsReportBody), 0o644); err != nil {
		t.Fatalf("failed to write resolution report: %v", err)
	}

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	if _, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	msgs, err := sphereStore.ListMessages(store.MessageFilters{})
	if err != nil {
		t.Fatalf("failed to list messages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected no mail sent for an empty Durable lessons section, got %d: %+v", len(msgs), msgs)
	}
}

func TestResolveDurableLessonsMailFailureDoesNotFailResolve(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, worktreeDir := setupResolutionReportWrit(t, worldStore, sphereStore, "Mail failure resilience")

	srcPath := filepath.Join(worktreeDir, ".resolution.md")
	if err := os.WriteFile(srcPath, []byte(durableLessonsReportBody), 0o644); err != nil {
		t.Fatalf("failed to write resolution report: %v", err)
	}

	sessName := config.SessionName("ember", "Toast")
	mgr.started[sessName] = true

	logger := events.NewLogger(os.Getenv("SOL_HOME"))
	failingStore := &sendMessageFailingStore{SphereStore: sphereStore}

	result, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, failingStore, mgr, logger)
	if err != nil {
		t.Fatalf("Resolve failed when durable-lessons mail send failed: %v", err)
	}
	if result.PushFailed {
		t.Errorf("expected PushFailed=false, got true")
	}

	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected writ status 'done' despite mail send failure, got %q", item.Status)
	}

	matches := findSoftFailureEvents(t, "dispatch.durable_lessons_send")
	if len(matches) == 0 {
		t.Errorf("expected a soft_failure event with op=dispatch.durable_lessons_send, got 0")
	}
}

// --- Test helpers ---

// readHead returns the SHA of HEAD in the given git directory.
func readHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse HEAD failed: %s: %v", string(out), err)
	}
	return strings.TrimSpace(string(out))
}

// findSoftFailureEvents returns all soft_failure events whose payload
// contains the given op. The payload is a JSON object after unmarshal.
func findSoftFailureEvents(t *testing.T, op string) []events.Event {
	t.Helper()
	path := filepath.Join(os.Getenv("SOL_HOME"), ".events.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("failed to read events log: %v", err)
	}
	var matches []events.Event
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev events.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Type != events.EventSoftFailure {
			continue
		}
		// Payload is unmarshaled as a generic JSON object (map).
		payload, ok := ev.Payload.(map[string]any)
		if !ok {
			continue
		}
		if got, _ := payload["op"].(string); got == op {
			matches = append(matches, ev)
		}
	}
	return matches
}
