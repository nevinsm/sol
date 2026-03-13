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
	// 6 prerequisite checks + 1 env_files check (no worlds in temp SOL_HOME).
	if len(report.Checks) != 7 {
		t.Fatalf("expected 7 checks, got %d", len(report.Checks))
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

// makeWorld creates a minimal initialized world directory structure under solHome
// and returns the world directory path.
func makeWorld(t *testing.T, solHome, world string) string {
	t.Helper()
	worldDir := filepath.Join(solHome, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a minimal world.toml so RequireWorld recognizes it as initialized.
	worldToml := filepath.Join(worldDir, "world.toml")
	if err := os.WriteFile(worldToml, []byte("[world]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return worldDir
}

func TestCheckEnvFilesNoWorlds(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())

	results := CheckEnvFiles()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if !results[0].Passed {
		t.Errorf("expected Passed=true for empty SOL_HOME, got false: %s", results[0].Message)
	}
}

func TestCheckEnvFilesNoEnvFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	makeWorld(t, dir, "alpha")

	results := CheckEnvFiles()
	if len(results) != 1 || !results[0].Passed {
		t.Errorf("expected 1 passing result when no .env exists, got %v", results)
	}
}

func TestCheckEnvFilesValidEnvFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	worldDir := makeWorld(t, dir, "beta")

	envPath := filepath.Join(worldDir, ".env")
	if err := os.WriteFile(envPath, []byte("API_KEY=secret\nDB_URL=postgres://localhost/db\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	results := CheckEnvFiles()
	if len(results) != 1 || !results[0].Passed {
		t.Errorf("expected 1 passing result for valid .env, got %v", results)
	}
}

func TestCheckEnvFilesParseError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	worldDir := makeWorld(t, dir, "gamma")

	// Write a malformed .env (line without '=').
	envPath := filepath.Join(worldDir, ".env")
	if err := os.WriteFile(envPath, []byte("GOOD=value\nBAD_LINE\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	results := CheckEnvFiles()
	if len(results) == 0 {
		t.Fatal("expected at least one failing result for parse error")
	}
	found := false
	for _, r := range results {
		if !r.Passed && strings.Contains(r.Message, "parse error") {
			found = true
			if r.Fix == "" {
				t.Error("expected non-empty Fix for parse error")
			}
		}
	}
	if !found {
		t.Errorf("expected a parse error result, got: %v", results)
	}
}

func TestCheckEnvFilesEmptyValue(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	worldDir := makeWorld(t, dir, "delta")

	envPath := filepath.Join(worldDir, ".env")
	if err := os.WriteFile(envPath, []byte("FILLED=value\nEMPTY=\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	results := CheckEnvFiles()
	if len(results) == 0 {
		t.Fatal("expected at least one result for empty value warning")
	}
	found := false
	for _, r := range results {
		if !r.Passed && strings.Contains(r.Message, `"EMPTY"`) {
			found = true
			if r.Fix == "" {
				t.Error("expected non-empty Fix for empty value")
			}
		}
	}
	if !found {
		t.Errorf("expected a warning about EMPTY key, got: %v", results)
	}
}

func TestCheckEnvFilesWorldReadable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: root ignores permission bits")
	}

	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	worldDir := makeWorld(t, dir, "epsilon")

	envPath := filepath.Join(worldDir, ".env")
	// Write with world-readable permissions (0644).
	if err := os.WriteFile(envPath, []byte("SECRET=value\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckEnvFiles()
	if len(results) == 0 {
		t.Fatal("expected at least one result for world-readable .env")
	}
	found := false
	for _, r := range results {
		if !r.Passed && strings.Contains(r.Message, "world-readable") {
			found = true
			if r.Fix == "" {
				t.Error("expected non-empty Fix for world-readable file")
			}
		}
	}
	if !found {
		t.Errorf("expected a world-readable warning, got: %v", results)
	}
}
