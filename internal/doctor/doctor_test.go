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

func TestParseTmuxVersion(t *testing.T) {
	tests := []struct {
		input string
		major int
		minor int
		ok    bool
	}{
		{"tmux 3.5a", 3, 5, true},
		{"tmux 3.1", 3, 1, true},
		{"tmux 2.9", 2, 9, true},
		{"tmux 3.0", 3, 0, true},
		{"tmux next-3.4", 3, 4, true},
		{"tmux 10.2c", 10, 2, true},
		{"tmux", 0, 0, false},
		{"", 0, 0, false},
		{"no version here", 0, 0, false},
	}
	for _, tt := range tests {
		major, minor, ok := parseTmuxVersion(tt.input)
		if ok != tt.ok || major != tt.major || minor != tt.minor {
			t.Errorf("parseTmuxVersion(%q) = (%d, %d, %v), want (%d, %d, %v)",
				tt.input, major, minor, ok, tt.major, tt.minor, tt.ok)
		}
	}
}

func TestCheckTmuxVersion(t *testing.T) {
	tests := []struct {
		version string
		passed  bool
		substr  string // expected substring in Message
	}{
		{"tmux 3.5a", true, "tmux 3.5a"},
		{"tmux 3.1", true, "tmux 3.1"},
		{"tmux 4.0", true, "tmux 4.0"},
		{"tmux 3.0", false, "sol requires tmux 3.1 or later"},
		{"tmux 2.9", false, "sol requires tmux 3.1 or later"},
		{"tmux 1.8", false, "sol requires tmux 3.1 or later"},
		{"tmux next-3.4", true, "tmux next-3.4"},
		{"tmux next-2.0", false, "sol requires tmux 3.1 or later"},
		{"weird-version", true, "could not parse version"},
		{"", true, "could not parse version"},
	}
	for _, tt := range tests {
		result := checkTmuxVersion(tt.version, "/usr/bin/tmux")
		if result.Passed != tt.passed {
			t.Errorf("checkTmuxVersion(%q): Passed=%v, want %v (msg: %s)",
				tt.version, result.Passed, tt.passed, result.Message)
		}
		if !strings.Contains(result.Message, tt.substr) {
			t.Errorf("checkTmuxVersion(%q): Message=%q, want substring %q",
				tt.version, result.Message, tt.substr)
		}
		if !result.Passed && result.Fix == "" {
			t.Errorf("checkTmuxVersion(%q): expected non-empty Fix when Passed=false", tt.version)
		}
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

	worlds, err := discoverWorlds(dir)
	if err != nil {
		t.Fatalf("discoverWorlds: %v", err)
	}
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

// TestDiscoverWorldsReturnsErrorOnUnreadable verifies discoverWorlds
// surfaces ReadDir failures to callers instead of silently returning nil,
// so doctor.RunAll can report the discovery failure as a CheckResult.
// (CF-L8 / pattern P1.)
func TestDiscoverWorldsReturnsErrorOnUnreadable(t *testing.T) {
	// Pass a path that definitely does not exist — ReadDir will fail.
	nonexistent := filepath.Join(t.TempDir(), "definitely", "not", "there")

	worlds, err := discoverWorlds(nonexistent)
	if err == nil {
		t.Fatal("expected error for nonexistent SOL_HOME, got nil")
	}
	if worlds != nil {
		t.Errorf("expected nil worlds slice on error, got %v", worlds)
	}
	if !strings.Contains(err.Error(), "read SOL_HOME") {
		t.Errorf("expected wrapped error to mention SOL_HOME, got: %v", err)
	}
}

// --- CheckRuntimeBinaries tests ---

func TestCheckRuntimeBinariesNoWorlds(t *testing.T) {
	results := CheckRuntimeBinaries(nil)
	if len(results) != 0 {
		t.Errorf("expected 0 results for no worlds, got %d", len(results))
	}
}

func TestCheckRuntimeBinariesSkipsClaude(t *testing.T) {
	// Create a world with default config (runtime = "claude").
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	worldDir := filepath.Join(dir, "testworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Empty world.toml → defaults to "claude" runtime.
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckRuntimeBinaries([]string{"testworld"})
	// "claude" should be skipped (handled by CheckClaude), so no results.
	for _, r := range results {
		if r.Name == "runtime:claude" {
			t.Error("expected claude runtime to be skipped, but got a check result for it")
		}
	}
}

func TestCheckRuntimeBinariesNonClaudeRuntime(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	worldDir := filepath.Join(dir, "codexworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Set default_runtime = "codex".
	tomlContent := `[agents]
default_runtime = "codex"
`
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckRuntimeBinaries([]string{"codexworld"})

	// Should have a check for "codex" runtime.
	var found bool
	for _, r := range results {
		if r.Name == "runtime:codex" {
			found = true
			// codex is unlikely to be on PATH in test env, so it should fail.
			if r.Passed {
				// It's fine if codex happens to exist; just verify the name is right.
				if !strings.Contains(r.Message, "codexworld") {
					t.Errorf("expected Message to reference codexworld, got %q", r.Message)
				}
			} else {
				if !strings.Contains(r.Message, "codexworld") {
					t.Errorf("expected Message to reference codexworld, got %q", r.Message)
				}
				if r.Fix == "" {
					t.Error("expected non-empty Fix when Passed=false")
				}
			}
		}
	}
	if !found {
		t.Error("expected a runtime:codex check result")
	}
}

func TestCheckRuntimeBinariesMultipleWorlds(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Create two worlds both using "somecli" runtime.
	for _, world := range []string{"world1", "world2"} {
		worldDir := filepath.Join(dir, world)
		if err := os.MkdirAll(worldDir, 0o755); err != nil {
			t.Fatal(err)
		}
		tomlContent := `[agents]
default_runtime = "somecli"
`
		if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(tomlContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	results := CheckRuntimeBinaries([]string{"world1", "world2"})

	var found bool
	for _, r := range results {
		if r.Name == "runtime:somecli" {
			found = true
			// Both worlds should be mentioned.
			if !strings.Contains(r.Message, "world1") || !strings.Contains(r.Message, "world2") {
				t.Errorf("expected Message to reference both worlds, got %q", r.Message)
			}
		}
	}
	if !found {
		t.Error("expected a runtime:somecli check result")
	}
}

func TestCheckRuntimeBinariesPerRoleOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	worldDir := filepath.Join(dir, "mixed")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Default runtime is claude, but forge uses "customrt".
	tomlContent := `[agents.runtimes]
forge = "customrt"
`
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckRuntimeBinaries([]string{"mixed"})

	// Should have a check for "customrt" but not for "claude".
	var foundCustom bool
	for _, r := range results {
		if r.Name == "runtime:claude" {
			t.Error("expected claude runtime to be skipped")
		}
		if r.Name == "runtime:customrt" {
			foundCustom = true
			if !strings.Contains(r.Message, "mixed") {
				t.Errorf("expected Message to reference mixed, got %q", r.Message)
			}
		}
	}
	if !foundCustom {
		t.Error("expected a runtime:customrt check result")
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
