package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/nevinsm/gt/internal/dispatch"
)

func TestCLISupervisorRunHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "supervisor", "run", "--help")
	if err != nil {
		t.Fatalf("gt supervisor run --help failed: %v: %s", err, out)
	}
}

func TestCLISupervisorStopHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "supervisor", "stop", "--help")
	if err != nil {
		t.Fatalf("gt supervisor stop --help failed: %v: %s", err, out)
	}
}

func TestCLIStatusHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "status", "--help")
	if err != nil {
		t.Fatalf("gt status --help failed: %v: %s", err, out)
	}
}

func TestCLIStatusRig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	// gt status testrig exits non-zero (no agents, but should not crash).
	// Expect exit code 2 (degraded — no supervisor running).
	out, err := runGT(t, gtHome, "status", "testrig")
	if err == nil {
		t.Log("gt status exited 0 (no agents, expected non-zero)")
	}
	// The important thing: it shouldn't crash with a stack trace.
	if strings.Contains(out, "panic") {
		t.Fatalf("gt status panicked: %s", out)
	}
}

func TestCLIStatusJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")

	// Create an agent and work item, sling.
	townStore.CreateAgent("Smoke", "testrig", "polecat")
	itemID, _ := rigStore.CreateWorkItem("CLI status test", "JSON test", "operator", 2, nil)

	// Need to sling from a git repo context via CLI.
	// Use the API directly since the CLI discovers the repo from cwd.
	mgr := dispatch.NewSessionManager()
	dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		AgentName:  "Smoke",
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr)

	// Close stores before running the CLI (CLI opens its own).
	rigStore.Close()
	townStore.Close()

	// Run gt status testrig --json.
	out, _ := runGT(t, gtHome, "status", "testrig", "--json")

	// Verify output is valid JSON.
	if !json.Valid([]byte(out)) {
		t.Fatalf("gt status --json output is not valid JSON: %s", out)
	}

	// Verify JSON has expected fields.
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal status JSON: %v", err)
	}

	for _, field := range []string{"rig", "supervisor", "agents", "summary"} {
		if _, ok := result[field]; !ok {
			t.Errorf("status JSON missing field %q", field)
		}
	}

	// Verify rig name is correct.
	if rig, ok := result["rig"].(string); !ok || rig != "testrig" {
		t.Errorf("status JSON rig: got %v, want testrig", result["rig"])
	}
}

func TestCLISlingAutoProvision(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)

	// Create a work item via CLI.
	itemID, err := runGT(t, gtHome, "store", "create", "--db=testrig", "--title=auto provision test")
	if err != nil {
		t.Fatalf("gt store create failed: %v: %s", err, itemID)
	}

	// Sling without --agent flag. Must run from a git repo directory.
	bin := gtBin(t)
	cmd := exec.Command(bin, "sling", itemID, "testrig")
	cmd.Env = append(os.Environ(), "GT_HOME="+gtHome)
	cmd.Dir = sourceRepo // Run from the git repo directory.
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gt sling (auto-provision) failed: %v: %s", err, strings.TrimSpace(string(out)))
	}

	// Verify: agent list contains an auto-provisioned agent name.
	agentOut, err := runGT(t, gtHome, "agent", "list", "--rig=testrig")
	if err != nil {
		t.Fatalf("gt agent list failed: %v: %s", err, agentOut)
	}

	// The auto-provisioned name should be the first from the default pool (Toast).
	if !strings.Contains(agentOut, "Toast") {
		t.Errorf("agent list should contain auto-provisioned name 'Toast': %s", agentOut)
	}
}
