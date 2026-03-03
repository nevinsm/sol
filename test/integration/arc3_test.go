package integration

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/store"
)

// =============================================================================
// Schema V7 — Caravan Phase Tests
// =============================================================================

func TestCaravanPhaseCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "myworld")

	// Create two work items.
	id1, err := runGT(t, gtHome, "store", "create", "--world=myworld", "--title=Phase 0 task")
	if err != nil {
		t.Fatalf("store create: %v: %s", err, id1)
	}
	id2, err := runGT(t, gtHome, "store", "create", "--world=myworld", "--title=Phase 1 task")
	if err != nil {
		t.Fatalf("store create: %v: %s", err, id2)
	}

	// Create caravan with phase 0 item.
	out, err := runGT(t, gtHome, "caravan", "create", "phased", id1, "--world=myworld", "--phase=0")
	if err != nil {
		t.Fatalf("caravan create: %v: %s", err, out)
	}
	// Extract caravan ID.
	caravanID := extractCaravanID(t, out)

	// Add phase 1 item.
	out, err = runGT(t, gtHome, "caravan", "add", caravanID, id2, "--world=myworld", "--phase=1")
	if err != nil {
		t.Fatalf("caravan add: %v: %s", err, out)
	}

	// Check readiness via JSON.
	out, err = runGT(t, gtHome, "caravan", "check", caravanID, "--json")
	if err != nil {
		t.Fatalf("caravan check: %v: %s", err, out)
	}

	var checkResult struct {
		Items []struct {
			WorkItemID string `json:"work_item_id"`
			Phase      int    `json:"phase"`
			Ready      bool   `json:"ready"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &checkResult); err != nil {
		t.Fatalf("parse caravan check JSON: %v: %s", err, out)
	}

	if len(checkResult.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(checkResult.Items))
	}

	// Find items by phase.
	for _, item := range checkResult.Items {
		switch item.Phase {
		case 0:
			if !item.Ready {
				t.Error("phase 0 item should be ready")
			}
		case 1:
			if item.Ready {
				t.Error("phase 1 item should NOT be ready (phase 0 not done)")
			}
		}
	}
}

func TestCaravanPhaseOrdering(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "myworld")

	// Create items.
	id1, err := runGT(t, gtHome, "store", "create", "--world=myworld", "--title=Phase 0 task")
	if err != nil {
		t.Fatalf("store create: %v: %s", err, id1)
	}
	id2, err := runGT(t, gtHome, "store", "create", "--world=myworld", "--title=Phase 1 task")
	if err != nil {
		t.Fatalf("store create: %v: %s", err, id2)
	}

	// Create caravan with both phases.
	out, err := runGT(t, gtHome, "caravan", "create", "phased", id1, "--world=myworld", "--phase=0")
	if err != nil {
		t.Fatalf("caravan create: %v: %s", err, out)
	}
	caravanID := extractCaravanID(t, out)

	out, err = runGT(t, gtHome, "caravan", "add", caravanID, id2, "--world=myworld", "--phase=1")
	if err != nil {
		t.Fatalf("caravan add: %v: %s", err, out)
	}

	// Mark phase 0 item as done via store.
	_, sphereStore := openStores(t, "myworld")
	_ = sphereStore
	worldStore, err := store.OpenWorld("myworld")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	defer worldStore.Close()

	if err := worldStore.UpdateWorkItem(id1, store.WorkItemUpdates{Status: "done"}); err != nil {
		t.Fatalf("update work item: %v", err)
	}

	// Check readiness again.
	out, err = runGT(t, gtHome, "caravan", "check", caravanID, "--json")
	if err != nil {
		t.Fatalf("caravan check: %v: %s", err, out)
	}

	var checkResult struct {
		Items []struct {
			WorkItemID     string `json:"work_item_id"`
			Phase          int    `json:"phase"`
			Ready          bool   `json:"ready"`
			WorkItemStatus string `json:"work_item_status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &checkResult); err != nil {
		t.Fatalf("parse caravan check JSON: %v: %s", err, out)
	}

	for _, item := range checkResult.Items {
		if item.Phase == 1 {
			if !item.Ready {
				t.Error("phase 1 item should be ready now that phase 0 is done")
			}
		}
	}
}

func TestCaravanPhaseBackwardCompat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "myworld")

	id1, err := runGT(t, gtHome, "store", "create", "--world=myworld", "--title=Task A")
	if err != nil {
		t.Fatalf("store create: %v: %s", err, id1)
	}
	id2, err := runGT(t, gtHome, "store", "create", "--world=myworld", "--title=Task B")
	if err != nil {
		t.Fatalf("store create: %v: %s", err, id2)
	}

	// Create caravan without explicit phase — should default to 0.
	out, err := runGT(t, gtHome, "caravan", "create", "no-phases", id1, id2, "--world=myworld")
	if err != nil {
		t.Fatalf("caravan create: %v: %s", err, out)
	}
	caravanID := extractCaravanID(t, out)

	out, err = runGT(t, gtHome, "caravan", "check", caravanID, "--json")
	if err != nil {
		t.Fatalf("caravan check: %v: %s", err, out)
	}

	var checkResult struct {
		Items []struct {
			Phase int  `json:"phase"`
			Ready bool `json:"ready"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &checkResult); err != nil {
		t.Fatalf("parse caravan check JSON: %v: %s", err, out)
	}

	for i, item := range checkResult.Items {
		if item.Phase != 0 {
			t.Errorf("item %d: expected phase 0, got %d", i, item.Phase)
		}
		if !item.Ready {
			t.Errorf("item %d: expected ready (all phase 0, no deps)", i)
		}
	}
}

// =============================================================================
// Brief System Tests
// =============================================================================

func TestBriefInjectEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	// Create brief directory and file.
	briefDir := filepath.Join(gtHome, "test-brief")
	os.MkdirAll(briefDir, 0o755)
	briefPath := filepath.Join(briefDir, "memory.md")
	if err := os.WriteFile(briefPath, []byte("# Test Brief\nSome context here.\n"), 0o644); err != nil {
		t.Fatalf("write brief: %v", err)
	}

	out, err := runGT(t, gtHome, "brief", "inject", "--path="+briefPath)
	if err != nil {
		t.Fatalf("brief inject: %v: %s", err, out)
	}

	// Verify framed output.
	if !strings.Contains(out, "<brief>") {
		t.Errorf("output missing <brief> tag: %s", out)
	}
	if !strings.Contains(out, "</brief>") {
		t.Errorf("output missing </brief> tag: %s", out)
	}
	if !strings.Contains(out, "Test Brief") {
		t.Errorf("output missing brief content: %s", out)
	}

	// Verify .session_start created.
	sessionStartPath := filepath.Join(briefDir, ".session_start")
	if _, err := os.Stat(sessionStartPath); os.IsNotExist(err) {
		t.Error(".session_start not created")
	}
}

func TestBriefCheckSaveEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	briefDir := filepath.Join(gtHome, "test-brief")
	os.MkdirAll(briefDir, 0o755)
	briefPath := filepath.Join(briefDir, "memory.md")

	// Write session start.
	sessionStartPath := filepath.Join(briefDir, ".session_start")
	ts := time.Now().Add(-1 * time.Minute).UTC().Format(time.RFC3339)
	if err := os.WriteFile(sessionStartPath, []byte(ts), 0o644); err != nil {
		t.Fatalf("write session start: %v", err)
	}

	// Write brief AFTER session start.
	time.Sleep(10 * time.Millisecond) // Ensure mtime is after session_start
	if err := os.WriteFile(briefPath, []byte("# Updated Brief\n"), 0o644); err != nil {
		t.Fatalf("write brief: %v", err)
	}

	// Check-save should exit 0 (brief was updated).
	out, err := runGT(t, gtHome, "brief", "check-save", briefPath)
	if err != nil {
		t.Errorf("check-save should exit 0 when brief is updated: %v: %s", err, out)
	}

	// Now write session_start with a future timestamp (newer than brief mtime).
	futureTS := time.Now().Add(1 * time.Minute).UTC().Format(time.RFC3339)
	if err := os.WriteFile(sessionStartPath, []byte(futureTS), 0o644); err != nil {
		t.Fatalf("write session start: %v", err)
	}

	// Check-save should exit 1 (brief NOT updated since new session start).
	out, err = runGT(t, gtHome, "brief", "check-save", briefPath)
	if err == nil {
		t.Error("check-save should exit non-zero when brief is NOT updated")
	}
	if !strings.Contains(out, "brief has not been updated") {
		t.Errorf("expected nudge message, got: %s", out)
	}
}

func TestBriefInjectTruncation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	briefDir := filepath.Join(gtHome, "test-brief")
	os.MkdirAll(briefDir, 0o755)
	briefPath := filepath.Join(briefDir, "memory.md")

	// Create 300-line brief.
	var lines []string
	for i := 0; i < 300; i++ {
		lines = append(lines, fmt.Sprintf("Line %d: some content here", i+1))
	}
	if err := os.WriteFile(briefPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write brief: %v", err)
	}

	out, err := runGT(t, gtHome, "brief", "inject", "--path="+briefPath)
	if err != nil {
		t.Fatalf("brief inject: %v: %s", err, out)
	}

	if !strings.Contains(out, "TRUNCATED") {
		t.Errorf("expected truncation notice in output: %s", out)
	}
}

func TestBriefInjectMissingFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	briefDir := filepath.Join(gtHome, "test-brief")
	os.MkdirAll(briefDir, 0o755)
	nonexistentPath := filepath.Join(briefDir, "does-not-exist.md")

	out, err := runGT(t, gtHome, "brief", "inject", "--path="+nonexistentPath)
	if err != nil {
		t.Errorf("brief inject on missing file should not error: %v: %s", err, out)
	}
}

func TestBriefStopHookActiveBypass(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	briefDir := filepath.Join(gtHome, "test-brief")
	os.MkdirAll(briefDir, 0o755)
	briefPath := filepath.Join(briefDir, "memory.md")

	// Write session_start (recent).
	sessionStartPath := filepath.Join(briefDir, ".session_start")
	ts := time.Now().UTC().Format(time.RFC3339)
	if err := os.WriteFile(sessionStartPath, []byte(ts), 0o644); err != nil {
		t.Fatalf("write session start: %v", err)
	}

	// Write brief BEFORE session start (will be "not updated").
	oldTs := time.Now().Add(-1 * time.Hour)
	if err := os.WriteFile(briefPath, []byte("# Old Brief\n"), 0o644); err != nil {
		t.Fatalf("write brief: %v", err)
	}
	os.Chtimes(briefPath, oldTs, oldTs)

	// Run with SOL_STOP_HOOK_ACTIVE=true — should bypass and exit 0.
	bin := gtBin(t)
	cmd := exec.Command(bin, "brief", "check-save", briefPath)
	cmd.Env = append(os.Environ(), "SOL_HOME="+gtHome, "SOL_STOP_HOOK_ACTIVE=true")
	outBytes, err := cmd.CombinedOutput()
	out := strings.TrimSpace(string(outBytes))
	if err != nil {
		t.Errorf("check-save with SOL_STOP_HOOK_ACTIVE=true should exit 0: %v: %s", err, out)
	}
}

// =============================================================================
// Envoy Lifecycle Tests
// =============================================================================

func TestEnvoyCreateAndList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)

	// Create envoy.
	out, err := runGT(t, gtHome, "envoy", "create", "scout", "--world=myworld")
	if err != nil {
		t.Fatalf("envoy create: %v: %s", err, out)
	}
	if !strings.Contains(out, "Created envoy") {
		t.Errorf("expected success message: %s", out)
	}

	// Verify envoy directory exists.
	envoyDir := filepath.Join(gtHome, "myworld", "envoys", "scout")
	if _, err := os.Stat(envoyDir); os.IsNotExist(err) {
		t.Error("envoy directory not created")
	}

	// Verify agent record has role=envoy.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	agent, err := sphereStore.GetAgent("myworld/scout")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if agent.Role != "envoy" {
		t.Errorf("expected role 'envoy', got %q", agent.Role)
	}

	// List envoys.
	out, err = runGT(t, gtHome, "envoy", "list", "--world=myworld")
	if err != nil {
		t.Fatalf("envoy list: %v: %s", err, out)
	}
	if !strings.Contains(out, "scout") {
		t.Errorf("envoy list missing 'scout': %s", out)
	}
}

func TestEnvoyStartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	// Check tmux availability.
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)

	createEnvoy(t, gtHome, "myworld", "scout")

	// Start envoy.
	out, err := runGT(t, gtHome, "envoy", "start", "scout", "--world=myworld")
	if err != nil {
		t.Fatalf("envoy start: %v: %s", err, out)
	}

	// Verify session exists.
	ok := pollUntil(5*time.Second, 200*time.Millisecond, func() bool {
		return tmuxSessionExists("sol-myworld-scout")
	})
	if !ok {
		t.Error("envoy session not started")
	}

	// Stop envoy.
	out, err = runGT(t, gtHome, "envoy", "stop", "scout", "--world=myworld")
	if err != nil {
		t.Fatalf("envoy stop: %v: %s", err, out)
	}

	// Verify session gone.
	ok = pollUntil(5*time.Second, 200*time.Millisecond, func() bool {
		return !tmuxSessionExists("sol-myworld-scout")
	})
	if !ok {
		t.Error("envoy session not stopped")
	}
}

func TestEnvoyBriefAndDebrief(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)

	createEnvoy(t, gtHome, "myworld", "scout")

	// Write brief content.
	briefDir := filepath.Join(gtHome, "myworld", "envoys", "scout", ".brief")
	briefPath := filepath.Join(briefDir, "memory.md")
	if err := os.WriteFile(briefPath, []byte("# Scout Brief\nImportant context.\n"), 0o644); err != nil {
		t.Fatalf("write brief: %v", err)
	}

	// View brief.
	out, err := runGT(t, gtHome, "envoy", "brief", "scout", "--world=myworld")
	if err != nil {
		t.Fatalf("envoy brief: %v: %s", err, out)
	}
	if !strings.Contains(out, "Scout Brief") {
		t.Errorf("envoy brief output missing content: %s", out)
	}

	// Debrief — archive the brief.
	out, err = runGT(t, gtHome, "envoy", "debrief", "scout", "--world=myworld")
	if err != nil {
		t.Fatalf("envoy debrief: %v: %s", err, out)
	}
	if !strings.Contains(out, "Archived") {
		t.Errorf("expected archive message: %s", out)
	}

	// Verify archive directory has a file.
	archiveDir := filepath.Join(briefDir, "archive")
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("read archive dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("archive directory is empty")
	}

	// Verify memory.md is gone.
	if _, err := os.Stat(briefPath); !os.IsNotExist(err) {
		t.Error("memory.md should be gone after debrief")
	}
}

func TestEnvoyHooksInstalled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)

	createEnvoy(t, gtHome, "myworld", "scout")

	// Start envoy to trigger hook installation.
	out, err := runGT(t, gtHome, "envoy", "start", "scout", "--world=myworld")
	if err != nil {
		t.Fatalf("envoy start: %v: %s", err, out)
	}
	t.Cleanup(func() {
		runGT(t, gtHome, "envoy", "stop", "scout", "--world=myworld")
	})

	// Check settings.local.json in worktree.
	worktree := filepath.Join(gtHome, "myworld", "envoys", "scout", "worktree")
	settingsPath := filepath.Join(worktree, ".claude", "settings.local.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings.local.json: %v", err)
	}

	settingsStr := string(data)
	if !strings.Contains(settingsStr, "brief inject") {
		t.Errorf("settings.local.json missing brief inject hook: %s", settingsStr)
	}
	if !strings.Contains(settingsStr, "brief check-save") {
		t.Errorf("settings.local.json missing brief check-save hook: %s", settingsStr)
	}
	if !strings.Contains(settingsStr, "--skip-session-start") {
		t.Errorf("settings.local.json missing --skip-session-start in compact hook: %s", settingsStr)
	}
}

// =============================================================================
// Governor Lifecycle Tests
// =============================================================================

func TestGovernorStartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)

	// Start governor.
	out, err := runGT(t, gtHome, "governor", "start", "--world=myworld")
	if err != nil {
		t.Fatalf("governor start: %v: %s", err, out)
	}
	t.Cleanup(func() {
		runGT(t, gtHome, "governor", "stop", "--world=myworld")
	})

	// Verify session exists.
	ok := pollUntil(5*time.Second, 200*time.Millisecond, func() bool {
		return tmuxSessionExists("sol-myworld-governor")
	})
	if !ok {
		t.Error("governor session not started")
	}

	// Verify mirror directory exists with git repo.
	mirrorPath := filepath.Join(gtHome, "myworld", "governor", "mirror")
	if _, err := os.Stat(mirrorPath); os.IsNotExist(err) {
		t.Error("mirror directory not created")
	}
	// Verify it's a git repo.
	cmd := exec.Command("git", "-C", mirrorPath, "rev-parse", "--is-inside-work-tree")
	if _, err := cmd.CombinedOutput(); err != nil {
		t.Error("mirror is not a git repo")
	}

	// Stop governor.
	out, err = runGT(t, gtHome, "governor", "stop", "--world=myworld")
	if err != nil {
		t.Fatalf("governor stop: %v: %s", err, out)
	}

	// Verify session gone.
	ok = pollUntil(5*time.Second, 200*time.Millisecond, func() bool {
		return !tmuxSessionExists("sol-myworld-governor")
	})
	if !ok {
		t.Error("governor session not stopped")
	}

	// Mirror should persist after stop.
	if _, err := os.Stat(mirrorPath); os.IsNotExist(err) {
		t.Error("mirror should persist after governor stop")
	}
}

func TestGovernorRefreshMirror(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)

	// Start governor.
	out, err := runGT(t, gtHome, "governor", "start", "--world=myworld")
	if err != nil {
		t.Fatalf("governor start: %v: %s", err, out)
	}
	t.Cleanup(func() {
		runGT(t, gtHome, "governor", "stop", "--world=myworld")
	})

	// Add a commit to the managed clone (which is the mirror's origin).
	managedRepo := filepath.Join(gtHome, "myworld", "repo")
	if err := os.WriteFile(filepath.Join(managedRepo, "newfile.txt"), []byte("new content"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitRun(t, managedRepo, "add", ".")
	gitRun(t, managedRepo, "commit", "-m", "add newfile")

	// Refresh mirror.
	out, err = runGT(t, gtHome, "governor", "refresh-mirror", "--world=myworld")
	if err != nil {
		t.Fatalf("governor refresh-mirror: %v: %s", err, out)
	}

	// Verify new commit visible in mirror.
	mirrorPath := filepath.Join(gtHome, "myworld", "governor", "mirror")
	cmd := exec.Command("git", "-C", mirrorPath, "log", "--oneline", "-1")
	logOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log in mirror: %v: %s", err, logOut)
	}
	if !strings.Contains(string(logOut), "add newfile") {
		t.Errorf("mirror missing new commit: %s", logOut)
	}
}

func TestGovernorBriefAndSummary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "myworld")

	// Create governor directories manually (without starting a session).
	govDir := filepath.Join(gtHome, "myworld", "governor")
	briefDir := filepath.Join(govDir, ".brief")
	os.MkdirAll(briefDir, 0o755)

	// Write brief.
	briefPath := filepath.Join(briefDir, "memory.md")
	if err := os.WriteFile(briefPath, []byte("# Governor Brief\nWorld state summary.\n"), 0o644); err != nil {
		t.Fatalf("write brief: %v", err)
	}

	// Write world summary.
	summaryPath := filepath.Join(briefDir, "world-summary.md")
	if err := os.WriteFile(summaryPath, []byte("# World Summary\n5 agents active.\n"), 0o644); err != nil {
		t.Fatalf("write summary: %v", err)
	}

	// View brief.
	out, err := runGT(t, gtHome, "governor", "brief", "--world=myworld")
	if err != nil {
		t.Fatalf("governor brief: %v: %s", err, out)
	}
	if !strings.Contains(out, "Governor Brief") {
		t.Errorf("governor brief output missing content: %s", out)
	}

	// View summary.
	out, err = runGT(t, gtHome, "governor", "summary", "--world=myworld")
	if err != nil {
		t.Fatalf("governor summary: %v: %s", err, out)
	}
	if !strings.Contains(out, "World Summary") {
		t.Errorf("governor summary output missing content: %s", out)
	}
}

func TestGovernorHooksInstalled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)

	// Start governor.
	out, err := runGT(t, gtHome, "governor", "start", "--world=myworld")
	if err != nil {
		t.Fatalf("governor start: %v: %s", err, out)
	}
	t.Cleanup(func() {
		runGT(t, gtHome, "governor", "stop", "--world=myworld")
	})

	// Check hooks file.
	govDir := filepath.Join(gtHome, "myworld", "governor")
	settingsPath := filepath.Join(govDir, ".claude", "settings.local.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings.local.json: %v", err)
	}

	settingsStr := string(data)
	if !strings.Contains(settingsStr, "brief inject") {
		t.Errorf("hooks missing brief inject: %s", settingsStr)
	}
	if !strings.Contains(settingsStr, "refresh-mirror") {
		t.Errorf("hooks missing refresh-mirror: %s", settingsStr)
	}
	if !strings.Contains(settingsStr, "--skip-session-start") {
		t.Errorf("hooks missing --skip-session-start in compact hook: %s", settingsStr)
	}
}

// =============================================================================
// Resolve Behavior Tests
// =============================================================================

func TestResolveEnvoyKeepsSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)

	// Create envoy via CLI.
	out, err := runGT(t, gtHome, "envoy", "create", "scout", "--world=myworld")
	if err != nil {
		t.Fatalf("envoy create: %v: %s", err, out)
	}

	// Start envoy.
	out, err = runGT(t, gtHome, "envoy", "start", "scout", "--world=myworld")
	if err != nil {
		t.Fatalf("envoy start: %v: %s", err, out)
	}
	t.Cleanup(func() {
		runGT(t, gtHome, "envoy", "stop", "scout", "--world=myworld")
	})

	// Wait for session.
	ok := pollUntil(5*time.Second, 200*time.Millisecond, func() bool {
		return tmuxSessionExists("sol-myworld-scout")
	})
	if !ok {
		t.Fatal("envoy session not started")
	}

	// Create work item.
	worldStore, err := store.OpenWorld("myworld")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	defer worldStore.Close()

	itemID, err := worldStore.CreateWorkItem("Envoy task", "test", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	// Update agent state to working and set tether.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	if err := sphereStore.UpdateAgentState("myworld/scout", "working", itemID); err != nil {
		t.Fatalf("update agent state: %v", err)
	}

	// Write tether file.
	tetherDir := filepath.Join(gtHome, "myworld", "outposts", "scout")
	os.MkdirAll(tetherDir, 0o755)
	tetherPath := filepath.Join(tetherDir, ".tether")
	if err := os.WriteFile(tetherPath, []byte(itemID), 0o644); err != nil {
		t.Fatalf("write tether: %v", err)
	}

	// Update work item to tethered.
	if err := worldStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "tethered"}); err != nil {
		t.Fatalf("update work item: %v", err)
	}

	// Use the envoy worktree created by envoy create.
	envoyWorktree := filepath.Join(gtHome, "myworld", "envoys", "scout", "worktree")
	gitRun(t, envoyWorktree, "config", "user.email", "test@test.com")
	gitRun(t, envoyWorktree, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(envoyWorktree, "work.txt"), []byte("envoy work"), 0o644); err != nil {
		t.Fatalf("write work file: %v", err)
	}
	gitRun(t, envoyWorktree, "add", ".")
	gitRun(t, envoyWorktree, "commit", "-m", "envoy work commit")
	// Point origin to a bare remote for push.
	bareRemote := t.TempDir()
	gitRun(t, bareRemote, "init", "--bare")
	gitRun(t, envoyWorktree, "remote", "set-url", "origin", bareRemote)

	// Resolve.
	out, err = runGT(t, gtHome, "resolve", "--world=myworld", "--agent=scout")
	if err != nil {
		t.Fatalf("resolve: %v: %s", err, out)
	}

	// Verify session STILL RUNNING.
	time.Sleep(2 * time.Second) // Wait longer than the 1s delay for regular agents.
	if !tmuxSessionExists("sol-myworld-scout") {
		t.Error("envoy session should still be running after resolve")
	}

	// Verify work item done.
	item, err := worldStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("get work item: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("work item status: expected 'done', got %q", item.Status)
	}
}

func TestResolveAgentKillsSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)

	// Create agent and work item manually.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	if _, err := sphereStore.CreateAgent("Alpha", "myworld", "agent"); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	worldStore, err := store.OpenWorld("myworld")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	defer worldStore.Close()

	itemID, err := worldStore.CreateWorkItem("Agent task", "test", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	// Set agent to working state.
	if err := sphereStore.UpdateAgentState("myworld/Alpha", "working", itemID); err != nil {
		t.Fatalf("update agent state: %v", err)
	}
	if err := worldStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "tethered"}); err != nil {
		t.Fatalf("update work item: %v", err)
	}

	// Write tether.
	tetherDir := filepath.Join(gtHome, "myworld", "outposts", "Alpha")
	os.MkdirAll(tetherDir, 0o755)
	tetherPath := filepath.Join(tetherDir, ".tether")
	if err := os.WriteFile(tetherPath, []byte(itemID), 0o644); err != nil {
		t.Fatalf("write tether: %v", err)
	}

	// Create worktree with git repo.
	worktree := filepath.Join(gtHome, "myworld", "outposts", "Alpha", "worktree")
	os.MkdirAll(worktree, 0o755)
	gitRun(t, worktree, "init")
	gitRun(t, worktree, "config", "user.email", "test@test.com")
	gitRun(t, worktree, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(worktree, "work.txt"), []byte("done"), 0o644); err != nil {
		t.Fatalf("write work file: %v", err)
	}
	gitRun(t, worktree, "add", ".")
	gitRun(t, worktree, "commit", "-m", "initial")
	bareRemote := t.TempDir()
	gitRun(t, bareRemote, "init", "--bare")
	gitRun(t, worktree, "remote", "add", "origin", bareRemote)

	// Resolve via CLI (session stop is fire-and-forget goroutine —
	// session kill behavior is covered by unit tests in dispatch_test.go).
	out, err := runGT(t, gtHome, "resolve", "--world=myworld", "--agent=Alpha")
	if err != nil {
		t.Fatalf("resolve: %v: %s", err, out)
	}

	// Verify state changes: agent idle, work item done, MR created.
	agent, err := sphereStore.GetAgent("myworld/Alpha")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("expected agent state 'idle', got %q", agent.State)
	}
	item, err := worldStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("get work item: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected work item status 'done', got %q", item.Status)
	}
	// Verify tether is cleared.
	if _, err := os.Stat(tetherPath); !os.IsNotExist(err) {
		t.Error("tether file should be cleared after resolve")
	}
}

// =============================================================================
// Prefect Behavior Tests
// =============================================================================

func TestPrefectSkipsEnvoy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "myworld")

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Create envoy agent with state "working" and dead session.
	if _, err := sphereStore.CreateAgent("scout", "myworld", "envoy"); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("myworld/scout", "working", "sol-deadbeef"); err != nil {
		t.Fatalf("update agent state: %v", err)
	}

	// Run prefect heartbeat using mock session checker.
	mock := newMockSessionChecker()
	// No sessions alive — envoy's session is "dead".
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	p := prefect.New(prefect.DefaultConfig(), sphereStore, mock, logger)

	// Run one heartbeat cycle.
	p.Heartbeat()

	// Verify envoy was NOT respawned.
	mock.mu.Lock()
	started := len(mock.started)
	mock.mu.Unlock()
	if started > 0 {
		t.Errorf("expected 0 sessions started (envoy should be skipped), got %d", started)
	}
}

func TestPrefectSkipsGovernor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "myworld")

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Create governor agent with state "working" and dead session.
	if err := sphereStore.EnsureAgent("governor", "myworld", "governor"); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("myworld/governor", "working", ""); err != nil {
		t.Fatalf("update agent state: %v", err)
	}

	mock := newMockSessionChecker()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	p := prefect.New(prefect.DefaultConfig(), sphereStore, mock, logger)

	p.Heartbeat()

	mock.mu.Lock()
	started := len(mock.started)
	mock.mu.Unlock()
	if started > 0 {
		t.Errorf("expected 0 sessions started (governor should be skipped), got %d", started)
	}
}

// =============================================================================
// Status Display Tests
// =============================================================================

func TestStatusWithEnvoys(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)

	createEnvoy(t, gtHome, "myworld", "scout")

	out, err := runGT(t, gtHome, "status", "myworld")
	if err != nil && strings.TrimSpace(out) == "" {
		t.Fatalf("status command failed: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "Envoys") {
		t.Errorf("status output missing 'Envoys' section: %s", out)
	}
	if !strings.Contains(out, "scout") {
		t.Errorf("status output missing envoy name 'scout': %s", out)
	}
}

func TestStatusWithGovernor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)

	// Register governor agent without starting a session (avoid tmux dependency).
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	if err := sphereStore.EnsureAgent("governor", "myworld", "governor"); err != nil {
		t.Fatalf("create governor agent: %v", err)
	}

	out, err := runGT(t, gtHome, "status", "myworld")
	if err != nil && strings.TrimSpace(out) == "" {
		t.Fatalf("status command failed: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "Governor") {
		t.Errorf("status output missing 'Governor' in Processes: %s", out)
	}
}

func TestStatusMixedRoles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)

	// Create outpost agent.
	_, err := runGT(t, gtHome, "agent", "create", "Smoke", "--world=myworld")
	if err != nil {
		t.Fatalf("agent create: %v", err)
	}

	// Create envoy.
	createEnvoy(t, gtHome, "myworld", "scout")

	// Register governor.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	if err := sphereStore.EnsureAgent("governor", "myworld", "governor"); err != nil {
		t.Fatalf("create governor agent: %v", err)
	}

	out, err := runGT(t, gtHome, "status", "myworld")
	if err != nil && strings.TrimSpace(out) == "" {
		t.Fatalf("status command failed: %v\noutput: %s", err, out)
	}

	// Verify all three sections present.
	if !strings.Contains(out, "Outposts") {
		t.Errorf("status missing 'Outposts' section: %s", out)
	}
	if !strings.Contains(out, "Envoys") {
		t.Errorf("status missing 'Envoys' section: %s", out)
	}
	if !strings.Contains(out, "Governor") {
		t.Errorf("status missing 'Governor' section: %s", out)
	}
}

func TestStatusNoEnvoySection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "myworld")

	out, err := runGT(t, gtHome, "status", "myworld")
	if err != nil && strings.TrimSpace(out) == "" {
		t.Fatalf("status command failed: %v\noutput: %s", err, out)
	}

	if strings.Contains(out, "Envoys") {
		t.Errorf("status should NOT show 'Envoys' section when no envoys exist: %s", out)
	}
}

func TestStatusSphereWithNewColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)

	// Create envoy and governor.
	createEnvoy(t, gtHome, "myworld", "scout")

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	if err := sphereStore.EnsureAgent("governor", "myworld", "governor"); err != nil {
		t.Fatalf("create governor agent: %v", err)
	}

	out, err := runGT(t, gtHome, "status")
	if err != nil && strings.TrimSpace(out) == "" {
		t.Fatalf("status command failed: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "ENVOYS") {
		t.Errorf("sphere overview missing 'ENVOYS' column: %s", out)
	}
	if !strings.Contains(out, "GOV") {
		t.Errorf("sphere overview missing 'GOV' column: %s", out)
	}
}

func TestStatusJSONBackwardCompat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)

	// Create envoy and governor.
	createEnvoy(t, gtHome, "myworld", "scout")

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	if err := sphereStore.EnsureAgent("governor", "myworld", "governor"); err != nil {
		t.Fatalf("create governor agent: %v", err)
	}

	out, err := runGT(t, gtHome, "status", "myworld", "--json")
	if err != nil && strings.TrimSpace(out) == "" {
		t.Fatalf("status command failed: %v\noutput: %s", err, out)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parse JSON: %v: %s", err, out)
	}

	// Verify new fields present.
	if _, ok := result["envoys"]; !ok {
		t.Errorf("JSON missing 'envoys' field: %s", out)
	}
	if _, ok := result["governor"]; !ok {
		t.Errorf("JSON missing 'governor' field: %s", out)
	}

	// Verify backward-compatible fields still present.
	for _, field := range []string{"world", "prefect", "forge", "agents", "merge_queue", "summary"} {
		if _, ok := result[field]; !ok {
			t.Errorf("JSON missing backward-compatible field %q", field)
		}
	}
}

func TestStatusCaravanPhases(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "myworld")

	// Create work items and phased caravan.
	id1, err := runGT(t, gtHome, "store", "create", "--world=myworld", "--title=Phase0 task")
	if err != nil {
		t.Fatalf("store create: %v: %s", err, id1)
	}
	id2, err := runGT(t, gtHome, "store", "create", "--world=myworld", "--title=Phase1 task")
	if err != nil {
		t.Fatalf("store create: %v: %s", err, id2)
	}

	out, err := runGT(t, gtHome, "caravan", "create", "phased-test", id1, "--world=myworld", "--phase=0")
	if err != nil {
		t.Fatalf("caravan create: %v: %s", err, out)
	}
	caravanID := extractCaravanID(t, out)

	_, err = runGT(t, gtHome, "caravan", "add", caravanID, id2, "--world=myworld", "--phase=1")
	if err != nil {
		t.Fatalf("caravan add: %v: %s", err, out)
	}

	// Check caravan status displays phase info.
	out, err = runGT(t, gtHome, "caravan", "status", caravanID)
	if err != nil {
		t.Fatalf("caravan status: %v: %s", err, out)
	}

	// Verify phase markers appear in output.
	if !strings.Contains(out, "p0") && !strings.Contains(out, "phase 0") {
		t.Errorf("caravan status missing phase 0 info: %s", out)
	}
	if !strings.Contains(out, "p1") && !strings.Contains(out, "phase 1") {
		t.Errorf("caravan status missing phase 1 info: %s", out)
	}
}

// =============================================================================
// Cross-Feature Tests
// =============================================================================

func TestEnvoyFullWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	gtHome, _ := setupTestEnv(t)
	bareRepo, _ := createSourceRepo(t, gtHome)
	initWorldWithRepo(t, gtHome, "myworld", bareRepo)

	// Create envoy.
	out, err := runGT(t, gtHome, "envoy", "create", "scout", "--world=myworld")
	if err != nil {
		t.Fatalf("envoy create: %v: %s", err, out)
	}

	// Start envoy.
	out, err = runGT(t, gtHome, "envoy", "start", "scout", "--world=myworld")
	if err != nil {
		t.Fatalf("envoy start: %v: %s", err, out)
	}
	t.Cleanup(func() {
		runGT(t, gtHome, "envoy", "stop", "scout", "--world=myworld")
	})

	ok := pollUntil(5*time.Second, 200*time.Millisecond, func() bool {
		return tmuxSessionExists("sol-myworld-scout")
	})
	if !ok {
		t.Fatal("envoy session not started")
	}

	// Create work item.
	worldStore, err := store.OpenWorld("myworld")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	defer worldStore.Close()

	itemID, err := worldStore.CreateWorkItem("Envoy full workflow", "test", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	// Tether.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	if err := sphereStore.UpdateAgentState("myworld/scout", "working", itemID); err != nil {
		t.Fatalf("update agent state: %v", err)
	}
	if err := worldStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "tethered"}); err != nil {
		t.Fatalf("update work item: %v", err)
	}

	// Write tether file.
	tetherDir := filepath.Join(gtHome, "myworld", "outposts", "scout")
	os.MkdirAll(tetherDir, 0o755)
	tetherPath := filepath.Join(tetherDir, ".tether")
	if err := os.WriteFile(tetherPath, []byte(itemID), 0o644); err != nil {
		t.Fatalf("write tether: %v", err)
	}

	// Set up envoy worktree for resolve (worktree already created by envoy create).
	envoyWorktree := filepath.Join(gtHome, "myworld", "envoys", "scout", "worktree")
	gitRun(t, envoyWorktree, "config", "user.email", "test@test.com")
	gitRun(t, envoyWorktree, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(envoyWorktree, "result.txt"), []byte("workflow result"), 0o644); err != nil {
		t.Fatalf("write result: %v", err)
	}
	gitRun(t, envoyWorktree, "add", ".")
	gitRun(t, envoyWorktree, "commit", "-m", "initial")
	bareRemote := t.TempDir()
	gitRun(t, bareRemote, "init", "--bare")
	gitRun(t, envoyWorktree, "remote", "set-url", "origin", bareRemote)

	// Resolve — session should stay.
	out, err = runGT(t, gtHome, "resolve", "--world=myworld", "--agent=scout")
	if err != nil {
		t.Fatalf("resolve: %v: %s", err, out)
	}

	time.Sleep(2 * time.Second)
	if !tmuxSessionExists("sol-myworld-scout") {
		t.Error("envoy session should remain after resolve")
	}

	// Verify work item done.
	item, err := worldStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("get work item: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected status 'done', got %q", item.Status)
	}

	// Write brief and verify it's readable.
	briefDir := filepath.Join(gtHome, "myworld", "envoys", "scout", ".brief")
	briefPath := filepath.Join(briefDir, "memory.md")
	if err := os.WriteFile(briefPath, []byte("# Session notes\nCompleted workflow.\n"), 0o644); err != nil {
		t.Fatalf("write brief: %v", err)
	}

	out, err = runGT(t, gtHome, "envoy", "brief", "scout", "--world=myworld")
	if err != nil {
		t.Fatalf("envoy brief: %v: %s", err, out)
	}
	if !strings.Contains(out, "Session notes") {
		t.Errorf("brief output missing content: %s", out)
	}

	// Debrief.
	out, err = runGT(t, gtHome, "envoy", "debrief", "scout", "--world=myworld")
	if err != nil {
		t.Fatalf("envoy debrief: %v: %s", err, out)
	}
	if !strings.Contains(out, "Archived") {
		t.Errorf("expected archive message: %s", out)
	}
}

func TestGovernorDispatchFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)

	// Start governor.
	out, err := runGT(t, gtHome, "governor", "start", "--world=myworld")
	if err != nil {
		t.Fatalf("governor start: %v: %s", err, out)
	}
	t.Cleanup(func() {
		runGT(t, gtHome, "governor", "stop", "--world=myworld")
	})

	// Verify operator can observe via status.
	out, _ = runGT(t, gtHome, "status", "myworld")
	if !strings.Contains(out, "Governor") {
		t.Errorf("status missing governor: %s", out)
	}

	// Stop governor.
	out, err = runGT(t, gtHome, "governor", "stop", "--world=myworld")
	if err != nil {
		t.Fatalf("governor stop: %v: %s", err, out)
	}

	// Verify brief directory persists after stop.
	briefDir := filepath.Join(gtHome, "myworld", "governor", ".brief")
	if _, err := os.Stat(briefDir); os.IsNotExist(err) {
		t.Error("governor brief directory should persist after stop")
	}
}

// =============================================================================
// Helpers
// =============================================================================

// createEnvoy creates an envoy via CLI.
func createEnvoy(t *testing.T, solHome, world, name string) {
	t.Helper()
	out, err := runGT(t, solHome, "envoy", "create", name, "--world="+world)
	if err != nil {
		t.Fatalf("create envoy %q: %v: %s", name, err, out)
	}
}

// writeBrief writes a brief file at the given path.
func writeBrief(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir for brief: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write brief: %v", err)
	}
}

// tmuxSessionExists checks if a tmux session with the given name exists.
func tmuxSessionExists(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// extractCaravanID extracts the caravan ID from create output.
// Expected format: "Created caravan car-<hex>: ..."
func extractCaravanID(t *testing.T, output string) string {
	t.Helper()
	for _, word := range strings.Fields(output) {
		if strings.HasPrefix(word, "car-") {
			// Remove trailing colon if present.
			return strings.TrimSuffix(word, ":")
		}
	}
	t.Fatalf("could not extract caravan ID from: %s", output)
	return ""
}

// extractAgentName extracts agent name from cast output.
// Expected format: "Dispatched <id> -> <agent> (<session>)"
func extractAgentName(t *testing.T, output string) string {
	t.Helper()
	// Look for the line with "-> <name>"
	parts := strings.Split(output, "->")
	if len(parts) < 2 {
		t.Fatalf("could not extract agent name from cast output: %s", output)
	}
	fields := strings.Fields(strings.TrimSpace(parts[1]))
	if len(fields) == 0 {
		t.Fatalf("could not extract agent name from cast output: %s", output)
	}
	return fields[0]
}
