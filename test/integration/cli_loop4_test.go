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
