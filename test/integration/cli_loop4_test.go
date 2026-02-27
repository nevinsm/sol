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

// --- Convoy CLI smoke tests ---

func TestCLIConvoyCreateHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "convoy", "create", "--help")
	if err != nil {
		t.Fatalf("gt convoy create --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Create a convoy") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIConvoyAddHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "convoy", "add", "--help")
	if err != nil {
		t.Fatalf("gt convoy add --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Add items") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIConvoyCheckHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "convoy", "check", "--help")
	if err != nil {
		t.Fatalf("gt convoy check --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "readiness") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIConvoyStatusHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "convoy", "status", "--help")
	if err != nil {
		t.Fatalf("gt convoy status --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "convoy status") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIConvoyLaunchHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "convoy", "launch", "--help")
	if err != nil {
		t.Fatalf("gt convoy launch --help failed: %v: %s", err, out)
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
