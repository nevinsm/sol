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
//
// Single-feature tests for doctor, init, etc. live in their own files
// (doctor_test.go, init_test.go). This file focuses on cross-feature
// flows and status rendering.

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

	// Create a writ.
	out, err = runGT(t, solHome, "writ", "create", "--world=myworld", "--title=First task")
	if err != nil {
		t.Fatalf("writ create failed: %v: %s", err, out)
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
	out, _ = runGT(t, solHome, "status", "myworld")
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
	out, err = runGT(t, solHome, "world", "status", "myworld")
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

	// Create a writ.
	_, err = runGT(t, solHome, "writ", "create", "--world=myworld", "--title=Test item")
	if err != nil {
		t.Fatalf("writ create failed: %v", err)
	}

	// Note: exit code may be non-zero (degraded) since prefect is not running.
	out, _ = runGT(t, solHome, "status", "myworld")

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

	out, _ = runGT(t, solHome, "status", "myworld", "--json")

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

