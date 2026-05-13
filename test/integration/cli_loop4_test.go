package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- Caravan CLI smoke tests ---

func TestCLICaravanCreateHelp(t *testing.T) {
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "writ", "dep", "list", "--help")
	if err != nil {
		t.Fatalf("sol writ dep list --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "dependencies") {
		t.Errorf("output missing expected text: %s", out)
	}
}

// --- Writ-dep behavioral CLI tests (M-6) ---
// These complement the caravan-dep tests in cli_loop6_test.go, providing
// equivalent end-to-end coverage for the writ-specific dep variants.

// TestCLIWritDepBehavioral exercises the full `sol writ dep add/list/remove`
// flow with real store interaction and output verification.
func TestCLIWritDepBehavioral(t *testing.T) {
	skipUnlessIntegration(t)

	solHome, _ := setupTestEnv(t)
	initWorld(t, solHome, "deptest")

	// Create two writs via CLI.
	outA, err := runGT(t, solHome, "writ", "create", "--world=deptest", "--title=Task A")
	if err != nil {
		t.Fatalf("writ create A: %v: %s", err, outA)
	}
	writA := extractWritID(t, outA)

	outB, err := runGT(t, solHome, "writ", "create", "--world=deptest", "--title=Task B")
	if err != nil {
		t.Fatalf("writ create B: %v: %s", err, outB)
	}
	writB := extractWritID(t, outB)

	// --- dep add: B depends on A ---
	out, err := runGT(t, solHome, "writ", "dep", "add", writB, writA, "--world=deptest")
	if err != nil {
		t.Fatalf("writ dep add: %v: %s", err, out)
	}
	if !strings.Contains(out, "Added dependency") {
		t.Errorf("expected 'Added dependency' in output, got: %s", out)
	}
	if !strings.Contains(out, writB) || !strings.Contains(out, writA) {
		t.Errorf("expected both writ IDs in dep add output, got: %s", out)
	}

	// --- dep list (text): B depends on A ---
	out, err = runGT(t, solHome, "writ", "dep", "list", writB, "--world=deptest")
	if err != nil {
		t.Fatalf("writ dep list: %v: %s", err, out)
	}
	if !strings.Contains(out, "Depends on:") {
		t.Errorf("expected 'Depends on:' section, got: %s", out)
	}
	if !strings.Contains(out, writA) {
		t.Errorf("expected dependency %s in list output, got: %s", writA, out)
	}

	// --- dep list (JSON): B depends on A ---
	out, err = runGT(t, solHome, "writ", "dep", "list", writB, "--world=deptest", "--json")
	if err != nil {
		t.Fatalf("writ dep list --json: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("writ dep list --json output is not valid JSON: %s", out)
	}
	// DepListResponse: {writ_id, depends_on: []string, depended_by: []string}
	var depResp struct {
		WritID     string   `json:"writ_id"`
		DependsOn  []string `json:"depends_on"`
		DependedBy []string `json:"depended_by"`
	}
	if err := json.Unmarshal([]byte(out), &depResp); err != nil {
		t.Fatalf("unmarshal dep list JSON: %v: %s", err, out)
	}
	if depResp.WritID != writB {
		t.Errorf("dep list JSON writ_id = %q, want %q", depResp.WritID, writB)
	}
	if len(depResp.DependsOn) != 1 || depResp.DependsOn[0] != writA {
		t.Errorf("expected dependency %s in JSON, got: %+v", writA, depResp.DependsOn)
	}

	// Verify A sees B as a dependent via its dep list.
	out, err = runGT(t, solHome, "writ", "dep", "list", writA, "--world=deptest", "--json")
	if err != nil {
		t.Fatalf("writ dep list A --json: %v: %s", err, out)
	}
	var aResp struct {
		DependedBy []string `json:"depended_by"`
	}
	if err := json.Unmarshal([]byte(out), &aResp); err != nil {
		t.Fatalf("unmarshal A dep list JSON: %v: %s", err, out)
	}
	if len(aResp.DependedBy) != 1 || aResp.DependedBy[0] != writB {
		t.Errorf("expected B (%s) in A's depended_by list, got: %+v", writB, aResp.DependedBy)
	}

	// --- dep remove: B no longer depends on A ---
	out, err = runGT(t, solHome, "writ", "dep", "remove", writB, writA, "--world=deptest")
	if err != nil {
		t.Fatalf("writ dep remove: %v: %s", err, out)
	}
	if !strings.Contains(out, "Removed dependency") {
		t.Errorf("expected 'Removed dependency' in output, got: %s", out)
	}

	// After removal, dep list should show "(none)".
	out, err = runGT(t, solHome, "writ", "dep", "list", writB, "--world=deptest")
	if err != nil {
		t.Fatalf("writ dep list after remove: %v: %s", err, out)
	}
	if !strings.Contains(out, "(none)") {
		t.Errorf("expected '(none)' after dependency removal, got: %s", out)
	}
}
