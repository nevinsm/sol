package supervisor

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/store"
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

// setupTestEnv creates a test GT_HOME with a town DB and returns the store and cleanup function.
func setupTestEnv(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatal(err)
	}

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("failed to open town store: %v", err)
	}
	t.Cleanup(func() { townStore.Close() })
	return townStore
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
	townStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create a working agent with a worktree.
	townStore.CreateAgent("Toast", "myrig", "polecat")
	townStore.UpdateAgentState("myrig/Toast", "working", "gt-abc12345")

	// Create the worktree directory so respawn doesn't bail.
	worktreeDir := filepath.Join(os.Getenv("GT_HOME"), "myrig", "polecats", "Toast", "rig")
	os.MkdirAll(worktreeDir, 0o755)

	// Session is dead (not in mock).
	sup := New(cfg, townStore, mock, logger)

	// Run one heartbeat.
	sup.heartbeat()

	// Should have started a session.
	started := mock.GetStarted()
	if len(started) != 1 {
		t.Fatalf("expected 1 session started, got %d", len(started))
	}
	if started[0] != "gt-myrig-Toast" {
		t.Errorf("started session = %q, want %q", started[0], "gt-myrig-Toast")
	}

	// Agent should be back to working.
	agent, err := townStore.GetAgent("myrig/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "working" {
		t.Errorf("agent state = %q, want %q", agent.State, "working")
	}
}

func TestHeartbeatIgnoresIdle(t *testing.T) {
	townStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create an idle agent.
	townStore.CreateAgent("Jasper", "myrig", "polecat")

	sup := New(cfg, townStore, mock, logger)
	sup.heartbeat()

	// No sessions should be started.
	started := mock.GetStarted()
	if len(started) != 0 {
		t.Fatalf("expected 0 sessions started for idle agent, got %d", len(started))
	}
}

func TestHeartbeatMultipleRigs(t *testing.T) {
	townStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create working agents in different rigs.
	townStore.CreateAgent("Toast", "rig1", "polecat")
	townStore.UpdateAgentState("rig1/Toast", "working", "gt-aaa11111")
	townStore.CreateAgent("Jasper", "rig2", "polecat")
	townStore.UpdateAgentState("rig2/Jasper", "working", "gt-bbb22222")

	// Create worktree directories.
	for _, p := range []string{
		filepath.Join(os.Getenv("GT_HOME"), "rig1", "polecats", "Toast", "rig"),
		filepath.Join(os.Getenv("GT_HOME"), "rig2", "polecats", "Jasper", "rig"),
	} {
		os.MkdirAll(p, 0o755)
	}

	sup := New(cfg, townStore, mock, logger)
	sup.heartbeat()

	// Both should be restarted.
	started := mock.GetStarted()
	if len(started) != 2 {
		t.Fatalf("expected 2 sessions started across rigs, got %d: %v", len(started), started)
	}
}

func TestBackoffEscalation(t *testing.T) {
	townStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	townStore.CreateAgent("Toast", "myrig", "polecat")
	townStore.UpdateAgentState("myrig/Toast", "working", "gt-abc12345")

	worktreeDir := filepath.Join(os.Getenv("GT_HOME"), "myrig", "polecats", "Toast", "rig")
	os.MkdirAll(worktreeDir, 0o755)

	sup := New(cfg, townStore, mock, logger)

	// First heartbeat: immediate respawn (restart 1, delay 0).
	sup.heartbeat()
	started := mock.GetStarted()
	if len(started) != 1 {
		t.Fatalf("restart 1: expected 1 start, got %d", len(started))
	}

	// Kill the session again.
	mock.Kill("gt-myrig-Toast")

	// Second heartbeat: restart 2, delay 30s — should stall, not restart.
	sup.heartbeat()
	started = mock.GetStarted()
	if len(started) != 1 {
		t.Fatalf("restart 2: expected still 1 start (deferred), got %d", len(started))
	}

	// Verify agent is stalled.
	agent, err := townStore.GetAgent("myrig/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "stalled" {
		t.Errorf("agent state = %q, want %q after deferred respawn", agent.State, "stalled")
	}
}

func TestMassDeathDetection(t *testing.T) {
	townStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.MassDeathThreshold = 3
	cfg.MassDeathWindow = 30 * time.Second

	// Create 3 working agents.
	for _, name := range []string{"Toast", "Jasper", "Olive"} {
		townStore.CreateAgent(name, "myrig", "polecat")
		townStore.UpdateAgentState("myrig/"+name, "working", "gt-"+name)
		worktreeDir := filepath.Join(os.Getenv("GT_HOME"), "myrig", "polecats", name, "rig")
		os.MkdirAll(worktreeDir, 0o755)
	}

	sup := New(cfg, townStore, mock, logger)

	// First heartbeat detects 3 deaths -> mass death -> degraded.
	sup.heartbeat()

	if !sup.IsDegraded() {
		t.Fatal("supervisor should be in degraded mode after 3 deaths")
	}
}

func TestMassDeathRecovery(t *testing.T) {
	townStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.MassDeathThreshold = 3
	cfg.MassDeathWindow = 30 * time.Second
	cfg.DegradedCooldown = 10 * time.Millisecond // Very short for testing.

	sup := New(cfg, townStore, mock, logger)

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
		t.Fatal("supervisor should have exited degraded mode after cooldown")
	}
}

func TestDegradedModeSkipsRespawn(t *testing.T) {
	townStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	townStore.CreateAgent("Toast", "myrig", "polecat")
	townStore.UpdateAgentState("myrig/Toast", "working", "gt-abc12345")

	worktreeDir := filepath.Join(os.Getenv("GT_HOME"), "myrig", "polecats", "Toast", "rig")
	os.MkdirAll(worktreeDir, 0o755)

	sup := New(cfg, townStore, mock, logger)

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
	agent, err := townStore.GetAgent("myrig/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "stalled" {
		t.Errorf("agent state = %q, want %q in degraded mode", agent.State, "stalled")
	}
}

func TestShutdownStopsSessions(t *testing.T) {
	townStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create working agents with live sessions.
	for _, name := range []string{"Toast", "Jasper"} {
		townStore.CreateAgent(name, "myrig", "polecat")
		townStore.UpdateAgentState("myrig/"+name, "working", "gt-"+name)
		mock.Start("gt-myrig-"+name, "/tmp", "echo", nil, "polecat", "myrig")
	}

	sup := New(cfg, townStore, mock, logger)
	sup.shutdown()

	// Both sessions should be stopped.
	stopped := mock.GetStopped()
	if len(stopped) != 2 {
		t.Fatalf("expected 2 sessions stopped, got %d: %v", len(stopped), stopped)
	}

	// Agents should be stalled.
	for _, name := range []string{"Toast", "Jasper"} {
		agent, err := townStore.GetAgent("myrig/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if agent.State != "stalled" {
			t.Errorf("agent %s state = %q, want %q", name, agent.State, "stalled")
		}
	}
}

func TestBackoffReset(t *testing.T) {
	townStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	townStore.CreateAgent("Toast", "myrig", "polecat")
	townStore.UpdateAgentState("myrig/Toast", "working", "gt-abc12345")

	worktreeDir := filepath.Join(os.Getenv("GT_HOME"), "myrig", "polecats", "Toast", "rig")
	os.MkdirAll(worktreeDir, 0o755)

	sup := New(cfg, townStore, mock, logger)

	// First heartbeat: respawn (backoff count = 1).
	sup.heartbeat()
	if sup.backoff["myrig/Toast"] != 1 {
		t.Fatalf("backoff count = %d, want 1", sup.backoff["myrig/Toast"])
	}

	// Set agent to idle (simulating gt done).
	townStore.UpdateAgentState("myrig/Toast", "idle", "")

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
	townStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()
	cfg.HeartbeatInterval = 100 * time.Millisecond

	sup := New(cfg, townStore, mock, logger)

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
	townStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	townStore.CreateAgent("Ghost", "myrig", "polecat")
	townStore.UpdateAgentState("myrig/Ghost", "working", "gt-ghost123")

	// Do NOT create worktree directory.

	sup := New(cfg, townStore, mock, logger)
	sup.heartbeat()

	// Should NOT have started a session.
	started := mock.GetStarted()
	if len(started) != 0 {
		t.Fatalf("expected 0 starts for missing worktree, got %d", len(started))
	}

	// Agent should be idle.
	agent, err := townStore.GetAgent("myrig/Ghost")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "idle" {
		t.Errorf("agent state = %q, want %q for missing worktree", agent.State, "idle")
	}
}

func TestRespawnRefinery(t *testing.T) {
	townStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create a refinery agent in working state.
	townStore.CreateAgent("refinery", "myrig", "refinery")
	townStore.UpdateAgentState("myrig/refinery", "working", "")

	// Create the refinery worktree directory.
	worktreeDir := filepath.Join(os.Getenv("GT_HOME"), "myrig", "refinery", "rig")
	os.MkdirAll(worktreeDir, 0o755)

	// Session is dead (not in mock).
	sup := New(cfg, townStore, mock, logger)
	sup.heartbeat()

	// Should have started a session.
	started := mock.GetStarted()
	if len(started) != 1 {
		t.Fatalf("expected 1 session started, got %d", len(started))
	}
	if started[0] != "gt-myrig-refinery" {
		t.Errorf("started session = %q, want %q", started[0], "gt-myrig-refinery")
	}

	// Verify the session was started with the right command by checking agent is working.
	agent, err := townStore.GetAgent("myrig/refinery")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "working" {
		t.Errorf("agent state = %q, want %q", agent.State, "working")
	}
}

func TestRespawnPolecatUnchanged(t *testing.T) {
	townStore := setupTestEnv(t)
	mock := newMockSessions()
	logger := testLogger()
	cfg := testConfig()

	// Create a polecat agent in working state.
	townStore.CreateAgent("Toast", "myrig", "polecat")
	townStore.UpdateAgentState("myrig/Toast", "working", "gt-abc12345")

	// Create the polecat worktree directory.
	worktreeDir := filepath.Join(os.Getenv("GT_HOME"), "myrig", "polecats", "Toast", "rig")
	os.MkdirAll(worktreeDir, 0o755)

	sup := New(cfg, townStore, mock, logger)
	sup.heartbeat()

	// Should have started with polecat session name.
	started := mock.GetStarted()
	if len(started) != 1 {
		t.Fatalf("expected 1 session started, got %d", len(started))
	}
	if started[0] != "gt-myrig-Toast" {
		t.Errorf("started session = %q, want %q", started[0], "gt-myrig-Toast")
	}

	agent, err := townStore.GetAgent("myrig/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "working" {
		t.Errorf("agent state = %q, want %q", agent.State, "working")
	}
}

func TestRespawnCommandByRole(t *testing.T) {
	refineryAgent := store.Agent{Name: "refinery", Rig: "myrig", Role: "refinery"}
	polecatAgent := store.Agent{Name: "Toast", Rig: "myrig", Role: "polecat"}

	// Both roles now use Claude sessions.
	refCmd := respawnCommand(refineryAgent)
	if refCmd != "claude --dangerously-skip-permissions" {
		t.Errorf("refinery command = %q, want %q", refCmd, "claude --dangerously-skip-permissions")
	}

	polCmd := respawnCommand(polecatAgent)
	if polCmd != "claude --dangerously-skip-permissions" {
		t.Errorf("polecat command = %q, want %q", polCmd, "claude --dangerously-skip-permissions")
	}
}

func TestWorktreeForAgentByRole(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)

	refineryAgent := store.Agent{Name: "refinery", Rig: "myrig", Role: "refinery"}
	polecatAgent := store.Agent{Name: "Toast", Rig: "myrig", Role: "polecat"}

	refPath := worktreeForAgent(refineryAgent)
	expected := filepath.Join(dir, "myrig", "refinery", "rig")
	if refPath != expected {
		t.Errorf("refinery worktree = %q, want %q", refPath, expected)
	}

	polPath := worktreeForAgent(polecatAgent)
	expected = filepath.Join(dir, "myrig", "polecats", "Toast", "rig")
	if polPath != expected {
		t.Errorf("polecat worktree = %q, want %q", polPath, expected)
	}
}
