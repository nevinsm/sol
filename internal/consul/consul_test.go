package consul

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
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

// setupSolHome creates a temporary SOL_HOME and sets the env var.
// Returns the path and a cleanup function.
func setupSolHome(t *testing.T) string {
	t.Helper()
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	config.EnsureDirs()
	return solHome
}

func TestRecoverStaleTethers(t *testing.T) {
	solHome := setupSolHome(t)

	// Open real stores.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "ember"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	// Create agents.
	// Agent A: working, session dead, old timestamp → should be recovered.
	sphereStore.CreateAgent("AgentA", worldName, "agent")
	wiA, _ := worldStore.CreateWorkItem("task-a", "description a", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/AgentA", "working", wiA)
	worldStore.UpdateWorkItem(wiA, store.WorkItemUpdates{Status: "tethered", Assignee: worldName + "/AgentA"})
	tether.Write(worldName, "AgentA", wiA, "agent")

	// Make Agent A's updated_at old (> 1 hour ago).
	sphereStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), worldName+"/AgentA")

	// Agent B: working, session alive → should NOT be recovered.
	sphereStore.CreateAgent("AgentB", worldName, "agent")
	wiB, _ := worldStore.CreateWorkItem("task-b", "description b", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/AgentB", "working", wiB)
	worldStore.UpdateWorkItem(wiB, store.WorkItemUpdates{Status: "tethered", Assignee: worldName + "/AgentB"})
	tether.Write(worldName, "AgentB", wiB, "agent")

	// Agent C: idle → should NOT be recovered.
	sphereStore.CreateAgent("AgentC", worldName, "agent")

	// Set up mock sessions: only AgentB is alive.
	sessions := newMockSessions()
	sessions.alive["sol-"+worldName+"-AgentB"] = true

	cfg := Config{
		StaleTetherTimeout: 1 * time.Hour,
		SolHome:             solHome,
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.Store, error) {
		return store.OpenWorld(world)
	})

	recovered, err := d.recoverStaleTethers(context.Background())
	if err != nil {
		t.Fatalf("recoverStaleTethers failed: %v", err)
	}
	if recovered != 1 {
		t.Errorf("recovered = %d, want 1", recovered)
	}

	// Verify Agent A was recovered.
	agentA, _ := sphereStore.GetAgent(worldName + "/AgentA")
	if agentA.State != "idle" {
		t.Errorf("AgentA state = %q, want idle", agentA.State)
	}
	if agentA.TetherItem != "" {
		t.Errorf("AgentA tether_item = %q, want empty", agentA.TetherItem)
	}

	// Verify work item A is back to open.
	worldStore2, _ := store.OpenWorld(worldName)
	defer worldStore2.Close()
	itemA, _ := worldStore2.GetWorkItem(wiA)
	if itemA.Status != "open" {
		t.Errorf("work item A status = %q, want open", itemA.Status)
	}
	if itemA.Assignee != "" {
		t.Errorf("work item A assignee = %q, want empty", itemA.Assignee)
	}

	// Verify tether file was cleared.
	if tether.IsTethered(worldName, "AgentA", "agent") {
		t.Error("AgentA tether file should have been cleared")
	}

	// Verify Agent B is untouched.
	agentB, _ := sphereStore.GetAgent(worldName + "/AgentB")
	if agentB.State != "working" {
		t.Errorf("AgentB state = %q, want working", agentB.State)
	}

	// Verify Agent C is untouched.
	agentC, _ := sphereStore.GetAgent(worldName + "/AgentC")
	if agentC.State != "idle" {
		t.Errorf("AgentC state = %q, want idle", agentC.State)
	}
}

func TestRecoverStaleTethersEnvoyAndGovernor(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "ember-eg"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	// Envoy: working, session dead, old timestamp → should be recovered.
	sphereStore.CreateAgent("MyEnvoy", worldName, "envoy")
	wiEnvoy, _ := worldStore.CreateWorkItem("task-envoy", "envoy work", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/MyEnvoy", "working", wiEnvoy)
	worldStore.UpdateWorkItem(wiEnvoy, store.WorkItemUpdates{Status: "tethered", Assignee: worldName + "/MyEnvoy"})
	tether.Write(worldName, "MyEnvoy", wiEnvoy, "envoy")

	// Governor: working, session dead, old timestamp → should be recovered.
	sphereStore.CreateAgent("MyGovernor", worldName, "governor")
	wiGov, _ := worldStore.CreateWorkItem("task-governor", "governor work", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/MyGovernor", "working", wiGov)
	worldStore.UpdateWorkItem(wiGov, store.WorkItemUpdates{Status: "tethered", Assignee: worldName + "/MyGovernor"})
	tether.Write(worldName, "MyGovernor", wiGov, "governor")

	// Sentinel: working, session dead, old timestamp → should NOT be recovered.
	sphereStore.CreateAgent("sentinel", worldName, "sentinel")
	sphereStore.UpdateAgentState(worldName+"/sentinel", "working", "fake-sentinel-item")

	// Make all agents old.
	for _, id := range []string{worldName + "/MyEnvoy", worldName + "/MyGovernor", worldName + "/sentinel"} {
		sphereStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
			time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), id)
	}

	sessions := newMockSessions() // no alive sessions

	cfg := Config{
		StaleTetherTimeout: 1 * time.Hour,
		SolHome:            solHome,
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.Store, error) {
		return store.OpenWorld(world)
	})

	recovered, err := d.recoverStaleTethers(context.Background())
	if err != nil {
		t.Fatalf("recoverStaleTethers failed: %v", err)
	}
	if recovered != 2 {
		t.Errorf("recovered = %d, want 2 (envoy + governor)", recovered)
	}

	// Verify envoy was recovered.
	envoy, _ := sphereStore.GetAgent(worldName + "/MyEnvoy")
	if envoy.State != "idle" {
		t.Errorf("envoy state = %q, want idle", envoy.State)
	}
	if envoy.TetherItem != "" {
		t.Errorf("envoy tether_item = %q, want empty", envoy.TetherItem)
	}

	// Verify governor was recovered.
	gov, _ := sphereStore.GetAgent(worldName + "/MyGovernor")
	if gov.State != "idle" {
		t.Errorf("governor state = %q, want idle", gov.State)
	}
	if gov.TetherItem != "" {
		t.Errorf("governor tether_item = %q, want empty", gov.TetherItem)
	}

	// Verify work items are back to open.
	worldStore2, _ := store.OpenWorld(worldName)
	defer worldStore2.Close()

	itemEnvoy, _ := worldStore2.GetWorkItem(wiEnvoy)
	if itemEnvoy.Status != "open" {
		t.Errorf("envoy work item status = %q, want open", itemEnvoy.Status)
	}
	itemGov, _ := worldStore2.GetWorkItem(wiGov)
	if itemGov.Status != "open" {
		t.Errorf("governor work item status = %q, want open", itemGov.Status)
	}

	// Verify sentinel was NOT recovered.
	sentinel, _ := sphereStore.GetAgent(worldName + "/sentinel")
	if sentinel.State != "working" {
		t.Errorf("sentinel state = %q, want working (should not be recovered)", sentinel.State)
	}
}

func TestRecoverStaleTethersTooRecent(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "ember2"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	// Agent with dead session but updated_at is 5 minutes ago.
	sphereStore.CreateAgent("RecentAgent", worldName, "agent")
	wiID, _ := worldStore.CreateWorkItem("task-recent", "desc", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/RecentAgent", "working", wiID)
	worldStore.UpdateWorkItem(wiID, store.WorkItemUpdates{Status: "tethered", Assignee: worldName + "/RecentAgent"})
	tether.Write(worldName, "RecentAgent", wiID, "agent")

	// Set updated_at to 5 minutes ago.
	sphereStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-5*time.Minute).UTC().Format(time.RFC3339), worldName+"/RecentAgent")

	sessions := newMockSessions() // no alive sessions

	cfg := Config{
		StaleTetherTimeout: 1 * time.Hour, // 1 hour timeout, 5 min is too recent
		SolHome:             solHome,
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.Store, error) {
		return store.OpenWorld(world)
	})

	recovered, err := d.recoverStaleTethers(context.Background())
	if err != nil {
		t.Fatalf("recoverStaleTethers failed: %v", err)
	}
	if recovered != 0 {
		t.Errorf("recovered = %d, want 0 (too recent)", recovered)
	}

	// Verify agent is still working.
	agent, _ := sphereStore.GetAgent(worldName + "/RecentAgent")
	if agent.State != "working" {
		t.Errorf("agent state = %q, want working", agent.State)
	}
}

func TestRecoverStaleTethersPartialFailure(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "ember3"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	// Agent 1: stale, recoverable.
	sphereStore.CreateAgent("Good", worldName, "agent")
	wi1, _ := worldStore.CreateWorkItem("task-good", "desc", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/Good", "working", wi1)
	worldStore.UpdateWorkItem(wi1, store.WorkItemUpdates{Status: "tethered", Assignee: worldName + "/Good"})
	tether.Write(worldName, "Good", wi1, "agent")

	// Agent 2: stale, but on a world that can't be opened (bad world name).
	sphereStore.CreateAgent("Bad", "nonexistent-world-xyz", "agent")
	sphereStore.UpdateAgentState("nonexistent-world-xyz/Bad", "working", "fake-item")

	// Make both agents old.
	sphereStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), worldName+"/Good")
	sphereStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), "nonexistent-world-xyz/Bad")

	sessions := newMockSessions() // no alive sessions

	cfg := Config{
		StaleTetherTimeout: 1 * time.Hour,
		SolHome:             solHome,
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.Store, error) {
		if world == "nonexistent-world-xyz" {
			// Simulate opening a world store — create it to open, but
			// the work item won't exist, causing a controlled failure.
			s, err := store.OpenWorld(world)
			if err != nil {
				return nil, err
			}
			return s, nil
		}
		return store.OpenWorld(world)
	})

	recovered, err := d.recoverStaleTethers(context.Background())
	if err != nil {
		t.Fatalf("recoverStaleTethers failed: %v", err)
	}

	// Good should be recovered. Bad should be skipped due to work item not found.
	if recovered != 1 {
		t.Errorf("recovered = %d, want 1 (partial failure)", recovered)
	}
}

func TestFeedStrandedCaravans(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "drift"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	// Create a caravan with 3 work items: 2 open (ready), 1 tethered.
	caravanID, _ := sphereStore.CreateCaravan("test-caravan", "operator")

	wi1, _ := worldStore.CreateWorkItem("caravan-task-1", "desc1", "test", 1, nil)
	wi2, _ := worldStore.CreateWorkItem("caravan-task-2", "desc2", "test", 1, nil)
	wi3, _ := worldStore.CreateWorkItem("caravan-task-3", "desc3", "test", 1, nil)

	sphereStore.CreateCaravanItem(caravanID, wi1, worldName, 0)
	sphereStore.CreateCaravanItem(caravanID, wi2, worldName, 0)
	sphereStore.CreateCaravanItem(caravanID, wi3, worldName, 0)

	// Make wi3 tethered (already dispatched).
	worldStore.UpdateWorkItem(wi3, store.WorkItemUpdates{Status: "tethered", Assignee: worldName + "/SomeAgent"})

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 1 * time.Hour,
		SolHome:             solHome,
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.Store, error) {
		return store.OpenWorld(world)
	})

	fed, err := d.feedStrandedCaravans(context.Background())
	if err != nil {
		t.Fatalf("feedStrandedCaravans failed: %v", err)
	}
	if fed != 1 {
		t.Errorf("fed = %d, want 1", fed)
	}

	// Verify CARAVAN_NEEDS_FEEDING message was sent.
	pending, _ := sphereStore.PendingProtocol("operator", store.ProtoCaravanNeedsFeeding)
	if len(pending) != 1 {
		t.Fatalf("pending messages = %d, want 1", len(pending))
	}
	if pending[0].Subject != store.ProtoCaravanNeedsFeeding {
		t.Errorf("message subject = %q, want %q", pending[0].Subject, store.ProtoCaravanNeedsFeeding)
	}
}

func TestFeedStrandedCaravansNoDuplicates(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "drift2"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	caravanID, _ := sphereStore.CreateCaravan("test-caravan-2", "operator")
	wi1, _ := worldStore.CreateWorkItem("dup-task-1", "desc1", "test", 1, nil)
	sphereStore.CreateCaravanItem(caravanID, wi1, worldName, 0)

	// Send a pre-existing CARAVAN_NEEDS_FEEDING message for this caravan.
	sphereStore.SendProtocolMessage("sphere/consul", "operator", store.ProtoCaravanNeedsFeeding,
		store.CaravanNeedsFeedingPayload{CaravanID: caravanID, ReadyCount: 1})

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 1 * time.Hour,
		SolHome:             solHome,
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.Store, error) {
		return store.OpenWorld(world)
	})

	fed, err := d.feedStrandedCaravans(context.Background())
	if err != nil {
		t.Fatalf("feedStrandedCaravans failed: %v", err)
	}
	if fed != 0 {
		t.Errorf("fed = %d, want 0 (already pending)", fed)
	}

	// Verify still only 1 message.
	pending, _ := sphereStore.PendingProtocol("operator", store.ProtoCaravanNeedsFeeding)
	if len(pending) != 1 {
		t.Errorf("pending messages = %d, want 1 (no duplicate)", len(pending))
	}
}

func TestFeedStrandedCaravansAllDispatched(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "drift3"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	caravanID, _ := sphereStore.CreateCaravan("test-caravan-3", "operator")
	wi1, _ := worldStore.CreateWorkItem("all-tethered-1", "desc1", "test", 1, nil)
	worldStore.UpdateWorkItem(wi1, store.WorkItemUpdates{Status: "tethered", Assignee: worldName + "/X"})
	sphereStore.CreateCaravanItem(caravanID, wi1, worldName, 0)

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 1 * time.Hour,
		SolHome:             solHome,
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.Store, error) {
		return store.OpenWorld(world)
	})

	fed, err := d.feedStrandedCaravans(context.Background())
	if err != nil {
		t.Fatalf("feedStrandedCaravans failed: %v", err)
	}
	if fed != 0 {
		t.Errorf("fed = %d, want 0 (all dispatched)", fed)
	}
}

func TestProcessLifecycleShutdown(t *testing.T) {
	solHome := setupSolHome(t)
	_ = solHome

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Send SHUTDOWN message to "sphere/consul".
	sphereStore.SendProtocolMessage("operator", "sphere/consul", "SHUTDOWN", nil)

	sessions := newMockSessions()
	cfg := DefaultConfig()

	d := New(cfg, sphereStore, sessions, nil, nil)

	shutdown, err := d.processLifecycleRequests(context.Background())
	if err != nil {
		t.Fatalf("processLifecycleRequests failed: %v", err)
	}
	if !shutdown {
		t.Error("expected shutdown=true")
	}

	// Verify message was acknowledged.
	pending, _ := sphereStore.PendingProtocol("sphere/consul", "")
	if len(pending) != 0 {
		t.Errorf("pending messages = %d, want 0 (acked)", len(pending))
	}
}

func TestProcessLifecycleCycle(t *testing.T) {
	solHome := setupSolHome(t)
	_ = solHome

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	sphereStore.SendProtocolMessage("operator", "sphere/consul", "CYCLE", nil)

	sessions := newMockSessions()
	cfg := DefaultConfig()

	d := New(cfg, sphereStore, sessions, nil, nil)

	shutdown, err := d.processLifecycleRequests(context.Background())
	if err != nil {
		t.Fatalf("processLifecycleRequests failed: %v", err)
	}
	if shutdown {
		t.Error("expected shutdown=false for CYCLE")
	}

	// Verify message was acknowledged.
	pending, _ := sphereStore.PendingProtocol("sphere/consul", "")
	if len(pending) != 0 {
		t.Errorf("pending messages = %d, want 0 (acked)", len(pending))
	}
}

func TestProcessLifecycleUnknown(t *testing.T) {
	solHome := setupSolHome(t)
	_ = solHome

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	sphereStore.SendProtocolMessage("operator", "sphere/consul", "UNKNOWN_CMD", nil)

	sessions := newMockSessions()
	cfg := DefaultConfig()

	d := New(cfg, sphereStore, sessions, nil, nil)

	shutdown, err := d.processLifecycleRequests(context.Background())
	if err != nil {
		t.Fatalf("processLifecycleRequests failed: %v", err)
	}
	if shutdown {
		t.Error("expected shutdown=false for unknown message")
	}

	// Verify message was acknowledged (even though unknown).
	pending, _ := sphereStore.PendingProtocol("sphere/consul", "")
	if len(pending) != 0 {
		t.Errorf("pending messages = %d, want 0 (acked)", len(pending))
	}
}

func TestPatrolCycle(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "vigil"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	// 1. Stale tethered agent (dead session, old timestamp).
	sphereStore.CreateAgent("Stale", worldName, "agent")
	wiStale, _ := worldStore.CreateWorkItem("stale-task", "desc", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/Stale", "working", wiStale)
	worldStore.UpdateWorkItem(wiStale, store.WorkItemUpdates{Status: "tethered", Assignee: worldName + "/Stale"})
	tether.Write(worldName, "Stale", wiStale, "agent")
	sphereStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), worldName+"/Stale")

	// 2. Open caravan with ready items.
	caravanID, _ := sphereStore.CreateCaravan("patrol-caravan", "operator")
	wiCaravan, _ := worldStore.CreateWorkItem("caravan-ready", "desc", "test", 1, nil)
	sphereStore.CreateCaravanItem(caravanID, wiCaravan, worldName, 0)

	// 3. Healthy working agent (session alive).
	sphereStore.CreateAgent("Healthy", worldName, "agent")
	wiHealthy, _ := worldStore.CreateWorkItem("healthy-task", "desc", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/Healthy", "working", wiHealthy)
	worldStore.UpdateWorkItem(wiHealthy, store.WorkItemUpdates{Status: "tethered", Assignee: worldName + "/Healthy"})
	tether.Write(worldName, "Healthy", wiHealthy, "agent")

	sessions := newMockSessions()
	sessions.alive["sol-"+worldName+"-Healthy"] = true

	cfg := Config{
		StaleTetherTimeout: 1 * time.Hour,
		SolHome:             solHome,
		PatrolInterval:     5 * time.Minute,
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.Store, error) {
		return store.OpenWorld(world)
	})

	err = d.Patrol(context.Background())
	if err != nil {
		t.Fatalf("Patrol failed: %v", err)
	}

	// Verify: stale tether recovered.
	agentStale, _ := sphereStore.GetAgent(worldName + "/Stale")
	if agentStale.State != "idle" {
		t.Errorf("Stale agent state = %q, want idle", agentStale.State)
	}

	// Verify: caravan feed message sent.
	pending, _ := sphereStore.PendingProtocol("operator", store.ProtoCaravanNeedsFeeding)
	if len(pending) == 0 {
		t.Error("expected CARAVAN_NEEDS_FEEDING message")
	}

	// Verify: heartbeat written.
	hb, err := ReadHeartbeat(solHome)
	if err != nil {
		t.Fatalf("ReadHeartbeat failed: %v", err)
	}
	if hb == nil {
		t.Fatal("expected heartbeat to be written")
	}
	if hb.PatrolCount != 1 {
		t.Errorf("heartbeat patrol_count = %d, want 1", hb.PatrolCount)
	}
	if hb.StaleTethers != 1 {
		t.Errorf("heartbeat stale_tethers = %d, want 1", hb.StaleTethers)
	}
	if hb.CaravanFeeds != 1 {
		t.Errorf("heartbeat caravan_feeds = %d, want 1", hb.CaravanFeeds)
	}

	// Verify: healthy agent untouched.
	agentHealthy, _ := sphereStore.GetAgent(worldName + "/Healthy")
	if agentHealthy.State != "working" {
		t.Errorf("Healthy agent state = %q, want working", agentHealthy.State)
	}

	// Verify: tether file still present for healthy agent.
	if !tether.IsTethered(worldName, "Healthy", "agent") {
		t.Error("Healthy agent tether file should still exist")
	}

	// Clean up.
	os.RemoveAll(solHome)
}
