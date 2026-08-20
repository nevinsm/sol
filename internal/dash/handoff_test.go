package dash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/flock"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// --- fakes for handoff.SessionManager / handoff.SphereStore ---

type fakeHandoffSessionMgr struct {
	captureResult string
	cycled        []fakeCycleCall
	stopped       []string
	started       []string
	nudged        []string
}

type fakeCycleCall struct {
	Name, Workdir, Cmd, Role, World string
}

func (m *fakeHandoffSessionMgr) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	m.started = append(m.started, name)
	return nil
}

func (m *fakeHandoffSessionMgr) Stop(name string, force bool) error {
	m.stopped = append(m.stopped, name)
	return nil
}

func (m *fakeHandoffSessionMgr) Exists(name string) bool { return true }

func (m *fakeHandoffSessionMgr) Capture(name string, lines int) (string, error) {
	return m.captureResult, nil
}

func (m *fakeHandoffSessionMgr) Cycle(name, workdir, cmd string, env map[string]string, role, world string) error {
	m.cycled = append(m.cycled, fakeCycleCall{name, workdir, cmd, role, world})
	return nil
}

func (m *fakeHandoffSessionMgr) NudgeSession(name string, message string) error {
	m.nudged = append(m.nudged, name)
	return nil
}

func (m *fakeHandoffSessionMgr) WaitForIdle(name string, timeout time.Duration) error {
	return nil
}

func (m *fakeHandoffSessionMgr) CountSessions(prefix string) (int, error) {
	return 0, nil
}

type fakeHandoffSphereStore struct {
	messages []string
	agents   map[string]*store.Agent
}

func (m *fakeHandoffSphereStore) SendMessage(sender, recipient, subject, body string, priority int, msgType string) (string, error) {
	m.messages = append(m.messages, subject)
	return "msg-00000001", nil
}

func (m *fakeHandoffSphereStore) GetAgent(id string) (*store.Agent, error) {
	if m.agents != nil {
		if a, ok := m.agents[id]; ok {
			return a, nil
		}
	}
	return nil, fmt.Errorf("agent %q not found", id)
}

// setupHandoffTestHome isolates SOL_HOME and drops a fake credential token
// so the startup.Resume/Launch path handoff.Exec drives doesn't fail on
// missing credentials — mirrors internal/handoff/handoff_test.go's
// setupSolHome, duplicated here since it's unexported in another package.
func setupHandoffTestHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	accountsDir := filepath.Join(dir, ".accounts")
	if err := os.MkdirAll(accountsDir, 0o755); err != nil {
		t.Fatalf("failed to create .accounts dir: %v", err)
	}
	tokenJSON := `{"type":"api_key","token":"test-key","created_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(accountsDir, "token.json"), []byte(tokenJSON), 0o600); err != nil {
		t.Fatalf("failed to write test token: %v", err)
	}
	return dir
}

func registerMinimalHandoffRole(t *testing.T, role, worktreeDir string) {
	t.Helper()
	startup.Register(role, startup.RoleConfig{
		WorktreeDir: func(w, a string) string { return worktreeDir },
	})
	t.Cleanup(func() { startup.Register(role, startup.RoleConfig{}) })
}

// TestHandoffAgentCyclesSessionTetherPreserved verifies the extracted
// handoffAgent entry point wires flock.AcquireAgentLock + handoff.Exec
// correctly: the session is cycled (not stopped+started) for a live outpost
// agent, and the tether binding survives the cycle untouched — handoff never
// clears a tether, only sol resolve does.
func TestHandoffAgentCyclesSessionTetherPreserved(t *testing.T) {
	solHome := setupHandoffTestHome(t)

	world, name := "ember", "Toast"
	if err := tether.Write(world, name, "sol-abc1234500000000", "outpost"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := filepath.Join(solHome, world, "outposts", name, "worktree")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	registerMinimalHandoffRole(t, "outpost", worktreeDir)

	mgr := &fakeHandoffSessionMgr{captureResult: "$ make test\nAll tests passed."}
	sphereStore := &fakeHandoffSphereStore{}

	if err := handoffAgent(world, name, "outpost", mgr, sphereStore); err != nil {
		t.Fatalf("handoffAgent failed: %v", err)
	}

	if len(mgr.stopped) != 0 || len(mgr.started) != 0 {
		t.Errorf("expected no Stop/Start calls (Cycle used instead), got stopped=%v started=%v", mgr.stopped, mgr.started)
	}
	if len(mgr.cycled) != 1 {
		t.Fatalf("expected 1 Cycle call, got %d", len(mgr.cycled))
	}
	wantSession := config.SessionName(world, name)
	if mgr.cycled[0].Name != wantSession {
		t.Errorf("cycled session name = %q, want %q", mgr.cycled[0].Name, wantSession)
	}
	if mgr.cycled[0].Workdir != worktreeDir {
		t.Errorf("cycled workdir = %q, want %q", mgr.cycled[0].Workdir, worktreeDir)
	}
	if mgr.cycled[0].Role != "outpost" {
		t.Errorf("cycled role = %q, want outpost", mgr.cycled[0].Role)
	}

	// Tether preserved: handoff.Exec must not clear it.
	writID, err := tether.ReadSingle(world, name, "outpost")
	if err != nil {
		t.Fatalf("tether should survive handoff, ReadSingle failed: %v", err)
	}
	if writID != "sol-abc1234500000000" {
		t.Errorf("tether writ id = %q after handoff, want sol-abc1234500000000", writID)
	}

	// Agent lock must be released — a fresh acquire should succeed.
	relock, err := flock.AcquireAgentLock(world + "/" + name)
	if err != nil {
		t.Errorf("expected agent lock to be released after handoffAgent returns, got: %v", err)
	} else {
		relock.Release()
	}
}

// TestHandoffWorktreeDir verifies the role→worktree resolution mirrors
// cmd/handoff.go's worktreeDirForRole for the two roles dash's handoff
// action supports.
func TestHandoffWorktreeDir(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	got := handoffWorktreeDir("ember", "Toast", "outpost")
	want := config.WorktreePath("ember", "Toast")
	if got != want {
		t.Errorf("outpost worktree dir = %q, want %q", got, want)
	}

	got = handoffWorktreeDir("ember", "Scout", "envoy")
	if !strings.Contains(got, "envoys") || !strings.Contains(got, "Scout") {
		t.Errorf("envoy worktree dir = %q, want it to route through the envoy path", got)
	}
}

// --- handleHandoff gating (dead/idle/live-working) ---

func TestHandleHandoffNoFocusIsNoop(t *testing.T) {
	wm := newWorldModel()
	wm.hasFocus = false
	data := &status.WorldStatus{World: "ember"}

	_, cmd := wm.handleHandoff(data)
	if cmd != nil {
		t.Error("handleHandoff without focus should produce no command")
	}
}

func TestHandleHandoffDeadOutpostShowsMessage(t *testing.T) {
	wm := newWorldModel()
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts
	wm.outpostCursor = 0
	data := &status.WorldStatus{
		World: "ember",
		Agents: []status.AgentStatus{
			{Name: "Toast", State: "working", SessionAlive: false},
		},
	}

	_, cmd := wm.handleHandoff(data)
	if cmd == nil {
		t.Fatal("expected a command for a dead session")
	}
	msg, ok := cmd().(noSessionMsg)
	if !ok {
		t.Fatalf("expected noSessionMsg, got %T", cmd())
	}
	if !strings.Contains(msg.message, "no active session") {
		t.Errorf("message = %q, want it to mention no active session", msg.message)
	}
}

func TestHandleHandoffIdleOutpostShowsMessage(t *testing.T) {
	wm := newWorldModel()
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts
	wm.outpostCursor = 0
	data := &status.WorldStatus{
		World: "ember",
		Agents: []status.AgentStatus{
			{Name: "Crisp", State: "idle", SessionAlive: true},
		},
	}

	_, cmd := wm.handleHandoff(data)
	if cmd == nil {
		t.Fatal("expected a command for an idle session")
	}
	msg, ok := cmd().(noSessionMsg)
	if !ok {
		t.Fatalf("expected noSessionMsg, got %T", cmd())
	}
	if !strings.Contains(msg.message, "idle") {
		t.Errorf("message = %q, want it to mention idle", msg.message)
	}
}

func TestHandleHandoffLiveWorkingOutpostRequestsConfirm(t *testing.T) {
	wm := newWorldModel()
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts
	wm.outpostCursor = 0
	data := &status.WorldStatus{
		World: "ember",
		Agents: []status.AgentStatus{
			{Name: "Toast", State: "working", SessionAlive: true},
		},
	}

	_, cmd := wm.handleHandoff(data)
	if cmd == nil {
		t.Fatal("expected a command for a live working session")
	}
	msg, ok := cmd().(requestHandoffMsg)
	if !ok {
		t.Fatalf("expected requestHandoffMsg, got %T", cmd())
	}
	if msg.target.name != "Toast" || msg.target.role != "outpost" || msg.target.world != "ember" {
		t.Errorf("unexpected target: %+v", msg.target)
	}
	if msg.target.sessionName != config.SessionName("ember", "Toast") {
		t.Errorf("target.sessionName = %q, want %q", msg.target.sessionName, config.SessionName("ember", "Toast"))
	}
	if !strings.Contains(msg.target.confirmTitle, "Toast") {
		t.Errorf("confirmTitle = %q, want it to mention Toast", msg.target.confirmTitle)
	}
}

func TestHandleHandoffLiveWorkingEnvoyRequestsConfirm(t *testing.T) {
	wm := newWorldModel()
	wm.hasFocus = true
	wm.focusedSection = sectionEnvoys
	wm.envoyCursor = 0
	data := &status.WorldStatus{
		World: "ember",
		Envoys: []status.EnvoyStatus{
			{Name: "Scout", State: "working", SessionAlive: true},
		},
	}

	_, cmd := wm.handleHandoff(data)
	if cmd == nil {
		t.Fatal("expected a command for a live working envoy")
	}
	msg, ok := cmd().(requestHandoffMsg)
	if !ok {
		t.Fatalf("expected requestHandoffMsg, got %T", cmd())
	}
	if msg.target.name != "Scout" || msg.target.role != "envoy" {
		t.Errorf("unexpected target: %+v", msg.target)
	}
}

func TestHandleHandoffProcessesSectionNoop(t *testing.T) {
	wm := newWorldModel()
	wm.hasFocus = true
	wm.focusedSection = sectionProcesses
	data := &status.WorldStatus{World: "ember"}

	_, cmd := wm.handleHandoff(data)
	if cmd != nil {
		t.Error("handoff on the processes section should be a no-op (H only applies to outpost/envoy rows)")
	}
}

// TestHandoffKeyRoutesThroughWorldUpdate exercises the 'H' key end-to-end
// through worldModel.update, matching the world.go key-switch wiring
// (rather than calling handleHandoff directly).
func TestHandoffKeyRoutesThroughWorldUpdate(t *testing.T) {
	wm := newWorldModel()
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts
	data := &status.WorldStatus{
		World: "ember",
		Agents: []status.AgentStatus{
			{Name: "Toast", State: "working", SessionAlive: true},
		},
	}

	_, cmd := wm.update(keyMsg("H"), data)
	if cmd == nil {
		t.Fatal("'H' on a live working outpost should produce a command")
	}
	if _, ok := cmd().(requestHandoffMsg); !ok {
		t.Fatalf("expected requestHandoffMsg, got %T", cmd())
	}
}
