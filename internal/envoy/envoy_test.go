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
		State: "idle",
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

func (m *mockSessionManager) Inject(name string, text string, submit bool) error {
	return nil
}

func (m *mockSessionManager) Capture(name string, lines int) (string, error) {
	return "", nil
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

func TestStartRollbackOnStateUpdateFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

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
		updateErr: fmt.Errorf("database locked"),
	}

	mgr := &mockSessionManager{sessions: map[string]bool{}}

	err := Start(StartOpts{World: "myworld", Name: "Echo"}, ss, mgr)
	if err == nil {
		t.Fatal("expected error from Start when state update fails")
	}

	// Verify rollback: session was stopped.
	sessName := SessionName("myworld", "Echo")
	if mgr.sessions[sessName] {
		t.Error("session should have been stopped by rollback")
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

	if groups, ok := cfg.Hooks["SessionStart"]; !ok {
		t.Error("no SessionStart hooks")
	} else if len(groups) != 2 {
		t.Errorf("expected 2 SessionStart matcher groups, got %d", len(groups))
	}
	if _, ok := cfg.Hooks["Stop"]; ok {
		t.Error("unexpected Stop hooks — removed in favor of CLAUDE.md instructions")
	}
	if pcGroups, ok := cfg.Hooks["PreCompact"]; !ok {
		t.Error("no PreCompact hooks")
	} else if len(pcGroups) != 1 {
		t.Errorf("expected 1 PreCompact matcher group, got %d", len(pcGroups))
	} else if pcGroups[0].Hooks[0].Command != "sol handoff --world=myworld --agent=Echo" {
		t.Errorf("unexpected PreCompact command: %q", pcGroups[0].Hooks[0].Command)
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
		if !strings.Contains(ptuGroups[0].Hooks[0].Command, ".claude/projects") {
			t.Error("PreToolUse hook should block .claude/projects/*/memory/ paths")
		}
		if !strings.Contains(ptuGroups[0].Hooks[0].Command, "exit 2") {
			t.Error("PreToolUse hook should exit 2 to block the tool call")
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
