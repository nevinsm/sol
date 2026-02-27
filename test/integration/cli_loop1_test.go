package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/dispatch"
)

func TestCLIPrefectRunHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "prefect", "run", "--help")
	if err != nil {
		t.Fatalf("gt prefect run --help failed: %v: %s", err, out)
	}
}

func TestCLIPrefectStopHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "prefect", "stop", "--help")
	if err != nil {
		t.Fatalf("gt prefect stop --help failed: %v: %s", err, out)
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
	// Expect exit code 2 (degraded — no prefect running).
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
	worldStore, sphereStore := openStores(t, "testrig")

	// Create an agent and work item, cast.
	sphereStore.CreateAgent("Smoke", "testrig", "agent")
	itemID, _ := worldStore.CreateWorkItem("CLI status test", "JSON test", "operator", 2, nil)

	// Need to cast from a git repo context via CLI.
	// Use the API directly since the CLI discovers the repo from cwd.
	mgr := dispatch.NewSessionManager()
	dispatch.Cast(dispatch.CastOpts{
		WorkItemID: itemID,
		World:        "testrig",
		AgentName:  "Smoke",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)

	// Close stores before running the CLI (CLI opens its own).
	worldStore.Close()
	sphereStore.Close()

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

	for _, field := range []string{"world", "prefect", "agents", "summary"} {
		if _, ok := result[field]; !ok {
			t.Errorf("status JSON missing field %q", field)
		}
	}

	// Verify world name is correct.
	if world, ok := result["world"].(string); !ok || world != "testrig" {
		t.Errorf("status JSON world: got %v, want testrig", result["world"])
	}
}

func TestCLICastAutoProvision(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)

	// Create a work item via CLI.
	itemID, err := runGT(t, gtHome, "store", "create", "--world=testrig", "--title=auto provision test")
	if err != nil {
		t.Fatalf("gt store create failed: %v: %s", err, itemID)
	}

	// Cast without --agent flag. Must run from a git repo directory.
	bin := gtBin(t)
	cmd := exec.Command(bin, "cast", itemID, "testrig")
	cmd.Env = append(os.Environ(), "SOL_HOME="+gtHome)
	cmd.Dir = sourceRepo // Run from the git repo directory.
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gt cast (auto-provision) failed: %v: %s", err, strings.TrimSpace(string(out)))
	}

	// Verify: agent list contains an auto-provisioned agent name.
	agentOut, err := runGT(t, gtHome, "agent", "list", "--world=testrig")
	if err != nil {
		t.Fatalf("gt agent list failed: %v: %s", err, agentOut)
	}

	// The auto-provisioned name should be the first from the default pool (Toast).
	if !strings.Contains(agentOut, "Toast") {
		t.Errorf("agent list should contain auto-provisioned name 'Toast': %s", agentOut)
	}
}
