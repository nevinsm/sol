package git

import (
	"errors"
	"fmt"
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

func TestVerifyCleanWorktreeClean(t *testing.T) {
	repoDir, _ := setupRepo(t)
	g := New(repoDir)

	if err := g.VerifyCleanWorktree(); err != nil {
		t.Fatalf("expected clean worktree, got error: %v", err)
	}
}

func TestVerifyCleanWorktreeDirty(t *testing.T) {
	repoDir, _ := setupRepo(t)
	g := New(repoDir)

	// Create an untracked file to dirty the worktree.
	os.WriteFile(filepath.Join(repoDir, "dirty.txt"), []byte("dirty"), 0o644)

	err := g.VerifyCleanWorktree()
	if err == nil {
		t.Fatal("expected error for dirty worktree")
	}
	if !strings.Contains(err.Error(), "worktree is dirty") {
		t.Errorf("error = %q, should contain 'worktree is dirty'", err.Error())
	}
}

func TestVerifyCleanWorktreeStagedChanges(t *testing.T) {
	repoDir, _ := setupRepo(t)
	g := New(repoDir)

	// Modify a tracked file and stage it.
	os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\n// modified\n"), 0o644)
	runCmd(t, "git", "-C", repoDir, "add", "main.go")

	err := g.VerifyCleanWorktree()
	if err == nil {
		t.Fatal("expected error for staged changes")
	}
	if !strings.Contains(err.Error(), "worktree is dirty") {
		t.Errorf("error = %q, should contain 'worktree is dirty'", err.Error())
	}
}

func TestCheckConflictsDirtyWorktreeGuard(t *testing.T) {
	repoDir, _ := setupRepo(t)
	g := New(repoDir)

	// Create a branch for the source.
	createBranch(t, repoDir, "feat/test-dirty", "new.go", "package main\n")

	// Dirty the worktree.
	os.WriteFile(filepath.Join(repoDir, "dirty.txt"), []byte("dirty"), 0o644)

	_, err := g.CheckConflicts("feat/test-dirty", "main")
	if err == nil {
		t.Fatal("expected error for dirty worktree")
	}
	if !strings.Contains(err.Error(), "clean worktree") {
		t.Errorf("error = %q, should contain 'clean worktree'", err.Error())
	}
}

// TestCheckConflictsUnclassifiableMerge injects a fake runner that fails the
// `git diff --name-only --diff-filter=U` call after a failed test merge,
// asserting that CheckConflicts returns *UnclassifiableMergeError instead of
// silently discarding the classification error and reporting the underlying
// merge failure as a clean non-conflict failure.
func TestCheckConflictsUnclassifiableMerge(t *testing.T) {
	mergeErr := errors.New("simulated merge failure")
	classifyErr := errors.New("simulated diff failure (EBUSY on .git/MERGE_HEAD)")

	g := New("/fake/workdir")
	g.runOverride = func(args ...string) (string, error) {
		if len(args) == 0 {
			return "", fmt.Errorf("empty args")
		}
		switch {
		case args[0] == "status" && containsArg(args, "--porcelain"):
			// VerifyCleanWorktree — clean.
			return "", nil
		case args[0] == "symbolic-ref":
			return "refs/heads/main", nil
		case args[0] == "checkout":
			// Target checkout AND deferred restore.
			return "", nil
		case args[0] == "merge" && containsArg(args, "--no-commit"):
			// The test merge — fail to trigger the classification path.
			return "", mergeErr
		case args[0] == "diff" && containsArg(args, "--diff-filter=U"):
			// Conflict classification — fail. This is the failure mode the
			// fix targets.
			return "", classifyErr
		case args[0] == "merge" && containsArg(args, "--abort"):
			// AbortMerge cleanup — succeed so we hit the unclassifiable
			// return path (not the wrapped abort-also-failed branch).
			return "", nil
		}
		return "", fmt.Errorf("unexpected git invocation in test: %v", args)
	}

	conflicts, err := g.CheckConflicts("feat/x", "main")
	if conflicts != nil {
		t.Errorf("expected nil conflicts, got %v", conflicts)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var unclassifiable *UnclassifiableMergeError
	if !errors.As(err, &unclassifiable) {
		t.Fatalf("expected *UnclassifiableMergeError, got %T: %v", err, err)
	}
	if !errors.Is(unclassifiable.MergeErr, mergeErr) {
		t.Errorf("MergeErr = %v, want %v", unclassifiable.MergeErr, mergeErr)
	}
	if !errors.Is(unclassifiable.ClassifyErr, classifyErr) {
		t.Errorf("ClassifyErr = %v, want %v", unclassifiable.ClassifyErr, classifyErr)
	}
	// Unwrap returns MergeErr, so errors.Is against the original merge error
	// continues to match — preserves backward-compatible "is this a merge
	// failure?" checks.
	if !errors.Is(err, mergeErr) {
		t.Errorf("errors.Is(err, mergeErr) = false, want true (Unwrap chain)")
	}
}

// TestCheckConflictsUnclassifiableMergeAbortAlsoFails covers the secondary
// path where both classification AND merge --abort fail. The returned error
// must still expose the unclassifiable type via errors.As so callers can
// route to manual intervention.
func TestCheckConflictsUnclassifiableMergeAbortAlsoFails(t *testing.T) {
	mergeErr := errors.New("simulated merge failure")
	classifyErr := errors.New("simulated diff failure")
	abortErr := errors.New("simulated abort failure")

	g := New("/fake/workdir")
	g.runOverride = func(args ...string) (string, error) {
		switch {
		case args[0] == "status" && containsArg(args, "--porcelain"):
			return "", nil
		case args[0] == "symbolic-ref":
			return "refs/heads/main", nil
		case args[0] == "checkout":
			return "", nil
		case args[0] == "merge" && containsArg(args, "--no-commit"):
			return "", mergeErr
		case args[0] == "diff" && containsArg(args, "--diff-filter=U"):
			return "", classifyErr
		case args[0] == "merge" && containsArg(args, "--abort"):
			return "", abortErr
		}
		return "", fmt.Errorf("unexpected git invocation in test: %v", args)
	}

	_, err := g.CheckConflicts("feat/x", "main")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var unclassifiable *UnclassifiableMergeError
	if !errors.As(err, &unclassifiable) {
		t.Fatalf("expected wrapped *UnclassifiableMergeError, got %T: %v", err, err)
	}
	if !errors.Is(err, abortErr) {
		t.Errorf("errors.Is(err, abortErr) = false, want true")
	}
}

// containsArg is a small helper for the runOverride switch above.
func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestGitErrorErrorIncludesStdoutAndStderr(t *testing.T) {
	tests := []struct {
		name        string
		err         *GitError
		mustContain []string
		mustNot     []string
	}{
		{
			name: "stderr only",
			err: &GitError{
				Command: "merge",
				Stderr:  "CONFLICT (content): Merge conflict",
			},
			mustContain: []string{"git merge", "stderr=CONFLICT"},
			mustNot:     []string{"stdout="},
		},
		{
			name: "stdout only",
			err: &GitError{
				Command: "push",
				Stdout:  "everything up-to-date",
			},
			mustContain: []string{"git push", "stdout=everything up-to-date"},
			mustNot:     []string{"stderr="},
		},
		{
			name: "both stdout and stderr",
			err: &GitError{
				Command: "push",
				Stdout:  "Counting objects",
				Stderr:  "remote rejected",
			},
			mustContain: []string{"git push", "stderr=remote rejected", "stdout=Counting objects"},
		},
		{
			name: "neither — falls back to underlying error",
			err: &GitError{
				Command: "fetch",
				Err:     errors.New("exit status 128"),
			},
			mustContain: []string{"git fetch", "exit status 128"},
			mustNot:     []string{"stdout=", "stderr="},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			for _, sub := range tt.mustContain {
				if !strings.Contains(got, sub) {
					t.Errorf("error string %q missing substring %q", got, sub)
				}
			}
			for _, sub := range tt.mustNot {
				if strings.Contains(got, sub) {
					t.Errorf("error string %q should not contain %q", got, sub)
				}
			}
		})
	}
}

func TestInitSubmodulesNoOp(t *testing.T) {
	repoDir, _ := setupRepo(t)

	// No .gitmodules — should be a no-op.
	if err := InitSubmodules(repoDir); err != nil {
		t.Fatalf("InitSubmodules should be no-op without .gitmodules: %v", err)
	}
}
