package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/cliformat"
	"github.com/nevinsm/sol/internal/config"
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

// setupAgentListTest creates a temp SOL_HOME with a single world whose
// world.toml pins agents.model = "opus" so tests can assert the MODEL
// column is populated. Returns the sphere store and world name.
func setupAgentListTest(t *testing.T, world string) (*store.SphereStore, string) {
	t.Helper()

	// Reset package-level flag vars to avoid cross-test pollution.
	agentListWorld = ""
	agentListJSON = false
	agentListAll = false

	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	t.Setenv("SOL_WORLD", "")

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	worldDir := filepath.Join(dir, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(worldDir, "world.toml"),
		[]byte("[agents]\nmodel = \"opus\"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	sphere, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sphere.Close() })

	return sphere, world
}

// writeAgentAccount seeds the .account metadata file that readAgentAccountBinding
// reads. Mirrors the layout broker uses when it provisions an agent's
// claude-config directory.
func writeAgentAccount(t *testing.T, world, role, name, handle string) {
	t.Helper()
	dir := config.ClaudeConfigDir(config.WorldDir(world), role, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".account"), []byte(handle+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildAgentListRowsPopulatesColumns(t *testing.T) {
	sphere, world := setupAgentListTest(t, "listtest")
	if _, err := sphere.CreateAgent("Nova", world, "outpost"); err != nil {
		t.Fatal(err)
	}
	writeAgentAccount(t, world, "outpost", "Nova", "personal")

	agents, err := sphere.ListAgents(world, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}

	rows := buildAgentListRows(agents, time.Now())
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.Name != "Nova" {
		t.Errorf("Name = %q, want Nova", r.Name)
	}
	if r.ActiveWrit != cliformat.EmptyMarker {
		t.Errorf("ActiveWrit = %q, want %q (no writ bound)", r.ActiveWrit, cliformat.EmptyMarker)
	}
	if r.Model != "opus" {
		t.Errorf("Model = %q, want %q (from world.toml agents.model)", r.Model, "opus")
	}
	if r.Account != "personal" {
		t.Errorf("Account = %q, want %q", r.Account, "personal")
	}
	if r.LastSeen == "" || r.LastSeen == cliformat.EmptyMarker {
		t.Errorf("LastSeen = %q, want non-empty (agents.updated_at is set on create)", r.LastSeen)
	}
}

func TestBuildAgentListRowsEmptyMarkersWhenUnset(t *testing.T) {
	// Fresh SOL_HOME with a world that has no agents.model and no .account.
	agentListWorld, agentListJSON, agentListAll = "", false, false
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	t.Setenv("SOL_WORLD", "")
	world := "bare"
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, world), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, world, "world.toml"), []byte("[world]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sphere, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sphere.Close() })

	if _, err := sphere.CreateAgent("Ghost", world, "outpost"); err != nil {
		t.Fatal(err)
	}
	agents, err := sphere.ListAgents(world, "")
	if err != nil {
		t.Fatal(err)
	}
	rows := buildAgentListRows(agents, time.Now())
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	// With no model configured and no .account file, both columns should
	// render as the canonical EmptyMarker rather than empty strings or
	// ad-hoc "n/a" values.
	if r.Model != cliformat.EmptyMarker {
		t.Errorf("Model = %q, want %q", r.Model, cliformat.EmptyMarker)
	}
	if r.Account != cliformat.EmptyMarker {
		t.Errorf("Account = %q, want %q", r.Account, cliformat.EmptyMarker)
	}
}

func TestAgentListJSONShape(t *testing.T) {
	sphere, world := setupAgentListTest(t, "jsontest")
	if _, err := sphere.CreateAgent("Nova", world, "outpost"); err != nil {
		t.Fatal(err)
	}
	writeAgentAccount(t, world, "outpost", "Nova", "personal")

	rootCmd.SetArgs([]string{"agent", "list", "--world", world, "--json"})
	var buf string
	buf = captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("agent list --json failed: %v", err)
		}
	})

	// Decode into a generic slice to assert the JSON field names — this
	// test is the contract for the --json field surface. Consumers rely on
	// snake_case names, not the Go struct field names.
	var decoded []map[string]interface{}
	if err := json.Unmarshal([]byte(buf), &decoded); err != nil {
		t.Fatalf("failed to parse JSON output: %v\n%s", err, buf)
	}
	if len(decoded) != 1 {
		t.Fatalf("expected 1 row in JSON, got %d: %s", len(decoded), buf)
	}
	row := decoded[0]
	// Required snake_case keys from the writ acceptance criteria.
	for _, key := range []string{"id", "name", "world", "role", "state", "active_writ_id", "model", "account", "last_seen_at"} {
		if _, ok := row[key]; !ok {
			t.Errorf("JSON row missing key %q: %v", key, row)
		}
	}
	if row["model"] != "opus" {
		t.Errorf("JSON model = %v, want opus", row["model"])
	}
	if row["account"] != "personal" {
		t.Errorf("JSON account = %v, want personal", row["account"])
	}
}

func TestAgentListAllAcrossWorlds(t *testing.T) {
	// Spin up SOL_HOME with two worlds, each with one agent, and assert
	// that --all returns both without requiring a world context.
	agentListWorld, agentListJSON, agentListAll = "", false, false
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	t.Setenv("SOL_WORLD", "")

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(dir, w), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, w, "world.toml"), []byte("[world]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	sphere, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sphere.Close() })

	if _, err := sphere.CreateAgent("Nova", "alpha", "outpost"); err != nil {
		t.Fatal(err)
	}
	if _, err := sphere.CreateAgent("Orion", "beta", "outpost"); err != nil {
		t.Fatal(err)
	}

	// --all path: no world context required. Capture JSON output so we
	// can inspect both rows deterministically.
	rootCmd.SetArgs([]string{"agent", "list", "--all", "--json"})
	buf := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("agent list --all --json failed: %v", err)
		}
	})

	var decoded []map[string]interface{}
	if err := json.Unmarshal([]byte(buf), &decoded); err != nil {
		t.Fatalf("failed to parse JSON: %v\n%s", err, buf)
	}
	if len(decoded) != 2 {
		t.Fatalf("expected 2 rows from --all, got %d: %s", len(decoded), buf)
	}

	seenWorlds := map[string]bool{}
	for _, row := range decoded {
		if w, ok := row["world"].(string); ok {
			seenWorlds[w] = true
		}
	}
	if !seenWorlds["alpha"] || !seenWorlds["beta"] {
		t.Errorf("--all missing worlds: seen=%v", seenWorlds)
	}
}
