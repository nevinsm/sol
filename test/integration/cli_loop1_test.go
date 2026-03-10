package integration

import (
	"context"
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
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "prefect", "run", "--help")
	if err != nil {
		t.Fatalf("sol prefect run --help failed: %v: %s", err, out)
	}
}

func TestCLIPrefectStopHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "prefect", "stop", "--help")
	if err != nil {
		t.Fatalf("sol prefect stop --help failed: %v: %s", err, out)
	}
}

func TestCLIStatusHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "status", "--help")
	if err != nil {
		t.Fatalf("sol status --help failed: %v: %s", err, out)
	}
}

func TestCLIStatusWorld(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	// sol status ember exits non-zero (no agents, but should not crash).
	// Expect exit code 2 (degraded — no prefect running).
	out, err := runGT(t, solHome, "status", "ember")
	if err == nil {
		t.Log("sol status exited 0 (no agents, expected non-zero)")
	}
	// The important thing: it shouldn't crash with a stack trace.
	if strings.Contains(out, "panic") {
		t.Fatalf("sol status panicked: %s", out)
	}
}

func TestCLIStatusJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome, sourceRepo := setupTestEnv(t)
	initWorld(t, solHome, "ember")
	worldStore, sphereStore := openStores(t, "ember")

	// Create an agent and writ, cast.
	sphereStore.CreateAgent("Smoke", "ember", "agent")
	itemID, _ := worldStore.CreateWrit("CLI status test", "JSON test", "autarch", 2, nil)

	// Need to cast from a git repo context via CLI.
	// Use the API directly since the CLI discovers the repo from cwd.
	mgr := dispatch.NewSessionManager()
	dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: itemID,
		World:        "ember",
		AgentName:  "Smoke",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)

	// Close stores before running the CLI (CLI opens its own).
	worldStore.Close()
	sphereStore.Close()

	// Run sol status ember --json.
	out, _ := runGT(t, solHome, "status", "ember", "--json")

	// Verify output is valid JSON.
	if !json.Valid([]byte(out)) {
		t.Fatalf("sol status --json output is not valid JSON: %s", out)
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
	if world, ok := result["world"].(string); !ok || world != "ember" {
		t.Errorf("status JSON world: got %v, want ember", result["world"])
	}
}

func TestCLICastAutoProvision(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, solHome, "ember", sourceRepo)

	// Create a writ via CLI.
	itemID, err := runGT(t, solHome, "writ", "create", "--world=ember", "--title=auto provision test")
	if err != nil {
		t.Fatalf("sol writ create failed: %v: %s", err, itemID)
	}

	// Cast without --agent flag. Must run from a git repo directory.
	bin := gtBin(t)
	cmd := exec.Command(bin, "cast", itemID, "--world=ember")
	cmd.Env = append(os.Environ(), "SOL_HOME="+solHome)
	cmd.Dir = sourceRepo // Run from the git repo directory.
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sol cast (auto-provision) failed: %v: %s", err, strings.TrimSpace(string(out)))
	}

	// Verify: agent list contains an auto-provisioned agent name.
	agentOut, err := runGT(t, solHome, "agent", "list", "--world=ember")
	if err != nil {
		t.Fatalf("sol agent list failed: %v: %s", err, agentOut)
	}

	// The auto-provisioned name should be the first from the default pool (Nova).
	if !strings.Contains(agentOut, "Nova") {
		t.Errorf("agent list should contain auto-provisioned name 'Nova': %s", agentOut)
	}
}
