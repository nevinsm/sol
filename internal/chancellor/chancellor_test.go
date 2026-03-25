package chancellor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

// writeTestToken writes a minimal api_key token so Start() can inject credentials in tests.
func writeTestToken(t *testing.T, solHome string) {
	t.Helper()
	accountsDir := filepath.Join(solHome, ".accounts")
	if err := os.MkdirAll(accountsDir, 0o755); err != nil {
		t.Fatalf("failed to create .accounts dir: %v", err)
	}
	tokenJSON := `{"type":"api_key","token":"test-key","created_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(accountsDir, "token.json"), []byte(tokenJSON), 0o600); err != nil {
		t.Fatalf("failed to write test token: %v", err)
	}
}

// --- Mocks ---

type mockSessionManager struct {
	sessions  map[string]bool
	startErr  error
	lastStart struct {
		name    string
		workdir string
		cmd     string
		role    string
		world   string
	}
}

func (m *mockSessionManager) Exists(name string) bool {
	return m.sessions[name]
}

func (m *mockSessionManager) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	if m.startErr != nil {
		return m.startErr
	}
	m.sessions[name] = true
	m.lastStart.name = name
	m.lastStart.workdir = workdir
	m.lastStart.cmd = cmd
	m.lastStart.role = role
	m.lastStart.world = world
	return nil
}

func (m *mockSessionManager) Stop(name string, force bool) error {
	delete(m.sessions, name)
	return nil
}

func (m *mockSessionManager) Inject(name string, text string, submit bool) error {
	return nil
}

func (m *mockSessionManager) Capture(name string, lines int) (string, error) {
	return "", nil
}

type mockStopStore struct {
	updated     map[string]store.AgentState
	activeWrits map[string]string
	getAgentErr error
	updateErr   error
}

func (m *mockStopStore) GetAgent(id string) (*store.Agent, error) {
	if m.getAgentErr != nil {
		return nil, m.getAgentErr
	}
	return &store.Agent{ID: id, Role: "chancellor", State: store.AgentWorking}, nil
}

func (m *mockStopStore) UpdateAgentState(id string, state store.AgentState, activeWrit string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	if m.updated != nil {
		m.updated[id] = state
	}
	if m.activeWrits != nil {
		m.activeWrits[id] = activeWrit
	}
	return nil
}

// --- Tests ---

func TestDirectoryHelpers(t *testing.T) {
	t.Setenv("SOL_HOME", "/tmp/sol-test")

	tests := []struct {
		name string
		fn   func() string
		want string
	}{
		{"ChancellorDir", ChancellorDir, "/tmp/sol-test/chancellor"},
		{"BriefDir", BriefDir, "/tmp/sol-test/chancellor/.brief"},
		{"BriefPath", BriefPath, "/tmp/sol-test/chancellor/.brief/memory.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn()
			if got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestStart(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	writeTestToken(t, tmp)

	mgr := &mockSessionManager{sessions: map[string]bool{}}

	err := Start(mgr)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify session started with correct parameters.
	if !mgr.sessions[SessionName] {
		t.Error("session not started")
	}
	if mgr.lastStart.name != SessionName {
		t.Errorf("session name = %q, want %q", mgr.lastStart.name, SessionName)
	}
	chancellorDir := ChancellorDir()
	if mgr.lastStart.workdir != chancellorDir {
		t.Errorf("workdir = %q, want %q", mgr.lastStart.workdir, chancellorDir)
	}
	if mgr.lastStart.role != "chancellor" {
		t.Errorf("role = %q, want \"chancellor\"", mgr.lastStart.role)
	}

	// Verify chancellor directory created.
	if _, err := os.Stat(chancellorDir); os.IsNotExist(err) {
		t.Error("chancellor directory not created")
	}

	// Verify brief directory created.
	briefDir := BriefDir()
	if _, err := os.Stat(briefDir); os.IsNotExist(err) {
		t.Error("brief directory not created")
	}

	// Verify hooks file written with PreToolUse hooks.
	hooksPath := filepath.Join(chancellorDir, ".claude", "settings.local.json")
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("hooks file not found: %v", err)
	}

	// hookConfig is a local type for deserializing settings.local.json in tests.
	type hookHandler struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type hookMatcherGroup struct {
		Matcher string        `json:"matcher,omitempty"`
		Hooks   []hookHandler `json:"hooks"`
	}
	type hookConfig struct {
		Hooks map[string][]hookMatcherGroup `json:"hooks"`
	}

	var cfg hookConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse hooks JSON: %v", err)
	}

	// Verify PreToolUse hooks block auto-memory writes and EnterPlanMode.
	if ptuGroups, ok := cfg.Hooks["PreToolUse"]; !ok {
		t.Error("no PreToolUse hooks")
	} else if len(ptuGroups) != 11 {
		t.Errorf("expected 11 PreToolUse matcher groups (2 base + 9 guards), got %d", len(ptuGroups))
	} else {
		if ptuGroups[0].Matcher != "Write|Edit" {
			t.Errorf("PreToolUse matcher[0] = %q, want \"Write|Edit\"", ptuGroups[0].Matcher)
		}
		if ptuGroups[1].Matcher != "EnterPlanMode" {
			t.Errorf("PreToolUse matcher[1] = %q, want \"EnterPlanMode\"", ptuGroups[1].Matcher)
		}
		if !strings.Contains(ptuGroups[1].Hooks[0].Command, "BLOCKED") {
			t.Error("EnterPlanMode hook should contain BLOCKED message")
		}
		if !strings.Contains(ptuGroups[1].Hooks[0].Command, "exit 2") {
			t.Error("EnterPlanMode hook should exit 2 to block the tool call")
		}
	}
}

func TestStartAlreadyRunning(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	mgr := &mockSessionManager{sessions: map[string]bool{SessionName: true}}

	err := Start(mgr)
	if err == nil {
		t.Fatal("expected error for already running session")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("error = %q, want contains \"already running\"", err.Error())
	}
}

func TestStartSessionError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	writeTestToken(t, tmp)

	mgr := &mockSessionManager{
		sessions: map[string]bool{},
		startErr: os.ErrPermission,
	}

	err := Start(mgr)
	if err == nil {
		t.Fatal("expected error when session start fails")
	}
	if !strings.Contains(err.Error(), "failed to start chancellor") {
		t.Errorf("error = %q, want contains \"failed to start chancellor\"", err.Error())
	}
}

func TestStop(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	mgr := &mockSessionManager{sessions: map[string]bool{SessionName: true}}
	ss := &mockStopStore{
		updated:     map[string]store.AgentState{},
		activeWrits: map[string]string{},
	}

	err := Stop(mgr, ss)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Verify session stopped.
	if mgr.sessions[SessionName] {
		t.Error("session not stopped")
	}

	// Verify agent state updated to idle.
	if state, ok := ss.updated["/chancellor"]; !ok {
		t.Error("agent state not updated")
	} else if state != store.AgentIdle {
		t.Errorf("agent state = %q, want %q", state, store.AgentIdle)
	}
}

func TestStopNoSession(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	mgr := &mockSessionManager{sessions: map[string]bool{}}
	ss := &mockStopStore{
		updated: map[string]store.AgentState{},
	}

	err := Stop(mgr, ss)
	if err == nil {
		t.Fatal("expected error when no session running")
	}
	if !strings.Contains(err.Error(), "no chancellor session running") {
		t.Errorf("error = %q, want contains \"no chancellor session running\"", err.Error())
	}
}
