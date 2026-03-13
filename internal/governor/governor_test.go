package governor

import (
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

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
