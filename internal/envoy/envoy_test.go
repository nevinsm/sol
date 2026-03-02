package envoy

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/store"
)

// --- Mocks ---

type mockSphereStore struct {
	agents    map[string]store.Agent
	createErr error
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
		State: "idle",
	}
	return id, nil
}

type mockStartStore struct {
	agents    map[string]*store.Agent
	getErr    error
	updateErr error
}

func (m *mockStartStore) GetAgent(id string) (*store.Agent, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	a, ok := m.agents[id]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", id)
	}
	return a, nil
}

func (m *mockStartStore) UpdateAgentState(id, state, tetherItem string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	if a, ok := m.agents[id]; ok {
		a.State = state
		a.TetherItem = tetherItem
	}
	return nil
}

type mockSessionManager struct {
	sessions map[string]bool
	startErr error
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

type mockListStore struct {
	agents  []store.Agent
	listErr error
}

func (m *mockListStore) ListAgents(world string, state string) ([]store.Agent, error) {
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

func TestStart(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// Create worktree directory (simulating a prior Create).
	worktree := WorktreePath("myworld", "Echo")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}

	ss := &mockStartStore{
		agents: map[string]*store.Agent{
			"myworld/Echo": {
				ID:    "myworld/Echo",
				Name:  "Echo",
				World: "myworld",
				Role:  "envoy",
				State: "idle",
			},
		},
	}

	mgr := &mockSessionManager{sessions: map[string]bool{}}

	err := Start(StartOpts{World: "myworld", Name: "Echo"}, ss, mgr)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify session started with correct name and workdir.
	sessName := SessionName("myworld", "Echo")
	if !mgr.sessions[sessName] {
		t.Error("session not started")
	}
	if mgr.lastStart.name != sessName {
		t.Errorf("session name = %q, want %q", mgr.lastStart.name, sessName)
	}
	if mgr.lastStart.workdir != worktree {
		t.Errorf("workdir = %q, want %q", mgr.lastStart.workdir, worktree)
	}
	if mgr.lastStart.role != "envoy" {
		t.Errorf("role = %q, want \"envoy\"", mgr.lastStart.role)
	}

	// Verify hooks file written.
	hooksPath := filepath.Join(worktree, ".claude", "settings.local.json")
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

	// CLAUDE.md is now installed by the CLI layer (following forge pattern),
	// so we don't check for it here.
}

func TestStartAlreadyRunning(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ss := &mockStartStore{
		agents: map[string]*store.Agent{
			"myworld/Echo": {
				ID:    "myworld/Echo",
				Name:  "Echo",
				World: "myworld",
				Role:  "envoy",
				State: "idle",
			},
		},
	}

	sessName := SessionName("myworld", "Echo")
	mgr := &mockSessionManager{sessions: map[string]bool{sessName: true}}

	err := Start(StartOpts{World: "myworld", Name: "Echo"}, ss, mgr)
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

	ss := &mockStartStore{
		agents: map[string]*store.Agent{
			"myworld/Echo": {
				ID:    "myworld/Echo",
				Name:  "Echo",
				World: "myworld",
				Role:  "envoy",
				State: "working",
			},
		},
	}

	sessName := SessionName("myworld", "Echo")
	mgr := &mockSessionManager{sessions: map[string]bool{sessName: true}}

	err := Stop("myworld", "Echo", ss, mgr)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Verify session stopped.
	if mgr.sessions[sessName] {
		t.Error("session not stopped")
	}

	// Verify agent state updated.
	agent := ss.agents["myworld/Echo"]
	if agent.State != "idle" {
		t.Errorf("agent state = %q, want \"idle\"", agent.State)
	}
}

func TestStopNoSession(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ss := &mockStartStore{
		agents: map[string]*store.Agent{
			"myworld/Echo": {
				ID:    "myworld/Echo",
				Name:  "Echo",
				World: "myworld",
				Role:  "envoy",
				State: "working",
			},
		},
	}

	mgr := &mockSessionManager{sessions: map[string]bool{}}

	err := Stop("myworld", "Echo", ss, mgr)
	if err != nil {
		t.Fatalf("Stop should not error when session doesn't exist: %v", err)
	}

	// Verify agent state still updated to idle.
	agent := ss.agents["myworld/Echo"]
	if agent.State != "idle" {
		t.Errorf("agent state = %q, want \"idle\"", agent.State)
	}
}

func TestList(t *testing.T) {
	agents := []store.Agent{
		{ID: "w/A", Name: "A", World: "w", Role: "agent"},
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
