package prefect

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/dispatch"
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
	startup.Register("agent", startup.RoleConfig{
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
		startup.Register("agent", startup.RoleConfig{})
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
	sphereStore.CreateAgent("Toast", "haven", "agent")
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
	sphereStore.CreateAgent("Jasper", "haven", "agent")

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
	sphereStore.CreateAgent("Toast", "alpha", "agent")
	sphereStore.UpdateAgentState("alpha/Toast", "working", "sol-aaa11111")
	sphereStore.CreateAgent("Jasper", "beta", "agent")
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

	sphereStore.CreateAgent("Toast", "haven", "agent")
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
		sphereStore.CreateAgent(name, "haven", "agent")
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

	sphereStore.CreateAgent("Toast", "haven", "agent")
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
		sphereStore.CreateAgent(name, "haven", "agent")
		sphereStore.UpdateAgentState("haven/"+name, "working", "sol-"+name)
		mock.Start("sol-haven-"+name, "/tmp", "echo", nil, "agent", "haven")
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

	sphereStore.CreateAgent("Toast", "haven", "agent")
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

	sphereStore.CreateAgent("Ghost", "haven", "agent")
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
	sphereStore.CreateAgent("Toast", "haven", "agent")
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
	agentBot := store.Agent{Name: "Toast", World: "haven", Role: "agent"}
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

	// Create a sentinel agent in working state with a live session.
	sphereStore.CreateAgent("sentinel", "haven", "sentinel")
	sphereStore.UpdateAgentState("haven/sentinel", "working", "")
	mock.Start("sol-haven-sentinel", "/tmp", "sol sentinel run --world=haven", nil, "sentinel", "haven")

	// Create a working agent with a dead session.
	sphereStore.CreateAgent("Toast", "haven", "agent")
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
	sphereStore.CreateAgent("Toast", "haven", "agent")
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
	sphereStore.CreateAgent("Toast", "haven", "agent")
	sphereStore.UpdateAgentState("haven/Toast", "working", "sol-abc12345")
	mock.Start("sol-haven-Toast", "/tmp", "echo", nil, "agent", "haven")

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
	sphereStore.CreateAgent("Toast", "alpha", "agent")
	sphereStore.UpdateAgentState("alpha/Toast", "working", "sol-aaa11111")
	sphereStore.CreateAgent("Jasper", "beta", "agent")
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
	sphereStore.CreateAgent("Toast", "alpha", "agent")
	sphereStore.UpdateAgentState("alpha/Toast", "working", "sol-aaa11111")
	mock.Start("sol-alpha-Toast", "/tmp", "echo", nil, "agent", "alpha")

	sphereStore.CreateAgent("Jasper", "beta", "agent")
	sphereStore.UpdateAgentState("beta/Jasper", "working", "sol-bbb22222")
	mock.Start("sol-beta-Jasper", "/tmp", "echo", nil, "agent", "beta")

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

func TestRespawnOutpostUsesStartupLaunch(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Register the outpost role config so prefect uses startup.Launch.
	// Use a simplified config that avoids needing tether/world store for persona.
	startup.Register("agent", startup.RoleConfig{
		Role:        "agent",
		WorktreeDir: func(w, a string) string { return dispatch.WorktreePath(w, a) },
		Persona:     func(w, a string) ([]byte, error) { return []byte("# Test Agent"), nil },
		PrimeBuilder: func(w, a string) string {
			return "Agent " + a + ", world " + w
		},
	})
	t.Cleanup(func() {
		// Unregister to avoid polluting other tests.
		startup.Register("agent", startup.RoleConfig{})
	})

	// Create world config (required by startup.Launch for CLAUDE_CONFIG_DIR).
	worldDir := filepath.Join(os.Getenv("SOL_HOME"), "haven")
	os.MkdirAll(worldDir, 0o755)
	os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(`[world]
source_repo = "/tmp/fakerepo"
`), 0o644)

	// Create a working agent with a worktree.
	sphereStore.CreateAgent("Toast", "haven", "agent")
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
	if agent.TetherItem != "sol-abc12345" {
		t.Errorf("agent tether_item = %q, want %q (tether item not preserved)", agent.TetherItem, "sol-abc12345")
	}
}

