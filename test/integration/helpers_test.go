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

// createBranchWithFile creates a new branch in the repo with a file change,
// pushes it to origin, and returns to the original branch.
func createBranchWithFile(t *testing.T, repoDir, branch, filename, content string) {
	t.Helper()

	// Get current branch to return to.
	cmd := exec.Command("git", "-C", repoDir, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("get current branch: %v", err)
	}
	origBranch := strings.TrimSpace(string(out))

	gitRun(t, repoDir, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(repoDir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
	gitRun(t, repoDir, "add", ".")
	gitRun(t, repoDir, "commit", "-m", "add "+filename)
	gitRun(t, repoDir, "push", "origin", branch)
	gitRun(t, repoDir, "checkout", origBranch)
}

// waitForMergePhase polls the store until a MR reaches the expected phase.
func waitForMergePhase(t *testing.T, rigStore *store.Store, mrID, expectedPhase string, timeout time.Duration) {
	t.Helper()
	ok := pollUntil(timeout, 500*time.Millisecond, func() bool {
		mr, err := rigStore.GetMergeRequest(mrID)
		return err == nil && mr != nil && mr.Phase == expectedPhase
	})
	if !ok {
		mr, err := rigStore.GetMergeRequest(mrID)
		phase := "unknown"
		if err == nil && mr != nil {
			phase = mr.Phase
		}
		t.Fatalf("MR %s did not reach phase %q within %v (current phase: %s)", mrID, expectedPhase, timeout, phase)
	}
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
