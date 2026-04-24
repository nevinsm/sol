package integration

// cli_behavioral_test.go — Behavioral CLI tests for commands that previously
// had only --help coverage:
//   sol caravan launch  (CF-40) — flag parsing, drydock rejection, no-ready-items
//   sol workflow manifest (CF-41) — materialization via CLI with output validation
//   sol feed (CF-42) — default rendering, --json, --type filtering
//
// These tests exercise real CLI wiring (cobra flag parsing, output formatting)
// not just --help text. They use runGT() from helpers_test.go for CLI execution
// and makeWorkflow() from workflow_tiers_test.go for workflow creation.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
)

// =============================================================================
// sol caravan launch — behavioral CLI tests (CF-40)
// =============================================================================

// TestCLICaravanLaunchInvalidID verifies that caravan launch rejects
// a malformed caravan ID with a validation error.
func TestCLICaravanLaunchInvalidID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	initWorld(t, solHome, "ember")

	out, err := runGT(t, solHome, "caravan", "launch", "not-a-valid-id", "--world=ember")
	if err == nil {
		t.Fatalf("expected error for invalid caravan ID, got success: %s", out)
	}
	if !strings.Contains(out, "invalid caravan ID") {
		t.Errorf("expected 'invalid caravan ID' error, got: %s", out)
	}
}

// TestCLICaravanLaunchMissingArg verifies that caravan launch requires
// exactly one positional argument (the caravan ID).
func TestCLICaravanLaunchMissingArg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "caravan", "launch")
	if err == nil {
		t.Fatalf("expected error for missing caravan-id arg, got success: %s", out)
	}
	if !strings.Contains(out, "accepts 1 arg") {
		t.Errorf("expected arg count error, got: %s", out)
	}
}

// TestCLICaravanLaunchDrydockReject verifies that launching a drydock
// caravan returns a specific error telling the user to commission it first.
func TestCLICaravanLaunchDrydockReject(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	initWorld(t, solHome, "ember")

	// Create a caravan (starts in drydock).
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	caravanID, err := sphereStore.CreateCaravan("drydock-test", "autarch")
	if err != nil {
		t.Fatalf("CreateCaravan: %v", err)
	}
	sphereStore.Close()

	// Attempt to launch — should fail because caravan is in drydock.
	out, err := runGT(t, solHome, "caravan", "launch", caravanID, "--world=ember")
	if err == nil {
		t.Fatalf("expected error for drydock caravan launch, got success: %s", out)
	}
	if !strings.Contains(out, "drydock") {
		t.Errorf("expected drydock-related error, got: %s", out)
	}
	if !strings.Contains(out, "commission") {
		t.Errorf("expected hint to commission, got: %s", out)
	}
}

// TestCLICaravanLaunchNoReadyItems verifies that launching a commissioned
// caravan with no ready items prints an informative message instead of failing.
func TestCLICaravanLaunchNoReadyItems(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	initWorld(t, solHome, "ember")

	// Create and commission an empty caravan.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	caravanID, err := sphereStore.CreateCaravan("empty-launch", "autarch")
	if err != nil {
		t.Fatalf("CreateCaravan: %v", err)
	}
	if err := sphereStore.UpdateCaravanStatus(caravanID, "open"); err != nil {
		t.Fatalf("commission caravan: %v", err)
	}
	sphereStore.Close()

	// Launch — should succeed with "no ready items" message.
	out, err := runGT(t, solHome, "caravan", "launch", caravanID, "--world=ember")
	if err != nil {
		t.Fatalf("caravan launch failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "No ready items") {
		t.Errorf("expected 'No ready items' message, got: %s", out)
	}
}

// TestCLICaravanLaunchNoReadyItemsJSON verifies that --json output
// returns valid JSON even when no items are ready for dispatch.
func TestCLICaravanLaunchNoReadyItemsJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	initWorld(t, solHome, "ember")

	// Create and commission an empty caravan.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	caravanID, err := sphereStore.CreateCaravan("json-launch", "autarch")
	if err != nil {
		t.Fatalf("CreateCaravan: %v", err)
	}
	if err := sphereStore.UpdateCaravanStatus(caravanID, "open"); err != nil {
		t.Fatalf("commission caravan: %v", err)
	}
	sphereStore.Close()

	out, err := runGT(t, solHome, "caravan", "launch", caravanID, "--world=ember", "--json")
	if err != nil {
		t.Fatalf("caravan launch --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("caravan launch --json output is not valid JSON: %s", out)
	}

	// Verify JSON structure.
	var resp struct {
		CaravanID  string `json:"caravan_id"`
		World      string `json:"world"`
		Dispatched []any  `json:"dispatched"`
		Blocked    int    `json:"blocked"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal launch response: %v", err)
	}
	if resp.CaravanID != caravanID {
		t.Errorf("caravan_id: got %q, want %q", resp.CaravanID, caravanID)
	}
	if resp.World != "ember" {
		t.Errorf("world: got %q, want ember", resp.World)
	}
	if len(resp.Dispatched) != 0 {
		t.Errorf("dispatched: got %d items, want 0", len(resp.Dispatched))
	}
}

// =============================================================================
// sol workflow manifest — behavioral CLI tests (CF-41)
// =============================================================================

// makeManifestWorkflow creates a minimal workflow directory configured for
// manifestation (mode = "manifest") with a required "issue" variable.
func makeManifestWorkflow(t *testing.T, dir, name, description string) {
	t.Helper()
	workflowDir := filepath.Join(dir, name)
	stepsDir := filepath.Join(workflowDir, "steps")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("create workflow dir %s: %v", name, err)
	}
	manifest := `name = "` + name + `"
type = "workflow"
mode = "manifest"
description = "` + description + `"

[variables]
[variables.issue]
required = true

[[steps]]
id = "only"
title = "Only Step"
instructions = "steps/01.md"
`
	if err := os.WriteFile(filepath.Join(workflowDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest for %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "01.md"), []byte("Instructions for "+name+" ("+description+"): {{issue}}\n"), 0o644); err != nil {
		t.Fatalf("write step for %s: %v", name, err)
	}
}

// TestCLIWorkflowManifestBasic verifies that `sol workflow manifest <name>`
// creates writs and a caravan, outputting caravan ID and step details.
func TestCLIWorkflowManifestBasic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	initWorld(t, solHome, "ember")

	// Create a simple user-tier workflow configured for manifestation.
	makeManifestWorkflow(t, filepath.Join(solHome, "workflows"), "test-manifest", "test manifest workflow")

	// Manifest the workflow.
	out, err := runGT(t, solHome, "workflow", "manifest", "test-manifest",
		"--world=ember", "--var=issue=TEST-123")
	if err != nil {
		t.Fatalf("sol workflow manifest failed: %v: %s", err, out)
	}

	// Verify human-readable output contains expected fields.
	if !strings.Contains(out, "Caravan:") {
		t.Errorf("output missing 'Caravan:' line: %s", out)
	}
	if !strings.Contains(out, "car-") {
		t.Errorf("output missing caravan ID: %s", out)
	}
	if !strings.Contains(out, "Items:") {
		t.Errorf("output missing 'Items:' line: %s", out)
	}
	// Our simple workflow has a single step "only", so phase info should appear.
	if !strings.Contains(out, "phase") {
		t.Errorf("output missing phase information: %s", out)
	}
	if !strings.Contains(out, "only") {
		t.Errorf("output missing step ID 'only': %s", out)
	}
}

// TestCLIWorkflowManifestJSON verifies that --json output returns
// valid JSON with the expected caravan structure.
func TestCLIWorkflowManifestJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	initWorld(t, solHome, "ember")

	makeManifestWorkflow(t, filepath.Join(solHome, "workflows"), "json-manifest", "json test workflow")

	out, err := runGT(t, solHome, "workflow", "manifest", "json-manifest",
		"--world=ember", "--var=issue=TEST-456", "--json")
	if err != nil {
		t.Fatalf("sol workflow manifest --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("workflow manifest --json output is not valid JSON: %s", out)
	}

	// Verify JSON has a caravan ID and item counts.
	var resp struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		ItemsTotal int    `json:"items_total"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal manifest response: %v", err)
	}
	if !strings.HasPrefix(resp.ID, "car-") {
		t.Errorf("caravan ID format invalid: %q", resp.ID)
	}
	if resp.Status == "" {
		t.Errorf("caravan status should not be empty")
	}
	if resp.ItemsTotal == 0 {
		t.Errorf("expected at least one item in caravan (items_total=0)")
	}
}

// TestCLIWorkflowManifestMissingWorkflow verifies that manifesting a
// nonexistent workflow returns an error.
func TestCLIWorkflowManifestMissingWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	initWorld(t, solHome, "ember")

	out, err := runGT(t, solHome, "workflow", "manifest", "nonexistent-workflow",
		"--world=ember")
	if err == nil {
		t.Fatalf("expected error for nonexistent workflow, got success: %s", out)
	}
	// Should mention the workflow name or indicate not found.
	if !strings.Contains(out, "nonexistent-workflow") && !strings.Contains(out, "not found") {
		t.Errorf("error should reference the workflow name or indicate not found: %s", out)
	}
}

// TestCLIWorkflowManifestMissingRequiredVar verifies that manifesting a
// workflow without a required variable returns an error.
func TestCLIWorkflowManifestMissingRequiredVar(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	initWorld(t, solHome, "ember")

	// makeManifestWorkflow creates a workflow with a required "issue" variable.
	makeManifestWorkflow(t, filepath.Join(solHome, "workflows"), "var-test", "variable test")

	// Manifest without providing the required --var=issue=...
	out, err := runGT(t, solHome, "workflow", "manifest", "var-test", "--world=ember")
	if err == nil {
		t.Fatalf("expected error for missing required variable, got success: %s", out)
	}
	if !strings.Contains(out, "issue") {
		t.Errorf("error should mention the missing variable 'issue': %s", out)
	}
}

// =============================================================================
// sol feed — behavioral CLI tests (CF-42)
// =============================================================================

// seedEvents writes a set of test events into the raw events file so the
// feed command has data to display.
func seedEvents(t *testing.T, solHome string) {
	t.Helper()
	logger := events.NewLogger(solHome)

	// Emit a variety of event types for testing rendering and filtering.
	logger.Emit(events.EventCast, "sol", "autarch", "both",
		map[string]string{"writ_id": "sol-1111111111111111", "agent": "Alpha", "world": "ember"})
	logger.Emit(events.EventResolve, "sol", "Alpha", "both",
		map[string]string{"writ_id": "sol-1111111111111111"})
	logger.Emit(events.EventCast, "sol", "autarch", "both",
		map[string]string{"writ_id": "sol-2222222222222222", "agent": "Beta", "world": "ember"})
}

// TestCLIFeedDefaultRendering verifies that `sol feed` renders events
// in human-readable format with timestamp, type, and actor columns.
func TestCLIFeedDefaultRendering(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	seedEvents(t, solHome)

	out, err := runGT(t, solHome, "feed", "--raw")
	if err != nil {
		t.Fatalf("sol feed failed: %v: %s", err, out)
	}

	// Verify human-readable format: [HH:MM:SS] type actor description
	if !strings.Contains(out, "cast") {
		t.Errorf("feed output missing 'cast' event type: %s", out)
	}
	if !strings.Contains(out, "resolve") {
		t.Errorf("feed output missing 'resolve' event type: %s", out)
	}
	if !strings.Contains(out, "autarch") {
		t.Errorf("feed output missing actor 'autarch': %s", out)
	}
	if !strings.Contains(out, "Alpha") {
		t.Errorf("feed output missing actor or agent 'Alpha': %s", out)
	}

	// Verify the formatted description includes dispatch details.
	if !strings.Contains(out, "Dispatched") {
		t.Errorf("feed output missing 'Dispatched' description: %s", out)
	}
	if !strings.Contains(out, "Completed") {
		t.Errorf("feed output missing 'Completed' description: %s", out)
	}
}

// TestCLIFeedJSON verifies that `sol feed --json` outputs valid JSONL
// with the expected field structure.
func TestCLIFeedJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	seedEvents(t, solHome)

	out, err := runGT(t, solHome, "feed", "--json", "--raw")
	if err != nil {
		t.Fatalf("sol feed --json failed: %v: %s", err, out)
	}

	// Each line should be valid JSON.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 {
		t.Fatal("sol feed --json produced no output")
	}

	for i, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Errorf("line %d is not valid JSON: %s", i, line)
			continue
		}
		var ev struct {
			OccurredAt string `json:"occurred_at"`
			Source     string `json:"source"`
			Type       string `json:"type"`
			Actor      string `json:"actor"`
			Visibility string `json:"visibility"`
			Payload    any    `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Errorf("line %d unmarshal error: %v", i, err)
			continue
		}
		if ev.Type == "" {
			t.Errorf("line %d missing event type", i)
		}
		if ev.Source == "" {
			t.Errorf("line %d missing source", i)
		}
	}
}

// TestCLIFeedTypeFilter verifies that `sol feed --type=<type>` only
// shows events of the specified type.
func TestCLIFeedTypeFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	seedEvents(t, solHome) // 2 cast events + 1 resolve event

	// Filter to resolve events only.
	out, err := runGT(t, solHome, "feed", "--type=resolve", "--raw")
	if err != nil {
		t.Fatalf("sol feed --type=resolve failed: %v: %s", err, out)
	}

	if !strings.Contains(out, "resolve") {
		t.Errorf("filtered output should contain 'resolve': %s", out)
	}
	// The output should NOT contain the "cast" event type as a column value.
	// Lines have format: [HH:MM:SS] type         actor        description
	// We check that no line starts with a cast event type in the type column.
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		// In human format, the type field appears after the timestamp bracket.
		// e.g., [12:34:56] cast         autarch      Dispatched ...
		if strings.Contains(line, "] cast") {
			t.Errorf("filtered output should not contain cast events: %s", line)
		}
	}
}

// TestCLIFeedTypeFilterJSON verifies --type filtering works with --json output.
func TestCLIFeedTypeFilterJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	seedEvents(t, solHome)

	out, err := runGT(t, solHome, "feed", "--type=cast", "--json", "--raw")
	if err != nil {
		t.Fatalf("sol feed --type=cast --json failed: %v: %s", err, out)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 cast events, got %d lines: %s", len(lines), out)
	}

	for _, line := range lines {
		var ev struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Errorf("unmarshal error: %v", err)
			continue
		}
		if ev.Type != "cast" {
			t.Errorf("expected type 'cast', got %q", ev.Type)
		}
	}
}

// TestCLIFeedLimit verifies that `sol feed -n <limit>` caps the number
// of events returned.
func TestCLIFeedLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	seedEvents(t, solHome) // 3 events total

	// Request only the last 1 event.
	out, err := runGT(t, solHome, "feed", "-n", "1", "--json", "--raw")
	if err != nil {
		t.Fatalf("sol feed -n 1 --json failed: %v: %s", err, out)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 event with -n 1, got %d: %s", len(lines), out)
	}
}

// TestCLIFeedEmptyFeed verifies that `sol feed` handles the case where
// no events exist gracefully (no error, empty output).
func TestCLIFeedEmptyFeed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// No events seeded — the file doesn't exist.
	out, err := runGT(t, solHome, "feed", "--raw")
	if err != nil {
		t.Fatalf("sol feed on empty feed failed: %v: %s", err, out)
	}
	// Output should be empty or whitespace-only.
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected empty output for empty feed, got: %s", out)
	}
}
