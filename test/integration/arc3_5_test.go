package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// =============================================================================
// Arc 3.5 — Managed World Repository Integration Tests
// =============================================================================

func TestInitWithRemoteURL(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	// Create a bare repo (simulates a remote URL).
	remoteDir := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, "", "init", "--bare", remoteDir)

	// Push an initial commit via a temp clone.
	tmpClone := t.TempDir()
	runGit(t, tmpClone, "clone", remoteDir, ".")
	writeTestFile(t, filepath.Join(tmpClone, "README.md"), "hello")
	runGit(t, tmpClone, "add", ".")
	runGit(t, tmpClone, "commit", "-m", "init")
	runGit(t, tmpClone, "push", "origin", "main")

	// Init with "URL" (bare repo path).
	out, err := runGT(t, solHome, "init", "--name=myworld",
		"--source-repo="+remoteDir, "--skip-checks")
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	// Verify managed clone exists.
	repoPath := filepath.Join(solHome, "myworld", "repo")
	if _, err := os.Stat(repoPath); err != nil {
		t.Fatalf("managed repo not created: %v", err)
	}

	// Verify it's a valid git repo with the commit.
	logOut := runGitOutput(t, repoPath, "log", "--oneline")
	if !strings.Contains(logOut, "init") {
		t.Error("managed repo missing initial commit")
	}

	// Verify origin points to remote.
	originOut := runGitOutput(t, repoPath, "remote", "get-url", "origin")
	if strings.TrimSpace(originOut) != remoteDir {
		t.Errorf("origin = %q, want %q", strings.TrimSpace(originOut), remoteDir)
	}
}

func TestInitWithLocalPathAdoptsUpstream(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	// Create remote → local clone chain.
	remoteDir := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, "", "init", "--bare", remoteDir)

	localDir := t.TempDir()
	runGit(t, localDir, "clone", remoteDir, ".")
	writeTestFile(t, filepath.Join(localDir, "README.md"), "hello")
	runGit(t, localDir, "add", ".")
	runGit(t, localDir, "commit", "-m", "init")
	runGit(t, localDir, "push", "origin", "main")

	// Init with local path.
	out, err := runGT(t, solHome, "init", "--name=myworld",
		"--source-repo="+localDir, "--skip-checks")
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	// Verify managed clone's origin is the REMOTE, not the local path.
	repoPath := filepath.Join(solHome, "myworld", "repo")
	originOut := runGitOutput(t, repoPath, "remote", "get-url", "origin")
	origin := strings.TrimSpace(originOut)

	if origin == localDir {
		t.Errorf("origin should be upstream (%s), got local path (%s)", remoteDir, localDir)
	}
	if origin != remoteDir {
		t.Errorf("origin = %q, want %q", origin, remoteDir)
	}
}

func TestWorldSyncFetchesLatest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	// Create remote with initial commit.
	remoteDir := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, "", "init", "--bare", remoteDir)

	tmpClone := t.TempDir()
	runGit(t, tmpClone, "clone", remoteDir, ".")
	writeTestFile(t, filepath.Join(tmpClone, "file1.txt"), "v1")
	runGit(t, tmpClone, "add", ".")
	runGit(t, tmpClone, "commit", "-m", "first")
	runGit(t, tmpClone, "push", "origin", "main")

	// Init world.
	out, err := runGT(t, solHome, "init", "--name=myworld",
		"--source-repo="+remoteDir, "--skip-checks")
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	// Push a new commit to remote.
	writeTestFile(t, filepath.Join(tmpClone, "file2.txt"), "v2")
	runGit(t, tmpClone, "add", ".")
	runGit(t, tmpClone, "commit", "-m", "second")
	runGit(t, tmpClone, "push", "origin", "main")

	// Sync.
	out, err = runGT(t, solHome, "world", "sync", "myworld")
	if err != nil {
		t.Fatalf("world sync failed: %v\n%s", err, out)
	}

	// Verify managed repo has the new commit.
	repoPath := filepath.Join(solHome, "myworld", "repo")
	logOut := runGitOutput(t, repoPath, "log", "--oneline")
	if !strings.Contains(logOut, "second") {
		t.Error("managed repo missing second commit after sync")
	}

	// Verify file2.txt exists in working tree.
	if _, err := os.Stat(filepath.Join(repoPath, "file2.txt")); err != nil {
		t.Error("file2.txt not present in managed repo after sync")
	}
}

func TestWorldSyncLateClone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	// Create remote.
	remoteDir := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, "", "init", "--bare", remoteDir)
	tmpClone := t.TempDir()
	runGit(t, tmpClone, "clone", remoteDir, ".")
	writeTestFile(t, filepath.Join(tmpClone, "README.md"), "hello")
	runGit(t, tmpClone, "add", ".")
	runGit(t, tmpClone, "commit", "-m", "init")
	runGit(t, tmpClone, "push", "origin", "main")

	// Init WITHOUT source-repo.
	out, err := runGT(t, solHome, "init", "--name=myworld", "--skip-checks")
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	// Verify no managed repo yet.
	repoPath := filepath.Join(solHome, "myworld", "repo")
	if _, err := os.Stat(repoPath); !os.IsNotExist(err) {
		t.Fatal("repo should not exist yet")
	}

	// Manually set source_repo in world.toml.
	tomlPath := filepath.Join(solHome, "myworld", "world.toml")
	data, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Replace(string(data),
		`source_repo = ""`,
		fmt.Sprintf(`source_repo = "%s"`, remoteDir),
		1)
	if err := os.WriteFile(tomlPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Sync — should clone.
	out, err = runGT(t, solHome, "world", "sync", "myworld")
	if err != nil {
		t.Fatalf("world sync failed: %v\n%s", err, out)
	}

	// Verify clone was created.
	if _, err := os.Stat(repoPath); err != nil {
		t.Fatalf("repo not created by sync: %v", err)
	}

	logOut := runGitOutput(t, repoPath, "log", "--oneline")
	if !strings.Contains(logOut, "init") {
		t.Error("managed repo missing initial commit")
	}
}

func TestCastUsesManagedRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available, skipping")
	}

	gtHome, sourceRepo := setupTestEnvWithRepo(t)
	setupWorld(t, gtHome, "myworld", sourceRepo)

	// Create a work item.
	out, err := runGT(t, gtHome, "store", "create",
		"--world=myworld", "--title=test task", "--description=test")
	if err != nil {
		t.Fatalf("store create failed: %v\n%s", err, out)
	}

	// Extract work item ID.
	workItemID := extractWorkItemID(t, out)

	// Cast.
	out, err = runGT(t, gtHome, "cast", workItemID, "myworld")
	if err != nil {
		t.Fatalf("cast failed: %v\n%s", err, out)
	}

	// Verify the outpost worktree was created.
	// The worktree should be linked to the managed repo, not the source repo.
	repoPath := filepath.Join(gtHome, "myworld", "repo")
	worktreeListOut := runGitOutput(t, repoPath, "worktree", "list")
	if !strings.Contains(worktreeListOut, "outposts") {
		t.Error("managed repo should list the outpost worktree")
	}
}
