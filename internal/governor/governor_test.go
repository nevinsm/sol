package governor

import (
	"strings"
	"testing"
)

// --- Mocks ---

type mockStopStore struct {
	updated   map[string]string // id -> state
	updateErr error
}

func (m *mockStopStore) UpdateAgentState(id, state, activeWrit string) error {
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

func TestGovernorHooksNoPreCompact(t *testing.T) {
	hooks := governorHooks("myworld", "")
	if _, ok := hooks.Hooks["PreCompact"]; ok {
		t.Error("governor hooks should not have a PreCompact hook")
	}
}

func TestGovernorHooksNoCompactSessionStart(t *testing.T) {
	hooks := governorHooks("myworld", "")
	groups, ok := hooks.Hooks["SessionStart"]
	if !ok {
		t.Fatal("governor hooks missing SessionStart")
	}
	for _, g := range groups {
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
		updated: map[string]string{},
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
	if ss.updated["myworld/governor"] != "idle" {
		t.Errorf("agent state = %q, want \"idle\"", ss.updated["myworld/governor"])
	}
}

func TestStopNoSession(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ss := &mockStopStore{
		updated: map[string]string{},
	}

	mgr := &mockStopManager{sessions: map[string]bool{}}

	err := Stop("myworld", ss, mgr)
	if err != nil {
		t.Fatalf("Stop should not error when session doesn't exist: %v", err)
	}

	// Verify agent state still updated to idle.
	if ss.updated["myworld/governor"] != "idle" {
		t.Errorf("agent state = %q, want \"idle\"", ss.updated["myworld/governor"])
	}
}
