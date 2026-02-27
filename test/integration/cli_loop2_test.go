package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/session"
)

func TestCLIForgeStartHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "forge", "start", "--help")
	if err != nil {
		t.Fatalf("gt forge start --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Start the forge") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIForgeStopHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "forge", "stop", "--help")
	if err != nil {
		t.Fatalf("gt forge stop --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Stop the forge") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIForgeQueueHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "forge", "queue", "--help")
	if err != nil {
		t.Fatalf("gt forge queue --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "merge request queue") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIForgeAttachHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "forge", "attach", "--help")
	if err != nil {
		t.Fatalf("gt forge attach --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Attach to the forge") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIForgeQueue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	worldStore, _ := openStores(t, "testrig")

	// Create a work item and a merge request.
	itemID, err := worldStore.CreateWorkItem("Test merge item", "desc", "test", 1, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	branch := "outpost/Toast/" + itemID
	mrID, err := worldStore.CreateMergeRequest(itemID, branch, 1)
	if err != nil {
		t.Fatalf("create merge request: %v", err)
	}
	_ = sourceRepo // needed by setupTestEnv but not directly used here

	// Human-readable output.
	out, err := runGT(t, gtHome, "forge", "queue", "testrig")
	if err != nil {
		t.Fatalf("gt forge queue failed: %v: %s", err, out)
	}
	if !strings.Contains(out, mrID) {
		t.Errorf("queue output missing MR ID %q: %s", mrID, out)
	}
	if !strings.Contains(out, itemID) {
		t.Errorf("queue output missing work item ID %q: %s", itemID, out)
	}

	// JSON output.
	out, err = runGT(t, gtHome, "forge", "queue", "testrig", "--json")
	if err != nil {
		t.Fatalf("gt forge queue --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("queue --json output is not valid JSON: %s", out)
	}
	if !strings.Contains(out, mrID) {
		t.Errorf("queue --json output missing MR ID %q: %s", mrID, out)
	}
}

func TestCLIResolveShowsMergeRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "testrig")
	mgr := session.New()

	// Create work item, agent, and cast via API.
	sphereStore.CreateAgent("Smoke", "testrig", "agent")
	itemID, err := worldStore.CreateWorkItem("CLI resolve test", "Test resolve CLI output", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	result, err := dispatch.Cast(dispatch.CastOpts{
		WorkItemID: itemID,
		World:        "testrig",
		AgentName:  "Smoke",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("cast: %v", err)
	}

	// Write a file in the worktree so there's something to commit.
	os.WriteFile(filepath.Join(result.WorktreeDir, "done_test.go"),
		[]byte("package main\n"), 0o644)

	// Close stores before CLI invocation (CLI opens its own).
	worldStore.Close()
	sphereStore.Close()

	// Run gt resolve via CLI.
	bin := gtBin(t)
	cmd := exec.Command(bin, "resolve", "--world=testrig", "--agent=Smoke")
	cmd.Env = append(os.Environ(), "SOL_HOME="+gtHome)
	out, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(out))
	if err != nil {
		t.Fatalf("gt resolve failed: %v: %s", err, outStr)
	}

	// Verify output contains "Merge request:" and "mr-".
	if !strings.Contains(outStr, "Merge request:") {
		t.Errorf("resolve output missing 'Merge request:': %s", outStr)
	}
	if !strings.Contains(outStr, "mr-") {
		t.Errorf("resolve output missing MR ID (mr- prefix): %s", outStr)
	}
}

func TestCLIForgeQueueEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	openStores(t, "testrig") // ensure rig DB exists

	out, err := runGT(t, gtHome, "forge", "queue", "testrig")
	if err != nil {
		t.Fatalf("gt forge queue failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "empty") {
		t.Errorf("queue output should indicate empty: %s", out)
	}
}

func TestCLIStatusWithForge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	openStores(t, "testrig") // ensure stores exist

	// Status without forge running.
	out, _ := runGT(t, gtHome, "status", "testrig")
	if !strings.Contains(out, "Forge:") {
		t.Errorf("status output missing 'Forge:' line: %s", out)
	}
	if !strings.Contains(out, "not running") {
		t.Errorf("status output should show forge not running: %s", out)
	}

	// JSON output should contain forge and merge_queue fields.
	out, _ = runGT(t, gtHome, "status", "testrig", "--json")
	if !json.Valid([]byte(out)) {
		t.Errorf("status --json output is not valid JSON: %s", out)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to unmarshal status JSON: %v", err)
	}
	if _, ok := result["forge"]; !ok {
		t.Error("status JSON missing 'forge' field")
	}
	if _, ok := result["merge_queue"]; !ok {
		t.Error("status JSON missing 'merge_queue' field")
	}
}
