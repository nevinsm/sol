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

func (m *mockSphereStore) UpdateAgentState(id string, state store.AgentState, activeWrit string) error {
	return nil // not exercised in Create tests
}

func (m *mockSphereStore) EnsureAgent(name, world, role string) error {
	return nil // not exercised in Create tests
}

func (m *mockSphereStore) DeleteAgentsForWorld(world string) error {
	return nil // not exercised in Create tests
}

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

type mockListStore struct {
	agents  []store.Agent
	listErr error
}

func (m *mockListStore) GetAgent(id string) (*store.Agent, error) {
	return nil, nil // not exercised in List tests
}

func (m *mockListStore) FindIdleAgent(world string) (*store.Agent, error) {
	return nil, nil // not exercised in List tests
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
	if len(hooks.SessionStart) == 0 {
		t.Fatal("envoy hooks missing SessionStart")
	}
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
		{"BriefDir", BriefDir, "/tmp/sol-test/myworld/envoys/Echo/.brief"},
		{"BriefPath", BriefPath, "/tmp/sol-test/myworld/envoys/Echo/.brief/memory.md"},
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
	if _, err := os.Stat(BriefDir("myworld", "Echo")); os.IsNotExist(err) {
		t.Error("brief directory not created")
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
