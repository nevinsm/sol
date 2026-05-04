package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitFlagMode(t *testing.T) {
	skipUnlessIntegration(t)
	// Set SOL_HOME to a non-existent path inside t.TempDir().
	solHome := filepath.Join(t.TempDir(), "sol-init-test")

	out, err := runGT(t, solHome, "init", "--name=myworld", "--skip-checks")
	if err != nil {
		t.Fatalf("sol init failed: %v: %s", err, out)
	}

	// Verify: SOL_HOME created.
	if _, err := os.Stat(solHome); os.IsNotExist(err) {
		t.Error("SOL_HOME not created")
	}

	// Verify: world.toml exists.
	tomlPath := filepath.Join(solHome, "myworld", "world.toml")
	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		t.Error("world.toml not created")
	}

	// Verify: myworld.db exists.
	dbPath := filepath.Join(solHome, ".store", "myworld.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("myworld.db not created")
	}

	// Verify: outposts/ directory exists.
	outpostsDir := filepath.Join(solHome, "myworld", "outposts")
	if _, err := os.Stat(outpostsDir); os.IsNotExist(err) {
		t.Error("myworld/outposts/ not created")
	}

	// Verify: output contains success message.
	if !strings.Contains(out, "sol initialized successfully") {
		t.Errorf("expected success message in output: %s", out)
	}
}

func TestInitFlagModeWithSourceRepo(t *testing.T) {
	skipUnlessIntegration(t)
	solHome := filepath.Join(t.TempDir(), "sol-init-test")

	// Create a real git repo as source.
	sourceRepo := t.TempDir()
	gitRun(t, sourceRepo, "init")
	gitRun(t, sourceRepo, "config", "user.email", "test@test.com")
	gitRun(t, sourceRepo, "config", "user.name", "Test")
	gitRun(t, sourceRepo, "commit", "--allow-empty", "-m", "init")

	out, err := runGT(t, solHome, "init", "--name=myworld", "--source-repo="+sourceRepo, "--skip-checks")
	if err != nil {
		t.Fatalf("sol init failed: %v: %s", err, out)
	}

	// Verify: world.toml contains source_repo.
	tomlPath := filepath.Join(solHome, "myworld", "world.toml")
	data, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if !strings.Contains(string(data), sourceRepo) {
		t.Errorf("world.toml does not contain source_repo %q: %s", sourceRepo, data)
	}

	// Verify: output mentions source.
	if !strings.Contains(out, sourceRepo) {
		t.Errorf("expected source repo in output: %s", out)
	}
}

func TestInitRequiresName(t *testing.T) {
	skipUnlessIntegration(t)
	solHome := filepath.Join(t.TempDir(), "sol-init-test")

	out, err := runGT(t, solHome, "init")
	if err == nil {
		t.Fatalf("expected error, got success: %s", out)
	}
	if !strings.Contains(out, "--name flag is required") {
		t.Errorf("expected '--name flag is required' error, got: %s", out)
	}
}

func TestInitAlreadyInitialized(t *testing.T) {
	skipUnlessIntegration(t)
	solHome := filepath.Join(t.TempDir(), "sol-init-test")

	// First run — success.
	out, err := runGT(t, solHome, "init", "--name=myworld", "--skip-checks")
	if err != nil {
		t.Fatalf("first init failed: %v: %s", err, out)
	}

	// Second run — error.
	out, err = runGT(t, solHome, "init", "--name=myworld", "--skip-checks")
	if err == nil {
		t.Fatalf("expected error on second init, got success: %s", out)
	}
	if !strings.Contains(out, "already initialized") {
		t.Errorf("expected 'already initialized' error, got: %s", out)
	}
}

func TestInitThenWorldOperations(t *testing.T) {
	skipUnlessIntegration(t)
	solHome := filepath.Join(t.TempDir(), "sol-init-test")

	// Run: sol init --name=myworld --skip-checks
	out, err := runGT(t, solHome, "init", "--name=myworld", "--skip-checks")
	if err != nil {
		t.Fatalf("init failed: %v: %s", err, out)
	}

	// Run: sol writ create --world=myworld --title="test"
	out, err = runGT(t, solHome, "writ", "create", "--world=myworld", "--title=test")
	if err != nil {
		t.Fatalf("writ create failed: %v: %s", err, out)
	}

	// Run: sol world list
	out, err = runGT(t, solHome, "world", "list")
	if err != nil {
		t.Fatalf("world list failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "myworld") {
		t.Errorf("world list output missing 'myworld': %s", out)
	}

	// Run: sol world status myworld
	out, err = runGT(t, solHome, "world", "status", "myworld")
	if err != nil {
		t.Fatalf("world status failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Config") {
		t.Errorf("world status output missing 'Config': %s", out)
	}
}

func TestInitInteractiveRequiresTTY(t *testing.T) {
	skipUnlessIntegration(t)
	solHome := filepath.Join(t.TempDir(), "sol-init-test")

	// Run: echo "" | sol init (piped stdin → not a TTY).
	// Should error, not hang waiting for input.
	bin := gtBin(t)
	cmd := exec.Command("sh", "-c", "echo '' | "+bin+" init")
	cmd.Env = append(os.Environ(), "SOL_HOME="+solHome)
	outBytes, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(outBytes))
	if err == nil {
		t.Fatalf("expected error, got success: %s", outStr)
	}
	if !strings.Contains(outStr, "--name flag is required when stdin is not a terminal") {
		t.Errorf("expected non-TTY error message, got: %s", outStr)
	}
}

func TestInitSourceRepoValidationIntegration(t *testing.T) {
	skipUnlessIntegration(t)

	t.Run("nonexistent path", func(t *testing.T) {
		solHome := filepath.Join(t.TempDir(), "sol-init-test")

		_, err := runGT(t, solHome, "init", "--name=testworld", "--source-repo=/nonexistent/path", "--skip-checks")
		if err == nil {
			t.Fatal("expected error for nonexistent source-repo, got success")
		}
	})

	t.Run("path is a file", func(t *testing.T) {
		solHome := filepath.Join(t.TempDir(), "sol-init-test")

		// Create a temp file (not a directory).
		tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(tmpFile, []byte("x"), 0o644); err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}

		_, err := runGT(t, solHome, "init", "--name=testworld", "--source-repo="+tmpFile, "--skip-checks")
		if err == nil {
			t.Fatal("expected error for non-directory source-repo, got success")
		}
	})
}

func TestGuidedFlagExists(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "init", "--help")
	if err != nil {
		t.Fatalf("sol init --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "--guided") {
		t.Errorf("sol init --help output missing '--guided': %s", out)
	}
}

func TestInitRunsDoctorByDefault(t *testing.T) {
	skipUnlessIntegration(t)
	solHome := filepath.Join(t.TempDir(), "sol-init-test")

	// Run: sol init --name=myworld (no --skip-checks)
	out, err := runGT(t, solHome, "init", "--name=myworld")
	if err != nil {
		// Doctor failed — error message should include failed check details.
		if !strings.Contains(out, "prerequisite check(s) failed") {
			t.Errorf("expected 'prerequisite check(s) failed' in error, got: %s", out)
		}
	} else {
		// Doctor passed — setup should have succeeded.
		if !strings.Contains(out, "sol initialized successfully") {
			t.Errorf("expected success message, got: %s", out)
		}
	}
}
