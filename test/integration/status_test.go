package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusSphereOverview(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	// Init two worlds.
	initWorld(t, gtHome, "alpha")
	initWorld(t, gtHome, "beta")

	out, err := runGT(t, gtHome, "status")
	if err != nil {
		t.Fatalf("sol status failed: %v: %s", err, out)
	}

	if !strings.Contains(out, "Sol Sphere") {
		t.Errorf("output missing 'Sol Sphere': %s", out)
	}
	if !strings.Contains(out, "alpha") {
		t.Errorf("output missing world 'alpha': %s", out)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("output missing world 'beta': %s", out)
	}
	if !strings.Contains(out, "Processes") {
		t.Errorf("output missing 'Processes' section: %s", out)
	}
}

func TestStatusSphereJSON(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	initWorld(t, gtHome, "myworld")

	out, err := runGT(t, gtHome, "status", "--json")
	if err != nil {
		t.Fatalf("sol status --json failed: %v: %s", err, out)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v: %s", err, out)
	}

	if _, ok := result["sol_home"]; !ok {
		t.Errorf("JSON missing 'sol_home' field: %s", out)
	}
	if _, ok := result["worlds"]; !ok {
		t.Errorf("JSON missing 'worlds' field: %s", out)
	}
	if _, ok := result["health"]; !ok {
		t.Errorf("JSON missing 'health' field: %s", out)
	}
}

func TestStatusSphereEmpty(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	out, err := runGT(t, gtHome, "status")
	if err != nil {
		t.Fatalf("sol status failed: %v: %s", err, out)
	}

	if !strings.Contains(out, "No worlds initialized") {
		t.Errorf("expected 'No worlds initialized' message: %s", out)
	}
}

func TestStatusWorldDetail(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	initWorld(t, gtHome, "myworld")

	// Create a writ.
	_, err := runGT(t, gtHome, "writ", "create", "--world=myworld", "--title=Test item")
	if err != nil {
		t.Fatalf("writ create failed: %v", err)
	}

	// Text mode may exit non-zero for degraded/unhealthy worlds (same as --json).
	// Since prefect is not running in tests, this will be degraded (exit 2).
	out, _ := runGT(t, gtHome, "status", "myworld")

	if !strings.Contains(out, "myworld") {
		t.Errorf("output missing world name: %s", out)
	}
	if !strings.Contains(out, "Processes") {
		t.Errorf("output missing 'Processes' section: %s", out)
	}
	if !strings.Contains(out, "Merge Queue") {
		t.Errorf("output missing 'Merge Queue' section: %s", out)
	}

	// Regression: output must not be duplicated.
	if strings.Count(out, "Merge Queue") > 1 {
		t.Errorf("status output is duplicated:\n%s", out)
	}
}

func TestStatusWorldJSON(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	initWorld(t, gtHome, "myworld")

	// Note: exit code may be non-zero (degraded) since prefect is not running.
	out, _ := runGT(t, gtHome, "status", "myworld", "--json")

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v: %s", err, out)
	}

	if result["world"] != "myworld" {
		t.Errorf("expected world 'myworld', got %v", result["world"])
	}
}

func TestStatusWorldNotFound(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	out, err := runGT(t, gtHome, "status", "nonexistent")
	if err == nil {
		t.Fatalf("expected error for nonexistent world, got success: %s", out)
	}
	if !strings.Contains(out, "does not exist") {
		t.Errorf("expected 'does not exist' error: %s", out)
	}
}

func TestWorldStatusStillWorks(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	initWorld(t, gtHome, "myworld")

	out, err := runGT(t, gtHome, "world", "status", "myworld")
	if err != nil {
		t.Fatalf("sol world status myworld failed: %v: %s", err, out)
	}

	if !strings.Contains(out, "Config") {
		t.Errorf("output missing 'Config' section: %s", out)
	}
	if !strings.Contains(out, "Source repo") {
		t.Errorf("output missing 'Source repo': %s", out)
	}
	if !strings.Contains(out, "Processes") {
		t.Errorf("output missing 'Processes' section: %s", out)
	}
}
