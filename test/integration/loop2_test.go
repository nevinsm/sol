package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/status"
)

// --- Test: Status Shows Refinery and Queue ---

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
