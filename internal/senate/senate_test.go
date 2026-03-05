package senate

import (
	"os"
	"strings"
	"testing"
)

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

// --- Tests ---

func TestDirectoryHelpers(t *testing.T) {
	t.Setenv("SOL_HOME", "/tmp/sol-test")

	tests := []struct {
		name string
		fn   func() string
		want string
	}{
		{"SenateDir", SenateDir, "/tmp/sol-test/senate"},
		{"BriefDir", BriefDir, "/tmp/sol-test/senate/.brief"},
		{"BriefPath", BriefPath, "/tmp/sol-test/senate/.brief/memory.md"},
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
	senateDir := SenateDir()
	if mgr.lastStart.workdir != senateDir {
		t.Errorf("workdir = %q, want %q", mgr.lastStart.workdir, senateDir)
	}
	if mgr.lastStart.role != "senate" {
		t.Errorf("role = %q, want \"senate\"", mgr.lastStart.role)
	}

	// Verify senate directory created.
	if _, err := os.Stat(senateDir); os.IsNotExist(err) {
		t.Error("senate directory not created")
	}

	// Verify brief directory created.
	briefDir := BriefDir()
	if _, err := os.Stat(briefDir); os.IsNotExist(err) {
		t.Error("brief directory not created")
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

	mgr := &mockSessionManager{
		sessions: map[string]bool{},
		startErr: os.ErrPermission,
	}

	err := Start(mgr)
	if err == nil {
		t.Fatal("expected error when session start fails")
	}
	if !strings.Contains(err.Error(), "failed to start senate") {
		t.Errorf("error = %q, want contains \"failed to start senate\"", err.Error())
	}
}

func TestStop(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	mgr := &mockSessionManager{sessions: map[string]bool{SessionName: true}}

	err := Stop(mgr)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Verify session stopped.
	if mgr.sessions[SessionName] {
		t.Error("session not stopped")
	}
}

func TestStopNoSession(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	mgr := &mockSessionManager{sessions: map[string]bool{}}

	err := Stop(mgr)
	if err == nil {
		t.Fatal("expected error when no session running")
	}
	if !strings.Contains(err.Error(), "no senate session running") {
		t.Errorf("error = %q, want contains \"no senate session running\"", err.Error())
	}
}
