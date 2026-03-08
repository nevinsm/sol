package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

func TestForgeStatusEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "statustest")

	out, err := runGT(t, gtHome, "forge", "status", "statustest")
	if err != nil {
		t.Fatalf("forge status failed: %v: %s", err, out)
	}

	if !strings.Contains(out, "Forge: statustest") {
		t.Errorf("expected header, got: %s", out)
	}
	if !strings.Contains(out, "stopped") {
		t.Errorf("expected 'stopped' in output, got: %s", out)
	}
	if !strings.Contains(out, "0 ready") {
		t.Errorf("expected '0 ready' in output, got: %s", out)
	}
}

func TestForgeStatusWithMRs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "statusmrs")

	worldStore, err := store.OpenWorld("statusmrs")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	defer worldStore.Close()

	// Create a writ.
	itemID, err := worldStore.CreateWrit("Test feature", "A test feature", "test", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}

	// Create MRs in various states.
	mrReady, err := worldStore.CreateMergeRequest(itemID, "feature/ready", 2)
	if err != nil {
		t.Fatalf("create MR: %v", err)
	}
	_ = mrReady

	mrMerged, err := worldStore.CreateMergeRequest(itemID, "feature/merged", 2)
	if err != nil {
		t.Fatalf("create MR: %v", err)
	}
	if err := worldStore.UpdateMergeRequestPhase(mrMerged, "merged"); err != nil {
		t.Fatalf("update MR phase: %v", err)
	}

	mrFailed, err := worldStore.CreateMergeRequest(itemID, "feature/failed", 2)
	if err != nil {
		t.Fatalf("create MR: %v", err)
	}
	if err := worldStore.UpdateMergeRequestPhase(mrFailed, "failed"); err != nil {
		t.Fatalf("update MR phase: %v", err)
	}

	out, err := runGT(t, gtHome, "forge", "status", "statusmrs")
	if err != nil {
		t.Fatalf("forge status failed: %v: %s", err, out)
	}

	if !strings.Contains(out, "1 ready") {
		t.Errorf("expected '1 ready', got: %s", out)
	}
	if !strings.Contains(out, "1 failed") {
		t.Errorf("expected '1 failed', got: %s", out)
	}
	if !strings.Contains(out, "1 merged") {
		t.Errorf("expected '1 merged', got: %s", out)
	}
	if !strings.Contains(out, "Last merge:") {
		t.Errorf("expected 'Last merge:', got: %s", out)
	}
	if !strings.Contains(out, "Last failure:") {
		t.Errorf("expected 'Last failure:', got: %s", out)
	}
}

func TestForgeStatusJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "statusjson")

	worldStore, err := store.OpenWorld("statusjson")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	defer worldStore.Close()

	// Create a writ and a ready MR.
	itemID, err := worldStore.CreateWrit("JSON test", "Test JSON output", "test", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}
	if _, err := worldStore.CreateMergeRequest(itemID, "feature/json", 2); err != nil {
		t.Fatalf("create MR: %v", err)
	}

	out, err := runGT(t, gtHome, "forge", "status", "statusjson", "--json")
	if err != nil {
		t.Fatalf("forge status --json failed: %v: %s", err, out)
	}

	var result struct {
		World       string `json:"world"`
		Running     bool   `json:"running"`
		SessionName string `json:"session_name"`
		Ready       int    `json:"ready"`
		Total       int    `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput: %s", err, out)
	}

	if result.World != "statusjson" {
		t.Errorf("expected world=statusjson, got %q", result.World)
	}
	if result.Running {
		t.Error("expected running=false")
	}
	if result.Ready != 1 {
		t.Errorf("expected ready=1, got %d", result.Ready)
	}
	if result.Total != 1 {
		t.Errorf("expected total=1, got %d", result.Total)
	}
}

func TestForgeStatusClaimed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "statusclaim")

	worldStore, err := store.OpenWorld("statusclaim")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	defer worldStore.Close()

	// Create a writ and a MR, then claim it.
	itemID, err := worldStore.CreateWrit("Claimed task", "Testing claimed display", "test", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}
	if _, err := worldStore.CreateMergeRequest(itemID, "feature/claimed", 1); err != nil {
		t.Fatalf("create MR: %v", err)
	}

	mr, err := worldStore.ClaimMergeRequest("test-forge")
	if err != nil {
		t.Fatalf("claim MR: %v", err)
	}
	if mr == nil {
		t.Fatal("expected to claim a MR, got nil")
	}

	out, err := runGT(t, gtHome, "forge", "status", "statusclaim")
	if err != nil {
		t.Fatalf("forge status failed: %v: %s", err, out)
	}

	if !strings.Contains(out, "1 in-progress") {
		t.Errorf("expected '1 in-progress', got: %s", out)
	}
	if !strings.Contains(out, "Claimed:") {
		t.Errorf("expected 'Claimed:', got: %s", out)
	}
	if !strings.Contains(out, "feature/claimed") {
		t.Errorf("expected branch name in output, got: %s", out)
	}
	if !strings.Contains(out, "Claimed task") {
		t.Errorf("expected writ title in output, got: %s", out)
	}

	// Also verify the JSON output includes claimed_mr.
	jsonOut, err := runGT(t, gtHome, "forge", "status", "statusclaim", "--json")
	if err != nil {
		t.Fatalf("forge status --json failed: %v: %s", err, jsonOut)
	}

	var result struct {
		InProgress int `json:"in_progress"`
		ClaimedMR  *struct {
			Title  string `json:"title"`
			Branch string `json:"branch"`
		} `json:"claimed_mr"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result.InProgress != 1 {
		t.Errorf("expected in_progress=1, got %d", result.InProgress)
	}
	if result.ClaimedMR == nil {
		t.Fatal("expected claimed_mr in JSON output")
	}
	if result.ClaimedMR.Title != "Claimed task" {
		t.Errorf("expected title='Claimed task', got %q", result.ClaimedMR.Title)
	}

}

func TestForgeStatusInvalidWorld(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnv(t)

	_, err := runGT(t, gtHome, "forge", "status", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent world")
	}
}

func TestForgeCreateResolutionDispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "resdisp", sourceRepo)

	worldStore, _ := openStores(t, "resdisp")

	// Create a writ and MR to resolve.
	writID, err := worldStore.CreateWrit("Test feature", "A test feature", "test", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}
	mrID, err := worldStore.CreateMergeRequest(writID, "feature/conflict", 2)
	if err != nil {
		t.Fatalf("create MR: %v", err)
	}

	// Run create-resolution — dispatch auto-provisions an agent and dispatches.
	out, err := runGT(t, gtHome, "forge", "create-resolution", mrID, "--world=resdisp")
	if err != nil {
		t.Fatalf("forge create-resolution failed: %v: %s", err, out)
	}

	// Verify the resolution task was created.
	if !strings.Contains(out, "Created resolution task:") {
		t.Errorf("expected 'Created resolution task:', got: %s", out)
	}

	// Verify the MR is now blocked.
	if !strings.Contains(out, "is now blocked") {
		t.Errorf("expected 'is now blocked' in output, got: %s", out)
	}

	// Verify dispatch was attempted — either succeeded or warned.
	dispatched := strings.Contains(out, "Dispatched")
	warned := strings.Contains(out, "auto-dispatch failed")
	if !dispatched && !warned {
		t.Errorf("expected dispatch attempt (success or warning), got: %s", out)
	}
}

func TestForgeCreateResolutionDispatchJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "resdispjson", sourceRepo)

	worldStore, _ := openStores(t, "resdispjson")

	// Create a writ and MR to resolve.
	writID, err := worldStore.CreateWrit("Test feature", "A test feature", "test", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}
	mrID, err := worldStore.CreateMergeRequest(writID, "feature/conflict", 2)
	if err != nil {
		t.Fatalf("create MR: %v", err)
	}

	// Run create-resolution with --json — dispatch auto-provisions an agent.
	out, err := runGT(t, gtHome, "forge", "create-resolution", mrID, "--world=resdispjson", "--json")
	if err != nil {
		t.Fatalf("forge create-resolution --json failed: %v: %s", err, out)
	}

	// Extract the printJSON output (multi-line indented JSON) from combined
	// output which may also contain single-line slog JSON log entries.
	// The printJSON output starts with "{\n  " on its own line.
	jsonStart := strings.LastIndex(out, "\n{")
	if jsonStart < 0 {
		// Maybe it's the first thing in output.
		if strings.HasPrefix(out, "{") {
			jsonStart = -1 // will add 1 below
		} else {
			t.Fatalf("no JSON found in output: %s", out)
		}
	}
	jsonStr := out[jsonStart+1:]
	// Trim anything after the closing brace (e.g., trailing warnings).
	if end := strings.Index(jsonStr, "\n}"); end >= 0 {
		jsonStr = jsonStr[:end+2]
	}

	var result map[string]string
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nraw: %s", err, jsonStr)
	}

	if result["mr_id"] != mrID {
		t.Errorf("expected mr_id=%s, got %s", mrID, result["mr_id"])
	}
	if result["task_id"] == "" {
		t.Error("expected non-empty task_id in JSON output")
	}

	// When dispatch succeeds, dispatched_agent should be present.
	if agent := result["dispatched_agent"]; agent != "" {
		t.Logf("dispatch succeeded: agent=%s", agent)
	} else {
		// If dispatch failed, verify the warning was emitted.
		if !strings.Contains(out, "auto-dispatch failed") {
			t.Errorf("expected either dispatched_agent in JSON or warning in output, got: %s", out)
		}
	}
}
