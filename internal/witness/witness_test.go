package witness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/gt/internal/store"
)

// --- Mock implementations ---

type mockSessions struct {
	mu       sync.Mutex
	alive    map[string]bool
	captures map[string]string // session name → captured output
	started  []string
	stopped  []string
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

func (m *mockSessions) Inject(name string, text string) error {
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

	rigStore, err := store.OpenRig("testrig")
	if err != nil {
		t.Fatalf("failed to open rig store: %v", err)
	}
	t.Cleanup(func() { rigStore.Close() })

	return townStore, rigStore
}

func testConfig() Config {
	return Config{
		Rig:            "testrig",
		PatrolInterval: 50 * time.Millisecond, // Fast for tests.
		MaxRespawns:    2,
		CaptureLines:   80,
		AssessCommand:  "claude -p",
		GTHome:         os.Getenv("GT_HOME"),
	}
}

func createWorkItem(t *testing.T, rigStore *store.Store, id, title string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := rigStore.DB().Exec(
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
	townStore, rigStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	w := New(cfg, townStore, rigStore, mock, nil)

	if err := w.Register(); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	agent, err := townStore.GetAgent("testrig/witness")
	if err != nil {
		t.Fatalf("GetAgent() error: %v", err)
	}
	if agent.Role != "witness" {
		t.Errorf("agent role = %q, want %q", agent.Role, "witness")
	}
	if agent.State != "idle" {
		t.Errorf("agent state = %q, want %q", agent.State, "idle")
	}
}

func TestRegisterAgentIdempotent(t *testing.T) {
	townStore, rigStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	w := New(cfg, townStore, rigStore, mock, nil)

	if err := w.Register(); err != nil {
		t.Fatalf("Register() first call error: %v", err)
	}
	if err := w.Register(); err != nil {
		t.Fatalf("Register() second call error: %v", err)
	}

	// Should still be the same agent.
	agent, err := townStore.GetAgent("testrig/witness")
	if err != nil {
		t.Fatalf("GetAgent() error: %v", err)
	}
	if agent.Role != "witness" {
		t.Errorf("agent role = %q, want %q", agent.Role, "witness")
	}
}

func TestRunLifecycle(t *testing.T) {
	townStore, rigStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.PatrolInterval = 100 * time.Millisecond

	w := New(cfg, townStore, rigStore, mock, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	// Check agent is registered and working.
	time.Sleep(50 * time.Millisecond)
	agent, err := townStore.GetAgent("testrig/witness")
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
	agent, err = townStore.GetAgent("testrig/witness")
	if err != nil {
		t.Fatalf("GetAgent() after run: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("agent state after run = %q, want %q", agent.State, "idle")
	}
}

func TestPatrolHealthyAgents(t *testing.T) {
	townStore, rigStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create 3 working polecats with live sessions and changing output.
	for _, name := range []string{"Toast", "Jasper", "Sage"} {
		townStore.CreateAgent(name, "testrig", "polecat")
		townStore.UpdateAgentState("testrig/"+name, "working", "gt-"+name)
		sessName := "gt-testrig-" + name
		mock.alive[sessName] = true
		mock.captures[sessName] = "output for " + name
	}

	w := New(cfg, townStore, rigStore, mock, nil)

	if err := w.patrol(); err != nil {
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
	townStore, rigStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working polecat with a dead session.
	townStore.CreateAgent("Toast", "testrig", "polecat")
	townStore.UpdateAgentState("testrig/Toast", "working", "gt-abc12345")
	createWorkItem(t, rigStore, "gt-abc12345", "Test task")
	// Session is NOT alive (not in mock.alive).

	// Create worktree directory so respawn doesn't fail on missing dir.
	worktreeDir := filepath.Join(os.Getenv("GT_HOME"), "testrig", "polecats", "Toast", "rig")
	os.MkdirAll(worktreeDir, 0o755)

	w := New(cfg, townStore, rigStore, mock, nil)

	if err := w.patrol(); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Should have started a session (respawn).
	started := mock.getStarted()
	if len(started) != 1 {
		t.Fatalf("expected 1 session started (respawn), got %d: %v", len(started), started)
	}
	if started[0] != "gt-testrig-Toast" {
		t.Errorf("started session = %q, want %q", started[0], "gt-testrig-Toast")
	}
}

func TestPatrolMaxRespawns(t *testing.T) {
	townStore, rigStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRespawns = 2

	// Create a working polecat with a dead session.
	townStore.CreateAgent("Toast", "testrig", "polecat")
	townStore.UpdateAgentState("testrig/Toast", "working", "gt-abc12345")
	createWorkItem(t, rigStore, "gt-abc12345", "Test task")

	worktreeDir := filepath.Join(os.Getenv("GT_HOME"), "testrig", "polecats", "Toast", "rig")
	os.MkdirAll(worktreeDir, 0o755)

	w := New(cfg, townStore, rigStore, mock, nil)

	// Pre-set respawn count to max.
	w.respawnCounts[respawnKey{AgentID: "testrig/Toast", WorkItemID: "gt-abc12345"}] = 2

	if err := w.patrol(); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Work should be returned to open, no respawn.
	started := mock.getStarted()
	if len(started) != 0 {
		t.Fatalf("expected 0 sessions started (max respawns), got %d", len(started))
	}

	// Work item should be open.
	item, err := rigStore.GetWorkItem("gt-abc12345")
	if err != nil {
		t.Fatalf("GetWorkItem() error: %v", err)
	}
	if item.Status != "open" {
		t.Errorf("work item status = %q, want %q", item.Status, "open")
	}

	// Agent should be idle.
	agent, err := townStore.GetAgent("testrig/Toast")
	if err != nil {
		t.Fatalf("GetAgent() error: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("agent state = %q, want %q", agent.State, "idle")
	}
}

func TestPatrolDetectsZombie(t *testing.T) {
	townStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an idle polecat with a live session but no hook.
	townStore.CreateAgent("Toast", "testrig", "polecat")
	// State is idle (default), no hook item.
	mock.alive["gt-testrig-Toast"] = true

	w := New(cfg, townStore, nil, mock, nil)

	if err := w.patrol(); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Session should have been stopped.
	stopped := mock.getStopped()
	if len(stopped) != 1 {
		t.Fatalf("expected 1 session stopped (zombie), got %d: %v", len(stopped), stopped)
	}
	if stopped[0] != "gt-testrig-Toast" {
		t.Errorf("stopped session = %q, want %q", stopped[0], "gt-testrig-Toast")
	}
}

func TestPatrolIgnoresIdleClean(t *testing.T) {
	townStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an idle polecat with no session and no hook.
	townStore.CreateAgent("Toast", "testrig", "polecat")
	// State is idle (default), no session, no hook.

	w := New(cfg, townStore, nil, mock, nil)

	if err := w.patrol(); err != nil {
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

func TestPatrolIgnoresNonPolecats(t *testing.T) {
	townStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create agents with non-polecat roles.
	townStore.CreateAgent("refinery", "testrig", "refinery")
	townStore.CreateAgent("witness", "testrig", "witness")

	w := New(cfg, townStore, nil, mock, nil)

	if err := w.patrol(); err != nil {
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
	townStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	townStore.CreateAgent("Toast", "testrig", "polecat")
	townStore.UpdateAgentState("testrig/Toast", "working", "gt-abc12345")
	mock.alive["gt-testrig-Toast"] = true

	assessCalled := false
	w := New(cfg, townStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		assessCalled = true
		return &AssessmentResult{Status: "progressing", Confidence: "high", SuggestedAction: "none"}, nil
	}

	// First patrol: establish baseline.
	mock.captures["gt-testrig-Toast"] = "output v1"
	w.patrol()

	// Second patrol: different output — should NOT trigger assessment.
	mock.captures["gt-testrig-Toast"] = "output v2"
	w.patrol()

	if assessCalled {
		t.Error("assessment should not be triggered when output changes")
	}
}

func TestProgressDetectionOutputUnchanged(t *testing.T) {
	townStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	townStore.CreateAgent("Toast", "testrig", "polecat")
	townStore.UpdateAgentState("testrig/Toast", "working", "gt-abc12345")
	mock.alive["gt-testrig-Toast"] = true
	mock.captures["gt-testrig-Toast"] = "same output"

	assessCalled := false
	w := New(cfg, townStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		assessCalled = true
		return &AssessmentResult{Status: "progressing", Confidence: "high", SuggestedAction: "none"}, nil
	}

	// First patrol: establish baseline.
	w.patrol()

	// Second patrol: same output — should trigger assessment.
	w.patrol()

	if !assessCalled {
		t.Error("assessment should be triggered when output is unchanged")
	}
}

func TestAssessmentNudge(t *testing.T) {
	townStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	townStore.CreateAgent("Toast", "testrig", "polecat")
	townStore.UpdateAgentState("testrig/Toast", "working", "gt-abc12345")
	mock.alive["gt-testrig-Toast"] = true
	mock.captures["gt-testrig-Toast"] = "stuck output"

	w := New(cfg, townStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "stuck",
			Confidence:      "high",
			SuggestedAction: "nudge",
			NudgeMessage:    "You appear stuck. Try checking the error log.",
		}, nil
	}

	// First patrol: baseline.
	w.patrol()
	// Second patrol: same output → assessment → nudge.
	w.patrol()

	injected := mock.getInjected()
	if len(injected) != 1 {
		t.Fatalf("expected 1 injection (nudge), got %d", len(injected))
	}
	if injected[0].Text != "You appear stuck. Try checking the error log." {
		t.Errorf("nudge text = %q, want %q", injected[0].Text, "You appear stuck. Try checking the error log.")
	}
}

func TestAssessmentEscalate(t *testing.T) {
	townStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	townStore.CreateAgent("Toast", "testrig", "polecat")
	townStore.UpdateAgentState("testrig/Toast", "working", "gt-abc12345")
	mock.alive["gt-testrig-Toast"] = true
	mock.captures["gt-testrig-Toast"] = "error output"

	w := New(cfg, townStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "stuck",
			Confidence:      "high",
			SuggestedAction: "escalate",
			Reason:          "auth token expired",
		}, nil
	}

	// First patrol: baseline.
	w.patrol()
	// Second patrol: same output → assessment → escalate.
	w.patrol()

	// No nudge should be injected.
	injected := mock.getInjected()
	if len(injected) != 0 {
		t.Errorf("expected 0 injections on escalation, got %d", len(injected))
	}

	// Check that a protocol message was sent (RECOVERY_NEEDED).
	msgs, err := townStore.PendingProtocol("operator", "RECOVERY_NEEDED")
	if err != nil {
		t.Fatalf("PendingProtocol() error: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("expected RECOVERY_NEEDED protocol message to operator")
	}
}

func TestAssessmentNone(t *testing.T) {
	townStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	townStore.CreateAgent("Toast", "testrig", "polecat")
	townStore.UpdateAgentState("testrig/Toast", "working", "gt-abc12345")
	mock.alive["gt-testrig-Toast"] = true
	mock.captures["gt-testrig-Toast"] = "output"

	w := New(cfg, townStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "progressing",
			Confidence:      "high",
			SuggestedAction: "none",
			Reason:          "agent is compiling",
		}, nil
	}

	// First patrol: baseline.
	w.patrol()
	// Second patrol: same output → assessment → none.
	w.patrol()

	// No nudge, no escalation.
	injected := mock.getInjected()
	if len(injected) != 0 {
		t.Errorf("expected 0 injections for action=none, got %d", len(injected))
	}
}

func TestAssessmentLowConfidenceIgnored(t *testing.T) {
	townStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	townStore.CreateAgent("Toast", "testrig", "polecat")
	townStore.UpdateAgentState("testrig/Toast", "working", "gt-abc12345")
	mock.alive["gt-testrig-Toast"] = true
	mock.captures["gt-testrig-Toast"] = "output"

	w := New(cfg, townStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return &AssessmentResult{
			Status:          "stuck",
			Confidence:      "low",
			SuggestedAction: "nudge",
			NudgeMessage:    "Should not be sent",
		}, nil
	}

	// First patrol: baseline.
	w.patrol()
	// Second patrol: same output → assessment → low confidence → no action.
	w.patrol()

	injected := mock.getInjected()
	if len(injected) != 0 {
		t.Errorf("expected 0 injections for low confidence, got %d", len(injected))
	}
}

func TestAssessmentFailureNonBlocking(t *testing.T) {
	townStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	townStore.CreateAgent("Toast", "testrig", "polecat")
	townStore.UpdateAgentState("testrig/Toast", "working", "gt-abc12345")
	mock.alive["gt-testrig-Toast"] = true
	mock.captures["gt-testrig-Toast"] = "output"

	w := New(cfg, townStore, nil, mock, nil)
	w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
		return nil, fmt.Errorf("AI service unavailable")
	}

	// First patrol: baseline.
	w.patrol()
	// Second patrol: same output → assessment → failure → should not crash.
	err := w.patrol()

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
	townStore, rigStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRespawns = 2

	townStore.CreateAgent("Toast", "testrig", "polecat")
	townStore.UpdateAgentState("testrig/Toast", "working", "gt-abc12345")
	createWorkItem(t, rigStore, "gt-abc12345", "Test task")

	worktreeDir := filepath.Join(os.Getenv("GT_HOME"), "testrig", "polecats", "Toast", "rig")
	os.MkdirAll(worktreeDir, 0o755)

	w := New(cfg, townStore, rigStore, mock, nil)

	// Patrol 1: stalled → respawn (attempt 1).
	w.patrol()
	started := mock.getStarted()
	if len(started) != 1 {
		t.Fatalf("patrol 1: expected 1 start, got %d", len(started))
	}

	// Kill the session.
	mock.mu.Lock()
	delete(mock.alive, "gt-testrig-Toast")
	mock.mu.Unlock()

	// Patrol 2: still stalled → respawn (attempt 2).
	w.patrol()
	started = mock.getStarted()
	if len(started) != 2 {
		t.Fatalf("patrol 2: expected 2 starts, got %d", len(started))
	}

	// Kill the session again.
	mock.mu.Lock()
	delete(mock.alive, "gt-testrig-Toast")
	mock.mu.Unlock()

	// Patrol 3: still stalled → return to open (max reached).
	w.patrol()
	started = mock.getStarted()
	if len(started) != 2 {
		t.Fatalf("patrol 3: expected still 2 starts (max reached), got %d", len(started))
	}

	// Agent should be idle, work item open.
	agent, err := townStore.GetAgent("testrig/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "idle" {
		t.Errorf("agent state = %q, want %q after max respawns", agent.State, "idle")
	}

	item, err := rigStore.GetWorkItem("gt-abc12345")
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
