package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
)

// isolateTmux sets up tmux isolation for tests that create tmux sessions.
// Must be called before any tmux sessions are created. See setupTestEnv for
// the full explanation of why all three env vars are required.
func isolateTmux(t *testing.T) {
	t.Helper()
	tmuxDir := t.TempDir()
	t.Setenv("TMUX_TMPDIR", tmuxDir)
	t.Setenv("TMUX", "")
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	t.Cleanup(func() {
		// 5s timeout on the entire cleanup to prevent hangs.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Kill the entire isolated tmux server. This is more reliable than
		// per-session cleanup and only affects the isolated server when
		// TMUX_TMPDIR is set. tmuxDir is captured explicitly here so cleanup
		// works even if the test panicked and the env var was lost.
		// kill-server returns an error when no server is running — that's
		// expected when a test created no sessions, so only log on timeout.
		tmuxEnv := append(os.Environ(), "TMUX_TMPDIR="+tmuxDir, "TMUX=")
		killCmd := exec.CommandContext(ctx, "tmux", "kill-server")
		killCmd.Env = tmuxEnv
		if err := killCmd.Run(); err != nil && ctx.Err() != nil {
			t.Logf("isolateTmux: kill-server timed out: %v", err)
			return
		}

		// Verify the server is gone: list-sessions should fail or return empty.
		verifyCmd := exec.CommandContext(ctx, "tmux", "list-sessions", "-F", "#{session_name}")
		verifyCmd.Env = tmuxEnv
		if out, err := verifyCmd.Output(); err == nil {
			if remaining := strings.TrimSpace(string(out)); remaining != "" {
				t.Logf("isolateTmux: warning: sessions still alive after cleanup:\n%s", remaining)
			}
		}
	})
}

// setupTestEnv creates an isolated test environment with temp SOL_HOME,
// a real git repo, and an isolated tmux server.
//
// IMPORTANT — tmux isolation:
// All three of these env vars are required to prevent tests from interfering
// with real sol sessions. If you skip any of them, test cleanup will connect
// to the real tmux server and kill every live sol-* session:
//
//   TMUX_TMPDIR  → isolated socket directory (new tmux server)
//   TMUX=""      → unset inherited tmux var (forces socket-based discovery)
//   SOL_SESSION_COMMAND="sleep 300" → stub process instead of real claude
// writeTestToken writes a minimal api_key token to $SOL_HOME/.accounts/token.json
// so startup.Launch can inject credentials in tests (empty account handle).
func writeTestToken(t *testing.T, solHome string) {
	t.Helper()
	accountsDir := filepath.Join(solHome, ".accounts")
	if err := os.MkdirAll(accountsDir, 0o755); err != nil {
		t.Fatalf("create .accounts dir: %v", err)
	}
	tokenJSON := `{"type":"api_key","token":"test-key","created_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(accountsDir, "token.json"), []byte(tokenJSON), 0o600); err != nil {
		t.Fatalf("write test token: %v", err)
	}
}

func setupTestEnv(t *testing.T) (gtHome string, sourceRepo string) {
	t.Helper()

	// 1. Create temp dir for SOL_HOME.
	gtHome = t.TempDir()
	t.Setenv("SOL_HOME", gtHome)

	// 2. Create .store and .runtime dirs.
	if err := os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755); err != nil {
		t.Fatalf("create .runtime dir: %v", err)
	}

	// 3. Write a fake token so startup.Launch can inject credentials.
	writeTestToken(t, gtHome)

	// 4. Create a temp git repo with one commit.
	sourceRepo = t.TempDir()
	gitRun(t, sourceRepo, "init")
	gitRun(t, sourceRepo, "config", "user.email", "test@test.com")
	gitRun(t, sourceRepo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(sourceRepo, "initial.txt"), []byte("init"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	gitRun(t, sourceRepo, "add", ".")
	gitRun(t, sourceRepo, "commit", "-m", "initial")

	// 5. Isolated tmux server + stub session command + cleanup.
	isolateTmux(t)

	return gtHome, sourceRepo
}

// registerAgentRole registers the "outpost" role in the startup registry so
// prefect/sentinel can respawn sessions via startup.Respawn. Without this,
// respawn fails because "outpost" has no registered startup config in tests
// (the registration normally happens in cmd/cast.go at init time).
func registerAgentRole(t *testing.T) {
	t.Helper()
	startup.Register("outpost", startup.RoleConfig{
		WorktreeDir: func(world, agent string) string {
			return config.WorktreePath(world, agent)
		},
	})
	t.Cleanup(func() { startup.Register("outpost", startup.RoleConfig{}) })
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), out, err)
	}
}

// initWorld initializes a world via CLI so world-scoped commands pass the hard gate.
func initWorld(t *testing.T, gtHome, world string) {
	t.Helper()
	out, err := runGT(t, gtHome, "world", "init", world)
	if err != nil {
		t.Fatalf("world init %s failed: %v: %s", world, err, out)
	}
}

// initWorldWithRepo initializes a world with a real source repo path.
// Use this when the test will call cast or other commands that read source_repo from config.
func initWorldWithRepo(t *testing.T, gtHome, world, sourceRepo string) {
	t.Helper()
	out, err := runGT(t, gtHome, "world", "init", world, "--source-repo="+sourceRepo)
	if err != nil {
		t.Fatalf("world init %s failed: %v: %s", world, err, out)
	}
}

func openStores(t *testing.T, world string) (*store.WorldStore, *store.SphereStore) {
	t.Helper()
	worldStore, err := store.OpenWorld(world)
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	t.Cleanup(func() { worldStore.Close() })

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	t.Cleanup(func() { sphereStore.Close() })

	return worldStore, sphereStore
}

// createSourceRepo creates a bare git repo and a clone with an initial commit.
// Returns paths to the bare repo (origin) and the working clone.
func createSourceRepo(t *testing.T, gtHome string) (bareRepo, workingClone string) {
	t.Helper()

	bareRepo = filepath.Join(gtHome, ".test-origin.git")
	workingClone = filepath.Join(gtHome, ".test-clone")

	// 1. Create bare repo.
	cmd := exec.Command("git", "init", "--bare", bareRepo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %s: %v", out, err)
	}

	// 2. Clone it.
	cmd = exec.Command("git", "clone", bareRepo, workingClone)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone: %s: %v", out, err)
	}

	// 3. Configure git user in clone.
	gitRun(t, workingClone, "config", "user.email", "test@test.com")
	gitRun(t, workingClone, "config", "user.name", "Test")

	// 4. Create initial commit and push.
	// .gitignore excludes .claude/ to prevent CLAUDE.md conflicts between branches.
	if err := os.WriteFile(filepath.Join(workingClone, ".gitignore"), []byte(".claude/\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workingClone, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	gitRun(t, workingClone, "add", ".")
	gitRun(t, workingClone, "commit", "-m", "initial commit")
	gitRun(t, workingClone, "push", "origin", "main")

	return bareRepo, workingClone
}


// pollUntil polls fn every interval until it returns true or timeout elapses.
func pollUntil(timeout, interval time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(interval)
	}
	return false
}

// collectEvents reads all events from the event feed file and returns
// them, optionally filtered by type.
func collectEvents(t *testing.T, gtHome, eventType string) []events.Event {
	t.Helper()
	path := filepath.Join(gtHome, ".events.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("open events file: %v", err)
	}
	defer f.Close()

	var result []events.Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var ev events.Event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if eventType != "" && ev.Type != eventType {
			continue
		}
		result = append(result, ev)
	}
	return result
}

// assertEventEmitted verifies that at least one event of the given type
// exists in the feed.
func assertEventEmitted(t *testing.T, gtHome, eventType string) {
	t.Helper()
	evts := collectEvents(t, gtHome, eventType)
	if len(evts) == 0 {
		t.Errorf("expected at least one %q event in feed, found none", eventType)
	}
}

// mockSessionChecker implements sentinel.SessionChecker for integration tests.
type mockSessionChecker struct {
	mu       sync.Mutex
	alive    map[string]bool
	captures map[string]string
	started  []string
	stopped  []string
	injected []mockInjectCall
}

type mockInjectCall struct {
	Session string
	Text    string
}

func newMockSessionChecker() *mockSessionChecker {
	return &mockSessionChecker{
		alive:    make(map[string]bool),
		captures: make(map[string]string),
	}
}

func (m *mockSessionChecker) Exists(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.alive[name]
}

func (m *mockSessionChecker) Capture(name string, lines int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if output, ok := m.captures[name]; ok {
		return output, nil
	}
	return "", nil
}

func (m *mockSessionChecker) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alive[name] = true
	m.started = append(m.started, name)
	return nil
}

func (m *mockSessionChecker) Stop(name string, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.alive, name)
	m.stopped = append(m.stopped, name)
	return nil
}

func (m *mockSessionChecker) Cycle(name, workdir, cmd string, env map[string]string, role, world string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil
}

func (m *mockSessionChecker) Inject(name string, text string, submit bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.injected = append(m.injected, mockInjectCall{Session: name, Text: text})
	return nil
}

func (m *mockSessionChecker) NudgeSession(name string, message string) error {
	return nil
}

func (m *mockSessionChecker) WaitForIdle(name string, timeout time.Duration) error {
	return nil
}

func (m *mockSessionChecker) CountSessions(prefix string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for name := range m.alive {
		if strings.HasPrefix(name, prefix) {
			count++
		}
	}
	return count, nil
}

func (m *mockSessionChecker) List() ([]session.SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var infos []session.SessionInfo
	for name, alive := range m.alive {
		infos = append(infos, session.SessionInfo{Name: name, Alive: alive})
	}
	return infos, nil
}

// --- Arc 3.5 helpers ---

// runGit runs a git command in the specified directory.
// If dir is empty, the command runs in the default working directory.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed in %s: %s: %v", args, dir, out, err)
	}
}

// runGitOutput runs a git command and returns its output.
func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed in %s: %s: %v", args, dir, out, err)
	}
	return string(out)
}

// writeTestFile writes content to a file, creating parent directories as needed.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// extractWritID extracts a sol-xxxx writ ID from command output.
func extractWritID(t *testing.T, output string) string {
	t.Helper()
	id := strings.TrimSpace(output)
	if strings.HasPrefix(id, "sol-") {
		return id
	}
	for _, word := range strings.Fields(output) {
		if strings.HasPrefix(word, "sol-") {
			return strings.TrimSuffix(word, ":")
		}
	}
	t.Fatalf("could not extract writ ID from: %s", output)
	return ""
}

// setupTestEnvWithRepo creates an isolated test environment with a temp SOL_HOME,
// a real git repo, and an isolated tmux server. Similar to setupTestEnv but uses
// the runGit helper with proper git env vars.
func setupTestEnvWithRepo(t *testing.T) (gtHome string, sourceRepo string) {
	t.Helper()

	gtHome = t.TempDir()
	t.Setenv("SOL_HOME", gtHome)

	if err := os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755); err != nil {
		t.Fatalf("create .runtime dir: %v", err)
	}

	// Write a fake token so startup.Launch can inject credentials.
	writeTestToken(t, gtHome)

	// Create a temp git repo with one commit.
	sourceRepo = t.TempDir()
	runGit(t, sourceRepo, "init")
	writeTestFile(t, filepath.Join(sourceRepo, "README.md"), "hello")
	runGit(t, sourceRepo, "add", ".")
	runGit(t, sourceRepo, "commit", "-m", "initial")

	// Isolated tmux server + stub session command + cleanup.
	isolateTmux(t)

	return gtHome, sourceRepo
}

// setupWorld initializes a world with a source repo via CLI.
func setupWorld(t *testing.T, gtHome, world, sourceRepo string) {
	t.Helper()
	out, err := runGT(t, gtHome, "world", "init", world, "--source-repo="+sourceRepo)
	if err != nil {
		t.Fatalf("world init %s failed: %v: %s", world, err, out)
	}
}
