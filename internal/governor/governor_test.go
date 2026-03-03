package governor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/protocol"
)

// --- Mocks ---

type mockSphereStore struct {
	ensured   map[string]bool
	updated   map[string]string // id -> state
	ensureErr error
	updateErr error
}

func (m *mockSphereStore) EnsureAgent(name, world, role string) error {
	if m.ensureErr != nil {
		return m.ensureErr
	}
	id := world + "/" + name
	m.ensured[id] = true
	return nil
}

func (m *mockSphereStore) UpdateAgentState(id, state, tetherItem string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.updated[id] = state
	return nil
}

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

// --- Tests ---

func TestGovernorDir(t *testing.T) {
	t.Setenv("SOL_HOME", "/tmp/sol-test")

	tests := []struct {
		name string
		fn   func(string) string
		want string
	}{
		{"GovernorDir", GovernorDir, "/tmp/sol-test/myworld/governor"},
		{"BriefDir", BriefDir, "/tmp/sol-test/myworld/governor/.brief"},
		{"BriefPath", BriefPath, "/tmp/sol-test/myworld/governor/.brief/memory.md"},
		{"WorldSummaryPath", WorldSummaryPath, "/tmp/sol-test/myworld/governor/.brief/world-summary.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn("myworld")
			if got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestStart(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ss := &mockSphereStore{
		ensured: map[string]bool{},
		updated: map[string]string{},
	}

	mgr := &mockSessionManager{sessions: map[string]bool{}}

	err := Start(StartOpts{
		World: "myworld",
	}, ss, mgr)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify agent ensured with role "governor".
	if !ss.ensured["myworld/governor"] {
		t.Error("agent not ensured in store")
	}

	// Verify agent state updated to "idle".
	if ss.updated["myworld/governor"] != "idle" {
		t.Errorf("agent state = %q, want \"idle\"", ss.updated["myworld/governor"])
	}

	// Verify session started with correct parameters.
	sessName := "sol-myworld-governor"
	if !mgr.sessions[sessName] {
		t.Error("session not started")
	}
	if mgr.lastStart.name != sessName {
		t.Errorf("session name = %q, want %q", mgr.lastStart.name, sessName)
	}
	govDir := GovernorDir("myworld")
	if mgr.lastStart.workdir != govDir {
		t.Errorf("workdir = %q, want %q", mgr.lastStart.workdir, govDir)
	}
	if mgr.lastStart.role != "governor" {
		t.Errorf("role = %q, want \"governor\"", mgr.lastStart.role)
	}
	if mgr.lastStart.world != "myworld" {
		t.Errorf("world = %q, want \"myworld\"", mgr.lastStart.world)
	}

	// Verify hooks file written.
	hooksPath := filepath.Join(govDir, ".claude", "settings.local.json")
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("hooks file not found: %v", err)
	}

	var cfg protocol.HookConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse hooks JSON: %v", err)
	}

	if hooks, ok := cfg.Hooks["SessionStart"]; !ok {
		t.Error("no SessionStart hooks")
	} else if len(hooks) != 2 {
		t.Errorf("expected 2 SessionStart hooks, got %d", len(hooks))
	} else {
		// Verify the startup/resume hook includes sol world sync.
		if !strings.Contains(hooks[0].Command, "sol world sync myworld") {
			t.Errorf("startup hook missing world sync command: %q", hooks[0].Command)
		}
		if hooks[0].Matcher != "startup|resume" {
			t.Errorf("startup hook matcher = %q, want \"startup|resume\"", hooks[0].Matcher)
		}
		if hooks[1].Matcher != "compact" {
			t.Errorf("compact hook matcher = %q, want \"compact\"", hooks[1].Matcher)
		}
		// Verify compact hook includes --skip-session-start.
		if !strings.Contains(hooks[1].Command, "--skip-session-start") {
			t.Errorf("compact hook missing --skip-session-start: %q", hooks[1].Command)
		}
	}
	if hooks, ok := cfg.Hooks["Stop"]; !ok {
		t.Error("no Stop hooks")
	} else if len(hooks) != 1 {
		t.Errorf("expected 1 Stop hook, got %d", len(hooks))
	}

	// Verify brief directory created.
	briefDir := BriefDir("myworld")
	if _, err := os.Stat(briefDir); os.IsNotExist(err) {
		t.Error("brief directory not created")
	}
}

func TestStartAlreadyRunning(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ss := &mockSphereStore{
		ensured: map[string]bool{},
		updated: map[string]string{},
	}

	sessName := "sol-myworld-governor"
	mgr := &mockSessionManager{sessions: map[string]bool{sessName: true}}

	err := Start(StartOpts{
		World: "myworld",
	}, ss, mgr)
	if err == nil {
		t.Fatal("expected error for already running session")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("error = %q, want contains \"already running\"", err.Error())
	}
}

func TestStop(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ss := &mockSphereStore{
		ensured: map[string]bool{},
		updated: map[string]string{},
	}

	sessName := "sol-myworld-governor"
	mgr := &mockSessionManager{sessions: map[string]bool{sessName: true}}

	err := Stop("myworld", ss, mgr)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Verify session stopped.
	if mgr.sessions[sessName] {
		t.Error("session not stopped")
	}

	// Verify agent state updated to idle.
	if ss.updated["myworld/governor"] != "idle" {
		t.Errorf("agent state = %q, want \"idle\"", ss.updated["myworld/governor"])
	}
}

func TestStopNoSession(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ss := &mockSphereStore{
		ensured: map[string]bool{},
		updated: map[string]string{},
	}

	mgr := &mockSessionManager{sessions: map[string]bool{}}

	err := Stop("myworld", ss, mgr)
	if err != nil {
		t.Fatalf("Stop should not error when session doesn't exist: %v", err)
	}

	// Verify agent state still updated to idle.
	if ss.updated["myworld/governor"] != "idle" {
		t.Errorf("agent state = %q, want \"idle\"", ss.updated["myworld/governor"])
	}
}

