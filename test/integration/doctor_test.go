package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorRuns(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	// Create .store and .runtime so other tests don't interfere,
	// but doctor itself doesn't need them.
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755)

	out, err := runGT(t, gtHome, "doctor")
	// doctor exits 0 if all checks pass, 1 if any fail.
	// tmux and git should be available; claude may not be.
	// We just verify the output contains expected check names.
	_ = err // exit code 1 is acceptable if claude is missing

	if !strings.Contains(out, "tmux") {
		t.Errorf("doctor output missing tmux check: %s", out)
	}
	if !strings.Contains(out, "git") {
		t.Errorf("doctor output missing git check: %s", out)
	}
}

func TestDoctorJSON(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755)

	out, err := runGT(t, gtHome, "doctor", "--json")
	_ = err // exit code 1 is acceptable

	// Parse output as JSON.
	var report struct {
		Checks []struct {
			Name    string `json:"name"`
			Passed  bool   `json:"passed"`
			Message string `json:"message"`
			Fix     string `json:"fix"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("failed to parse doctor --json output: %v\noutput: %s", err, out)
	}

	if len(report.Checks) == 0 {
		t.Fatal("doctor --json returned no checks")
	}

	// Verify expected check names are present.
	names := make(map[string]bool)
	for _, c := range report.Checks {
		names[c.Name] = true
		if c.Name == "" {
			t.Error("check has empty name")
		}
		if c.Message == "" {
			t.Error("check has empty message")
		}
	}
	for _, expected := range []string{"tmux", "git", "claude", "sol_home", "sqlite_wal"} {
		if !names[expected] {
			t.Errorf("missing check %q in JSON output", expected)
		}
	}
}

func TestDoctorBeforeInit(t *testing.T) {
	skipUnlessIntegration(t)
	// Set SOL_HOME to a path that doesn't exist.
	nonExistent := filepath.Join(t.TempDir(), "not-created-yet")

	out, err := runGT(t, nonExistent, "doctor")
	_ = err // exit code 1 is acceptable if some checks fail
	_ = out

	// Verify SOL_HOME was NOT created as a side effect.
	if _, err := os.Stat(nonExistent); !os.IsNotExist(err) {
		t.Errorf("SOL_HOME %q should not exist after doctor, but it does", nonExistent)
	}
}

func TestInitBypassesEnsureDirs(t *testing.T) {
	// This test validates that "sol world init" bypasses PersistentPreRunE.
	// Full init testing is in prompt 04; this only checks the bypass works.
	skipUnlessIntegration(t)

	// Set SOL_HOME to a path that doesn't exist (but parent exists).
	parent := t.TempDir()
	solHome := filepath.Join(parent, "sol-test-home")

	out, err := runGT(t, solHome, "world", "init", "testworld")
	if err != nil {
		t.Fatalf("sol world init failed: %v: %s", err, out)
	}

	// The world init command should have created SOL_HOME (it calls
	// os.MkdirAll and config.EnsureDirs internally), but the point is
	// that PersistentPreRunE didn't block it.
	if !strings.Contains(out, "initialized") {
		t.Errorf("expected 'initialized' in output, got: %s", out)
	}
}
