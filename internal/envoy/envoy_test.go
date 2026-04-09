package envoy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
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

func (m *mockSessionStarter) Exists(name string) bool {
	return false
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

type mockSphereStore struct {
	agents    map[string]store.Agent
	createErr error
	deleteErr error
	deleted   []string // tracks DeleteAgent calls
}

func (m *mockSphereStore) CreateAgent(name, world, role string) (string, error) {
	if m.createErr != nil {
		return "", m.createErr
	}
	id := world + "/" + name
	m.agents[id] = store.Agent{
		ID:    id,
		Name:  name,
		World: world,
		Role:  role,
		State: store.AgentIdle,
	}
	return id, nil
}

func (m *mockSphereStore) DeleteAgent(id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.deleted = append(m.deleted, id)
	delete(m.agents, id)
	return nil
}


type mockStopStore struct {
	agents      map[string]*store.Agent
	getErr      error
	updated     map[string]store.AgentState // id -> state
	activeWrits map[string]string           // id -> activeWrit passed to UpdateAgentState
	updateErr   error
}

func (m *mockStopStore) GetAgent(id string) (*store.Agent, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	a, ok := m.agents[id]
	if !ok {
		return nil, fmt.Errorf("agent %q: %w", id, store.ErrNotFound)
	}
	return a, nil
}

func (m *mockStopStore) UpdateAgentState(id string, state store.AgentState, activeWrit string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.updated[id] = state
	if m.activeWrits != nil {
		m.activeWrits[id] = activeWrit
	}
	return nil
}

type mockStopManager struct {
	sessions map[string]bool
	stopErr  error
}

func (m *mockStopManager) Exists(name string) bool {
	return m.sessions[name]
}

func (m *mockStopManager) Stop(name string, force bool) error {
	if m.stopErr != nil {
		return m.stopErr
	}
	delete(m.sessions, name)
	return nil
}

func (m *mockStopManager) Inject(name string, text string, submit bool) error {
	return nil
}

func (m *mockStopManager) Capture(name string, lines int) (string, error) {
	return "", nil
}

type mockListStore struct {
	agents  []store.Agent
	listErr error
}

func (m *mockListStore) ListAgents(world string, state store.AgentState) ([]store.Agent, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []store.Agent
	for _, a := range m.agents {
		if world != "" && a.World != world {
			continue
		}
		if state != "" && a.State != state {
			continue
		}
		result = append(result, a)
	}
	return result, nil
}

// --- Helpers ---

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init", dir},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %v failed: %s: %v", args, out, err)
		}
	}
	// Create initial commit.
	dummyFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(dummyFile, []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", dir, "add", ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %s: %v", out, err)
	}
	cmd = exec.Command("git", "-C", dir, "commit", "-m", "initial")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %s: %v", out, err)
	}
}

// --- Tests ---

func TestEnvoyPrimeNoActiveWrit(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// No stores set up — envoyPrime should return base string without error.
	result := envoyPrime("myworld", "Echo")
	if !strings.Contains(result, "Envoy Echo") {
		t.Errorf("expected base prime to contain agent name, got %q", result)
	}
	if strings.Contains(result, "Active writ") {
		t.Error("should not contain active writ when no store exists")
	}
}

func TestEnvoyPrimeWithActiveWrit(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	if err := os.MkdirAll(filepath.Join(tmp, ".store"), 0o755); err != nil {
		t.Fatalf("failed to create store dir: %v", err)
	}

	// Set up sphere store with an agent that has an active writ.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Echo", "myworld", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Set up world store with a writ.
	worldStore, err := store.OpenWorld("myworld")
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	writID, err := worldStore.CreateWrit("Test Writ Title", "Description", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	worldStore.Close()

	// Set active writ on the agent.
	if err := sphereStore.UpdateAgentState("myworld/Echo", store.AgentWorking, writID); err != nil {
		t.Fatalf("failed to update agent state: %v", err)
	}
	sphereStore.Close()

	// Now test envoyPrime.
	result := envoyPrime("myworld", "Echo")
	if !strings.Contains(result, "Envoy Echo") {
		t.Errorf("expected prime to contain agent name, got %q", result)
	}
	if !strings.Contains(result, "Active writ:") {
		t.Errorf("expected prime to contain active writ info, got %q", result)
	}
	if !strings.Contains(result, writID) {
		t.Errorf("expected prime to contain writ ID %q, got %q", writID, result)
	}
	if !strings.Contains(result, "Test Writ Title") {
		t.Errorf("expected prime to contain writ title, got %q", result)
	}
	if !strings.Contains(result, "sol prime") {
		t.Errorf("expected prime to contain sol prime command, got %q", result)
	}
}

func TestEnvoyHooksPreCompact(t *testing.T) {
	hooks := envoyHooks("myworld", "Echo")
	if len(hooks.PreCompact) == 0 {
		t.Fatal("envoy hooks missing PreCompact")
	}
	cmd := hooks.PreCompact[0].Command
	want := "sol prime --world=myworld --agent=Echo --compact"
	if cmd != want {
		t.Errorf("PreCompact command = %q, want %q", cmd, want)
	}
}

func TestEnvoyHooksNoCompactSessionStart(t *testing.T) {
	hooks := envoyHooks("myworld", "Echo")
	// After brief retirement, envoy has no SessionStart hooks; Claude Code's
	// native auto-memory loads <envoyDir>/memory/MEMORY.md automatically.
	for _, g := range hooks.SessionStart {
		if strings.Contains(g.Matcher, "compact") {
			t.Error("envoy SessionStart should not have a compact matcher")
		}
	}
}

func TestEnvoyDir(t *testing.T) {
	t.Setenv("SOL_HOME", "/tmp/sol-test")

	tests := []struct {
		name string
		fn   func(string, string) string
		want string
	}{
		{"EnvoyDir", EnvoyDir, "/tmp/sol-test/myworld/envoys/Echo"},
		{"WorktreePath", WorktreePath, "/tmp/sol-test/myworld/envoys/Echo/worktree"},
		{"PersonaPath", PersonaPath, "/tmp/sol-test/myworld/envoys/Echo/persona.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn("myworld", "Echo")
			if got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestCreate(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	sourceRepo := filepath.Join(tmp, "repo")
	initGitRepo(t, sourceRepo)

	ss := &mockSphereStore{agents: map[string]store.Agent{}}

	err := Create(CreateOpts{
		World:      "myworld",
		Name:       "Echo",
		SourceRepo: sourceRepo,
	}, ss)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify agent created with role "envoy".
	agent, ok := ss.agents["myworld/Echo"]
	if !ok {
		t.Fatal("agent not created in store")
	}
	if agent.Role != "envoy" {
		t.Errorf("agent role = %q, want \"envoy\"", agent.Role)
	}

	// Verify directory structure.
	if _, err := os.Stat(EnvoyDir("myworld", "Echo")); os.IsNotExist(err) {
		t.Error("envoy directory not created")
	}

	worktree := WorktreePath("myworld", "Echo")
	if _, err := os.Stat(worktree); os.IsNotExist(err) {
		t.Error("worktree directory not created")
	}

	// Verify worktree is valid git repo.
	cmd := exec.Command("git", "-C", worktree, "rev-parse", "--is-inside-work-tree")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("worktree is not a valid git repo: %s: %v", out, err)
	}

	// Verify worktree branch name.
	cmd = exec.Command("git", "-C", worktree, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to get branch: %s: %v", out, err)
	}
	branch := strings.TrimSpace(string(out))
	if branch != "envoy/myworld/Echo" {
		t.Errorf("worktree branch = %q, want \"envoy/myworld/Echo\"", branch)
	}
}

func TestCreateIdempotentWorktree(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	sourceRepo := filepath.Join(tmp, "repo")
	initGitRepo(t, sourceRepo)

	opts := CreateOpts{
		World:      "myworld",
		Name:       "Echo",
		SourceRepo: sourceRepo,
	}

	// First create.
	ss1 := &mockSphereStore{agents: map[string]store.Agent{}}
	if err := Create(opts, ss1); err != nil {
		t.Fatalf("first Create failed: %v", err)
	}

	// Second create — worktree exists, should not error.
	ss2 := &mockSphereStore{agents: map[string]store.Agent{}}
	if err := Create(opts, ss2); err != nil {
		t.Fatalf("second Create (idempotent worktree) failed: %v", err)
	}
}

func TestCreateRollbackOnWorktreeFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// Use a nonexistent source repo so worktree creation fails.
	ss := &mockSphereStore{agents: map[string]store.Agent{}}

	err := Create(CreateOpts{
		World:      "myworld",
		Name:       "Echo",
		SourceRepo: filepath.Join(tmp, "no-such-repo"),
	}, ss)
	if err == nil {
		t.Fatal("expected error from Create with invalid source repo")
	}

	// Verify rollback: agent record deleted.
	if _, ok := ss.agents["myworld/Echo"]; ok {
		t.Error("agent record should have been deleted by rollback")
	}
	if len(ss.deleted) != 1 || ss.deleted[0] != "myworld/Echo" {
		t.Errorf("expected DeleteAgent called with \"myworld/Echo\", got %v", ss.deleted)
	}

	// Verify rollback: envoy directory removed.
	if _, err := os.Stat(EnvoyDir("myworld", "Echo")); !os.IsNotExist(err) {
		t.Error("envoy directory should have been removed by rollback")
	}
}

func TestCreateRollbackOnAgentRecordFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	sourceRepo := filepath.Join(tmp, "repo")
	initGitRepo(t, sourceRepo)

	// Agent creation fails (e.g., name conflict).
	ss := &mockSphereStore{
		agents:    map[string]store.Agent{},
		createErr: fmt.Errorf("UNIQUE constraint failed"),
	}

	err := Create(CreateOpts{
		World:      "myworld",
		Name:       "Echo",
		SourceRepo: sourceRepo,
	}, ss)
	if err == nil {
		t.Fatal("expected error from Create")
	}

	// Verify no directory was created (agent record is step 1, fails before any dirs).
	if _, err := os.Stat(EnvoyDir("myworld", "Echo")); !os.IsNotExist(err) {
		t.Error("envoy directory should not exist when agent creation fails")
	}
}

func TestStop(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ss := &mockStopStore{
		agents: map[string]*store.Agent{
			"myworld/Echo": {ID: "myworld/Echo", Name: "Echo", World: "myworld", Role: "envoy", State: store.AgentIdle},
		},
		updated: map[string]store.AgentState{},
	}

	sessName := config.SessionName("myworld", "Echo")
	mgr := &mockStopManager{sessions: map[string]bool{sessName: true}}

	err := Stop("myworld", "Echo", ss, mgr)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Verify session stopped.
	if mgr.sessions[sessName] {
		t.Error("session not stopped")
	}

	// Verify agent state updated.
	if ss.updated["myworld/Echo"] != store.AgentIdle {
		t.Errorf("agent state = %q, want \"idle\"", ss.updated["myworld/Echo"])
	}
}

func TestStopNoSession(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ss := &mockStopStore{
		agents: map[string]*store.Agent{
			"myworld/Echo": {ID: "myworld/Echo", Name: "Echo", World: "myworld", Role: "envoy", State: store.AgentIdle},
		},
		updated: map[string]store.AgentState{},
	}

	mgr := &mockStopManager{sessions: map[string]bool{}}

	err := Stop("myworld", "Echo", ss, mgr)
	if err != nil {
		t.Fatalf("Stop should not error when session doesn't exist: %v", err)
	}

	// Verify agent state still updated to idle.
	if ss.updated["myworld/Echo"] != store.AgentIdle {
		t.Errorf("agent state = %q, want \"idle\"", ss.updated["myworld/Echo"])
	}
}

func TestStopWrongRole(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ss := &mockStopStore{
		agents: map[string]*store.Agent{
			"myworld/outpost-agent": {ID: "myworld/outpost-agent", Name: "outpost-agent", World: "myworld", Role: "outpost", State: store.AgentIdle},
		},
		updated: map[string]store.AgentState{},
	}
	mgr := &mockStopManager{sessions: map[string]bool{}}

	err := Stop("myworld", "outpost-agent", ss, mgr)
	if err == nil {
		t.Fatal("expected error for wrong role, got nil")
	}
	if !strings.Contains(err.Error(), "expected \"envoy\"") {
		t.Errorf("error should mention expected role, got %q", err.Error())
	}
	// Verify agent state was NOT updated.
	if _, ok := ss.updated["myworld/outpost-agent"]; ok {
		t.Error("agent state should not have been updated for wrong-role agent")
	}
}

func TestStopUpdatesStateEvenWhenGracefulStopFails(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ss := &mockStopStore{
		agents: map[string]*store.Agent{
			"myworld/Echo": {ID: "myworld/Echo", Name: "Echo", World: "myworld", Role: "envoy", State: store.AgentWorking, ActiveWrit: "sol-abc123"},
		},
		updated:     map[string]store.AgentState{},
		activeWrits: map[string]string{},
	}

	sessName := config.SessionName("myworld", "Echo")
	mgr := &mockStopManager{
		sessions: map[string]bool{sessName: true},
		stopErr:  fmt.Errorf("tmux: session vanished"),
	}

	err := Stop("myworld", "Echo", ss, mgr)
	// Stop should return the GracefulStop error.
	if err == nil {
		t.Fatal("expected error when GracefulStop fails, got nil")
	}

	// Agent state should be updated to idle despite the stop error.
	if ss.updated["myworld/Echo"] != store.AgentIdle {
		t.Errorf("agent state = %q, want \"idle\" (state must be updated even on stop failure)", ss.updated["myworld/Echo"])
	}
	// ActiveWrit should be preserved.
	if got := ss.activeWrits["myworld/Echo"]; got != "sol-abc123" {
		t.Errorf("active_writ after failed stop = %q, want %q", got, "sol-abc123")
	}
}

func TestStopPreservesActiveWrit(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	const writID = "sol-abc123def456abc1"
	ss := &mockStopStore{
		agents: map[string]*store.Agent{
			"myworld/Echo": {ID: "myworld/Echo", Name: "Echo", World: "myworld", Role: "envoy", State: store.AgentWorking, ActiveWrit: writID},
		},
		updated:     map[string]store.AgentState{},
		activeWrits: map[string]string{},
	}

	mgr := &mockStopManager{sessions: map[string]bool{}}

	if err := Stop("myworld", "Echo", ss, mgr); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if got := ss.activeWrits["myworld/Echo"]; got != writID {
		t.Errorf("active_writ after stop = %q, want %q (should be preserved)", got, writID)
	}
}

func TestList(t *testing.T) {
	agents := []store.Agent{
		{ID: "w/A", Name: "A", World: "w", Role: "outpost"},
		{ID: "w/B", Name: "B", World: "w", Role: "envoy"},
		{ID: "w/C", Name: "C", World: "w", Role: "envoy"},
		{ID: "w/D", Name: "D", World: "w", Role: "forge"},
		{ID: "x/E", Name: "E", World: "x", Role: "envoy"},
	}

	ls := &mockListStore{agents: agents}

	// Filter by world "w".
	result, err := List("w", ls)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 envoys for world \"w\", got %d", len(result))
	}
	for _, a := range result {
		if a.Role != "envoy" {
			t.Errorf("expected role \"envoy\", got %q", a.Role)
		}
	}

	// All envoys (empty world).
	result, err = List("", ls)
	if err != nil {
		t.Fatalf("List (all) failed: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 envoys across all worlds, got %d", len(result))
	}
}

func TestEnvoyStart(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	writeTestToken(t, tmp)

	// Create required dirs.
	if err := os.MkdirAll(filepath.Join(tmp, ".store"), 0o755); err != nil {
		t.Fatalf("failed to create .store dir: %v", err)
	}

	// Create envoy worktree dir (startup.Launch checks it exists).
	worktreeDir := WorktreePath("myworld", "Echo")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	// Open sphere store.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	mock := &mockSessionStarter{}

	_, err = startup.Launch(RoleConfig(), "myworld", "Echo", startup.LaunchOpts{
		Sessions: mock,
		Sphere:   sphereStore,
	})
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	// Verify hooks file written.
	hooksPath := filepath.Join(worktreeDir, ".claude", "settings.local.json")
	if _, err := os.Stat(hooksPath); err != nil {
		t.Errorf("hooks file not written: %v", err)
	}

	// Verify persona injected.
	personaPath := filepath.Join(worktreeDir, "CLAUDE.local.md")
	data, err := os.ReadFile(personaPath)
	if err != nil {
		t.Fatalf("persona not written: %v", err)
	}
	if !strings.Contains(string(data), "Echo") {
		t.Errorf("persona missing agent name, got: %q", string(data))
	}
}

// --- mockDeleteStore ---

type mockDeleteStore struct {
	agents       map[string]*store.Agent
	getErr       error
	deleteErr    error
	deleted      []string
	escalations  []mockEscalation
	escalateErr  error
}

type mockEscalation struct {
	severity    string
	source      string
	description string
	sourceRef   string
}

func (m *mockDeleteStore) CreateEscalation(severity, source, description string, sourceRef ...string) (string, error) {
	if m.escalateErr != nil {
		return "", m.escalateErr
	}
	ref := ""
	if len(sourceRef) > 0 {
		ref = sourceRef[0]
	}
	m.escalations = append(m.escalations, mockEscalation{
		severity:    severity,
		source:      source,
		description: description,
		sourceRef:   ref,
	})
	return fmt.Sprintf("esc-%d", len(m.escalations)), nil
}

func (m *mockDeleteStore) GetAgent(id string) (*store.Agent, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	a, ok := m.agents[id]
	if !ok {
		return nil, fmt.Errorf("agent %q: %w", id, store.ErrNotFound)
	}
	return a, nil
}

func (m *mockDeleteStore) DeleteAgent(id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.deleted = append(m.deleted, id)
	delete(m.agents, id)
	return nil
}

// --- mockWritReopener ---

type mockWritReopener struct {
	updates map[string]store.WritUpdates
	err     error
}

func (m *mockWritReopener) UpdateWrit(id string, updates store.WritUpdates) error {
	if m.err != nil {
		return m.err
	}
	if m.updates == nil {
		m.updates = map[string]store.WritUpdates{}
	}
	m.updates[id] = updates
	return nil
}

// newEnvoyAgent returns a store.Agent with role "envoy" for use in Delete tests.
func newEnvoyAgent(world, name string) *store.Agent {
	return &store.Agent{
		ID:    world + "/" + name,
		Name:  name,
		World: world,
		Role:  "envoy",
		State: store.AgentIdle,
	}
}

// setupEnvoy creates a git repo and provisions a real envoy in the temp dir,
// then returns a matching mockDeleteStore for use in Delete tests.
func setupEnvoy(t *testing.T, tmp, world, name string) (sourceRepo string, ds *mockDeleteStore) {
	t.Helper()
	sourceRepo = filepath.Join(tmp, "repo")
	initGitRepo(t, sourceRepo)

	ss := &mockSphereStore{agents: map[string]store.Agent{}}
	if err := Create(CreateOpts{
		World:      world,
		Name:       name,
		SourceRepo: sourceRepo,
	}, ss); err != nil {
		t.Fatalf("setupEnvoy: Create failed: %v", err)
	}

	ds = &mockDeleteStore{
		agents: map[string]*store.Agent{
			world + "/" + name: newEnvoyAgent(world, name),
		},
	}
	return sourceRepo, ds
}

// --- Delete Tests ---

func TestDeleteHappyPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	world, name := "myworld", "Echo"
	sourceRepo, ds := setupEnvoy(t, tmp, world, name)
	mgr := &mockStopManager{sessions: map[string]bool{}}

	// Verify preconditions: dirs exist, no tether, no session.
	if _, err := os.Stat(EnvoyDir(world, name)); os.IsNotExist(err) {
		t.Fatal("precondition: envoy dir should exist before Delete")
	}
	if _, err := os.Stat(WorktreePath(world, name)); os.IsNotExist(err) {
		t.Fatal("precondition: worktree should exist before Delete")
	}

	if err := Delete(DeleteOpts{
		World:      world,
		Name:       name,
		SourceRepo: sourceRepo,
		Force:      false,
	}, ds, mgr); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Agent record deleted.
	if len(ds.deleted) != 1 || ds.deleted[0] != world+"/"+name {
		t.Errorf("DeleteAgent not called correctly, got %v", ds.deleted)
	}
	if _, ok := ds.agents[world+"/"+name]; ok {
		t.Error("agent should have been removed from store")
	}

	// Envoy directory removed (covers worktree and memory dirs too).
	if _, err := os.Stat(EnvoyDir(world, name)); !os.IsNotExist(err) {
		t.Error("envoy directory should have been removed")
	}

	// Git branch deleted.
	branch := "envoy/" + world + "/" + name
	cmd := exec.Command("git", "-C", sourceRepo, "branch", "--list", branch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git branch --list failed: %v", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("git branch %q should have been deleted, still listed", branch)
	}
}

func TestDeleteAgentNotFound(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ds := &mockDeleteStore{
		agents: map[string]*store.Agent{},
		getErr: fmt.Errorf("agent %q: %w", "myworld/Echo", store.ErrNotFound),
	}
	mgr := &mockStopManager{sessions: map[string]bool{}}

	err := Delete(DeleteOpts{
		World:      "myworld",
		Name:       "Echo",
		SourceRepo: tmp,
		Force:      false,
	}, ds, mgr)
	if err == nil {
		t.Fatal("expected error when agent not found, got nil")
	}
}

func TestDeleteWrongRole(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ds := &mockDeleteStore{
		agents: map[string]*store.Agent{
			"myworld/Echo": {
				ID:    "myworld/Echo",
				Name:  "Echo",
				World: "myworld",
				Role:  "outpost", // wrong role
				State: store.AgentIdle,
			},
		},
	}
	mgr := &mockStopManager{sessions: map[string]bool{}}

	err := Delete(DeleteOpts{
		World:      "myworld",
		Name:       "Echo",
		SourceRepo: tmp,
		Force:      false,
	}, ds, mgr)
	if err == nil {
		t.Fatal("expected error for wrong role, got nil")
	}
	if !strings.Contains(err.Error(), "expected \"envoy\"") {
		t.Errorf("error should mention expected role, got %q", err.Error())
	}
}

func TestDeleteActiveSessionRefuses(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	world, name := "myworld", "Echo"
	ds := &mockDeleteStore{
		agents: map[string]*store.Agent{
			world + "/" + name: newEnvoyAgent(world, name),
		},
	}
	sessName := config.SessionName(world, name)
	mgr := &mockStopManager{sessions: map[string]bool{sessName: true}}

	err := Delete(DeleteOpts{
		World:      world,
		Name:       name,
		SourceRepo: tmp,
		Force:      false,
	}, ds, mgr)
	if err == nil {
		t.Fatal("expected error for active session with Force=false, got nil")
	}
	if !strings.Contains(err.Error(), "active session") {
		t.Errorf("error should mention active session, got %q", err.Error())
	}
	// Session should not have been stopped.
	if !mgr.sessions[sessName] {
		t.Error("session should still be running after refused delete")
	}
}

func TestDeleteActiveSessionForce(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	world, name := "myworld", "Echo"
	sourceRepo, ds := setupEnvoy(t, tmp, world, name)

	sessName := config.SessionName(world, name)
	mgr := &mockStopManager{sessions: map[string]bool{sessName: true}}

	if err := Delete(DeleteOpts{
		World:      world,
		Name:       name,
		SourceRepo: sourceRepo,
		Force:      true,
	}, ds, mgr); err != nil {
		t.Fatalf("Delete with Force=true failed: %v", err)
	}

	// Session should have been stopped.
	if mgr.sessions[sessName] {
		t.Error("session should have been stopped when Force=true")
	}

	// Agent record deleted.
	if len(ds.deleted) != 1 {
		t.Errorf("expected DeleteAgent called once, got %d", len(ds.deleted))
	}

	// Envoy directory removed.
	if _, err := os.Stat(EnvoyDir(world, name)); !os.IsNotExist(err) {
		t.Error("envoy directory should have been removed")
	}
}

func TestDeleteTetheredRefuses(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	world, name := "myworld", "Echo"

	// Create the envoy directory so tether.Write has a valid path.
	envoyDir := EnvoyDir(world, name)
	if err := os.MkdirAll(envoyDir, 0o755); err != nil {
		t.Fatalf("failed to create envoy dir: %v", err)
	}

	// Write a tether file.
	if err := tether.Write(world, name, "sol-abc12345abcdef01", "envoy"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	ds := &mockDeleteStore{
		agents: map[string]*store.Agent{
			world + "/" + name: newEnvoyAgent(world, name),
		},
	}
	mgr := &mockStopManager{sessions: map[string]bool{}}

	err := Delete(DeleteOpts{
		World:      world,
		Name:       name,
		SourceRepo: tmp,
		Force:      false,
	}, ds, mgr)
	if err == nil {
		t.Fatal("expected error for tethered envoy with Force=false, got nil")
	}
	if !strings.Contains(err.Error(), "tethered") {
		t.Errorf("error should mention tether, got %q", err.Error())
	}

	// Tether should still be present.
	if !tether.IsTethered(world, name, "envoy") {
		t.Error("tether should still be present after refused delete")
	}
}

func TestDeleteTetheredForce(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	world, name := "myworld", "Echo"
	sourceRepo, ds := setupEnvoy(t, tmp, world, name)
	mgr := &mockStopManager{sessions: map[string]bool{}}
	ws := &mockWritReopener{}

	writID := "sol-abc12345abcdef01"

	// Write a tether file.
	if err := tether.Write(world, name, writID, "envoy"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}
	if !tether.IsTethered(world, name, "envoy") {
		t.Fatal("precondition: tether should be present before Delete")
	}

	if err := Delete(DeleteOpts{
		World:      world,
		Name:       name,
		SourceRepo: sourceRepo,
		Force:      true,
		WorldStore: ws,
	}, ds, mgr); err != nil {
		t.Fatalf("Delete with Force=true (tethered) failed: %v", err)
	}

	// Tether should have been cleared.
	if tether.IsTethered(world, name, "envoy") {
		t.Error("tether should have been cleared when Force=true")
	}

	// Agent record deleted.
	if len(ds.deleted) != 1 {
		t.Errorf("expected DeleteAgent called once, got %d", len(ds.deleted))
	}

	// Envoy directory removed.
	if _, err := os.Stat(EnvoyDir(world, name)); !os.IsNotExist(err) {
		t.Error("envoy directory should have been removed")
	}

	// Writ should have been reopened.
	update, ok := ws.updates[writID]
	if !ok {
		t.Fatal("expected writ to be reopened via UpdateWrit")
	}
	if update.Status != "open" {
		t.Errorf("expected writ status 'open', got %q", update.Status)
	}
	if update.Assignee != "-" {
		t.Errorf("expected writ assignee '-', got %q", update.Assignee)
	}
}

func TestDeleteTetheredForceWritReopenFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	world, name := "myworld", "Echo"
	sourceRepo, ds := setupEnvoy(t, tmp, world, name)
	mgr := &mockStopManager{sessions: map[string]bool{}}
	ws := &mockWritReopener{err: fmt.Errorf("db locked")}

	// Write a tether file.
	if err := tether.Write(world, name, "sol-abc12345abcdef01", "envoy"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Delete should succeed even when writ update fails.
	if err := Delete(DeleteOpts{
		World:      world,
		Name:       name,
		SourceRepo: sourceRepo,
		Force:      true,
		WorldStore: ws,
	}, ds, mgr); err != nil {
		t.Fatalf("Delete should succeed despite writ reopen failure: %v", err)
	}

	// Agent should still be deleted.
	if len(ds.deleted) != 1 {
		t.Errorf("expected DeleteAgent called once, got %d", len(ds.deleted))
	}
}

// TestDeleteTetherListErrorRefuses verifies that when tether.List fails
// (e.g. the .tether path is corrupted into a regular file → ENOTDIR), Delete
// refuses without --force and propagates the underlying error.
func TestDeleteTetherListErrorRefuses(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	world, name := "myworld", "Echo"
	sourceRepo, ds := setupEnvoy(t, tmp, world, name)
	mgr := &mockStopManager{sessions: map[string]bool{}}

	// Replace the .tether directory with a regular file so ReadDir fails
	// with ENOTDIR. This is a deterministic, root-safe failure mode.
	tetherPath := tether.TetherDir(world, name, "envoy")
	if err := os.MkdirAll(filepath.Dir(tetherPath), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	// Ensure path doesn't already exist as a directory.
	_ = os.RemoveAll(tetherPath)
	if err := os.WriteFile(tetherPath, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("failed to write fake tether file: %v", err)
	}

	err := Delete(DeleteOpts{
		World:      world,
		Name:       name,
		SourceRepo: sourceRepo,
		Force:      false,
	}, ds, mgr)
	if err == nil {
		t.Fatal("expected error when tether.List fails, got nil")
	}
	if !strings.Contains(err.Error(), "cannot enumerate tether") {
		t.Errorf("error should mention enumeration failure, got %q", err.Error())
	}

	// Without --force, no escalation should be created.
	if len(ds.escalations) != 0 {
		t.Errorf("expected no escalations on non-force refusal, got %d", len(ds.escalations))
	}

	// Agent should NOT be deleted.
	if len(ds.deleted) != 0 {
		t.Errorf("agent should not be deleted when tether enumeration fails, deleted=%v", ds.deleted)
	}
}

// TestDeleteTetherListErrorForceCreatesEscalation verifies that --force does
// NOT override an enumeration failure: the delete is still refused, AND an
// escalation is recorded so the operator notices.
func TestDeleteTetherListErrorForceCreatesEscalation(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	world, name := "myworld", "Echo"
	sourceRepo, ds := setupEnvoy(t, tmp, world, name)
	mgr := &mockStopManager{sessions: map[string]bool{}}
	ws := &mockWritReopener{}

	// Corrupt the tether path: make it a regular file so ReadDir fails.
	tetherPath := tether.TetherDir(world, name, "envoy")
	if err := os.MkdirAll(filepath.Dir(tetherPath), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	_ = os.RemoveAll(tetherPath)
	if err := os.WriteFile(tetherPath, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("failed to write fake tether file: %v", err)
	}

	err := Delete(DeleteOpts{
		World:      world,
		Name:       name,
		SourceRepo: sourceRepo,
		Force:      true,
		WorldStore: ws,
	}, ds, mgr)
	if err == nil {
		t.Fatal("expected error even with --force when tether.List fails, got nil")
	}
	if !strings.Contains(err.Error(), "cannot enumerate tether") {
		t.Errorf("error should mention enumeration failure, got %q", err.Error())
	}

	// Force path must record an escalation so the operator notices.
	if len(ds.escalations) != 1 {
		t.Fatalf("expected 1 escalation on force-refusal, got %d", len(ds.escalations))
	}
	esc := ds.escalations[0]
	if esc.severity != "high" {
		t.Errorf("expected severity 'high', got %q", esc.severity)
	}
	if esc.source != "envoy.delete" {
		t.Errorf("expected source 'envoy.delete', got %q", esc.source)
	}
	if !strings.Contains(esc.description, name) {
		t.Errorf("escalation description should mention envoy name, got %q", esc.description)
	}
	if !strings.Contains(esc.sourceRef, "envoy:") {
		t.Errorf("escalation sourceRef should reference envoy, got %q", esc.sourceRef)
	}

	// Agent must NOT be deleted — refusing to orphan the writs is the whole point.
	if len(ds.deleted) != 0 {
		t.Errorf("agent should not be deleted when tether enumeration fails, deleted=%v", ds.deleted)
	}
	// Writ reopen must NOT have been attempted (we don't know which writs to reopen).
	if len(ws.updates) != 0 {
		t.Errorf("writ reopen should not run when enumeration fails, got %d updates", len(ws.updates))
	}
}

func TestDeleteTetheredForceNoWorldStore(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	world, name := "myworld", "Echo"
	sourceRepo, ds := setupEnvoy(t, tmp, world, name)
	mgr := &mockStopManager{sessions: map[string]bool{}}

	// Write a tether file.
	if err := tether.Write(world, name, "sol-abc12345abcdef01", "envoy"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Delete should succeed without world store (nil WorldStore, backward compat).
	if err := Delete(DeleteOpts{
		World:      world,
		Name:       name,
		SourceRepo: sourceRepo,
		Force:      true,
	}, ds, mgr); err != nil {
		t.Fatalf("Delete should succeed without WorldStore: %v", err)
	}

	if len(ds.deleted) != 1 {
		t.Errorf("expected DeleteAgent called once, got %d", len(ds.deleted))
	}
}
