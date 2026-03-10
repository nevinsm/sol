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

	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/quota"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

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

func setupTestEnv(t *testing.T) (*store.Store, *store.Store) {
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

	worldStore, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	t.Cleanup(func() { worldStore.Close() })

	return sphereStore, worldStore
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

func createWrit(t *testing.T, worldStore *store.Store, id, title string) {
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
	if agent.State != "idle" {
		t.Errorf("agent state = %q, want %q", agent.State, "idle")
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

	// Check agent is registered and working.
	time.Sleep(50 * time.Millisecond)
	agent, err := sphereStore.GetAgent("ember/sentinel")
	if err != nil {
		t.Fatalf("GetAgent() error: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("agent state during run = %q, want %q", agent.State, "working")
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
	if agent.State != "idle" {
		t.Errorf("agent state after run = %q, want %q", agent.State, "idle")
	}
}

func TestPatrolHealthyAgents(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create 3 working agents with live sessions and changing output.
	for _, name := range []string{"Toast", "Jasper", "Sage"} {
		sphereStore.CreateAgent(name, "ember", "agent")
		sphereStore.UpdateAgentState("ember/"+name, "working", "sol-"+name)
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

func TestPatrolDetectsStalled(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working agent with a dead session.
	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345")
	createWrit(t, worldStore, "sol-abc12345", "Test task")
	// Session is NOT alive (not in mock.alive).

	// Write tether so stalled detection sees non-empty tether directory.
	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	// Create worktree directory so respawn doesn't fail on missing dir.
	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "ember", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	// Register role so startup.Respawn succeeds.
	startup.Register("agent", startup.RoleConfig{
		WorktreeDir: func(w, a string) string {
			return filepath.Join(os.Getenv("SOL_HOME"), w, "outposts", a, "worktree")
		},
	})
	t.Cleanup(func() { startup.Register("agent", startup.RoleConfig{}) })

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Should have started a session (respawn).
	started := mock.getStarted()
	if len(started) != 1 {
		t.Fatalf("expected 1 session started (respawn), got %d: %v", len(started), started)
	}
	if started[0] != "sol-ember-Toast" {
		t.Errorf("started session = %q, want %q", started[0], "sol-ember-Toast")
	}
}

func TestPatrolMaxRespawns(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRespawns = 2

	// Create a working agent with a dead session.
	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345")
	createWrit(t, worldStore, "sol-abc12345", "Test task")

	// Write tether so stalled detection sees non-empty tether directory.
	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "ember", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	w := New(cfg, sphereStore, worldStore, mock, nil)

	// Pre-set respawn count to max.
	w.respawnCounts[respawnKey{AgentID: "ember/Toast", WritID: "sol-abc12345"}] = 2

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Work should be returned to open, no respawn.
	started := mock.getStarted()
	if len(started) != 0 {
		t.Fatalf("expected 0 sessions started (max respawns), got %d", len(started))
	}

	// Writ should be open.
	item, err := worldStore.GetWrit("sol-abc12345")
	if err != nil {
		t.Fatalf("GetWrit() error: %v", err)
	}
	if item.Status != "open" {
		t.Errorf("writ status = %q, want %q", item.Status, "open")
	}

	// Agent should be idle.
	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatalf("GetAgent() error: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("agent state = %q, want %q", agent.State, "idle")
	}
}

func TestPatrolDetectsZombie(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an idle agent with a live session but no tether.
	sphereStore.CreateAgent("Toast", "ember", "agent")
	// State is idle (default), no tether item.
	mock.alive["sol-ember-Toast"] = true

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Session should have been stopped.
	stopped := mock.getStopped()
	if len(stopped) != 1 {
		t.Fatalf("expected 1 session stopped (zombie), got %d: %v", len(stopped), stopped)
	}
	if stopped[0] != "sol-ember-Toast" {
		t.Errorf("stopped session = %q, want %q", stopped[0], "sol-ember-Toast")
	}
}

func TestPatrolIgnoresIdleClean(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an idle agent with no session and no tether.
	sphereStore.CreateAgent("Toast", "ember", "agent")
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

func TestProgressDetectionOutputChanged(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345")
	mock.alive["sol-ember-Toast"] = true

	assessCalled := false
	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		assessCalled = true
		return &AssessmentResult{Status: "progressing", Confidence: "high", SuggestedAction: "none"}, nil
	}

	// First patrol: establish baseline.
	mock.captures["sol-ember-Toast"] = "output v1"
	w.patrol(context.Background())

	// Second patrol: different output — should NOT trigger assessment.
	mock.captures["sol-ember-Toast"] = "output v2"
	w.patrol(context.Background())

	if assessCalled {
		t.Error("assessment should not be triggered when output changes")
	}
}

func TestProgressDetectionOutputUnchanged(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "same output"

	assessCalled := false
	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		assessCalled = true
		return &AssessmentResult{Status: "progressing", Confidence: "high", SuggestedAction: "none"}, nil
	}

	// First patrol: establish baseline.
	w.patrol(context.Background())

	// Second patrol: same output — should trigger assessment.
	w.patrol(context.Background())

	if !assessCalled {
		t.Error("assessment should be triggered when output is unchanged")
	}
}

func TestAssessmentNudge(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "stuck output"

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "stuck",
			Confidence:      "high",
			SuggestedAction: "nudge",
			NudgeMessage:    "You appear stuck. Try checking the error log.",
		}, nil
	}

	// First patrol: baseline.
	w.patrol(context.Background())
	// Second patrol: same output → assessment → nudge.
	w.patrol(context.Background())

	injected := mock.getInjected()
	if len(injected) != 1 {
		t.Fatalf("expected 1 injection (nudge), got %d", len(injected))
	}
	if injected[0].Text != "You appear stuck. Try checking the error log." {
		t.Errorf("nudge text = %q, want %q", injected[0].Text, "You appear stuck. Try checking the error log.")
	}
}

func TestAssessmentEscalate(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "error output"

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "stuck",
			Confidence:      "high",
			SuggestedAction: "escalate",
			Reason:          "auth token expired",
		}, nil
	}

	// First patrol: baseline.
	w.patrol(context.Background())
	// Second patrol: same output → assessment → escalate.
	w.patrol(context.Background())

	// No nudge should be injected.
	injected := mock.getInjected()
	if len(injected) != 0 {
		t.Errorf("expected 0 injections on escalation, got %d", len(injected))
	}

	// Check that a protocol message was sent (RECOVERY_NEEDED).
	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("expected RECOVERY_NEEDED protocol message to operator")
	}
}

func TestAssessmentNone(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "output"

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "progressing",
			Confidence:      "high",
			SuggestedAction: "none",
			Reason:          "agent is compiling",
		}, nil
	}

	// First patrol: baseline.
	w.patrol(context.Background())
	// Second patrol: same output → assessment → none.
	w.patrol(context.Background())

	// No nudge, no escalation.
	injected := mock.getInjected()
	if len(injected) != 0 {
		t.Errorf("expected 0 injections for action=none, got %d", len(injected))
	}
}

func TestAssessmentLowConfidenceIgnored(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "output"

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "stuck",
			Confidence:      "low",
			SuggestedAction: "nudge",
			NudgeMessage:    "Should not be sent",
		}, nil
	}

	// First patrol: baseline.
	w.patrol(context.Background())
	// Second patrol: same output → assessment → low confidence → no action.
	w.patrol(context.Background())

	injected := mock.getInjected()
	if len(injected) != 0 {
		t.Errorf("expected 0 injections for low confidence, got %d", len(injected))
	}
}

func TestAssessmentFailureNonBlocking(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "output"

	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return nil, fmt.Errorf("AI service unavailable")
	}

	// First patrol: baseline.
	w.patrol(context.Background())
	// Second patrol: same output → assessment → failure → should not crash.
	err := w.patrol(context.Background())

	if err != nil {
		t.Errorf("patrol should succeed even when assessment fails, got error: %v", err)
	}

	// No nudge or escalation.
	injected := mock.getInjected()
	if len(injected) != 0 {
		t.Errorf("expected 0 injections on assessment failure, got %d", len(injected))
	}
}

func TestRespawnAttemptsTracking(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRespawns = 2

	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345")
	createWrit(t, worldStore, "sol-abc12345", "Test task")

	// Write tether so stalled detection sees non-empty tether directory.
	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "ember", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	// Register role so startup.Respawn succeeds.
	startup.Register("agent", startup.RoleConfig{
		WorktreeDir: func(w, a string) string {
			return filepath.Join(os.Getenv("SOL_HOME"), w, "outposts", a, "worktree")
		},
	})
	t.Cleanup(func() { startup.Register("agent", startup.RoleConfig{}) })

	w := New(cfg, sphereStore, worldStore, mock, nil)

	// Patrol 1: stalled → respawn (attempt 1).
	w.patrol(context.Background())
	started := mock.getStarted()
	if len(started) != 1 {
		t.Fatalf("patrol 1: expected 1 start, got %d", len(started))
	}

	// Kill the session.
	mock.mu.Lock()
	delete(mock.alive, "sol-ember-Toast")
	mock.mu.Unlock()

	// Patrol 2: still stalled → respawn (attempt 2).
	w.patrol(context.Background())
	started = mock.getStarted()
	if len(started) != 2 {
		t.Fatalf("patrol 2: expected 2 starts, got %d", len(started))
	}

	// Kill the session again.
	mock.mu.Lock()
	delete(mock.alive, "sol-ember-Toast")
	mock.mu.Unlock()

	// Patrol 3: still stalled → return to open (max reached).
	w.patrol(context.Background())
	started = mock.getStarted()
	if len(started) != 2 {
		t.Fatalf("patrol 3: expected still 2 starts (max reached), got %d", len(started))
	}

	// Agent should be idle, writ open.
	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "idle" {
		t.Errorf("agent state = %q, want %q after max respawns", agent.State, "idle")
	}

	item, err := worldStore.GetWrit("sol-abc12345")
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != "open" {
		t.Errorf("writ status = %q, want %q after max respawns", item.Status, "open")
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "clean JSON",
			input: `{"status":"stuck","confidence":"high","reason":"test","suggested_action":"nudge","nudge_message":"hello"}`,
			want:  "stuck",
		},
		{
			name:  "JSON with surrounding text",
			input: "Here is the analysis:\n{\"status\":\"progressing\",\"confidence\":\"medium\",\"reason\":\"compiling\",\"suggested_action\":\"none\",\"nudge_message\":\"\"}\nEnd of response.",
			want:  "progressing",
		},
		{
			name:    "no JSON",
			input:   "This is just text without any JSON",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := extractJSON([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("extractJSON() error: %v", err)
			}
			if result.Status != tt.want {
				t.Errorf("status = %q, want %q", result.Status, tt.want)
			}
		})
	}
}

func TestSentinelIgnoresEnvoy(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an envoy agent with a dead session.
	sphereStore.CreateAgent("Scout", "ember", "envoy")
	sphereStore.UpdateAgentState("ember/Scout", "working", "sol-envoy123")
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

func TestSentinelIgnoresGovernor(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a governor agent with a dead session.
	sphereStore.CreateAgent("governor", "ember", "governor")
	sphereStore.UpdateAgentState("ember/governor", "working", "")
	// Session is NOT alive.

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No sessions should have been started or stopped.
	if started := mock.getStarted(); len(started) != 0 {
		t.Errorf("expected 0 sessions started for governor, got %d: %v", len(started), started)
	}
	if stopped := mock.getStopped(); len(stopped) != 0 {
		t.Errorf("expected 0 sessions stopped for governor, got %d: %v", len(stopped), stopped)
	}
}

func TestReapIdleAgent(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.IdleReapTimeout = 1 * time.Millisecond // Very short for tests.

	// Create an idle agent with an old UpdatedAt.
	sphereStore.CreateAgent("Toast", "ember", "agent")
	// Agent is idle by default. Make it old.
	now := time.Now().UTC().Add(-1 * time.Hour)
	sphereStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		now.Format(time.RFC3339), "ember/Toast")

	// Create tether to verify cleanup.
	if err := tether.Write("ember", "Toast", "sol-old-item", "agent"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Agent record should be deleted.
	_, err := sphereStore.GetAgent("ember/Toast")
	if err == nil {
		t.Error("expected agent to be deleted after reap, but it still exists")
	}

	// Tether should be cleaned up.
	if tether.IsTethered("ember", "Toast", "agent") {
		t.Error("expected tether to be removed after reap")
	}
}

func TestReapIdleAgentSkipsRecent(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.IdleReapTimeout = 1 * time.Hour // Long timeout.

	// Create a recently-updated idle agent.
	sphereStore.CreateAgent("Toast", "ember", "agent")
	// UpdatedAt is now (recent), so it should not be reaped.

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Agent should still exist.
	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatalf("GetAgent() error: %v — agent was incorrectly reaped", err)
	}
	if agent.State != "idle" {
		t.Errorf("agent state = %q, want %q", agent.State, "idle")
	}
}

func TestReturnWorkToOpenCleansUpResources(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRespawns = 0 // Immediately return to open.

	// Create a working agent with a dead session.
	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345")
	createWrit(t, worldStore, "sol-abc12345", "Test task")

	// Create outpost directory with worktree and tether.
	solHome := os.Getenv("SOL_HOME")
	worktreeDir := filepath.Join(solHome, "ember", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)
	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	// Create session metadata.
	sessDir := filepath.Join(solHome, ".runtime", "sessions")
	os.MkdirAll(sessDir, 0o755)
	os.WriteFile(filepath.Join(sessDir, "sol-ember-Toast.json"), []byte(`{"name":"sol-ember-Toast"}`), 0o644)

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Writ should be open.
	item, err := worldStore.GetWrit("sol-abc12345")
	if err != nil {
		t.Fatalf("GetWrit() error: %v", err)
	}
	if item.Status != "open" {
		t.Errorf("writ status = %q, want %q", item.Status, "open")
	}

	// Tether should be cleared.
	if tether.IsTethered("ember", "Toast", "agent") {
		t.Error("expected tether to be removed")
	}

	// Session metadata should be removed.
	if _, err := os.Stat(filepath.Join(sessDir, "sol-ember-Toast.json")); !os.IsNotExist(err) {
		t.Error("expected session metadata to be removed")
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
	if err := tether.Write("ember", "Ghost", "sol-orphaned", "agent"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Tether should be cleaned up.
	if tether.IsTethered("ember", "Ghost", "agent") {
		t.Error("expected orphaned tether to be removed")
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
		[]byte(`{"name":"sol-ember-Ghost","role":"agent","world":"ember"}`), 0o644)
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
	sphereStore.CreateAgent("Toast", "ember", "agent")
	// Agent is idle by default.

	if err := tether.Write("ember", "Toast", "sol-stale-item", "agent"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Tether should NOT be cleaned up — agent exists in DB (even though idle).
	// Consul's stale-tether recovery handles idle agents with tethers.
	if !tether.IsTethered("ember", "Toast", "agent") {
		t.Error("expected tether to be preserved for idle agent — sentinel only cleans truly orphaned tethers")
	}
}

func TestCleanupOrphanedTetherSkipsWorking(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working agent with a live session and tether.
	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-active")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "working output"

	if err := tether.Write("ember", "Toast", "sol-active", "agent"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Tether should NOT be cleaned up (agent is working).
	if !tether.IsTethered("ember", "Toast", "agent") {
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
	sphereStore.CreateAgent("Toast", "ember", "agent")

	// Write a tether file (simulating Cast() writing the tether).
	if err := tether.Write("ember", "Toast", "sol-active-writ", "agent"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	// Now update agent to "working" AFTER the initial state — simulating
	// Cast() completing the agent state update between sentinel's snapshot
	// and cleanupOrphanedTethers execution.
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-active-writ")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "working output"

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Tether should NOT be cleaned up — agent exists in DB.
	if !tether.IsTethered("ember", "Toast", "agent") {
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
	sphereStore.CreateAgent("Toast", "ember", "agent")
	// Agent stays idle — do NOT update to "working".

	if err := tether.Write("ember", "Toast", "sol-stale-writ", "agent"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Tether should NOT be cleaned up — agent exists in DB (even though idle).
	// Consul's stale-tether recovery handles this case with proper context.
	if !tether.IsTethered("ember", "Toast", "agent") {
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
	if err := tether.Write("ember", "Ghost", "sol-orphan-writ", "agent"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	// Verify tether exists before patrol.
	if !tether.IsTethered("ember", "Ghost", "agent") {
		t.Fatal("expected tether to exist before patrol")
	}

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Tether SHOULD be cleaned up — no agent record in DB (truly orphaned).
	if tether.IsTethered("ember", "Ghost", "agent") {
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
	sphereStore.UpdateAgentState("ember/forge", "working", "")
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

// --- Recast tests ---

// createFailedMR creates a writ and a failed MR for it.
// Transitions through the valid path: ready → claimed → failed.
func createFailedMR(t *testing.T, worldStore *store.Store, writID, title, branch string) string {
	t.Helper()
	createWrit(t, worldStore, writID, title)
	mrID, err := worldStore.CreateMergeRequest(writID, branch, 3)
	if err != nil {
		t.Fatalf("failed to create MR: %v", err)
	}
	if _, err := worldStore.ClaimMergeRequest("test/forge"); err != nil {
		t.Fatalf("failed to claim MR: %v", err)
	}
	if err := worldStore.UpdateMergeRequestPhase(mrID, "failed"); err != nil {
		t.Fatalf("failed to set MR phase to failed: %v", err)
	}
	return mrID
}

func TestReleaseStaleClaims(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.ClaimTTL = 30 * time.Minute

	// Create a writ and MR, then claim it.
	createWrit(t, worldStore, "sol-stale001", "Stale claim test")
	mrID, err := worldStore.CreateMergeRequest("sol-stale001", "outpost/A/sol-stale001", 3)
	if err != nil {
		t.Fatalf("failed to create MR: %v", err)
	}
	claimed, err := worldStore.ClaimMergeRequest("forge-1")
	if err != nil {
		t.Fatalf("failed to claim MR: %v", err)
	}
	if claimed == nil || claimed.ID != mrID {
		t.Fatal("expected to claim the MR")
	}

	// Backdate the claimed_at to make it stale (> 30 min ago).
	staleTime := time.Now().UTC().Add(-45 * time.Minute).Format(time.RFC3339)
	_, err = worldStore.DB().Exec(
		`UPDATE merge_requests SET claimed_at = ? WHERE id = ?`, staleTime, mrID)
	if err != nil {
		t.Fatalf("failed to backdate claimed_at: %v", err)
	}

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// The MR should be back to "ready" phase.
	mr, err := worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("failed to get MR: %v", err)
	}
	if mr.Phase != "ready" {
		t.Errorf("MR phase = %q, want %q", mr.Phase, "ready")
	}
	if mr.ClaimedBy != "" {
		t.Errorf("MR claimed_by = %q, want empty", mr.ClaimedBy)
	}
}

func TestReleaseStaleClaims_SkipsFresh(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.ClaimTTL = 30 * time.Minute

	// Create a writ and MR, then claim it (claimed_at = now, so fresh).
	createWrit(t, worldStore, "sol-fresh001", "Fresh claim test")
	mrID, err := worldStore.CreateMergeRequest("sol-fresh001", "outpost/A/sol-fresh001", 3)
	if err != nil {
		t.Fatalf("failed to create MR: %v", err)
	}
	claimed, err := worldStore.ClaimMergeRequest("forge-1")
	if err != nil {
		t.Fatalf("failed to claim MR: %v", err)
	}
	if claimed == nil || claimed.ID != mrID {
		t.Fatal("expected to claim the MR")
	}

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// The MR should still be claimed — it's fresh.
	mr, err := worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("failed to get MR: %v", err)
	}
	if mr.Phase != "claimed" {
		t.Errorf("MR phase = %q, want %q (claim is fresh, should not be released)", mr.Phase, "claimed")
	}
	if mr.ClaimedBy != "forge-1" {
		t.Errorf("MR claimed_by = %q, want %q", mr.ClaimedBy, "forge-1")
	}
}

// recastNowFunc returns a time function that skips ahead by the given duration,
// sufficient to bypass all cooldown/backoff checks in recast tests.
func recastNowFunc(skip time.Duration) func() time.Time {
	return func() time.Time { return time.Now().Add(skip) }
}

// assertRecastMetadata checks that a writ's metadata has the expected recast count.
func assertRecastMetadata(t *testing.T, worldStore *store.Store, writID string, wantCount int) {
	t.Helper()
	item, err := worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("GetWrit(%q) error: %v", writID, err)
	}
	got := recastCountFromMetadata(item)
	if got != wantCount {
		t.Errorf("recast-count metadata for %q = %d, want %d", writID, got, wantCount)
	}
}

func TestRecastFailedMR(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR with an open writ.
	createFailedMR(t, worldStore, "sol-fail1111", "Failing task", "outpost/Toast/sol-fail1111")

	castCalled := false
	var castWritID string

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(15 * time.Minute)) // skip past cooldown
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		castWritID = writID
		return &CastResult{
			WritID:      writID,
			AgentName:   "Sage",
			SessionName: "sol-ember-Sage",
		}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if !castCalled {
		t.Fatal("expected castFn to be called for failed MR")
	}
	if castWritID != "sol-fail1111" {
		t.Errorf("castFn called with %q, want %q", castWritID, "sol-fail1111")
	}

	// Recast count should be 1 (persisted in metadata).
	assertRecastMetadata(t, worldStore, "sol-fail1111", 1)
}

func TestRecastSkipsNonOpenWrit(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a failed MR but set the writ to "tethered" (already re-dispatched).
	mrID := createFailedMR(t, worldStore, "sol-teth2222", "Already tethered", "outpost/X/sol-teth2222")
	_ = mrID
	worldStore.UpdateWrit("sol-teth2222", store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"})

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when writ is not open")
	}
}

func TestRecastMaxAttemptsEscalates(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 2

	// Create a failed MR with an open writ.
	createFailedMR(t, worldStore, "sol-maxr3333", "Max retries task", "outpost/Toast/sol-maxr3333")

	// Pre-set recast count to max via writ metadata.
	worldStore.SetWritMetadata("sol-maxr3333", map[string]any{
		"recast-count": float64(2),
		"recast-last":  time.Now().UTC().Format(time.RFC3339),
	})

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(2 * time.Hour)) // skip past all backoff
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when max recast attempts reached")
	}

	// Should have sent RECOVERY_NEEDED to operator.
	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("expected RECOVERY_NEEDED protocol message after max recast attempts")
	}

	// Recast count should be incremented past max to prevent re-escalation.
	assertRecastMetadata(t, worldStore, "sol-maxr3333", 3)
}

func TestRecastMaxAttemptsEscalatesOnlyOnce(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 2

	// Create a failed MR with an open writ.
	createFailedMR(t, worldStore, "sol-once4444", "Escalate once", "outpost/Toast/sol-once4444")

	// Pre-set recast count past max via metadata (already escalated).
	worldStore.SetWritMetadata("sol-once4444", map[string]any{
		"recast-count": float64(3),
	})

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(2 * time.Hour))
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No new RECOVERY_NEEDED message.
	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) != 0 {
		t.Error("should not send RECOVERY_NEEDED again after already escalated")
	}
}

func TestRecastNoCastFuncSkips(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a failed MR.
	createFailedMR(t, worldStore, "sol-nocast55", "No cast func", "outpost/X/sol-nocast55")

	// No castFn set.
	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Should complete without error and without panic.
}

func TestRecastDeduplicatesByWrit(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a writ with TWO failed MRs (e.g., two merge attempts).
	createWrit(t, worldStore, "sol-dedup666", "Dedup task")
	mr1, _ := worldStore.CreateMergeRequest("sol-dedup666", "outpost/A/sol-dedup666", 3)
	worldStore.ClaimMergeRequest("test/forge")
	worldStore.UpdateMergeRequestPhase(mr1, "failed")
	mr2, _ := worldStore.CreateMergeRequest("sol-dedup666", "outpost/B/sol-dedup666", 3)
	worldStore.ClaimMergeRequest("test/forge")
	worldStore.UpdateMergeRequestPhase(mr2, "failed")

	castCount := 0
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(15 * time.Minute)) // skip past cooldown
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCount++
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Cast should only be called once despite two failed MRs.
	if castCount != 1 {
		t.Errorf("castFn called %d times, want 1 (deduplication)", castCount)
	}
}

func TestRecastPrunesDedupOnHandledItem(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a failed MR with a "tethered" writ (already re-dispatched).
	createFailedMR(t, worldStore, "sol-prune777", "Already tethered", "outpost/X/sol-prune777")
	worldStore.UpdateWrit("sol-prune777", store.WritUpdates{Status: "tethered", Assignee: "ember/Toast"})

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		return &CastResult{AgentName: "Sage"}, nil
	})

	// Pre-set a dedup guard entry (old enough to pass the dedup check).
	w.lastCastTime["sol-prune777"] = time.Now().Add(-time.Minute)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Dedup guard should be pruned since writ is tethered (handled elsewhere).
	if _, exists := w.lastCastTime["sol-prune777"]; exists {
		t.Error("expected lastCastTime to be pruned for tethered writ")
	}
}

func TestRecastDoneWritNoAssigneeTransitionsToOpen(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR with a "done" writ and no assignee (orphaned).
	createFailedMR(t, worldStore, "sol-done1111", "Orphaned done", "outpost/X/sol-done1111")
	worldStore.UpdateWrit("sol-done1111", store.WritUpdates{Status: "done"})

	castCalled := false
	var castWritID string

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(15 * time.Minute)) // skip past cooldown
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		castWritID = writID
		return &CastResult{
			WritID:    writID,
			AgentName: "Sage",
		}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if !castCalled {
		t.Fatal("expected castFn to be called for done writ with no assignee")
	}
	if castWritID != "sol-done1111" {
		t.Errorf("castFn called with %q, want %q", castWritID, "sol-done1111")
	}

	// Verify writ was transitioned to "open".
	item, err := worldStore.GetWrit("sol-done1111")
	if err != nil {
		t.Fatalf("GetWrit() error: %v", err)
	}
	// After castFn succeeds, dispatch sets the writ status; here we verify
	// sentinel at least transitioned it from "done" (it's now "open" or
	// whatever castFn/dispatch set it to).
	if item.Status == "done" {
		t.Error("writ should no longer be in done status after recast")
	}

	// Recast count should be 1 (persisted in metadata).
	assertRecastMetadata(t, worldStore, "sol-done1111", 1)
}

func TestRecastDoneWritWithAssigneeSkipped(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a failed MR with a "done" writ that has an assignee.
	createFailedMR(t, worldStore, "sol-dassn222", "Done with agent", "outpost/X/sol-dassn222")
	worldStore.UpdateWrit("sol-dassn222", store.WritUpdates{Status: "done", Assignee: "ember/Toast"})

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called for done writ with active assignee")
	}
}

func TestRecastSkipsDuplicateMR(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a writ with a failed MR AND a non-failed MR (e.g., "ready").
	createWrit(t, worldStore, "sol-dupmr333", "Dup MR task")
	failedMR, _ := worldStore.CreateMergeRequest("sol-dupmr333", "outpost/A/sol-dupmr333", 3)
	worldStore.ClaimMergeRequest("test/forge")
	worldStore.UpdateMergeRequestPhase(failedMR, "failed")
	readyMR, _ := worldStore.CreateMergeRequest("sol-dupmr333", "outpost/B/sol-dupmr333", 3)
	_ = readyMR

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(15 * time.Minute)) // skip past cooldown
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when a non-failed MR exists (duplicate prevention)")
	}
}

func TestRecastCastFailureNonBlocking(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a failed MR.
	createFailedMR(t, worldStore, "sol-cfail888", "Cast failure", "outpost/X/sol-cfail888")

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(15 * time.Minute)) // skip past cooldown
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		return nil, fmt.Errorf("no idle agents available")
	})

	// Should not error — cast failure is non-blocking.
	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Recast count should NOT be incremented on failure (metadata unchanged).
	assertRecastMetadata(t, worldStore, "sol-cfail888", 0)
}

// --- Cooldown, backoff, and dedup guard tests ---

func TestRecastCooldownSkipsRecentFailure(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR with an open writ (MR failure is "now").
	createFailedMR(t, worldStore, "sol-cool1111", "Recent failure", "outpost/X/sol-cool1111")

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	// Do NOT set nowFn — default is time.Now, so MR failure is <10 min old.
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when MR failure is less than 10 minutes old")
	}
}

func TestRecastCooldownAllowsOldFailure(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR with an open writ.
	createFailedMR(t, worldStore, "sol-cool2222", "Old failure", "outpost/X/sol-cool2222")

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	// Jump 11 minutes into the future — past the 10-minute cooldown.
	w.SetNowFunc(recastNowFunc(11 * time.Minute))
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if !castCalled {
		t.Error("castFn should be called when MR failure is older than 10 minutes")
	}
}

func TestRecastBackoffDelaysSecondRecast(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR with an open writ.
	createFailedMR(t, worldStore, "sol-back1111", "Backoff test", "outpost/X/sol-back1111")

	// Pre-set: 1 recast already done, last recast 15 min ago.
	recastTime := time.Now().Add(-15 * time.Minute).UTC().Format(time.RFC3339)
	worldStore.SetWritMetadata("sol-back1111", map[string]any{
		"recast-count": float64(1),
		"recast-last":  recastTime,
	})

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	// nowFn not needed — the last recast was 15 min ago but the 2nd recast
	// requires 30 min backoff, so it should be skipped.
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when 30-min backoff has not elapsed")
	}
}

func TestRecastBackoffAllowsAfterElapsed(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR with an open writ.
	createFailedMR(t, worldStore, "sol-back2222", "Backoff elapsed", "outpost/X/sol-back2222")

	// Pre-set: 1 recast already done, last recast 35 min ago (>30 min backoff).
	recastTime := time.Now().Add(-35 * time.Minute).UTC().Format(time.RFC3339)
	worldStore.SetWritMetadata("sol-back2222", map[string]any{
		"recast-count": float64(1),
		"recast-last":  recastTime,
	})

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	// MR failure is "now" but the cooldown check uses mr.UpdatedAt only for attempt 0.
	// For attempt 1+, the backoff check uses recast-last. Since recast-last is 35 min ago
	// and we need 30 min for the 2nd recast, this should pass. But we still need the
	// initial cooldown check to pass (which it does because attempts > 0 uses recast-last).
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if !castCalled {
		t.Error("castFn should be called when 30-min backoff has elapsed")
	}
	assertRecastMetadata(t, worldStore, "sol-back2222", 2)
}

func TestRecastThirdAttemptBackoff60Min(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR.
	createFailedMR(t, worldStore, "sol-back3333", "60m backoff", "outpost/X/sol-back3333")

	// Pre-set: 2 recasts done, last recast 45 min ago (<60 min backoff).
	recastTime := time.Now().Add(-45 * time.Minute).UTC().Format(time.RFC3339)
	worldStore.SetWritMetadata("sol-back3333", map[string]any{
		"recast-count": float64(2),
		"recast-last":  recastTime,
	})

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when 60-min backoff has not elapsed")
	}
}

func TestRecastDeduplicationGuard(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.PatrolInterval = 3 * time.Minute // realistic interval for dedup test

	// Create a failed MR with an open writ.
	createFailedMR(t, worldStore, "sol-dedup111", "Dedup guard", "outpost/X/sol-dedup111")

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetNowFunc(recastNowFunc(15 * time.Minute)) // skip past cooldown

	// Pre-set dedup guard: writ was cast very recently (within 2× patrol interval).
	w.lastCastTime["sol-dedup111"] = w.now().Add(-time.Minute) // 1 min ago, within 6 min window

	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when writ was recently cast (dedup guard)")
	}
}

func TestRecastPersistentCountSurvivesRestart(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR with an open writ.
	createFailedMR(t, worldStore, "sol-pers1111", "Persistent count", "outpost/X/sol-pers1111")

	// First sentinel instance: recast once.
	w1 := New(cfg, sphereStore, worldStore, mock, nil)
	w1.SetNowFunc(recastNowFunc(15 * time.Minute))
	w1.SetCastFunc(func(writID string) (*CastResult, error) {
		return &CastResult{AgentName: "Sage"}, nil
	})
	if err := w1.patrol(context.Background()); err != nil {
		t.Fatalf("w1.patrol() error: %v", err)
	}
	assertRecastMetadata(t, worldStore, "sol-pers1111", 1)

	// Simulate sentinel restart — create a new Sentinel (no in-memory state).
	// Reset writ to open (simulate MR failure cycle).
	worldStore.UpdateWrit("sol-pers1111", store.WritUpdates{Status: "open", Assignee: "-"})

	w2 := New(cfg, sphereStore, worldStore, mock, nil)
	// Jump 35 min to pass the 30-min backoff for the 2nd recast.
	w2.SetNowFunc(recastNowFunc(50 * time.Minute))
	w2.SetCastFunc(func(writID string) (*CastResult, error) {
		return &CastResult{AgentName: "Sage"}, nil
	})
	if err := w2.patrol(context.Background()); err != nil {
		t.Fatalf("w2.patrol() error: %v", err)
	}

	// Count should be 2 (persisted across sentinel restarts).
	assertRecastMetadata(t, worldStore, "sol-pers1111", 2)
}

// --- Orphaned resolution dispatch tests ---

// createBlockedMR creates an original writ with MR, a blocker (resolution) writ,
// and blocks the MR with the blocker writ. Returns the MR ID.
// The blocker writ is created with the given age (time before now).
func createBlockedMR(t *testing.T, worldStore *store.Store, writID, blockerWritID, title, branch string, blockerAge time.Duration) string {
	t.Helper()
	createWrit(t, worldStore, writID, title)
	mrID, err := worldStore.CreateMergeRequest(writID, branch, 3)
	if err != nil {
		t.Fatalf("failed to create MR: %v", err)
	}

	// Create blocker (resolution) writ with a backdated created_at.
	createdAt := time.Now().UTC().Add(-blockerAge).Format(time.RFC3339)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = worldStore.DB().Exec(
		`INSERT INTO writs (id, title, description, status, priority, created_by, created_at, updated_at)
		 VALUES (?, ?, '', 'open', 1, 'forge', ?, ?)`,
		blockerWritID, "Resolve conflict for "+title, createdAt, now,
	)
	if err != nil {
		t.Fatalf("failed to create blocker writ %q: %v", blockerWritID, err)
	}

	if err := worldStore.BlockMergeRequest(mrID, blockerWritID); err != nil {
		t.Fatalf("failed to block MR: %v", err)
	}
	return mrID
}

func TestDispatchOrphanedResolution_HappyPath(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Blocked MR with open+unassigned resolution writ older than 5 min.
	mrID := createBlockedMR(t, worldStore, "sol-orig1111", "sol-res-1111", "Feature A", "outpost/Toast/sol-orig1111", 10*time.Minute)
	_ = mrID

	castCalled := false
	var castWritID string

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		castWritID = writID
		return &CastResult{
			WritID:      writID,
			AgentName:   "Sage",
			SessionName: "sol-ember-Sage",
		}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if !castCalled {
		t.Fatal("expected castFn to be called for orphaned resolution writ")
	}
	if castWritID != "sol-res-1111" {
		t.Errorf("castFn called with %q, want %q", castWritID, "sol-res-1111")
	}

	// Dispatch count should be 1.
	if w.resolutionDispatchCounts["sol-res-1111"] != 1 {
		t.Errorf("resolution dispatch count = %d, want 1", w.resolutionDispatchCounts["sol-res-1111"])
	}
}

func TestDispatchOrphanedResolution_SkipAssigned(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create blocked MR with old resolution writ.
	createBlockedMR(t, worldStore, "sol-orig2222", "sol-res-2222", "Feature B", "outpost/Toast/sol-orig2222", 10*time.Minute)

	// Assign the resolution writ (governor already dispatched it).
	worldStore.UpdateWrit("sol-res-2222", store.WritUpdates{Assignee: "ember/Toast"})

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when blocker writ has assignee")
	}
}

func TestDispatchOrphanedResolution_SkipClosed(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create blocked MR with old resolution writ.
	createBlockedMR(t, worldStore, "sol-orig3333", "sol-res-3333", "Feature C", "outpost/Toast/sol-orig3333", 10*time.Minute)

	// Close the resolution writ (already handled).
	worldStore.UpdateWrit("sol-res-3333", store.WritUpdates{Status: "closed"})

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when blocker writ is closed")
	}
}

func TestDispatchOrphanedResolution_SkipYoung(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create blocked MR with resolution writ only 2 min old (within grace period).
	createBlockedMR(t, worldStore, "sol-orig4444", "sol-res-4444", "Feature D", "outpost/Toast/sol-orig4444", 2*time.Minute)

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when blocker writ is younger than grace period")
	}
}

func TestDispatchOrphanedResolution_AttemptCapEscalates(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 2

	// Create blocked MR with old resolution writ.
	createBlockedMR(t, worldStore, "sol-orig5555", "sol-res-5555", "Feature E", "outpost/Toast/sol-orig5555", 10*time.Minute)

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(writID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	// Pre-set dispatch count to max.
	w.resolutionDispatchCounts["sol-res-5555"] = 2

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when max dispatch attempts reached")
	}

	// Should have sent RECOVERY_NEEDED to operator.
	msgs, err := sphereStore.PendingProtocol("autarch", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("expected RECOVERY_NEEDED protocol message after max dispatch attempts")
	}

	// Dispatch count should be incremented past max to prevent re-escalation.
	if w.resolutionDispatchCounts["sol-res-5555"] != 3 {
		t.Errorf("resolution dispatch count = %d, want %d (max+1)", w.resolutionDispatchCounts["sol-res-5555"], 3)
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

// --- Quota patrol tests ---

// setupQuotaAccount creates an account directory with a .credentials.json file
// and registers the account in the account registry.
func setupQuotaAccount(t *testing.T, handle string) {
	t.Helper()
	solHome := os.Getenv("SOL_HOME")
	dir := filepath.Join(solHome, ".accounts", handle)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Add to account registry so quota patrol can discover it.
	regPath := filepath.Join(solHome, ".accounts", "accounts.json")
	var reg struct {
		Accounts map[string]any `json:"accounts"`
		Default  string         `json:"default"`
	}
	if data, err := os.ReadFile(regPath); err == nil {
		_ = json.Unmarshal(data, &reg)
	}
	if reg.Accounts == nil {
		reg.Accounts = make(map[string]any)
	}
	reg.Accounts[handle] = map[string]string{"config_dir": dir}
	data, _ := json.Marshal(reg)
	os.WriteFile(regPath, data, 0o644)
}

// setupAgentCredentials creates a CLAUDE_CONFIG_DIR with an access-token-only
// .credentials.json and a .account metadata file.
func setupAgentCredentials(t *testing.T, world, role, name, accountHandle string) {
	t.Helper()
	solHome := os.Getenv("SOL_HOME")
	worldDir := filepath.Join(solHome, world)

	// Build configDir the same way config.ClaudeConfigDir does.
	var configDir string
	switch role {
	case "envoy":
		configDir = filepath.Join(worldDir, ".claude-config", "envoys", name)
	case "governor":
		configDir = filepath.Join(worldDir, ".claude-config", "governor", name)
	case "forge":
		configDir = filepath.Join(worldDir, ".claude-config", "forge", name)
	default:
		configDir = filepath.Join(worldDir, ".claude-config", "outposts", name)
	}

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write .account metadata file.
	accountFile := filepath.Join(configDir, ".account")
	if err := os.WriteFile(accountFile, []byte(accountHandle+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write access-token-only credentials (copy from account, no refreshToken).
	srcCreds := filepath.Join(solHome, ".accounts", accountHandle, ".credentials.json")
	data, err := os.ReadFile(srcCreds)
	if err != nil {
		// If source doesn't exist yet, write minimal creds.
		data = []byte(`{}`)
	}
	destCreds := filepath.Join(configDir, ".credentials.json")
	if err := os.WriteFile(destCreds, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestQuotaPatrolNoRateLimits(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create agent and session.
	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-work-1")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "Working on task..."

	setupQuotaAccount(t, "alice")
	setupAgentCredentials(t, "ember", "agent", "Toast", "alice")

	w := New(cfg, sphereStore, worldStore, mock, nil)

	agents, _ := sphereStore.ListAgents("ember", "")
	scanned, rotated, paused := w.quotaPatrol(agents)

	if scanned != 1 {
		t.Errorf("scanned = %d, want 1", scanned)
	}
	if rotated != 0 {
		t.Errorf("rotated = %d, want 0", rotated)
	}
	if paused != 0 {
		t.Errorf("paused = %d, want 0", paused)
	}
}

func TestQuotaPatrolRotatesEntireWorld(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create two accounts: alice (will be rate-limited), bob (available).
	setupQuotaAccount(t, "alice")
	setupQuotaAccount(t, "bob")

	// Create agents across roles.
	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-work-1")
	setupAgentCredentials(t, "ember", "agent", "Toast", "alice")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "You've hit your usage limit · resets 3:45pm"

	sphereStore.CreateAgent("forge", "ember", "forge")
	sphereStore.UpdateAgentState("ember/forge", "working", "")
	setupAgentCredentials(t, "ember", "forge", "forge", "alice")
	mock.alive["sol-ember-forge"] = true
	mock.captures["sol-ember-forge"] = "Processing merge requests..."

	sphereStore.CreateAgent("governor", "ember", "governor")
	sphereStore.UpdateAgentState("ember/governor", "working", "")
	setupAgentCredentials(t, "ember", "governor", "governor", "alice")
	mock.alive["sol-ember-governor"] = true
	mock.captures["sol-ember-governor"] = "Idle, waiting for work..."

	// Also set up workdir so that Cycle can build command.
	solHome := os.Getenv("SOL_HOME")
	for _, dir := range []string{
		filepath.Join(solHome, "ember", "outposts", "Toast", "worktree"),
		filepath.Join(solHome, "ember", "forge", "worktree"),
		filepath.Join(solHome, "ember", "governor"),
	} {
		os.MkdirAll(dir, 0o755)
	}

	// Register roles so the startup path succeeds.
	for _, role := range []string{"agent", "forge", "governor"} {
		r := role
		startup.Register(r, startup.RoleConfig{
			WorktreeDir: func(w, a string) string {
				return agentWorkdir(w, store.Agent{Name: a, Role: r})
			},
		})
	}
	t.Cleanup(func() {
		for _, r := range []string{"agent", "forge", "governor"} {
			startup.Register(r, startup.RoleConfig{})
		}
	})

	w := New(cfg, sphereStore, worldStore, mock, nil)

	agents, _ := sphereStore.ListAgents("ember", "")
	scanned, rotated, paused := w.quotaPatrol(agents)

	if scanned != 3 {
		t.Errorf("scanned = %d, want 3", scanned)
	}
	// Toast is rate-limited; forge and governor are on alice too.
	// All 3 should be cycled to bob.
	if rotated != 3 {
		t.Errorf("rotated = %d, want 3", rotated)
	}
	if paused != 0 {
		t.Errorf("paused = %d, want 0", paused)
	}

	// All three sessions should have been cycled.
	cycled := mock.getCycled()
	if len(cycled) != 3 {
		t.Errorf("cycled sessions = %d, want 3", len(cycled))
	}
}

func TestQuotaPatrolPausesWhenNoAccountsAvailable(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create only one account: alice (will be rate-limited, no alternative).
	setupQuotaAccount(t, "alice")

	// Create agents.
	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-work-1")
	setupAgentCredentials(t, "ember", "agent", "Toast", "alice")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "You've hit your usage limit"

	sphereStore.CreateAgent("governor", "ember", "governor")
	sphereStore.UpdateAgentState("ember/governor", "working", "")
	setupAgentCredentials(t, "ember", "governor", "governor", "alice")
	mock.alive["sol-ember-governor"] = true
	mock.captures["sol-ember-governor"] = "Idle..."

	w := New(cfg, sphereStore, worldStore, mock, nil)

	agents, _ := sphereStore.ListAgents("ember", "")
	scanned, rotated, paused := w.quotaPatrol(agents)

	if scanned != 2 {
		t.Errorf("scanned = %d, want 2", scanned)
	}
	if rotated != 0 {
		t.Errorf("rotated = %d, want 0", rotated)
	}
	// Only Toast should be paused — governor is exempt.
	if paused != 1 {
		t.Errorf("paused = %d, want 1", paused)
	}

	stopped := mock.getStopped()
	if len(stopped) != 1 || stopped[0] != "sol-ember-Toast" {
		t.Errorf("stopped = %v, want [sol-ember-Toast]", stopped)
	}
}

func TestQuotaPatrolGovernorNeverPaused(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Only one account — will be limited.
	setupQuotaAccount(t, "alice")

	// Only governor is running (on the limited account).
	sphereStore.CreateAgent("governor", "ember", "governor")
	sphereStore.UpdateAgentState("ember/governor", "working", "")
	setupAgentCredentials(t, "ember", "governor", "governor", "alice")
	mock.alive["sol-ember-governor"] = true
	mock.captures["sol-ember-governor"] = "You've hit your usage limit"

	w := New(cfg, sphereStore, worldStore, mock, nil)

	agents, _ := sphereStore.ListAgents("ember", "")
	_, _, paused := w.quotaPatrol(agents)

	if paused != 0 {
		t.Errorf("paused = %d, want 0 (governor never paused)", paused)
	}

	stopped := mock.getStopped()
	if len(stopped) != 0 {
		t.Errorf("stopped = %v, want empty (governor never stopped)", stopped)
	}
}

func TestQuotaPatrolSkipsAgentsWithoutSession(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	setupQuotaAccount(t, "alice")

	// Agent exists but has no live session.
	sphereStore.CreateAgent("Ghost", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Ghost", "idle", "")
	// No mock.alive entry — session doesn't exist.

	w := New(cfg, sphereStore, worldStore, mock, nil)

	agents, _ := sphereStore.ListAgents("ember", "")
	scanned, rotated, paused := w.quotaPatrol(agents)

	if scanned != 0 {
		t.Errorf("scanned = %d, want 0", scanned)
	}
	if rotated != 0 {
		t.Errorf("rotated = %d, want 0", rotated)
	}
	if paused != 0 {
		t.Errorf("paused = %d, want 0", paused)
	}
}

func TestQuotaPatrolUsesStartupPathForRegisteredRole(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	solHome := os.Getenv("SOL_HOME")

	// Create two accounts: alice (will be rate-limited), bob (available).
	setupQuotaAccount(t, "alice")
	setupQuotaAccount(t, "bob")

	// Create outpost agent on alice.
	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-work-1")
	setupAgentCredentials(t, "ember", "agent", "Toast", "alice")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "You've hit your usage limit · resets 3:45pm"

	// Create worktree directory (startup needs it).
	worktreeDir := filepath.Join(solHome, "ember", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	// Register the "agent" role with a system prompt so we can verify
	// the startup path adds --append-system-prompt-file.
	roleName := "agent"
	startup.Register(roleName, startup.RoleConfig{
		WorktreeDir: func(w, a string) string {
			return filepath.Join(os.Getenv("SOL_HOME"), w, "outposts", a, "worktree")
		},
		SystemPromptContent: "You are a test agent.",
	})
	t.Cleanup(func() {
		// Deregister to avoid polluting other tests.
		startup.Register(roleName, startup.RoleConfig{})
	})

	w := New(cfg, sphereStore, worldStore, mock, nil)

	agents, _ := sphereStore.ListAgents("ember", "")
	scanned, rotated, paused := w.quotaPatrol(agents)

	if scanned != 1 {
		t.Errorf("scanned = %d, want 1", scanned)
	}
	if rotated != 1 {
		t.Errorf("rotated = %d, want 1", rotated)
	}
	if paused != 0 {
		t.Errorf("paused = %d, want 0", paused)
	}

	// Verify the session was cycled (startup uses SessionOp which calls Cycle).
	cycled := mock.getCycled()
	if len(cycled) != 1 {
		t.Fatalf("cycled sessions = %d, want 1", len(cycled))
	}

	// The command should include --continue (quota rotation preserves conversation).
	cmd := mock.getLastCmd("sol-ember-Toast")
	if !strings.Contains(cmd, "--continue") {
		t.Errorf("expected --continue in command, got %q", cmd)
	}

	// The command should include system prompt flag from the registered role.
	if !strings.Contains(cmd, "--append-system-prompt-file") {
		t.Errorf("expected --append-system-prompt-file in command, got %q", cmd)
	}
}

func TestCheckQuotaPausedUsesStartupPathForRegisteredRole(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	solHome := os.Getenv("SOL_HOME")

	// Set up two accounts: alice (was limited, now expired), bob (available).
	setupQuotaAccount(t, "alice")
	setupQuotaAccount(t, "bob")

	// Create worktree directory.
	worktreeDir := filepath.Join(solHome, "ember", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	// Register the role.
	roleName := "agent"
	startup.Register(roleName, startup.RoleConfig{
		WorktreeDir: func(w, a string) string {
			return filepath.Join(os.Getenv("SOL_HOME"), w, "outposts", a, "worktree")
		},
		SystemPromptContent: "You are a test agent.",
	})
	t.Cleanup(func() {
		startup.Register(roleName, startup.RoleConfig{})
	})

	// Set up a paused session in quota state.
	lock, state, err := quota.AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	state.PausedSessions["ember/Toast"] = quota.PausedSession{
		PausedAt:        time.Now().Add(-5 * time.Minute).UTC(),
		PreviousAccount: "alice",
		Writ:        "sol-work-1",
		World:           "ember",
		AgentName:       "Toast",
		Role:            "agent",
	}
	// bob is available.
	state.MarkAvailable("bob")
	if err := quota.Save(state); err != nil {
		t.Fatal(err)
	}
	lock.Release()

	// Create agent record.
	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-work-1")
	setupAgentCredentials(t, "ember", "agent", "Toast", "alice")

	w := New(cfg, sphereStore, worldStore, mock, nil)

	restarted := w.checkQuotaPaused()

	if restarted != 1 {
		t.Errorf("restarted = %d, want 1", restarted)
	}

	// Verify session was started (not cycled — paused sessions use Start).
	started := mock.getStarted()
	if len(started) != 1 {
		t.Fatalf("started sessions = %d, want 1", len(started))
	}

	// The command should include --continue and system prompt flag.
	cmd := mock.getLastCmd("sol-ember-Toast")
	if !strings.Contains(cmd, "--continue") {
		t.Errorf("expected --continue in command, got %q", cmd)
	}
	if !strings.Contains(cmd, "--append-system-prompt-file") {
		t.Errorf("expected --append-system-prompt-file in command, got %q", cmd)
	}
}

func TestPatrolReapsAgentTetheredToClosedWrit(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working agent with a live session tethered to a writ.
	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345")
	createWrit(t, worldStore, "sol-abc12345", "Cancelled task")
	mock.alive["sol-ember-Toast"] = true

	// Write tether file on disk so patrol discovers it via tether.List().
	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	// Close the writ with a reason.
	if _, err := worldStore.CloseWrit("sol-abc12345", "superseded"); err != nil {
		t.Fatalf("CloseWrit() error: %v", err)
	}

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Session should have been stopped.
	stopped := mock.getStopped()
	if len(stopped) != 1 {
		t.Fatalf("expected 1 session stopped, got %d: %v", len(stopped), stopped)
	}
	if stopped[0] != "sol-ember-Toast" {
		t.Errorf("stopped session = %q, want %q", stopped[0], "sol-ember-Toast")
	}

	// Agent record should be deleted.
	_, err := sphereStore.GetAgent("ember/Toast")
	if err == nil {
		t.Error("expected agent to be deleted, but GetAgent succeeded")
	}
}

func TestPatrolDoesNotReapAgentTetheredToOpenWrit(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working agent with a live session tethered to an open writ.
	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345")
	createWrit(t, worldStore, "sol-abc12345", "Active task")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "working on task..."

	// Write tether file on disk.
	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No sessions should have been stopped.
	if stopped := mock.getStopped(); len(stopped) != 0 {
		t.Errorf("expected 0 sessions stopped, got %d: %v", len(stopped), stopped)
	}

	// Agent should still exist and be working.
	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatalf("GetAgent() error: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("agent state = %q, want %q", agent.State, "working")
	}
}

func TestPatrolClosedWritReapLogsCloseReason(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a logger that writes to a temp file.
	solHome := os.Getenv("SOL_HOME")
	logger := events.NewLogger(solHome)

	// Create a working agent tethered to a closed writ.
	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345")
	createWrit(t, worldStore, "sol-abc12345", "Cancelled task")
	mock.alive["sol-ember-Toast"] = true

	// Write tether file on disk.
	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	if _, err := worldStore.CloseWrit("sol-abc12345", "cancelled_by_governor"); err != nil {
		t.Fatalf("CloseWrit() error: %v", err)
	}

	w := New(cfg, sphereStore, worldStore, mock, logger)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Read the events log and verify close_reason appears.
	eventsFile := filepath.Join(solHome, ".events.jsonl")
	data, err := os.ReadFile(eventsFile)
	if err != nil {
		t.Fatalf("failed to read events file: %v", err)
	}

	logContent := string(data)
	if !strings.Contains(logContent, "cancelled_by_governor") {
		t.Errorf("expected close_reason 'cancelled_by_governor' in events log, got:\n%s", logContent)
	}
	if !strings.Contains(logContent, `"type":"reap"`) {
		t.Errorf("expected reap event in events log, got:\n%s", logContent)
	}
}

func TestPersistentAgentClosedTetherRemoved(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a persistent (forge) agent with 3 tethered writs.
	sphereStore.CreateAgent("forge", "ember", "forge")
	sphereStore.UpdateAgentState("ember/forge", "working", "sol-writ-1")
	mock.alive["sol-ember-forge"] = true
	mock.captures["sol-ember-forge"] = "forge output"

	createWrit(t, worldStore, "sol-writ-1", "Open writ 1")
	createWrit(t, worldStore, "sol-writ-2", "Closed writ 2")
	createWrit(t, worldStore, "sol-writ-3", "Open writ 3")

	// Write 3 tether files.
	for _, wid := range []string{"sol-writ-1", "sol-writ-2", "sol-writ-3"} {
		if err := tether.Write("ember", "forge", wid, "forge"); err != nil {
			t.Fatalf("tether.Write(%s) error: %v", wid, err)
		}
	}

	// Close writ 2.
	if _, err := worldStore.CloseWrit("sol-writ-2", "superseded"); err != nil {
		t.Fatalf("CloseWrit() error: %v", err)
	}

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Only the closed writ tether should be removed.
	remaining, err := tether.List("ember", "forge", "forge")
	if err != nil {
		t.Fatalf("tether.List() error: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 tethers remaining, got %d: %v", len(remaining), remaining)
	}
	for _, wid := range remaining {
		if wid == "sol-writ-2" {
			t.Error("closed writ tether should have been removed, but sol-writ-2 still present")
		}
	}

	// Agent should still exist and be working.
	agent, err := sphereStore.GetAgent("ember/forge")
	if err != nil {
		t.Fatalf("GetAgent() error: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("agent state = %q, want %q (persistent agent should not be reaped)", agent.State, "working")
	}

	// No sessions should have been stopped.
	if stopped := mock.getStopped(); len(stopped) != 0 {
		t.Errorf("expected 0 sessions stopped (persistent agent), got %d: %v", len(stopped), stopped)
	}
}

func TestOutpostClosedTetherFullReap(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an outpost agent tethered to a closed writ.
	sphereStore.CreateAgent("Toast", "ember", "agent")
	sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345")
	createWrit(t, worldStore, "sol-abc12345", "Closed task")
	mock.alive["sol-ember-Toast"] = true

	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	if _, err := worldStore.CloseWrit("sol-abc12345", "completed"); err != nil {
		t.Fatalf("CloseWrit() error: %v", err)
	}

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Session should have been stopped.
	stopped := mock.getStopped()
	if len(stopped) != 1 {
		t.Fatalf("expected 1 session stopped, got %d: %v", len(stopped), stopped)
	}

	// Agent record should be deleted (full reap).
	_, err := sphereStore.GetAgent("ember/Toast")
	if err == nil {
		t.Error("expected agent to be deleted after reap, but GetAgent succeeded")
	}

	// Tether should be cleaned up.
	if tether.IsTethered("ember", "Toast", "agent") {
		t.Error("expected tether to be cleaned up after reap")
	}
}

func TestOrphanedTetherDirectoryCleaned(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Write multiple tether files for an agent that does NOT exist in the DB.
	// This is a truly orphaned agent — deleted from DB but tether dir remains.
	for _, wid := range []string{"sol-orphan-1", "sol-orphan-2", "sol-orphan-3"} {
		if err := tether.Write("ember", "Ghost", wid, "agent"); err != nil {
			t.Fatalf("tether.Write(%s) error: %v", wid, err)
		}
	}

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// All tether files should be cleaned up — agent has no DB record (truly orphaned).
	if tether.IsTethered("ember", "Ghost", "agent") {
		t.Error("expected all orphaned tether files to be removed for non-existent agent")
	}

	remaining, err := tether.List("ember", "Ghost", "agent")
	if err != nil {
		t.Fatalf("tether.List() error: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 tether files remaining, got %d: %v", len(remaining), remaining)
	}
}
