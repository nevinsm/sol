package prefect

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
)

// mockSessions implements SessionManager for testing.
type mockSessions struct {
	mu      sync.Mutex
	alive   map[string]bool
	started []string
	stopped []string
}

func newMockSessions() *mockSessions {
	return &mockSessions{alive: make(map[string]bool)}
}

func (m *mockSessions) Exists(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.alive[name]
}

func (m *mockSessions) Start(name, workdir, cmd string, env map[string]string, role, rig string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alive[name] = true
	m.started = append(m.started, name)
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

func TestHeartbeatDetectsDead(t *testing.T) {
	sphereStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create a working agent with a worktree.
	sphereStore.CreateAgent("Toast", "myrig", "agent")
	sphereStore.UpdateAgentState("myrig/Toast", "working", "gt-abc12345")

	// Create the worktree directory so respawn doesn't bail.
	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "myrig", "outposts", "Toast", "worktree")
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
	if started[0] != "sol-myrig-Toast" {
		t.Errorf("started session = %q, want %q", started[0], "sol-myrig-Toast")
	}

	// Agent should be back to working.
	agent, err := sphereStore.GetAgent("myrig/Toast")
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
	sphereStore.CreateAgent("Jasper", "myrig", "agent")

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
	sphereStore.CreateAgent("Toast", "rig1", "agent")
	sphereStore.UpdateAgentState("rig1/Toast", "working", "gt-aaa11111")
	sphereStore.CreateAgent("Jasper", "rig2", "agent")
	sphereStore.UpdateAgentState("rig2/Jasper", "working", "gt-bbb22222")

	// Create worktree directories.
	for _, p := range []string{
		filepath.Join(os.Getenv("SOL_HOME"), "rig1", "outposts", "Toast", "worktree"),
		filepath.Join(os.Getenv("SOL_HOME"), "rig2", "outposts", "Jasper", "worktree"),
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

	sphereStore.CreateAgent("Toast", "myrig", "agent")
	sphereStore.UpdateAgentState("myrig/Toast", "working", "gt-abc12345")

	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "myrig", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sup := New(cfg, sphereStore, mock, logger)

	// First heartbeat: immediate respawn (restart 1, delay 0).
	sup.heartbeat()
	started := mock.GetStarted()
	if len(started) != 1 {
		t.Fatalf("restart 1: expected 1 start, got %d", len(started))
	}

	// Kill the session again.
	mock.Kill("sol-myrig-Toast")

	// Second heartbeat: restart 2, delay 30s — should stall, not restart.
	sup.heartbeat()
	started = mock.GetStarted()
	if len(started) != 1 {
		t.Fatalf("restart 2: expected still 1 start (deferred), got %d", len(started))
	}

	// Verify agent is stalled.
	agent, err := sphereStore.GetAgent("myrig/Toast")
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
		sphereStore.CreateAgent(name, "myrig", "agent")
		sphereStore.UpdateAgentState("myrig/"+name, "working", "sol-"+name)
		worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "myrig", "outposts", name, "worktree")
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

	sphereStore.CreateAgent("Toast", "myrig", "agent")
	sphereStore.UpdateAgentState("myrig/Toast", "working", "gt-abc12345")

	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "myrig", "outposts", "Toast", "worktree")
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
	agent, err := sphereStore.GetAgent("myrig/Toast")
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
		sphereStore.CreateAgent(name, "myrig", "agent")
		sphereStore.UpdateAgentState("myrig/"+name, "working", "sol-"+name)
		mock.Start("sol-myrig-"+name, "/tmp", "echo", nil, "agent", "myrig")
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
		agent, err := sphereStore.GetAgent("myrig/" + name)
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

	sphereStore.CreateAgent("Toast", "myrig", "agent")
	sphereStore.UpdateAgentState("myrig/Toast", "working", "gt-abc12345")

	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "myrig", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sup := New(cfg, sphereStore, mock, logger)

	// First heartbeat: respawn (backoff count = 1).
	sup.heartbeat()
	if sup.backoff["myrig/Toast"] != 1 {
		t.Fatalf("backoff count = %d, want 1", sup.backoff["myrig/Toast"])
	}

	// Set agent to idle (simulating sol done).
	sphereStore.UpdateAgentState("myrig/Toast", "idle", "")

	// Next heartbeat should reset backoff.
	sup.heartbeat()
	if count, ok := sup.backoff["myrig/Toast"]; ok {
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

	sphereStore.CreateAgent("Ghost", "myrig", "agent")
	sphereStore.UpdateAgentState("myrig/Ghost", "working", "gt-ghost123")

	// Do NOT create worktree directory.

	sup := New(cfg, sphereStore, mock, logger)
	sup.heartbeat()

	// Should NOT have started a session.
	started := mock.GetStarted()
	if len(started) != 0 {
		t.Fatalf("expected 0 starts for missing worktree, got %d", len(started))
	}

	// Agent should be idle.
	agent, err := sphereStore.GetAgent("myrig/Ghost")
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
	sphereStore.CreateAgent("forge", "myrig", "forge")
	sphereStore.UpdateAgentState("myrig/forge", "working", "")

	// Create the forge worktree directory.
	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "myrig", "forge", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	// Session is dead (not in mock).
	sup := New(cfg, sphereStore, mock, logger)
	sup.heartbeat()

	// Should have started a session.
	started := mock.GetStarted()
	if len(started) != 1 {
		t.Fatalf("expected 1 session started, got %d", len(started))
	}
	if started[0] != "sol-myrig-forge" {
		t.Errorf("started session = %q, want %q", started[0], "sol-myrig-forge")
	}

	// Verify the session was started with the right command by checking agent is working.
	agent, err := sphereStore.GetAgent("myrig/forge")
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
	sphereStore.CreateAgent("Toast", "myrig", "agent")
	sphereStore.UpdateAgentState("myrig/Toast", "working", "gt-abc12345")

	// Create the agent worktree directory.
	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "myrig", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sup := New(cfg, sphereStore, mock, logger)
	sup.heartbeat()

	// Should have started with agent session name.
	started := mock.GetStarted()
	if len(started) != 1 {
		t.Fatalf("expected 1 session started, got %d", len(started))
	}
	if started[0] != "sol-myrig-Toast" {
		t.Errorf("started session = %q, want %q", started[0], "sol-myrig-Toast")
	}

	agent, err := sphereStore.GetAgent("myrig/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "working" {
		t.Errorf("agent state = %q, want %q", agent.State, "working")
	}
}

func TestRespawnCommandByRole(t *testing.T) {
	forgeAgent := store.Agent{Name: "forge", World: "myrig", Role: "forge"}
	agentBot := store.Agent{Name: "Toast", World: "myrig", Role: "agent"}
	sentinelAgent := store.Agent{Name: "sentinel", World: "myrig", Role: "sentinel"}

	// Forge and agents use Claude sessions.
	forgeCmd := respawnCommand(forgeAgent)
	if forgeCmd != "claude --dangerously-skip-permissions" {
		t.Errorf("forge command = %q, want %q", forgeCmd, "claude --dangerously-skip-permissions")
	}

	agentCmd := respawnCommand(agentBot)
	if agentCmd != "claude --dangerously-skip-permissions" {
		t.Errorf("agent command = %q, want %q", agentCmd, "claude --dangerously-skip-permissions")
	}

	// Sentinel uses sol sentinel run.
	sentCmd := respawnCommand(sentinelAgent)
	want := "sol sentinel run myrig"
	if sentCmd != want {
		t.Errorf("sentinel command = %q, want %q", sentCmd, want)
	}
}

func TestWorktreeForAgentByRole(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	forgeAgent := store.Agent{Name: "forge", World: "myrig", Role: "forge"}
	agentBot := store.Agent{Name: "Toast", World: "myrig", Role: "agent"}
	sentinelAgent := store.Agent{Name: "sentinel", World: "myrig", Role: "sentinel"}

	forgePath := worktreeForAgent(forgeAgent)
	expected := filepath.Join(dir, "myrig", "forge", "worktree")
	if forgePath != expected {
		t.Errorf("forge worktree = %q, want %q", forgePath, expected)
	}

	agentPath := worktreeForAgent(agentBot)
	expected = filepath.Join(dir, "myrig", "outposts", "Toast", "worktree")
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
	sphereStore.CreateAgent("sentinel", "myrig", "sentinel")
	sphereStore.UpdateAgentState("myrig/sentinel", "working", "")
	mock.Start("sol-myrig-sentinel", "/tmp", "sol sentinel run myrig", nil, "sentinel", "myrig")

	// Create a working agent with a dead session.
	sphereStore.CreateAgent("Toast", "myrig", "agent")
	sphereStore.UpdateAgentState("myrig/Toast", "working", "gt-abc12345")
	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "myrig", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)
	// Session is dead (not started in mock for this agent).

	sup := New(cfg, sphereStore, mock, logger)
	sup.heartbeat()

	// The agent should NOT have been respawned by the prefect
	// because the world is sentineled.
	started := mock.GetStarted()
	for _, s := range started {
		if s == "sol-myrig-Toast" {
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
	sphereStore.CreateAgent("sentinel", "myrig", "sentinel")
	// State is idle (default).

	// Create a working agent with a dead session.
	sphereStore.CreateAgent("Toast", "myrig", "agent")
	sphereStore.UpdateAgentState("myrig/Toast", "working", "gt-abc12345")
	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "myrig", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sup := New(cfg, sphereStore, mock, logger)
	sup.heartbeat()

	// Without a working sentinel, the prefect should respawn the agent.
	started := mock.GetStarted()
	found := false
	for _, s := range started {
		if s == "sol-myrig-Toast" {
			found = true
		}
	}
	if !found {
		t.Error("prefect should respawn agents in unsentineled worlds")
	}
}
