package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/gt/internal/store"
)

// setupTestEnv creates an isolated test environment with temp GT_HOME,
// a real git repo, and an isolated tmux server.
func setupTestEnv(t *testing.T) (gtHome string, sourceRepo string) {
	t.Helper()

	// 1. Create temp dir for GT_HOME.
	gtHome = t.TempDir()
	t.Setenv("GT_HOME", gtHome)

	// 2. Create .store and .runtime dirs.
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755)

	// 3. Create a temp git repo with one commit.
	sourceRepo = t.TempDir()
	gitRun(t, sourceRepo, "init")
	gitRun(t, sourceRepo, "config", "user.email", "test@test.com")
	gitRun(t, sourceRepo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(sourceRepo, "initial.txt"), []byte("init"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	gitRun(t, sourceRepo, "add", ".")
	gitRun(t, sourceRepo, "commit", "-m", "initial")

	// 4. Isolated tmux server.
	tmuxDir := t.TempDir()
	t.Setenv("TMUX_TMPDIR", tmuxDir)

	// 5. Cleanup tmux sessions on test end.
	t.Cleanup(func() {
		out, _ := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
		for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if strings.HasPrefix(name, "gt-") {
				exec.Command("tmux", "kill-session", "-t", name).Run()
			}
		}
	})

	return gtHome, sourceRepo
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), out, err)
	}
}

func openStores(t *testing.T, rig string) (*store.Store, *store.Store) {
	t.Helper()
	rigStore, err := store.OpenRig(rig)
	if err != nil {
		t.Fatalf("open rig store: %v", err)
	}
	t.Cleanup(func() { rigStore.Close() })

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("open town store: %v", err)
	}
	t.Cleanup(func() { townStore.Close() })

	return rigStore, townStore
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
