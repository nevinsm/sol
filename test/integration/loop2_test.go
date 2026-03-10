package integration

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/status"
)

// --- Test: Status Shows Forge and Queue ---

func TestStatusWithMergeQueue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)
	_, sourceClone := createSourceRepo(t, solHome)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := session.New()

	// Create writ, cast, simulate work, done.
	itemID, err := worldStore.CreateWrit("Status test", "Status with merge queue", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}

	result, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: itemID,
		World:        "ember",
		SourceRepo: sourceClone,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("cast: %v", err)
	}

	if err := os.WriteFile(filepath.Join(result.WorktreeDir, "status_test.go"),
		[]byte("package main\n\nfunc statusTest() {}\n"), 0o644); err != nil {
		t.Fatalf("write status_test.go: %v", err)
	}

	_, err = dispatch.Resolve(context.Background(), dispatch.ResolveOpts{
		World:       "ember",
		AgentName: result.AgentName,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Gather status (no forge running).
	rs, err := status.Gather("ember", sphereStore, worldStore, worldStore, mgr)
	if err != nil {
		t.Fatalf("status.Gather: %v", err)
	}

	if rs.Forge.Running {
		t.Error("forge should not be running yet")
	}
	if rs.MergeQueue.Ready != 1 {
		t.Errorf("merge queue ready: got %d, want 1", rs.MergeQueue.Ready)
	}
	if rs.MergeQueue.Total != 1 {
		t.Errorf("merge queue total: got %d, want 1", rs.MergeQueue.Total)
	}

	// Simulate running forge via PID file (forge now runs as direct process).
	forgeDir := filepath.Join(solHome, "ember", "forge")
	os.MkdirAll(forgeDir, 0o755)
	pidFile := filepath.Join(forgeDir, "forge.pid")
	os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644)
	defer os.Remove(pidFile)

	// Gather status again — forge should be running.
	rs2, err := status.Gather("ember", sphereStore, worldStore, worldStore, mgr)
	if err != nil {
		t.Fatalf("status.Gather with forge: %v", err)
	}

	if !rs2.Forge.Running {
		t.Error("forge should be running")
	}
	if rs2.Forge.PID != os.Getpid() {
		t.Errorf("forge PID: got %d, want %d", rs2.Forge.PID, os.Getpid())
	}
}
