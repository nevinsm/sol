package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available in test environment")
	}
	result := CheckTmux()
	if !result.Passed {
		t.Fatalf("expected Passed=true, got false: %s", result.Message)
	}
	if !strings.Contains(result.Message, "tmux") {
		t.Errorf("expected Message to contain 'tmux', got %q", result.Message)
	}
}

func TestCheckGit(t *testing.T) {
	result := CheckGit()
	if !result.Passed {
		t.Fatalf("expected Passed=true, got false: %s", result.Message)
	}
	if !strings.Contains(result.Message, "git version") {
		t.Errorf("expected Message to contain 'git version', got %q", result.Message)
	}
}

func TestCheckClaude(t *testing.T) {
	result := CheckClaude()
	if result.Name != "claude" {
		t.Errorf("expected Name='claude', got %q", result.Name)
	}
	if result.Passed {
		if result.Message == "" {
			t.Error("expected non-empty Message when Passed=true")
		}
	} else {
		if result.Fix == "" {
			t.Error("expected non-empty Fix when Passed=false")
		}
	}
}

func TestCheckJq(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available in test environment")
	}
	result := CheckJq()
	if !result.Passed {
		t.Fatalf("expected Passed=true, got false: %s", result.Message)
	}
	if !strings.Contains(result.Message, "jq-") {
		t.Errorf("expected Message to contain 'jq-', got %q", result.Message)
	}
}

func TestCheckSOLHomeExists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	result := CheckSOLHome()
	if !result.Passed {
		t.Fatalf("expected Passed=true, got false: %s", result.Message)
	}
	if result.Message != dir {
		t.Errorf("expected Message=%q, got %q", dir, result.Message)
	}
}

func TestCheckSOLHomeNotExists(t *testing.T) {
	dir := t.TempDir()
	solHome := filepath.Join(dir, "nonexistent")
	t.Setenv("SOL_HOME", solHome)

	result := CheckSOLHome()
	if !result.Passed {
		t.Fatalf("expected Passed=true, got false: %s", result.Message)
	}
	if !strings.Contains(result.Message, "will be created") {
		t.Errorf("expected Message to contain 'will be created', got %q", result.Message)
	}
}

func TestCheckSOLHomeNotWritable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: root can always write")
	}

	dir := t.TempDir()
	readOnly := filepath.Join(dir, "readonly")
	if err := os.Mkdir(readOnly, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOL_HOME", readOnly)

	result := CheckSOLHome()
	if result.Passed {
		t.Fatal("expected Passed=false for read-only directory")
	}
	if result.Fix == "" {
		t.Error("expected non-empty Fix")
	}
}

func TestCheckSQLiteWAL(t *testing.T) {
	result := CheckSQLiteWAL()
	if !result.Passed {
		t.Fatalf("expected Passed=true, got false: %s", result.Message)
	}
}

func TestRunAll(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())

	report := RunAll()
	// 6 base checks (tmux, git, claude, jq, sol_home, sqlite_wal) plus any
	// env file checks discovered in SOL_HOME (none in a fresh temp dir).
	if len(report.Checks) < 6 {
		t.Fatalf("expected at least 6 checks, got %d", len(report.Checks))
	}

	// Verify AllPassed is consistent with individual checks.
	allPassed := true
	for _, c := range report.Checks {
		if !c.Passed {
			allPassed = false
			break
		}
	}
	if report.AllPassed() != allPassed {
		t.Errorf("AllPassed() = %v, but manual check = %v", report.AllPassed(), allPassed)
	}
}

func TestReportAllPassed(t *testing.T) {
	passing := &Report{
		Checks: []CheckResult{
			{Name: "a", Passed: true, Message: "ok"},
			{Name: "b", Passed: true, Message: "ok"},
		},
	}
	if !passing.AllPassed() {
		t.Error("expected AllPassed()=true for all-passing report")
	}

	failing := &Report{
		Checks: []CheckResult{
			{Name: "a", Passed: true, Message: "ok"},
			{Name: "b", Passed: false, Message: "fail", Fix: "fix it"},
		},
	}
	if failing.AllPassed() {
		t.Error("expected AllPassed()=false when one check fails")
	}
}

func TestReportFailedCount(t *testing.T) {
	report := &Report{
		Checks: []CheckResult{
			{Name: "a", Passed: true, Message: "ok"},
			{Name: "b", Passed: false, Message: "fail", Fix: "fix b"},
			{Name: "c", Passed: false, Message: "fail", Fix: "fix c"},
			{Name: "d", Passed: true, Message: "ok"},
		},
	}
	if got := report.FailedCount(); got != 2 {
		t.Errorf("expected FailedCount()=2, got %d", got)
	}
}

// --- CheckEnvFiles tests ---

func TestCheckEnvFilesSphereLevel(t *testing.T) {
	dir := t.TempDir()

	// Write a valid sphere-level .env file.
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("API_KEY=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	results := CheckEnvFiles(dir, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Name != "env:sphere" {
		t.Errorf("expected Name=%q, got %q", "env:sphere", r.Name)
	}
	if !r.Passed {
		t.Errorf("expected Passed=true, got false: %s", r.Message)
	}
}

func TestCheckEnvFilesSphereLevelParseError(t *testing.T) {
	dir := t.TempDir()

	// Write a sphere .env with invalid syntax.
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("INVALID_NO_EQUALS\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	results := CheckEnvFiles(dir, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Passed {
		t.Error("expected Passed=false for malformed .env file")
	}
	if results[0].Fix == "" {
		t.Error("expected non-empty Fix")
	}
}

func TestCheckEnvFilesSphereLevelBadPermissions(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: root ignores file permissions")
	}

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	// Write file with world-readable permissions.
	if err := os.WriteFile(envPath, []byte("KEY=val\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckEnvFiles(dir, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Passed {
		t.Error("expected Passed=false for world-readable .env file")
	}
	if results[0].Fix == "" {
		t.Error("expected non-empty Fix for permission failure")
	}
}

func TestCheckEnvFilesSphereAbsent(t *testing.T) {
	dir := t.TempDir()
	// No .env file — should return no results.
	results := CheckEnvFiles(dir, nil)
	if len(results) != 0 {
		t.Errorf("expected 0 results when .env absent, got %d", len(results))
	}
}

func TestCheckEnvFilesWorldLevel(t *testing.T) {
	dir := t.TempDir()

	// Create a world directory with a valid .env file.
	worldDir := filepath.Join(dir, "myworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(worldDir, ".env")
	if err := os.WriteFile(envPath, []byte("DB_URL=postgres://localhost/mydb\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	results := CheckEnvFiles(dir, []string{"myworld"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Name != "env:myworld" {
		t.Errorf("expected Name=%q, got %q", "env:myworld", r.Name)
	}
	if !r.Passed {
		t.Errorf("expected Passed=true, got false: %s", r.Message)
	}
}

func TestCheckEnvFilesEmptyValue(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("KEY=\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	results := CheckEnvFiles(dir, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Passed {
		t.Error("expected Passed=false for empty value")
	}
}

func TestCheckEnvFilesBothSphereAndWorld(t *testing.T) {
	dir := t.TempDir()

	// Sphere-level .env.
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("GLOBAL=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// World-level .env.
	worldDir := filepath.Join(dir, "dev")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, ".env"), []byte("LOCAL=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	results := CheckEnvFiles(dir, []string{"dev"})
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %v", len(results), results)
	}
	// First result should be the sphere-level check.
	if results[0].Name != "env:sphere" {
		t.Errorf("expected first result Name=%q, got %q", "env:sphere", results[0].Name)
	}
	if results[1].Name != "env:dev" {
		t.Errorf("expected second result Name=%q, got %q", "env:dev", results[1].Name)
	}
}

func TestDiscoverWorlds(t *testing.T) {
	dir := t.TempDir()

	// Create two world directories with world.toml.
	for _, world := range []string{"alpha", "beta"} {
		wdir := filepath.Join(dir, world)
		if err := os.MkdirAll(wdir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wdir, "world.toml"), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Create a directory without world.toml — should be excluded.
	if err := os.MkdirAll(filepath.Join(dir, "notaworld"), 0o755); err != nil {
		t.Fatal(err)
	}

	worlds := discoverWorlds(dir)
	if len(worlds) != 2 {
		t.Fatalf("expected 2 worlds, got %d: %v", len(worlds), worlds)
	}
	// Check both are found (order may vary).
	found := make(map[string]bool)
	for _, w := range worlds {
		found[w] = true
	}
	for _, want := range []string{"alpha", "beta"} {
		if !found[want] {
			t.Errorf("expected world %q not found in %v", want, worlds)
		}
	}
}

func TestRunAllWithSphereEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Write a valid sphere-level .env.
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("TOKEN=abc123\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report := RunAll()

	// Find the env:sphere check.
	var envCheck *CheckResult
	for i := range report.Checks {
		if report.Checks[i].Name == "env:sphere" {
			envCheck = &report.Checks[i]
			break
		}
	}
	if envCheck == nil {
		names := make([]string, len(report.Checks))
		for i, c := range report.Checks {
			names[i] = c.Name
		}
		t.Fatalf("env:sphere check not found in RunAll results: %v", names)
	}
	if !envCheck.Passed {
		t.Errorf("expected env:sphere Passed=true, got false: %s", envCheck.Message)
	}
}
