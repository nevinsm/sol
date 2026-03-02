package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitDispatchFlagMode(t *testing.T) {
	// Set SOL_HOME to a fresh temp dir so setup can create directories.
	solHome := filepath.Join(t.TempDir(), "sol-test")
	t.Setenv("SOL_HOME", solHome)

	// Reset package-level flags.
	initName = "testworld"
	initSourceRepo = ""
	initSkipChecks = true
	defer func() {
		initName = ""
		initSourceRepo = ""
		initSkipChecks = false
	}()

	rootCmd.SetArgs([]string{"init", "--name=testworld", "--skip-checks"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	// Verify world.toml was created (flag mode ran).
	tomlPath := filepath.Join(solHome, "testworld", "world.toml")
	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		t.Error("world.toml not created — flag mode did not run")
	}
}

func TestInitDispatchNoFlagsNoTTY(t *testing.T) {
	solHome := filepath.Join(t.TempDir(), "sol-test")
	t.Setenv("SOL_HOME", solHome)

	// Reset flags to simulate no --name.
	initName = ""
	initSourceRepo = ""
	initSkipChecks = false
	defer func() {
		initName = ""
		initSourceRepo = ""
		initSkipChecks = false
	}()

	// Redirect stdin to /dev/null so isTerminal() returns false.
	oldStdin := os.Stdin
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("failed to open %s: %v", os.DevNull, err)
	}
	os.Stdin = devNull
	defer func() {
		os.Stdin = oldStdin
		devNull.Close()
	}()

	rootCmd.SetArgs([]string{"init"})
	err = rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when no --name and no TTY, got success")
	}
	if !strings.Contains(err.Error(), "--name flag is required when stdin is not a terminal") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestInitSourceRepoValidation(t *testing.T) {
	t.Run("nonexistent path", func(t *testing.T) {
		solHome := filepath.Join(t.TempDir(), "sol-test")
		t.Setenv("SOL_HOME", solHome)

		initName = "testworld"
		initSourceRepo = "/nonexistent/path/to/repo"
		initSkipChecks = true
		defer func() {
			initName = ""
			initSourceRepo = ""
			initSkipChecks = false
		}()

		rootCmd.SetArgs([]string{"init", "--name=testworld", "--source-repo=/nonexistent/path/to/repo", "--skip-checks"})
		err := rootCmd.Execute()
		if err == nil {
			t.Fatal("expected error for nonexistent source-repo, got success")
		}
		if !strings.Contains(err.Error(), "source repo path") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("path is a file not directory", func(t *testing.T) {
		solHome := filepath.Join(t.TempDir(), "sol-test")
		t.Setenv("SOL_HOME", solHome)

		// Create a temp file (not a directory).
		tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(tmpFile, []byte("x"), 0o644); err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}

		initName = "testworld"
		initSourceRepo = tmpFile
		initSkipChecks = true
		defer func() {
			initName = ""
			initSourceRepo = ""
			initSkipChecks = false
		}()

		rootCmd.SetArgs([]string{"init", "--name=testworld", "--source-repo=" + tmpFile, "--skip-checks"})
		err := rootCmd.Execute()
		if err == nil {
			t.Fatal("expected error for non-directory source-repo, got success")
		}
		if !strings.Contains(err.Error(), "not a directory") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}
