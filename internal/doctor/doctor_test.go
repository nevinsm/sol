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
	if len(report.Checks) != 6 {
		t.Fatalf("expected 6 checks, got %d", len(report.Checks))
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
