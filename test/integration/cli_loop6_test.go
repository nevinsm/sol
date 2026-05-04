package integration

// cli_loop6_test.go — Integration tests for CLI commands that previously
// had no end-to-end integration coverage:
//   sol config claude   (T18): directory creation and file seeding
//   sol writ activate   (T19): argument parsing, flag validation, output
//   sol caravan list    (T22): end-to-end with store interaction
//   sol caravan dep     (T24): end-to-end dependency management

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================
// sol config claude (T18)
// ============================================================

// TestConfigClaudeSeedsDefaults verifies that `sol config claude` creates the
// defaults directory and seeds settings.json + statusline.sh before attempting
// to launch the interactive claude session. When claude is not in PATH, the
// command returns an appropriate error, but the seeded files persist.
func TestConfigClaudeSeedsDefaults(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, _ := setupTestEnv(t)

	// Ensure the sol binary is built BEFORE we restrict PATH.
	// buildOnce.Do needs 'go' in PATH to compile the binary.
	gtBin(t)

	// Ensure claude binary is not found by stripping PATH to a minimal set.
	// The sol binary is invoked by absolute path, so this is safe.
	t.Setenv("PATH", t.TempDir()) // empty temp dir — no claude here

	out, err := runGT(t, gtHome, "config", "claude")
	// Must fail because claude is not in PATH.
	if err == nil {
		t.Fatal("expected error when claude is not in PATH, got success")
	}
	if !strings.Contains(out, "claude not found") {
		t.Errorf("expected 'claude not found' in error output, got: %s", out)
	}

	// But the defaults directory should have been seeded before the error.
	defaultsDir := filepath.Join(gtHome, ".claude-defaults")
	if _, err := os.Stat(defaultsDir); os.IsNotExist(err) {
		t.Fatal("expected .claude-defaults directory to be created")
	}

	// settings.json should exist (sol-owned, always overwritten from template).
	settingsPath := filepath.Join(defaultsDir, "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		t.Error("expected settings.json to be created in .claude-defaults")
	} else {
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("read settings.json: %v", err)
		}
		if !json.Valid(data) {
			t.Error("settings.json should be valid JSON")
		}
	}

	// statusline.sh should exist (sol-managed script).
	statuslinePath := filepath.Join(defaultsDir, "statusline.sh")
	if _, err := os.Stat(statuslinePath); os.IsNotExist(err) {
		t.Error("expected statusline.sh to be created in .claude-defaults")
	} else {
		info, err := os.Stat(statuslinePath)
		if err != nil {
			t.Fatalf("stat statusline.sh: %v", err)
		}
		if info.Mode()&0o111 == 0 {
			t.Error("statusline.sh should be executable")
		}
	}

	// CLAUDE.local.md persona file should exist (written for config session).
	personaPath := filepath.Join(defaultsDir, "CLAUDE.local.md")
	if _, err := os.Stat(personaPath); os.IsNotExist(err) {
		t.Error("expected CLAUDE.local.md persona file to be created")
	}
}

// TestConfigClaudeHelpSmoke verifies that --help does not fail and shows
// relevant usage information.
func TestConfigClaudeHelpSmoke(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "config", "claude", "--help")
	if err != nil {
		t.Fatalf("sol config claude --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "defaults") {
		t.Errorf("expected 'defaults' in config claude help, got: %s", out)
	}
	if !strings.Contains(out, "settings.json") {
		t.Errorf("expected 'settings.json' mentioned in help, got: %s", out)
	}
}

// ============================================================
// sol writ activate (T19)
// ============================================================

// TestWritActivateCLI exercises the `sol writ activate` Cobra command
// end-to-end: argument parsing, flag validation, store interaction, and
// output formatting (both human-readable and JSON).
//
// writ activate is for persistent agents (envoy), so this test creates an
// envoy and tethers writs to it.
func TestWritActivateCLI(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, sourceRepo := setupTestEnvWithRepo(t)
	setupWorld(t, gtHome, "ember", sourceRepo)

	// Create an envoy (persistent agent — required for writ activate).
	out, err := runGT(t, gtHome, "envoy", "create", "Kindle", "--world=ember")
	if err != nil {
		t.Fatalf("envoy create: %v: %s", err, out)
	}

	// Create two writs.
	out1, err := runGT(t, gtHome, "writ", "create", "--world=ember", "--title=First task")
	if err != nil {
		t.Fatalf("writ create #1: %v: %s", err, out1)
	}
	writID1 := extractWritID(t, out1)

	out2, err := runGT(t, gtHome, "writ", "create", "--world=ember", "--title=Second task")
	if err != nil {
		t.Fatalf("writ create #2: %v: %s", err, out2)
	}
	writID2 := extractWritID(t, out2)

	// Tether both writs to the envoy.
	out, err = runGT(t, gtHome, "tether", writID1, "--agent=Kindle", "--world=ember")
	if err != nil {
		t.Fatalf("tether #1: %v: %s", err, out)
	}
	out, err = runGT(t, gtHome, "tether", writID2, "--agent=Kindle", "--world=ember")
	if err != nil {
		t.Fatalf("tether #2: %v: %s", err, out)
	}

	// After tethering, the first writ is already active (Tether sets active_writ
	// when the agent was idle). Activating it again should be idempotent.
	out, err = runGT(t, gtHome, "writ", "activate", writID1, "--world=ember", "--agent=Kindle")
	if err != nil {
		t.Fatalf("writ activate (idempotent): %v: %s", err, out)
	}
	if !strings.Contains(out, "already active") {
		t.Errorf("expected 'already active' for first tethered writ, got: %s", out)
	}

	// Activate the second writ — should switch active writ and mention the previous one.
	out, err = runGT(t, gtHome, "writ", "activate", writID2, "--world=ember", "--agent=Kindle")
	if err != nil {
		t.Fatalf("writ activate #2: %v: %s", err, out)
	}
	if !strings.Contains(out, "Activated") || !strings.Contains(out, writID2) {
		t.Errorf("expected activation message with second writ ID, got: %s", out)
	}
	if !strings.Contains(out, writID1) {
		t.Errorf("expected previous writ ID %s in output, got: %s", writID1, out)
	}

	// Activate writID2 again — idempotent.
	out, err = runGT(t, gtHome, "writ", "activate", writID2, "--world=ember", "--agent=Kindle")
	if err != nil {
		t.Fatalf("writ activate (second idempotent): %v: %s", err, out)
	}
	if !strings.Contains(out, "already active") {
		t.Errorf("expected 'already active' for second activation, got: %s", out)
	}

	// JSON output — activate back to writID1 and verify JSON response.
	out, err = runGT(t, gtHome, "writ", "activate", writID1, "--world=ember", "--agent=Kindle", "--json")
	if err != nil {
		t.Fatalf("writ activate --json: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("writ activate --json output is not valid JSON: %s", out)
	}
	var agentJSON struct {
		Name       string `json:"name"`
		ActiveWrit string `json:"active_writ_id"`
	}
	if err := json.Unmarshal([]byte(out), &agentJSON); err != nil {
		t.Fatalf("unmarshal agent JSON: %v: %s", err, out)
	}
	if agentJSON.Name != "Kindle" {
		t.Errorf("JSON agent name = %q, want %q", agentJSON.Name, "Kindle")
	}
	if agentJSON.ActiveWrit != writID1 {
		t.Errorf("JSON active_writ_id = %q, want %q", agentJSON.ActiveWrit, writID1)
	}
}

// TestWritActivateErrorPaths exercises error cases for `sol writ activate`.
func TestWritActivateErrorPaths(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, sourceRepo := setupTestEnvWithRepo(t)
	setupWorld(t, gtHome, "ember", sourceRepo)

	// No arguments — should fail with usage.
	out, err := runGT(t, gtHome, "writ", "activate")
	if err == nil {
		t.Fatalf("expected error with no arguments, got: %s", out)
	}

	// Invalid writ ID format.
	out, err = runGT(t, gtHome, "writ", "activate", "not-a-writ-id", "--world=ember", "--agent=Ghost")
	if err == nil {
		t.Fatalf("expected error for invalid writ ID, got: %s", out)
	}

	// Nonexistent agent.
	out, err = runGT(t, gtHome, "writ", "activate", "sol-0000000000000000", "--world=ember", "--agent=Ghost")
	if err == nil {
		t.Fatalf("expected error for nonexistent agent, got: %s", out)
	}
}

// ============================================================
// sol caravan list (T22)
// ============================================================

// TestCaravanListCLI exercises the `sol caravan list` end-to-end flow with
// store interaction, output formatting, and filtering.
func TestCaravanListCLI(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// Create a writ so we can add it to a caravan.
	writOut, err := runGT(t, gtHome, "writ", "create", "--world=ember", "--title=Caravan task")
	if err != nil {
		t.Fatalf("writ create: %v: %s", err, writOut)
	}
	writID := extractWritID(t, writOut)

	// Create a caravan with an item.
	out, err := runGT(t, gtHome, "caravan", "create", "alpha-batch", writID, "--world=ember")
	if err != nil {
		t.Fatalf("caravan create: %v: %s", err, out)
	}
	if !strings.Contains(out, "alpha-batch") {
		t.Fatalf("expected caravan name in create output, got: %s", out)
	}

	// Create a second caravan (no items).
	out, err = runGT(t, gtHome, "caravan", "create", "beta-batch")
	if err != nil {
		t.Fatalf("caravan create #2: %v: %s", err, out)
	}

	// Default list: should show both caravans (both are drydock/active).
	out, err = runGT(t, gtHome, "caravan", "list")
	if err != nil {
		t.Fatalf("caravan list: %v: %s", err, out)
	}
	if !strings.Contains(out, "alpha-batch") {
		t.Errorf("expected 'alpha-batch' in list output, got: %s", out)
	}
	if !strings.Contains(out, "beta-batch") {
		t.Errorf("expected 'beta-batch' in list output, got: %s", out)
	}
	// Should show count footer.
	if !strings.Contains(out, "2 caravans") {
		t.Errorf("expected '2 caravans' count footer, got: %s", out)
	}

	// JSON output: valid JSON array.
	out, err = runGT(t, gtHome, "caravan", "list", "--json")
	if err != nil {
		t.Fatalf("caravan list --json: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("caravan list --json output is not valid JSON: %s", out)
	}
	var entries []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Items  int    `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("unmarshal caravan list JSON: %v: %s", err, out)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 caravans in JSON, got %d", len(entries))
	}

	// Find the alpha-batch entry and check it has 1 item.
	found := false
	for _, e := range entries {
		if e.Name == "alpha-batch" {
			found = true
			if e.Items != 1 {
				t.Errorf("alpha-batch items = %d, want 1", e.Items)
			}
			if e.Status != "drydock" {
				t.Errorf("alpha-batch status = %q, want %q", e.Status, "drydock")
			}
		}
	}
	if !found {
		t.Errorf("alpha-batch not found in JSON output: %s", out)
	}

	// Status filter: --status=drydock should show both.
	out, err = runGT(t, gtHome, "caravan", "list", "--status=drydock")
	if err != nil {
		t.Fatalf("caravan list --status=drydock: %v: %s", err, out)
	}
	if !strings.Contains(out, "alpha-batch") || !strings.Contains(out, "beta-batch") {
		t.Errorf("expected both caravans in --status=drydock output, got: %s", out)
	}

	// Status filter: --status=open should show none.
	out, err = runGT(t, gtHome, "caravan", "list", "--status=open")
	if err != nil {
		t.Fatalf("caravan list --status=open: %v: %s", err, out)
	}
	if !strings.Contains(out, "No caravans") {
		t.Errorf("expected 'No caravans' for --status=open, got: %s", out)
	}

	// Invalid status filter.
	out, err = runGT(t, gtHome, "caravan", "list", "--status=bogus")
	if err == nil {
		t.Fatalf("expected error for invalid status, got: %s", out)
	}
	if !strings.Contains(out, "invalid status") {
		t.Errorf("expected 'invalid status' in error, got: %s", out)
	}

	// Mutually exclusive --all and --status.
	out, err = runGT(t, gtHome, "caravan", "list", "--all", "--status=open")
	if err == nil {
		t.Fatalf("expected error for --all + --status, got: %s", out)
	}
	if !strings.Contains(out, "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' in error, got: %s", out)
	}
}

// TestCaravanListEmpty verifies that listing with no caravans produces
// the appropriate empty-state message.
func TestCaravanListEmpty(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, _ := setupTestEnv(t)

	out, err := runGT(t, gtHome, "caravan", "list")
	if err != nil {
		t.Fatalf("caravan list (empty): %v: %s", err, out)
	}
	if !strings.Contains(out, "No active caravans") {
		t.Errorf("expected 'No active caravans' message, got: %s", out)
	}

	// JSON: empty array.
	out, err = runGT(t, gtHome, "caravan", "list", "--json")
	if err != nil {
		t.Fatalf("caravan list --json (empty): %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("empty list JSON is not valid: %s", out)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("expected empty JSON array, got: %s", out)
	}
}

// ============================================================
// sol caravan dep (T24)
// ============================================================

// TestCaravanDepCLI exercises the `sol caravan dep add/remove/list`
// end-to-end flow with store interaction and output formatting.
func TestCaravanDepCLI(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, _ := setupTestEnv(t)

	// Create two caravans (no items needed for dependency management).
	out1, err := runGT(t, gtHome, "caravan", "create", "upstream-batch")
	if err != nil {
		t.Fatalf("caravan create upstream: %v: %s", err, out1)
	}
	// Extract the caravan ID from the create output.
	upstreamID := extractCaravanIDFromOutput(t, out1)

	out2, err := runGT(t, gtHome, "caravan", "create", "downstream-batch")
	if err != nil {
		t.Fatalf("caravan create downstream: %v: %s", err, out2)
	}
	downstreamID := extractCaravanIDFromOutput(t, out2)

	// --- dep add ---
	out, err := runGT(t, gtHome, "caravan", "dep", "add", downstreamID, upstreamID)
	if err != nil {
		t.Fatalf("caravan dep add: %v: %s", err, out)
	}
	if !strings.Contains(out, "Added dependency") {
		t.Errorf("expected 'Added dependency' in output, got: %s", out)
	}
	if !strings.Contains(out, "downstream-batch") || !strings.Contains(out, "upstream-batch") {
		t.Errorf("expected both caravan names in dep add output, got: %s", out)
	}

	// --- dep list (text) ---
	out, err = runGT(t, gtHome, "caravan", "dep", "list", downstreamID)
	if err != nil {
		t.Fatalf("caravan dep list: %v: %s", err, out)
	}
	if !strings.Contains(out, "Depends on:") {
		t.Errorf("expected 'Depends on:' section in dep list output, got: %s", out)
	}
	if !strings.Contains(out, upstreamID) {
		t.Errorf("expected upstream ID %s in dep list output, got: %s", upstreamID, out)
	}

	// --- dep list (JSON) ---
	out, err = runGT(t, gtHome, "caravan", "dep", "list", downstreamID, "--json")
	if err != nil {
		t.Fatalf("caravan dep list --json: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("dep list --json output is not valid JSON: %s", out)
	}
	var depResponse struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		DependsOn []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"depends_on"`
		DependedBy []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"depended_by"`
	}
	if err := json.Unmarshal([]byte(out), &depResponse); err != nil {
		t.Fatalf("unmarshal dep list JSON: %v: %s", err, out)
	}
	if depResponse.ID != downstreamID {
		t.Errorf("dep list JSON id = %q, want %q", depResponse.ID, downstreamID)
	}
	if len(depResponse.DependsOn) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(depResponse.DependsOn))
	}
	if depResponse.DependsOn[0].ID != upstreamID {
		t.Errorf("dependency id = %q, want %q", depResponse.DependsOn[0].ID, upstreamID)
	}

	// Verify the upstream sees the downstream as a dependent.
	out, err = runGT(t, gtHome, "caravan", "dep", "list", upstreamID, "--json")
	if err != nil {
		t.Fatalf("caravan dep list upstream --json: %v: %s", err, out)
	}
	var upstreamDeps struct {
		DependedBy []struct {
			ID string `json:"id"`
		} `json:"depended_by"`
	}
	if err := json.Unmarshal([]byte(out), &upstreamDeps); err != nil {
		t.Fatalf("unmarshal upstream deps: %v: %s", err, out)
	}
	if len(upstreamDeps.DependedBy) != 1 || upstreamDeps.DependedBy[0].ID != downstreamID {
		t.Errorf("expected upstream depended_by to contain %s, got: %+v", downstreamID, upstreamDeps.DependedBy)
	}

	// --- dep remove ---
	out, err = runGT(t, gtHome, "caravan", "dep", "remove", downstreamID, upstreamID)
	if err != nil {
		t.Fatalf("caravan dep remove: %v: %s", err, out)
	}
	if !strings.Contains(out, "Removed dependency") {
		t.Errorf("expected 'Removed dependency' in output, got: %s", out)
	}

	// After removal, dep list should show (none).
	out, err = runGT(t, gtHome, "caravan", "dep", "list", downstreamID)
	if err != nil {
		t.Fatalf("caravan dep list (after remove): %v: %s", err, out)
	}
	if !strings.Contains(out, "(none)") {
		t.Errorf("expected '(none)' after dependency removal, got: %s", out)
	}
}

// TestCaravanDepAddJSON verifies the JSON output of `sol caravan dep add`.
func TestCaravanDepAddJSON(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, _ := setupTestEnv(t)

	out1, err := runGT(t, gtHome, "caravan", "create", "pre-req")
	if err != nil {
		t.Fatalf("caravan create pre-req: %v: %s", err, out1)
	}
	preReqID := extractCaravanIDFromOutput(t, out1)

	out2, err := runGT(t, gtHome, "caravan", "create", "main-batch")
	if err != nil {
		t.Fatalf("caravan create main-batch: %v: %s", err, out2)
	}
	mainID := extractCaravanIDFromOutput(t, out2)

	out, err := runGT(t, gtHome, "caravan", "dep", "add", mainID, preReqID, "--json")
	if err != nil {
		t.Fatalf("caravan dep add --json: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("dep add --json output is not valid JSON: %s", out)
	}
}

// TestCaravanDepErrorPaths exercises error cases for caravan dep commands.
func TestCaravanDepErrorPaths(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, _ := setupTestEnv(t)

	// No arguments.
	out, err := runGT(t, gtHome, "caravan", "dep", "add")
	if err == nil {
		t.Fatalf("expected error with no arguments, got: %s", out)
	}

	// Invalid caravan ID format.
	out, err = runGT(t, gtHome, "caravan", "dep", "list", "not-a-caravan-id")
	if err == nil {
		t.Fatalf("expected error for invalid caravan ID, got: %s", out)
	}

	// dep list with wrong arg count.
	out, err = runGT(t, gtHome, "caravan", "dep", "list")
	if err == nil {
		t.Fatalf("expected error with no arguments for dep list, got: %s", out)
	}
}

// --- Helpers ---

// extractCaravanIDFromOutput extracts a caravan ID (car-...) from CLI output.
func extractCaravanIDFromOutput(t *testing.T, output string) string {
	t.Helper()
	for _, word := range strings.Fields(output) {
		// Caravan IDs start with "car-".
		clean := strings.TrimSuffix(strings.TrimSuffix(word, ":"), ",")
		if strings.HasPrefix(clean, "car-") {
			return clean
		}
	}
	t.Fatalf("could not extract caravan ID from output: %s", output)
	return ""
}
