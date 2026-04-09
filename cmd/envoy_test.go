package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/envoy"
	"github.com/nevinsm/sol/internal/store"
)

// isolateCmdTmux sets up tmux isolation for cmd tests that create tmux sessions.
// Mirrors the isolation from test/integration/helpers_test.go:
//   TMUX_TMPDIR — isolated socket directory (new tmux server)
//   TMUX=""     — unset inherited tmux var (forces socket-based discovery)
//   SOL_SESSION_COMMAND="sleep 300" — stub process instead of real claude
func isolateCmdTmux(t *testing.T) {
	t.Helper()
	tmuxDir := t.TempDir()
	t.Setenv("TMUX_TMPDIR", tmuxDir)
	t.Setenv("TMUX", "")
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	t.Cleanup(func() {
		out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
		if err != nil {
			return
		}
		for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if strings.HasPrefix(name, "sol-") {
				exec.Command("tmux", "kill-session", "-t", name).Run()
			}
		}
	})
}

// initTestWorld sets up SOL_HOME with sphere.db and a world directory.
func initTestWorld(t *testing.T, world string) string {
	t.Helper()
	solHome := filepath.Join(t.TempDir(), "sol-test")
	t.Setenv("SOL_HOME", solHome)

	// Create world directory, world.toml, and .store directory.
	worldDir := filepath.Join(solHome, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	worldToml := filepath.Join(worldDir, "world.toml")
	if err := os.WriteFile(worldToml, []byte("[world]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Join(solHome, ".store")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a fake token so startup.Launch can inject credentials.
	accountsDir := filepath.Join(solHome, ".accounts")
	if err := os.MkdirAll(accountsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tokenJSON := `{"type":"api_key","token":"test-key","created_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(accountsDir, "token.json"), []byte(tokenJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create sphere.db with schema.
	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	ss.Close()

	return solHome
}

// initGitRepo creates a temporary git repo with an initial commit.
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

// runGitCmd runs a git command with the given args and fails the test on error.
func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), out, err)
	}
}

func TestEnvoyCreateCommand(t *testing.T) {
	solHome := initTestWorld(t, "myworld")

	// Create a source repo.
	sourceRepo := filepath.Join(t.TempDir(), "repo")
	initGitRepo(t, sourceRepo)

	// Create managed repo clone.
	repoPath := config.RepoPath("myworld")
	runGitCmd(t, sourceRepo, "clone", sourceRepo, repoPath)

	// Reset flags.
	envoyCreateWorld = "myworld"
	defer func() {
		envoyCreateWorld = ""
	}()

	rootCmd.SetArgs([]string{"envoy", "create", "scout", "--world=myworld"})

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("envoy create failed: %v", err)
	}

	output := captured.String()
	if !strings.Contains(output, "Created envoy") {
		t.Errorf("expected 'Created envoy' in output, got: %s", output)
	}

	// Verify envoy directory created.
	envoyDir := envoy.EnvoyDir("myworld", "scout")
	if _, err := os.Stat(envoyDir); os.IsNotExist(err) {
		t.Error("envoy directory not created")
	}

	// Verify worktree created.
	worktree := envoy.WorktreePath("myworld", "scout")
	if _, err := os.Stat(worktree); os.IsNotExist(err) {
		t.Error("worktree not created")
	}

	// Verify agent record in sphere store.
	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()

	agent, err := ss.GetAgent("myworld/scout")
	if err != nil {
		t.Fatalf("agent not found in sphere store: %v", err)
	}
	if agent.Role != "envoy" {
		t.Errorf("agent role = %q, want \"envoy\"", agent.Role)
	}

	_ = solHome
}

func TestEnvoyListCommand(t *testing.T) {
	initTestWorld(t, "myworld")

	// Create agents directly in sphere store.
	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	ss.CreateAgent("scout", "myworld", "envoy")
	ss.CreateAgent("ranger", "myworld", "envoy")
	ss.CreateAgent("worker", "myworld", "outpost")
	ss.Close()

	envoyListWorld = "myworld"
	envoyListJSON = false
	defer func() {
		envoyListWorld = ""
		envoyListJSON = false
	}()

	rootCmd.SetArgs([]string{"envoy", "list", "--world=myworld"})

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("envoy list failed: %v", err)
	}

	output := captured.String()
	if !strings.Contains(output, "scout") {
		t.Errorf("expected 'scout' in list output, got: %s", output)
	}
	if !strings.Contains(output, "ranger") {
		t.Errorf("expected 'ranger' in list output, got: %s", output)
	}
	// worker is role=outpost, should NOT appear.
	if strings.Contains(output, "worker") {
		t.Errorf("did not expect 'worker' (role=outpost) in envoy list, got: %s", output)
	}
}

