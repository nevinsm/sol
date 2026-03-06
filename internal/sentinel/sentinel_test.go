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

	"github.com/nevinsm/sol/internal/store"
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
}

type injectCall struct {
	Session string
	Text    string
}

func newMockSessions() *mockSessions {
	return &mockSessions{
		alive:    make(map[string]bool),
		captures: make(map[string]string),
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

func createWorkItem(t *testing.T, worldStore *store.Store, id, title string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := worldStore.DB().Exec(
		`INSERT INTO work_items (id, title, description, status, priority, created_by, created_at, updated_at)
		 VALUES (?, ?, '', 'open', 3, 'test', ?, ?)`,
		id, title, now, now,
	)
	if err != nil {
		t.Fatalf("failed to create work item %q: %v", id, err)
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
	createWorkItem(t, worldStore, "sol-abc12345", "Test task")
	// Session is NOT alive (not in mock.alive).

	// Create worktree directory so respawn doesn't fail on missing dir.
	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "ember", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

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
	createWorkItem(t, worldStore, "sol-abc12345", "Test task")

	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "ember", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	w := New(cfg, sphereStore, worldStore, mock, nil)

	// Pre-set respawn count to max.
	w.respawnCounts[respawnKey{AgentID: "ember/Toast", WorkItemID: "sol-abc12345"}] = 2

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Work should be returned to open, no respawn.
	started := mock.getStarted()
	if len(started) != 0 {
		t.Fatalf("expected 0 sessions started (max respawns), got %d", len(started))
	}

	// Work item should be open.
	item, err := worldStore.GetWorkItem("sol-abc12345")
	if err != nil {
		t.Fatalf("GetWorkItem() error: %v", err)
	}
	if item.Status != "open" {
		t.Errorf("work item status = %q, want %q", item.Status, "open")
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
	msgs, err := sphereStore.PendingProtocol("operator", "RECOVERY_NEEDED")
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
	createWorkItem(t, worldStore, "sol-abc12345", "Test task")

	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "ember", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

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

	// Agent should be idle, work item open.
	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "idle" {
		t.Errorf("agent state = %q, want %q after max respawns", agent.State, "idle")
	}

	item, err := worldStore.GetWorkItem("sol-abc12345")
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != "open" {
		t.Errorf("work item status = %q, want %q after max respawns", item.Status, "open")
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

	// Create outpost directory with a tether to verify cleanup.
	outpostDir := filepath.Join(os.Getenv("SOL_HOME"), "ember", "outposts", "Toast")
	os.MkdirAll(outpostDir, 0o755)
	os.WriteFile(filepath.Join(outpostDir, ".tether"), []byte("sol-old-item"), 0o644)

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Agent record should be deleted.
	_, err := sphereStore.GetAgent("ember/Toast")
	if err == nil {
		t.Error("expected agent to be deleted after reap, but it still exists")
	}

	// Tether file should be cleaned up.
	if _, err := os.Stat(filepath.Join(outpostDir, ".tether")); !os.IsNotExist(err) {
		t.Error("expected tether file to be removed after reap")
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
	createWorkItem(t, worldStore, "sol-abc12345", "Test task")

	// Create outpost directory with worktree and tether.
	solHome := os.Getenv("SOL_HOME")
	worktreeDir := filepath.Join(solHome, "ember", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)
	tetherPath := filepath.Join(solHome, "ember", "outposts", "Toast", ".tether")
	os.WriteFile(tetherPath, []byte("sol-abc12345"), 0o644)

	// Create session metadata.
	sessDir := filepath.Join(solHome, ".runtime", "sessions")
	os.MkdirAll(sessDir, 0o755)
	os.WriteFile(filepath.Join(sessDir, "sol-ember-Toast.json"), []byte(`{"name":"sol-ember-Toast"}`), 0o644)

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Work item should be open.
	item, err := worldStore.GetWorkItem("sol-abc12345")
	if err != nil {
		t.Fatalf("GetWorkItem() error: %v", err)
	}
	if item.Status != "open" {
		t.Errorf("work item status = %q, want %q", item.Status, "open")
	}

	// Tether should be cleared.
	if _, err := os.Stat(tetherPath); !os.IsNotExist(err) {
		t.Error("expected tether file to be removed")
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
	// Also create a tether file.
	os.WriteFile(filepath.Join(solHome, "ember", "outposts", "Ghost", ".tether"),
		[]byte("sol-orphaned"), 0o644)

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Tether should be cleaned up.
	tetherPath := filepath.Join(solHome, "ember", "outposts", "Ghost", ".tether")
	if _, err := os.Stat(tetherPath); !os.IsNotExist(err) {
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

	// Create an idle agent WITH a tether file (stale tether from failed cleanup).
	sphereStore.CreateAgent("Toast", "ember", "agent")
	// Agent is idle by default.

	solHome := os.Getenv("SOL_HOME")
	outpostDir := filepath.Join(solHome, "ember", "outposts", "Toast")
	os.MkdirAll(outpostDir, 0o755)
	tetherPath := filepath.Join(outpostDir, ".tether")
	os.WriteFile(tetherPath, []byte("sol-stale-item"), 0o644)

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Tether should be cleaned up (agent exists but is not working).
	if _, err := os.Stat(tetherPath); !os.IsNotExist(err) {
		t.Error("expected orphaned tether to be removed for idle agent")
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

	solHome := os.Getenv("SOL_HOME")
	outpostDir := filepath.Join(solHome, "ember", "outposts", "Toast")
	os.MkdirAll(outpostDir, 0o755)
	tetherPath := filepath.Join(outpostDir, ".tether")
	os.WriteFile(tetherPath, []byte("sol-active"), 0o644)

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Tether should NOT be cleaned up (agent is working).
	if _, err := os.Stat(tetherPath); os.IsNotExist(err) {
		t.Error("tether for working agent should not be removed")
	}
}

func TestPatrolMonitorsForge(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working forge agent with a live session.
	sphereStore.CreateAgent("forge", "ember", "forge")
	sphereStore.UpdateAgentState("ember/forge", "working", "")
	mock.alive["sol-ember-forge"] = true
	mock.captures["sol-ember-forge"] = "forge output"

	assessCalled := false
	w := New(cfg, sphereStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		assessCalled = true
		return &AssessmentResult{Status: "progressing", Confidence: "high", SuggestedAction: "none"}, nil
	}

	// First patrol: establish baseline.
	w.patrol(context.Background())
	// Second patrol: same output → should trigger assessment (forge is monitored).
	w.patrol(context.Background())

	if !assessCalled {
		t.Error("expected forge to be monitored and assessed when output is unchanged")
	}
}

func TestPatrolDetectsForgeStalled(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working forge agent with a dead session.
	sphereStore.CreateAgent("forge", "ember", "forge")
	sphereStore.UpdateAgentState("ember/forge", "working", "")
	// Session is NOT alive.

	// Create worktree directory for respawn.
	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "ember", "outposts", "forge", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Should have started a session (respawn).
	started := mock.getStarted()
	if len(started) != 1 {
		t.Fatalf("expected 1 session started (forge respawn), got %d: %v", len(started), started)
	}
	if started[0] != "sol-ember-forge" {
		t.Errorf("started session = %q, want %q", started[0], "sol-ember-forge")
	}
}

func TestPatrolForgeMaxRespawns(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRespawns = 2

	// Create a working forge agent with a dead session.
	sphereStore.CreateAgent("forge", "ember", "forge")
	sphereStore.UpdateAgentState("ember/forge", "working", "")
	// Session is NOT alive.

	w := New(cfg, sphereStore, nil, mock, nil)

	// Pre-set respawn count to max.
	w.respawnCounts[respawnKey{AgentID: "ember/forge", WorkItemID: ""}] = 2

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No respawn should happen.
	started := mock.getStarted()
	if len(started) != 0 {
		t.Fatalf("expected 0 sessions started (forge max respawns), got %d", len(started))
	}

	// Forge should be idle.
	agent, err := sphereStore.GetAgent("ember/forge")
	if err != nil {
		t.Fatalf("GetAgent() error: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("forge state = %q, want %q after max respawns", agent.State, "idle")
	}

	// Should have sent RECOVERY_NEEDED to operator.
	msgs, err := sphereStore.PendingProtocol("operator", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("expected RECOVERY_NEEDED protocol message to operator after forge max respawns")
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

// createFailedMR creates a work item and a failed MR for it.
func createFailedMR(t *testing.T, worldStore *store.Store, workItemID, title, branch string) string {
	t.Helper()
	createWorkItem(t, worldStore, workItemID, title)
	mrID, err := worldStore.CreateMergeRequest(workItemID, branch, 3)
	if err != nil {
		t.Fatalf("failed to create MR: %v", err)
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

	// Create a work item and MR, then claim it.
	createWorkItem(t, worldStore, "sol-stale001", "Stale claim test")
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

	// Create a work item and MR, then claim it (claimed_at = now, so fresh).
	createWorkItem(t, worldStore, "sol-fresh001", "Fresh claim test")
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

func TestRecastFailedMR(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 3

	// Create a failed MR with an open work item.
	createFailedMR(t, worldStore, "sol-fail1111", "Failing task", "outpost/Toast/sol-fail1111")

	castCalled := false
	var castWorkItemID string

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(workItemID string) (*CastResult, error) {
		castCalled = true
		castWorkItemID = workItemID
		return &CastResult{
			WorkItemID:  workItemID,
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
	if castWorkItemID != "sol-fail1111" {
		t.Errorf("castFn called with %q, want %q", castWorkItemID, "sol-fail1111")
	}

	// Recast count should be 1.
	if w.recastCounts["sol-fail1111"] != 1 {
		t.Errorf("recast count = %d, want 1", w.recastCounts["sol-fail1111"])
	}
}

func TestRecastSkipsNonOpenWorkItem(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a failed MR but set the work item to "tethered" (already re-dispatched).
	mrID := createFailedMR(t, worldStore, "sol-teth2222", "Already tethered", "outpost/X/sol-teth2222")
	_ = mrID
	worldStore.UpdateWorkItem("sol-teth2222", store.WorkItemUpdates{Status: "tethered", Assignee: "ember/Toast"})

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(workItemID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when work item is not open")
	}
}

func TestRecastMaxAttemptsEscalates(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 2

	// Create a failed MR with an open work item.
	createFailedMR(t, worldStore, "sol-maxr3333", "Max retries task", "outpost/Toast/sol-maxr3333")

	castCalled := false
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(workItemID string) (*CastResult, error) {
		castCalled = true
		return &CastResult{AgentName: "Sage"}, nil
	})

	// Pre-set recast count to max.
	w.recastCounts["sol-maxr3333"] = 2

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	if castCalled {
		t.Error("castFn should NOT be called when max recast attempts reached")
	}

	// Should have sent RECOVERY_NEEDED to operator.
	msgs, err := sphereStore.PendingProtocol("operator", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("expected RECOVERY_NEEDED protocol message after max recast attempts")
	}

	// Recast count should be incremented past max to prevent re-escalation.
	if w.recastCounts["sol-maxr3333"] != 3 {
		t.Errorf("recast count = %d, want %d (max+1)", w.recastCounts["sol-maxr3333"], 3)
	}
}

func TestRecastMaxAttemptsEscalatesOnlyOnce(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRecastAttempts = 2

	// Create a failed MR with an open work item.
	createFailedMR(t, worldStore, "sol-once4444", "Escalate once", "outpost/Toast/sol-once4444")

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(workItemID string) (*CastResult, error) {
		return &CastResult{AgentName: "Sage"}, nil
	})

	// Pre-set recast count past max (already escalated).
	w.recastCounts["sol-once4444"] = 3

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No new RECOVERY_NEEDED message.
	msgs, err := sphereStore.PendingProtocol("operator", "RECOVERY_NEEDED")
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

func TestRecastDeduplicatesByWorkItem(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a work item with TWO failed MRs (e.g., two merge attempts).
	createWorkItem(t, worldStore, "sol-dedup666", "Dedup task")
	mr1, _ := worldStore.CreateMergeRequest("sol-dedup666", "outpost/A/sol-dedup666", 3)
	worldStore.UpdateMergeRequestPhase(mr1, "failed")
	mr2, _ := worldStore.CreateMergeRequest("sol-dedup666", "outpost/B/sol-dedup666", 3)
	worldStore.UpdateMergeRequestPhase(mr2, "failed")

	castCount := 0
	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(workItemID string) (*CastResult, error) {
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

func TestRecastPrunesCountOnHandledItem(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a failed MR with a "done" work item (already resolved).
	createFailedMR(t, worldStore, "sol-prune777", "Already done", "outpost/X/sol-prune777")
	worldStore.UpdateWorkItem("sol-prune777", store.WorkItemUpdates{Status: "done"})

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(workItemID string) (*CastResult, error) {
		return &CastResult{AgentName: "Sage"}, nil
	})

	// Pre-set a recast count.
	w.recastCounts["sol-prune777"] = 2

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Recast count should be pruned since work item is no longer open.
	if _, exists := w.recastCounts["sol-prune777"]; exists {
		t.Error("expected recast count to be pruned for non-open work item")
	}
}

func TestRecastCastFailureNonBlocking(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a failed MR.
	createFailedMR(t, worldStore, "sol-cfail888", "Cast failure", "outpost/X/sol-cfail888")

	w := New(cfg, sphereStore, worldStore, mock, nil)
	w.SetCastFunc(func(workItemID string) (*CastResult, error) {
		return nil, fmt.Errorf("no idle agents available")
	})

	// Should not error — cast failure is non-blocking.
	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Recast count should NOT be incremented on failure.
	if w.recastCounts["sol-cfail888"] != 0 {
		t.Errorf("recast count = %d, want 0 (cast failed)", w.recastCounts["sol-cfail888"])
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
		recastCounts:  make(map[string]int),
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
		recastCounts:  make(map[string]int),
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

// setupAgentCredentials creates a CLAUDE_CONFIG_DIR with a symlinked .credentials.json.
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

	target := filepath.Join(solHome, ".accounts", accountHandle, ".credentials.json")
	link := filepath.Join(configDir, ".credentials.json")
	if err := os.Symlink(target, link); err != nil {
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
