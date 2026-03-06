package envoy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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

type mockStopStore struct {
	updated   map[string]string // id -> state
	updateErr error
}

func (m *mockStopStore) UpdateAgentState(id, state, tetherItem string) error {
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

func TestStop(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ss := &mockStopStore{
		updated: map[string]string{},
	}

	sessName := SessionName("myworld", "Echo")
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
	if ss.updated["myworld/Echo"] != "idle" {
		t.Errorf("agent state = %q, want \"idle\"", ss.updated["myworld/Echo"])
	}
}

func TestStopNoSession(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ss := &mockStopStore{
		updated: map[string]string{},
	}

	mgr := &mockStopManager{sessions: map[string]bool{}}

	err := Stop("myworld", "Echo", ss, mgr)
	if err != nil {
		t.Fatalf("Stop should not error when session doesn't exist: %v", err)
	}

	// Verify agent state still updated to idle.
	if ss.updated["myworld/Echo"] != "idle" {
		t.Errorf("agent state = %q, want \"idle\"", ss.updated["myworld/Echo"])
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
