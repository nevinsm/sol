package deacon

import (
	"os"
	"testing"
	"time"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/hook"
	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/store"
)

// mockSessionChecker tracks which sessions are "alive".
type mockSessionChecker struct {
	alive map[string]bool
}

func newMockSessions() *mockSessionChecker {
	return &mockSessionChecker{alive: make(map[string]bool)}
}

func (m *mockSessionChecker) Exists(name string) bool {
	return m.alive[name]
}

func (m *mockSessionChecker) List() ([]session.SessionInfo, error) {
	return nil, nil
}

// setupGTHome creates a temporary GT_HOME and sets the env var.
// Returns the path and a cleanup function.
func setupGTHome(t *testing.T) string {
	t.Helper()
	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	config.EnsureDirs()
	return gtHome
}

func TestRecoverStaleHooks(t *testing.T) {
	gtHome := setupGTHome(t)

	// Open real stores.
	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("failed to open town store: %v", err)
	}
	defer townStore.Close()

	rigName := "testrig"
	rigStore, err := store.OpenRig(rigName)
	if err != nil {
		t.Fatalf("failed to open rig store: %v", err)
	}
	defer rigStore.Close()

	// Create agents.
	// Agent A: working, session dead, old timestamp → should be recovered.
	townStore.CreateAgent("AgentA", rigName, "polecat")
	wiA, _ := rigStore.CreateWorkItem("task-a", "description a", "test", 1, nil)
	townStore.UpdateAgentState(rigName+"/AgentA", "working", wiA)
	rigStore.UpdateWorkItem(wiA, store.WorkItemUpdates{Status: "hooked", Assignee: rigName + "/AgentA"})
	hook.Write(rigName, "AgentA", wiA)

	// Make Agent A's updated_at old (> 1 hour ago).
	townStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), rigName+"/AgentA")

	// Agent B: working, session alive → should NOT be recovered.
	townStore.CreateAgent("AgentB", rigName, "polecat")
	wiB, _ := rigStore.CreateWorkItem("task-b", "description b", "test", 1, nil)
	townStore.UpdateAgentState(rigName+"/AgentB", "working", wiB)
	rigStore.UpdateWorkItem(wiB, store.WorkItemUpdates{Status: "hooked", Assignee: rigName + "/AgentB"})
	hook.Write(rigName, "AgentB", wiB)

	// Agent C: idle → should NOT be recovered.
	townStore.CreateAgent("AgentC", rigName, "polecat")

	// Set up mock sessions: only AgentB is alive.
	sessions := newMockSessions()
	sessions.alive["gt-"+rigName+"-AgentB"] = true

	cfg := Config{
		StaleHookTimeout: 1 * time.Hour,
		GTHome:           gtHome,
	}

	d := New(cfg, townStore, sessions, nil, nil)
	d.SetRigOpener(func(rig string) (*store.Store, error) {
		return store.OpenRig(rig)
	})

	recovered, err := d.recoverStaleHooks()
	if err != nil {
		t.Fatalf("recoverStaleHooks failed: %v", err)
	}
	if recovered != 1 {
		t.Errorf("recovered = %d, want 1", recovered)
	}

	// Verify Agent A was recovered.
	agentA, _ := townStore.GetAgent(rigName + "/AgentA")
	if agentA.State != "idle" {
		t.Errorf("AgentA state = %q, want idle", agentA.State)
	}
	if agentA.HookItem != "" {
		t.Errorf("AgentA hook_item = %q, want empty", agentA.HookItem)
	}

	// Verify work item A is back to open.
	rigStore2, _ := store.OpenRig(rigName)
	defer rigStore2.Close()
	itemA, _ := rigStore2.GetWorkItem(wiA)
	if itemA.Status != "open" {
		t.Errorf("work item A status = %q, want open", itemA.Status)
	}
	if itemA.Assignee != "" {
		t.Errorf("work item A assignee = %q, want empty", itemA.Assignee)
	}

	// Verify hook file was cleared.
	if hook.IsHooked(rigName, "AgentA") {
		t.Error("AgentA hook file should have been cleared")
	}

	// Verify Agent B is untouched.
	agentB, _ := townStore.GetAgent(rigName + "/AgentB")
	if agentB.State != "working" {
		t.Errorf("AgentB state = %q, want working", agentB.State)
	}

	// Verify Agent C is untouched.
	agentC, _ := townStore.GetAgent(rigName + "/AgentC")
	if agentC.State != "idle" {
		t.Errorf("AgentC state = %q, want idle", agentC.State)
	}
}

func TestRecoverStaleHooksTooRecent(t *testing.T) {
	gtHome := setupGTHome(t)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("failed to open town store: %v", err)
	}
	defer townStore.Close()

	rigName := "testrig2"
	rigStore, err := store.OpenRig(rigName)
	if err != nil {
		t.Fatalf("failed to open rig store: %v", err)
	}
	defer rigStore.Close()

	// Agent with dead session but updated_at is 5 minutes ago.
	townStore.CreateAgent("RecentAgent", rigName, "polecat")
	wiID, _ := rigStore.CreateWorkItem("task-recent", "desc", "test", 1, nil)
	townStore.UpdateAgentState(rigName+"/RecentAgent", "working", wiID)
	rigStore.UpdateWorkItem(wiID, store.WorkItemUpdates{Status: "hooked", Assignee: rigName + "/RecentAgent"})
	hook.Write(rigName, "RecentAgent", wiID)

	// Set updated_at to 5 minutes ago.
	townStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-5*time.Minute).UTC().Format(time.RFC3339), rigName+"/RecentAgent")

	sessions := newMockSessions() // no alive sessions

	cfg := Config{
		StaleHookTimeout: 1 * time.Hour, // 1 hour timeout, 5 min is too recent
		GTHome:           gtHome,
	}

	d := New(cfg, townStore, sessions, nil, nil)
	d.SetRigOpener(func(rig string) (*store.Store, error) {
		return store.OpenRig(rig)
	})

	recovered, err := d.recoverStaleHooks()
	if err != nil {
		t.Fatalf("recoverStaleHooks failed: %v", err)
	}
	if recovered != 0 {
		t.Errorf("recovered = %d, want 0 (too recent)", recovered)
	}

	// Verify agent is still working.
	agent, _ := townStore.GetAgent(rigName + "/RecentAgent")
	if agent.State != "working" {
		t.Errorf("agent state = %q, want working", agent.State)
	}
}

func TestRecoverStaleHooksPartialFailure(t *testing.T) {
	gtHome := setupGTHome(t)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("failed to open town store: %v", err)
	}
	defer townStore.Close()

	rigName := "testrig3"
	rigStore, err := store.OpenRig(rigName)
	if err != nil {
		t.Fatalf("failed to open rig store: %v", err)
	}
	defer rigStore.Close()

	// Agent 1: stale, recoverable.
	townStore.CreateAgent("Good", rigName, "polecat")
	wi1, _ := rigStore.CreateWorkItem("task-good", "desc", "test", 1, nil)
	townStore.UpdateAgentState(rigName+"/Good", "working", wi1)
	rigStore.UpdateWorkItem(wi1, store.WorkItemUpdates{Status: "hooked", Assignee: rigName + "/Good"})
	hook.Write(rigName, "Good", wi1)

	// Agent 2: stale, but on a rig that can't be opened (bad rig name).
	townStore.CreateAgent("Bad", "nonexistent-rig-xyz", "polecat")
	townStore.UpdateAgentState("nonexistent-rig-xyz/Bad", "working", "fake-item")

	// Make both agents old.
	townStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), rigName+"/Good")
	townStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), "nonexistent-rig-xyz/Bad")

	sessions := newMockSessions() // no alive sessions

	cfg := Config{
		StaleHookTimeout: 1 * time.Hour,
		GTHome:           gtHome,
	}

	d := New(cfg, townStore, sessions, nil, nil)
	d.SetRigOpener(func(rig string) (*store.Store, error) {
		if rig == "nonexistent-rig-xyz" {
			// Simulate opening a rig store — create it to open, but
			// the work item won't exist, causing a controlled failure.
			s, err := store.OpenRig(rig)
			if err != nil {
				return nil, err
			}
			return s, nil
		}
		return store.OpenRig(rig)
	})

	recovered, err := d.recoverStaleHooks()
	if err != nil {
		t.Fatalf("recoverStaleHooks failed: %v", err)
	}

	// Good should be recovered. Bad should be skipped due to work item not found.
	if recovered != 1 {
		t.Errorf("recovered = %d, want 1 (partial failure)", recovered)
	}
}

func TestFeedStrandedConvoys(t *testing.T) {
	gtHome := setupGTHome(t)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("failed to open town store: %v", err)
	}
	defer townStore.Close()

	rigName := "convoyrig"
	rigStore, err := store.OpenRig(rigName)
	if err != nil {
		t.Fatalf("failed to open rig store: %v", err)
	}
	defer rigStore.Close()

	// Create a convoy with 3 work items: 2 open (ready), 1 hooked.
	convoyID, _ := townStore.CreateConvoy("test-convoy", "operator")

	wi1, _ := rigStore.CreateWorkItem("convoy-task-1", "desc1", "test", 1, nil)
	wi2, _ := rigStore.CreateWorkItem("convoy-task-2", "desc2", "test", 1, nil)
	wi3, _ := rigStore.CreateWorkItem("convoy-task-3", "desc3", "test", 1, nil)

	townStore.AddConvoyItem(convoyID, wi1, rigName)
	townStore.AddConvoyItem(convoyID, wi2, rigName)
	townStore.AddConvoyItem(convoyID, wi3, rigName)

	// Make wi3 hooked (already dispatched).
	rigStore.UpdateWorkItem(wi3, store.WorkItemUpdates{Status: "hooked", Assignee: rigName + "/SomeAgent"})

	sessions := newMockSessions()
	cfg := Config{
		StaleHookTimeout: 1 * time.Hour,
		GTHome:           gtHome,
	}

	d := New(cfg, townStore, sessions, nil, nil)
	d.SetRigOpener(func(rig string) (*store.Store, error) {
		return store.OpenRig(rig)
	})

	fed, err := d.feedStrandedConvoys()
	if err != nil {
		t.Fatalf("feedStrandedConvoys failed: %v", err)
	}
	if fed != 1 {
		t.Errorf("fed = %d, want 1", fed)
	}

	// Verify CONVOY_NEEDS_FEEDING message was sent.
	pending, _ := townStore.PendingProtocol("operator", store.ProtoConvoyNeedsFeeding)
	if len(pending) != 1 {
		t.Fatalf("pending messages = %d, want 1", len(pending))
	}
	if pending[0].Subject != store.ProtoConvoyNeedsFeeding {
		t.Errorf("message subject = %q, want %q", pending[0].Subject, store.ProtoConvoyNeedsFeeding)
	}
}

func TestFeedStrandedConvoysNoDuplicates(t *testing.T) {
	gtHome := setupGTHome(t)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("failed to open town store: %v", err)
	}
	defer townStore.Close()

	rigName := "convoyrig2"
	rigStore, err := store.OpenRig(rigName)
	if err != nil {
		t.Fatalf("failed to open rig store: %v", err)
	}
	defer rigStore.Close()

	convoyID, _ := townStore.CreateConvoy("test-convoy-2", "operator")
	wi1, _ := rigStore.CreateWorkItem("dup-task-1", "desc1", "test", 1, nil)
	townStore.AddConvoyItem(convoyID, wi1, rigName)

	// Send a pre-existing CONVOY_NEEDS_FEEDING message for this convoy.
	townStore.SendProtocolMessage("town/deacon", "operator", store.ProtoConvoyNeedsFeeding,
		store.ConvoyNeedsFeedingPayload{ConvoyID: convoyID, ReadyCount: 1})

	sessions := newMockSessions()
	cfg := Config{
		StaleHookTimeout: 1 * time.Hour,
		GTHome:           gtHome,
	}

	d := New(cfg, townStore, sessions, nil, nil)
	d.SetRigOpener(func(rig string) (*store.Store, error) {
		return store.OpenRig(rig)
	})

	fed, err := d.feedStrandedConvoys()
	if err != nil {
		t.Fatalf("feedStrandedConvoys failed: %v", err)
	}
	if fed != 0 {
		t.Errorf("fed = %d, want 0 (already pending)", fed)
	}

	// Verify still only 1 message.
	pending, _ := townStore.PendingProtocol("operator", store.ProtoConvoyNeedsFeeding)
	if len(pending) != 1 {
		t.Errorf("pending messages = %d, want 1 (no duplicate)", len(pending))
	}
}

func TestFeedStrandedConvoysAllDispatched(t *testing.T) {
	gtHome := setupGTHome(t)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("failed to open town store: %v", err)
	}
	defer townStore.Close()

	rigName := "convoyrig3"
	rigStore, err := store.OpenRig(rigName)
	if err != nil {
		t.Fatalf("failed to open rig store: %v", err)
	}
	defer rigStore.Close()

	convoyID, _ := townStore.CreateConvoy("test-convoy-3", "operator")
	wi1, _ := rigStore.CreateWorkItem("all-hooked-1", "desc1", "test", 1, nil)
	rigStore.UpdateWorkItem(wi1, store.WorkItemUpdates{Status: "hooked", Assignee: rigName + "/X"})
	townStore.AddConvoyItem(convoyID, wi1, rigName)

	sessions := newMockSessions()
	cfg := Config{
		StaleHookTimeout: 1 * time.Hour,
		GTHome:           gtHome,
	}

	d := New(cfg, townStore, sessions, nil, nil)
	d.SetRigOpener(func(rig string) (*store.Store, error) {
		return store.OpenRig(rig)
	})

	fed, err := d.feedStrandedConvoys()
	if err != nil {
		t.Fatalf("feedStrandedConvoys failed: %v", err)
	}
	if fed != 0 {
		t.Errorf("fed = %d, want 0 (all dispatched)", fed)
	}
}

func TestProcessLifecycleShutdown(t *testing.T) {
	gtHome := setupGTHome(t)
	_ = gtHome

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("failed to open town store: %v", err)
	}
	defer townStore.Close()

	// Send SHUTDOWN message to "town/deacon".
	townStore.SendProtocolMessage("operator", "town/deacon", "SHUTDOWN", nil)

	sessions := newMockSessions()
	cfg := DefaultConfig()

	d := New(cfg, townStore, sessions, nil, nil)

	shutdown, err := d.processLifecycleRequests()
	if err != nil {
		t.Fatalf("processLifecycleRequests failed: %v", err)
	}
	if !shutdown {
		t.Error("expected shutdown=true")
	}

	// Verify message was acknowledged.
	pending, _ := townStore.PendingProtocol("town/deacon", "")
	if len(pending) != 0 {
		t.Errorf("pending messages = %d, want 0 (acked)", len(pending))
	}
}

func TestProcessLifecycleCycle(t *testing.T) {
	gtHome := setupGTHome(t)
	_ = gtHome

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("failed to open town store: %v", err)
	}
	defer townStore.Close()

	townStore.SendProtocolMessage("operator", "town/deacon", "CYCLE", nil)

	sessions := newMockSessions()
	cfg := DefaultConfig()

	d := New(cfg, townStore, sessions, nil, nil)

	shutdown, err := d.processLifecycleRequests()
	if err != nil {
		t.Fatalf("processLifecycleRequests failed: %v", err)
	}
	if shutdown {
		t.Error("expected shutdown=false for CYCLE")
	}

	// Verify message was acknowledged.
	pending, _ := townStore.PendingProtocol("town/deacon", "")
	if len(pending) != 0 {
		t.Errorf("pending messages = %d, want 0 (acked)", len(pending))
	}
}

func TestProcessLifecycleUnknown(t *testing.T) {
	gtHome := setupGTHome(t)
	_ = gtHome

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("failed to open town store: %v", err)
	}
	defer townStore.Close()

	townStore.SendProtocolMessage("operator", "town/deacon", "UNKNOWN_CMD", nil)

	sessions := newMockSessions()
	cfg := DefaultConfig()

	d := New(cfg, townStore, sessions, nil, nil)

	shutdown, err := d.processLifecycleRequests()
	if err != nil {
		t.Fatalf("processLifecycleRequests failed: %v", err)
	}
	if shutdown {
		t.Error("expected shutdown=false for unknown message")
	}

	// Verify message was acknowledged (even though unknown).
	pending, _ := townStore.PendingProtocol("town/deacon", "")
	if len(pending) != 0 {
		t.Errorf("pending messages = %d, want 0 (acked)", len(pending))
	}
}

func TestPatrolCycle(t *testing.T) {
	gtHome := setupGTHome(t)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("failed to open town store: %v", err)
	}
	defer townStore.Close()

	rigName := "patrolrig"
	rigStore, err := store.OpenRig(rigName)
	if err != nil {
		t.Fatalf("failed to open rig store: %v", err)
	}
	defer rigStore.Close()

	// 1. Stale hooked agent (dead session, old timestamp).
	townStore.CreateAgent("Stale", rigName, "polecat")
	wiStale, _ := rigStore.CreateWorkItem("stale-task", "desc", "test", 1, nil)
	townStore.UpdateAgentState(rigName+"/Stale", "working", wiStale)
	rigStore.UpdateWorkItem(wiStale, store.WorkItemUpdates{Status: "hooked", Assignee: rigName + "/Stale"})
	hook.Write(rigName, "Stale", wiStale)
	townStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), rigName+"/Stale")

	// 2. Open convoy with ready items.
	convoyID, _ := townStore.CreateConvoy("patrol-convoy", "operator")
	wiConvoy, _ := rigStore.CreateWorkItem("convoy-ready", "desc", "test", 1, nil)
	townStore.AddConvoyItem(convoyID, wiConvoy, rigName)

	// 3. Healthy working agent (session alive).
	townStore.CreateAgent("Healthy", rigName, "polecat")
	wiHealthy, _ := rigStore.CreateWorkItem("healthy-task", "desc", "test", 1, nil)
	townStore.UpdateAgentState(rigName+"/Healthy", "working", wiHealthy)
	rigStore.UpdateWorkItem(wiHealthy, store.WorkItemUpdates{Status: "hooked", Assignee: rigName + "/Healthy"})
	hook.Write(rigName, "Healthy", wiHealthy)

	sessions := newMockSessions()
	sessions.alive["gt-"+rigName+"-Healthy"] = true

	cfg := Config{
		StaleHookTimeout: 1 * time.Hour,
		GTHome:           gtHome,
		PatrolInterval:   5 * time.Minute,
	}

	d := New(cfg, townStore, sessions, nil, nil)
	d.SetRigOpener(func(rig string) (*store.Store, error) {
		return store.OpenRig(rig)
	})

	err = d.Patrol()
	if err != nil {
		t.Fatalf("Patrol failed: %v", err)
	}

	// Verify: stale hook recovered.
	agentStale, _ := townStore.GetAgent(rigName + "/Stale")
	if agentStale.State != "idle" {
		t.Errorf("Stale agent state = %q, want idle", agentStale.State)
	}

	// Verify: convoy feed message sent.
	pending, _ := townStore.PendingProtocol("operator", store.ProtoConvoyNeedsFeeding)
	if len(pending) == 0 {
		t.Error("expected CONVOY_NEEDS_FEEDING message")
	}

	// Verify: heartbeat written.
	hb, err := ReadHeartbeat(gtHome)
	if err != nil {
		t.Fatalf("ReadHeartbeat failed: %v", err)
	}
	if hb == nil {
		t.Fatal("expected heartbeat to be written")
	}
	if hb.PatrolCount != 1 {
		t.Errorf("heartbeat patrol_count = %d, want 1", hb.PatrolCount)
	}
	if hb.StaleHooks != 1 {
		t.Errorf("heartbeat stale_hooks = %d, want 1", hb.StaleHooks)
	}
	if hb.ConvoyFeeds != 1 {
		t.Errorf("heartbeat convoy_feeds = %d, want 1", hb.ConvoyFeeds)
	}

	// Verify: healthy agent untouched.
	agentHealthy, _ := townStore.GetAgent(rigName + "/Healthy")
	if agentHealthy.State != "working" {
		t.Errorf("Healthy agent state = %q, want working", agentHealthy.State)
	}

	// Verify: hook file still present for healthy agent.
	if !hook.IsHooked(rigName, "Healthy") {
		t.Error("Healthy agent hook file should still exist")
	}

	// Clean up.
	os.RemoveAll(gtHome)
}
