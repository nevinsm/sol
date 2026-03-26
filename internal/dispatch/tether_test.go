package dispatch

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// racingWorldStore wraps a real WorldStore and intercepts GetWrit.
// On the Nth call (1-indexed), it updates the writ status to "tethered"
// before returning, simulating a concurrent Cast completing between the
// pre-lock and post-lock reads inside Tether.
type racingWorldStore struct {
	WorldStore
	writID      string
	injectAfter int32 // inject after this many GetWrit calls (atomic)
	callCount   atomic.Int32
}

func (r *racingWorldStore) GetWrit(id string) (*store.Writ, error) {
	count := int(r.callCount.Add(1))
	// On the call number matching injectAfter, mutate the writ status first.
	if id == r.writID && count == int(r.injectAfter) {
		_ = r.WorldStore.UpdateWrit(id, store.WritUpdates{Status: "tethered", Assignee: "ember/other"})
	}
	return r.WorldStore.GetWrit(id)
}

// --- Tether tests ---

func TestTetherPersistentAgent(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWrit("Fix bug", "Fix the bug", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Meridian", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	result, err := Tether(TetherOpts{
		AgentName: "Meridian",
		WritID:    itemID,
		World:     "ember",
	}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Tether failed: %v", err)
	}

	if result.WritID != itemID {
		t.Errorf("expected writ ID %q, got %q", itemID, result.WritID)
	}
	if result.AgentName != "Meridian" {
		t.Errorf("expected agent name Meridian, got %q", result.AgentName)
	}
	if result.AgentRole != "envoy" {
		t.Errorf("expected role envoy, got %q", result.AgentRole)
	}

	// Verify tether file was written.
	if !tether.IsTetheredTo("ember", "Meridian", itemID, "envoy") {
		t.Error("expected tether file to exist")
	}

	// Verify writ was updated.
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "tethered" {
		t.Errorf("expected writ status 'tethered', got %q", item.Status)
	}
	if item.Assignee != "ember/Meridian" {
		t.Errorf("expected assignee 'ember/Meridian', got %q", item.Assignee)
	}

	// Verify agent state was updated.
	agent, err := sphereStore.GetAgent("ember/Meridian")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("expected agent state 'working', got %q", agent.State)
	}
	if agent.ActiveWrit != itemID {
		t.Errorf("expected active writ %q, got %q", itemID, agent.ActiveWrit)
	}
}

func TestTetherSecondWrit(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	item1, err := worldStore.CreateWrit("First task", "First", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ 1: %v", err)
	}
	item2, err := worldStore.CreateWrit("Second task", "Second", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ 2: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Meridian", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Tether first writ.
	_, err = Tether(TetherOpts{
		AgentName: "Meridian",
		WritID:    item1,
		World:     "ember",
	}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Tether first failed: %v", err)
	}

	// Tether second writ — should succeed.
	_, err = Tether(TetherOpts{
		AgentName: "Meridian",
		WritID:    item2,
		World:     "ember",
	}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Tether second failed: %v", err)
	}

	// Both tether files should exist.
	if !tether.IsTetheredTo("ember", "Meridian", item1, "envoy") {
		t.Error("expected first tether file to exist")
	}
	if !tether.IsTetheredTo("ember", "Meridian", item2, "envoy") {
		t.Error("expected second tether file to exist")
	}

	// First writ should still be the active_writ.
	agent, err := sphereStore.GetAgent("ember/Meridian")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.ActiveWrit != item1 {
		t.Errorf("expected active writ to remain %q (first), got %q", item1, agent.ActiveWrit)
	}
}

func TestTetherRejectsOutpost(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWrit("Add feature", "Add the feature", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	_, err = Tether(TetherOpts{
		AgentName: "Toast",
		WritID:    itemID,
		World:     "ember",
	}, worldStore, sphereStore, nil)
	if err == nil {
		t.Fatal("expected error for outpost agent")
	}
	if want := `agent "ember/Toast" has role "outpost" — only persistent roles (envoy, forge) can use tether; outposts use sol cast`; err.Error() != want {
		t.Errorf("unexpected error:\n  got:  %v\n  want: %s", err, want)
	}
}

func TestTetherRejectsNonOpenWrit(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWrit("Done task", "Already done", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered"}); err != nil {
		t.Fatalf("failed to update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Meridian", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	_, err = Tether(TetherOpts{
		AgentName: "Meridian",
		WritID:    itemID,
		World:     "ember",
	}, worldStore, sphereStore, nil)
	if err == nil {
		t.Fatal("expected error for non-open writ")
	}
	if want := `writ "` + itemID + `" has status "tethered", expected "open"`; err.Error() != want {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTetherRejectsUnknownAgent(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWrit("Task", "Do it", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	_, err = Tether(TetherOpts{
		AgentName: "Ghost",
		WritID:    itemID,
		World:     "ember",
	}, worldStore, sphereStore, nil)
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
}

// --- Untether tests ---

func TestUntetherHappyPath(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWrit("Fix bug", "Fix the bug", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Meridian", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// First tether.
	_, err = Tether(TetherOpts{
		AgentName: "Meridian",
		WritID:    itemID,
		World:     "ember",
	}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Tether failed: %v", err)
	}

	// Now untether.
	result, err := Untether(UntetherOpts{
		AgentName: "Meridian",
		WritID:    itemID,
		World:     "ember",
	}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Untether failed: %v", err)
	}

	if result.WritID != itemID {
		t.Errorf("expected writ ID %q, got %q", itemID, result.WritID)
	}
	if result.AgentName != "Meridian" {
		t.Errorf("expected agent name Meridian, got %q", result.AgentName)
	}
	if result.AgentRole != "envoy" {
		t.Errorf("expected role envoy, got %q", result.AgentRole)
	}

	// Verify tether was cleared.
	if tether.IsTetheredTo("ember", "Meridian", itemID, "envoy") {
		t.Error("expected tether file to be removed")
	}

	// Verify writ was reset.
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "open" {
		t.Errorf("expected writ status 'open', got %q", item.Status)
	}
	if item.Assignee != "" {
		t.Errorf("expected empty assignee, got %q", item.Assignee)
	}

	// Verify agent was reset to idle (no remaining tethers).
	agent, err := sphereStore.GetAgent("ember/Meridian")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("expected agent state 'idle', got %q", agent.State)
	}
	if agent.ActiveWrit != "" {
		t.Errorf("expected empty active writ, got %q", agent.ActiveWrit)
	}
}

func TestUntetherOneOfTwo(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	item1, err := worldStore.CreateWrit("First task", "First", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ 1: %v", err)
	}
	item2, err := worldStore.CreateWrit("Second task", "Second", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ 2: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Meridian", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Tether both.
	if _, err := Tether(TetherOpts{AgentName: "Meridian", WritID: item1, World: "ember"}, worldStore, sphereStore, nil); err != nil {
		t.Fatalf("Tether first failed: %v", err)
	}
	if _, err := Tether(TetherOpts{AgentName: "Meridian", WritID: item2, World: "ember"}, worldStore, sphereStore, nil); err != nil {
		t.Fatalf("Tether second failed: %v", err)
	}

	// Untether second — first should remain.
	_, err = Untether(UntetherOpts{AgentName: "Meridian", WritID: item2, World: "ember"}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Untether failed: %v", err)
	}

	// First tether should still exist.
	if !tether.IsTetheredTo("ember", "Meridian", item1, "envoy") {
		t.Error("expected first tether to remain")
	}
	// Second tether should be gone.
	if tether.IsTetheredTo("ember", "Meridian", item2, "envoy") {
		t.Error("expected second tether to be removed")
	}

	// Agent should still be working (has remaining tethers).
	agent, err := sphereStore.GetAgent("ember/Meridian")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("expected agent state 'working', got %q", agent.State)
	}

	// Second writ should be open.
	writ2, err := worldStore.GetWrit(item2)
	if err != nil {
		t.Fatalf("failed to get writ 2: %v", err)
	}
	if writ2.Status != "open" {
		t.Errorf("expected writ 2 status 'open', got %q", writ2.Status)
	}
}

func TestUntetherActiveWrit(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	item1, err := worldStore.CreateWrit("First task", "First", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ 1: %v", err)
	}
	item2, err := worldStore.CreateWrit("Second task", "Second", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ 2: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Meridian", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Tether both — first becomes active_writ.
	if _, err := Tether(TetherOpts{AgentName: "Meridian", WritID: item1, World: "ember"}, worldStore, sphereStore, nil); err != nil {
		t.Fatalf("Tether first failed: %v", err)
	}
	if _, err := Tether(TetherOpts{AgentName: "Meridian", WritID: item2, World: "ember"}, worldStore, sphereStore, nil); err != nil {
		t.Fatalf("Tether second failed: %v", err)
	}

	// Untether the active writ (first).
	_, err = Untether(UntetherOpts{AgentName: "Meridian", WritID: item1, World: "ember"}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Untether active writ failed: %v", err)
	}

	// Active writ should be cleared.
	agent, err := sphereStore.GetAgent("ember/Meridian")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.ActiveWrit != "" {
		t.Errorf("expected active writ cleared, got %q", agent.ActiveWrit)
	}
	// Agent should still be working (second tether remains).
	if agent.State != "working" {
		t.Errorf("expected agent state 'working', got %q", agent.State)
	}
}

func TestUntetherRejectsNonTetheredWrit(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWrit("Task", "Do it", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Meridian", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	_, err = Untether(UntetherOpts{
		AgentName: "Meridian",
		WritID:    itemID,
		World:     "ember",
	}, worldStore, sphereStore, nil)
	if err == nil {
		t.Fatal("expected error for non-tethered writ")
	}
	if want := `writ "` + itemID + `" is not tethered to agent "Meridian" in world "ember"`; err.Error() != want {
		t.Errorf("unexpected error:\n  got:  %v\n  want: %s", err, want)
	}
}

func TestTetherThenUntetherRoundTrip(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWrit("Round trip", "Test full cycle", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Meridian", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Tether.
	_, err = Tether(TetherOpts{
		AgentName: "Meridian",
		WritID:    itemID,
		World:     "ember",
	}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Tether failed: %v", err)
	}

	// Untether.
	_, err = Untether(UntetherOpts{
		AgentName: "Meridian",
		WritID:    itemID,
		World:     "ember",
	}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Untether failed: %v", err)
	}

	// Agent should be idle and retetherable.
	_, err = Tether(TetherOpts{
		AgentName: "Meridian",
		WritID:    itemID,
		World:     "ember",
	}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Re-tether after untether failed: %v", err)
	}
}

// TestTetherAgentStateBeforeTetherWrite verifies that Tether() sets agent
// state to "working" BEFORE writing the tether file. This ordering prevents
// a race with sentinel's cleanupOrphanedTethers, which skips agents that
// exist in the DB.
func TestTetherAgentStateBeforeTetherWrite(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWrit("Order test", "Verify ordering", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Meridian", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Verify agent starts idle.
	agent, err := sphereStore.GetAgent("ember/Meridian")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "idle" {
		t.Fatalf("expected agent to start idle, got %q", agent.State)
	}

	// Tether — this should set agent state before writing tether file.
	_, err = Tether(TetherOpts{
		AgentName: "Meridian",
		WritID:    itemID,
		World:     "ember",
	}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Tether failed: %v", err)
	}

	// After Tether completes, verify:
	// 1. Agent is "working" (was set before tether.Write)
	agent, err = sphereStore.GetAgent("ember/Meridian")
	if err != nil {
		t.Fatalf("failed to get agent after tether: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("expected agent state 'working', got %q", agent.State)
	}

	// 2. Tether file exists
	if !tether.IsTetheredTo("ember", "Meridian", itemID, "envoy") {
		t.Error("expected tether file to exist")
	}

	// 3. Writ is tethered
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("failed to get writ: %v", err)
	}
	if item.Status != "tethered" {
		t.Errorf("expected writ status 'tethered', got %q", item.Status)
	}
}

// TestTetherRejectsWritTetheredBetweenReadAndLock verifies the TOCTOU fix:
// if a concurrent Cast tethers a writ between Tether's pre-lock status check
// and the writ lock acquisition, the post-lock re-read detects the stale state
// and returns an error rather than double-tethering.
func TestTetherRejectsWritTetheredBetweenReadAndLock(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWrit("Race task", "Concurrent dispatch", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Meridian", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// racingWorldStore intercepts the 2nd GetWrit call (the post-lock re-read)
	// and updates the writ status to "tethered" before returning it, simulating
	// a concurrent Cast completing between the pre-lock check and lock acquisition.
	racing := &racingWorldStore{
		WorldStore:  worldStore,
		writID:      itemID,
		injectAfter: 2, // 1st call = pre-lock check (passes), 2nd = post-lock re-read (races)
	}

	_, err = Tether(TetherOpts{
		AgentName: "Meridian",
		WritID:    itemID,
		World:     "ember",
	}, racing, sphereStore, nil)

	if err == nil {
		t.Fatal("expected Tether to return an error when writ was tethered between reads, got nil")
	}
	if !strings.Contains(err.Error(), "expected \"open\"") {
		t.Errorf("expected error about writ status, got: %v", err)
	}

	// Verify no tether file was written for Meridian.
	if tether.IsTetheredTo("ember", "Meridian", itemID, "envoy") {
		t.Error("tether file must not exist after failed Tether")
	}

	// Verify agent state was not changed (should still be idle).
	agent, err := sphereStore.GetAgent("ember/Meridian")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("agent state must remain 'idle' after failed Tether, got %q", agent.State)
	}
}
