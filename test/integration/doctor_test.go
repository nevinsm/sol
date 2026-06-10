package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupWorldForDoctor creates a minimal world directory (with world.toml) under
// solHome so that the upgrade-path checks in doctor are exercised.
func setupWorldForDoctor(t *testing.T, solHome, world string) {
	t.Helper()
	worldDir := filepath.Join(solHome, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tomlContent := "[world]\nbranch = \"main\"\n"
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}
}

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

func TestDoctorDetectsStaleCredentialFile(t *testing.T) {
	skipUnlessIntegration(t)
	solHome := t.TempDir()
	setupWorldForDoctor(t, solHome, "myworld")

	// Plant a regular-file .credentials.json (stale pre-simplification state).
	configDir := filepath.Join(solHome, "myworld", ".claude-config", "outposts", "Toast")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	credPath := filepath.Join(configDir, ".credentials.json")
	if err := os.WriteFile(credPath, []byte(`{"token":"tok"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	out, _ := runGT(t, solHome, "doctor")

	if !strings.Contains(out, "credential_symlink") {
		t.Errorf("expected doctor to report credential_symlink warning, got:\n%s", out)
	}
	if !strings.Contains(out, "Toast") {
		t.Errorf("expected doctor output to mention agent name 'Toast', got:\n%s", out)
	}
}

func TestDoctorDetectsObsoleteAccountsDir(t *testing.T) {
	skipUnlessIntegration(t)
	solHome := t.TempDir()
	setupWorldForDoctor(t, solHome, "myworld")

	// Create stale .accounts/ directory.
	accountsDir := filepath.Join(solHome, ".accounts", "alice")
	if err := os.MkdirAll(accountsDir, 0o700); err != nil {
		t.Fatal(err)
	}

	out, _ := runGT(t, solHome, "doctor")

	if !strings.Contains(out, "obsolete_accounts_dir") {
		t.Errorf("expected doctor to report obsolete_accounts_dir warning, got:\n%s", out)
	}
}

func TestDoctorDetectsDeadConfigKeys(t *testing.T) {
	skipUnlessIntegration(t)
	solHome := t.TempDir()

	// World with a dead [budget] section.
	worldDir := filepath.Join(solHome, "myworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tomlContent := "[world]\nbranch = \"main\"\n\n[budget]\ndaily_limit = 100\n"
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := runGT(t, solHome, "doctor")

	if !strings.Contains(out, "dead_config_keys") {
		t.Errorf("expected doctor to report dead_config_keys warning, got:\n%s", out)
	}
	if !strings.Contains(out, "budget") {
		t.Errorf("expected doctor output to mention 'budget', got:\n%s", out)
	}
}

func TestDoctorDetectsDefunctForgeDir(t *testing.T) {
	skipUnlessIntegration(t)
	solHome := t.TempDir()
	setupWorldForDoctor(t, solHome, "myworld")

	// Create defunct "forge" role directory.
	forgeDir := filepath.Join(solHome, "myworld", ".claude-config", "forge", "forge-merge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	out, _ := runGT(t, solHome, "doctor")

	if !strings.Contains(out, "defunct_config_dir") {
		t.Errorf("expected doctor to report defunct_config_dir warning, got:\n%s", out)
	}
}

func TestDoctorFixCredentialFile(t *testing.T) {
	skipUnlessIntegration(t)
	solHome := t.TempDir()
	setupWorldForDoctor(t, solHome, "myworld")

	// Plant a regular-file .credentials.json.
	configDir := filepath.Join(solHome, "myworld", ".claude-config", "outposts", "Sage")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	credPath := filepath.Join(configDir, ".credentials.json")
	if err := os.WriteFile(credPath, []byte(`{"token":"tok"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Run with --fix --yes (no stdin needed).
	out, _ := runGT(t, solHome, "doctor", "--fix", "--yes")

	if !strings.Contains(out, "done") {
		t.Errorf("expected 'done' in fix output, got:\n%s", out)
	}

	// Credential file should be deleted.
	if _, err := os.Stat(credPath); !os.IsNotExist(err) {
		t.Errorf("expected credential file to be deleted after --fix, but it still exists")
	}
}

func TestDoctorFixObsoleteAccountsDir(t *testing.T) {
	skipUnlessIntegration(t)
	solHome := t.TempDir()
	setupWorldForDoctor(t, solHome, "myworld")

	// Create stale .accounts/ directory.
	accountsDir := filepath.Join(solHome, ".accounts")
	if err := os.MkdirAll(filepath.Join(accountsDir, "alice"), 0o700); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(accountsDir, "alice", "token.json")
	if err := os.WriteFile(tokenPath, []byte(`{"type":"api_key"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	out, _ := runGT(t, solHome, "doctor", "--fix", "--yes")

	// .accounts/ should be moved to a backup.
	if _, err := os.Stat(accountsDir); !os.IsNotExist(err) {
		t.Errorf("expected .accounts/ to be moved, but it still exists")
	}

	// Verify the backup contains the original content.
	entries, err := os.ReadDir(solHome)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".accounts.bak.") && e.IsDir() {
			found = true
			backupToken := filepath.Join(solHome, e.Name(), "alice", "token.json")
			if _, err := os.Stat(backupToken); err != nil {
				t.Errorf("expected token.json preserved in backup dir, got: %v", err)
			}
		}
	}
	if !found {
		t.Errorf("expected a .accounts.bak.* backup directory after fix, output:\n%s", out)
	}
}

func TestDoctorFixDryRun(t *testing.T) {
	skipUnlessIntegration(t)
	solHome := t.TempDir()
	setupWorldForDoctor(t, solHome, "myworld")

	// Plant a stale credential file.
	configDir := filepath.Join(solHome, "myworld", ".claude-config", "outposts", "Nova")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	credPath := filepath.Join(configDir, ".credentials.json")
	if err := os.WriteFile(credPath, []byte(`{"token":"tok"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	out, _ := runGT(t, solHome, "doctor", "--fix", "--dry-run")

	if !strings.Contains(out, "dry-run") {
		t.Errorf("expected '--dry-run' in output, got:\n%s", out)
	}

	// File should NOT be deleted in dry-run mode.
	if _, err := os.Stat(credPath); err != nil {
		t.Errorf("expected credential file to remain after --dry-run, got: %v", err)
	}
}

func TestDoctorFixDoesNotTouchActiveSession(t *testing.T) {
	// This test verifies that --fix skips defunct config dirs whose agent
	// name matches an active tmux session. Since we cannot create a real
	// tmux session without the full test isolation machinery, we test that
	// the "no active sessions" path works (the file is moved).
	//
	// The "has active session" path is covered at the unit level in
	// internal/doctor/upgrade_test.go via the sessionActive() function.
	skipUnlessIntegration(t)
	solHome := t.TempDir()
	setupWorldForDoctor(t, solHome, "myworld")

	// Create defunct "forge" role directory with no active sessions.
	forgeDir := filepath.Join(solHome, "myworld", ".claude-config", "forge", "forge-merge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	out, _ := runGT(t, solHome, "doctor", "--fix", "--yes")

	if !strings.Contains(out, "done") {
		t.Errorf("expected fix to complete, got:\n%s", out)
	}

	// The forge directory should have been moved.
	forgeRolePath := filepath.Join(solHome, "myworld", ".claude-config", "forge")
	if _, err := os.Stat(forgeRolePath); !os.IsNotExist(err) {
		t.Errorf("expected defunct 'forge' dir to be moved, but it still exists")
	}
}

func TestDoctorFixNoIssues(t *testing.T) {
	skipUnlessIntegration(t)
	solHome := t.TempDir()
	setupWorldForDoctor(t, solHome, "myworld")

	// Clean world — no stale state. --fix should report "no fixable issues".
	out, _ := runGT(t, solHome, "doctor", "--fix", "--yes")

	if !strings.Contains(out, "No fixable issues") {
		t.Errorf("expected 'No fixable issues' message for clean world, got:\n%s", out)
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
