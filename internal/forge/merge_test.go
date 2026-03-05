package forge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// setupForgeWorktree creates a forge worktree from the source repo on a
// dedicated branch tracking main. Returns the worktree path.
func setupForgeWorktree(t *testing.T, sourceRepo, wtPath string) string {
	t.Helper()
	run(t, "git", "-C", sourceRepo, "worktree", "add", "-b", "forge/test", wtPath, "HEAD")
	run(t, "git", "-C", wtPath, "config", "user.email", "test@test.com")
	run(t, "git", "-C", wtPath, "config", "user.name", "Test")
	return wtPath
}

func TestMergeHappyPath(t *testing.T) {
	sourceRepo, wtPath := setupGitTest(t)
	createBranchWithChanges(t, sourceRepo, "outpost/test/feat-1", "feature.go", "package main\nfunc Feature() {}\n")

	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	setupForgeWorktree(t, sourceRepo, wtPath)

	worldStore := newMockWorldStore()
	sphereStore := newMockSphereStore()

	r := &Forge{
		world:       "test",
		agentID:     "test/forge",
		sourceRepo:  sourceRepo,
		worktree:    wtPath,
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
		cfg: Config{
			TargetBranch: "main",
			QualityGates: []string{"true"}, // always-pass gate
			GateTimeout:  30 * time.Second,
		},
	}

	// Create a merge slot directory for dispatch.AcquireMergeSlotLock.
	mergeSlotDir := filepath.Join(dir, "test", "forge")
	os.MkdirAll(mergeSlotDir, 0o755)

	mr := &store.MergeRequest{
		ID:         "mr-001",
		WorkItemID: "sol-001",
		Branch:     "outpost/test/feat-1",
	}

	result := r.Merge(context.Background(), mr)

	if !result.Success {
		t.Fatalf("expected success, got: %+v", result)
	}
	if result.MergeCommit == "" {
		t.Error("expected merge commit SHA")
	}
	if result.Conflict {
		t.Error("did not expect conflict")
	}

	// Verify the file was merged.
	data, err := os.ReadFile(filepath.Join(wtPath, "feature.go"))
	if err != nil {
		t.Fatalf("merged file not found: %v", err)
	}
	if !strings.Contains(string(data), "func Feature()") {
		t.Error("merged file has wrong content")
	}
}

func TestMergeConflictDetection(t *testing.T) {
	sourceRepo, wtPath := setupGitTest(t)

	// Create a conflicting situation: modify main.go on a branch,
	// then also modify it on main.
	run(t, "git", "-C", sourceRepo, "checkout", "-b", "outpost/test/conflict")
	os.WriteFile(filepath.Join(sourceRepo, "main.go"), []byte("package main\n// conflict branch\n"), 0o644)
	run(t, "git", "-C", sourceRepo, "add", ".")
	run(t, "git", "-C", sourceRepo, "commit", "-m", "conflict branch")
	run(t, "git", "-C", sourceRepo, "push", "origin", "outpost/test/conflict")
	run(t, "git", "-C", sourceRepo, "checkout", "main")

	// Now modify main.go on main too.
	os.WriteFile(filepath.Join(sourceRepo, "main.go"), []byte("package main\n// main branch\n"), 0o644)
	run(t, "git", "-C", sourceRepo, "add", ".")
	run(t, "git", "-C", sourceRepo, "commit", "-m", "main change")
	run(t, "git", "-C", sourceRepo, "push", "origin", "main")

	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	setupForgeWorktree(t, sourceRepo, wtPath)

	r := &Forge{
		world:       "test",
		agentID:     "test/forge",
		sourceRepo:  sourceRepo,
		worktree:    wtPath,
		worldStore:  newMockWorldStore(),
		sphereStore: newMockSphereStore(),
		logger:      testLogger(),
		cfg: Config{
			TargetBranch: "main",
			QualityGates: []string{"true"},
			GateTimeout:  30 * time.Second,
		},
	}

	mr := &store.MergeRequest{
		ID:         "mr-002",
		WorkItemID: "sol-002",
		Branch:     "outpost/test/conflict",
	}

	result := r.Merge(context.Background(), mr)

	if result.Success {
		t.Error("expected failure due to conflict")
	}
	if !result.Conflict {
		t.Errorf("expected conflict=true, got: %+v", result)
	}
	if len(result.ConflictFiles) == 0 {
		t.Error("expected conflict files")
	}

	// Verify worktree was reset (no dirty state).
	cmd := exec.Command("git", "-C", wtPath, "status", "--porcelain")
	out, _ := cmd.CombinedOutput()
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("worktree should be clean after conflict, got: %s", string(out))
	}
}

func TestMergeGateFailureResetsWorktree(t *testing.T) {
	sourceRepo, wtPath := setupGitTest(t)
	createBranchWithChanges(t, sourceRepo, "outpost/test/gate-fail", "gated.go", "package main\n")

	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	setupForgeWorktree(t, sourceRepo, wtPath)

	r := &Forge{
		world:       "test",
		agentID:     "test/forge",
		sourceRepo:  sourceRepo,
		worktree:    wtPath,
		worldStore:  newMockWorldStore(),
		sphereStore: newMockSphereStore(),
		logger:      testLogger(),
		cfg: Config{
			TargetBranch: "main",
			QualityGates: []string{"false"}, // always-fail gate
			GateTimeout:  30 * time.Second,
		},
	}

	mr := &store.MergeRequest{
		ID:         "mr-003",
		WorkItemID: "sol-003",
		Branch:     "outpost/test/gate-fail",
	}

	result := r.Merge(context.Background(), mr)

	if result.Success {
		t.Error("expected failure due to gate")
	}
	if !result.GatesFailed {
		t.Errorf("expected gates_failed=true, got: %+v", result)
	}
	if len(result.GateResults) == 0 {
		t.Error("expected gate results")
	}

	// Verify worktree was reset — the merged file should not exist.
	if _, err := os.Stat(filepath.Join(wtPath, "gated.go")); err == nil {
		t.Error("merged file should not exist after gate failure reset")
	}
}

func TestMergePostPushVerificationFailure(t *testing.T) {
	sourceRepo, wtPath := setupGitTest(t)
	createBranchWithChanges(t, sourceRepo, "outpost/test/verify-fail", "verify.go", "package main\n")

	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	setupForgeWorktree(t, sourceRepo, wtPath)

	// Create merge slot directory.
	mergeSlotDir := filepath.Join(dir, "test", "forge")
	os.MkdirAll(mergeSlotDir, 0o755)

	// Install a post-receive hook that resets main to the previous commit,
	// simulating a push that "succeeded" but the ref was reverted by the remote.
	originURL := run(t, "git", "-C", wtPath, "remote", "get-url", "origin")
	hookDir := filepath.Join(originURL, "hooks")
	os.MkdirAll(hookDir, 0o755)
	hookScript := `#!/bin/sh
# Revert main to its parent after receiving the push, simulating a silent failure.
while read oldrev newrev refname; do
  if [ "$refname" = "refs/heads/main" ]; then
    git update-ref refs/heads/main "$oldrev"
  fi
done
`
	os.WriteFile(filepath.Join(hookDir, "post-receive"), []byte(hookScript), 0o755)

	r := &Forge{
		world:       "test",
		agentID:     "test/forge",
		sourceRepo:  sourceRepo,
		worktree:    wtPath,
		worldStore:  newMockWorldStore(),
		sphereStore: newMockSphereStore(),
		logger:      testLogger(),
		cfg: Config{
			TargetBranch: "main",
			QualityGates: []string{"true"},
			GateTimeout:  30 * time.Second,
		},
	}

	mr := &store.MergeRequest{
		ID:         "mr-005",
		WorkItemID: "sol-005",
		Branch:     "outpost/test/verify-fail",
	}

	result := r.Merge(context.Background(), mr)

	if result.Success {
		t.Error("expected failure due to post-push verification")
	}
	if !result.PushRejected {
		t.Errorf("expected push_rejected=true, got: %+v", result)
	}
	if !strings.Contains(result.Error, "post-push verification failed") {
		t.Errorf("expected post-push verification error, got: %s", result.Error)
	}
}

func TestMergePushRejection(t *testing.T) {
	sourceRepo, wtPath := setupGitTest(t)
	createBranchWithChanges(t, sourceRepo, "outpost/test/push-reject", "pushed.go", "package main\n")

	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	setupForgeWorktree(t, sourceRepo, wtPath)

	// Create merge slot directory.
	mergeSlotDir := filepath.Join(dir, "test", "forge")
	os.MkdirAll(mergeSlotDir, 0o755)

	// Install a pre-receive hook on the bare repo that rejects pushes.
	originURL := run(t, "git", "-C", wtPath, "remote", "get-url", "origin")
	hookDir := filepath.Join(originURL, "hooks")
	os.MkdirAll(hookDir, 0o755)
	os.WriteFile(filepath.Join(hookDir, "pre-receive"),
		[]byte("#!/bin/sh\necho 'push rejected by test hook'\nexit 1\n"), 0o755)

	r := &Forge{
		world:       "test",
		agentID:     "test/forge",
		sourceRepo:  sourceRepo,
		worktree:    wtPath,
		worldStore:  newMockWorldStore(),
		sphereStore: newMockSphereStore(),
		logger:      testLogger(),
		cfg: Config{
			TargetBranch: "main",
			QualityGates: []string{"true"},
			GateTimeout:  30 * time.Second,
		},
	}

	mr := &store.MergeRequest{
		ID:         "mr-004",
		WorkItemID: "sol-004",
		Branch:     "outpost/test/push-reject",
	}

	result := r.Merge(context.Background(), mr)

	if result.Success {
		t.Error("expected push rejection")
	}
	if !result.PushRejected {
		t.Errorf("expected push_rejected=true, got: %+v", result)
	}
}
