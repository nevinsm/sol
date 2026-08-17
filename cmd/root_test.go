package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDoctorBypassesEnsureDirs(t *testing.T) {
	// Set SOL_HOME to a non-existent directory inside t.TempDir().
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist")
	t.Setenv("SOL_HOME", nonExistent)

	// Run: sol doctor — should succeed without EnsureDirs creating directories.
	rootCmd.SetArgs([]string{"doctor"})
	err := rootCmd.Execute()
	// doctor may return exit code 1 if checks fail (e.g. claude not installed),
	// but it should NOT fail due to EnsureDirs.
	if err != nil {
		// Check that the error is not from EnsureDirs — it would mention
		// directory creation. A nil error or an exitError from failed checks
		// is acceptable.
		if ExitCode(err) == 0 {
			t.Fatalf("doctor command failed unexpectedly: %v", err)
		}
	}

	// The SOL_HOME directory should NOT have been created.
	if _, err := os.Stat(nonExistent); !os.IsNotExist(err) {
		t.Errorf("SOL_HOME %q should not exist after doctor, but it does", nonExistent)
	}
}

// TestExecuteSetsGitTerminalPromptEnv verifies that Execute() sets
// GIT_TERMINAL_PROMPT=0 process-wide before running any subcommand, so every
// child git process sol spawns (directly, via re-exec'd daemons, or via a
// tmux agent session) inherits a non-interactive git. Without this, an HTTPS
// remote with no stored credential lets git prompt "Username for
// 'https://...':" — harmless in an attended terminal, but a permanent hang
// in a headless daemon or agent session.
func TestExecuteSetsGitTerminalPromptEnv(t *testing.T) {
	t.Setenv("GIT_TERMINAL_PROMPT", "unset")

	nonExistent := filepath.Join(t.TempDir(), "does-not-exist")
	t.Setenv("SOL_HOME", nonExistent)

	// "doctor" bypasses EnsureDirs (see TestDoctorBypassesEnsureDirs) so this
	// exercises Execute()'s env setup without needing a real SOL_HOME.
	rootCmd.SetArgs([]string{"doctor"})
	_ = Execute()

	if got := os.Getenv("GIT_TERMINAL_PROMPT"); got != "0" {
		t.Errorf("GIT_TERMINAL_PROMPT after Execute() = %q, want %q", got, "0")
	}
}
