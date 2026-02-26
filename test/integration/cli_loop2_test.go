package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/session"
)

func TestCLIRefineryStartHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "refinery", "start", "--help")
	if err != nil {
		t.Fatalf("gt refinery start --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Start the refinery") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIRefineryStopHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "refinery", "stop", "--help")
	if err != nil {
		t.Fatalf("gt refinery stop --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Stop the refinery") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIRefineryQueueHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "refinery", "queue", "--help")
	if err != nil {
		t.Fatalf("gt refinery queue --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "merge request queue") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIRefineryAttachHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "refinery", "attach", "--help")
	if err != nil {
		t.Fatalf("gt refinery attach --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Attach to the refinery") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIRefineryQueue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	rigStore, _ := openStores(t, "testrig")

	// Create a work item and a merge request.
	itemID, err := rigStore.CreateWorkItem("Test merge item", "desc", "test", 1, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	branch := "polecat/Toast/" + itemID
	mrID, err := rigStore.CreateMergeRequest(itemID, branch, 1)
	if err != nil {
		t.Fatalf("create merge request: %v", err)
	}
	_ = sourceRepo // needed by setupTestEnv but not directly used here

	// Human-readable output.
	out, err := runGT(t, gtHome, "refinery", "queue", "testrig")
	if err != nil {
		t.Fatalf("gt refinery queue failed: %v: %s", err, out)
	}
	if !strings.Contains(out, mrID) {
		t.Errorf("queue output missing MR ID %q: %s", mrID, out)
	}
	if !strings.Contains(out, itemID) {
		t.Errorf("queue output missing work item ID %q: %s", itemID, out)
	}

	// JSON output.
	out, err = runGT(t, gtHome, "refinery", "queue", "testrig", "--json")
	if err != nil {
		t.Fatalf("gt refinery queue --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("queue --json output is not valid JSON: %s", out)
	}
	if !strings.Contains(out, mrID) {
		t.Errorf("queue --json output missing MR ID %q: %s", mrID, out)
	}
}

func TestCLIDoneShowsMergeRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// Create work item, agent, and sling via API.
	townStore.CreateAgent("Smoke", "testrig", "polecat")
	itemID, err := rigStore.CreateWorkItem("CLI done test", "Test done CLI output", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	result, err := dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		AgentName:  "Smoke",
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("sling: %v", err)
	}

	// Write a file in the worktree so there's something to commit.
	os.WriteFile(filepath.Join(result.WorktreeDir, "done_test.go"),
		[]byte("package main\n"), 0o644)

	// Close stores before CLI invocation (CLI opens its own).
	rigStore.Close()
	townStore.Close()

	// Run gt done via CLI.
	bin := gtBin(t)
	cmd := exec.Command(bin, "done", "--rig=testrig", "--agent=Smoke")
	cmd.Env = append(os.Environ(), "GT_HOME="+gtHome)
	out, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(out))
	if err != nil {
		t.Fatalf("gt done failed: %v: %s", err, outStr)
	}

	// Verify output contains "Merge request:" and "mr-".
	if !strings.Contains(outStr, "Merge request:") {
		t.Errorf("done output missing 'Merge request:': %s", outStr)
	}
	if !strings.Contains(outStr, "mr-") {
		t.Errorf("done output missing MR ID (mr- prefix): %s", outStr)
	}
}

func TestCLIRefineryQueueEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	openStores(t, "testrig") // ensure rig DB exists

	out, err := runGT(t, gtHome, "refinery", "queue", "testrig")
	if err != nil {
		t.Fatalf("gt refinery queue failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "empty") {
		t.Errorf("queue output should indicate empty: %s", out)
	}
}

func TestCLIStatusWithRefinery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	openStores(t, "testrig") // ensure stores exist

	// Status without refinery running.
	out, _ := runGT(t, gtHome, "status", "testrig")
	if !strings.Contains(out, "Refinery:") {
		t.Errorf("status output missing 'Refinery:' line: %s", out)
	}
	if !strings.Contains(out, "not running") {
		t.Errorf("status output should show refinery not running: %s", out)
	}

	// JSON output should contain refinery and merge_queue fields.
	out, _ = runGT(t, gtHome, "status", "testrig", "--json")
	if !json.Valid([]byte(out)) {
		t.Errorf("status --json output is not valid JSON: %s", out)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to unmarshal status JSON: %v", err)
	}
	if _, ok := result["refinery"]; !ok {
		t.Error("status JSON missing 'refinery' field")
	}
	if _, ok := result["merge_queue"]; !ok {
		t.Error("status JSON missing 'merge_queue' field")
	}
}
