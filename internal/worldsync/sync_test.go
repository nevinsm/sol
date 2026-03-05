package worldsync

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

// mockNotifyManager records Inject calls and tracks session existence.
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

func (m *mockNotifyManager) Inject(name, text string, submit bool) error {
	m.injected = append(m.injected, mockCall{Name: name, Text: text})
	return nil
}

// mockAgentLister returns a static list of agents.
type mockAgentLister struct {
	agents []store.Agent
	err    error
}

func (m *mockAgentLister) ListAgents(world string, state string) ([]store.Agent, error) {
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
	if err := SyncRepo(world); err != nil {
		t.Fatalf("SyncRepo failed: %v", err)
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

	if err := SyncRepo(world); err != nil {
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
	if err := SyncRepo(world); err != nil {
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

	err := SyncRepo("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent repo")
	}
}

func TestSyncForge(t *testing.T) {
	bare, workingClone := createBareAndClone(t)

	// Create a forge worktree structure.
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	world := "testworld"

	// Clone bare into the forge worktree path.
	forgeWT := filepath.Join(solHome, world, "forge", "worktree")
	run(t, "", "git", "clone", bare, forgeWT)
	run(t, forgeWT, "git", "config", "user.email", "test@test.com")
	run(t, forgeWT, "git", "config", "user.name", "Test")

	// Create forge branch.
	forgeBranch := "forge/" + world
	run(t, forgeWT, "git", "checkout", "-b", forgeBranch)

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

	if err := SyncEnvoy("testworld", "Jimmy", mgr); err != nil {
		t.Fatalf("SyncEnvoy failed: %v", err)
	}

	if len(mgr.injected) != 1 {
		t.Fatalf("expected 1 Inject call, got %d", len(mgr.injected))
	}
	if mgr.injected[0].Name != "sol-testworld-Jimmy" {
		t.Errorf("Inject session = %q, want sol-testworld-Jimmy", mgr.injected[0].Name)
	}

}

func TestSyncEnvoyNoSession(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	mgr := newMockNotifyManager()
	// No sessions registered.

	if err := SyncEnvoy("testworld", "Jimmy", mgr); err != nil {
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

	// Create governor directory so it gets synced.
	govDir := filepath.Join(solHome, world, "governor")
	if err := os.MkdirAll(govDir, 0o755); err != nil {
		t.Fatal(err)
	}

	mgr := newMockNotifyManager()
	mgr.sessions["sol-testworld-Jimmy"] = true
	mgr.sessions["sol-testworld-governor"] = true

	lister := &mockAgentLister{
		agents: []store.Agent{
			{Name: "Jimmy", World: world, Role: "envoy"},
			{Name: "forge", World: world, Role: "forge"},
		},
	}

	results := SyncAllComponents(world, "main", lister, mgr)

	// Should have envoy:Jimmy and governor results (no forge since worktree doesn't exist).
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	components := map[string]bool{}
	for _, r := range results {
		components[r.Component] = true
		if r.Err != nil {
			// Governor and envoy notifications should succeed with mock.
			t.Errorf("unexpected error for %s: %v", r.Component, r.Err)
		}
	}

	if !components["envoy:Jimmy"] {
		t.Error("missing envoy:Jimmy result")
	}
	if !components["governor"] {
		t.Error("missing governor result")
	}

	// Verify both sessions were notified.
	if len(mgr.injected) != 2 {
		t.Errorf("expected 2 Inject calls, got %d", len(mgr.injected))
	}
}
