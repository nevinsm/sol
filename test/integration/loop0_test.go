package integration

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/hook"
	"github.com/nevinsm/gt/internal/session"
)

// --- Test 1: Full Dispatch-Execute-Done Cycle ---

func TestFullDispatchExecuteDone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// 1. Create agent.
	_, err := townStore.CreateAgent("TestBot", "testrig", "polecat")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// 2. Create work item.
	itemID, err := rigStore.CreateWorkItem("Test task", "Integration test description", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	// 3. Sling.
	result, err := dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		AgentName:  "TestBot",
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("sling: %v", err)
	}

	// 4. Verify sling state.
	sessName := "gt-testrig-TestBot"
	if !mgr.Exists(sessName) {
		t.Error("tmux session does not exist after sling")
	}

	item, err := rigStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("get work item: %v", err)
	}
	if item.Status != "hooked" {
		t.Errorf("work item status: got %q, want hooked", item.Status)
	}
	if item.Assignee != "testrig/TestBot" {
		t.Errorf("work item assignee: got %q, want testrig/TestBot", item.Assignee)
	}

	agent, err := townStore.GetAgent("testrig/TestBot")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("agent state: got %q, want working", agent.State)
	}
	if agent.HookItem != itemID {
		t.Errorf("agent hook_item: got %q, want %q", agent.HookItem, itemID)
	}

	if !hook.IsHooked("testrig", "TestBot") {
		t.Error("hook file does not exist after sling")
	}

	worktreeDir := result.WorktreeDir
	if _, err := os.Stat(worktreeDir); os.IsNotExist(err) {
		t.Error("worktree does not exist after sling")
	}

	claudeMD := filepath.Join(worktreeDir, ".claude", "CLAUDE.md")
	if _, err := os.Stat(claudeMD); os.IsNotExist(err) {
		t.Error(".claude/CLAUDE.md does not exist in worktree")
	}

	// 5. Simulate agent work: create a file in the worktree.
	if err := os.WriteFile(filepath.Join(worktreeDir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("create test file: %v", err)
	}

	// 6. Call Done programmatically.
	doneResult, err := dispatch.Done(dispatch.DoneOpts{
		Rig:       "testrig",
		AgentName: "TestBot",
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("done: %v", err)
	}

	// 7. Wait for session to stop (done stops in background after 1s).
	ok := pollUntil(10*time.Second, 500*time.Millisecond, func() bool {
		return !mgr.Exists(sessName)
	})
	if !ok {
		t.Error("session still exists after done (waited 10s)")
	}

	// 8. Verify done state.
	item, err = rigStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("get work item after done: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("work item status after done: got %q, want done", item.Status)
	}

	agent, err = townStore.GetAgent("testrig/TestBot")
	if err != nil {
		t.Fatalf("get agent after done: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("agent state after done: got %q, want idle", agent.State)
	}
	if agent.HookItem != "" {
		t.Errorf("agent hook_item after done: got %q, want empty", agent.HookItem)
	}

	if hook.IsHooked("testrig", "TestBot") {
		t.Error("hook file still exists after done")
	}

	if mgr.Exists(sessName) {
		t.Error("tmux session still exists after done")
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

// --- Test 2: Crash Recovery (Re-sling) ---

func TestCrashRecoveryResling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// Create agent + work item, sling.
	townStore.CreateAgent("TestBot", "testrig", "polecat")
	itemID, _ := rigStore.CreateWorkItem("Crash test", "Recovery test", "operator", 2, nil)

	_, err := dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		AgentName:  "TestBot",
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("initial sling: %v", err)
	}

	// Kill tmux session directly (simulate crash).
	sessName := "gt-testrig-TestBot"
	exec.Command("tmux", "kill-session", "-t", sessName).Run()

	// Verify durability: work item still hooked, hook file persists.
	item, _ := rigStore.GetWorkItem(itemID)
	if item.Status != "hooked" {
		t.Errorf("work item status after crash: got %q, want hooked", item.Status)
	}
	if !hook.IsHooked("testrig", "TestBot") {
		t.Error("hook file missing after crash")
	}

	// Re-sling the same work item to the same agent.
	_, err = dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		AgentName:  "TestBot",
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("re-sling failed: %v", err)
	}

	// Verify: new session exists, same hook.
	if !mgr.Exists(sessName) {
		t.Error("tmux session not created after re-sling")
	}

	hookID, _ := hook.Read("testrig", "TestBot")
	if hookID != itemID {
		t.Errorf("hook after re-sling: got %q, want %q", hookID, itemID)
	}
}

// --- Test 3: Double-Dispatch Prevention ---

func TestDoubleDispatchPrevention(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// Create agent + first work item, sling.
	townStore.CreateAgent("TestBot", "testrig", "polecat")
	item1ID, _ := rigStore.CreateWorkItem("First task", "Task 1", "operator", 2, nil)

	_, err := dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: item1ID,
		Rig:        "testrig",
		AgentName:  "TestBot",
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("first sling: %v", err)
	}

	// Create second work item and try to sling to same agent.
	item2ID, _ := rigStore.CreateWorkItem("Second task", "Task 2", "operator", 2, nil)

	_, err = dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: item2ID,
		Rig:        "testrig",
		AgentName:  "TestBot",
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr)
	if err == nil {
		t.Fatal("expected error for double dispatch, got nil")
	}

	// Verify second item remains open.
	item2, _ := rigStore.GetWorkItem(item2ID)
	if item2.Status != "open" {
		t.Errorf("second work item status: got %q, want open", item2.Status)
	}
}

// --- Test 4: Prime Output ---

func TestPrimeOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	townStore.CreateAgent("TestBot", "testrig", "polecat")
	itemID, _ := rigStore.CreateWorkItem("Prime test task", "Check prime output", "operator", 2, nil)

	dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		AgentName:  "TestBot",
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr)

	// Run Prime.
	result, err := dispatch.Prime("testrig", "TestBot", rigStore)
	if err != nil {
		t.Fatalf("prime: %v", err)
	}

	checks := map[string]string{
		"work item ID": itemID,
		"title":        "Prime test task",
		"description":  "Check prime output",
		"gt done":      "gt done",
	}
	for what, want := range checks {
		if !strings.Contains(result.Output, want) {
			t.Errorf("prime output missing %s (%q)", what, want)
		}
	}
}

// --- Test 5: Prime Without Hook ---

func TestPrimeWithoutHook(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")

	townStore.CreateAgent("TestBot", "testrig", "polecat")

	result, err := dispatch.Prime("testrig", "TestBot", rigStore)
	if err != nil {
		t.Fatalf("prime: %v", err)
	}
	if result.Output != "No work hooked" {
		t.Errorf("prime output: got %q, want 'No work hooked'", result.Output)
	}
}

// --- Test 6: Store Inspection ---

func TestStoreInspection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// Create work items.
	id1, _ := rigStore.CreateWorkItem("Task one", "First", "operator", 2, nil)
	id2, _ := rigStore.CreateWorkItem("Task two", "Second", "operator", 2, nil)

	// Sling one.
	townStore.CreateAgent("TestBot", "testrig", "polecat")
	dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: id1,
		Rig:        "testrig",
		AgentName:  "TestBot",
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr)

	// Query the rig DB directly via database/sql.
	db := rigStore.DB()
	rows, err := db.Query("SELECT id, title, status, assignee FROM work_items ORDER BY created_at")
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

	// First item: hooked, assigned.
	if results[0].id != id1 || results[0].status != "hooked" {
		t.Errorf("item 1: got id=%s status=%s, want id=%s status=hooked", results[0].id, results[0].status, id1)
	}
	if !results[0].assignee.Valid || results[0].assignee.String != "testrig/TestBot" {
		t.Errorf("item 1 assignee: got %v, want testrig/TestBot", results[0].assignee)
	}

	// Second item: open, no assignee.
	if results[1].id != id2 || results[1].status != "open" {
		t.Errorf("item 2: got id=%s status=%s, want id=%s status=open", results[1].id, results[1].status, id2)
	}
	if results[1].assignee.Valid {
		t.Errorf("item 2 assignee: got %q, want NULL", results[1].assignee.String)
	}
}
