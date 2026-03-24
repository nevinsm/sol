package sitrep_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nevinsm/sol/internal/sitrep"
	"github.com/nevinsm/sol/internal/store"
)

func setupTestEnv(t *testing.T) (sphere *store.SphereStore, worldOpener sitrep.WorldOpener) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}

	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	opener := func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	}

	return s, opener
}

func TestCollectSphereEmpty(t *testing.T) {
	sphere, opener := setupTestEnv(t)

	data, err := sitrep.Collect(sphere, opener, sitrep.Scope{Sphere: true})
	if err != nil {
		t.Fatal(err)
	}

	if data.Scope != "sphere" {
		t.Errorf("expected scope %q, got %q", "sphere", data.Scope)
	}
	if len(data.Agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(data.Agents))
	}
	if len(data.Caravans) != 0 {
		t.Errorf("expected 0 caravans, got %d", len(data.Caravans))
	}
	if len(data.Worlds) != 0 {
		t.Errorf("expected 0 worlds, got %d", len(data.Worlds))
	}
}

func TestCollectWorldScoped(t *testing.T) {
	sphere, opener := setupTestEnv(t)

	// Register a world and create some data.
	if err := sphere.RegisterWorld("test-world", "/tmp/test"); err != nil {
		t.Fatal(err)
	}

	// Create an agent.
	if _, err := sphere.CreateAgent("Alpha", "test-world", "outpost"); err != nil {
		t.Fatal(err)
	}

	// Create writs in the world.
	ws, err := store.OpenWorld("test-world")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	if _, err := ws.CreateWrit("Test writ", "Description", "autarch", 2, nil); err != nil {
		t.Fatal(err)
	}

	data, err := sitrep.Collect(sphere, opener, sitrep.Scope{World: "test-world"})
	if err != nil {
		t.Fatal(err)
	}

	if data.Scope != "test-world" {
		t.Errorf("expected scope %q, got %q", "test-world", data.Scope)
	}
	if len(data.Agents) != 1 {
		t.Errorf("expected 1 agent, got %d", len(data.Agents))
	}
	if len(data.Worlds) != 1 {
		t.Errorf("expected 1 world, got %d", len(data.Worlds))
	}
	if len(data.Worlds) > 0 && len(data.Worlds[0].Writs) != 1 {
		t.Errorf("expected 1 writ, got %d", len(data.Worlds[0].Writs))
	}
}

func TestCollectSphereWithWorlds(t *testing.T) {
	sphere, opener := setupTestEnv(t)

	// Register two worlds.
	if err := sphere.RegisterWorld("alpha", "/tmp/alpha"); err != nil {
		t.Fatal(err)
	}
	if err := sphere.RegisterWorld("bravo", "/tmp/bravo"); err != nil {
		t.Fatal(err)
	}

	// Create agents in different worlds.
	if _, err := sphere.CreateAgent("A1", "alpha", "outpost"); err != nil {
		t.Fatal(err)
	}
	if _, err := sphere.CreateAgent("B1", "bravo", "outpost"); err != nil {
		t.Fatal(err)
	}

	data, err := sitrep.Collect(sphere, opener, sitrep.Scope{Sphere: true})
	if err != nil {
		t.Fatal(err)
	}

	if data.Scope != "sphere" {
		t.Errorf("expected scope %q, got %q", "sphere", data.Scope)
	}
	if len(data.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(data.Agents))
	}
	if len(data.Worlds) != 2 {
		t.Errorf("expected 2 worlds, got %d", len(data.Worlds))
	}
}

func TestCollectCaravansFilteredToActionable(t *testing.T) {
	sphere, opener := setupTestEnv(t)

	// Create caravans in different statuses.
	openID, err := sphere.CreateCaravan("open-caravan", "test")
	if err != nil {
		t.Fatal(err)
	}
	// New caravans start as drydock; move to open.
	if err := sphere.UpdateCaravanStatus(openID, "open"); err != nil {
		t.Fatal(err)
	}

	drydockID, err := sphere.CreateCaravan("drydock-caravan", "test")
	if err != nil {
		t.Fatal(err)
	}
	_ = drydockID // stays drydock (default)

	closedID, err := sphere.CreateCaravan("closed-caravan", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := sphere.UpdateCaravanStatus(closedID, "open"); err != nil {
		t.Fatal(err)
	}
	if err := sphere.UpdateCaravanStatus(closedID, "closed"); err != nil {
		t.Fatal(err)
	}

	data, err := sitrep.Collect(sphere, opener, sitrep.Scope{Sphere: true})
	if err != nil {
		t.Fatal(err)
	}

	// Should contain only open + drydock, not closed.
	if len(data.Caravans) != 2 {
		t.Errorf("expected 2 caravans (open + drydock), got %d", len(data.Caravans))
	}

	// Verify no closed caravans in results.
	for _, c := range data.Caravans {
		if c.Status == "closed" {
			t.Errorf("found closed caravan %q in collected data", c.ID)
		}
	}
}

func TestCollectMergeRequestsFilteredToActionable(t *testing.T) {
	sphere, opener := setupTestEnv(t)

	if err := sphere.RegisterWorld("test-world", "/tmp/test"); err != nil {
		t.Fatal(err)
	}

	ws, err := store.OpenWorld("test-world")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	// Create writs to attach MRs to.
	writID1, err := ws.CreateWrit("Writ 1", "desc", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	writID2, err := ws.CreateWrit("Writ 2", "desc", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	writID3, err := ws.CreateWrit("Writ 3", "desc", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	writID4, err := ws.CreateWrit("Writ 4", "desc", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create MRs in various phases.
	readyMR, err := ws.CreateMergeRequest(writID1, "branch-ready", 2)
	if err != nil {
		t.Fatal(err)
	}
	_ = readyMR // stays ready (default)

	claimedMR, err := ws.CreateMergeRequest(writID2, "branch-claimed", 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.UpdateMergeRequestPhase(claimedMR, store.MRClaimed); err != nil {
		t.Fatal(err)
	}

	failedMR, err := ws.CreateMergeRequest(writID3, "branch-failed", 2)
	if err != nil {
		t.Fatal(err)
	}
	// ready → claimed → failed
	if err := ws.UpdateMergeRequestPhase(failedMR, store.MRClaimed); err != nil {
		t.Fatal(err)
	}
	if err := ws.UpdateMergeRequestPhase(failedMR, store.MRFailed); err != nil {
		t.Fatal(err)
	}

	mergedMR, err := ws.CreateMergeRequest(writID4, "branch-merged", 2)
	if err != nil {
		t.Fatal(err)
	}
	// ready → claimed → merged
	if err := ws.UpdateMergeRequestPhase(mergedMR, store.MRClaimed); err != nil {
		t.Fatal(err)
	}
	if err := ws.UpdateMergeRequestPhase(mergedMR, store.MRMerged); err != nil {
		t.Fatal(err)
	}

	data, err := sitrep.Collect(sphere, opener, sitrep.Scope{World: "test-world"})
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Worlds) != 1 {
		t.Fatalf("expected 1 world, got %d", len(data.Worlds))
	}
	wd := data.Worlds[0]

	// MergeRequests should contain only ready, claimed, failed (3 total).
	if len(wd.MergeRequests) != 3 {
		t.Errorf("expected 3 actionable MRs, got %d", len(wd.MergeRequests))
	}

	// Verify no merged/superseded MRs in detail list.
	for _, mr := range wd.MergeRequests {
		if mr.Phase == store.MRMerged || mr.Phase == store.MRSuperseded {
			t.Errorf("found terminal MR phase %q in collected MergeRequests", mr.Phase)
		}
	}
}

func TestCollectMRSummary(t *testing.T) {
	sphere, opener := setupTestEnv(t)

	if err := sphere.RegisterWorld("test-world", "/tmp/test"); err != nil {
		t.Fatal(err)
	}

	ws, err := store.OpenWorld("test-world")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	// Create writs and MRs in various phases.
	writID1, err := ws.CreateWrit("Writ 1", "desc", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	writID2, err := ws.CreateWrit("Writ 2", "desc", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	writID3, err := ws.CreateWrit("Writ 3", "desc", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}

	// ready MR
	if _, err := ws.CreateMergeRequest(writID1, "branch-1", 2); err != nil {
		t.Fatal(err)
	}

	// merged MR (ready → claimed → merged)
	mr2, err := ws.CreateMergeRequest(writID2, "branch-2", 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.UpdateMergeRequestPhase(mr2, store.MRClaimed); err != nil {
		t.Fatal(err)
	}
	if err := ws.UpdateMergeRequestPhase(mr2, store.MRMerged); err != nil {
		t.Fatal(err)
	}

	// failed MR (ready → claimed → failed)
	mr3, err := ws.CreateMergeRequest(writID3, "branch-3", 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.UpdateMergeRequestPhase(mr3, store.MRClaimed); err != nil {
		t.Fatal(err)
	}
	if err := ws.UpdateMergeRequestPhase(mr3, store.MRFailed); err != nil {
		t.Fatal(err)
	}

	data, err := sitrep.Collect(sphere, opener, sitrep.Scope{World: "test-world"})
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Worlds) != 1 {
		t.Fatalf("expected 1 world, got %d", len(data.Worlds))
	}
	wd := data.Worlds[0]

	// MRSummary should exist with correct counts.
	if wd.MRSummary == nil {
		t.Fatal("expected MRSummary to be non-nil")
	}
	if wd.MRSummary["total"] != 3 {
		t.Errorf("expected total=3, got %d", wd.MRSummary["total"])
	}
	if wd.MRSummary["ready"] != 1 {
		t.Errorf("expected ready=1, got %d", wd.MRSummary["ready"])
	}
	if wd.MRSummary["merged"] != 1 {
		t.Errorf("expected merged=1, got %d", wd.MRSummary["merged"])
	}
	if wd.MRSummary["failed"] != 1 {
		t.Errorf("expected failed=1, got %d", wd.MRSummary["failed"])
	}
}

func TestCollectForgeStatus(t *testing.T) {
	sphere, opener := setupTestEnv(t)

	if err := sphere.RegisterWorld("test-world", "/tmp/test"); err != nil {
		t.Fatal(err)
	}

	ws, err := store.OpenWorld("test-world")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	// Create writs and MRs in various phases.
	writID1, err := ws.CreateWrit("Ready writ", "desc", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	writID2, err := ws.CreateWrit("Claimed writ", "desc", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	writID3, err := ws.CreateWrit("Failed writ", "desc", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	writID4, err := ws.CreateWrit("Merged writ", "desc", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Ready MR.
	if _, err := ws.CreateMergeRequest(writID1, "branch-ready", 2); err != nil {
		t.Fatal(err)
	}

	// Claimed MR (ready → claimed).
	claimedMR, err := ws.CreateMergeRequest(writID2, "branch-claimed", 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.UpdateMergeRequestPhase(claimedMR, store.MRClaimed); err != nil {
		t.Fatal(err)
	}

	// Failed MR (ready → claimed → failed).
	failedMR, err := ws.CreateMergeRequest(writID3, "branch-failed", 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.UpdateMergeRequestPhase(failedMR, store.MRClaimed); err != nil {
		t.Fatal(err)
	}
	if err := ws.UpdateMergeRequestPhase(failedMR, store.MRFailed); err != nil {
		t.Fatal(err)
	}

	// Merged MR (ready → claimed → merged).
	mergedMR, err := ws.CreateMergeRequest(writID4, "branch-merged", 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.UpdateMergeRequestPhase(mergedMR, store.MRClaimed); err != nil {
		t.Fatal(err)
	}
	if err := ws.UpdateMergeRequestPhase(mergedMR, store.MRMerged); err != nil {
		t.Fatal(err)
	}

	data, err := sitrep.Collect(sphere, opener, sitrep.Scope{World: "test-world"})
	if err != nil {
		t.Fatal(err)
	}

	// ForgeStatuses should include the world.
	fs, ok := data.ForgeStatuses["test-world"]
	if !ok {
		t.Fatal("expected forge status for test-world")
	}

	// Process state should be off (no real forge running in tests).
	if fs.Running {
		t.Error("expected running=false in test environment")
	}
	if fs.Paused {
		t.Error("expected paused=false in test environment")
	}
	if fs.Merging {
		t.Error("expected merging=false in test environment")
	}

	// Queue counts from MR data.
	if fs.QueueReady != 1 {
		t.Errorf("expected queue_ready=1, got %d", fs.QueueReady)
	}
	if fs.QueueFailed != 1 {
		t.Errorf("expected queue_failed=1, got %d", fs.QueueFailed)
	}
	if fs.QueueBlocked != 0 {
		t.Errorf("expected queue_blocked=0, got %d", fs.QueueBlocked)
	}

	// Merged counts.
	if fs.MergedTotal != 1 {
		t.Errorf("expected merged_total=1, got %d", fs.MergedTotal)
	}

	// Claimed MR detail.
	if fs.ClaimedMR == nil {
		t.Fatal("expected claimed MR detail")
	}
	if fs.ClaimedMR.Title != "Claimed writ" {
		t.Errorf("expected claimed MR title %q, got %q", "Claimed writ", fs.ClaimedMR.Title)
	}

	// Last failure.
	if fs.LastFailure == nil {
		t.Fatal("expected last failure event")
	}

	// Last merge.
	if fs.LastMerge == nil {
		t.Fatal("expected last merge event")
	}
}

func TestCollectForgeStatusSphereScope(t *testing.T) {
	sphere, opener := setupTestEnv(t)

	if err := sphere.RegisterWorld("alpha", "/tmp/alpha"); err != nil {
		t.Fatal(err)
	}
	if err := sphere.RegisterWorld("bravo", "/tmp/bravo"); err != nil {
		t.Fatal(err)
	}

	data, err := sitrep.Collect(sphere, opener, sitrep.Scope{Sphere: true})
	if err != nil {
		t.Fatal(err)
	}

	// Both worlds should have forge status entries.
	if len(data.ForgeStatuses) != 2 {
		t.Errorf("expected 2 forge statuses, got %d", len(data.ForgeStatuses))
	}
	if _, ok := data.ForgeStatuses["alpha"]; !ok {
		t.Error("expected forge status for alpha")
	}
	if _, ok := data.ForgeStatuses["bravo"]; !ok {
		t.Error("expected forge status for bravo")
	}
}
