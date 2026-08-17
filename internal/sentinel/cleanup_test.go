package sentinel

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// --- Tests ---

func TestCleanupOrphanedWorktree(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Do NOT create an agent record for "Ghost".
	// But create an outpost directory with a worktree on disk.
	solHome := os.Getenv("SOL_HOME")
	worktreeDir := filepath.Join(solHome, "ember", "outposts", "Ghost", "worktree")
	os.MkdirAll(worktreeDir, 0o755)
	// Also create a tether.
	if err := tether.Write("ember", "Ghost", "sol-00000000000a0ced", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Tether should be cleaned up.
	if tether.IsTethered("ember", "Ghost", "outpost") {
		t.Error("expected orphaned tether to be removed")
	}

	// Outpost directory should be fully removed.
	outpostDir := filepath.Join(solHome, "ember", "outposts", "Ghost")
	if _, err := os.Stat(outpostDir); !os.IsNotExist(err) {
		t.Error("expected orphaned outpost directory to be fully removed")
	}
}

func TestCleanupOrphanedOutpostRemovesAdapterConfigDirs(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// No agent record for "Spectre" — orphan sweep target.
	solHome := os.Getenv("SOL_HOME")
	worldDir := filepath.Join(solHome, "ember")

	// 1. Outpost worktree dir.
	worktreeDir := filepath.Join(worldDir, "outposts", "Spectre", "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 2. Claude config dir (the previously-leaking path).
	claudeConfigDir := filepath.Join(worldDir, ".claude-config", "outposts", "Spectre")
	if err := os.MkdirAll(claudeConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeConfigDir, "settings.json"),
		[]byte(`{"foo":"bar"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. Codex config dir with an auth.json containing a credential.
	codexConfigDir := filepath.Join(worldDir, ".codex-config", "outposts", "Spectre")
	if err := os.MkdirAll(codexConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const credSecret = "sk-orphan-sweep-leak-canary"
	if err := os.WriteFile(filepath.Join(codexConfigDir, "auth.json"),
		[]byte(`{"OPENAI_API_KEY":"`+credSecret+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// 4. Sibling envoy config dir that MUST survive — regression check.
	envoyConfigDir := filepath.Join(worldDir, ".claude-config", "envoys", "Reaver")
	if err := os.MkdirAll(envoyConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	envoySentinel := filepath.Join(envoyConfigDir, "settings.json")
	if err := os.WriteFile(envoySentinel, []byte(`{"keep":"me"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Outpost dir gone (existing behavior).
	if _, err := os.Stat(filepath.Join(worldDir, "outposts", "Spectre")); !os.IsNotExist(err) {
		t.Error("expected orphaned outpost directory to be removed")
	}

	// Claude config dir gone (new behavior under fix).
	if _, err := os.Stat(claudeConfigDir); !os.IsNotExist(err) {
		t.Error("expected orphaned .claude-config/outposts/Spectre to be removed")
	}

	// Codex config dir gone (new behavior under fix).
	if _, err := os.Stat(codexConfigDir); !os.IsNotExist(err) {
		t.Error("expected orphaned .codex-config/outposts/Spectre to be removed")
	}

	// Envoy config dir untouched.
	if _, err := os.Stat(envoySentinel); err != nil {
		t.Errorf("envoy config disturbed by orphan sweep: %v", err)
	}
}

// TestCleanupAgentResourcesNonOutpostRoleClearsTether verifies that
// cleanupAgentResources clears the tether and handoff for the actual role
// passed in, not a hardwired "outpost". A non-outpost role (envoy) is
// exercised here as the regression check for CF-L6.
func TestCleanupAgentResourcesNonOutpostRoleClearsTether(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	world := cfg.World
	role := "envoy"
	agentName := "Reaver"

	// Seed an envoy tether file. tether.Write places it under
	// $SOL_HOME/{world}/envoys/{name}/.tether/{writ}.
	const envoyWrit = "sol-aaaaaaaaaaaaaaaa"
	const outpostWrit = "sol-bbbbbbbbbbbbbbbb"
	if err := tether.Write(world, agentName, envoyWrit, role); err != nil {
		t.Fatalf("seed envoy tether: %v", err)
	}
	if !tether.IsTethered(world, agentName, role) {
		t.Fatal("expected envoy to be tethered after Write")
	}

	// Also seed a sibling outpost tether for the same name to confirm
	// cleanupAgentResources does NOT touch it (the role-scoped fix means
	// passing role=envoy must leave the outpost tether alone).
	if err := tether.Write(world, agentName, outpostWrit, "outpost"); err != nil {
		t.Fatalf("seed outpost tether: %v", err)
	}

	w := New(cfg, sphereStore, nil, mock, nil)
	w.cleanupAgentResources(agentName, role)

	if tether.IsTethered(world, agentName, role) {
		t.Errorf("envoy tether should be cleared after cleanupAgentResources(role=envoy)")
	}
	if !tether.IsTethered(world, agentName, "outpost") {
		t.Errorf("outpost tether for sibling name was incorrectly cleared — role-scoping broken")
	}
}

// TestCleanupOrphanedEnvoyDirsRemovesOrphanedEnvoy verifies that the sentinel
// orphan sweep removes envoy directories with no matching agent record (CD-2).
// Without this sweep, an envoy.Delete that fails midway leaves the envoy
// directory and adapter config dirs on disk forever.
func TestCleanupOrphanedEnvoyDirsRemovesOrphanedEnvoy(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Do NOT create an agent record for "Wraith".
	// Create an envoy directory with worktree, persona, memory, and an
	// adapter config dir as if envoy.Delete failed mid-cleanup.
	solHome := os.Getenv("SOL_HOME")
	worldDir := filepath.Join(solHome, "ember")

	envoyDir := filepath.Join(worldDir, "envoys", "Wraith")
	if err := os.MkdirAll(filepath.Join(envoyDir, "worktree"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(envoyDir, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envoyDir, "memory", "MEMORY.md"),
		[]byte(`# stale envoy memory`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Adapter config dir at the path the claude runtime would use.
	claudeConfigDir := filepath.Join(worldDir, ".claude-config", "envoys", "Wraith")
	if err := os.MkdirAll(claudeConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const credSecret = "sk-orphan-envoy-leak-canary"
	if err := os.WriteFile(filepath.Join(claudeConfigDir, "auth.json"),
		[]byte(`{"token":"`+credSecret+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Envoy directory entirely removed.
	if _, err := os.Stat(envoyDir); !os.IsNotExist(err) {
		t.Error("expected orphaned envoy directory to be removed")
	}

	// Adapter config dir removed too — credential leak gap closed.
	if _, err := os.Stat(claudeConfigDir); !os.IsNotExist(err) {
		t.Error("expected orphaned envoy adapter config dir to be removed")
	}
}

// TestCleanupOrphanedEnvoyDirsPreservesLiveEnvoy verifies that the orphan
// sweep does NOT touch envoy directories whose agent record exists. This
// guards against accidentally reaping a live envoy.
func TestCleanupOrphanedEnvoyDirsPreservesLiveEnvoy(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an agent record for "Reaver" with role=envoy.
	if _, err := sphereStore.CreateAgent("Reaver", "ember", "envoy"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	// Create the on-disk envoy directory with a sentinel file.
	solHome := os.Getenv("SOL_HOME")
	envoyDir := filepath.Join(solHome, "ember", "envoys", "Reaver")
	if err := os.MkdirAll(filepath.Join(envoyDir, "worktree"), 0o755); err != nil {
		t.Fatal(err)
	}
	keepFile := filepath.Join(envoyDir, "memory", "MEMORY.md")
	if err := os.MkdirAll(filepath.Dir(keepFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keepFile, []byte(`# live envoy memory`), 0o644); err != nil {
		t.Fatal(err)
	}

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Live envoy's directory must survive the sweep.
	if _, err := os.Stat(keepFile); err != nil {
		t.Errorf("live envoy memory file disturbed by orphan sweep: %v", err)
	}
}

func TestCleanupOrphanedOutpostDirWithoutWorktree(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Do NOT create an agent record for "Phantom".
	// Create an outpost directory with stale files but NO worktree.
	solHome := os.Getenv("SOL_HOME")
	outpostDir := filepath.Join(solHome, "ember", "outposts", "Phantom")
	os.MkdirAll(outpostDir, 0o755)
	// Stale .resume_state.json.
	os.WriteFile(filepath.Join(outpostDir, ".resume_state.json"),
		[]byte(`{"writ":"sol-stale"}`), 0o644)
	// Empty .tether/ directory (leftover after tether clear).
	os.MkdirAll(filepath.Join(outpostDir, ".tether"), 0o755)

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Entire outpost directory should be removed.
	if _, err := os.Stat(outpostDir); !os.IsNotExist(err) {
		t.Error("expected orphaned outpost directory without worktree to be removed")
	}
}

func TestCleanupOrphanedSessionMeta(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create session metadata for an agent that doesn't exist.
	solHome := os.Getenv("SOL_HOME")
	sessDir := filepath.Join(solHome, ".runtime", "sessions")
	os.MkdirAll(sessDir, 0o755)
	os.WriteFile(filepath.Join(sessDir, "sol-ember-Ghost.json"),
		[]byte(`{"name":"sol-ember-Ghost","role":"outpost","world":"ember"}`), 0o644)
	os.WriteFile(filepath.Join(sessDir, "sol-ember-Ghost.last-capture-hash"),
		[]byte(`{"hash":"abc"}`), 0o644)

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Session metadata should be removed.
	if _, err := os.Stat(filepath.Join(sessDir, "sol-ember-Ghost.json")); !os.IsNotExist(err) {
		t.Error("expected orphaned session metadata to be removed")
	}
	if _, err := os.Stat(filepath.Join(sessDir, "sol-ember-Ghost.last-capture-hash")); !os.IsNotExist(err) {
		t.Error("expected orphaned capture hash to be removed")
	}
}

func TestCleanupOrphanedTether(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an idle agent WITH a tether (stale tether from failed cleanup).
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	// Agent is idle by default.

	if err := tether.Write("ember", "Toast", "sol-00005ea1e01e0000", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Tether should NOT be cleaned up — agent exists in DB (even though idle).
	// Consul's stale-tether recovery handles idle agents with tethers.
	if !tether.IsTethered("ember", "Toast", "outpost") {
		t.Error("expected tether to be preserved for idle agent — sentinel only cleans truly orphaned tethers")
	}
}

func TestCleanupOrphanedTetherSkipsWorking(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working agent with a live session and tether.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-ac01ae0000000000")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "working output"

	if err := tether.Write("ember", "Toast", "sol-ac01ae0000000000", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Tether should NOT be cleaned up (agent is working).
	if !tether.IsTethered("ember", "Toast", "outpost") {
		t.Error("tether for working agent should not be removed")
	}
}

// TestCleanupOrphanedTetherRaceWithCast verifies that cleanupOrphanedTethers
// skips agents that exist in the DB — regardless of state — preventing a race
// with Cast() which updates agent state before writing the tether.
func TestCleanupOrphanedTetherRaceWithCast(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an agent that starts idle (simulating the snapshot state).
	sphereStore.CreateAgent("Toast", "ember", "outpost")

	// Write a tether file (simulating Cast() writing the tether).
	if err := tether.Write("ember", "Toast", "sol-ac01ae0000000001", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	// Now update agent to "working" AFTER the initial state — simulating
	// Cast() completing the agent state update between sentinel's snapshot
	// and cleanupOrphanedTethers execution.
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-ac01ae0000000001")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "working output"

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Tether should NOT be cleaned up — agent exists in DB.
	if !tether.IsTethered("ember", "Toast", "outpost") {
		t.Error("tether for known agent should not be removed")
	}
}

// TestCleanupOrphanedTethersIdleAgentPreserved verifies that an idle agent
// with a tether directory is NOT cleaned up by sentinel. Only consul's
// stale-tether recovery handles idle agents with tethers.
func TestCleanupOrphanedTethersIdleAgentPreserved(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an idle agent with a tether file.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	// Agent stays idle — do NOT update to "working".

	if err := tether.Write("ember", "Toast", "sol-00005ea1e0000001", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Tether should NOT be cleaned up — agent exists in DB (even though idle).
	// Consul's stale-tether recovery handles this case with proper context.
	if !tether.IsTethered("ember", "Toast", "outpost") {
		t.Error("tether for idle agent should not be removed by sentinel — consul handles stale tethers")
	}
}

// TestCleanupOrphanedTethersTrulyOrphaned verifies that tether directories
// for agents with NO record in the sphere DB are cleaned up.
func TestCleanupOrphanedTethersTrulyOrphaned(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Write a tether for an agent that does NOT exist in the DB.
	if err := tether.Write("ember", "Ghost", "sol-000000000a0c0001", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	// Verify tether exists before patrol.
	if !tether.IsTethered("ember", "Ghost", "outpost") {
		t.Fatal("expected tether to exist before patrol")
	}

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Tether SHOULD be cleaned up — no agent record in DB (truly orphaned).
	if tether.IsTethered("ember", "Ghost", "outpost") {
		t.Error("tether for non-existent agent should be cleaned up")
	}
}

func TestCleanupDoesNotTouchOtherWorlds(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create session metadata for a DIFFERENT world.
	solHome := os.Getenv("SOL_HOME")
	sessDir := filepath.Join(solHome, ".runtime", "sessions")
	os.MkdirAll(sessDir, 0o755)
	otherMeta := filepath.Join(sessDir, "sol-other-Ghost.json")
	os.WriteFile(otherMeta, []byte(`{"name":"sol-other-Ghost"}`), 0o644)

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Session metadata for other world should NOT be touched.
	if _, err := os.Stat(otherMeta); os.IsNotExist(err) {
		t.Error("session metadata for other world should not be removed")
	}
}

func TestPruneOrphanedBranches(t *testing.T) {
	// Create a bare "remote" repo and a local clone to simulate real git workflows.
	tmpDir := t.TempDir()
	remoteDir := filepath.Join(tmpDir, "remote.git")
	repoDir := filepath.Join(tmpDir, "repo")

	// Helper to run git commands.
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v in %s failed: %v\n%s", args, dir, err, out)
		}
	}

	// Set up bare remote repo with an initial commit.
	git(tmpDir, "init", "--bare", remoteDir)
	git(tmpDir, "clone", remoteDir, repoDir)
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("hello"), 0o644)
	git(repoDir, "add", "file.txt")
	git(repoDir, "commit", "-m", "init")
	git(repoDir, "push", "origin", "main")

	// Create a branch, push it, then delete it on remote (simulates merged & deleted).
	git(repoDir, "checkout", "-b", "outpost/Toast/sol-aaa")
	os.WriteFile(filepath.Join(repoDir, "a.txt"), []byte("a"), 0o644)
	git(repoDir, "add", "a.txt")
	git(repoDir, "commit", "-m", "branch a")
	git(repoDir, "push", "-u", "origin", "outpost/Toast/sol-aaa")

	// Create another branch, push and delete remote.
	git(repoDir, "checkout", "-b", "outpost/Sage/sol-bbb")
	os.WriteFile(filepath.Join(repoDir, "b.txt"), []byte("b"), 0o644)
	git(repoDir, "add", "b.txt")
	git(repoDir, "commit", "-m", "branch b")
	git(repoDir, "push", "-u", "origin", "outpost/Sage/sol-bbb")

	// Create a branch that still has its remote (should NOT be pruned).
	git(repoDir, "checkout", "-b", "outpost/Ember/sol-ccc")
	os.WriteFile(filepath.Join(repoDir, "c.txt"), []byte("c"), 0o644)
	git(repoDir, "add", "c.txt")
	git(repoDir, "commit", "-m", "branch c")
	git(repoDir, "push", "-u", "origin", "outpost/Ember/sol-ccc")

	// Create a worktree branch (should be protected even if remote is gone).
	git(repoDir, "checkout", "-b", "outpost/Wren/sol-ddd")
	os.WriteFile(filepath.Join(repoDir, "d.txt"), []byte("d"), 0o644)
	git(repoDir, "add", "d.txt")
	git(repoDir, "commit", "-m", "branch d")
	git(repoDir, "push", "-u", "origin", "outpost/Wren/sol-ddd")

	// Go back to main.
	git(repoDir, "checkout", "main")

	// Create a worktree for Wren's branch.
	worktreeDir := filepath.Join(tmpDir, "worktree-wren")
	git(repoDir, "worktree", "add", worktreeDir, "outpost/Wren/sol-ddd")

	// Delete remotes for aaa, bbb, and ddd to simulate merged-and-cleaned.
	git(remoteDir, "branch", "-D", "outpost/Toast/sol-aaa")
	git(remoteDir, "branch", "-D", "outpost/Sage/sol-bbb")
	git(remoteDir, "branch", "-D", "outpost/Wren/sol-ddd")

	// Set up sentinel.
	t.Setenv("SOL_HOME", tmpDir)
	cfg := testConfig()
	cfg.SourceRepo = repoDir

	mock := newMockSessions()
	w := &Sentinel{
		config:        cfg,
		sessions:      mock,
		respawnCounts: make(map[respawnKey]int),
		lastCastTime:  make(map[string]time.Time),
		lastCaptures:  make(map[string]string),
	}

	pruned := w.pruneOrphanedBranches()

	// Should prune aaa and bbb (remote gone, no worktree).
	// Should NOT prune ccc (remote still exists).
	// Should NOT prune ddd (has active worktree despite remote gone).
	// Should NOT prune main.
	if pruned != 2 {
		t.Errorf("pruneOrphanedBranches() = %d, want 2", pruned)
	}

	// Verify which branches remain.
	out, err := exec.Command("git", "-C", repoDir, "branch", "--list").CombinedOutput()
	if err != nil {
		t.Fatalf("git branch --list failed: %v", err)
	}
	branches := string(out)

	if strings.Contains(branches, "outpost/Toast/sol-aaa") {
		t.Error("branch outpost/Toast/sol-aaa should have been pruned")
	}
	if strings.Contains(branches, "outpost/Sage/sol-bbb") {
		t.Error("branch outpost/Sage/sol-bbb should have been pruned")
	}
	if !strings.Contains(branches, "outpost/Ember/sol-ccc") {
		t.Error("branch outpost/Ember/sol-ccc should NOT have been pruned")
	}
	if !strings.Contains(branches, "outpost/Wren/sol-ddd") {
		t.Error("branch outpost/Wren/sol-ddd should NOT have been pruned (active worktree)")
	}
	if !strings.Contains(branches, "main") {
		t.Error("main branch should NOT have been pruned")
	}
}

func TestPruneOrphanedBranchesNoSourceRepo(t *testing.T) {
	cfg := testConfig()
	cfg.SourceRepo = "" // no source repo configured

	mock := newMockSessions()
	w := &Sentinel{
		config:        cfg,
		sessions:      mock,
		respawnCounts: make(map[respawnKey]int),
		lastCastTime:  make(map[string]time.Time),
		lastCaptures:  make(map[string]string),
	}

	pruned := w.pruneOrphanedBranches()
	if pruned != 0 {
		t.Errorf("pruneOrphanedBranches() = %d, want 0 (no source repo)", pruned)
	}
}

func TestOrphanedTetherDirectoryCleaned(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Write multiple tether files for an agent that does NOT exist in the DB.
	// This is a truly orphaned agent — deleted from DB but tether dir remains.
	for _, wid := range []string{"sol-000000000a0c0011", "sol-000000000a0c0022", "sol-000000000a0c0033"} {
		if err := tether.Write("ember", "Ghost", wid, "outpost"); err != nil {
			t.Fatalf("tether.Write(%s) error: %v", wid, err)
		}
	}

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// All tether files should be cleaned up — agent has no DB record (truly orphaned).
	if tether.IsTethered("ember", "Ghost", "outpost") {
		t.Error("expected all orphaned tether files to be removed for non-existent agent")
	}

	remaining, err := tether.List("ember", "Ghost", "outpost")
	if err != nil {
		t.Fatalf("tether.List() error: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 tether files remaining, got %d: %v", len(remaining), remaining)
	}
}

// TestCleanupAgentResourcesRemovesNudgeQueueDir verifies that after
// cleanupAgentResources runs, the nudge queue directory for the cleaned-up
// session no longer exists (CD-8 / M-M3 fix).
func TestCleanupAgentResourcesRemovesNudgeQueueDir(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	world := cfg.World
	agentName := "Phantom"
	sessionName := config.SessionName(world, agentName)

	// Enqueue a few nudges so the queue directory is created.
	for i := 0; i < 3; i++ {
		msg := nudge.Message{Sender: "test", Type: "info", Subject: "queued"}
		if err := nudge.Enqueue(sessionName, msg); err != nil {
			t.Fatalf("Enqueue #%d failed: %v", i, err)
		}
	}

	// Confirm the queue dir exists before cleanup.
	queueDir := config.NudgeQueueDir(sessionName)
	if _, err := os.Stat(queueDir); err != nil {
		t.Fatalf("expected nudge queue dir to exist before cleanup: %v", err)
	}

	w := New(cfg, sphereStore, nil, mock, nil)
	w.cleanupAgentResources(agentName, "outpost")

	// The nudge queue directory must be gone after cleanup.
	if _, err := os.Stat(queueDir); !os.IsNotExist(err) {
		t.Fatalf("expected nudge queue dir to be removed after cleanupAgentResources; stat returned: %v", err)
	}
}

// TestCleanupAgentResourcesNoNudgeQueueDirIsNoop verifies that
// cleanupAgentResources does not error when the nudge queue directory never
// existed (agent that never received a nudge).
func TestCleanupAgentResourcesNoNudgeQueueDirIsNoop(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	world := cfg.World
	agentName := "Ghost"
	sessionName := config.SessionName(world, agentName)

	// No nudges enqueued — queue dir was never created.
	queueDir := config.NudgeQueueDir(sessionName)
	if _, err := os.Stat(queueDir); !os.IsNotExist(err) {
		t.Fatalf("expected nudge queue dir to not exist before cleanup")
	}

	w := New(cfg, sphereStore, nil, mock, nil)
	// Must not panic or error.
	w.cleanupAgentResources(agentName, "outpost")

	// Queue dir should still not exist.
	if _, err := os.Stat(queueDir); !os.IsNotExist(err) {
		t.Fatalf("unexpected nudge queue dir after cleanupAgentResources of agent with no queue")
	}
}
