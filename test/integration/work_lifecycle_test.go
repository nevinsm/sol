package integration

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// =============================================================================
// sol tether — CLI integration tests
// =============================================================================

func TestCLITetherHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "tether", "--help")
	if err != nil {
		t.Fatalf("sol tether --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Bind a writ to a persistent agent") {
		t.Errorf("help output missing expected text: %s", out)
	}
	if !strings.Contains(out, "--agent") {
		t.Errorf("help output missing --agent flag: %s", out)
	}
}

func TestCLITetherMissingArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	// No writ-id argument — cobra should reject.
	out, err := runGT(t, gtHome, "tether", "--agent=Scout")
	if err == nil {
		t.Fatalf("expected error for missing writ-id arg, got success: %s", out)
	}
	// Cobra prints "accepts 1 arg(s)" when ExactArgs(1) fails.
	if !strings.Contains(out, "accepts 1 arg") {
		t.Errorf("expected arg count error, got: %s", out)
	}
}

func TestCLITetherMissingAgentFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	// No --agent flag — cobra should reject (MarkFlagRequired).
	out, err := runGT(t, gtHome, "tether", "sol-0000000000000000")
	if err == nil {
		t.Fatalf("expected error for missing --agent flag, got success: %s", out)
	}
	if !strings.Contains(out, "required flag") && !strings.Contains(out, "agent") {
		t.Errorf("expected required-flag error mentioning agent, got: %s", out)
	}
}

func TestCLITetherHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "tetherworld", sourceRepo)

	// Create an envoy (persistent role — tether is allowed).
	createEnvoy(t, gtHome, "tetherworld", "Scout")

	// Create an open writ.
	writOut, err := runGT(t, gtHome, "writ", "create", "--world=tetherworld", "--title=tether test")
	if err != nil {
		t.Fatalf("writ create failed: %v: %s", err, writOut)
	}
	writID := strings.TrimSpace(writOut)

	// Tether the writ to the envoy.
	out, err := runGT(t, gtHome, "tether", writID, "--agent=Scout", "--world=tetherworld")
	if err != nil {
		t.Fatalf("sol tether failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Tethered") {
		t.Errorf("tether output missing 'Tethered': %s", out)
	}
	if !strings.Contains(out, "Scout") {
		t.Errorf("tether output missing agent name: %s", out)
	}
	if !strings.Contains(out, writID) {
		t.Errorf("tether output missing writ ID: %s", out)
	}

	// Verify writ status changed to tethered.
	worldStore, _ := openStores(t, "tetherworld")
	item, err := worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("get writ: %v", err)
	}
	if item.Status != "tethered" {
		t.Errorf("expected writ status 'tethered', got %q", item.Status)
	}
}

func TestCLITetherJSONOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "tetherworld2", sourceRepo)

	createEnvoy(t, gtHome, "tetherworld2", "Scout")

	writOut, err := runGT(t, gtHome, "writ", "create", "--world=tetherworld2", "--title=json tether")
	if err != nil {
		t.Fatalf("writ create failed: %v: %s", err, writOut)
	}
	writID := strings.TrimSpace(writOut)

	out, err := runGT(t, gtHome, "tether", writID, "--agent=Scout", "--world=tetherworld2", "--json")
	if err != nil {
		t.Fatalf("sol tether --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("tether --json output is not valid JSON: %s", out)
	}
	if !strings.Contains(out, "Scout") {
		t.Errorf("JSON output missing agent name: %s", out)
	}
}

func TestCLITetherRejectsOutpost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "tetherworld3")

	// Create an outpost agent (not a persistent role).
	_, err := runGT(t, gtHome, "agent", "create", "Smoke", "--world=tetherworld3")
	if err != nil {
		t.Fatalf("agent create failed: %v", err)
	}

	// Create an open writ.
	writOut, err := runGT(t, gtHome, "writ", "create", "--world=tetherworld3", "--title=outpost tether")
	if err != nil {
		t.Fatalf("writ create failed: %v: %s", err, writOut)
	}
	writID := strings.TrimSpace(writOut)

	// Tether should fail because outpost agents must use cast.
	out, err := runGT(t, gtHome, "tether", writID, "--agent=Smoke", "--world=tetherworld3")
	if err == nil {
		t.Fatalf("expected error for outpost tether, got success: %s", out)
	}
	if !strings.Contains(out, "outpost") || !strings.Contains(out, "cast") {
		t.Errorf("expected error mentioning outpost/cast, got: %s", out)
	}
}

func TestCLITetherNonexistentWrit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "tetherworld4", sourceRepo)

	createEnvoy(t, gtHome, "tetherworld4", "Scout")

	out, err := runGT(t, gtHome, "tether", "sol-0000000000000000", "--agent=Scout", "--world=tetherworld4")
	if err == nil {
		t.Fatalf("expected error for nonexistent writ, got success: %s", out)
	}
	// Error should reference the writ ID.
	if !strings.Contains(out, "sol-0000000000000000") {
		t.Errorf("expected error mentioning writ ID, got: %s", out)
	}
}

// =============================================================================
// sol untether — CLI integration tests
// =============================================================================

func TestCLIUntetherHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "untether", "--help")
	if err != nil {
		t.Fatalf("sol untether --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Unbind a") {
		t.Errorf("help output missing expected text: %s", out)
	}
	if !strings.Contains(out, "--agent") {
		t.Errorf("help output missing --agent flag: %s", out)
	}
}

func TestCLIUntetherMissingArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "untether", "--agent=Scout")
	if err == nil {
		t.Fatalf("expected error for missing writ-id arg, got success: %s", out)
	}
	if !strings.Contains(out, "accepts 1 arg") {
		t.Errorf("expected arg count error, got: %s", out)
	}
}

func TestCLIUntetherMissingAgentFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "untether", "sol-0000000000000000")
	if err == nil {
		t.Fatalf("expected error for missing --agent flag, got success: %s", out)
	}
	if !strings.Contains(out, "required flag") && !strings.Contains(out, "agent") {
		t.Errorf("expected required-flag error mentioning agent, got: %s", out)
	}
}

func TestCLIUntetherHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "untetherworld", sourceRepo)

	// Create envoy and writ.
	createEnvoy(t, gtHome, "untetherworld", "Scout")

	writOut, err := runGT(t, gtHome, "writ", "create", "--world=untetherworld", "--title=untether test")
	if err != nil {
		t.Fatalf("writ create failed: %v: %s", err, writOut)
	}
	writID := strings.TrimSpace(writOut)

	// First tether.
	_, err = runGT(t, gtHome, "tether", writID, "--agent=Scout", "--world=untetherworld")
	if err != nil {
		t.Fatalf("tether failed: %v", err)
	}

	// Now untether.
	out, err := runGT(t, gtHome, "untether", writID, "--agent=Scout", "--world=untetherworld")
	if err != nil {
		t.Fatalf("sol untether failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Untethered") {
		t.Errorf("untether output missing 'Untethered': %s", out)
	}
	if !strings.Contains(out, "Scout") {
		t.Errorf("untether output missing agent name: %s", out)
	}
	if !strings.Contains(out, writID) {
		t.Errorf("untether output missing writ ID: %s", out)
	}

	// Verify writ reverted to open.
	worldStore, _ := openStores(t, "untetherworld")
	item, err := worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("get writ: %v", err)
	}
	if item.Status != "open" {
		t.Errorf("expected writ status 'open' after untether, got %q", item.Status)
	}
}

func TestCLIUntetherJSONOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "untetherworld2", sourceRepo)

	createEnvoy(t, gtHome, "untetherworld2", "Scout")

	writOut, err := runGT(t, gtHome, "writ", "create", "--world=untetherworld2", "--title=json untether")
	if err != nil {
		t.Fatalf("writ create failed: %v: %s", err, writOut)
	}
	writID := strings.TrimSpace(writOut)

	// Tether first.
	_, err = runGT(t, gtHome, "tether", writID, "--agent=Scout", "--world=untetherworld2")
	if err != nil {
		t.Fatalf("tether failed: %v", err)
	}

	// Untether with JSON output.
	out, err := runGT(t, gtHome, "untether", writID, "--agent=Scout", "--world=untetherworld2", "--json")
	if err != nil {
		t.Fatalf("sol untether --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("untether --json output is not valid JSON: %s", out)
	}
}

func TestCLIUntetherNotTethered(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "untetherworld3", sourceRepo)

	createEnvoy(t, gtHome, "untetherworld3", "Scout")

	writOut, err := runGT(t, gtHome, "writ", "create", "--world=untetherworld3", "--title=not tethered")
	if err != nil {
		t.Fatalf("writ create failed: %v: %s", err, writOut)
	}
	writID := strings.TrimSpace(writOut)

	// Untether without prior tether should fail.
	out, err := runGT(t, gtHome, "untether", writID, "--agent=Scout", "--world=untetherworld3")
	if err == nil {
		t.Fatalf("expected error for untether of non-tethered writ, got success: %s", out)
	}
	if !strings.Contains(out, "not tethered") {
		t.Errorf("expected 'not tethered' error, got: %s", out)
	}
}

// =============================================================================
// sol resolve — non-code writ path
// =============================================================================

func TestCLIResolveNonCodeWrit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "resolvenc", sourceRepo)

	worldStore, sphereStore := openStores(t, "resolvenc")

	// Create an envoy agent (persistent — session stays alive).
	createEnvoy(t, gtHome, "resolvenc", "Scout")

	// Create an analysis writ (non-code).
	writID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:       "Analysis task",
		Description: "Non-code writ for testing resolve",
		CreatedBy:   "autarch",
		Priority:    2,
		Kind:        "analysis",
	})
	if err != nil {
		t.Fatalf("create analysis writ: %v", err)
	}

	// Manually set up the tether: update agent state, writ status, write tether file.
	agentID := "resolvenc/Scout"
	if err := sphereStore.UpdateAgentState(agentID, "working", writID); err != nil {
		t.Fatalf("update agent state: %v", err)
	}
	if err := worldStore.UpdateWrit(writID, store.WritUpdates{
		Status:   "tethered",
		Assignee: agentID,
	}); err != nil {
		t.Fatalf("update writ: %v", err)
	}
	if err := tether.Write("resolvenc", "Scout", writID, "envoy"); err != nil {
		t.Fatalf("tether.Write: %v", err)
	}

	// Set up envoy worktree for resolve (envoy create already made the worktree).
	envoyWorktree := filepath.Join(gtHome, "resolvenc", "envoys", "Scout", "worktree")
	gitRun(t, envoyWorktree, "config", "user.email", "test@test.com")
	gitRun(t, envoyWorktree, "config", "user.name", "Test")

	// Close stores before CLI invocation (CLI opens its own).
	worldStore.Close()
	sphereStore.Close()

	// Resolve the non-code writ.
	out, err := runGT(t, gtHome, "resolve", "--world=resolvenc", "--agent=Scout")
	if err != nil {
		t.Fatalf("sol resolve failed: %v: %s", err, out)
	}

	// Verify output shows "Done:" with the writ ID.
	if !strings.Contains(out, "Done:") {
		t.Errorf("resolve output missing 'Done:': %s", out)
	}
	if !strings.Contains(out, writID) {
		t.Errorf("resolve output missing writ ID: %s", out)
	}

	// Non-code writs should NOT have a branch or merge request line.
	if strings.Contains(out, "Branch:") {
		t.Errorf("non-code resolve should not show Branch line: %s", out)
	}
	if strings.Contains(out, "Merge request:") {
		t.Errorf("non-code resolve should not show Merge request line: %s", out)
	}

	// Verify writ is closed (not "done" — non-code writs close directly).
	worldStore2, _ := openStores(t, "resolvenc")
	item, err := worldStore2.GetWrit(writID)
	if err != nil {
		t.Fatalf("get writ: %v", err)
	}
	if item.Status != "closed" {
		t.Errorf("expected writ status 'closed' for non-code resolve, got %q", item.Status)
	}
}

func TestCLIResolveNonCodeWritJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "resolvenc2", sourceRepo)

	worldStore, sphereStore := openStores(t, "resolvenc2")

	createEnvoy(t, gtHome, "resolvenc2", "Scout")

	writID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:       "Analysis JSON test",
		Description: "Non-code writ for JSON resolve test",
		CreatedBy:   "autarch",
		Priority:    2,
		Kind:        "analysis",
	})
	if err != nil {
		t.Fatalf("create analysis writ: %v", err)
	}

	agentID := "resolvenc2/Scout"
	if err := sphereStore.UpdateAgentState(agentID, "working", writID); err != nil {
		t.Fatalf("update agent state: %v", err)
	}
	if err := worldStore.UpdateWrit(writID, store.WritUpdates{
		Status:   "tethered",
		Assignee: agentID,
	}); err != nil {
		t.Fatalf("update writ: %v", err)
	}
	if err := tether.Write("resolvenc2", "Scout", writID, "envoy"); err != nil {
		t.Fatalf("tether.Write: %v", err)
	}

	envoyWorktree := filepath.Join(gtHome, "resolvenc2", "envoys", "Scout", "worktree")
	gitRun(t, envoyWorktree, "config", "user.email", "test@test.com")
	gitRun(t, envoyWorktree, "config", "user.name", "Test")

	worldStore.Close()
	sphereStore.Close()

	// Resolve with JSON output.
	out, err := runGT(t, gtHome, "resolve", "--world=resolvenc2", "--agent=Scout", "--json")
	if err != nil {
		t.Fatalf("sol resolve --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("resolve --json output is not valid JSON: %s", out)
	}

	// JSON should contain the writ ID and kind.
	if !strings.Contains(out, writID) {
		t.Errorf("JSON output missing writ ID: %s", out)
	}
	if !strings.Contains(out, "analysis") {
		t.Errorf("JSON output missing kind 'analysis': %s", out)
	}

	// Non-code resolve should NOT have merge_request or branch in JSON.
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to unmarshal resolve JSON: %v", err)
	}
	if mr, ok := result["merge_request_id"]; ok && mr != "" {
		t.Errorf("non-code resolve JSON should not have merge_request_id, got: %v", mr)
	}
}

func TestCLIResolveNonCodeViaWritCreateKind(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "resolvenc3", sourceRepo)

	// Create analysis writ via CLI (tests --kind flag parsing end-to-end).
	writOut, err := runGT(t, gtHome, "writ", "create", "--world=resolvenc3", "--title=CLI analysis", "--kind=analysis")
	if err != nil {
		t.Fatalf("writ create --kind=analysis failed: %v: %s", err, writOut)
	}
	writID := strings.TrimSpace(writOut)

	// Verify the writ was created with analysis kind.
	worldStore, sphereStore := openStores(t, "resolvenc3")
	item, err := worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("get writ: %v", err)
	}
	if item.Kind != "analysis" {
		t.Errorf("expected writ kind 'analysis', got %q", item.Kind)
	}

	// Create envoy and set up tether for resolve.
	createEnvoy(t, gtHome, "resolvenc3", "Scout")

	agentID := "resolvenc3/Scout"
	if err := sphereStore.UpdateAgentState(agentID, "working", writID); err != nil {
		t.Fatalf("update agent state: %v", err)
	}
	if err := worldStore.UpdateWrit(writID, store.WritUpdates{
		Status:   "tethered",
		Assignee: agentID,
	}); err != nil {
		t.Fatalf("update writ: %v", err)
	}
	if err := tether.Write("resolvenc3", "Scout", writID, "envoy"); err != nil {
		t.Fatalf("tether.Write: %v", err)
	}

	envoyWorktree := filepath.Join(gtHome, "resolvenc3", "envoys", "Scout", "worktree")
	gitRun(t, envoyWorktree, "config", "user.email", "test@test.com")
	gitRun(t, envoyWorktree, "config", "user.name", "Test")

	worldStore.Close()
	sphereStore.Close()

	// Resolve.
	out, err := runGT(t, gtHome, "resolve", "--world=resolvenc3", "--agent=Scout")
	if err != nil {
		t.Fatalf("sol resolve failed: %v: %s", err, out)
	}

	// Verify closed (not done).
	worldStore2, _ := openStores(t, "resolvenc3")
	item, err = worldStore2.GetWrit(writID)
	if err != nil {
		t.Fatalf("get writ: %v", err)
	}
	if item.Status != "closed" {
		t.Errorf("expected writ status 'closed', got %q", item.Status)
	}

	// No branch or MR output.
	if strings.Contains(out, "Branch:") {
		t.Errorf("non-code resolve should not show Branch: %s", out)
	}
	if strings.Contains(out, "Merge request:") {
		t.Errorf("non-code resolve should not show Merge request: %s", out)
	}
}

// =============================================================================
// sol resolve — missing agent flag error
// =============================================================================

func TestCLIResolveMissingAgent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "resolvenoagent")

	// Resolve without --agent and without SOL_AGENT env var should fail.
	out, err := runGT(t, gtHome, "resolve", "--world=resolvenoagent")
	if err == nil {
		t.Fatalf("expected error for resolve without agent, got success: %s", out)
	}
	if !strings.Contains(out, "agent") {
		t.Errorf("expected error mentioning agent, got: %s", out)
	}
}

// =============================================================================
// Tether → Untether round-trip with agent state verification
// =============================================================================

func TestCLITetherUntetherRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "roundtrip", sourceRepo)

	createEnvoy(t, gtHome, "roundtrip", "Scout")

	writOut, err := runGT(t, gtHome, "writ", "create", "--world=roundtrip", "--title=roundtrip test")
	if err != nil {
		t.Fatalf("writ create failed: %v: %s", err, writOut)
	}
	writID := strings.TrimSpace(writOut)

	// Tether.
	out, err := runGT(t, gtHome, "tether", writID, "--agent=Scout", "--world=roundtrip")
	if err != nil {
		t.Fatalf("tether failed: %v: %s", err, out)
	}

	// Verify agent state is "working" via agent list --json.
	out, err = runGT(t, gtHome, "agent", "list", "--world=roundtrip", "--json")
	if err != nil {
		t.Fatalf("agent list failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "working") {
		t.Errorf("expected agent state 'working' after tether, got: %s", out)
	}

	// Untether.
	out, err = runGT(t, gtHome, "untether", writID, "--agent=Scout", "--world=roundtrip")
	if err != nil {
		t.Fatalf("untether failed: %v: %s", err, out)
	}

	// Verify agent state is "idle" via agent list --json.
	out, err = runGT(t, gtHome, "agent", "list", "--world=roundtrip", "--json")
	if err != nil {
		t.Fatalf("agent list failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "idle") {
		t.Errorf("expected agent state 'idle' after untether, got: %s", out)
	}

	// Verify writ is back to open.
	worldStore, _ := openStores(t, "roundtrip")
	item, err := worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("get writ: %v", err)
	}
	if item.Status != "open" {
		t.Errorf("expected writ status 'open', got %q", item.Status)
	}
}
