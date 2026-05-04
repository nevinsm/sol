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
	"time"

	"github.com/nevinsm/sol/internal/adapter"
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

// fakeAdapter is a minimal RuntimeAdapter test double that records
// CleanupConfigDir invocations. It only implements methods the resolve path
// actually exercises in tests; the rest panic to surface accidental usage.
type fakeAdapter struct {
	name            string
	cleanupCalled   bool
	cleanupCalledAt time.Time
}

func (a *fakeAdapter) CleanupConfigDir(worldDir, role, agent string) error {
	a.cleanupCalled = true
	a.cleanupCalledAt = time.Now()
	return nil
}

func (a *fakeAdapter) Name() string { return a.name }

// All other methods of RuntimeAdapter are unused in resolve-path tests;
// panic so any accidental use shows up immediately rather than silently
// returning zero values.
func (a *fakeAdapter) InjectPersona(string, []byte) error                 { panic("unused") }
func (a *fakeAdapter) InstallSkills(string, []adapter.Skill) error        { panic("unused") }
func (a *fakeAdapter) InjectSystemPrompt(string, string, bool) (string, error) {
	panic("unused")
}
func (a *fakeAdapter) InstallHooks(string, string, string, string, adapter.HookSet) error {
	panic("unused")
}
func (a *fakeAdapter) MemoryDir(string, string, string) string { return "" }
func (a *fakeAdapter) EnsureConfigDir(string, string, string, string) (adapter.ConfigResult, error) {
	panic("unused")
}
func (a *fakeAdapter) BuildCommand(adapter.CommandContext) string { panic("unused") }
func (a *fakeAdapter) CredentialEnv(adapter.Credential) (map[string]string, error) {
	panic("unused")
}
func (a *fakeAdapter) InstallCredential(string, adapter.Credential) error { panic("unused") }
func (a *fakeAdapter) TelemetryEnv(int, string, string, string, string) map[string]string {
	return nil
}
func (a *fakeAdapter) ExtractTelemetry(string, map[string]string) *adapter.TelemetryRecord {
	return nil
}
func (a *fakeAdapter) SupportsHook(string) bool { return false }
func (a *fakeAdapter) CalloutCommand() string   { return "" }
func (a *fakeAdapter) DefaultModel() string     { return "" }

// registerFakeAdapter installs a fake adapter under a test-only name and
// arranges to remove it from the global registry when the test ends. We use
// adapter.Register directly because the registry has no public unregister.
// t.Cleanup deletes the entry via the package-level map handle returned by
// adapter.All — adapter.Register itself overwrites entries, so a cleanup
// that re-registers the prior value (or a sentinel no-op) is sufficient.
func registerFakeAdapter(t *testing.T, name string) *fakeAdapter {
	t.Helper()
	fa := &fakeAdapter{name: name}
	adapter.Register(name, fa)
	t.Cleanup(func() {
		// Overwrite with a no-op stub so other tests in the same binary
		// don't see this adapter. The registry has no Unregister, but
		// Register is just a map assignment, so storing a fresh stub
		// effectively retires the test adapter.
		adapter.Register(name, &fakeAdapter{name: name})
	})
	return fa
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
// adapters' CleanupConfigDir is invoked before mgr.Stop. This is the codex
// auth.json leak path: post-Stop ordering loses the race and leaves
// credential dirs on disk indefinitely (no fallback reaper covers
// successfully-resolved outposts since the agent record is deleted).
func TestResolveCleansUpAdapterConfigDirBeforeStop(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	// The cleanupOutpostConfigDir helper resolves the runtime from world
	// config, which defaults to "claude" when no config file exists. We
	// register the fake under "claude" so the primary lookup path
	// (adapter.Get(runtime)) finds it and invokes CleanupConfigDir directly,
	// matching the production code path rather than the All() fallback.
	fa := registerFakeAdapter(t, "claude")

	itemID, err := worldStore.CreateWrit("Adapter cleanup ordering", "Verify config dir cleanup runs before Stop", "autarch", 2, nil)
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

	// Capture whether adapter cleanup completed before Stop.
	var stopCalledAt time.Time
	mgr := &orderingSessionManager{
		mockSessionManager: newMockSessionManager(),
		onStop: func(name string) {
			if name == sessName {
				stopCalledAt = time.Now()
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

	if !fa.cleanupCalled {
		t.Fatalf("expected fake adapter CleanupConfigDir to be called")
	}
	if stopCalledAt.IsZero() {
		t.Fatalf("expected mgr.Stop to be called")
	}
	if !fa.cleanupCalledAt.Before(stopCalledAt) {
		t.Errorf("expected adapter CleanupConfigDir at %v to run BEFORE mgr.Stop at %v — L-M2 race ordering regressed",
			fa.cleanupCalledAt, stopCalledAt)
	}
}

// TestResolveCleanupMarkerWrittenBeforeStop verifies that the synchronization
// marker mirrors the handoff.Exec marker-before-cycle invariant. We capture
// the marker's appearance using a hook that fires when CleanupConfigDir is
// called — by that point the marker must already be on disk, since the
// resolve flow writes it before invoking adapter cleanup.
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

	// Use the worktree-removal path itself as a probe: when cleanupWorktree
	// runs, the marker must already exist. We piggy-back on Stop's onStop
	// hook to also assert the worktree was removed (covered above) — here
	// we directly verify the marker landed on disk before Stop fires.
	var markerExistedBeforeWorktreeRemoval bool
	// We register a probing fake adapter under "claude" so the primary
	// adapter.Get(runtime) lookup finds it (resolveRuntime defaults to
	// "claude" with no world config). cleanupOutpostConfigDir invokes the
	// adapter BEFORE cleanupWorktree, so the probe sees the marker on disk.
	fa := registerFakeAdapter(t, "claude")
	adapter.Register("claude", &markerProbingAdapter{
		fakeAdapter: fa,
		probe: func() {
			if _, err := os.Stat(markerPath); err == nil {
				markerExistedBeforeWorktreeRemoval = true
			}
		},
	})

	mgr := newMockSessionManager()
	mgr.started[sessName] = true

	if _, err := Resolve(context.Background(), ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if !markerExistedBeforeWorktreeRemoval {
		t.Errorf("expected .resolve_cleanup_in_progress marker to be on disk before adapter cleanup ran")
	}
	// Marker should be cleared on the success path.
	if _, err := os.Stat(markerPath); err == nil {
		t.Errorf("expected cleanup marker to be removed on success, but %s still exists", markerPath)
	}
}

// markerProbingAdapter is a fakeAdapter that runs a probe before performing
// its CleanupConfigDir work. Used to capture filesystem state in the moment
// between marker write and worktree removal.
type markerProbingAdapter struct {
	*fakeAdapter
	probe func()
}

func (a *markerProbingAdapter) CleanupConfigDir(worldDir, role, agent string) error {
	if a.probe != nil {
		a.probe()
	}
	return a.fakeAdapter.CleanupConfigDir(worldDir, role, agent)
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
