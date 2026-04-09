package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// setupAgentResetTest creates a temporary SOL_HOME with a world, returns the
// sphere store, world store, and world name. Callers may create agents and
// writs via the returned stores.
func setupAgentResetTest(t *testing.T) (*store.SphereStore, *store.WorldStore, string) {
	t.Helper()

	// Reset package-level flag vars to avoid cross-test pollution.
	agentResetWorld = ""
	agentResetConfirm = false

	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	world := "resettest"
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	worldDir := filepath.Join(dir, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[world]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sphere, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sphere.Close() })

	ws, err := store.OpenWorld(world)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })

	return sphere, ws, world
}

// bindAgentToWrit creates an agent in world, creates a writ, tethers the
// agent to the writ, and sets the agent's state to working. Returns the
// agent name and writ ID.
func bindAgentToWrit(t *testing.T, sphere *store.SphereStore, ws *store.WorldStore, world, agentName, writTitle string) (string, string) {
	t.Helper()

	if _, err := sphere.CreateAgent(agentName, world, "outpost"); err != nil {
		t.Fatal(err)
	}
	agentID := world + "/" + agentName

	writID, err := ws.CreateWrit(writTitle, "", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Mark writ tethered → working and assignee to the agent.
	if err := ws.UpdateWrit(writID, store.WritUpdates{Status: "tethered", Assignee: agentName}); err != nil {
		t.Fatal(err)
	}
	if err := ws.UpdateWrit(writID, store.WritUpdates{Status: "working"}); err != nil {
		t.Fatal(err)
	}

	// Update agent state.
	if err := sphere.UpdateAgentState(agentID, store.AgentWorking, writID); err != nil {
		t.Fatal(err)
	}

	// Write the tether file.
	if err := tether.Write(world, agentName, writID, "outpost"); err != nil {
		t.Fatal(err)
	}

	return agentName, writID
}

// runAgentReset executes `sol agent reset <name>` with the given flags.
// It captures any error returned from rootCmd.Execute.
func runAgentReset(t *testing.T, world, name string, confirm bool) error {
	t.Helper()
	agentResetWorld = ""
	agentResetConfirm = false
	args := []string{"agent", "reset", name, "--world", world}
	if confirm {
		args = append(args, "--confirm")
	}
	rootCmd.SetArgs(args)
	return rootCmd.Execute()
}

func TestAgentResetSkipsClosedWrit(t *testing.T) {
	sphere, ws, world := setupAgentResetTest(t)
	agentName, writID := bindAgentToWrit(t, sphere, ws, world, "Nova", "closed writ test")

	// Close the writ (this is the scenario the operator hit).
	if _, err := ws.CloseWrit(writID); err != nil {
		t.Fatal(err)
	}

	// Verify pre-conditions.
	before, err := ws.GetWrit(writID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Status != "closed" {
		t.Fatalf("pre: writ status = %q, want closed", before.Status)
	}
	beforeAssignee := before.Assignee

	// Run reset with --confirm.
	if err := runAgentReset(t, world, agentName, true); err != nil {
		t.Fatalf("agent reset --confirm failed: %v", err)
	}

	// (a) Agent should be idle with active_writ cleared.
	agentID := world + "/" + agentName
	a, err := sphere.GetAgent(agentID)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != "idle" {
		t.Errorf("agent state = %q, want idle", a.State)
	}
	if a.ActiveWrit != "" {
		t.Errorf("agent active_writ = %q, want empty", a.ActiveWrit)
	}

	// (b) Tether file should be cleared.
	if tether.IsTethered(world, agentName, "outpost") {
		t.Errorf("tether was not cleared")
	}

	// (c) Writ should be untouched.
	after, err := ws.GetWrit(writID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "closed" {
		t.Errorf("writ status = %q, want closed (should not have been modified)", after.Status)
	}
	if after.Assignee != beforeAssignee {
		t.Errorf("writ assignee = %q, want %q (should not have been modified)", after.Assignee, beforeAssignee)
	}
}

func TestAgentResetSkipsDoneWrit(t *testing.T) {
	sphere, ws, world := setupAgentResetTest(t)
	agentName, writID := bindAgentToWrit(t, sphere, ws, world, "Nova", "done writ test")

	// Mark writ done (working → done is a valid transition).
	if err := ws.UpdateWrit(writID, store.WritUpdates{Status: "done"}); err != nil {
		t.Fatal(err)
	}

	before, err := ws.GetWrit(writID)
	if err != nil {
		t.Fatal(err)
	}
	beforeAssignee := before.Assignee

	if err := runAgentReset(t, world, agentName, true); err != nil {
		t.Fatalf("agent reset --confirm failed: %v", err)
	}

	after, err := ws.GetWrit(writID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "done" {
		t.Errorf("writ status = %q, want done (should not have been modified)", after.Status)
	}
	if after.Assignee != beforeAssignee {
		t.Errorf("writ assignee = %q, want %q (should not have been modified)", after.Assignee, beforeAssignee)
	}

	// Agent state should still have been reset.
	a, err := sphere.GetAgent(world + "/" + agentName)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != "idle" {
		t.Errorf("agent state = %q, want idle", a.State)
	}
	if tether.IsTethered(world, agentName, "outpost") {
		t.Errorf("tether was not cleared")
	}
}

func TestAgentResetHandlesOpenWrit(t *testing.T) {
	sphere, ws, world := setupAgentResetTest(t)
	agentName, writID := bindAgentToWrit(t, sphere, ws, world, "Nova", "in-progress writ test")

	// Writ is in "working" state via bindAgentToWrit. Run reset.
	if err := runAgentReset(t, world, agentName, true); err != nil {
		t.Fatalf("agent reset --confirm failed: %v", err)
	}

	// Writ should be returned to open pool.
	after, err := ws.GetWrit(writID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "open" {
		t.Errorf("writ status = %q, want open", after.Status)
	}
	if after.Assignee != "" {
		t.Errorf("writ assignee = %q, want empty", after.Assignee)
	}

	a, err := sphere.GetAgent(world + "/" + agentName)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != "idle" {
		t.Errorf("agent state = %q, want idle", a.State)
	}
	if a.ActiveWrit != "" {
		t.Errorf("agent active_writ = %q, want empty", a.ActiveWrit)
	}
	if tether.IsTethered(world, agentName, "outpost") {
		t.Errorf("tether was not cleared")
	}
}

func TestAgentResetPreviewShowsTerminalState(t *testing.T) {
	sphere, ws, world := setupAgentResetTest(t)
	agentName, writID := bindAgentToWrit(t, sphere, ws, world, "Nova", "preview closed")

	if _, err := ws.CloseWrit(writID); err != nil {
		t.Fatal(err)
	}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runAgentReset(t, world, agentName, false)
	})

	// Preview mode exits with code 1 (exitError).
	ee, ok := runErr.(*exitError)
	if !ok || ee.code != 1 {
		t.Fatalf("preview: expected exitError{1}, got %v", runErr)
	}

	if !strings.Contains(out, "already closed") {
		t.Errorf("preview output missing 'already closed' marker:\n%s", out)
	}
	if !strings.Contains(out, "will not be modified") {
		t.Errorf("preview output missing 'will not be modified' marker:\n%s", out)
	}

	// Confirm writ was not touched.
	after, err := ws.GetWrit(writID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "closed" {
		t.Errorf("writ status = %q, want closed", after.Status)
	}
}
