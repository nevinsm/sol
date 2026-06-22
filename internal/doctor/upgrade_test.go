package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- CheckCredentialSymlinks tests ---

func TestCheckCredentialSymlinksCleanState(t *testing.T) {
	dir := t.TempDir()
	world := "myworld"

	// Create a .credentials.json that IS a symlink — clean post-simplification state.
	configDir := filepath.Join(dir, world, ".claude-config", "outposts", "Toast")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "fake-global-creds.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(configDir, ".credentials.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	results := CheckCredentialSymlinks(dir, []string{world})
	// No warnings — the symlink is the correct state.
	for _, r := range results {
		if r.Warning || !r.Passed {
			t.Errorf("expected no warnings for symlink credential, got: %+v", r)
		}
	}
}

func TestCheckCredentialSymlinksRegularFile(t *testing.T) {
	dir := t.TempDir()
	world := "myworld"

	// Create a .credentials.json that is a regular file — stale pre-simplification state.
	configDir := filepath.Join(dir, world, ".claude-config", "outposts", "Toast")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	credPath := filepath.Join(configDir, ".credentials.json")
	if err := os.WriteFile(credPath, []byte(`{"token":"tok"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	results := CheckCredentialSymlinks(dir, []string{world})
	if len(results) != 1 {
		t.Fatalf("expected 1 result for regular-file credential, got %d", len(results))
	}
	r := results[0]
	if !r.Passed {
		t.Errorf("expected Passed=true (warning, not failure), got Passed=false")
	}
	if !r.Warning {
		t.Errorf("expected Warning=true for regular-file credential")
	}
	if !strings.Contains(r.Name, "myworld") {
		t.Errorf("expected Name to contain world name, got %q", r.Name)
	}
	if !strings.Contains(r.Name, "outposts") {
		t.Errorf("expected Name to contain role, got %q", r.Name)
	}
	if !strings.Contains(r.Name, "Toast") {
		t.Errorf("expected Name to contain agent name, got %q", r.Name)
	}
	if r.Fix == "" {
		t.Error("expected non-empty Fix")
	}
	if r.Remediate == nil {
		t.Fatal("expected non-nil Remediate for regular-file credential")
	}
}

func TestCheckCredentialSymlinksRemediationDeletesFile(t *testing.T) {
	dir := t.TempDir()
	world := "myworld"

	configDir := filepath.Join(dir, world, ".claude-config", "outposts", "Sage")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	credPath := filepath.Join(configDir, ".credentials.json")
	if err := os.WriteFile(credPath, []byte(`{"token":"tok"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	results := CheckCredentialSymlinks(dir, []string{world})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Apply remediation.
	if err := results[0].Remediate(); err != nil {
		t.Fatalf("Remediate() returned error: %v", err)
	}

	// File should be gone.
	if _, err := os.Stat(credPath); !os.IsNotExist(err) {
		t.Errorf("expected credential file to be deleted, but it still exists")
	}
}

func TestCheckCredentialSymlinksRemediationIdempotent(t *testing.T) {
	dir := t.TempDir()
	world := "myworld"

	configDir := filepath.Join(dir, world, ".claude-config", "outposts", "Sage")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	credPath := filepath.Join(configDir, ".credentials.json")
	if err := os.WriteFile(credPath, []byte(`{"token":"tok"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	results := CheckCredentialSymlinks(dir, []string{world})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Apply remediation twice — should not error on second run.
	if err := results[0].Remediate(); err != nil {
		t.Fatalf("first Remediate() returned error: %v", err)
	}
	if err := results[0].Remediate(); err != nil {
		t.Fatalf("second Remediate() returned error: %v", err)
	}
}

func TestCheckCredentialSymlinksNoConfigDir(t *testing.T) {
	dir := t.TempDir()
	// World without any .claude-config/ directory.
	worldDir := filepath.Join(dir, "emptyworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}

	results := CheckCredentialSymlinks(dir, []string{"emptyworld"})
	if len(results) != 0 {
		t.Errorf("expected 0 results for world with no .claude-config, got %d", len(results))
	}
}

func TestCheckCredentialSymlinksMultipleWorlds(t *testing.T) {
	dir := t.TempDir()

	// World 1: regular file (stale).
	w1AgentDir := filepath.Join(dir, "world1", ".claude-config", "outposts", "Toast")
	if err := os.MkdirAll(w1AgentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w1AgentDir, ".credentials.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	// World 2: symlink (clean).
	w2AgentDir := filepath.Join(dir, "world2", ".claude-config", "outposts", "Nova")
	if err := os.MkdirAll(w2AgentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "creds.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(w2AgentDir, ".credentials.json")); err != nil {
		t.Fatal(err)
	}

	results := CheckCredentialSymlinks(dir, []string{"world1", "world2"})
	// Only world1's regular file should generate a warning.
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d: %+v", len(results), results)
	}
	if !strings.Contains(results[0].Name, "world1") {
		t.Errorf("expected result to reference world1, got %q", results[0].Name)
	}
}

// --- CheckObsoleteAccountsDir tests ---

func TestCheckObsoleteAccountsDirAbsent(t *testing.T) {
	dir := t.TempDir()
	result := CheckObsoleteAccountsDir(dir)
	if !result.Passed {
		t.Errorf("expected Passed=true when .accounts/ absent, got: %s", result.Message)
	}
	if result.Warning {
		t.Errorf("expected Warning=false when .accounts/ absent")
	}
	if result.Remediate != nil {
		t.Error("expected nil Remediate when .accounts/ absent")
	}
}

func TestCheckObsoleteAccountsDirPresent(t *testing.T) {
	dir := t.TempDir()
	accountsDir := filepath.Join(dir, ".accounts")
	if err := os.MkdirAll(filepath.Join(accountsDir, "alice"), 0o700); err != nil {
		t.Fatal(err)
	}

	result := CheckObsoleteAccountsDir(dir)
	if !result.Passed {
		t.Errorf("expected Passed=true (warning not failure), got false: %s", result.Message)
	}
	if !result.Warning {
		t.Errorf("expected Warning=true for present .accounts/ directory")
	}
	if !strings.Contains(result.Message, ".accounts") {
		t.Errorf("expected Message to reference .accounts, got %q", result.Message)
	}
	if result.Fix == "" {
		t.Error("expected non-empty Fix")
	}
	if result.Remediate == nil {
		t.Fatal("expected non-nil Remediate when .accounts/ present")
	}
}

func TestCheckObsoleteAccountsDirRemediationMoves(t *testing.T) {
	dir := t.TempDir()
	accountsDir := filepath.Join(dir, ".accounts")
	if err := os.MkdirAll(filepath.Join(accountsDir, "alice"), 0o700); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(accountsDir, "alice", "token.json")
	if err := os.WriteFile(tokenPath, []byte(`{"type":"api_key"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	result := CheckObsoleteAccountsDir(dir)
	if err := result.Remediate(); err != nil {
		t.Fatalf("Remediate() returned error: %v", err)
	}

	// Original .accounts/ should be gone.
	if _, err := os.Stat(accountsDir); !os.IsNotExist(err) {
		t.Error("expected .accounts/ to be moved away, but it still exists")
	}

	// A .accounts.bak.<timestamp>/ should exist somewhere.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".accounts.bak.") && e.IsDir() {
			found = true
			// Verify the original content is preserved in the backup.
			backupToken := filepath.Join(dir, e.Name(), "alice", "token.json")
			if _, err := os.Stat(backupToken); err != nil {
				t.Errorf("expected token.json preserved in backup, got: %v", err)
			}
		}
	}
	if !found {
		t.Error("expected a .accounts.bak.<timestamp>/ directory after remediation")
	}
}

func TestCheckObsoleteAccountsDirRemediationIdempotent(t *testing.T) {
	dir := t.TempDir()
	// After remediation, .accounts/ is gone. Running again should pass cleanly.
	accountsDir := filepath.Join(dir, ".accounts")
	if err := os.MkdirAll(accountsDir, 0o700); err != nil {
		t.Fatal(err)
	}

	result := CheckObsoleteAccountsDir(dir)
	if err := result.Remediate(); err != nil {
		t.Fatalf("first Remediate() failed: %v", err)
	}

	// Second run: .accounts/ is gone, so check should pass with no warning.
	result2 := CheckObsoleteAccountsDir(dir)
	if result2.Warning {
		t.Error("expected no warning on second run after remediation")
	}
}

// --- CheckDeadWorldConfigKeys tests ---

func TestCheckDeadWorldConfigKeysCleanConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	world := "cleanworld"
	worldDir := filepath.Join(dir, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tomlContent := `[world]
source_repo = "git@github.com:org/repo.git"
branch = "main"

[forge]
quality_gates = ["make test"]
`
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckDeadWorldConfigKeys(dir, []string{world})
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	for _, r := range results {
		if r.Warning {
			t.Errorf("expected no warnings for clean config, got: %s — %s", r.Name, r.Message)
		}
	}
}

func TestCheckDeadWorldConfigKeysDeadSection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	world := "oldworld"
	worldDir := filepath.Join(dir, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// [budget] is not in the current WorldConfig struct.
	tomlContent := `[world]
source_repo = "git@github.com:org/repo.git"
branch = "main"

[budget]
daily_limit = 100
`
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckDeadWorldConfigKeys(dir, []string{world})
	var found bool
	for _, r := range results {
		if strings.Contains(r.Name, world) && r.Warning {
			found = true
			if !strings.Contains(r.Message, "budget") {
				t.Errorf("expected Message to mention 'budget', got %q", r.Message)
			}
		}
	}
	if !found {
		t.Error("expected a warning result for world with [budget] section")
	}
}

func TestCheckDeadWorldConfigKeysDefaultAccount(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	world := "oldworld2"
	worldDir := filepath.Join(dir, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// default_account is parsed but a no-op after the simplification.
	tomlContent := `[world]
source_repo = "git@github.com:org/repo.git"
branch = "main"
default_account = "alice"
`
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckDeadWorldConfigKeys(dir, []string{world})
	var found bool
	for _, r := range results {
		if strings.Contains(r.Name, world) && r.Warning {
			found = true
			if !strings.Contains(r.Message, "default_account") {
				t.Errorf("expected Message to mention 'default_account', got %q", r.Message)
			}
		}
	}
	if !found {
		t.Error("expected a warning result for world with default_account set")
	}
}

func TestCheckDeadWorldConfigKeysNoAutoFix(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	world := "oldworld3"
	worldDir := filepath.Join(dir, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tomlContent := "[budget]\ndaily_limit = 50\n"
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckDeadWorldConfigKeys(dir, []string{world})
	for _, r := range results {
		if r.Remediate != nil {
			t.Error("expected Remediate=nil for dead config keys (no auto-fix)")
		}
	}
}

func TestCheckDeadWorldConfigKeysMissingToml(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// World without world.toml — should produce no results (silently skipped).
	results := CheckDeadWorldConfigKeys(dir, []string{"nofile"})
	if len(results) != 0 {
		t.Errorf("expected 0 results for missing world.toml, got %d", len(results))
	}
}

// --- CheckDefunctConfigDirs tests ---

func TestCheckDefunctConfigDirsNoConfigDir(t *testing.T) {
	dir := t.TempDir()
	worldDir := filepath.Join(dir, "myworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// No .claude-config/ directory.
	results := CheckDefunctConfigDirs(dir, []string{"myworld"})
	if len(results) != 0 {
		t.Errorf("expected 0 results when no .claude-config dir, got %d", len(results))
	}
}

func TestCheckDefunctConfigDirsKnownRoles(t *testing.T) {
	dir := t.TempDir()
	world := "cleanworld"

	// Create known active role directories — should not generate warnings.
	for _, role := range []string{"outposts", "envoys", "forge-merge"} {
		roleDir := filepath.Join(dir, world, ".claude-config", role, "SomeAgent")
		if err := os.MkdirAll(roleDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	results := CheckDefunctConfigDirs(dir, []string{world})
	for _, r := range results {
		if r.Warning {
			t.Errorf("expected no warnings for known active roles, got: %s — %s", r.Name, r.Message)
		}
	}
}

func TestCheckDefunctConfigDirsForgeRole(t *testing.T) {
	dir := t.TempDir()
	world := "oldworld"

	// Create a "forge" directory — defunct, renamed to "forge-merge".
	forgeDir := filepath.Join(dir, world, ".claude-config", "forge", "forge-merge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	results := CheckDefunctConfigDirs(dir, []string{world})
	if len(results) == 0 {
		t.Fatal("expected at least one warning for defunct 'forge' directory")
	}
	var found bool
	for _, r := range results {
		if strings.Contains(r.Name, "forge") && r.Warning {
			found = true
			if !strings.Contains(r.Message, "forge-merge") {
				t.Errorf("expected Message to mention 'forge-merge' replacement, got %q", r.Message)
			}
			if r.Remediate == nil {
				t.Error("expected non-nil Remediate for defunct role dir")
			}
		}
	}
	if !found {
		t.Error("expected a warning with 'forge' in name")
	}
}

func TestCheckDefunctConfigDirsUnknownRole(t *testing.T) {
	dir := t.TempDir()
	world := "weirdworld"

	// Unknown role directory.
	unknownDir := filepath.Join(dir, world, ".claude-config", "governors", "agent1")
	if err := os.MkdirAll(unknownDir, 0o755); err != nil {
		t.Fatal(err)
	}

	results := CheckDefunctConfigDirs(dir, []string{world})
	if len(results) == 0 {
		t.Fatal("expected at least one result for unknown role directory")
	}
	var found bool
	for _, r := range results {
		if strings.Contains(r.Name, "governors") && r.Warning {
			found = true
			if !strings.Contains(r.Message, "unrecognized") {
				t.Errorf("expected Message to say 'unrecognized', got %q", r.Message)
			}
		}
	}
	if !found {
		t.Error("expected a warning for unknown 'governors' role directory")
	}
}

func TestCheckDefunctConfigDirsRemediationMoves(t *testing.T) {
	dir := t.TempDir()
	world := "oldworld"

	// Create a "forge" directory with an agent subdir.
	forgeAgentDir := filepath.Join(dir, world, ".claude-config", "forge", "forge-merge")
	if err := os.MkdirAll(forgeAgentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a fake file inside.
	if err := os.WriteFile(filepath.Join(forgeAgentDir, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckDefunctConfigDirs(dir, []string{world})
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	var target *CheckResult
	for i := range results {
		if strings.Contains(results[i].Name, "forge") {
			target = &results[i]
			break
		}
	}
	if target == nil {
		t.Fatal("expected a result for 'forge' role")
	}
	if target.Remediate == nil {
		t.Fatal("expected non-nil Remediate")
	}

	if err := target.Remediate(); err != nil {
		t.Fatalf("Remediate() returned error: %v", err)
	}

	// The forge directory should have been moved.
	forgeRolePath := filepath.Join(dir, world, ".claude-config", "forge")
	if _, err := os.Stat(forgeRolePath); !os.IsNotExist(err) {
		t.Error("expected 'forge' role dir to be moved, but it still exists")
	}

	// A backup directory should exist.
	configRoot := filepath.Join(dir, world, ".claude-config")
	entries, err := os.ReadDir(configRoot)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "forge.bak.") && e.IsDir() {
			found = true
		}
	}
	if !found {
		t.Error("expected a forge.bak.<timestamp>/ backup directory after remediation")
	}
}

func TestCheckDefunctConfigDirsMixedRoles(t *testing.T) {
	dir := t.TempDir()
	world := "mixedworld"

	// Active roles — should not be flagged.
	for _, role := range []string{"outposts", "forge-merge"} {
		if err := os.MkdirAll(filepath.Join(dir, world, ".claude-config", role, "Agent1"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Defunct role.
	if err := os.MkdirAll(filepath.Join(dir, world, ".claude-config", "forge", "Agent2"), 0o755); err != nil {
		t.Fatal(err)
	}

	results := CheckDefunctConfigDirs(dir, []string{world})

	// Should only flag the "forge" directory.
	var warnings int
	for _, r := range results {
		if r.Warning {
			warnings++
			if !strings.Contains(r.Name, "forge") {
				t.Errorf("expected only 'forge' to be flagged, got: %s", r.Name)
			}
		}
	}
	if warnings != 1 {
		t.Errorf("expected exactly 1 warning, got %d", warnings)
	}
}

func TestCheckDefunctConfigDirsSkipsOwnBackups(t *testing.T) {
	dir := t.TempDir()
	world := "loopworld"

	// Simulate doctor's own --fix output: a *.bak.<timestamp>/ directory
	// sitting next to live role dirs. Without the skip, doctor would
	// flag its own backup as defunct on every subsequent run.
	bakDir := filepath.Join(dir, world, ".claude-config", "cache.bak.20260622T184111Z", "agent1")
	if err := os.MkdirAll(bakDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Also a real defunct dir to confirm normal detection still works.
	defunctDir := filepath.Join(dir, world, ".claude-config", "governors", "agentX")
	if err := os.MkdirAll(defunctDir, 0o755); err != nil {
		t.Fatal(err)
	}

	results := CheckDefunctConfigDirs(dir, []string{world})

	var warnings []string
	for _, r := range results {
		if r.Warning {
			warnings = append(warnings, r.Name)
		}
	}
	if len(warnings) != 1 {
		t.Fatalf("expected exactly 1 warning (governors), got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "governors") {
		t.Errorf("expected the warning to be for 'governors', got %q", warnings[0])
	}
	for _, w := range warnings {
		if strings.Contains(w, ".bak.") {
			t.Errorf("doctor flagged its own backup directory: %q", w)
		}
	}
}

// --- FixableChecks tests ---

func TestReportFixableChecks(t *testing.T) {
	fixFn := func() error { return nil }
	report := &Report{
		Checks: []CheckResult{
			{Name: "a", Passed: true, Message: "ok"},
			{Name: "b", Passed: true, Warning: true, Message: "warn", Remediate: fixFn},
			{Name: "c", Passed: false, Message: "fail", Remediate: fixFn},
			{Name: "d", Passed: true, Message: "ok"},
		},
	}

	fixable := report.FixableChecks()
	if len(fixable) != 2 {
		t.Errorf("expected 2 fixable checks, got %d", len(fixable))
	}
	names := make(map[string]bool)
	for _, c := range fixable {
		names[c.Name] = true
	}
	if !names["b"] || !names["c"] {
		t.Errorf("expected fixable checks b and c, got: %v", names)
	}
}

func TestReportFixableChecksEmpty(t *testing.T) {
	report := &Report{
		Checks: []CheckResult{
			{Name: "a", Passed: true, Message: "ok"},
			{Name: "b", Passed: false, Message: "fail", Fix: "manual fix needed"},
		},
	}
	fixable := report.FixableChecks()
	if len(fixable) != 0 {
		t.Errorf("expected 0 fixable checks when no Remediate functions set, got %d", len(fixable))
	}
}
