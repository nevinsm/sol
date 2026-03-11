package prefect

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/sentinel"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
)

// mockSessions implements SessionManager for testing.
type mockSessions struct {
	mu      sync.Mutex
	alive   map[string]bool
	started []string
	stopped []string
	lastEnv map[string]string // env from the most recent Start call
}

func newMockSessions() *mockSessions {
	return &mockSessions{alive: make(map[string]bool)}
}

func (m *mockSessions) Exists(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.alive[name]
}

func (m *mockSessions) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alive[name] = true
	m.started = append(m.started, name)
	m.lastEnv = env
	return nil
}

func (m *mockSessions) Stop(name string, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.alive, name)
	m.stopped = append(m.stopped, name)
	return nil
}

func (m *mockSessions) List() ([]session.SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var infos []session.SessionInfo
	for name, alive := range m.alive {
		infos = append(infos, session.SessionInfo{Name: name, Alive: alive})
	}
	return infos, nil
}

func (m *mockSessions) Kill(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.alive, name)
}

func (m *mockSessions) GetStarted() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.started))
	copy(result, m.started)
	return result
}

func (m *mockSessions) GetStopped() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.stopped))
	copy(result, m.stopped)
	return result
}

// setupTestEnv creates a test SOL_HOME with a sphere DB and returns the store and cleanup function.
func setupTestEnv(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Register common roles so startup.Respawn succeeds in respawn tests.
	startup.Register("outpost", startup.RoleConfig{
		WorktreeDir: func(w, a string) string {
			return filepath.Join(os.Getenv("SOL_HOME"), w, "outposts", a, "worktree")
		},
	})
	startup.Register("forge", startup.RoleConfig{
		WorktreeDir: func(w, a string) string {
			return filepath.Join(os.Getenv("SOL_HOME"), w, "forge", "worktree")
		},
	})
	t.Cleanup(func() {
		startup.Register("outpost", startup.RoleConfig{})
		startup.Register("forge", startup.RoleConfig{})
	})

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	t.Cleanup(func() { sphereStore.Close() })
	return sphereStore
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func testConfig() Config {
	return Config{
		HeartbeatInterval:  50 * time.Millisecond, // Fast for tests.
		MassDeathThreshold: 3,
		MassDeathWindow:    30 * time.Second,
		DegradedCooldown:   5 * time.Minute,
		ForgeHeartbeatMax:  5 * time.Minute,
	}
}

func TestRunRejectsZeroHeartbeatInterval(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()

	// Zero interval should be rejected.
	cfg := testConfig()
	cfg.HeartbeatInterval = 0
	sup := New(cfg, sphereStore, mock, logger)

	ctx := context.Background()
	err := sup.Run(ctx)
	if err == nil {
		t.Fatal("expected error for zero HeartbeatInterval, got nil")
	}
	if !strings.Contains(err.Error(), "invalid heartbeat interval") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "invalid heartbeat interval")
	}

	// Negative interval should also be rejected.
	cfg.HeartbeatInterval = -1 * time.Second
	sup = New(cfg, sphereStore, mock, logger)

	err = sup.Run(ctx)
	if err == nil {
		t.Fatal("expected error for negative HeartbeatInterval, got nil")
	}
	if !strings.Contains(err.Error(), "invalid heartbeat interval") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "invalid heartbeat interval")
	}
}

func TestHeartbeatDetectsDead(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create a working agent with a worktree.
	sphereStore.CreateAgent("Toast", "haven", "outpost")
	sphereStore.UpdateAgentState("haven/Toast", "working", "sol-abc12345")

	// Create the worktree directory so respawn doesn't bail.
	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "haven", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	// Session is dead (not in mock).
	sup := New(cfg, sphereStore, mock, logger)

	// Run one heartbeat.
	sup.heartbeat()

	// Should have started a session.
	started := mock.GetStarted()
	if len(started) != 1 {
		t.Fatalf("expected 1 session started, got %d", len(started))
	}
	if started[0] != "sol-haven-Toast" {
		t.Errorf("started session = %q, want %q", started[0], "sol-haven-Toast")
	}

	// Agent should be back to working.
	agent, err := sphereStore.GetAgent("haven/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "working" {
		t.Errorf("agent state = %q, want %q", agent.State, "working")
	}
}

func TestHeartbeatIgnoresIdle(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create an idle agent.
	sphereStore.CreateAgent("Jasper", "haven", "outpost")

	sup := New(cfg, sphereStore, mock, logger)
	sup.heartbeat()

	// No sessions should be started.
	started := mock.GetStarted()
	if len(started) != 0 {
		t.Fatalf("expected 0 sessions started for idle agent, got %d", len(started))
	}
}

func TestHeartbeatMultipleWorlds(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create working agents in different worlds.
	sphereStore.CreateAgent("Toast", "alpha", "outpost")
	sphereStore.UpdateAgentState("alpha/Toast", "working", "sol-aaa11111")
	sphereStore.CreateAgent("Jasper", "beta", "outpost")
	sphereStore.UpdateAgentState("beta/Jasper", "working", "sol-bbb22222")

	// Create worktree directories.
	for _, p := range []string{
		filepath.Join(os.Getenv("SOL_HOME"), "alpha", "outposts", "Toast", "worktree"),
		filepath.Join(os.Getenv("SOL_HOME"), "beta", "outposts", "Jasper", "worktree"),
	} {
		os.MkdirAll(p, 0o755)
	}

	sup := New(cfg, sphereStore, mock, logger)
	sup.heartbeat()

	// Both should be restarted.
	started := mock.GetStarted()
	if len(started) != 2 {
		t.Fatalf("expected 2 sessions started across worlds, got %d: %v", len(started), started)
	}
}

func TestBackoffEscalation(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "haven", "outpost")
	sphereStore.UpdateAgentState("haven/Toast", "working", "sol-abc12345")

	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "haven", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sup := New(cfg, sphereStore, mock, logger)

	// First heartbeat: immediate respawn (restart 1, delay 0).
	sup.heartbeat()
	started := mock.GetStarted()
	if len(started) != 1 {
		t.Fatalf("restart 1: expected 1 start, got %d", len(started))
	}

	// Kill the session again.
	mock.Kill("sol-haven-Toast")

	// Second heartbeat: restart 2, delay 30s — should stall, not restart.
	sup.heartbeat()
	started = mock.GetStarted()
	if len(started) != 1 {
		t.Fatalf("restart 2: expected still 1 start (deferred), got %d", len(started))
	}

	// Verify agent is stalled.
	agent, err := sphereStore.GetAgent("haven/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "stalled" {
		t.Errorf("agent state = %q, want %q after deferred respawn", agent.State, "stalled")
	}
}

func TestMassDeathDetection(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.MassDeathThreshold = 3
	cfg.MassDeathWindow = 30 * time.Second

	// Create 3 working agents.
	for _, name := range []string{"Toast", "Jasper", "Olive"} {
		sphereStore.CreateAgent(name, "haven", "outpost")
		sphereStore.UpdateAgentState("haven/"+name, "working", "sol-"+name)
		worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "haven", "outposts", name, "worktree")
		os.MkdirAll(worktreeDir, 0o755)
	}

	sup := New(cfg, sphereStore, mock, logger)

	// First heartbeat detects 3 deaths -> mass death -> degraded.
	sup.heartbeat()

	if !sup.IsDegraded() {
		t.Fatal("prefect should be in degraded mode after 3 deaths")
	}
}

func TestMassDeathRecovery(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.MassDeathThreshold = 3
	cfg.MassDeathWindow = 30 * time.Second
	cfg.DegradedCooldown = 10 * time.Millisecond // Very short for testing.

	sup := New(cfg, sphereStore, mock, logger)

	// Manually enter degraded mode with old death times.
	sup.mu.Lock()
	sup.degraded = true
	sup.degradedSince = time.Now().Add(-time.Minute)
	sup.deathTimes = []time.Time{
		time.Now().Add(-time.Minute), // Old death.
	}
	sup.mu.Unlock()

	// Wait for cooldown to pass.
	time.Sleep(20 * time.Millisecond)

	// Heartbeat should recover from degraded.
	sup.heartbeat()

	if sup.IsDegraded() {
		t.Fatal("prefect should have exited degraded mode after cooldown")
	}
}

func TestDegradedModeSkipsRespawn(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "haven", "outpost")
	sphereStore.UpdateAgentState("haven/Toast", "working", "sol-abc12345")

	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "haven", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sup := New(cfg, sphereStore, mock, logger)

	// Enter degraded mode.
	sup.mu.Lock()
	sup.degraded = true
	sup.degradedSince = time.Now()
	// Add recent deaths to prevent recovery.
	sup.deathTimes = []time.Time{time.Now()}
	sup.mu.Unlock()

	// Heartbeat should NOT respawn.
	sup.heartbeat()

	started := mock.GetStarted()
	if len(started) != 0 {
		t.Fatalf("expected 0 starts in degraded mode, got %d", len(started))
	}

	// Agent should be stalled.
	agent, err := sphereStore.GetAgent("haven/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "stalled" {
		t.Errorf("agent state = %q, want %q in degraded mode", agent.State, "stalled")
	}
}

func TestShutdownStopsSessions(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create working agents with live sessions.
	for _, name := range []string{"Toast", "Jasper"} {
		sphereStore.CreateAgent(name, "haven", "outpost")
		sphereStore.UpdateAgentState("haven/"+name, "working", "sol-"+name)
		mock.Start("sol-haven-"+name, "/tmp", "echo", nil, "outpost", "haven")
	}

	sup := New(cfg, sphereStore, mock, logger)
	sup.shutdown()

	// Both sessions should be stopped.
	stopped := mock.GetStopped()
	if len(stopped) != 2 {
		t.Fatalf("expected 2 sessions stopped, got %d: %v", len(stopped), stopped)
	}

	// Agents should be stalled.
	for _, name := range []string{"Toast", "Jasper"} {
		agent, err := sphereStore.GetAgent("haven/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if agent.State != "stalled" {
			t.Errorf("agent %s state = %q, want %q", name, agent.State, "stalled")
		}
	}
}

func TestBackoffReset(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "haven", "outpost")
	sphereStore.UpdateAgentState("haven/Toast", "working", "sol-abc12345")

	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "haven", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sup := New(cfg, sphereStore, mock, logger)

	// First heartbeat: respawn (backoff count = 1).
	sup.heartbeat()
	if sup.backoff["haven/Toast"] != 1 {
		t.Fatalf("backoff count = %d, want 1", sup.backoff["haven/Toast"])
	}

	// Set agent to idle (simulating sol done).
	sphereStore.UpdateAgentState("haven/Toast", "idle", "")

	// Next heartbeat should reset backoff.
	sup.heartbeat()
	if count, ok := sup.backoff["haven/Toast"]; ok {
		t.Fatalf("backoff should be cleared for idle agent, got count %d", count)
	}
}

func TestBackoffDuration(t *testing.T) {
	cases := []struct {
		restarts int
		want     time.Duration
	}{
		{1, 0},
		{2, 30 * time.Second},
		{3, 1 * time.Minute},
		{4, 2 * time.Minute},
		{5, 5 * time.Minute},
		{6, 5 * time.Minute},
		{100, 5 * time.Minute},
	}

	for _, tc := range cases {
		got := backoffDuration(tc.restarts)
		if got != tc.want {
			t.Errorf("backoffDuration(%d) = %v, want %v", tc.restarts, got, tc.want)
		}
	}
}

func TestRunWritesAndClearsPID(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.HeartbeatInterval = 100 * time.Millisecond

	sup := New(cfg, sphereStore, mock, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Run in goroutine.
	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx) }()

	// Wait a bit and check PID file exists.
	time.Sleep(50 * time.Millisecond)
	pid, err := ReadPID()
	if err != nil {
		t.Fatalf("ReadPID() during run: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("PID = %d, want %d", pid, os.Getpid())
	}

	// Wait for context to expire.
	if err := <-done; err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// PID file should be cleared.
	pid, err = ReadPID()
	if err != nil {
		t.Fatalf("ReadPID() after run: %v", err)
	}
	if pid != 0 {
		t.Errorf("PID after run = %d, want 0", pid)
	}
}

func TestRespawnMissingWorktree(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	sphereStore.CreateAgent("Ghost", "haven", "outpost")
	sphereStore.UpdateAgentState("haven/Ghost", "working", "sol-ghost123")

	// Do NOT create worktree directory.

	sup := New(cfg, sphereStore, mock, logger)
	sup.heartbeat()

	// Should NOT have started a session.
	started := mock.GetStarted()
	if len(started) != 0 {
		t.Fatalf("expected 0 starts for missing worktree, got %d", len(started))
	}

	// Agent should be idle.
	agent, err := sphereStore.GetAgent("haven/Ghost")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "idle" {
		t.Errorf("agent state = %q, want %q for missing worktree", agent.State, "idle")
	}
}

func TestRespawnForge(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create a forge agent in working state.
	sphereStore.CreateAgent("forge", "haven", "forge")
	sphereStore.UpdateAgentState("haven/forge", "working", "")

	// Create the forge worktree directory.
	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "haven", "forge", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	// Session is dead (not in mock).
	sup := New(cfg, sphereStore, mock, logger)
	sup.heartbeat()

	// Should have started a session.
	started := mock.GetStarted()
	if len(started) != 1 {
		t.Fatalf("expected 1 session started, got %d", len(started))
	}
	if started[0] != "sol-haven-forge" {
		t.Errorf("started session = %q, want %q", started[0], "sol-haven-forge")
	}

	// Verify the session was started with the right command by checking agent is working.
	agent, err := sphereStore.GetAgent("haven/forge")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "working" {
		t.Errorf("agent state = %q, want %q", agent.State, "working")
	}
}

func TestRespawnOutpostUnchanged(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create an agent in working state.
	sphereStore.CreateAgent("Toast", "haven", "outpost")
	sphereStore.UpdateAgentState("haven/Toast", "working", "sol-abc12345")

	// Create the agent worktree directory.
	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "haven", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sup := New(cfg, sphereStore, mock, logger)
	sup.heartbeat()

	// Should have started with agent session name.
	started := mock.GetStarted()
	if len(started) != 1 {
		t.Fatalf("expected 1 session started, got %d", len(started))
	}
	if started[0] != "sol-haven-Toast" {
		t.Errorf("started session = %q, want %q", started[0], "sol-haven-Toast")
	}

	agent, err := sphereStore.GetAgent("haven/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "working" {
		t.Errorf("agent state = %q, want %q", agent.State, "working")
	}
}

func TestWorktreeForAgentByRole(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	forgeAgent := store.Agent{Name: "forge", World: "haven", Role: "forge"}
	agentBot := store.Agent{Name: "Toast", World: "haven", Role: "outpost"}
	sentinelAgent := store.Agent{Name: "sentinel", World: "haven", Role: "sentinel"}

	forgePath := worktreeForAgent(forgeAgent)
	expected := filepath.Join(dir, "haven", "forge", "worktree")
	if forgePath != expected {
		t.Errorf("forge worktree = %q, want %q", forgePath, expected)
	}

	agentPath := worktreeForAgent(agentBot)
	expected = filepath.Join(dir, "haven", "outposts", "Toast", "worktree")
	if agentPath != expected {
		t.Errorf("agent worktree = %q, want %q", agentPath, expected)
	}

	// Sentinel uses SOL_HOME as working directory.
	sentPath := worktreeForAgent(sentinelAgent)
	if sentPath != dir {
		t.Errorf("sentinel worktree = %q, want %q", sentPath, dir)
	}
}

func TestHeartbeatDefersToSentinel(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create a sentinel agent in working state with a live PID.
	sphereStore.CreateAgent("sentinel", "haven", "sentinel")
	sphereStore.UpdateAgentState("haven/sentinel", "working", "")
	// Write sentinel PID file pointing to our own PID (alive process).
	if err := sentinel.WritePID("haven", os.Getpid()); err != nil {
		t.Fatalf("failed to write sentinel PID: %v", err)
	}

	// Create a working agent with a dead session.
	sphereStore.CreateAgent("Toast", "haven", "outpost")
	sphereStore.UpdateAgentState("haven/Toast", "working", "sol-abc12345")
	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "haven", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)
	// Session is dead (not started in mock for this agent).

	sup := New(cfg, sphereStore, mock, logger)
	sup.heartbeat()

	// The agent should NOT have been respawned by the prefect
	// because the world is sentineled.
	started := mock.GetStarted()
	for _, s := range started {
		if s == "sol-haven-Toast" {
			t.Error("prefect should not respawn agents in sentineled worlds")
		}
	}
}

func TestHeartbeatRespondsWithoutSentinel(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create a sentinel agent that is NOT active (state=idle).
	sphereStore.CreateAgent("sentinel", "haven", "sentinel")
	// State is idle (default).

	// Create a working agent with a dead session.
	sphereStore.CreateAgent("Toast", "haven", "outpost")
	sphereStore.UpdateAgentState("haven/Toast", "working", "sol-abc12345")
	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "haven", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sup := New(cfg, sphereStore, mock, logger)
	sup.heartbeat()

	// Without a working sentinel, the prefect should respawn the agent.
	started := mock.GetStarted()
	found := false
	for _, s := range started {
		if s == "sol-haven-Toast" {
			found = true
		}
	}
	if !found {
		t.Error("prefect should respawn agents in unsentineled worlds")
	}
}

func TestHeartbeatSkipsEnvoy(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create an envoy agent in working state with a dead session.
	sphereStore.CreateAgent("Scout", "haven", "envoy")
	sphereStore.UpdateAgentState("haven/Scout", "working", "sol-envoy123")

	// Create worktree directory (should not matter — should be skipped).
	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "haven", "envoys", "Scout", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sup := New(cfg, sphereStore, mock, logger)
	sup.heartbeat()

	// Should NOT have started any sessions.
	started := mock.GetStarted()
	if len(started) != 0 {
		t.Fatalf("expected 0 sessions started for envoy, got %d: %v", len(started), started)
	}
}

func TestHeartbeatSkipsGovernor(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create a governor agent in working state with a dead session.
	sphereStore.CreateAgent("governor", "haven", "governor")
	sphereStore.UpdateAgentState("haven/governor", "working", "")

	// Create governor directory (should not matter — should be skipped).
	govDir := filepath.Join(os.Getenv("SOL_HOME"), "haven", "governor")
	os.MkdirAll(govDir, 0o755)

	sup := New(cfg, sphereStore, mock, logger)
	sup.heartbeat()

	// Should NOT have started any sessions.
	started := mock.GetStarted()
	if len(started) != 0 {
		t.Fatalf("expected 0 sessions started for governor, got %d: %v", len(started), started)
	}
}


func TestShutdownSkipsEnvoyGovernor(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create working agents: one regular, one envoy, one governor — all with live sessions.
	sphereStore.CreateAgent("Toast", "haven", "outpost")
	sphereStore.UpdateAgentState("haven/Toast", "working", "sol-abc12345")
	mock.Start("sol-haven-Toast", "/tmp", "echo", nil, "outpost", "haven")

	sphereStore.CreateAgent("Scout", "haven", "envoy")
	sphereStore.UpdateAgentState("haven/Scout", "working", "sol-envoy123")
	mock.Start("sol-haven-Scout", "/tmp", "echo", nil, "envoy", "haven")

	sphereStore.CreateAgent("governor", "haven", "governor")
	sphereStore.UpdateAgentState("haven/governor", "working", "")
	mock.Start("sol-haven-governor", "/tmp", "echo", nil, "governor", "haven")

	sup := New(cfg, sphereStore, mock, logger)
	sup.shutdown()

	// Only the regular agent's session should be stopped.
	stopped := mock.GetStopped()
	if len(stopped) != 1 {
		t.Fatalf("expected 1 session stopped, got %d: %v", len(stopped), stopped)
	}
	if stopped[0] != "sol-haven-Toast" {
		t.Errorf("stopped session = %q, want %q", stopped[0], "sol-haven-Toast")
	}

	// Envoy and governor sessions should still be alive.
	if !mock.Exists("sol-haven-Scout") {
		t.Error("envoy session should not be stopped by shutdown")
	}
	if !mock.Exists("sol-haven-governor") {
		t.Error("governor session should not be stopped by shutdown")
	}
}

func TestHeartbeatWorldsFilter(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.Worlds = []string{"alpha"}

	// Create working agents in two worlds.
	sphereStore.CreateAgent("Toast", "alpha", "outpost")
	sphereStore.UpdateAgentState("alpha/Toast", "working", "sol-aaa11111")
	sphereStore.CreateAgent("Jasper", "beta", "outpost")
	sphereStore.UpdateAgentState("beta/Jasper", "working", "sol-bbb22222")

	// Create worktree directories.
	for _, p := range []string{
		filepath.Join(os.Getenv("SOL_HOME"), "alpha", "outposts", "Toast", "worktree"),
		filepath.Join(os.Getenv("SOL_HOME"), "beta", "outposts", "Jasper", "worktree"),
	} {
		os.MkdirAll(p, 0o755)
	}

	sup := New(cfg, sphereStore, mock, logger)
	sup.heartbeat()

	// Only alpha agent should be restarted.
	started := mock.GetStarted()
	if len(started) != 1 {
		t.Fatalf("expected 1 session started with worlds filter, got %d: %v", len(started), started)
	}
	if started[0] != "sol-alpha-Toast" {
		t.Errorf("started session = %q, want %q", started[0], "sol-alpha-Toast")
	}
}

func TestShutdownWorldsFilter(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.Worlds = []string{"alpha"}

	// Create working agents with live sessions in two worlds.
	sphereStore.CreateAgent("Toast", "alpha", "outpost")
	sphereStore.UpdateAgentState("alpha/Toast", "working", "sol-aaa11111")
	mock.Start("sol-alpha-Toast", "/tmp", "echo", nil, "outpost", "alpha")

	sphereStore.CreateAgent("Jasper", "beta", "outpost")
	sphereStore.UpdateAgentState("beta/Jasper", "working", "sol-bbb22222")
	mock.Start("sol-beta-Jasper", "/tmp", "echo", nil, "outpost", "beta")

	sup := New(cfg, sphereStore, mock, logger)
	sup.shutdown()

	// Only alpha agent's session should be stopped.
	stopped := mock.GetStopped()
	if len(stopped) != 1 {
		t.Fatalf("expected 1 session stopped with worlds filter, got %d: %v", len(stopped), stopped)
	}
	if stopped[0] != "sol-alpha-Toast" {
		t.Errorf("stopped session = %q, want %q", stopped[0], "sol-alpha-Toast")
	}

	// Beta session should still be alive.
	if !mock.Exists("sol-beta-Jasper") {
		t.Error("beta session should not be stopped when outside worlds filter")
	}
}

// mockCommandRunner tracks exec.Command calls for testing checkWorldInfrastructure.
type mockCommandRunner struct {
	mu    sync.Mutex
	calls [][]string // each call is [name, arg1, arg2, ...]
}

func (m *mockCommandRunner) run(name string, args ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	call := append([]string{name}, args...)
	m.calls = append(m.calls, call)
	return nil
}

func (m *mockCommandRunner) getCalls() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([][]string, len(m.calls))
	for i, c := range m.calls {
		cp := make([]string, len(c))
		copy(cp, c)
		result[i] = cp
	}
	return result
}

func TestHeartbeatStartsMissingWorldServices(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.SolBinary = "/usr/bin/sol" // Set so infrastructure check runs.

	// Register a world in the sphere store.
	sphereStore.RegisterWorld("haven", "/tmp/repo")

	// Create world.toml so IsSleeping returns false (not sleeping).
	worldDir := filepath.Join(os.Getenv("SOL_HOME"), "haven")
	os.MkdirAll(worldDir, 0o755)
	os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(`[world]
source_repo = "/tmp/repo"
`), 0o644)

	// Write alive PID files for sphere daemons so checkSphereDaemons doesn't trigger.
	for _, name := range []string{"chronicle", "ledger", "broker"} {
		writePIDFile(t, name, os.Getpid())
	}

	cmdRunner := &mockCommandRunner{}

	sup := New(cfg, sphereStore, mock, logger)
	sup.runCommand = cmdRunner.run

	// First heartbeat triggers infrastructure check.
	sup.heartbeat()

	// Should have started sentinel and forge for the world.
	calls := cmdRunner.getCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 command calls (sentinel + forge), got %d: %v", len(calls), calls)
	}

	// Verify the commands contain the right service and world args.
	foundSentinel := false
	foundForge := false
	for _, call := range calls {
		// call is [solBin, service, "start", "--world=haven"]
		if len(call) >= 4 && call[1] == "sentinel" && call[2] == "start" && call[3] == "--world=haven" {
			foundSentinel = true
		}
		if len(call) >= 4 && call[1] == "forge" && call[2] == "start" && call[3] == "--world=haven" {
			foundForge = true
		}
	}
	if !foundSentinel {
		t.Error("expected sentinel start command, not found in calls")
	}
	if !foundForge {
		t.Error("expected forge start command, not found in calls")
	}
}

func TestHeartbeatSkipsRunningWorldServices(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.SolBinary = "/usr/bin/sol"

	sphereStore.RegisterWorld("haven", "/tmp/repo")

	worldDir := filepath.Join(os.Getenv("SOL_HOME"), "haven")
	os.MkdirAll(worldDir, 0o755)
	os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(`[world]
source_repo = "/tmp/repo"
`), 0o644)

	// Mark forge as already running (PID-based) with a fresh heartbeat.
	writeForgePIDFile(t, "haven", os.Getpid())
	writeForgeHeartbeat(t, "haven")

	// Mark sentinel as running (PID file).
	if err := sentinel.WritePID("haven", os.Getpid()); err != nil {
		t.Fatalf("failed to write sentinel PID: %v", err)
	}

	// Write alive PID files for sphere daemons so checkSphereDaemons doesn't trigger.
	for _, name := range []string{"chronicle", "ledger", "broker"} {
		writePIDFile(t, name, os.Getpid())
	}

	cmdRunner := &mockCommandRunner{}

	sup := New(cfg, sphereStore, mock, logger)
	sup.runCommand = cmdRunner.run

	sup.heartbeat()

	// No commands should be issued — both services are already running.
	calls := cmdRunner.getCalls()
	if len(calls) != 0 {
		t.Fatalf("expected 0 command calls (services running), got %d: %v", len(calls), calls)
	}
}

func TestHeartbeatSkipsSleepingWorldInfra(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.SolBinary = "/usr/bin/sol"

	sphereStore.RegisterWorld("sleepy", "/tmp/repo")

	// Create a sleeping world config.
	worldDir := filepath.Join(os.Getenv("SOL_HOME"), "sleepy")
	os.MkdirAll(worldDir, 0o755)
	os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(`[world]
source_repo = "/tmp/repo"
sleeping = true
`), 0o644)

	// Write alive PID files for sphere daemons so checkSphereDaemons doesn't trigger.
	for _, name := range []string{"chronicle", "ledger", "broker"} {
		writePIDFile(t, name, os.Getpid())
	}

	cmdRunner := &mockCommandRunner{}

	sup := New(cfg, sphereStore, mock, logger)
	sup.runCommand = cmdRunner.run

	sup.heartbeat()

	// No commands should be issued — world is sleeping.
	calls := cmdRunner.getCalls()
	if len(calls) != 0 {
		t.Fatalf("expected 0 command calls for sleeping world, got %d: %v", len(calls), calls)
	}
}

func TestHeartbeatWorldInfraRespectsWorldsFilter(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.SolBinary = "/usr/bin/sol"
	cfg.Worlds = []string{"alpha"}

	// Register two worlds.
	sphereStore.RegisterWorld("alpha", "/tmp/repo")
	sphereStore.RegisterWorld("beta", "/tmp/repo")

	for _, w := range []string{"alpha", "beta"} {
		worldDir := filepath.Join(os.Getenv("SOL_HOME"), w)
		os.MkdirAll(worldDir, 0o755)
		os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(`[world]
source_repo = "/tmp/repo"
`), 0o644)
	}

	// Write alive PID files for sphere daemons so checkSphereDaemons doesn't trigger.
	for _, name := range []string{"chronicle", "ledger", "broker"} {
		writePIDFile(t, name, os.Getpid())
	}

	cmdRunner := &mockCommandRunner{}

	sup := New(cfg, sphereStore, mock, logger)
	sup.runCommand = cmdRunner.run

	sup.heartbeat()

	// Only alpha services should be started.
	calls := cmdRunner.getCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 command calls (sentinel + forge for alpha only), got %d: %v", len(calls), calls)
	}

	for _, call := range calls {
		if len(call) >= 4 && call[3] != "--world=alpha" {
			t.Errorf("expected only alpha world services, got call: %v", call)
		}
	}
}

func TestShutdownStopsWorldServices(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	sphereStore.RegisterWorld("haven", "/tmp/repo")

	worldDir := filepath.Join(os.Getenv("SOL_HOME"), "haven")
	os.MkdirAll(worldDir, 0o755)
	os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(`[world]
source_repo = "/tmp/repo"
`), 0o644)

	// Write forge PID file pointing to a PID that is NOT running
	// (use PID 0 or a dead PID so StopProcess won't actually kill anything).
	writeForgePIDFile(t, "haven", 99999999)

	// Note: sentinel is now a direct process — shutdown sends SIGTERM via PID.
	// We don't write a sentinel PID in this test to avoid SIGTERM-ing the test process.

	sup := New(cfg, sphereStore, mock, logger)
	sup.shutdown()

	// Forge PID file should be cleaned up (process was dead).
	pidPath := filepath.Join(os.Getenv("SOL_HOME"), "haven", "forge", "forge.pid")
	if _, err := os.Stat(pidPath); err == nil {
		t.Error("expected forge PID file to be cleaned up during shutdown")
	}
}

func TestShutdownSkipsSleepingWorldServices(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	sphereStore.RegisterWorld("sleepy", "/tmp/repo")

	worldDir := filepath.Join(os.Getenv("SOL_HOME"), "sleepy")
	os.MkdirAll(worldDir, 0o755)
	os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(`[world]
source_repo = "/tmp/repo"
sleeping = true
`), 0o644)

	// Write forge PID file (should not be touched since world is sleeping).
	writeForgePIDFile(t, "sleepy", os.Getpid())
	// Note: sentinel is now a direct process. We don't write PID in this test
	// to avoid SIGTERM-ing the test process.

	sup := New(cfg, sphereStore, mock, logger)
	sup.shutdown()

	// World service sessions should NOT be stopped — world is sleeping.
	stopped := mock.GetStopped()
	for _, s := range stopped {
		if s == "sol-sleepy-sentinel" || s == "sol-sleepy-forge" {
			t.Errorf("sleeping world service %q should not be stopped during shutdown", s)
		}
	}

	// Forge PID file should still exist (sleeping world not touched).
	pidPath := filepath.Join(os.Getenv("SOL_HOME"), "sleepy", "forge", "forge.pid")
	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		t.Error("sleeping world forge PID file should not be cleaned up during shutdown")
	}
}

func TestShutdownWorldServicesRespectsWorldsFilter(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.Worlds = []string{"alpha"}

	sphereStore.RegisterWorld("alpha", "/tmp/repo")
	sphereStore.RegisterWorld("beta", "/tmp/repo")

	for _, w := range []string{"alpha", "beta"} {
		worldDir := filepath.Join(os.Getenv("SOL_HOME"), w)
		os.MkdirAll(worldDir, 0o755)
		os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(`[world]
source_repo = "/tmp/repo"
`), 0o644)
		// Write forge PID files with dead PIDs (won't try to signal).
		writeForgePIDFile(t, w, 99999999)
		// Note: sentinel is now a direct process. We don't write PID in this test
		// to avoid SIGTERM-ing the test process.
	}

	sup := New(cfg, sphereStore, mock, logger)
	sup.shutdown()

	// Only alpha sentinel should be stopped via session.
	stopped := mock.GetStopped()
	for _, s := range stopped {
		if strings.Contains(s, "beta") {
			t.Errorf("beta world service %q should not be stopped with worlds filter", s)
		}
	}

	// Alpha's forge PID file should be cleaned up.
	alphaPID := filepath.Join(os.Getenv("SOL_HOME"), "alpha", "forge", "forge.pid")
	if _, err := os.Stat(alphaPID); err == nil {
		t.Error("expected alpha forge PID file to be cleaned up during shutdown")
	}

	// Beta's forge PID file should still exist (excluded from world filter).
	betaPID := filepath.Join(os.Getenv("SOL_HOME"), "beta", "forge", "forge.pid")
	if _, err := os.Stat(betaPID); os.IsNotExist(err) {
		t.Error("expected beta forge PID file to remain (excluded from worlds filter)")
	}
}

func TestHeartbeatInfraCheckPeriodicity(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.SolBinary = "/usr/bin/sol"

	sphereStore.RegisterWorld("haven", "/tmp/repo")

	worldDir := filepath.Join(os.Getenv("SOL_HOME"), "haven")
	os.MkdirAll(worldDir, 0o755)
	os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(`[world]
source_repo = "/tmp/repo"
`), 0o644)

	// Write alive PID files for sphere daemons so checkSphereDaemons doesn't trigger.
	for _, name := range []string{"chronicle", "ledger", "broker"} {
		writePIDFile(t, name, os.Getpid())
	}

	cmdRunner := &mockCommandRunner{}

	sup := New(cfg, sphereStore, mock, logger)
	sup.runCommand = cmdRunner.run

	// Heartbeat 1: first cycle, should check infrastructure.
	sup.heartbeat()
	calls1 := len(cmdRunner.getCalls())
	if calls1 == 0 {
		t.Fatal("expected infrastructure check on first heartbeat")
	}

	// Heartbeat 2: count=2, not %3==0, should NOT check.
	sup.heartbeat()
	calls2 := len(cmdRunner.getCalls())
	if calls2 != calls1 {
		t.Errorf("heartbeat 2 should not trigger infra check, calls went from %d to %d", calls1, calls2)
	}

	// Heartbeat 3: count=3, 3%%3==0, should check.
	sup.heartbeat()
	calls3 := len(cmdRunner.getCalls())
	if calls3 == calls2 {
		t.Error("heartbeat 3 should trigger infra check")
	}
}

func TestRespawnOutpostUsesStartupLaunch(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Register the outpost role config so prefect uses startup.Launch.
	// Use a simplified config that avoids needing tether/world store for persona.
	startup.Register("outpost", startup.RoleConfig{
		Role:        "outpost",
		WorktreeDir: func(w, a string) string { return dispatch.WorktreePath(w, a) },
		Persona:     func(w, a string) ([]byte, error) { return []byte("# Test Agent"), nil },
		PrimeBuilder: func(w, a string) string {
			return "Agent " + a + ", world " + w
		},
	})
	t.Cleanup(func() {
		// Unregister to avoid polluting other tests.
		startup.Register("outpost", startup.RoleConfig{})
	})

	// Create world config (required by startup.Launch for CLAUDE_CONFIG_DIR).
	worldDir := filepath.Join(os.Getenv("SOL_HOME"), "haven")
	os.MkdirAll(worldDir, 0o755)
	os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(`[world]
source_repo = "/tmp/fakerepo"
`), 0o644)

	// Create a working agent with a worktree.
	sphereStore.CreateAgent("Toast", "haven", "outpost")
	sphereStore.UpdateAgentState("haven/Toast", "working", "sol-abc12345")

	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "haven", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sup := New(cfg, sphereStore, mock, logger)
	sup.heartbeat()

	// Should have started a session via startup.Launch.
	started := mock.GetStarted()
	if len(started) != 1 {
		t.Fatalf("expected 1 session started, got %d", len(started))
	}
	if started[0] != "sol-haven-Toast" {
		t.Errorf("started session = %q, want %q", started[0], "sol-haven-Toast")
	}

	// Verify CLAUDE_CONFIG_DIR is set (proves startup.Launch was used,
	// not the legacy respawnCommand path which doesn't set it).
	mock.mu.Lock()
	env := mock.lastEnv
	mock.mu.Unlock()

	if env["CLAUDE_CONFIG_DIR"] == "" {
		t.Error("CLAUDE_CONFIG_DIR not set — respawn did not use startup.Launch")
	}
	if env["SOL_HOME"] == "" {
		t.Error("SOL_HOME not set in env")
	}
	if env["SOL_WORLD"] != "haven" {
		t.Errorf("SOL_WORLD = %q, want %q", env["SOL_WORLD"], "haven")
	}
	if env["SOL_AGENT"] != "Toast" {
		t.Errorf("SOL_AGENT = %q, want %q", env["SOL_AGENT"], "Toast")
	}

	// Verify tether item is preserved in agent state.
	agent, err := sphereStore.GetAgent("haven/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "working" {
		t.Errorf("agent state = %q, want %q", agent.State, "working")
	}
	if agent.ActiveWrit != "sol-abc12345" {
		t.Errorf("agent active_writ = %q, want %q (tether item not preserved)", agent.ActiveWrit, "sol-abc12345")
	}
}

// --- Sphere daemon supervision tests ---

// mockDaemonTracker tracks calls to runCommand and startDaemonProcess for sphere daemon tests.
type mockDaemonTracker struct {
	mu               sync.Mutex
	runCalls         [][]string // [binary, arg1, arg2, ...]
	detachedCalls    [][]string // [daemon, binary, arg1, arg2, ...]
	runErr           error      // if set, runCommand returns this error
	detachedErr      error      // if set, startDaemonProcess returns this error
}

func (m *mockDaemonTracker) runCommand(name string, args ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	call := append([]string{name}, args...)
	m.runCalls = append(m.runCalls, call)
	return m.runErr
}

func (m *mockDaemonTracker) startDaemonProcess(daemon string, binPath string, args ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	call := append([]string{daemon, binPath}, args...)
	m.detachedCalls = append(m.detachedCalls, call)
	return m.detachedErr
}

func (m *mockDaemonTracker) getRunCalls() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([][]string, len(m.runCalls))
	for i, c := range m.runCalls {
		cp := make([]string, len(c))
		copy(cp, c)
		result[i] = cp
	}
	return result
}

func (m *mockDaemonTracker) getDetachedCalls() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([][]string, len(m.detachedCalls))
	for i, c := range m.detachedCalls {
		cp := make([]string, len(c))
		copy(cp, c)
		result[i] = cp
	}
	return result
}

// writePIDFile writes a PID file for a named daemon in the test runtime dir.
func writePIDFile(t *testing.T, name string, pid int) {
	t.Helper()
	runtimeDir := filepath.Join(os.Getenv("SOL_HOME"), ".runtime")
	os.MkdirAll(runtimeDir, 0o755)
	path := filepath.Join(runtimeDir, name+".pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		t.Fatalf("failed to write PID file for %s: %v", name, err)
	}
}

// writeForgePIDFile writes a forge PID file for a world in the test SOL_HOME.
func writeForgePIDFile(t *testing.T, world string, pid int) {
	t.Helper()
	forgeDir := filepath.Join(os.Getenv("SOL_HOME"), world, "forge")
	os.MkdirAll(forgeDir, 0o755)
	path := filepath.Join(forgeDir, "forge.pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		t.Fatalf("failed to write forge PID file for world %s: %v", world, err)
	}
}

// writeForgeHeartbeat writes a fresh forge heartbeat for a world.
func writeForgeHeartbeat(t *testing.T, world string) {
	t.Helper()
	forgeDir := filepath.Join(os.Getenv("SOL_HOME"), world, "forge")
	os.MkdirAll(forgeDir, 0o755)
	hbJSON := fmt.Sprintf(`{"timestamp":"%s","status":"idle","patrol_count":1}`, time.Now().UTC().Format(time.RFC3339))
	path := filepath.Join(forgeDir, "heartbeat.json")
	if err := os.WriteFile(path, []byte(hbJSON), 0o644); err != nil {
		t.Fatalf("failed to write forge heartbeat for world %s: %v", world, err)
	}
}

func TestCheckSphereDaemonsRestartsDeadDaemons(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.SolBinary = "/usr/bin/sol"

	tracker := &mockDaemonTracker{}

	sup := New(cfg, sphereStore, mock, logger)
	sup.runCommand = tracker.runCommand
	sup.startDaemonProcess = tracker.startDaemonProcess

	// No PID files exist and no tmux sessions — all daemons are dead.
	// Run heartbeat (first heartbeat triggers sphere daemon check).
	sup.heartbeat()

	// All sphere daemons now use startDaemonProcess (detached).
	// Ledger + broker via checkSphereDaemons; chronicle via checkChronicleHealth.
	detachedCalls := tracker.getDetachedCalls()
	foundLedger := false
	foundBroker := false
	foundChronicle := false
	for _, call := range detachedCalls {
		if len(call) >= 4 && call[0] == "ledger" && call[1] == "/usr/bin/sol" &&
			call[2] == "ledger" && call[3] == "run" {
			foundLedger = true
		}
		if len(call) >= 4 && call[0] == "broker" && call[1] == "/usr/bin/sol" &&
			call[2] == "broker" && call[3] == "run" {
			foundBroker = true
		}
		if len(call) >= 4 && call[0] == "chronicle" && call[1] == "/usr/bin/sol" &&
			call[2] == "chronicle" && call[3] == "run" {
			foundChronicle = true
		}
	}
	if !foundLedger {
		t.Error("expected ledger restart via startDaemonProcess, not found")
	}
	if !foundBroker {
		t.Error("expected broker restart via startDaemonProcess, not found")
	}
	if !foundChronicle {
		t.Error("expected chronicle restart via startDaemonProcess, not found")
	}
}

func TestCheckSphereDaemonsSkipsAlivePID(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.SolBinary = "/usr/bin/sol"

	tracker := &mockDaemonTracker{}

	sup := New(cfg, sphereStore, mock, logger)
	sup.runCommand = tracker.runCommand
	sup.startDaemonProcess = tracker.startDaemonProcess

	// Write PID files with our own PID (alive).
	myPID := os.Getpid()
	for _, name := range []string{"chronicle", "ledger", "broker"} {
		writePIDFile(t, name, myPID)
	}

	sup.heartbeat()

	// No restarts should occur.
	runCalls := tracker.getRunCalls()
	detachedCalls := tracker.getDetachedCalls()
	if len(runCalls) != 0 {
		t.Errorf("expected 0 runCommand calls for alive daemons, got %d: %v", len(runCalls), runCalls)
	}
	if len(detachedCalls) != 0 {
		t.Errorf("expected 0 startDaemonProcess calls for alive daemons, got %d: %v", len(detachedCalls), detachedCalls)
	}
}

func TestCheckSphereDaemonsSkipsAliveSession(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.SolBinary = "/usr/bin/sol"

	tracker := &mockDaemonTracker{}

	sup := New(cfg, sphereStore, mock, logger)
	sup.runCommand = tracker.runCommand
	sup.startDaemonProcess = tracker.startDaemonProcess

	// Chronicle has a tmux session, ledger has a PID file (both alive).
	mock.Start("sol-chronicle", "/tmp", "sol chronicle run", nil, "chronicle", "")
	writePIDFile(t, "ledger", os.Getpid()) // our PID is known-alive
	// Broker has no session (and no PID file) — should be restarted.

	sup.heartbeat()

	// Only broker should be restarted (chronicle has live session, ledger has live PID).
	runCalls := tracker.getRunCalls()
	for _, call := range runCalls {
		if len(call) >= 2 && (call[1] == "chronicle" || call[1] == "ledger") {
			t.Errorf("should not restart daemon with live session/PID: %v", call)
		}
	}

	detachedCalls := tracker.getDetachedCalls()
	foundBroker := false
	for _, call := range detachedCalls {
		if len(call) >= 1 && call[0] == "broker" {
			foundBroker = true
		}
		if len(call) >= 1 && call[0] == "ledger" {
			t.Errorf("should not restart ledger with live PID: %v", call)
		}
	}
	if !foundBroker {
		t.Error("expected broker restart (no session, no PID)")
	}
}

func TestCheckSphereDaemonsRestartFailureNonFatal(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.SolBinary = "/usr/bin/sol"

	tracker := &mockDaemonTracker{
		runErr:      fmt.Errorf("simulated failure"),
		detachedErr: fmt.Errorf("simulated failure"),
	}

	sup := New(cfg, sphereStore, mock, logger)
	sup.runCommand = tracker.runCommand
	sup.startDaemonProcess = tracker.startDaemonProcess

	// No PID files — all daemons are dead.
	// Heartbeat should not panic or crash even though restarts fail.
	sup.heartbeat()

	// Verify restart was attempted for all daemons.
	// All sphere daemons use startDaemonProcess (detached) in the merged state.
	detachedCalls := tracker.getDetachedCalls()
	if len(detachedCalls) < 3 {
		t.Errorf("expected at least 3 startDaemonProcess calls (ledger + broker + chronicle), got %d", len(detachedCalls))
	}
}

func TestCheckSphereDaemonsSkipsWithoutSolBinary(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	// cfg.SolBinary is empty — sphere daemon check should be skipped.

	tracker := &mockDaemonTracker{}

	sup := New(cfg, sphereStore, mock, logger)
	sup.runCommand = tracker.runCommand
	sup.startDaemonProcess = tracker.startDaemonProcess

	// No PID files — all daemons are dead.
	sup.heartbeat()

	// No restarts should occur since SolBinary is not configured.
	runCalls := tracker.getRunCalls()
	detachedCalls := tracker.getDetachedCalls()
	if len(runCalls) != 0 {
		t.Errorf("expected 0 runCommand calls without SolBinary, got %d", len(runCalls))
	}
	if len(detachedCalls) != 0 {
		t.Errorf("expected 0 startDaemonProcess calls without SolBinary, got %d", len(detachedCalls))
	}
}

func TestCheckSphereDaemonsPeriodicity(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.SolBinary = "/usr/bin/sol"

	tracker := &mockDaemonTracker{}

	sup := New(cfg, sphereStore, mock, logger)
	sup.runCommand = tracker.runCommand
	sup.startDaemonProcess = tracker.startDaemonProcess

	// Write alive PID files for chronicle and ledger so only broker triggers.
	// This lets us count detachedCalls precisely.
	myPID := os.Getpid()
	writePIDFile(t, "chronicle", myPID)
	writePIDFile(t, "ledger", myPID)
	// broker has no PID — will be restarted each check.

	// Heartbeat 1 (count=1): should check sphere daemons.
	sup.heartbeat()
	calls1 := len(tracker.getDetachedCalls())
	if calls1 != 1 {
		t.Fatalf("heartbeat 1: expected 1 detached call, got %d", calls1)
	}

	// Heartbeat 2 (count=2): 2%%3 != 0, should NOT check.
	sup.heartbeat()
	calls2 := len(tracker.getDetachedCalls())
	if calls2 != calls1 {
		t.Errorf("heartbeat 2: should not check daemons, calls went from %d to %d", calls1, calls2)
	}

	// Heartbeat 3 (count=3): 3%%3 == 0, should check.
	sup.heartbeat()
	calls3 := len(tracker.getDetachedCalls())
	if calls3 != calls1+1 {
		t.Errorf("heartbeat 3: expected daemon check, calls = %d (want %d)", calls3, calls1+1)
	}

	// Heartbeat 4 (count=4): 4%%3 != 0, should NOT check.
	sup.heartbeat()
	calls4 := len(tracker.getDetachedCalls())
	if calls4 != calls3 {
		t.Errorf("heartbeat 4: should not check daemons, calls went from %d to %d", calls3, calls4)
	}
}

func TestReadDaemonPID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755)

	// No file — returns 0.
	if pid := ReadDaemonPID("noexist"); pid != 0 {
		t.Errorf("ReadDaemonPID(noexist) = %d, want 0", pid)
	}

	// Valid PID file.
	if err := WriteDaemonPID("test", 12345); err != nil {
		t.Fatal(err)
	}
	if pid := ReadDaemonPID("test"); pid != 12345 {
		t.Errorf("ReadDaemonPID(test) = %d, want 12345", pid)
	}

	// Invalid content.
	os.WriteFile(filepath.Join(dir, ".runtime", "bad.pid"), []byte("not-a-number"), 0o644)
	if pid := ReadDaemonPID("bad"); pid != 0 {
		t.Errorf("ReadDaemonPID(bad) = %d, want 0", pid)
	}
}

func TestCheckSphereDaemonsDeadPIDTriggersRestart(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.SolBinary = "/usr/bin/sol"

	tracker := &mockDaemonTracker{}

	sup := New(cfg, sphereStore, mock, logger)
	sup.runCommand = tracker.runCommand
	sup.startDaemonProcess = tracker.startDaemonProcess

	// Write PID files with a dead PID (very high, certainly not running).
	for _, name := range []string{"chronicle", "ledger", "broker"} {
		writePIDFile(t, name, 2147483647)
	}

	sup.heartbeat()

	// All daemons should be restarted via startDaemonProcess.
	detachedCalls := tracker.getDetachedCalls()

	if len(detachedCalls) < 3 {
		t.Errorf("expected at least 3 startDaemonProcess calls (ledger + broker + chronicle), got %d", len(detachedCalls))
	}
}

