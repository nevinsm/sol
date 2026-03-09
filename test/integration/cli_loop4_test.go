package integration

import (
	"strings"
	"testing"
)

func TestCLIWorkflowInstantiateHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "workflow", "instantiate", "--help")
	if err != nil {
		t.Fatalf("sol workflow instantiate --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Instantiate a workflow") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIWorkflowCurrentHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "workflow", "current", "--help")
	if err != nil {
		t.Fatalf("sol workflow current --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "current step") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIWorkflowAdvanceHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "workflow", "advance", "--help")
	if err != nil {
		t.Fatalf("sol workflow advance --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Advance") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIWorkflowStatusHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "workflow", "status", "--help")
	if err != nil {
		t.Fatalf("sol workflow status --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "workflow status") {
		t.Errorf("output missing expected text: %s", out)
	}
}

// --- Caravan CLI smoke tests ---

func TestCLICaravanCreateHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "caravan", "create", "--help")
	if err != nil {
		t.Fatalf("sol caravan create --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Create a caravan") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLICaravanAddHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "caravan", "add", "--help")
	if err != nil {
		t.Fatalf("sol caravan add --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Add items") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLICaravanCheckHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "caravan", "check", "--help")
	if err != nil {
		t.Fatalf("sol caravan check --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "readiness") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLICaravanStatusHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "caravan", "status", "--help")
	if err != nil {
		t.Fatalf("sol caravan status --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "caravan status") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLICaravanLaunchHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "caravan", "launch", "--help")
	if err != nil {
		t.Fatalf("sol caravan launch --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Check readiness of all items") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIWritDepAddHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "writ", "dep", "add", "--help")
	if err != nil {
		t.Fatalf("sol writ dep add --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "dependency") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIWritDepListHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "writ", "dep", "list", "--help")
	if err != nil {
		t.Fatalf("sol writ dep list --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "dependencies") {
		t.Errorf("output missing expected text: %s", out)
	}
}
