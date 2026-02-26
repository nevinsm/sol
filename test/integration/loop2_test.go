package integration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/refinery"
	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/status"
)

// --- Test 1: Full Merge Pipeline (Happy Path) ---

func TestMergePipelineHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnv(t)
	bareRepo, sourceClone := createSourceRepo(t, gtHome)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// 1. Create work item.
	itemID, err := rigStore.CreateWorkItem("Add feature X", "Implement feature X", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	// 2. Sling — dispatches to auto-provisioned agent.
	result, err := dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		SourceRepo: sourceClone,
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("sling: %v", err)
	}

	agentName := result.AgentName
	worktreeDir := result.WorktreeDir

	// 3. Simulate polecat completing work.
	if err := os.WriteFile(filepath.Join(worktreeDir, "feature.go"),
		[]byte("package main\n\nfunc featureX() {}\n"), 0o644); err != nil {
		t.Fatalf("create feature.go: %v", err)
	}

	// 4. Call Done — should create a merge request.
	doneResult, err := dispatch.Done(dispatch.DoneOpts{
		Rig:       "testrig",
		AgentName: agentName,
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("done: %v", err)
	}

	// 5. Verify MR created.
	mrID := doneResult.MergeRequestID
	if !strings.HasPrefix(mrID, "mr-") {
		t.Fatalf("MR ID should start with 'mr-': got %q", mrID)
	}

	mrs, err := rigStore.ListMergeRequests("ready")
	if err != nil {
		t.Fatalf("list merge requests: %v", err)
	}
	if len(mrs) != 1 {
		t.Fatalf("expected 1 ready MR, got %d", len(mrs))
	}
	if mrs[0].WorkItemID != itemID {
		t.Errorf("MR work_item_id: got %q, want %q", mrs[0].WorkItemID, itemID)
	}
	if mrs[0].Branch != doneResult.BranchName {
		t.Errorf("MR branch: got %q, want %q", mrs[0].Branch, doneResult.BranchName)
	}

	// 6. Start refinery.
	cfg := refinery.DefaultConfig()
	cfg.PollInterval = 1 * time.Second
	cfg.QualityGates = []string{"true"} // always-pass gate
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ref := refinery.New("testrig", sourceClone, rigStore, townStore, cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ref.Run(ctx)

	// 7. Wait for merge.
	waitForMergePhase(t, rigStore, mrID, "merged", 30*time.Second)

	// 8. Verify post-merge state.
	mr, err := rigStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("get MR after merge: %v", err)
	}
	if mr.MergedAt == nil {
		t.Error("MR merged_at should be set")
	}

	item, err := rigStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("get work item after merge: %v", err)
	}
	if item.Status != "closed" {
		t.Errorf("work item status after merge: got %q, want closed", item.Status)
	}

	// Verify the polecat's branch changes are on main in the source repo.
	// The bare repo should have the merge commit on main.
	cmd := exec.Command("git", "-C", bareRepo, "log", "--oneline", "main")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if !strings.Contains(string(out), "Merge") {
		t.Logf("git log output: %s", out)
		// The merge commit message might not contain "Merge" literally,
		// but the commit from the polecat should be present.
	}

	// feature.go should exist on main.
	cmd = exec.Command("git", "-C", bareRepo, "show", "main:feature.go")
	out, err = cmd.Output()
	if err != nil {
		t.Fatalf("git show main:feature.go: %v", err)
	}
	if !strings.Contains(string(out), "featureX") {
		t.Errorf("feature.go on main should contain featureX: %s", out)
	}

	cancel()
}

// --- Test 2: Quality Gate Failure and Retry ---

func TestMergePipelineQualityGateRetry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnv(t)
	_, sourceClone := createSourceRepo(t, gtHome)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// Set up: create work item, sling, simulate work, done.
	itemID, err := rigStore.CreateWorkItem("Gate retry test", "Test quality gate retry", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	result, err := dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		SourceRepo: sourceClone,
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("sling: %v", err)
	}

	os.WriteFile(filepath.Join(result.WorktreeDir, "retry.go"),
		[]byte("package main\n\nfunc retry() {}\n"), 0o644)

	doneResult, err := dispatch.Done(dispatch.DoneOpts{
		Rig:       "testrig",
		AgentName: result.AgentName,
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("done: %v", err)
	}
	mrID := doneResult.MergeRequestID

	// Start refinery with a failing quality gate.
	cfg := refinery.DefaultConfig()
	cfg.PollInterval = 1 * time.Second
	cfg.QualityGates = []string{"exit 1"} // always fails
	cfg.MaxAttempts = 3
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ref := refinery.New("testrig", sourceClone, rigStore, townStore, cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	go ref.Run(ctx)

	// Wait for the MR to be retried and eventually failed.
	waitForMergePhase(t, rigStore, mrID, "failed", 30*time.Second)
	cancel()

	// Verify attempts exceeded max.
	mr, err := rigStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("get MR: %v", err)
	}
	if mr.Phase != "failed" {
		t.Errorf("MR phase: got %q, want failed", mr.Phase)
	}

	// Reset MR for retry: set phase back to ready, reset attempts.
	rigStore.DB().Exec("UPDATE merge_requests SET phase = 'ready', claimed_by = NULL, claimed_at = NULL, attempts = 0 WHERE id = ?", mrID)

	// Restart refinery with a passing gate.
	cfg.QualityGates = []string{"true"}
	ref2 := refinery.New("testrig", sourceClone, rigStore, townStore, cfg, logger)

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go ref2.Run(ctx2)

	// Wait for merge.
	waitForMergePhase(t, rigStore, mrID, "merged", 30*time.Second)

	// Verify.
	mr, err = rigStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("get MR after retry: %v", err)
	}
	if mr.Phase != "merged" {
		t.Errorf("MR phase after retry: got %q, want merged", mr.Phase)
	}

	cancel2()
}

// --- Test 3: Merge Conflict ---

func TestMergePipelineConflict(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnv(t)
	_, sourceClone := createSourceRepo(t, gtHome)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// Create a shared file in the source repo.
	os.WriteFile(filepath.Join(sourceClone, "shared.go"),
		[]byte("package main\n\n// original content\n"), 0o644)
	gitRun(t, sourceClone, "add", ".")
	gitRun(t, sourceClone, "commit", "-m", "add shared.go")
	gitRun(t, sourceClone, "push", "origin", "main")

	// Create work item and sling.
	itemID, err := rigStore.CreateWorkItem("Conflict test", "Test merge conflict", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	result, err := dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		SourceRepo: sourceClone,
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("sling: %v", err)
	}

	// Polecat modifies shared.go in its worktree.
	os.WriteFile(filepath.Join(result.WorktreeDir, "shared.go"),
		[]byte("package main\n\n// polecat's changes\nfunc polecatWork() {}\n"), 0o644)

	// Meanwhile, push a conflicting change to main directly.
	os.WriteFile(filepath.Join(sourceClone, "shared.go"),
		[]byte("package main\n\n// conflicting change\nfunc mainlineWork() {}\n"), 0o644)
	gitRun(t, sourceClone, "add", ".")
	gitRun(t, sourceClone, "commit", "-m", "conflicting change to shared.go")
	gitRun(t, sourceClone, "push", "origin", "main")

	// Call Done — creates MR.
	doneResult, err := dispatch.Done(dispatch.DoneOpts{
		Rig:       "testrig",
		AgentName: result.AgentName,
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("done: %v", err)
	}
	mrID := doneResult.MergeRequestID

	// Start refinery.
	cfg := refinery.DefaultConfig()
	cfg.PollInterval = 1 * time.Second
	cfg.QualityGates = []string{"true"}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ref := refinery.New("testrig", sourceClone, rigStore, townStore, cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ref.Run(ctx)

	// Wait for refinery to process — should fail due to conflict.
	waitForMergePhase(t, rigStore, mrID, "failed", 30*time.Second)

	mr, err := rigStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("get MR: %v", err)
	}
	if mr.Phase != "failed" {
		t.Errorf("MR phase: got %q, want failed", mr.Phase)
	}

	cancel()
}

// --- Test 4: Merge Slot Serialization ---

func TestMergeSlotSerialization(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnv(t)
	_, sourceClone := createSourceRepo(t, gtHome)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// Create work item, sling, simulate work, done.
	itemID, err := rigStore.CreateWorkItem("Slot test", "Merge slot test", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	result, err := dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		SourceRepo: sourceClone,
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("sling: %v", err)
	}

	os.WriteFile(filepath.Join(result.WorktreeDir, "slot.go"),
		[]byte("package main\n\nfunc slotTest() {}\n"), 0o644)

	doneResult, err := dispatch.Done(dispatch.DoneOpts{
		Rig:       "testrig",
		AgentName: result.AgentName,
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("done: %v", err)
	}
	mrID := doneResult.MergeRequestID

	// 1. Acquire the merge slot lock manually.
	lock, err := dispatch.AcquireMergeSlotLock("testrig")
	if err != nil {
		t.Fatalf("acquire merge slot: %v", err)
	}

	// 2. Start the refinery with a ready MR.
	// Use high MaxAttempts since each poll cycle where the slot is busy
	// still increments the attempts counter.
	cfg := refinery.DefaultConfig()
	cfg.PollInterval = 1 * time.Second
	cfg.MaxAttempts = 20
	cfg.QualityGates = []string{"true"}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ref := refinery.New("testrig", sourceClone, rigStore, townStore, cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ref.Run(ctx)

	// 3. Wait briefly — the refinery should not be able to merge.
	time.Sleep(3 * time.Second)

	// 4. Verify: MR is still ready (not processed — slot was busy).
	mr, err := rigStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("get MR: %v", err)
	}
	if mr.Phase == "merged" {
		t.Error("MR should not be merged while slot is held")
	}

	// 5. Release the manual lock.
	lock.Release()

	// 6. Wait for the refinery to process the MR on the next poll.
	waitForMergePhase(t, rigStore, mrID, "merged", 30*time.Second)

	// 7. Verify.
	mr, err = rigStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("get MR after release: %v", err)
	}
	if mr.Phase != "merged" {
		t.Errorf("MR phase: got %q, want merged", mr.Phase)
	}

	cancel()
}

// --- Test 5: Stale Claim TTL Recovery ---

func TestStaleClaimTTLRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnv(t)
	_, sourceClone := createSourceRepo(t, gtHome)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// Create work item, sling, simulate work, done.
	itemID, err := rigStore.CreateWorkItem("TTL test", "Stale claim TTL test", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	result, err := dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		SourceRepo: sourceClone,
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("sling: %v", err)
	}

	os.WriteFile(filepath.Join(result.WorktreeDir, "ttl.go"),
		[]byte("package main\n\nfunc ttlTest() {}\n"), 0o644)

	doneResult, err := dispatch.Done(dispatch.DoneOpts{
		Rig:       "testrig",
		AgentName: result.AgentName,
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("done: %v", err)
	}
	mrID := doneResult.MergeRequestID

	// Claim the MR manually (simulate a crashed refinery).
	_, err = rigStore.ClaimMergeRequest("crashed-refinery")
	if err != nil {
		t.Fatalf("manual claim: %v", err)
	}

	// Verify it's claimed.
	mr, err := rigStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("get MR: %v", err)
	}
	if mr.Phase != "claimed" {
		t.Fatalf("MR phase should be claimed: got %q", mr.Phase)
	}

	// Set claimed_at to 31 minutes ago (direct SQL update).
	staleTime := time.Now().UTC().Add(-31 * time.Minute).Format(time.RFC3339)
	rigStore.DB().Exec("UPDATE merge_requests SET claimed_at = ? WHERE id = ?", staleTime, mrID)

	// Start a new refinery with a short ClaimTTL.
	cfg := refinery.DefaultConfig()
	cfg.PollInterval = 1 * time.Second
	cfg.ClaimTTL = 1 * time.Second // short for testing
	cfg.QualityGates = []string{"true"}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ref := refinery.New("testrig", sourceClone, rigStore, townStore, cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ref.Run(ctx)

	// Wait for the MR to be processed.
	waitForMergePhase(t, rigStore, mrID, "merged", 30*time.Second)

	// Verify.
	mr, err = rigStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("get MR after TTL recovery: %v", err)
	}
	if mr.Phase != "merged" {
		t.Errorf("MR phase: got %q, want merged", mr.Phase)
	}

	cancel()
}

// --- Test 6: Multiple MRs Priority Ordering ---

func TestMergeQueuePriorityOrdering(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnv(t)
	_, sourceClone := createSourceRepo(t, gtHome)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// Create 3 work items with different priorities.
	type workItem struct {
		title    string
		priority int
	}
	items := []workItem{
		{"Low priority", 3},
		{"High priority", 1},
		{"Med priority", 2},
	}

	var mrIDs []string
	var mrPriorities []int

	for _, wi := range items {
		itemID, err := rigStore.CreateWorkItem(wi.title, "Priority test", "operator", wi.priority, nil)
		if err != nil {
			t.Fatalf("create work item %q: %v", wi.title, err)
		}

		result, err := dispatch.Sling(dispatch.SlingOpts{
			WorkItemID: itemID,
			Rig:        "testrig",
			SourceRepo: sourceClone,
		}, rigStore, townStore, mgr)
		if err != nil {
			t.Fatalf("sling %q: %v", wi.title, err)
		}

		// Create a unique file in the worktree.
		filename := fmt.Sprintf("priority_%d.go", wi.priority)
		os.WriteFile(filepath.Join(result.WorktreeDir, filename),
			[]byte(fmt.Sprintf("package main\n\nfunc priority%d() {}\n", wi.priority)), 0o644)

		doneResult, err := dispatch.Done(dispatch.DoneOpts{
			Rig:       "testrig",
			AgentName: result.AgentName,
		}, rigStore, townStore, mgr)
		if err != nil {
			t.Fatalf("done %q: %v", wi.title, err)
		}

		mrIDs = append(mrIDs, doneResult.MergeRequestID)
		mrPriorities = append(mrPriorities, wi.priority)

		// Wait for the session to be stopped (Done stops in background after 1s).
		sessName := dispatch.SessionName("testrig", result.AgentName)
		pollUntil(5*time.Second, 200*time.Millisecond, func() bool {
			return !mgr.Exists(sessName)
		})
	}

	// Start refinery.
	cfg := refinery.DefaultConfig()
	cfg.PollInterval = 1 * time.Second
	cfg.QualityGates = []string{"true"}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ref := refinery.New("testrig", sourceClone, rigStore, townStore, cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ref.Run(ctx)

	// Wait for all MRs to merge.
	for _, mrID := range mrIDs {
		waitForMergePhase(t, rigStore, mrID, "merged", 60*time.Second)
	}

	// Track the order in which MRs were merged by their merged_at timestamps.
	type mergeOrder struct {
		priority int
		mergedAt time.Time
	}
	var order []mergeOrder
	for i, mrID := range mrIDs {
		mr, err := rigStore.GetMergeRequest(mrID)
		if err != nil {
			t.Fatalf("get MR %s: %v", mrID, err)
		}
		if mr.MergedAt == nil {
			t.Fatalf("MR %s has no merged_at", mrID)
		}
		order = append(order, mergeOrder{priority: mrPriorities[i], mergedAt: *mr.MergedAt})
	}

	// Verify: priority 1 merged first, then 2, then 3.
	// Find the merge order by sorting by merged_at.
	for i := 0; i < len(order); i++ {
		for j := i + 1; j < len(order); j++ {
			if order[i].mergedAt.After(order[j].mergedAt) {
				order[i], order[j] = order[j], order[i]
			}
		}
	}

	expectedPriorities := []int{1, 2, 3}
	for i, expected := range expectedPriorities {
		if order[i].priority != expected {
			t.Errorf("merge order[%d]: got priority %d, want %d", i, order[i].priority, expected)
		}
	}

	cancel()
}

// --- Test 7: Status Shows Refinery and Queue ---

func TestStatusWithMergeQueue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnv(t)
	_, sourceClone := createSourceRepo(t, gtHome)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// Create work item, sling, simulate work, done.
	itemID, err := rigStore.CreateWorkItem("Status test", "Status with merge queue", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	result, err := dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		SourceRepo: sourceClone,
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("sling: %v", err)
	}

	os.WriteFile(filepath.Join(result.WorktreeDir, "status_test.go"),
		[]byte("package main\n\nfunc statusTest() {}\n"), 0o644)

	_, err = dispatch.Done(dispatch.DoneOpts{
		Rig:       "testrig",
		AgentName: result.AgentName,
	}, rigStore, townStore, mgr)
	if err != nil {
		t.Fatalf("done: %v", err)
	}

	// Gather status (no refinery running).
	rs, err := status.Gather("testrig", townStore, rigStore, rigStore, mgr)
	if err != nil {
		t.Fatalf("status.Gather: %v", err)
	}

	if rs.Refinery.Running {
		t.Error("refinery should not be running yet")
	}
	if rs.MergeQueue.Ready != 1 {
		t.Errorf("merge queue ready: got %d, want 1", rs.MergeQueue.Ready)
	}
	if rs.MergeQueue.Total != 1 {
		t.Errorf("merge queue total: got %d, want 1", rs.MergeQueue.Total)
	}

	// Start refinery in a tmux session.
	refSessName := dispatch.SessionName("testrig", "refinery")
	err = mgr.Start(refSessName, sourceClone, "sleep 60",
		map[string]string{"GT_HOME": gtHome}, "refinery", "testrig")
	if err != nil {
		t.Fatalf("start refinery session: %v", err)
	}
	defer mgr.Stop(refSessName, true)

	// Gather status again — refinery should be running.
	rs2, err := status.Gather("testrig", townStore, rigStore, rigStore, mgr)
	if err != nil {
		t.Fatalf("status.Gather with refinery: %v", err)
	}

	if !rs2.Refinery.Running {
		t.Error("refinery should be running")
	}
	if rs2.Refinery.SessionName != refSessName {
		t.Errorf("refinery session name: got %q, want %q", rs2.Refinery.SessionName, refSessName)
	}
}
