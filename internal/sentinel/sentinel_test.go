// Package sentinel_test: why are these tests slow under -race?
//
// Profiling (go test -race -cpuprofile=...) shows that ~62% of wall time is
// spent in setupTestEnv → store.OpenSphere / store.OpenWorld → schema
// migrations. The culprit is modernc.org/sqlite, a pure-Go SQLite port that
// uses modernc.org/libc (a C-to-Go translation). modernc.org/libc performs
// heavy unsafe pointer arithmetic; under -race, Go's checkptr validation
// (runtime.checkptrBase, checkptrAlignment, checkptrArithmetic) fires on
// every unsafe dereference, making each migration SQL parse 5–10× slower.
// With 96 tests each opening fresh stores and running all schema migrations,
// this cost multiplies to ~24 s out of 42 s total under -race (vs 1.5 s
// without -race).
//
// Fix: TestMain opens the SQLite stores ONCE and stores them in
// sharedSphereStore / sharedWorldStore. Each test's setupTestEnv resets all
// table rows (DELETE FROM ...) instead of reopening the database. Schema
// migrations run a single time at startup, reducing checkptr overhead from
// 96× to 1×. File-system state (tether dirs, outpost dirs, events log) stays
// isolated via per-test t.TempDir() set as SOL_HOME.

package sentinel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/jsoncontract"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// sharedSphereStore and sharedWorldStore are opened once in TestMain and
// reused across all tests. Each test resets their contents via
// resetSharedStores instead of reopening (which would re-run migrations).
var (
	sharedSphereStore *store.SphereStore
	sharedWorldStore  *store.WorldStore
)

// TestMain opens the shared SQLite stores once for the entire test binary run
// to avoid repeating schema migrations (and their checkptr overhead) for each
// of the ~96 tests. Each test still gets its own SOL_HOME temp directory for
// file-system isolation.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sentinel-shared-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: MkdirTemp: %v\n", err)
		os.Exit(1)
	}

	// Point SOL_HOME at the shared dir so store.OpenSphere/OpenWorld can
	// derive the correct paths. Individual tests redirect SOL_HOME to their
	// own temp dir via t.Setenv; the shared DB files remain open by fd.
	if err := os.Setenv("SOL_HOME", dir); err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: Setenv SOL_HOME: %v\n", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: MkdirAll .store: %v\n", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: MkdirAll .runtime: %v\n", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}

	sharedSphereStore, err = store.OpenSphere()
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: OpenSphere: %v\n", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	sharedWorldStore, err = store.OpenWorld("ember")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: OpenWorld: %v\n", err)
		sharedSphereStore.Close()
		os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()

	// Cleanup: close before RemoveAll so WAL files are flushed.
	sharedSphereStore.Close()
	sharedWorldStore.Close()
	os.RemoveAll(dir)
	os.Exit(code)
}

// --- Mock implementations ---

type mockSessions struct {
	mu       sync.Mutex
	alive    map[string]bool
	captures map[string]string // session name → captured output
	started  []string
	stopped  []string
	cycled   []string
	injected []injectCall
	lastCmds map[string]string // session name → last command used in Start/Cycle
}

type injectCall struct {
	Session string
	Text    string
}

func newMockSessions() *mockSessions {
	return &mockSessions{
		alive:    make(map[string]bool),
		captures: make(map[string]string),
		lastCmds: make(map[string]string),
	}
}

func (m *mockSessions) Exists(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.alive[name]
}

func (m *mockSessions) Capture(name string, lines int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if output, ok := m.captures[name]; ok {
		return output, nil
	}
	return "", fmt.Errorf("session %q not found", name)
}

func (m *mockSessions) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alive[name] = true
	m.started = append(m.started, name)
	m.lastCmds[name] = cmd
	return nil
}

func (m *mockSessions) Stop(name string, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.alive, name)
	m.stopped = append(m.stopped, name)
	return nil
}

func (m *mockSessions) Cycle(name, workdir, cmd string, env map[string]string, role, world string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cycled = append(m.cycled, name)
	m.lastCmds[name] = cmd
	return nil
}

func (m *mockSessions) Inject(name string, text string, submit bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.injected = append(m.injected, injectCall{Session: name, Text: text})
	return nil
}

func (m *mockSessions) getStarted() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.started))
	copy(result, m.started)
	return result
}

func (m *mockSessions) getStopped() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.stopped))
	copy(result, m.stopped)
	return result
}

func (m *mockSessions) getCycled() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.cycled))
	copy(result, m.cycled)
	return result
}

func (m *mockSessions) getInjected() []injectCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]injectCall, len(m.injected))
	copy(result, m.injected)
	return result
}

func (m *mockSessions) getLastCmd(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastCmds[name]
}

// --- Test helpers ---

func setupTestEnv(t *testing.T) (*store.SphereStore, *store.WorldStore) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a fake token so startup.Launch can inject credentials.
	jsoncontract.WriteTestToken(t, dir)

	// Reset shared stores so this test starts with empty tables.
	// Schema migrations already ran once in TestMain — see package comment.
	resetSharedStores(t)

	return sharedSphereStore, sharedWorldStore
}

// resetSharedStores deletes all user-data rows from both shared SQLite stores.
// Tables are cleared inside a single transaction per store so the WAL commit
// (and its fsync) happens once instead of once per table. Tables are deleted
// in FK-safe order (children before parents) because the connection uses
// foreign_keys=ON.
func resetSharedStores(t *testing.T) {
	t.Helper()

	// World (ember.db): delete dependents before writs.
	//   token_usage → agent_history (FK: history_id)
	//   labels, dependencies, merge_requests → writs (FK: writ_id)
	worldTx, err := sharedWorldStore.DB().Begin()
	if err != nil {
		t.Fatalf("resetSharedStores: begin world tx: %v", err)
	}
	defer worldTx.Rollback() //nolint:errcheck
	for _, tbl := range []string{
		"token_usage",
		"labels",
		"dependencies",
		"merge_requests",
		"agent_history",
		"writs",
	} {
		if _, err := worldTx.Exec("DELETE FROM " + tbl); err != nil {
			t.Fatalf("resetSharedStores: clear world table %q: %v", tbl, err)
		}
	}
	if err := worldTx.Commit(); err != nil {
		t.Fatalf("resetSharedStores: commit world tx: %v", err)
	}

	// Sphere (sphere.db): delete dependents before their parents.
	//   caravan_items, caravan_dependencies → caravans
	sphereTx, err := sharedSphereStore.DB().Begin()
	if err != nil {
		t.Fatalf("resetSharedStores: begin sphere tx: %v", err)
	}
	defer sphereTx.Rollback() //nolint:errcheck
	for _, tbl := range []string{
		"caravan_items",
		"caravan_dependencies",
		"caravans",
		"messages",
		"escalations",
		"agents",
		"worlds",
	} {
		if _, err := sphereTx.Exec("DELETE FROM " + tbl); err != nil {
			t.Fatalf("resetSharedStores: clear sphere table %q: %v", tbl, err)
		}
	}
	if err := sphereTx.Commit(); err != nil {
		t.Fatalf("resetSharedStores: commit sphere tx: %v", err)
	}
}

func testConfig() Config {
	return Config{
		World:          "ember",
		PatrolInterval: 50 * time.Millisecond, // Fast for tests.
		MaxRespawns:    2,
		CaptureLines:   80,
		AssessCommand:  "claude -p",
		SolHome:        os.Getenv("SOL_HOME"),
	}
}

func createWrit(t *testing.T, worldStore *store.WorldStore, id, title string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := worldStore.DB().Exec(
		`INSERT INTO writs (id, title, description, status, priority, created_by, created_at, updated_at)
		 VALUES (?, ?, '', 'open', 3, 'test', ?, ?)`,
		id, title, now, now,
	)
	if err != nil {
		t.Fatalf("failed to create writ %q: %v", id, err)
	}
}

// --- Tests ---

func TestRegisterAgent(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.Register(); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	agent, err := sphereStore.GetAgent("ember/sentinel")
	if err != nil {
		t.Fatalf("GetAgent() error: %v", err)
	}
	if agent.Role != "sentinel" {
		t.Errorf("agent role = %q, want %q", agent.Role, "sentinel")
	}
	if agent.State != store.AgentIdle {
		t.Errorf("agent state = %q, want %q", agent.State, store.AgentIdle)
	}
}

func TestRegisterAgentIdempotent(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.Register(); err != nil {
		t.Fatalf("Register() first call error: %v", err)
	}
	if err := w.Register(); err != nil {
		t.Fatalf("Register() second call error: %v", err)
	}

	// Should still be the same agent.
	agent, err := sphereStore.GetAgent("ember/sentinel")
	if err != nil {
		t.Fatalf("GetAgent() error: %v", err)
	}
	if agent.Role != "sentinel" {
		t.Errorf("agent role = %q, want %q", agent.Role, "sentinel")
	}
}

func TestRunLifecycle(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.PatrolInterval = 100 * time.Millisecond

	w := New(cfg, sphereStore, worldStore, mock, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	// Poll until the sentinel registers and marks itself working.
	// Run() calls Register() then UpdateAgentState("working") synchronously
	// before the first patrol, so this normally completes in milliseconds.
	var (
		agent *store.Agent
		err   error
	)
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		agent, err = sphereStore.GetAgent("ember/sentinel")
		if err == nil && agent.State == store.AgentWorking {
			break
		}
	}
	if err != nil {
		t.Fatalf("GetAgent() error: %v", err)
	}
	if agent.State != store.AgentWorking {
		t.Errorf("agent state during run = %q, want %q", agent.State, store.AgentWorking)
	}

	// Wait for context to expire.
	if err := <-done; err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Agent should be idle after shutdown.
	agent, err = sphereStore.GetAgent("ember/sentinel")
	if err != nil {
		t.Fatalf("GetAgent() after run: %v", err)
	}
	if agent.State != store.AgentIdle {
		t.Errorf("agent state after run = %q, want %q", agent.State, store.AgentIdle)
	}
}

func TestPatrolHealthyAgents(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create 3 working agents with live sessions and changing output.
	for _, name := range []string{"Toast", "Jasper", "Sage"} {
		sphereStore.CreateAgent(name, "ember", "outpost")
		sphereStore.UpdateAgentState("ember/"+name, store.AgentWorking, "sol-"+name)
		sessName := "sol-ember-" + name
		mock.alive[sessName] = true
		mock.captures[sessName] = "output for " + name
	}

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No sessions should have been started or stopped.
	if started := mock.getStarted(); len(started) != 0 {
		t.Errorf("expected 0 sessions started, got %d: %v", len(started), started)
	}
	if stopped := mock.getStopped(); len(stopped) != 0 {
		t.Errorf("expected 0 sessions stopped, got %d: %v", len(stopped), stopped)
	}
}

func TestPatrolSkipsWhenWorldSleeping(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Put the world to sleep.
	worldCfg := config.DefaultWorldConfig()
	worldCfg.World.Sleeping = true
	if err := config.WriteWorldConfig("ember", worldCfg); err != nil {
		t.Fatalf("WriteWorldConfig() error: %v", err)
	}

	// Create a stalled agent (working state, no live session, tethered) —
	// this would normally trigger a respawn.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	createWrit(t, worldStore, "sol-abc1234500000000", "Test task")
	if err := tether.Write("ember", "Toast", "sol-abc1234500000000", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No sessions should have been started — patrol must skip agent work while sleeping.
	if started := mock.getStarted(); len(started) != 0 {
		t.Errorf("expected 0 sessions started (world sleeping), got %d: %v", len(started), started)
	}

	// Heartbeat should have been written so prefect does not restart the sentinel.
	hb, err := ReadHeartbeat("ember")
	if err != nil {
		t.Fatalf("ReadHeartbeat() error: %v", err)
	}
	if hb == nil {
		t.Fatal("expected heartbeat to be written, got nil")
	}
}

func TestPatrolIgnoresIdleClean(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an idle agent with no session and no tether.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	// State is idle (default), no session, no tether.

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No sessions started or stopped.
	if started := mock.getStarted(); len(started) != 0 {
		t.Errorf("expected 0 sessions started, got %d", len(started))
	}
	if stopped := mock.getStopped(); len(stopped) != 0 {
		t.Errorf("expected 0 sessions stopped, got %d", len(stopped))
	}
}

func TestPatrolIgnoresNonMonitored(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create agents with non-monitored roles (sentinel is excluded by filter).
	sphereStore.CreateAgent("sentinel", "ember", "sentinel")

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No actions taken.
	if started := mock.getStarted(); len(started) != 0 {
		t.Errorf("expected 0 sessions started, got %d", len(started))
	}
	if stopped := mock.getStopped(); len(stopped) != 0 {
		t.Errorf("expected 0 sessions stopped, got %d", len(stopped))
	}
}

func TestSentinelIgnoresEnvoy(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an envoy agent with a dead session.
	sphereStore.CreateAgent("Scout", "ember", "envoy")
	sphereStore.UpdateAgentState("ember/Scout", store.AgentWorking, "sol-envoy123")
	// Session is NOT alive.

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No sessions should have been started or stopped.
	if started := mock.getStarted(); len(started) != 0 {
		t.Errorf("expected 0 sessions started for envoy, got %d: %v", len(started), started)
	}
	if stopped := mock.getStopped(); len(stopped) != 0 {
		t.Errorf("expected 0 sessions stopped for envoy, got %d: %v", len(stopped), stopped)
	}
}

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

func TestPatrolIgnoresForgeAgents(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working forge agent with a dead session.
	// Sentinel should NOT monitor it (prefect handles forge via heartbeat).
	sphereStore.CreateAgent("forge", "ember", "forge")
	sphereStore.UpdateAgentState("ember/forge", store.AgentWorking, "")
	// Session is NOT alive — sentinel should not attempt respawn.

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No sessions should have been started (forge is not sentinel's responsibility).
	started := mock.getStarted()
	if len(started) != 0 {
		t.Fatalf("expected 0 sessions started (forge not monitored by sentinel), got %d: %v", len(started), started)
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

// --- Escalation creation tests ---

// readEvents reads all events from the logger's event file and returns those
// matching the given event type.
func readEvents(t *testing.T, solHome, eventType string) []events.Event {
	t.Helper()
	path := filepath.Join(solHome, ".events.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("failed to read events file: %v", err)
	}
	var matched []events.Event
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var ev events.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Type == eventType {
			matched = append(matched, ev)
		}
	}
	return matched
}

// --- Error path tests ---

// listAgentsErrorSphere wraps a real SphereStore and forces ListAgents to
// return a configured error. All other methods delegate to the embedded store.
// Used to simulate persistent infrastructure failures (e.g. sphere DB locked)
// inside patrol so we can assert the run loop surfaces the error.
type listAgentsErrorSphere struct {
	*store.SphereStore
	err error
}

func (s *listAgentsErrorSphere) ListAgents(world, state string) ([]store.Agent, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.SphereStore.ListAgents(world, state)
}

// TestRunPatrolLoggedSurfacesError verifies that when patrol returns an error,
// the run-loop wrapper (used at both the initial-patrol and per-tick call
// sites) emits a sentinel_error event with action="patrol" so persistent
// infrastructure failures are observable. Regression test for CWE-391
// (Unchecked Error Condition) — see sol-974d39599fe86c88.
func TestRunPatrolLoggedSurfacesError(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	logger := events.NewLogger(cfg.SolHome)
	failing := &listAgentsErrorSphere{
		SphereStore: sphereStore,
		err:         fmt.Errorf("sphere db is locked"),
	}

	w := New(cfg, failing, worldStore, mock, logger)

	// Drive a single patrol — the wrapper must catch the error and emit it
	// rather than discarding it (the prior bug at sentinel.go:372 / :389).
	w.runPatrolLogged(context.Background())

	evts := readEvents(t, cfg.SolHome, "sentinel_error")
	if len(evts) == 0 {
		t.Fatal("expected sentinel_error event from failed patrol, got none")
	}

	var found bool
	for _, ev := range evts {
		payload, ok := ev.Payload.(map[string]any)
		if !ok {
			continue
		}
		if payload["action"] != "patrol" {
			continue
		}
		errMsg, _ := payload["error"].(string)
		if !strings.Contains(errMsg, "sphere db is locked") {
			t.Errorf("event error = %q, want it to contain %q", errMsg, "sphere db is locked")
		}
		found = true
		break
	}
	if !found {
		t.Errorf("expected sentinel_error with action=%q, got events: %+v", "patrol", evts)
	}
}

// TestRunPatrolLoggedSuccessEmitsNoError verifies that the successful patrol
// path is unchanged: no sentinel_error event is emitted when patrol returns
// nil. Guards against regressions where the wrapper accidentally logs spurious
// errors on the happy path.
func TestRunPatrolLoggedSuccessEmitsNoError(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	logger := events.NewLogger(cfg.SolHome)
	w := New(cfg, sphereStore, worldStore, mock, logger)

	// Patrol with no agents and no failure injection — should return nil.
	w.runPatrolLogged(context.Background())

	evts := readEvents(t, cfg.SolHome, "sentinel_error")
	for _, ev := range evts {
		payload, ok := ev.Payload.(map[string]any)
		if !ok {
			continue
		}
		if payload["action"] == "patrol" {
			t.Errorf("unexpected sentinel_error with action=patrol on success path: %+v", ev)
		}
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
