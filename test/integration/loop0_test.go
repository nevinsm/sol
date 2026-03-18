package integration

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/nevinsm/sol/internal/session"
)

// --- Test 1: Full Dispatch-Execute-Done Cycle ---

func TestFullDispatchExecuteDone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := session.New()

	// 1. Create agent.
	_, err := sphereStore.CreateAgent("TestBot", "ember", "outpost")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// 2. Create writ.
	itemID, err := worldStore.CreateWrit("Test task", "Integration test description", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}

	// 3. Cast.
	result, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: itemID,
		World:        "ember",
		AgentName:  "TestBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("cast: %v", err)
	}

	// 4. Verify cast state.
	sessName := "sol-ember-TestBot"
	if !mgr.Exists(sessName) {
		t.Error("tmux session does not exist after cast")
	}

	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("get writ: %v", err)
	}
	if item.Status != "tethered" {
		t.Errorf("writ status: got %q, want tethered", item.Status)
	}
	if item.Assignee != "ember/TestBot" {
		t.Errorf("writ assignee: got %q, want ember/TestBot", item.Assignee)
	}

	agent, err := sphereStore.GetAgent("ember/TestBot")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("agent state: got %q, want working", agent.State)
	}
	if agent.ActiveWrit != itemID {
		t.Errorf("agent active_writ: got %q, want %q", agent.ActiveWrit, itemID)
	}

	if !tether.IsTethered("ember", "TestBot", "outpost") {
		t.Error("tether file does not exist after cast")
	}

	worktreeDir := result.WorktreeDir
	if _, err := os.Stat(worktreeDir); os.IsNotExist(err) {
		t.Error("worktree does not exist after cast")
	}

	claudeMD := filepath.Join(worktreeDir, "CLAUDE.local.md")
	if _, err := os.Stat(claudeMD); os.IsNotExist(err) {
		t.Error("CLAUDE.local.md does not exist in worktree")
	}

	// 5. Simulate agent work: create a file in the worktree.
	if err := os.WriteFile(filepath.Join(worktreeDir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("create test file: %v", err)
	}

	// 6. Call Resolve programmatically.
	doneResult, err := dispatch.Resolve(context.Background(), dispatch.ResolveOpts{
		World:       "ember",
		AgentName: "TestBot",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// 7. Wait for session to stop (done stops in background after 1s).
	ok := pollUntil(20*time.Second, 500*time.Millisecond, func() bool {
		return !mgr.Exists(sessName)
	})
	if !ok {
		t.Error("session still exists after resolve (waited 20s)")
	}

	// 8. Verify resolve state.
	item, err = worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("get writ after resolve: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("writ status after resolve: got %q, want done", item.Status)
	}

	// Outpost agent record should be deleted after resolve (name reclaimed).
	_, err = sphereStore.GetAgent("ember/TestBot")
	if err == nil {
		t.Error("expected agent record to be deleted after resolve")
	}

	if tether.IsTethered("ember", "TestBot", "outpost") {
		t.Error("tether file still exists after resolve")
	}

	if mgr.Exists(sessName) {
		t.Error("tmux session still exists after resolve")
	}

	// Verify branch exists in source repo.
	branchName := doneResult.BranchName
	cmd := exec.Command("git", "-C", sourceRepo, "branch", "--list", branchName)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git branch list: %v", err)
	}
	if !strings.Contains(string(out), branchName) {
		t.Errorf("branch %q not found in source repo", branchName)
	}
}

// --- Test 2: Crash Recovery (Re-cast) ---

func TestCrashRecoveryRecast(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := session.New()

	// Create agent + writ, cast.
	if _, err := sphereStore.CreateAgent("TestBot", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWrit("Crash test", "Recovery test", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	_, err = dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: itemID,
		World:        "ember",
		AgentName:  "TestBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("initial cast: %v", err)
	}

	// Kill tmux session directly (simulate crash).
	sessName := "sol-ember-TestBot"
	exec.Command("tmux", "kill-session", "-t", sessName).Run()

	// Verify durability: writ still tethered, tether file persists.
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("get writ after crash: %v", err)
	}
	if item.Status != "tethered" {
		t.Errorf("writ status after crash: got %q, want tethered", item.Status)
	}
	if !tether.IsTethered("ember", "TestBot", "outpost") {
		t.Error("tether file missing after crash")
	}

	// Re-cast the same writ to the same agent.
	_, err = dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: itemID,
		World:        "ember",
		AgentName:  "TestBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("re-cast failed: %v", err)
	}

	// Verify: new session exists, same tether.
	if !mgr.Exists(sessName) {
		t.Error("tmux session not created after re-cast")
	}

	tetherID, err := tether.Read("ember", "TestBot", "outpost")
	if err != nil {
		t.Fatalf("read tether after re-cast: %v", err)
	}
	if tetherID != itemID {
		t.Errorf("tether after re-cast: got %q, want %q", tetherID, itemID)
	}
}

// --- Test 3: Double-Dispatch Prevention ---

func TestDoubleDispatchPrevention(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := session.New()

	// Create agent + first writ, cast.
	if _, err := sphereStore.CreateAgent("TestBot", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	item1ID, err := worldStore.CreateWrit("First task", "Task 1", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	_, err = dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: item1ID,
		World:        "ember",
		AgentName:  "TestBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("first cast: %v", err)
	}

	// Create second writ and try to cast to same agent.
	item2ID, err := worldStore.CreateWrit("Second task", "Task 2", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	_, err = dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: item2ID,
		World:        "ember",
		AgentName:  "TestBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err == nil {
		t.Fatal("expected error for double dispatch, got nil")
	}

	// Verify second item remains open.
	item2, err := worldStore.GetWrit(item2ID)
	if err != nil {
		t.Fatalf("get writ 2: %v", err)
	}
	if item2.Status != "open" {
		t.Errorf("second writ status: got %q, want open", item2.Status)
	}
}

// --- Test 4: Prime Output ---

func TestPrimeOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := session.New()

	if _, err := sphereStore.CreateAgent("TestBot", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWrit("Prime test task", "Check prime output", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	if _, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: itemID,
		World:        "ember",
		AgentName:  "TestBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("cast: %v", err)
	}

	// Run Prime.
	result, err := dispatch.Prime("ember", "TestBot", "outpost", worldStore)
	if err != nil {
		t.Fatalf("prime: %v", err)
	}

	checks := map[string]string{
		"writ ID": itemID,
		"title":        "Prime test task",
		"description":  "Check prime output",
		"sol resolve":      "sol resolve",
	}
	for what, want := range checks {
		if !strings.Contains(result.Output, want) {
			t.Errorf("prime output missing %s (%q)", what, want)
		}
	}
}

// --- Test 5: Prime Without Tether ---

func TestPrimeWithoutHook(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")

	if _, err := sphereStore.CreateAgent("TestBot", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	result, err := dispatch.Prime("ember", "TestBot", "outpost", worldStore)
	if err != nil {
		t.Fatalf("prime: %v", err)
	}
	if result.Output != "No work tethered" {
		t.Errorf("prime output: got %q, want 'No work tethered'", result.Output)
	}
}

// --- Test 6: Store Inspection ---

func TestStoreInspection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := session.New()

	// Create writs.
	id1, err := worldStore.CreateWrit("Task one", "First", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	id2, err := worldStore.CreateWrit("Task two", "Second", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Cast one.
	if _, err := sphereStore.CreateAgent("TestBot", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if _, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: id1,
		World:        "ember",
		AgentName:  "TestBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("cast: %v", err)
	}

	// Query the world DB directly via database/sql.
	db := worldStore.DB()
	rows, err := db.Query("SELECT id, title, status, assignee FROM writs ORDER BY created_at")
	if err != nil {
		t.Fatalf("SQL query: %v", err)
	}
	defer rows.Close()

	type row struct {
		id, title, status string
		assignee          sql.NullString
	}
	var results []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.title, &r.status, &r.assignee); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		results = append(results, r)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(results))
	}

	// First item: tethered, assigned.
	if results[0].id != id1 || results[0].status != "tethered" {
		t.Errorf("item 1: got id=%s status=%s, want id=%s status=tethered", results[0].id, results[0].status, id1)
	}
	if !results[0].assignee.Valid || results[0].assignee.String != "ember/TestBot" {
		t.Errorf("item 1 assignee: got %v, want ember/TestBot", results[0].assignee)
	}

	// Second item: open, no assignee.
	if results[1].id != id2 || results[1].status != "open" {
		t.Errorf("item 2: got id=%s status=%s, want id=%s status=open", results[1].id, results[1].status, id2)
	}
	if results[1].assignee.Valid {
		t.Errorf("item 2 assignee: got %q, want NULL", results[1].assignee.String)
	}
}
