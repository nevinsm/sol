package governor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
)

// writeTestToken writes a minimal api_key token so startup.Launch can inject credentials in tests.
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

// mockSessionStarter captures session start calls for startup.Launch tests.
type mockSessionStarter struct {
	startErr error
	lastCall struct {
		name    string
		workdir string
		role    string
		world   string
	}
}

func (m *mockSessionStarter) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	if m.startErr != nil {
		return m.startErr
	}
	m.lastCall.name = name
	m.lastCall.workdir = workdir
	m.lastCall.role = role
	m.lastCall.world = world
	return nil
}

// --- Mocks ---

type mockStopStore struct {
	updated   map[string]store.AgentState // id -> state
	updateErr error
}

func (m *mockStopStore) CreateAgent(name, world, role string) (string, error) {
	return "", nil // not exercised in Stop tests
}

func (m *mockStopStore) EnsureAgent(name, world, role string) error {
	return nil // not exercised in Stop tests
}

func (m *mockStopStore) DeleteAgent(id string) error {
	return nil // not exercised in Stop tests
}

func (m *mockStopStore) DeleteAgentsForWorld(world string) error {
	return nil // not exercised in Stop tests
}

func (m *mockStopStore) UpdateAgentState(id string, state store.AgentState, activeWrit string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.updated[id] = state
	return nil
}

type mockStopManager struct {
	sessions map[string]bool
}

func (m *mockStopManager) Exists(name string) bool {
	return m.sessions[name]
}

func (m *mockStopManager) Stop(name string, force bool) error {
	delete(m.sessions, name)
	return nil
}

func (m *mockStopManager) Inject(name string, text string, submit bool) error {
	return nil
}

func (m *mockStopManager) Capture(name string, lines int) (string, error) {
	return "", nil
}

// --- Tests ---

func TestGovernorHooksPreCompact(t *testing.T) {
	hooks := governorHooks("myworld", "")
	if len(hooks.PreCompact) == 0 {
		t.Fatal("governor hooks missing PreCompact")
	}
	cmd := hooks.PreCompact[0].Command
	want := "sol prime --world=myworld --agent=governor --compact"
	if cmd != want {
		t.Errorf("PreCompact command = %q, want %q", cmd, want)
	}
}

func TestGovernorHooksNoCompactSessionStart(t *testing.T) {
	hooks := governorHooks("myworld", "")
	if len(hooks.SessionStart) == 0 {
		t.Fatal("governor hooks missing SessionStart")
	}
	for _, g := range hooks.SessionStart {
		if strings.Contains(g.Matcher, "compact") {
			t.Error("governor SessionStart should not have a compact matcher")
		}
	}
}

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

func TestStop(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ss := &mockStopStore{
		updated: map[string]store.AgentState{},
	}

	sessName := "sol-myworld-governor"
	mgr := &mockStopManager{sessions: map[string]bool{sessName: true}}

	err := Stop("myworld", ss, mgr)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Verify session stopped.
	if mgr.sessions[sessName] {
		t.Error("session not stopped")
	}

	// Verify agent state updated to idle.
	if ss.updated["myworld/governor"] != store.AgentIdle {
		t.Errorf("agent state = %q, want \"idle\"", ss.updated["myworld/governor"])
	}
}

func TestStopNoSession(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ss := &mockStopStore{
		updated: map[string]store.AgentState{},
	}

	mgr := &mockStopManager{sessions: map[string]bool{}}

	err := Stop("myworld", ss, mgr)
	if err != nil {
		t.Fatalf("Stop should not error when session doesn't exist: %v", err)
	}

	// Verify agent state still updated to idle.
	if ss.updated["myworld/governor"] != store.AgentIdle {
		t.Errorf("agent state = %q, want \"idle\"", ss.updated["myworld/governor"])
	}
}

func TestGovernorPrime(t *testing.T) {
	result := governorPrime("myworld", "")
	if !strings.Contains(result, "sol brief inject") {
		t.Errorf("governorPrime missing 'sol brief inject': %q", result)
	}
	if !strings.Contains(result, "sol world sync") {
		t.Errorf("governorPrime missing 'sol world sync': %q", result)
	}
	if !strings.Contains(result, "myworld") {
		t.Errorf("governorPrime missing world name: %q", result)
	}
}

func TestGovernorStart(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	writeTestToken(t, tmp)

	// Create required dirs.
	if err := os.MkdirAll(filepath.Join(tmp, ".store"), 0o755); err != nil {
		t.Fatalf("failed to create .store dir: %v", err)
	}

	// Create governor dir (startup.Launch checks it exists).
	governorDir := GovernorDir("myworld")
	if err := os.MkdirAll(governorDir, 0o755); err != nil {
		t.Fatalf("failed to create governor dir: %v", err)
	}

	// Open sphere store.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	mock := &mockSessionStarter{}

	_, err = startup.Launch(RoleConfig(), "myworld", "governor", startup.LaunchOpts{
		Sessions: mock,
		Sphere:   sphereStore,
	})
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	// Verify hooks file written.
	hooksPath := filepath.Join(governorDir, ".claude", "settings.local.json")
	if _, err := os.Stat(hooksPath); err != nil {
		t.Errorf("hooks file not written: %v", err)
	}

	// Verify persona injected.
	personaPath := filepath.Join(governorDir, "CLAUDE.local.md")
	data, err := os.ReadFile(personaPath)
	if err != nil {
		t.Fatalf("persona not written: %v", err)
	}
	if !strings.Contains(string(data), "Governor") {
		t.Errorf("persona missing governor content, got: %q", string(data))
	}
}
