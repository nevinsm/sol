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

func TestInstallExcludes(t *testing.T) {
	t.Run("fresh install writes BEGIN/END block", func(t *testing.T) {
		repoDir := t.TempDir()
		runGit(t, repoDir, "init")

		if err := InstallExcludes(repoDir); err != nil {
			t.Fatalf("InstallExcludes failed: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(repoDir, ".git", "info", "exclude"))
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)

		if !strings.Contains(content, "# BEGIN sol-managed paths") {
			t.Error("missing BEGIN marker")
		}
		if !strings.Contains(content, "# END sol-managed paths") {
			t.Error("missing END marker")
		}
		for _, pat := range []string{
			".claude/settings.local.json",
			".claude/system-prompt.md",
			".claude/skills/",
			"CLAUDE.local.md",
			".brief/",
			".workflow/",
			"AGENTS.override.md",
			".agents/skills/",
			".codex/",
		} {
			if !strings.Contains(content, pat) {
				t.Errorf("missing pattern %q", pat)
			}
		}
	})

	t.Run("idempotent re-run replaces block without duplication", func(t *testing.T) {
		repoDir := t.TempDir()
		runGit(t, repoDir, "init")

		if err := InstallExcludes(repoDir); err != nil {
			t.Fatalf("first InstallExcludes failed: %v", err)
		}
		if err := InstallExcludes(repoDir); err != nil {
			t.Fatalf("second InstallExcludes failed: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(repoDir, ".git", "info", "exclude"))
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)

		// Should have exactly one BEGIN and one END marker.
		if strings.Count(content, "# BEGIN sol-managed paths") != 1 {
			t.Errorf("expected exactly 1 BEGIN marker, got %d", strings.Count(content, "# BEGIN sol-managed paths"))
		}
		if strings.Count(content, "# END sol-managed paths") != 1 {
			t.Errorf("expected exactly 1 END marker, got %d", strings.Count(content, "# END sol-managed paths"))
		}
	})

	t.Run("legacy marker format gets migrated", func(t *testing.T) {
		repoDir := t.TempDir()
		runGit(t, repoDir, "init")

		// Write legacy format (old style without BEGIN/END).
		excludePath := filepath.Join(repoDir, ".git", "info", "exclude")
		existing, err := os.ReadFile(excludePath)
		if err != nil {
			t.Fatal(err)
		}
		legacy := string(existing) + "\n# sol-managed paths\n.claude/\nCLAUDE.local.md\n"
		if err := os.WriteFile(excludePath, []byte(legacy), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := InstallExcludes(repoDir); err != nil {
			t.Fatalf("InstallExcludes failed: %v", err)
		}

		data, err := os.ReadFile(excludePath)
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)

		// Legacy marker should be gone, replaced by BEGIN/END block.
		if strings.Contains(content, "# sol-managed paths\n.claude/\n") {
			t.Error("legacy block was not replaced")
		}
		if !strings.Contains(content, "# BEGIN sol-managed paths") {
			t.Error("missing BEGIN marker after migration")
		}
		if !strings.Contains(content, "# END sol-managed paths") {
			t.Error("missing END marker after migration")
		}
		// Should have the new fine-grained patterns.
		if !strings.Contains(content, ".claude/settings.local.json") {
			t.Error("missing .claude/settings.local.json after migration")
		}
		if !strings.Contains(content, ".claude/system-prompt.md") {
			t.Error("missing .claude/system-prompt.md after migration")
		}
	})

	t.Run("updates existing block when canonical list changes", func(t *testing.T) {
		repoDir := t.TempDir()
		runGit(t, repoDir, "init")

		// Write a stale BEGIN/END block missing .claude/system-prompt.md.
		excludePath := filepath.Join(repoDir, ".git", "info", "exclude")
		existing, err := os.ReadFile(excludePath)
		if err != nil {
			t.Fatal(err)
		}
		stale := string(existing) + "\n# BEGIN sol-managed paths\n.claude/settings.local.json\nCLAUDE.local.md\n# END sol-managed paths\n"
		if err := os.WriteFile(excludePath, []byte(stale), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := InstallExcludes(repoDir); err != nil {
			t.Fatalf("InstallExcludes failed: %v", err)
		}

		data, err := os.ReadFile(excludePath)
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)

		// Should now have the full canonical list.
		if !strings.Contains(content, ".claude/system-prompt.md") {
			t.Error("missing .claude/system-prompt.md after update")
		}
		if !strings.Contains(content, ".brief/") {
			t.Error("missing .brief/ after update")
		}
		if !strings.Contains(content, ".workflow/") {
			t.Error("missing .workflow/ after update")
		}
	})

	t.Run("preserves content before and after block", func(t *testing.T) {
		repoDir := t.TempDir()
		runGit(t, repoDir, "init")

		excludePath := filepath.Join(repoDir, ".git", "info", "exclude")
		existing, err := os.ReadFile(excludePath)
		if err != nil {
			t.Fatal(err)
		}
		// Add content before and after the block.
		withSurrounding := string(existing) + "\n# custom exclude\n*.log\n\n# BEGIN sol-managed paths\nold-pattern\n# END sol-managed paths\n\n# another custom section\n*.tmp\n"
		if err := os.WriteFile(excludePath, []byte(withSurrounding), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := InstallExcludes(repoDir); err != nil {
			t.Fatalf("InstallExcludes failed: %v", err)
		}

		data, err := os.ReadFile(excludePath)
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)

		if !strings.Contains(content, "*.log") {
			t.Error("content before block was lost")
		}
		if !strings.Contains(content, "*.tmp") {
			t.Error("content after block was lost")
		}
		if strings.Contains(content, "old-pattern") {
			t.Error("old block content was not replaced")
		}
	})
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
	if !strings.Contains(content, "# BEGIN sol-managed paths") {
		t.Error("exclude file missing BEGIN marker")
	}
	if !strings.Contains(content, ".claude/settings.local.json") {
		t.Error("exclude file missing .claude/settings.local.json pattern")
	}
	if !strings.Contains(content, ".claude/system-prompt.md") {
		t.Error("exclude file missing .claude/system-prompt.md pattern")
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
	if !strings.Contains(content, "AGENTS.override.md") {
		t.Error("exclude file missing AGENTS.override.md pattern")
	}
	if !strings.Contains(content, ".agents/skills/") {
		t.Error("exclude file missing .agents/skills/ pattern")
	}
	if !strings.Contains(content, ".codex/") {
		t.Error("exclude file missing .codex/ pattern")
	}

	// Verify git ignores sol-managed local files but NOT shared .claude/ files.
	os.MkdirAll(filepath.Join(repoPath, ".claude"), 0o755)
	writeFile(t, filepath.Join(repoPath, "CLAUDE.local.md"), "test")
	writeFile(t, filepath.Join(repoPath, ".claude", "settings.local.json"), "test")
	writeFile(t, filepath.Join(repoPath, ".claude", "system-prompt.md"), "test")
	writeFile(t, filepath.Join(repoPath, ".claude", "CLAUDE.md"), "shared project instructions")
	writeFile(t, filepath.Join(repoPath, ".claude", "settings.json"), "shared settings")
	os.MkdirAll(filepath.Join(repoPath, ".brief"), 0o755)
	writeFile(t, filepath.Join(repoPath, ".brief", "memory.md"), "test")
	os.MkdirAll(filepath.Join(repoPath, ".workflow"), 0o755)
	writeFile(t, filepath.Join(repoPath, ".workflow", "manifest.json"), "test")
	writeFile(t, filepath.Join(repoPath, "AGENTS.override.md"), "test")
	os.MkdirAll(filepath.Join(repoPath, ".agents", "skills"), 0o755)
	writeFile(t, filepath.Join(repoPath, ".agents", "skills", "test-skill.md"), "test")
	os.MkdirAll(filepath.Join(repoPath, ".codex"), 0o755)
	writeFile(t, filepath.Join(repoPath, ".codex", "config.json"), "test")

	// Use git check-ignore to verify which paths are excluded.
	// Sol-managed local files should be ignored.
	shouldBeIgnored := []string{
		".claude/settings.local.json",
		".claude/system-prompt.md",
		"CLAUDE.local.md",
		".brief/memory.md",
		".workflow/manifest.json",
		"AGENTS.override.md",
		".agents/skills/test-skill.md",
		".codex/config.json",
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

func TestCloneRepoSkipsWhenValidRepoExists(t *testing.T) {
	// Simulate crash recovery: repo was cloned but setup didn't finish.
	// CloneRepo should skip the clone and succeed.
	sourceDir := t.TempDir()
	runGit(t, sourceDir, "init")
	runGit(t, sourceDir, "commit", "--allow-empty", "-m", "init")

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	world := "testworld"
	os.MkdirAll(filepath.Join(solHome, world), 0o755)

	// First clone — succeeds normally.
	if err := CloneRepo(world, sourceDir); err != nil {
		t.Fatalf("first CloneRepo failed: %v", err)
	}

	// Second clone — should skip and succeed (crash recovery path).
	if err := CloneRepo(world, sourceDir); err != nil {
		t.Fatalf("second CloneRepo should succeed (recovery), got: %v", err)
	}

	// Verify the repo is still valid.
	repoPath := config.RepoPath(world)
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--is-inside-work-tree")
	if _, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("managed clone is not a git repo after recovery: %v", err)
	}
}

func TestCloneRepoCleansUpInvalidDir(t *testing.T) {
	// If repo dir exists but is NOT a valid git repo, CloneRepo should
	// remove it and re-clone.
	sourceDir := t.TempDir()
	runGit(t, sourceDir, "init")
	runGit(t, sourceDir, "commit", "--allow-empty", "-m", "init")

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	world := "testworld"

	// Create a bare directory (not a git repo) at the repo path.
	repoPath := config.RepoPath(world)
	os.MkdirAll(repoPath, 0o755)

	// CloneRepo should clean up and re-clone successfully.
	if err := CloneRepo(world, sourceDir); err != nil {
		t.Fatalf("CloneRepo should succeed after cleanup, got: %v", err)
	}

	// Verify the repo is valid.
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--is-inside-work-tree")
	if _, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("managed clone is not a git repo: %v", err)
	}
}

func TestRunCrashRecoveryAfterClone(t *testing.T) {
	// Simulate crash between CloneRepo and world.toml write:
	// - repo dir exists (valid git repo)
	// - world.toml does NOT exist
	// Run should succeed on retry.
	sourceDir := t.TempDir()
	runGit(t, sourceDir, "init")
	runGit(t, sourceDir, "commit", "--allow-empty", "-m", "init")

	solHome := filepath.Join(t.TempDir(), "sol-test")
	t.Setenv("SOL_HOME", solHome)

	world := "myworld"

	// Pre-create the managed repo (simulating a successful clone before crash).
	worldDir := filepath.Join(solHome, world)
	os.MkdirAll(filepath.Join(worldDir, "outposts"), 0o755)
	repoPath := config.RepoPath(world)
	cloneCmd := exec.Command("git", "clone", sourceDir, repoPath)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		t.Fatalf("pre-clone failed: %s: %v", out, err)
	}

	// Verify world.toml does NOT exist (simulating crash before write).
	tomlPath := config.WorldConfigPath(world)
	if _, err := os.Stat(tomlPath); err == nil {
		t.Fatal("world.toml should not exist yet")
	}

	// Run setup — should recover and complete successfully.
	result, err := Run(Params{
		WorldName:  world,
		SourceRepo: sourceDir,
		SkipChecks: true,
	})
	if err != nil {
		t.Fatalf("Run() should recover after partial setup, got: %v", err)
	}

	// Verify setup completed.
	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		t.Error("world.toml not created after recovery")
	}
	if result.WorldName != world {
		t.Errorf("WorldName = %q, want %q", result.WorldName, world)
	}
}
