package integration

import (
	"strings"
	"testing"
)

func TestCLIWorkflowInstantiateHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "workflow", "instantiate", "--help")
	if err != nil {
		t.Fatalf("gt workflow instantiate --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Instantiate a workflow") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIWorkflowCurrentHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "workflow", "current", "--help")
	if err != nil {
		t.Fatalf("gt workflow current --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "current step") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIWorkflowAdvanceHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "workflow", "advance", "--help")
	if err != nil {
		t.Fatalf("gt workflow advance --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Advance") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIWorkflowStatusHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "workflow", "status", "--help")
	if err != nil {
		t.Fatalf("gt workflow status --help failed: %v: %s", err, out)
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
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "caravan", "create", "--help")
	if err != nil {
		t.Fatalf("gt caravan create --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Create a caravan") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLICaravanAddHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "caravan", "add", "--help")
	if err != nil {
		t.Fatalf("gt caravan add --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Add items") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLICaravanCheckHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "caravan", "check", "--help")
	if err != nil {
		t.Fatalf("gt caravan check --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "readiness") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLICaravanStatusHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "caravan", "status", "--help")
	if err != nil {
		t.Fatalf("gt caravan status --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "caravan status") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLICaravanLaunchHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "caravan", "launch", "--help")
	if err != nil {
		t.Fatalf("gt caravan launch --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Dispatch ready items") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIStoreDepAddHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "store", "dep", "add", "--help")
	if err != nil {
		t.Fatalf("gt store dep add --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "dependency") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIStoreDepListHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "store", "dep", "list", "--help")
	if err != nil {
		t.Fatalf("gt store dep list --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "dependencies") {
		t.Errorf("output missing expected text: %s", out)
	}
}
