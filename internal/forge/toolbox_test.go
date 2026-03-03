package forge

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

func TestListReady(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", Phase: "ready", BlockedBy: ""},
		{ID: "mr-00000002", Phase: "ready", BlockedBy: "sol-blocker1"},
		{ID: "mr-00000003", Phase: "ready", BlockedBy: ""},
		{ID: "mr-00000004", Phase: "claimed", BlockedBy: ""},
	}

	r := &Forge{
		world:      "ember",
		worldStore: worldStore,
		logger:   testLogger(),
	}

	ready, err := r.ListReady()
	if err != nil {
		t.Fatalf("ListReady() error: %v", err)
	}
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready MRs, got %d", len(ready))
	}
	for _, mr := range ready {
		if mr.BlockedBy != "" {
			t.Errorf("ListReady returned blocked MR %s", mr.ID)
		}
	}
}

func TestListBlocked(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", Phase: "ready", BlockedBy: ""},
		{ID: "mr-00000002", Phase: "ready", BlockedBy: "sol-blocker1"},
		{ID: "mr-00000003", Phase: "ready", BlockedBy: "sol-blocker2"},
	}

	r := &Forge{
		world:      "ember",
		worldStore: worldStore,
		logger:   testLogger(),
	}

	blocked, err := r.ListBlocked()
	if err != nil {
		t.Fatalf("ListBlocked() error: %v", err)
	}
	if len(blocked) != 2 {
		t.Fatalf("expected 2 blocked MRs, got %d", len(blocked))
	}
}

func TestCreateResolutionTask(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".runtime", "locks"), 0o755)

	// Set up a mock git repo to satisfy rev-parse.
	repoDir := t.TempDir()
	run(t, "git", "init", repoDir)
	run(t, "git", "-C", repoDir, "commit", "--allow-empty", "-m", "init")

	worldStore := newMockWorldStore()
	worldStore.items["sol-original1"] = &store.WorkItem{
		ID:       "sol-original1",
		Title:    "Add feature X",
		Priority: 2,
	}
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WorkItemID: "sol-original1", Branch: "outpost/Toast/sol-original1", Phase: "claimed"},
	}

	r := &Forge{
		world:      "ember",
		agentID:  "ember/forge",
		worktree: repoDir,
		worldStore: worldStore,
		logger:   testLogger(),
		cfg:      DefaultConfig(),
	}

	mr := &store.MergeRequest{
		ID:         "mr-00000001",
		WorkItemID: "sol-original1",
		Branch:     "outpost/Toast/sol-original1",
		Phase:      "claimed",
	}

	taskID, err := r.CreateResolutionTask(mr)
	if err != nil {
		t.Fatalf("CreateResolutionTask() error: %v", err)
	}
	if taskID == "" {
		t.Fatal("expected non-empty task ID")
	}

	// Verify the resolution task was created.
	item := worldStore.items[taskID]
	if item == nil {
		t.Fatal("resolution task not found in store")
	}
	if !strings.Contains(item.Title, "Resolve merge conflicts") {
		t.Errorf("task title = %q, want to contain 'Resolve merge conflicts'", item.Title)
	}
	if item.Priority != 1 {
		t.Errorf("task priority = %d, want 1 (boosted from 2)", item.Priority)
	}
	if item.ParentID != "sol-original1" {
		t.Errorf("task parent_id = %q, want %q", item.ParentID, "sol-original1")
	}
	if !item.HasLabel("conflict-resolution") {
		t.Error("task missing 'conflict-resolution' label")
	}
	if !item.HasLabel("source-mr:mr-00000001") {
		t.Error("task missing 'source-mr:mr-00000001' label")
	}

	// Verify the MR is blocked.
	worldStore.mu.Lock()
	blockedMR := worldStore.mrs[0]
	worldStore.mu.Unlock()
	if blockedMR.BlockedBy != taskID {
		t.Errorf("MR blocked_by = %q, want %q", blockedMR.BlockedBy, taskID)
	}
}

func TestCheckUnblocked(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", Phase: "ready", BlockedBy: "sol-resolved1"},
		{ID: "mr-00000002", Phase: "ready", BlockedBy: "sol-pending1"},
	}
	worldStore.items["sol-resolved1"] = &store.WorkItem{ID: "sol-resolved1", Status: "closed"}
	worldStore.items["sol-pending1"] = &store.WorkItem{ID: "sol-pending1", Status: "open"}

	r := &Forge{
		world:      "ember",
		worldStore: worldStore,
		logger:   testLogger(),
	}

	unblocked, err := r.CheckUnblocked()
	if err != nil {
		t.Fatalf("CheckUnblocked() error: %v", err)
	}
	if len(unblocked) != 1 {
		t.Fatalf("expected 1 unblocked MR, got %d", len(unblocked))
	}
	if unblocked[0] != "mr-00000001" {
		t.Errorf("unblocked MR = %q, want %q", unblocked[0], "mr-00000001")
	}

	// Verify the unblocked MR has its BlockedBy cleared.
	worldStore.mu.Lock()
	mr := worldStore.mrs[0]
	worldStore.mu.Unlock()
	if mr.BlockedBy != "" {
		t.Errorf("MR blocked_by after unblock = %q, want empty", mr.BlockedBy)
	}

	// Verify the still-blocked MR is unchanged.
	worldStore.mu.Lock()
	mr2 := worldStore.mrs[1]
	worldStore.mu.Unlock()
	if mr2.BlockedBy != "sol-pending1" {
		t.Errorf("MR blocked_by = %q, want %q", mr2.BlockedBy, "sol-pending1")
	}
}

func TestRunGates(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	cfg := DefaultConfig()
	cfg.QualityGates = []string{"true", "echo hello"}

	r := &Forge{
		world:      "ember",
		worktree: dir,
		logger:   testLogger(),
		cfg:      cfg,
	}

	results, err := r.RunGates(context.Background())
	if err != nil {
		t.Fatalf("RunGates() error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.Passed {
			t.Errorf("gate %q did not pass", r.Gate)
		}
	}
}

func TestRunGatesFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	cfg := DefaultConfig()
	cfg.QualityGates = []string{"true", "exit 1", "true"}

	r := &Forge{
		world:      "ember",
		worktree: dir,
		logger:   testLogger(),
		cfg:      cfg,
	}

	results, err := r.RunGates(context.Background())
	if err != nil {
		t.Fatalf("RunGates() error: %v", err)
	}
	// Should return after first failure (2 results: pass, fail).
	if len(results) != 2 {
		t.Fatalf("expected 2 results (stops at first failure), got %d", len(results))
	}
	if !results[0].Passed {
		t.Error("first gate should have passed")
	}
	if results[1].Passed {
		t.Error("second gate should have failed")
	}
}

func TestRunGatesCancelledContext(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	cfg := DefaultConfig()
	cfg.QualityGates = []string{"sleep 60"}
	cfg.GateTimeout = 60 * time.Second // ensure the per-gate timeout doesn't interfere

	r := &Forge{
		world:    "ember",
		worktree: dir,
		logger:   testLogger(),
		cfg:      cfg,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	results, err := r.RunGates(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("RunGates() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Passed {
		t.Error("gate should have failed due to cancelled context")
	}
	if elapsed > 10*time.Second {
		t.Errorf("RunGates took %v, expected well under 60s", elapsed)
	}
}

func TestPush(t *testing.T) {
	sourceRepo, worktreeDir := setupGitTest(t)

	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".runtime", "locks"), 0o755)

	// Create worktree.
	branch := "forge/ember"
	run(t, "git", "-C", sourceRepo, "worktree", "add", "-b", branch, worktreeDir, "HEAD")
	run(t, "git", "-C", worktreeDir, "config", "user.email", "test@test.com")
	run(t, "git", "-C", worktreeDir, "config", "user.name", "Test")

	// Make a change in the worktree.
	os.WriteFile(filepath.Join(worktreeDir, "push-test.go"), []byte("package main\n"), 0o644)
	run(t, "git", "-C", worktreeDir, "add", ".")
	run(t, "git", "-C", worktreeDir, "commit", "-m", "push test")

	r := &Forge{
		world:      "ember",
		agentID:  "ember/forge",
		worktree: worktreeDir,
		worldStore: newMockWorldStore(),
		logger:   testLogger(),
		cfg:      DefaultConfig(),
	}

	if err := r.Push(); err != nil {
		t.Fatalf("Push() error: %v", err)
	}

	// Verify the commit is on main.
	out := run(t, "git", "-C", sourceRepo, "log", "--oneline", "origin/main")
	if !strings.Contains(out, "push test") {
		t.Errorf("main should contain push commit, got:\n%s", out)
	}
}

func TestMarkFailedReopensWorkItem(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WorkItemID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: "claimed"},
	}
	worldStore.items["sol-aaa11111"] = &store.WorkItem{
		ID:       "sol-aaa11111",
		Title:    "Test feature",
		Status:   "done",
		Assignee: "Toast",
	}

	sphereStore := newMockSphereStore()

	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
		cfg:         DefaultConfig(),
	}

	if err := r.MarkFailed("mr-00000001"); err != nil {
		t.Fatalf("MarkFailed() error: %v", err)
	}

	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()

	// Verify MR phase set to failed.
	if phase, ok := worldStore.phaseUpdates["mr-00000001"]; !ok || phase != "failed" {
		t.Errorf("MR phase = %q, want 'failed'", phase)
	}

	// Verify work item reopened.
	item := worldStore.items["sol-aaa11111"]
	if item.Status != "open" {
		t.Errorf("work item status = %q, want 'open'", item.Status)
	}
	if item.Assignee != "" {
		t.Errorf("work item assignee = %q, want empty (cleared)", item.Assignee)
	}

	// Verify escalation created.
	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()
	if len(sphereStore.escalations) != 1 {
		t.Fatalf("expected 1 escalation, got %d", len(sphereStore.escalations))
	}
	esc := sphereStore.escalations[0]
	if esc.severity != "high" {
		t.Errorf("escalation severity = %q, want 'high'", esc.severity)
	}
	if esc.source != "ember/forge" {
		t.Errorf("escalation source = %q, want 'ember/forge'", esc.source)
	}
	if !strings.Contains(esc.description, "sol-aaa11111") {
		t.Errorf("escalation description should mention work item ID, got: %s", esc.description)
	}
}

func TestMarkMergedClosesWorkItem(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WorkItemID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: "claimed"},
	}
	worldStore.items["sol-aaa11111"] = &store.WorkItem{ID: "sol-aaa11111", Title: "Test", Status: "done"}

	// Create a temp dir for git operations.
	dir := t.TempDir()
	run(t, "git", "init", dir)

	r := &Forge{
		world:      "ember",
		agentID:  "ember/forge",
		worktree: dir,
		worldStore: worldStore,
		logger:   slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		cfg:      DefaultConfig(),
	}

	if err := r.MarkMerged("mr-00000001"); err != nil {
		t.Fatalf("MarkMerged() error: %v", err)
	}

	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()

	// Verify MR phase.
	if phase, ok := worldStore.phaseUpdates["mr-00000001"]; !ok || phase != "merged" {
		t.Errorf("MR phase = %q, want 'merged'", phase)
	}

	// Verify work item closed.
	if worldStore.items["sol-aaa11111"].Status != "closed" {
		t.Errorf("work item status = %q, want 'closed'", worldStore.items["sol-aaa11111"].Status)
	}
}

