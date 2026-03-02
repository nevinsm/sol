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
