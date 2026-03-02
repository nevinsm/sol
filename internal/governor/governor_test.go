package governor

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/protocol"
)

// --- Mocks ---

type mockSphereStore struct {
	ensured   map[string]bool
	updated   map[string]string // id -> state
	ensureErr error
	updateErr error
}

func (m *mockSphereStore) EnsureAgent(name, world, role string) error {
	if m.ensureErr != nil {
		return m.ensureErr
	}
	id := world + "/" + name
	m.ensured[id] = true
	return nil
}

func (m *mockSphereStore) UpdateAgentState(id, state, tetherItem string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.updated[id] = state
	return nil
}

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

func TestGovernorDir(t *testing.T) {
	t.Setenv("SOL_HOME", "/tmp/sol-test")

	tests := []struct {
		name string
		fn   func(string) string
		want string
	}{
		{"GovernorDir", GovernorDir, "/tmp/sol-test/myworld/governor"},
		{"MirrorPath", MirrorPath, "/tmp/sol-test/myworld/governor/mirror"},
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

func TestSetupMirrorClone(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	sourceRepo := filepath.Join(tmp, "repo")
	initGitRepo(t, sourceRepo)

	err := SetupMirror("myworld", sourceRepo, "")
	if err != nil {
		t.Fatalf("SetupMirror (clone) failed: %v", err)
	}

	mirrorPath := MirrorPath("myworld")

	// Verify mirror exists.
	if _, err := os.Stat(mirrorPath); os.IsNotExist(err) {
		t.Fatal("mirror directory not created")
	}

	// Verify mirror is a valid git repo.
	cmd := exec.Command("git", "-C", mirrorPath, "rev-parse", "--is-inside-work-tree")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mirror is not a valid git repo: %s: %v", out, err)
	}

	// Verify initial commit is present.
	cmd = exec.Command("git", "-C", mirrorPath, "log", "--oneline")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log failed: %s: %v", out, err)
	}
	if !strings.Contains(string(out), "initial") {
		t.Errorf("mirror missing initial commit, got: %s", out)
	}
}

func TestSetupMirrorRefresh(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	sourceRepo := filepath.Join(tmp, "repo")
	initGitRepo(t, sourceRepo)

	// First call — clones.
	if err := SetupMirror("myworld", sourceRepo, ""); err != nil {
		t.Fatalf("SetupMirror (clone) failed: %v", err)
	}

	// Add a commit to the source repo.
	newFile := filepath.Join(sourceRepo, "new.txt")
	if err := os.WriteFile(newFile, []byte("new content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", sourceRepo, "add", ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %s: %v", out, err)
	}
	cmd = exec.Command("git", "-C", sourceRepo, "commit", "-m", "second commit")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %s: %v", out, err)
	}

	// Second call — pulls.
	if err := SetupMirror("myworld", sourceRepo, ""); err != nil {
		t.Fatalf("SetupMirror (refresh) failed: %v", err)
	}

	// Verify new commit is visible in the mirror.
	mirrorPath := MirrorPath("myworld")
	cmd = exec.Command("git", "-C", mirrorPath, "log", "--oneline")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log failed: %s: %v", out, err)
	}
	if !strings.Contains(string(out), "second commit") {
		t.Errorf("mirror missing second commit, got: %s", out)
	}
}

func TestRefreshMirror(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	sourceRepo := filepath.Join(tmp, "repo")
	initGitRepo(t, sourceRepo)

	// Clone mirror first.
	if err := SetupMirror("myworld", sourceRepo, ""); err != nil {
		t.Fatalf("SetupMirror failed: %v", err)
	}

	// Add a commit to the source repo.
	newFile := filepath.Join(sourceRepo, "refresh.txt")
	if err := os.WriteFile(newFile, []byte("refresh content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", sourceRepo, "add", ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %s: %v", out, err)
	}
	cmd = exec.Command("git", "-C", sourceRepo, "commit", "-m", "refresh commit")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %s: %v", out, err)
	}

	// RefreshMirror should pull the new commit.
	if err := RefreshMirror("myworld", ""); err != nil {
		t.Fatalf("RefreshMirror failed: %v", err)
	}

	mirrorPath := MirrorPath("myworld")
	cmd = exec.Command("git", "-C", mirrorPath, "log", "--oneline")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log failed: %s: %v", out, err)
	}
	if !strings.Contains(string(out), "refresh commit") {
		t.Errorf("mirror missing refresh commit, got: %s", out)
	}
}

func TestRefreshMirrorNoMirror(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	err := RefreshMirror("myworld", "")
	if err == nil {
		t.Fatal("expected error when mirror doesn't exist")
	}
	if !strings.Contains(err.Error(), "mirror not found") {
		t.Errorf("error = %q, want contains \"mirror not found\"", err.Error())
	}
}

func TestRefreshMirrorNonMainBranch(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	sourceRepo := filepath.Join(tmp, "repo")
	initGitRepo(t, sourceRepo)

	// Create a "develop" branch in the source repo.
	cmd := exec.Command("git", "-C", sourceRepo, "checkout", "-b", "develop")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b develop failed: %s: %v", out, err)
	}
	devFile := filepath.Join(sourceRepo, "develop.txt")
	if err := os.WriteFile(devFile, []byte("develop content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "-C", sourceRepo, "add", ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %s: %v", out, err)
	}
	cmd = exec.Command("git", "-C", sourceRepo, "commit", "-m", "develop commit")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %s: %v", out, err)
	}

	// Clone mirror with develop branch.
	if err := SetupMirror("myworld", sourceRepo, "develop"); err != nil {
		t.Fatalf("SetupMirror failed: %v", err)
	}

	// Verify mirror is on develop branch.
	mirrorPath := MirrorPath("myworld")
	cmd = exec.Command("git", "-C", mirrorPath, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse failed: %s: %v", out, err)
	}
	branch := strings.TrimSpace(string(out))
	if branch != "develop" {
		t.Errorf("mirror branch = %q, want \"develop\"", branch)
	}

	// Add a new commit on develop in source.
	devFile2 := filepath.Join(sourceRepo, "develop2.txt")
	if err := os.WriteFile(devFile2, []byte("more develop\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "-C", sourceRepo, "add", ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %s: %v", out, err)
	}
	cmd = exec.Command("git", "-C", sourceRepo, "commit", "-m", "develop commit 2")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %s: %v", out, err)
	}

	// RefreshMirror with develop should pull the new commit.
	if err := RefreshMirror("myworld", "develop"); err != nil {
		t.Fatalf("RefreshMirror failed: %v", err)
	}

	cmd = exec.Command("git", "-C", mirrorPath, "log", "--oneline")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log failed: %s: %v", out, err)
	}
	if !strings.Contains(string(out), "develop commit 2") {
		t.Errorf("mirror missing develop commit 2, got: %s", out)
	}
}

func TestStart(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	sourceRepo := filepath.Join(tmp, "repo")
	initGitRepo(t, sourceRepo)

	ss := &mockSphereStore{
		ensured: map[string]bool{},
		updated: map[string]string{},
	}

	mgr := &mockSessionManager{sessions: map[string]bool{}}

	err := Start(StartOpts{
		World:      "myworld",
		SourceRepo: sourceRepo,
	}, ss, mgr)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify agent ensured with role "governor".
	if !ss.ensured["myworld/governor"] {
		t.Error("agent not ensured in store")
	}

	// Verify agent state updated to "idle".
	if ss.updated["myworld/governor"] != "idle" {
		t.Errorf("agent state = %q, want \"idle\"", ss.updated["myworld/governor"])
	}

	// Verify session started with correct parameters.
	sessName := "sol-myworld-governor"
	if !mgr.sessions[sessName] {
		t.Error("session not started")
	}
	if mgr.lastStart.name != sessName {
		t.Errorf("session name = %q, want %q", mgr.lastStart.name, sessName)
	}
	govDir := GovernorDir("myworld")
	if mgr.lastStart.workdir != govDir {
		t.Errorf("workdir = %q, want %q", mgr.lastStart.workdir, govDir)
	}
	if mgr.lastStart.role != "governor" {
		t.Errorf("role = %q, want \"governor\"", mgr.lastStart.role)
	}
	if mgr.lastStart.world != "myworld" {
		t.Errorf("world = %q, want \"myworld\"", mgr.lastStart.world)
	}

	// Verify hooks file written.
	hooksPath := filepath.Join(govDir, ".claude", "settings.local.json")
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
		// Verify the startup/resume hook includes refresh-mirror with world.
		if !strings.Contains(hooks[0].Command, "sol governor refresh-mirror --world=myworld") {
			t.Errorf("startup hook missing refresh-mirror command: %q", hooks[0].Command)
		}
		if hooks[0].Matcher != "startup|resume" {
			t.Errorf("startup hook matcher = %q, want \"startup|resume\"", hooks[0].Matcher)
		}
		if hooks[1].Matcher != "compact" {
			t.Errorf("compact hook matcher = %q, want \"compact\"", hooks[1].Matcher)
		}
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

	// Verify mirror was cloned.
	mirrorPath := MirrorPath("myworld")
	if _, err := os.Stat(mirrorPath); os.IsNotExist(err) {
		t.Error("mirror not cloned")
	}

	// Verify brief directory created.
	briefDir := BriefDir("myworld")
	if _, err := os.Stat(briefDir); os.IsNotExist(err) {
		t.Error("brief directory not created")
	}
}

func TestStartAlreadyRunning(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	ss := &mockSphereStore{
		ensured: map[string]bool{},
		updated: map[string]string{},
	}

	sessName := "sol-myworld-governor"
	mgr := &mockSessionManager{sessions: map[string]bool{sessName: true}}

	err := Start(StartOpts{
		World:      "myworld",
		SourceRepo: "/tmp/fake-repo",
	}, ss, mgr)
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

	ss := &mockSphereStore{
		ensured: map[string]bool{},
		updated: map[string]string{},
	}

	sessName := "sol-myworld-governor"
	mgr := &mockSessionManager{sessions: map[string]bool{sessName: true}}

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

	ss := &mockSphereStore{
		ensured: map[string]bool{},
		updated: map[string]string{},
	}

	mgr := &mockSessionManager{sessions: map[string]bool{}}

	err := Stop("myworld", ss, mgr)
	if err != nil {
		t.Fatalf("Stop should not error when session doesn't exist: %v", err)
	}

	// Verify agent state still updated to idle.
	if ss.updated["myworld/governor"] != "idle" {
		t.Errorf("agent state = %q, want \"idle\"", ss.updated["myworld/governor"])
	}
}

func TestSetupMirrorCorruptedDirectory(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// Create a non-git directory at the mirror path.
	mirrorPath := MirrorPath("testworld")
	if err := os.MkdirAll(mirrorPath, 0o755); err != nil {
		t.Fatal(err)
	}

	err := SetupMirror("testworld", "/tmp/fake-repo", "main")
	if err == nil {
		t.Fatal("expected error for non-git mirror directory")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("expected 'not a git repository' error, got: %v", err)
	}
}
