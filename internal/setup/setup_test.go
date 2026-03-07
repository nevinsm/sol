package setup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/config"
)

func TestRunBasic(t *testing.T) {
	// Set SOL_HOME to a non-existent path inside t.TempDir().
	solHome := filepath.Join(t.TempDir(), "sol-test")
	t.Setenv("SOL_HOME", solHome)

	result, err := Run(Params{
		WorldName:  "myworld",
		SkipChecks: true,
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Verify Result has correct paths.
	if result.SOLHome != solHome {
		t.Errorf("SOLHome = %q, want %q", result.SOLHome, solHome)
	}
	if result.WorldName != "myworld" {
		t.Errorf("WorldName = %q, want %q", result.WorldName, "myworld")
	}
	if result.ConfigPath != filepath.Join(solHome, "myworld", "world.toml") {
		t.Errorf("ConfigPath = %q, want %q", result.ConfigPath, filepath.Join(solHome, "myworld", "world.toml"))
	}
	if result.DBPath != filepath.Join(solHome, ".store", "myworld.db") {
		t.Errorf("DBPath = %q, want %q", result.DBPath, filepath.Join(solHome, ".store", "myworld.db"))
	}

	// Verify SOL_HOME directory created.
	if _, err := os.Stat(solHome); os.IsNotExist(err) {
		t.Error("SOL_HOME directory not created")
	}

	// Verify .store/ directory created.
	if _, err := os.Stat(filepath.Join(solHome, ".store")); os.IsNotExist(err) {
		t.Error(".store/ directory not created")
	}

	// Verify world.toml exists.
	if _, err := os.Stat(result.ConfigPath); os.IsNotExist(err) {
		t.Error("world.toml not created")
	}

	// Verify myworld.db exists.
	if _, err := os.Stat(result.DBPath); os.IsNotExist(err) {
		t.Error("myworld.db not created")
	}

	// Verify myworld/outposts/ directory exists.
	outpostsDir := filepath.Join(solHome, "myworld", "outposts")
	if _, err := os.Stat(outpostsDir); os.IsNotExist(err) {
		t.Error("myworld/outposts/ directory not created")
	}
}

func TestRunWithSourceRepo(t *testing.T) {
	solHome := filepath.Join(t.TempDir(), "sol-test")
	t.Setenv("SOL_HOME", solHome)

	// Create a source git repo with a commit.
	sourceRepo := t.TempDir()
	runGit(t, sourceRepo, "init")
	runGit(t, sourceRepo, "commit", "--allow-empty", "-m", "init")

	result, err := Run(Params{
		WorldName:  "myworld",
		SourceRepo: sourceRepo,
		SkipChecks: true,
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Verify Result.SourceRepo is set.
	if result.SourceRepo != sourceRepo {
		t.Errorf("SourceRepo = %q, want %q", result.SourceRepo, sourceRepo)
	}

	// Load world.toml and verify source_repo field.
	data, err := os.ReadFile(result.ConfigPath)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if !strings.Contains(string(data), sourceRepo) {
		t.Errorf("world.toml does not contain source_repo %q: %s", sourceRepo, data)
	}

	// Verify managed clone exists.
	repoPath := config.RepoPath("myworld")
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		t.Error("managed clone not created")
	}
}

func TestRunInvalidWorldName(t *testing.T) {
	solHome := filepath.Join(t.TempDir(), "sol-test")
	t.Setenv("SOL_HOME", solHome)

	// Empty world name.
	_, err := Run(Params{WorldName: "", SkipChecks: true})
	if err == nil {
		t.Fatal("expected error for empty world name")
	}
	if !strings.Contains(err.Error(), "world name is required") {
		t.Errorf("expected 'world name is required' error, got: %v", err)
	}

	// Reserved name.
	_, err = Run(Params{WorldName: "store", SkipChecks: true})
	if err == nil {
		t.Fatal("expected error for reserved world name")
	}
	if !strings.Contains(err.Error(), "reserved") {
		t.Errorf("expected 'reserved' error, got: %v", err)
	}
}

func TestRunDoctorFails(t *testing.T) {
	solHome := filepath.Join(t.TempDir(), "sol-test")
	t.Setenv("SOL_HOME", solHome)

	// Run with SkipChecks=false.
	// If all prerequisites are present, doctor will pass and setup succeeds.
	// If doctor reports failures, the error message should include failure details.
	result, err := Run(Params{
		WorldName:  "myworld",
		SkipChecks: false,
	})
	if err != nil {
		// Doctor failed — verify error message includes failure details.
		if !strings.Contains(err.Error(), "prerequisite check(s) failed") {
			t.Errorf("expected 'prerequisite check(s) failed' in error, got: %v", err)
		}
		if !strings.Contains(err.Error(), "sol doctor") {
			t.Errorf("expected 'sol doctor' hint in error, got: %v", err)
		}
	} else {
		// Doctor passed — verify setup completed.
		if result == nil {
			t.Fatal("expected non-nil result when doctor passes")
		}
	}
}

func TestRunSkipChecks(t *testing.T) {
	solHome := filepath.Join(t.TempDir(), "sol-test")
	t.Setenv("SOL_HOME", solHome)

	result, err := Run(Params{
		WorldName:  "myworld",
		SkipChecks: true,
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestRunIdempotent(t *testing.T) {
	solHome := filepath.Join(t.TempDir(), "sol-test")
	t.Setenv("SOL_HOME", solHome)

	// First run — success.
	_, err := Run(Params{WorldName: "myworld", SkipChecks: true})
	if err != nil {
		t.Fatalf("first Run() error: %v", err)
	}

	// Second run — should fail with "already initialized".
	_, err = Run(Params{WorldName: "myworld", SkipChecks: true})
	if err == nil {
		t.Fatal("expected error on second run")
	}
	if !strings.Contains(err.Error(), "already initialized") {
		t.Errorf("expected 'already initialized' error, got: %v", err)
	}
}

func TestValidateParams(t *testing.T) {
	// Empty WorldName → error.
	p := Params{WorldName: ""}
	if err := p.Validate(); err == nil {
		t.Error("expected error for empty WorldName")
	}

	// Reserved name → error.
	p = Params{WorldName: "store"}
	if err := p.Validate(); err == nil {
		t.Error("expected error for reserved name")
	}

	// Valid name → nil.
	p = Params{WorldName: "myworld"}
	if err := p.Validate(); err != nil {
		t.Errorf("unexpected error for valid name: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %s: %v", args, out, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCloneRepoFromLocalPath(t *testing.T) {
	// Create a source git repo with a commit.
	sourceDir := t.TempDir()
	runGit(t, sourceDir, "init")
	runGit(t, sourceDir, "commit", "--allow-empty", "-m", "init")

	// Set SOL_HOME to temp dir.
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	world := "testworld"
	worldDir := filepath.Join(solHome, world)
	os.MkdirAll(worldDir, 0o755)

	err := CloneRepo(world, sourceDir)
	if err != nil {
		t.Fatalf("CloneRepo failed: %v", err)
	}

	// Verify clone exists and is a git repo.
	repoPath := config.RepoPath(world)
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--is-inside-work-tree")
	if _, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("managed clone is not a git repo: %v", err)
	}
}

func TestCloneRepoAdoptsUpstream(t *testing.T) {
	// Create a "remote" repo.
	remoteDir := t.TempDir()
	runGit(t, remoteDir, "init", "--bare")

	// Create a local repo cloned from the remote.
	localDir := t.TempDir()
	runGit(t, localDir, "clone", remoteDir, ".")

	// Create initial commit in local and push.
	writeFile(t, filepath.Join(localDir, "README.md"), "hello")
	runGit(t, localDir, "add", ".")
	runGit(t, localDir, "commit", "-m", "init")
	runGit(t, localDir, "push", "origin", "main")

	// Clone from local path.
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	world := "testworld"
	os.MkdirAll(filepath.Join(solHome, world), 0o755)

	err := CloneRepo(world, localDir)
	if err != nil {
		t.Fatalf("CloneRepo failed: %v", err)
	}

	// Verify managed clone's origin points to the remote, not the local path.
	repoPath := config.RepoPath(world)
	cmd := exec.Command("git", "-C", repoPath, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to get origin URL: %v", err)
	}
	origin := strings.TrimSpace(string(out))
	if origin == localDir {
		t.Errorf("managed clone origin should be upstream (%s), got local path (%s)", remoteDir, origin)
	}
	if origin != remoteDir {
		t.Errorf("managed clone origin = %q, want %q", origin, remoteDir)
	}
}

func TestCloneRepoFromURL(t *testing.T) {
	// Create a bare repo (simulates a remote URL — git clone works with paths).
	remoteDir := t.TempDir()
	runGit(t, remoteDir, "init", "--bare")

	// Push an initial commit via a temp clone.
	tmpClone := t.TempDir()
	runGit(t, tmpClone, "clone", remoteDir, ".")
	writeFile(t, filepath.Join(tmpClone, "README.md"), "hello")
	runGit(t, tmpClone, "add", ".")
	runGit(t, tmpClone, "commit", "-m", "init")
	runGit(t, tmpClone, "push", "origin", "main")

	// Clone using the bare path (acts like a URL — os.Stat won't IsDir true for bare).
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	world := "testworld"
	os.MkdirAll(filepath.Join(solHome, world), 0o755)

	err := CloneRepo(world, remoteDir)
	if err != nil {
		t.Fatalf("CloneRepo failed: %v", err)
	}

	// Verify clone has the commit.
	repoPath := config.RepoPath(world)
	cmd := exec.Command("git", "-C", repoPath, "log", "--oneline")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log failed: %v", err)
	}
	if !strings.Contains(string(out), "init") {
		t.Error("managed clone missing initial commit")
	}
}

func TestCloneRepoInstallsExcludes(t *testing.T) {
	sourceDir := t.TempDir()
	runGit(t, sourceDir, "init")
	runGit(t, sourceDir, "commit", "--allow-empty", "-m", "init")

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	world := "testworld"
	os.MkdirAll(filepath.Join(solHome, world), 0o755)

	if err := CloneRepo(world, sourceDir); err != nil {
		t.Fatalf("CloneRepo failed: %v", err)
	}

	// Verify .git/info/exclude contains sol patterns.
	repoPath := config.RepoPath(world)
	data, err := os.ReadFile(filepath.Join(repoPath, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("failed to read exclude file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, ".claude/settings.local.json") {
		t.Error("exclude file missing .claude/settings.local.json pattern")
	}
	if !strings.Contains(content, "CLAUDE.local.md") {
		t.Error("exclude file missing CLAUDE.local.md pattern")
	}
	if !strings.Contains(content, ".brief/") {
		t.Error("exclude file missing .brief/ pattern")
	}
	if !strings.Contains(content, ".workflow/") {
		t.Error("exclude file missing .workflow/ pattern")
	}

	// Verify git ignores sol-managed local files but NOT shared .claude/ files.
	os.MkdirAll(filepath.Join(repoPath, ".claude"), 0o755)
	writeFile(t, filepath.Join(repoPath, "CLAUDE.local.md"), "test")
	writeFile(t, filepath.Join(repoPath, ".claude", "settings.local.json"), "test")
	writeFile(t, filepath.Join(repoPath, ".claude", "CLAUDE.md"), "shared project instructions")
	writeFile(t, filepath.Join(repoPath, ".claude", "settings.json"), "shared settings")
	os.MkdirAll(filepath.Join(repoPath, ".brief"), 0o755)
	writeFile(t, filepath.Join(repoPath, ".brief", "memory.md"), "test")
	os.MkdirAll(filepath.Join(repoPath, ".workflow"), 0o755)
	writeFile(t, filepath.Join(repoPath, ".workflow", "manifest.json"), "test")

	// Use git check-ignore to verify which paths are excluded.
	// Sol-managed local files should be ignored.
	shouldBeIgnored := []string{
		".claude/settings.local.json",
		"CLAUDE.local.md",
		".brief/memory.md",
		".workflow/manifest.json",
	}
	for _, p := range shouldBeIgnored {
		cmd := exec.Command("git", "-C", repoPath, "check-ignore", "-q", p)
		if err := cmd.Run(); err != nil {
			t.Errorf("%s should be ignored by git but is not", p)
		}
	}

	// Shared .claude/ files should NOT be ignored — they belong in version control.
	shouldNotBeIgnored := []string{
		".claude/CLAUDE.md",
		".claude/settings.json",
	}
	for _, p := range shouldNotBeIgnored {
		cmd := exec.Command("git", "-C", repoPath, "check-ignore", "-q", p)
		if err := cmd.Run(); err == nil {
			t.Errorf("%s should NOT be ignored by git but it is", p)
		}
	}
}

func TestCloneRepoAlreadyExists(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	world := "testworld"
	repoPath := config.RepoPath(world)
	os.MkdirAll(repoPath, 0o755)

	err := CloneRepo(world, "/tmp/fake")
	if err == nil {
		t.Fatal("expected error when repo already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error: %v", err)
	}
}
