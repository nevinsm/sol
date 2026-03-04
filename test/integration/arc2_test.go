package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Arc 2 end-to-end integration tests — cross-feature flows that verify
// the operator onboarding experience as a cohesive whole.

func TestDoctorEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "doctor")
	if err != nil {
		t.Fatalf("sol doctor failed: %v: %s", err, out)
	}

	// Verify expected check markers.
	if !strings.Contains(out, "✓ tmux") {
		t.Errorf("output missing '✓ tmux': %s", out)
	}
	if !strings.Contains(out, "✓ git") {
		t.Errorf("output missing '✓ git': %s", out)
	}
	if !strings.Contains(out, "✓ sqlite_wal") {
		t.Errorf("output missing '✓ sqlite_wal': %s", out)
	}
	if !strings.Contains(out, "All checks passed") {
		t.Errorf("output missing 'All checks passed': %s", out)
	}
}

func TestDoctorJSONEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "doctor", "--json")
	_ = err // exit code 1 is acceptable if claude is missing

	var report struct {
		Checks []struct {
			Name    string `json:"name"`
			Passed  bool   `json:"passed"`
			Message string `json:"message"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("failed to parse doctor --json: %v\noutput: %s", err, out)
	}
	if len(report.Checks) == 0 {
		t.Fatal("doctor --json returned no checks")
	}

	// Verify each check has required fields and expected checks are present.
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
	for _, expected := range []string{"tmux", "git", "sqlite_wal"} {
		if !names[expected] {
			t.Errorf("missing check %q in JSON output", expected)
		}
	}

	// Verify tmux, git, sqlite_wal all passed.
	for _, c := range report.Checks {
		switch c.Name {
		case "tmux", "git", "sqlite_wal":
			if !c.Passed {
				t.Errorf("check %q should have passed: %s", c.Name, c.Message)
			}
		}
	}
}

func TestDoctorBeforeSOLHome(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	nonExistent := filepath.Join(t.TempDir(), "not-created-yet")

	out, err := runGT(t, nonExistent, "doctor")
	_ = err // exit code 1 is acceptable if some checks fail
	_ = out

	// Verify SOL_HOME was NOT created as a side effect.
	if _, err := os.Stat(nonExistent); !os.IsNotExist(err) {
		t.Errorf("SOL_HOME %q should not exist after doctor, but it does", nonExistent)
	}
}

func TestInitFlagModeEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := filepath.Join(t.TempDir(), "sol-init-test")

	out, err := runGT(t, solHome, "init", "--name=myworld", "--skip-checks")
	if err != nil {
		t.Fatalf("sol init failed: %v: %s", err, out)
	}

	if !strings.Contains(out, "sol initialized successfully") {
		t.Errorf("expected success message: %s", out)
	}

	// Verify directory structure.
	for _, check := range []struct {
		path string
		desc string
	}{
		{solHome, "SOL_HOME"},
		{filepath.Join(solHome, ".store"), ".store/"},
		{filepath.Join(solHome, ".runtime"), ".runtime/"},
		{filepath.Join(solHome, "myworld", "world.toml"), "world.toml"},
		{filepath.Join(solHome, ".store", "myworld.db"), "myworld.db"},
		{filepath.Join(solHome, "myworld", "outposts"), "outposts/"},
	} {
		if _, err := os.Stat(check.path); os.IsNotExist(err) {
			t.Errorf("%s not created: %s", check.desc, check.path)
		}
	}
}

func TestInitWithSourceRepoEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := filepath.Join(t.TempDir(), "sol-init-test")

	// Create a real git repo as source.
	sourceRepo := t.TempDir()
	gitRun(t, sourceRepo, "init")
	gitRun(t, sourceRepo, "config", "user.email", "test@test.com")
	gitRun(t, sourceRepo, "config", "user.name", "Test")
	gitRun(t, sourceRepo, "commit", "--allow-empty", "-m", "init")

	out, err := runGT(t, solHome, "init", "--name=myworld", "--source-repo="+sourceRepo, "--skip-checks")
	if err != nil {
		t.Fatalf("sol init failed: %v: %s", err, out)
	}

	// Verify world.toml contains source_repo.
	tomlPath := filepath.Join(solHome, "myworld", "world.toml")
	data, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if !strings.Contains(string(data), sourceRepo) {
		t.Errorf("world.toml does not contain source_repo %q: %s", sourceRepo, data)
	}

	// Verify managed clone exists.
	repoDir := filepath.Join(solHome, "myworld", "repo")
	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		t.Fatal("managed clone not created")
	}
}

func TestInitAlreadyInitializedEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := filepath.Join(t.TempDir(), "sol-init-test")

	// First run — success.
	out, err := runGT(t, solHome, "init", "--name=myworld", "--skip-checks")
	if err != nil {
		t.Fatalf("first init failed: %v: %s", err, out)
	}

	// Second run — error.
	out, err = runGT(t, solHome, "init", "--name=myworld", "--skip-checks")
	if err == nil {
		t.Fatalf("expected error on second init, got success: %s", out)
	}
	if !strings.Contains(out, "already initialized") {
		t.Errorf("expected 'already initialized' error, got: %s", out)
	}
}

func TestInitThenFullFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := filepath.Join(t.TempDir(), "sol-flow-test")

	// Init.
	out, err := runGT(t, solHome, "init", "--name=myworld", "--skip-checks")
	if err != nil {
		t.Fatalf("init failed: %v: %s", err, out)
	}

	// Create a work item.
	out, err = runGT(t, solHome, "store", "create", "--world=myworld", "--title=First task")
	if err != nil {
		t.Fatalf("store create failed: %v: %s", err, out)
	}

	// World list shows the world.
	out, err = runGT(t, solHome, "world", "list")
	if err != nil {
		t.Fatalf("world list failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "myworld") {
		t.Errorf("world list missing 'myworld': %s", out)
	}

	// Status world.
	out, _ = runGT(t, solHome, "status", "--world=myworld")
	if !strings.Contains(out, "myworld") {
		t.Errorf("status myworld missing world name: %s", out)
	}

	// Sphere overview.
	out, err = runGT(t, solHome, "status")
	if err != nil {
		t.Fatalf("sphere status failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "myworld") {
		t.Errorf("sphere status missing 'myworld': %s", out)
	}

	// World status (legacy command).
	out, err = runGT(t, solHome, "world", "status", "--world=myworld")
	if err != nil {
		t.Fatalf("world status failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Config") {
		t.Errorf("world status missing 'Config': %s", out)
	}
}

func TestStatusSphereEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := filepath.Join(t.TempDir(), "sol-status-test")

	// Init via sol init.
	out, err := runGT(t, solHome, "init", "--name=myworld", "--skip-checks")
	if err != nil {
		t.Fatalf("init failed: %v: %s", err, out)
	}

	out, err = runGT(t, solHome, "status")
	if err != nil {
		t.Fatalf("sol status failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Sol Sphere") {
		t.Errorf("output missing 'Sol Sphere': %s", out)
	}
	if !strings.Contains(out, "myworld") {
		t.Errorf("output missing world name: %s", out)
	}
	if !strings.Contains(out, "Processes") {
		t.Errorf("output missing 'Processes': %s", out)
	}
}

func TestStatusSphereJSONEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := filepath.Join(t.TempDir(), "sol-status-test")

	out, err := runGT(t, solHome, "init", "--name=myworld", "--skip-checks")
	if err != nil {
		t.Fatalf("init failed: %v: %s", err, out)
	}

	out, err = runGT(t, solHome, "status", "--json")
	if err != nil {
		t.Fatalf("sol status --json failed: %v: %s", err, out)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v: %s", err, out)
	}
	if _, ok := result["sol_home"]; !ok {
		t.Errorf("JSON missing 'sol_home': %s", out)
	}
	worlds, ok := result["worlds"]
	if !ok {
		t.Fatalf("JSON missing 'worlds': %s", out)
	}
	worldsArr, ok := worlds.([]interface{})
	if !ok {
		t.Fatalf("'worlds' is not an array: %T", worlds)
	}
	if len(worldsArr) != 1 {
		t.Errorf("expected 1 world, got %d", len(worldsArr))
	}
	if _, ok := result["health"]; !ok {
		t.Errorf("JSON missing 'health': %s", out)
	}
}

func TestStatusSphereMultipleWorlds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := filepath.Join(t.TempDir(), "sol-status-test")

	// Init first world via sol init.
	out, err := runGT(t, solHome, "init", "--name=alpha", "--skip-checks")
	if err != nil {
		t.Fatalf("init alpha failed: %v: %s", err, out)
	}

	// Init second world via sol world init.
	out, err = runGT(t, solHome, "world", "init", "beta")
	if err != nil {
		t.Fatalf("world init beta failed: %v: %s", err, out)
	}

	out, err = runGT(t, solHome, "status")
	if err != nil {
		t.Fatalf("sol status failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "alpha") {
		t.Errorf("output missing world 'alpha': %s", out)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("output missing world 'beta': %s", out)
	}
}

func TestStatusWorldWithLipgloss(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := filepath.Join(t.TempDir(), "sol-status-test")

	out, err := runGT(t, solHome, "init", "--name=myworld", "--skip-checks")
	if err != nil {
		t.Fatalf("init failed: %v: %s", err, out)
	}

	// Create a work item.
	_, err = runGT(t, solHome, "store", "create", "--world=myworld", "--title=Test item")
	if err != nil {
		t.Fatalf("store create failed: %v", err)
	}

	// Note: exit code may be non-zero (degraded) since prefect is not running.
	out, _ = runGT(t, solHome, "status", "--world=myworld")

	if !strings.Contains(out, "Processes") {
		t.Errorf("output missing 'Processes': %s", out)
	}
	if !strings.Contains(out, "Merge Queue") {
		t.Errorf("output missing 'Merge Queue': %s", out)
	}
}

func TestStatusWorldJSONUnchanged(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := filepath.Join(t.TempDir(), "sol-status-test")

	out, err := runGT(t, solHome, "init", "--name=myworld", "--skip-checks")
	if err != nil {
		t.Fatalf("init failed: %v: %s", err, out)
	}

	out, _ = runGT(t, solHome, "status", "--world=myworld", "--json")

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v: %s", err, out)
	}

	// Verify backward-compatible fields from pre-Arc-2 format.
	if result["world"] != "myworld" {
		t.Errorf("expected world 'myworld', got %v", result["world"])
	}
	for _, field := range []string{"world", "prefect", "forge", "agents", "merge_queue", "summary"} {
		if _, ok := result[field]; !ok {
			t.Errorf("JSON missing backward-compatible field %q", field)
		}
	}
}

func TestStatusEmptySphere(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()
	os.MkdirAll(filepath.Join(solHome, ".store"), 0o755)

	out, err := runGT(t, solHome, "status")
	if err != nil {
		t.Fatalf("sol status failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "No worlds initialized") {
		t.Errorf("expected 'No worlds initialized': %s", out)
	}
}

// --- Cross-Feature Tests ---

func TestDoctorThenInit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := filepath.Join(t.TempDir(), "sol-flow-test")

	// Step 1: Doctor passes.
	out, err := runGT(t, solHome, "doctor")
	if err != nil {
		t.Fatalf("sol doctor failed: %v: %s", err, out)
	}

	// Step 2: Init succeeds.
	out, err = runGT(t, solHome, "init", "--name=myworld", "--skip-checks")
	if err != nil {
		t.Fatalf("sol init failed: %v: %s", err, out)
	}

	// Step 3: Status shows the world.
	out, err = runGT(t, solHome, "status")
	if err != nil {
		t.Fatalf("sol status failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "myworld") {
		t.Errorf("status output missing 'myworld': %s", out)
	}
}

func TestInitRunsDoctorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := filepath.Join(t.TempDir(), "sol-flow-test")

	// Run: sol init --name=myworld (no --skip-checks)
	out, err := runGT(t, solHome, "init", "--name=myworld")
	if err != nil {
		// Doctor failed — error should include check details.
		if !strings.Contains(out, "prerequisite check(s) failed") {
			t.Errorf("expected 'prerequisite check(s) failed' in error, got: %s", out)
		}
	} else {
		// Doctor passed — setup should succeed.
		if !strings.Contains(out, "sol initialized successfully") {
			t.Errorf("expected success message, got: %s", out)
		}
	}
}
