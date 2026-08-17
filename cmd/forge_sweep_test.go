package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// runGitSweep runs a git command and fails the test on error.
func runGitSweep(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// setupForgeSweepWorld creates a SOL_HOME with a world whose managed repo
// (config.RepoPath(world)) is a real git clone with an "origin" remote and a
// "main" branch — enough for forge.SweepBranches to run its git fetch/verify
// steps without error, with nothing to sweep.
func setupForgeSweepWorld(t *testing.T, world string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	worldDir := filepath.Join(dir, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[world]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Bare "origin" outside the world, and a managed clone at the exact
	// path openForge(Strict) expects: $SOL_HOME/{world}/repo.
	bareDir := filepath.Join(dir, "origin.git")
	runGitSweep(t, dir, "init", "--bare", bareDir)

	repoPath := config.RepoPath(world)
	runGitSweep(t, dir, "clone", bareDir, repoPath)
	runGitSweep(t, repoPath, "config", "user.email", "test@test.com")
	runGitSweep(t, repoPath, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitSweep(t, repoPath, "add", ".")
	runGitSweep(t, repoPath, "commit", "-m", "init")
	runGitSweep(t, repoPath, "push", "origin", "main")

	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	ss.Close()
}

func resetForgeSweepFlags() {
	forgeSweepWorld = ""
	forgeSweepIncludeClosedOrphans = false
	forgeSweepDryRun = false
	forgeSweepJSON = false
	forgeSweepConfirm = false
}

func TestForgeSweepWithoutConfirmIsDryRunAndExitsNonZero(t *testing.T) {
	world := "sweepconfirmtest"
	setupForgeSweepWorld(t, world)
	resetForgeSweepFlags()
	t.Cleanup(resetForgeSweepFlags)

	rootCmd.SetArgs([]string{"forge", "sweep", "--world=" + world})

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := rootCmd.Execute()
	w.Close()
	os.Stdout = oldStdout
	var out strings.Builder
	buf := make([]byte, 4096)
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
		}
		if rerr != nil {
			break
		}
	}

	if err == nil {
		t.Fatal("expected non-nil error (no --confirm passed)")
	}
	if code := ExitCode(err); code != 1 {
		t.Errorf("expected exit code 1, got %d (err: %v)", code, err)
	}
	output := out.String()
	if !strings.Contains(output, "(dry-run)") {
		t.Errorf("expected dry-run preview output, got: %s", output)
	}
	if !strings.Contains(output, "Run with --confirm to proceed.") {
		t.Errorf("expected confirm-gate hint, got: %s", output)
	}
}

func TestForgeSweepWithConfirmRuns(t *testing.T) {
	world := "sweepconfirmtest2"
	setupForgeSweepWorld(t, world)
	resetForgeSweepFlags()
	t.Cleanup(resetForgeSweepFlags)

	rootCmd.SetArgs([]string{"forge", "sweep", "--world=" + world, "--confirm"})

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := rootCmd.Execute()
	w.Close()
	os.Stdout = oldStdout
	var out strings.Builder
	buf := make([]byte, 4096)
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
		}
		if rerr != nil {
			break
		}
	}

	if err != nil {
		t.Fatalf("forge sweep --confirm: %v (output: %s)", err, out.String())
	}
	if strings.Contains(out.String(), "(dry-run)") {
		t.Errorf("confirmed sweep should not report dry-run, got: %s", out.String())
	}
}

// TestForgeSweepFailsClosedWithoutConfiguredRepo verifies the second half of
// confirmed fix #2: a world with no managed clone and no world.toml
// source_repo must fail closed instead of silently discovering whatever git
// repo the test process's cwd happens to be inside (this worktree itself).
func TestForgeSweepFailsClosedWithoutConfiguredRepo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	world := "norepoworld"
	worldDir := filepath.Join(dir, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// No "repo" subdirectory, no source_repo configured.
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[world]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	ss.Close()

	resetForgeSweepFlags()
	t.Cleanup(resetForgeSweepFlags)

	// Deliberately do NOT change cwd — the test process's cwd is inside this
	// very git worktree, which is exactly the scenario the old fallback
	// would have silently swept.
	rootCmd.SetArgs([]string{"forge", "sweep", "--world=" + world, "--confirm"})
	err = rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for world with no managed or configured repo")
	}
	if !strings.Contains(err.Error(), "has no managed or configured repo") {
		t.Errorf("expected fail-closed error naming the world, got: %v", err)
	}
	if strings.Contains(err.Error(), "not in a git repo") {
		t.Errorf("should fail closed on the world's missing repo config, not fall back to cwd discovery: %v", err)
	}
}
