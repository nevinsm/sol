package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupRepo creates a bare origin and a cloned working repo with an initial commit.
func setupRepo(t *testing.T) (repoDir, bareDir string) {
	t.Helper()
	dir := t.TempDir()

	bareDir = filepath.Join(dir, "origin.git")
	runCmd(t, "git", "init", "--bare", bareDir)

	repoDir = filepath.Join(dir, "work")
	runCmd(t, "git", "clone", bareDir, repoDir)

	runCmd(t, "git", "-C", repoDir, "config", "user.email", "test@test.com")
	runCmd(t, "git", "-C", repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\n"), 0o644)
	runCmd(t, "git", "-C", repoDir, "add", ".")
	runCmd(t, "git", "-C", repoDir, "commit", "-m", "init")
	runCmd(t, "git", "-C", repoDir, "push", "origin", "main")

	return repoDir, bareDir
}

// createBranch creates a branch with a file change, pushes it, and returns to main.
func createBranch(t *testing.T, repoDir, branch, filename, content string) {
	t.Helper()
	runCmd(t, "git", "-C", repoDir, "checkout", "-b", branch)
	os.WriteFile(filepath.Join(repoDir, filename), []byte(content), 0o644)
	runCmd(t, "git", "-C", repoDir, "add", ".")
	runCmd(t, "git", "-C", repoDir, "commit", "-m", "changes on "+branch)
	runCmd(t, "git", "-C", repoDir, "push", "origin", branch)
	runCmd(t, "git", "-C", repoDir, "checkout", "main")
}

func runCmd(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %s: %v", name, strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out))
}

func TestSquashMerge(t *testing.T) {
	repoDir, _ := setupRepo(t)
	createBranch(t, repoDir, "feat/add-file", "hello.go", "package main\nfunc hello() {}\n")

	g := New(repoDir)

	// Get the original commit message.
	msg, err := g.GetBranchCommitMessage("feat/add-file")
	if err != nil {
		t.Fatalf("GetBranchCommitMessage: %v", err)
	}
	if !strings.Contains(msg, "changes on feat/add-file") {
		t.Errorf("unexpected message: %q", msg)
	}

	// Squash merge.
	if err := g.MergeSquash("feat/add-file", msg); err != nil {
		t.Fatalf("MergeSquash: %v", err)
	}

	// Verify file exists.
	data, err := os.ReadFile(filepath.Join(repoDir, "hello.go"))
	if err != nil {
		t.Fatalf("file not found after merge: %v", err)
	}
	if !strings.Contains(string(data), "func hello()") {
		t.Error("merged file has unexpected content")
	}

	// Verify commit message.
	out := runCmd(t, "git", "-C", repoDir, "log", "-1", "--format=%B")
	if !strings.Contains(out, "changes on feat/add-file") {
		t.Errorf("commit message not preserved: %q", out)
	}
}

func TestCheckConflictsClean(t *testing.T) {
	repoDir, _ := setupRepo(t)
	createBranch(t, repoDir, "feat/no-conflict", "new-file.go", "package main\n")

	g := New(repoDir)
	conflicts, err := g.CheckConflicts("feat/no-conflict", "main")
	if err != nil {
		t.Fatalf("CheckConflicts: %v", err)
	}
	if len(conflicts) != 0 {
		t.Errorf("expected no conflicts, got %v", conflicts)
	}
}

func TestCheckConflictsDetectsConflict(t *testing.T) {
	repoDir, _ := setupRepo(t)

	// Create two branches that modify the same file.
	runCmd(t, "git", "-C", repoDir, "checkout", "-b", "branch-a")
	os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\n// branch A\n"), 0o644)
	runCmd(t, "git", "-C", repoDir, "add", ".")
	runCmd(t, "git", "-C", repoDir, "commit", "-m", "branch A change")
	runCmd(t, "git", "-C", repoDir, "push", "origin", "branch-a")
	runCmd(t, "git", "-C", repoDir, "checkout", "main")

	// Merge branch-a into main first.
	runCmd(t, "git", "-C", repoDir, "merge", "branch-a")
	runCmd(t, "git", "-C", repoDir, "push", "origin", "main")

	// Create branch-b from old main that conflicts.
	runCmd(t, "git", "-C", repoDir, "checkout", "-b", "branch-b", "origin/main~1")
	os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\n// branch B\n"), 0o644)
	runCmd(t, "git", "-C", repoDir, "add", ".")
	runCmd(t, "git", "-C", repoDir, "commit", "-m", "branch B change")
	runCmd(t, "git", "-C", repoDir, "push", "origin", "branch-b")
	runCmd(t, "git", "-C", repoDir, "checkout", "main")

	g := New(repoDir)
	conflicts, err := g.CheckConflicts("branch-b", "main")
	if err != nil {
		t.Fatalf("CheckConflicts: %v", err)
	}
	if len(conflicts) == 0 {
		t.Error("expected conflicts, got none")
	}
	if conflicts[0] != "main.go" {
		t.Errorf("expected main.go in conflicts, got %v", conflicts)
	}
}

func TestBranchExists(t *testing.T) {
	repoDir, _ := setupRepo(t)
	createBranch(t, repoDir, "feat/exists", "file.go", "package main\n")

	g := New(repoDir)

	exists, err := g.BranchExists("feat/exists")
	if err != nil {
		t.Fatalf("BranchExists: %v", err)
	}
	if !exists {
		t.Error("expected branch to exist")
	}

	exists, err = g.BranchExists("feat/nonexistent")
	if err != nil {
		t.Fatalf("BranchExists: %v", err)
	}
	if exists {
		t.Error("expected branch to not exist")
	}
}

func TestBranchExistsRemoteOnly(t *testing.T) {
	repoDir, _ := setupRepo(t)
	createBranch(t, repoDir, "feat/remote-only", "remote.go", "package main\n")

	// Delete the local branch ref — only the remote tracking ref remains.
	runCmd(t, "git", "-C", repoDir, "branch", "-D", "feat/remote-only")

	g := New(repoDir)

	exists, err := g.BranchExists("origin/feat/remote-only")
	if err != nil {
		t.Fatalf("BranchExists remote-only: %v", err)
	}
	if !exists {
		t.Error("expected remote tracking branch to exist")
	}
}

func TestBranchExistsReturnsRemoteError(t *testing.T) {
	// Point Git at a non-existent directory. Both the local and remote
	// show-ref calls fail, but BranchExists should evaluate and return
	// the *remote* check error (err2), not the local one (err).
	g := New(t.TempDir()) // empty dir, not a git repo

	exists, err := g.BranchExists("anything")
	if exists {
		t.Error("expected false for non-git directory")
	}
	// Both errors are *GitError from run(), so isGitError matches and
	// the function returns false, nil. The fix ensures err2 (remote
	// check) is the one being evaluated, not err (local check).
	// With the old code checking err instead of err2, the wrong error
	// was being inspected — this test guards against that regression.
	if err != nil {
		t.Fatalf("expected nil error (GitError caught), got: %v", err)
	}
}

func TestFetchAndResetHard(t *testing.T) {
	repoDir, _ := setupRepo(t)

	g := New(repoDir)
	if err := g.Fetch("origin"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if err := g.ResetHard("origin/main"); err != nil {
		t.Fatalf("ResetHard: %v", err)
	}
}

func TestRevAndMergeBase(t *testing.T) {
	repoDir, _ := setupRepo(t)
	createBranch(t, repoDir, "feat/rev-test", "rev.go", "package main\n")

	g := New(repoDir)

	sha, err := g.Rev("HEAD")
	if err != nil {
		t.Fatalf("Rev: %v", err)
	}
	if len(sha) < 7 {
		t.Errorf("expected SHA, got %q", sha)
	}

	base, err := g.MergeBase("main", "feat/rev-test")
	if err != nil {
		t.Fatalf("MergeBase: %v", err)
	}
	if len(base) < 7 {
		t.Errorf("expected merge base SHA, got %q", base)
	}
}

func TestDiffNameOnly(t *testing.T) {
	repoDir, _ := setupRepo(t)
	createBranch(t, repoDir, "feat/diff-test", "diff-file.go", "package main\n")

	g := New(repoDir)
	files, err := g.DiffNameOnly("main", "feat/diff-test")
	if err != nil {
		t.Fatalf("DiffNameOnly: %v", err)
	}
	if len(files) != 1 || files[0] != "diff-file.go" {
		t.Errorf("expected [diff-file.go], got %v", files)
	}
}

func TestInitSubmodulesNoOp(t *testing.T) {
	repoDir, _ := setupRepo(t)

	// No .gitmodules — should be a no-op.
	if err := InitSubmodules(repoDir); err != nil {
		t.Fatalf("InitSubmodules should be no-op without .gitmodules: %v", err)
	}
}
