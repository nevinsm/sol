package worldsync

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/store"
)

// mockNotifyManager records NudgeSession calls and tracks session existence.
type mockNotifyManager struct {
	sessions map[string]bool
	injected []mockCall
}

type mockCall struct {
	Name string
	Text string
}

func newMockNotifyManager() *mockNotifyManager {
	return &mockNotifyManager{sessions: make(map[string]bool)}
}

func (m *mockNotifyManager) Exists(name string) bool {
	return m.sessions[name]
}

func (m *mockNotifyManager) NudgeSession(name, message string) error {
	m.injected = append(m.injected, mockCall{Name: name, Text: message})
	return nil
}

// mockAgentLister returns a static list of agents.
type mockAgentLister struct {
	agents []store.Agent
	err    error
}

func (m *mockAgentLister) GetAgent(id string) (*store.Agent, error) {
	return nil, nil
}

func (m *mockAgentLister) FindIdleAgent(world string) (*store.Agent, error) {
	return nil, nil
}

func (m *mockAgentLister) ListAgents(world string, state store.AgentState) ([]store.Agent, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []store.Agent
	for _, a := range m.agents {
		if world != "" && a.World != world {
			continue
		}
		result = append(result, a)
	}
	return result, nil
}

// createBareAndClone creates a bare git repo and a clone with an initial commit.
// Returns (bareRepo, clone) paths.
func createBareAndClone(t *testing.T) (string, string) {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "origin.git")
	clone := filepath.Join(t.TempDir(), "clone")

	run(t, "", "git", "init", "--bare", bare)
	run(t, "", "git", "clone", bare, clone)
	run(t, clone, "git", "config", "user.email", "test@test.com")
	run(t, clone, "git", "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(clone, "file.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, clone, "git", "add", ".")
	run(t, clone, "git", "commit", "-m", "initial")
	run(t, clone, "git", "push", "origin", "main")

	return bare, clone
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %s: %v", name, args, out, err)
	}
}

func TestSyncRepo(t *testing.T) {
	bare, workingClone := createBareAndClone(t)

	// Create a "managed repo" clone of the bare repo.
	managedRepo := t.TempDir()
	run(t, "", "git", "clone", bare, managedRepo)

	// Push a new commit from the working clone.
	if err := os.WriteFile(filepath.Join(workingClone, "file.txt"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, workingClone, "git", "add", ".")
	run(t, workingClone, "git", "commit", "-m", "update")
	run(t, workingClone, "git", "push", "origin", "main")

	// Point SOL_HOME so config.RepoPath finds our managed repo.
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	world := "testworld"
	repoDir := filepath.Join(solHome, world, "repo")
	if err := os.MkdirAll(filepath.Dir(repoDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(managedRepo, repoDir); err != nil {
		t.Fatal(err)
	}

	// SyncRepo should bring in the new commit.
	outcome, err := SyncRepo(world)
	if err != nil {
		t.Fatalf("SyncRepo failed: %v", err)
	}

	// Should report advancement.
	if !outcome.Advanced {
		t.Error("expected outcome.Advanced to be true")
	}
	if outcome.OldHead == "" || outcome.NewHead == "" {
		t.Errorf("expected non-empty SHAs, got old=%q new=%q", outcome.OldHead, outcome.NewHead)
	}
	if outcome.OldHead == outcome.NewHead {
		t.Errorf("expected different SHAs, got old=%q new=%q", outcome.OldHead, outcome.NewHead)
	}

	// Verify file has v2 content.
	data, err := os.ReadFile(filepath.Join(repoDir, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v2" {
		t.Errorf("expected file content 'v2', got %q", string(data))
	}
}

// TestSyncRepoInstallsExcludes verifies that SyncRepo re-installs the
// sol-managed excludes into .git/info/exclude on every sync, so a world
// created before a new exclude pattern was added (or one whose excludes were
// never installed) converges on the current canonical list without any
// manual step. See setup.InstallExcludes.
func TestSyncRepoInstallsExcludes(t *testing.T) {
	bare, _ := createBareAndClone(t)

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	world := "testworld"
	repoDir := filepath.Join(solHome, world, "repo")
	run(t, "", "git", "clone", bare, repoDir)

	excludePath := filepath.Join(repoDir, ".git", "info", "exclude")

	// Fresh clone has no sol-managed block yet.
	before, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if strings.Contains(string(before), "# BEGIN sol-managed paths") {
		t.Fatal("test setup invariant violated: fresh clone already has sol-managed block")
	}

	if _, err := SyncRepo(world); err != nil {
		t.Fatalf("SyncRepo failed: %v", err)
	}

	data, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "# BEGIN sol-managed paths") {
		t.Error("expected SyncRepo to install the sol-managed exclude block")
	}
	if !strings.Contains(content, ".resolution.md") {
		t.Error("expected SyncRepo to install the .resolution.md exclude pattern")
	}

	// Syncing again must not duplicate the block — SyncRepo re-installs
	// excludes on every run, so this is the primary regression guard for
	// convergence without manual intervention.
	if _, err := SyncRepo(world); err != nil {
		t.Fatalf("second SyncRepo failed: %v", err)
	}
	data, err = os.ReadFile(excludePath)
	if err != nil {
		t.Fatal(err)
	}
	content = string(data)
	if n := strings.Count(content, "# BEGIN sol-managed paths"); n != 1 {
		t.Errorf("expected exactly 1 BEGIN marker after repeated sync, got %d", n)
	}
	if n := strings.Count(content, "# END sol-managed paths"); n != 1 {
		t.Errorf("expected exactly 1 END marker after repeated sync, got %d", n)
	}
}

func TestSyncRepoDirtyWorkingTree(t *testing.T) {
	bare, workingClone := createBareAndClone(t)

	// Push v2 from working clone.
	if err := os.WriteFile(filepath.Join(workingClone, "file.txt"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, workingClone, "git", "add", ".")
	run(t, workingClone, "git", "commit", "-m", "update")
	run(t, workingClone, "git", "push", "origin", "main")

	// Create managed repo and dirty its working tree.
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	world := "testworld"
	repoDir := filepath.Join(solHome, world, "repo")
	run(t, "", "git", "clone", bare, repoDir)

	// Dirty: modify tracked file and add untracked file.
	if err := os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "untracked.txt"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := SyncRepo(world); err != nil {
		t.Fatalf("SyncRepo failed with dirty working tree: %v", err)
	}

	// Tracked file should have v2 content.
	data, err := os.ReadFile(filepath.Join(repoDir, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v2" {
		t.Errorf("expected file content 'v2', got %q", string(data))
	}

	// Untracked file should be removed by git clean.
	if _, err := os.Stat(filepath.Join(repoDir, "untracked.txt")); !os.IsNotExist(err) {
		t.Error("expected untracked.txt to be removed by git clean")
	}
}

func TestSyncRepoDivergedBranch(t *testing.T) {
	bare, workingClone := createBareAndClone(t)

	// Create managed repo clone.
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	world := "testworld"
	repoDir := filepath.Join(solHome, world, "repo")
	run(t, "", "git", "clone", bare, repoDir)
	run(t, repoDir, "git", "config", "user.email", "test@test.com")
	run(t, repoDir, "git", "config", "user.name", "Test")

	// Create a local-only commit in managed repo (diverge from origin).
	if err := os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "local divergence")

	// Push a different commit from working clone.
	if err := os.WriteFile(filepath.Join(workingClone, "file.txt"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, workingClone, "git", "add", ".")
	run(t, workingClone, "git", "commit", "-m", "update")
	run(t, workingClone, "git", "push", "origin", "main")

	// SyncRepo should succeed despite divergence.
	if _, err := SyncRepo(world); err != nil {
		t.Fatalf("SyncRepo failed with diverged branch: %v", err)
	}

	// File should have v2 content from origin.
	data, err := os.ReadFile(filepath.Join(repoDir, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v2" {
		t.Errorf("expected file content 'v2', got %q", string(data))
	}
}

func TestSyncRepoNoRepo(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	_, err := SyncRepo("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent repo")
	}
}

// TestSyncRepoAuthFailureFailsFastWithGuidance reproduces the bug this writ
// fixes: an HTTPS remote with no stored credential must never let git block
// on an interactive "Username for '...':" prompt (fatal in a headless
// daemon — there is no terminal to answer it). GIT_TERMINAL_PROMPT=0 is set
// process-wide by cmd.Execute() in the real binary; this test sets it
// directly to reproduce that behavior, then asserts SyncRepo fails quickly
// (rather than hanging) with an error that points the operator at
// docs/credentials.md instead of surfacing git's raw "terminal prompts
// disabled" wording.
func TestSyncRepoAuthFailureFailsFastWithGuidance(t *testing.T) {
	t.Setenv("GIT_TERMINAL_PROMPT", "0")

	// A minimal local HTTP server that always demands auth — simulates an
	// HTTPS remote with no stored credential without touching the network.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	world := "testworld"
	repoDir := filepath.Join(solHome, world, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, repoDir, "git", "init")
	run(t, repoDir, "git", "config", "user.email", "test@test.com")
	run(t, repoDir, "git", "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "initial")
	run(t, repoDir, "git", "remote", "add", "origin", srv.URL+"/repo.git")

	done := make(chan struct{})
	var err error
	start := time.Now()
	go func() {
		_, err = SyncRepo(world)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("SyncRepo hung — git must fail fast on a credential-less HTTPS remote, not prompt")
	}
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected SyncRepo to fail against a credential-less HTTPS remote")
	}
	t.Logf("SyncRepo failed after %v: %v", elapsed, err)
	if !strings.Contains(err.Error(), "docs/credentials.md") {
		t.Errorf("SyncRepo error = %q, want mention of docs/credentials.md", err.Error())
	}
}

func TestSyncForge(t *testing.T) {
	bare, workingClone := createBareAndClone(t)

	// Create a forge worktree structure (detached HEAD, like real forge).
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	world := "testworld"

	// Clone bare into the forge worktree path and detach HEAD.
	forgeWT := filepath.Join(solHome, world, "forge", "worktree")
	run(t, "", "git", "clone", bare, forgeWT)
	run(t, forgeWT, "git", "checkout", "--detach")

	// Push a new commit from working clone.
	if err := os.WriteFile(filepath.Join(workingClone, "file.txt"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, workingClone, "git", "add", ".")
	run(t, workingClone, "git", "commit", "-m", "update")
	run(t, workingClone, "git", "push", "origin", "main")

	// SyncForge should reset to origin/main.
	if err := SyncForge(world, "main"); err != nil {
		t.Fatalf("SyncForge failed: %v", err)
	}

	// Verify file has v2 content.
	data, err := os.ReadFile(filepath.Join(forgeWT, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v2" {
		t.Errorf("expected file content 'v2', got %q", string(data))
	}
}

func TestSyncForgeCleanUntrackedFiles(t *testing.T) {
	bare, _ := createBareAndClone(t)

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	world := "testworld"

	// Clone bare into the forge worktree path and detach HEAD.
	forgeWT := filepath.Join(solHome, world, "forge", "worktree")
	run(t, "", "git", "clone", bare, forgeWT)
	run(t, forgeWT, "git", "checkout", "--detach")

	// Create untracked cruft in the forge worktree.
	if err := os.WriteFile(filepath.Join(forgeWT, "cruft.txt"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(forgeWT, "cruft-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(forgeWT, "cruft-dir", "junk.txt"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SyncForge(world, "main"); err != nil {
		t.Fatalf("SyncForge failed: %v", err)
	}

	// Untracked files should be removed by git clean -fd.
	if _, err := os.Stat(filepath.Join(forgeWT, "cruft.txt")); !os.IsNotExist(err) {
		t.Error("expected cruft.txt to be removed by git clean")
	}
	if _, err := os.Stat(filepath.Join(forgeWT, "cruft-dir")); !os.IsNotExist(err) {
		t.Error("expected cruft-dir/ to be removed by git clean")
	}
}

func TestSyncForgePreservesExcludedFiles(t *testing.T) {
	bare, _ := createBareAndClone(t)

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	world := "testworld"

	// Clone bare into the forge worktree path and detach HEAD.
	forgeWT := filepath.Join(solHome, world, "forge", "worktree")
	run(t, "", "git", "clone", bare, forgeWT)
	run(t, forgeWT, "git", "checkout", "--detach")

	// Add sol-managed patterns to .git/info/exclude.
	excludePath := filepath.Join(forgeWT, ".git", "info", "exclude")
	existing, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatal(err)
	}
	excludeContent := string(existing) + "\nCLAUDE.local.md\n.claude/settings.local.json\n"
	if err := os.WriteFile(excludePath, []byte(excludeContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create both excluded (sol-managed) and untracked files.
	if err := os.WriteFile(filepath.Join(forgeWT, "CLAUDE.local.md"), []byte("agent persona"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(forgeWT, "cruft.txt"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SyncForge(world, "main"); err != nil {
		t.Fatalf("SyncForge failed: %v", err)
	}

	// Excluded file should be preserved (git clean -fd without -x respects excludes).
	if _, err := os.Stat(filepath.Join(forgeWT, "CLAUDE.local.md")); os.IsNotExist(err) {
		t.Error("CLAUDE.local.md should be preserved (excluded), but was removed")
	}

	// Untracked file should be removed.
	if _, err := os.Stat(filepath.Join(forgeWT, "cruft.txt")); !os.IsNotExist(err) {
		t.Error("expected cruft.txt to be removed by git clean")
	}
}

func TestSyncForgeNoWorktree(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// No forge worktree — should return nil.
	if err := SyncForge("nonexistent", "main"); err != nil {
		t.Fatalf("expected nil for nonexistent forge worktree, got: %v", err)
	}
}

func TestSyncEnvoyNotifiesSession(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	mgr := newMockNotifyManager()
	mgr.sessions["sol-testworld-Jimmy"] = true

	outcome := &SyncOutcome{Advanced: true, OldHead: "abc1234", NewHead: "def5678"}
	if err := SyncEnvoy("testworld", "Jimmy", mgr, outcome); err != nil {
		t.Fatalf("SyncEnvoy failed: %v", err)
	}

	// The pane only ever sees the fixed doorbell now; the actual sync
	// message goes through the durable nudge queue.
	if len(mgr.injected) != 1 {
		t.Fatalf("expected 1 Inject call, got %d", len(mgr.injected))
	}
	if mgr.injected[0].Name != "sol-testworld-Jimmy" {
		t.Errorf("Inject session = %q, want sol-testworld-Jimmy", mgr.injected[0].Name)
	}
	if mgr.injected[0].Text != nudge.DoorbellMessage {
		t.Errorf("Inject text = %q, want doorbell %q", mgr.injected[0].Text, nudge.DoorbellMessage)
	}

	messages, err := nudge.Drain("sol-testworld-Jimmy")
	if err != nil {
		t.Fatalf("nudge.Drain failed: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 queued sync message, got %d", len(messages))
	}
	msg := messages[0].Body
	if !strings.Contains(msg, "testworld") {
		t.Errorf("expected queued message to contain world name, got %q", msg)
	}
	if !strings.Contains(msg, "abc1234..def5678") {
		t.Errorf("expected queued message to contain commit range, got %q", msg)
	}
}

func TestSyncEnvoyNoAdvancement(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	mgr := newMockNotifyManager()
	mgr.sessions["sol-testworld-Jimmy"] = true

	// Repo did not advance — should skip injection.
	outcome := &SyncOutcome{Advanced: false, OldHead: "abc1234", NewHead: "abc1234"}
	if err := SyncEnvoy("testworld", "Jimmy", mgr, outcome); err != nil {
		t.Fatalf("SyncEnvoy failed: %v", err)
	}

	if len(mgr.injected) != 0 {
		t.Errorf("expected no Inject calls when repo didn't advance, got %d", len(mgr.injected))
	}
}

func TestSyncEnvoyNoSession(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	mgr := newMockNotifyManager()
	// No sessions registered.

	outcome := &SyncOutcome{Advanced: true, OldHead: "abc1234", NewHead: "def5678"}
	if err := SyncEnvoy("testworld", "Jimmy", mgr, outcome); err != nil {
		t.Fatalf("SyncEnvoy failed: %v", err)
	}

	if len(mgr.injected) != 0 {
		t.Errorf("expected no Inject calls, got %d", len(mgr.injected))
	}
}

func TestSyncAllComponents(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	world := "testworld"

	mgr := newMockNotifyManager()
	mgr.sessions["sol-testworld-Jimmy"] = true

	lister := &mockAgentLister{
		agents: []store.Agent{
			{Name: "Jimmy", World: world, Role: "envoy"},
			{Name: "forge", World: world, Role: "forge"},
		},
	}

	outcome := &SyncOutcome{Advanced: true, OldHead: "abc1234", NewHead: "def5678"}
	results := SyncAllComponents(world, "main", lister, mgr, outcome)

	// Should have envoy:Jimmy result (no forge since worktree doesn't exist).
	if len(results) < 1 {
		t.Fatalf("expected at least 1 result, got %d", len(results))
	}

	components := map[string]bool{}
	for _, r := range results {
		components[r.Component] = true
		if r.Err != nil {
			t.Errorf("unexpected error for %s: %v", r.Component, r.Err)
		}
	}

	if !components["envoy:Jimmy"] {
		t.Error("missing envoy:Jimmy result")
	}

	// Verify envoy session was notified.
	if len(mgr.injected) != 1 {
		t.Errorf("expected 1 Inject call, got %d", len(mgr.injected))
	}
}

func TestSyncAllComponentsNoAdvancement(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	world := "testworld"

	mgr := newMockNotifyManager()
	mgr.sessions["sol-testworld-Jimmy"] = true

	lister := &mockAgentLister{
		agents: []store.Agent{
			{Name: "Jimmy", World: world, Role: "envoy"},
		},
	}

	outcome := &SyncOutcome{Advanced: false, OldHead: "abc1234", NewHead: "abc1234"}
	results := SyncAllComponents(world, "main", lister, mgr, outcome)

	// Envoy result should exist but no injection should have happened.
	components := map[string]bool{}
	for _, r := range results {
		components[r.Component] = true
	}
	if !components["envoy:Jimmy"] {
		t.Error("missing envoy:Jimmy result")
	}

	if len(mgr.injected) != 0 {
		t.Errorf("expected no Inject calls when repo didn't advance, got %d", len(mgr.injected))
	}
}
