package consul

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// mockSessionManager tracks which sessions are "alive" and records starts/stops.
type mockSessionManager struct {
	alive     map[string]bool
	createdAt map[string]time.Time // per-session tmux creation time; defaults to time.Now() if absent
	started   []string             // session names that were started
	stopped   []string             // session names that were stopped
	listErr   error                // if set, List() returns this error
}

func newMockSessions() *mockSessionManager {
	return &mockSessionManager{
		alive:     make(map[string]bool),
		createdAt: make(map[string]time.Time),
	}
}

func (m *mockSessionManager) Exists(name string) bool {
	return m.alive[name]
}

func (m *mockSessionManager) List() ([]session.SessionInfo, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var infos []session.SessionInfo
	for name := range m.alive {
		created, ok := m.createdAt[name]
		if !ok {
			created = time.Now()
		}
		infos = append(infos, session.SessionInfo{
			Name:      name,
			StartedAt: created,
			CreatedAt: created,
			Alive:     true,
		})
	}
	return infos, nil
}

func (m *mockSessionManager) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	m.started = append(m.started, name)
	m.alive[name] = true
	return nil
}

func (m *mockSessionManager) Stop(name string, force bool) error {
	m.stopped = append(m.stopped, name)
	delete(m.alive, name)
	return nil
}

func (m *mockSessionManager) Inject(name string, text string, submit bool) error {
	return nil
}

func (m *mockSessionManager) NudgeSession(name string, message string) error {
	return nil
}

func (m *mockSessionManager) WaitForIdle(name string, timeout time.Duration) error {
	return nil
}

func (m *mockSessionManager) Capture(name string, lines int) (string, error) {
	return "", nil
}

func (m *mockSessionManager) Cycle(name, workdir, cmd string, env map[string]string, role, world string) error {
	return fmt.Errorf("cycle not supported in mock")
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

// mockDispatchResult holds a recorded dispatch call.
type mockDispatchResult struct {
	WritID string
	World      string
}

// newMockDispatchFunc returns a DispatchFunc that records calls and returns success.
func newMockDispatchFunc(results *[]mockDispatchResult) DispatchFunc {
	agentCounter := 0
	return func(ctx context.Context, opts dispatch.CastOpts, worldStore dispatch.WorldStore, sphereStore dispatch.SphereStore, mgr dispatch.SessionManager, logger *events.Logger) (*dispatch.CastResult, error) {
		agentCounter++
		*results = append(*results, mockDispatchResult{
			WritID: opts.WritID,
			World:      opts.World,
		})
		return &dispatch.CastResult{
			WritID:  opts.WritID,
			AgentName:   "MockAgent",
			SessionName: "sol-mock-session",
			WorktreeDir: "/tmp/mock-worktree",
		}, nil
	}
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
	sphereStore.CreateAgent("AgentA", worldName, "outpost")
	wiA, _ := worldStore.CreateWrit("task-a", "description a", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/AgentA", "working", wiA)
	worldStore.UpdateWrit(wiA, store.WritUpdates{Status: "tethered", Assignee: worldName + "/AgentA"})
	tether.Write(worldName, "AgentA", wiA, "outpost")

	// Make Agent A's updated_at old (> 15 minutes ago).
	sphereStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), worldName+"/AgentA")

	// Agent B: working, session alive → should NOT be recovered.
	sphereStore.CreateAgent("AgentB", worldName, "outpost")
	wiB, _ := worldStore.CreateWrit("task-b", "description b", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/AgentB", "working", wiB)
	worldStore.UpdateWrit(wiB, store.WritUpdates{Status: "tethered", Assignee: worldName + "/AgentB"})
	tether.Write(worldName, "AgentB", wiB, "outpost")

	// Agent C: idle → should NOT be recovered.
	sphereStore.CreateAgent("AgentC", worldName, "outpost")

	// Set up mock sessions: only AgentB is alive.
	sessions := newMockSessions()
	sessions.alive["sol-"+worldName+"-AgentB"] = true

	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:             solHome,
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
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
	if agentA.ActiveWrit != "" {
		t.Errorf("AgentA active_writ = %q, want empty", agentA.ActiveWrit)
	}

	// Verify writ A is back to open.
	worldStore2, _ := store.OpenWorld(worldName)
	defer worldStore2.Close()
	itemA, _ := worldStore2.GetWrit(wiA)
	if itemA.Status != "open" {
		t.Errorf("writ A status = %q, want open", itemA.Status)
	}
	if itemA.Assignee != "" {
		t.Errorf("writ A assignee = %q, want empty", itemA.Assignee)
	}

	// Verify tether file was cleared.
	if tether.IsTethered(worldName, "AgentA", "outpost") {
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
	wiEnvoy, _ := worldStore.CreateWrit("task-envoy", "envoy work", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/MyEnvoy", "working", wiEnvoy)
	worldStore.UpdateWrit(wiEnvoy, store.WritUpdates{Status: "tethered", Assignee: worldName + "/MyEnvoy"})
	tether.Write(worldName, "MyEnvoy", wiEnvoy, "envoy")

	// Governor: working, session dead, old timestamp → should be recovered.
	sphereStore.CreateAgent("MyGovernor", worldName, "governor")
	wiGov, _ := worldStore.CreateWrit("task-governor", "governor work", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/MyGovernor", "working", wiGov)
	worldStore.UpdateWrit(wiGov, store.WritUpdates{Status: "tethered", Assignee: worldName + "/MyGovernor"})
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
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            solHome,
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
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
	if envoy.ActiveWrit != "" {
		t.Errorf("envoy active_writ = %q, want empty", envoy.ActiveWrit)
	}

	// Verify governor was recovered.
	gov, _ := sphereStore.GetAgent(worldName + "/MyGovernor")
	if gov.State != "idle" {
		t.Errorf("governor state = %q, want idle", gov.State)
	}
	if gov.ActiveWrit != "" {
		t.Errorf("governor active_writ = %q, want empty", gov.ActiveWrit)
	}

	// Verify writs are back to open.
	worldStore2, _ := store.OpenWorld(worldName)
	defer worldStore2.Close()

	itemEnvoy, _ := worldStore2.GetWrit(wiEnvoy)
	if itemEnvoy.Status != "open" {
		t.Errorf("envoy writ status = %q, want open", itemEnvoy.Status)
	}
	itemGov, _ := worldStore2.GetWrit(wiGov)
	if itemGov.Status != "open" {
		t.Errorf("governor writ status = %q, want open", itemGov.Status)
	}

	// Verify sentinel was NOT recovered.
	sentinel, _ := sphereStore.GetAgent(worldName + "/sentinel")
	if sentinel.State != "working" {
		t.Errorf("sentinel state = %q, want working (should not be recovered)", sentinel.State)
	}
}

// TestRecoverStaleTethersStalled verifies that consul recovers agents that were
// permanently stalled by prefect (MaxRespawns exceeded, state = "stalled"), and
// that stalled agents updated recently (within StaleTetherTimeout) are skipped.
func TestRecoverStaleTethersStalled(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "ember-stalled"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	// Agent A: permanently stalled (MaxRespawns exceeded), stale timestamp → should be recovered.
	sphereStore.CreateAgent("StalledOld", worldName, "outpost")
	wiOld, _ := worldStore.CreateWrit("task-stalled-old", "old stalled task", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/StalledOld", "stalled", wiOld)
	worldStore.UpdateWrit(wiOld, store.WritUpdates{Status: "tethered", Assignee: worldName + "/StalledOld"})
	tether.Write(worldName, "StalledOld", wiOld, "outpost")

	// Make StalledOld's updated_at 2 hours ago (well beyond StaleTetherTimeout).
	sphereStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), worldName+"/StalledOld")

	// Agent B: stalled recently (active backoff) → should NOT be recovered.
	sphereStore.CreateAgent("StalledRecent", worldName, "outpost")
	wiRecent, _ := worldStore.CreateWrit("task-stalled-recent", "recent stalled task", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/StalledRecent", "stalled", wiRecent)
	worldStore.UpdateWrit(wiRecent, store.WritUpdates{Status: "tethered", Assignee: worldName + "/StalledRecent"})
	tether.Write(worldName, "StalledRecent", wiRecent, "outpost")
	// updated_at defaults to now — within StaleTetherTimeout.

	sessions := newMockSessions() // no alive sessions

	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            solHome,
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})

	recovered, err := d.recoverStaleTethers(context.Background())
	if err != nil {
		t.Fatalf("recoverStaleTethers failed: %v", err)
	}
	if recovered != 1 {
		t.Errorf("recovered = %d, want 1 (only old stalled agent)", recovered)
	}

	// Verify StalledOld was recovered.
	agentOld, _ := sphereStore.GetAgent(worldName + "/StalledOld")
	if agentOld.State != "idle" {
		t.Errorf("StalledOld state = %q, want idle", agentOld.State)
	}
	if agentOld.ActiveWrit != "" {
		t.Errorf("StalledOld active_writ = %q, want empty", agentOld.ActiveWrit)
	}

	// Verify writ A is back to open.
	itemOld, _ := worldStore.GetWrit(wiOld)
	if itemOld.Status != "open" {
		t.Errorf("wiOld status = %q, want open", itemOld.Status)
	}

	// Verify tether cleared.
	if tether.IsTethered(worldName, "StalledOld", "outpost") {
		t.Error("StalledOld tether file should have been cleared")
	}

	// Verify StalledRecent was NOT recovered (still in active backoff window).
	agentRecent, _ := sphereStore.GetAgent(worldName + "/StalledRecent")
	if agentRecent.State != "stalled" {
		t.Errorf("StalledRecent state = %q, want stalled (too recent)", agentRecent.State)
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
	sphereStore.CreateAgent("RecentAgent", worldName, "outpost")
	wiID, _ := worldStore.CreateWrit("task-recent", "desc", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/RecentAgent", "working", wiID)
	worldStore.UpdateWrit(wiID, store.WritUpdates{Status: "tethered", Assignee: worldName + "/RecentAgent"})
	tether.Write(worldName, "RecentAgent", wiID, "outpost")

	// Set updated_at to 5 minutes ago.
	sphereStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-5*time.Minute).UTC().Format(time.RFC3339), worldName+"/RecentAgent")

	sessions := newMockSessions() // no alive sessions

	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute, // 15 min timeout, 5 min is too recent
		SolHome:             solHome,
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
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
	sphereStore.CreateAgent("Good", worldName, "outpost")
	wi1, _ := worldStore.CreateWrit("task-good", "desc", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/Good", "working", wi1)
	worldStore.UpdateWrit(wi1, store.WritUpdates{Status: "tethered", Assignee: worldName + "/Good"})
	tether.Write(worldName, "Good", wi1, "outpost")

	// Agent 2: stale, but on a world that can't be opened (bad world name).
	sphereStore.CreateAgent("Bad", "nonexistent-world-xyz", "outpost")
	sphereStore.UpdateAgentState("nonexistent-world-xyz/Bad", "working", "fake-item")

	// Make both agents old.
	sphereStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), worldName+"/Good")
	sphereStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), "nonexistent-world-xyz/Bad")

	sessions := newMockSessions() // no alive sessions

	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:             solHome,
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		if world == "nonexistent-world-xyz" {
			// Simulate opening a world store — create it to open, but
			// the writ won't exist, causing a controlled failure.
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

	// Good should be recovered. Bad should be skipped due to writ not found.
	if recovered != 1 {
		t.Errorf("recovered = %d, want 1 (partial failure)", recovered)
	}
}

func TestFeedStrandedCaravansAutoDispatch(t *testing.T) {
	setupSolHome(t)

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

	// Create a caravan with 3 writs: 2 open (ready), 1 tethered.
	caravanID, _ := sphereStore.CreateCaravan("test-caravan", "autarch")
	sphereStore.UpdateCaravanStatus(caravanID, "open")

	wi1, _ := worldStore.CreateWrit("caravan-task-1", "desc1", "test", 1, nil)
	wi2, _ := worldStore.CreateWrit("caravan-task-2", "desc2", "test", 1, nil)
	wi3, _ := worldStore.CreateWrit("caravan-task-3", "desc3", "test", 1, nil)

	sphereStore.CreateCaravanItem(caravanID, wi1, worldName, 0)
	sphereStore.CreateCaravanItem(caravanID, wi2, worldName, 0)
	sphereStore.CreateCaravanItem(caravanID, wi3, worldName, 0)

	// Make wi3 tethered (already dispatched).
	worldStore.UpdateWrit(wi3, store.WritUpdates{Status: "tethered", Assignee: worldName + "/SomeAgent"})

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            config.Home(),
	}

	var dispatched []mockDispatchResult
	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})
	d.SetDispatchFunc(newMockDispatchFunc(&dispatched))

	fed, err := d.feedStrandedCaravans(context.Background())
	if err != nil {
		t.Fatalf("feedStrandedCaravans failed: %v", err)
	}
	if fed != 2 {
		t.Errorf("fed = %d, want 2 (2 ready items dispatched)", fed)
	}

	// Verify dispatch was called for the 2 ready items.
	if len(dispatched) != 2 {
		t.Fatalf("dispatched = %d, want 2", len(dispatched))
	}
	dispatchedIDs := map[string]bool{}
	for _, d := range dispatched {
		dispatchedIDs[d.WritID] = true
		if d.World != worldName {
			t.Errorf("dispatched world = %q, want %q", d.World, worldName)
		}
	}
	if !dispatchedIDs[wi1] {
		t.Errorf("expected writ %s to be dispatched", wi1)
	}
	if !dispatchedIDs[wi2] {
		t.Errorf("expected writ %s to be dispatched", wi2)
	}

	// Verify NO CARAVAN_NEEDS_FEEDING message was sent (auto-dispatch replaces it).
	pending, _ := sphereStore.PendingProtocol("autarch", store.ProtoCaravanNeedsFeeding)
	if len(pending) != 0 {
		t.Errorf("pending CARAVAN_NEEDS_FEEDING = %d, want 0 (auto-dispatch replaces notification)", len(pending))
	}
}

func TestFeedStrandedCaravansAllDispatched(t *testing.T) {
	setupSolHome(t)

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

	caravanID, _ := sphereStore.CreateCaravan("test-caravan-3", "autarch")
	sphereStore.UpdateCaravanStatus(caravanID, "open")
	wi1, _ := worldStore.CreateWrit("all-tethered-1", "desc1", "test", 1, nil)
	worldStore.UpdateWrit(wi1, store.WritUpdates{Status: "tethered", Assignee: worldName + "/X"})
	sphereStore.CreateCaravanItem(caravanID, wi1, worldName, 0)

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            config.Home(),
	}

	var dispatched []mockDispatchResult
	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})
	d.SetDispatchFunc(newMockDispatchFunc(&dispatched))

	fed, err := d.feedStrandedCaravans(context.Background())
	if err != nil {
		t.Fatalf("feedStrandedCaravans failed: %v", err)
	}
	if fed != 0 {
		t.Errorf("fed = %d, want 0 (all dispatched)", fed)
	}
	if len(dispatched) != 0 {
		t.Errorf("dispatch calls = %d, want 0", len(dispatched))
	}
}

func TestFeedStrandedCaravansAutoCloseAllMerged(t *testing.T) {
	setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "drift-autoclose"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	// Create an open caravan with all items closed (merged).
	// This simulates the production bug: all items merged but caravan never closed
	// because TryCloseCaravan only ran when new work was dispatched.
	caravanID, _ := sphereStore.CreateCaravan("all-merged-caravan", "autarch")
	sphereStore.UpdateCaravanStatus(caravanID, "open")

	wi1, _ := worldStore.CreateWrit("merged-1", "desc1", "test", 1, nil)
	wi2, _ := worldStore.CreateWrit("merged-2", "desc2", "test", 1, nil)
	worldStore.UpdateWrit(wi1, store.WritUpdates{Status: "closed"})
	worldStore.UpdateWrit(wi2, store.WritUpdates{Status: "closed"})
	sphereStore.CreateCaravanItem(caravanID, wi1, worldName, 0)
	sphereStore.CreateCaravanItem(caravanID, wi2, worldName, 0)

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            config.Home(),
	}

	var dispatched []mockDispatchResult
	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})
	d.SetDispatchFunc(newMockDispatchFunc(&dispatched))

	fed, err := d.feedStrandedCaravans(context.Background())
	if err != nil {
		t.Fatalf("feedStrandedCaravans failed: %v", err)
	}
	if fed != 0 {
		t.Errorf("fed = %d, want 0 (no items to dispatch)", fed)
	}
	if len(dispatched) != 0 {
		t.Errorf("dispatch calls = %d, want 0", len(dispatched))
	}

	// Key assertion: caravan should be auto-closed even though nothing was dispatched.
	caravan, err := sphereStore.GetCaravan(caravanID)
	if err != nil {
		t.Fatalf("GetCaravan failed: %v", err)
	}
	if caravan.Status != "closed" {
		t.Errorf("caravan status = %q, want closed (auto-close should run unconditionally)", caravan.Status)
	}
}

func TestFeedStrandedCaravansSkipsRedispatch(t *testing.T) {
	setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "drift-redispatch"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	// Create a caravan with 2 open writs:
	// - wi1: has existing MRs (was dispatched before, MR failed, writ reopened)
	// - wi2: no MRs (fresh, never dispatched)
	caravanID, _ := sphereStore.CreateCaravan("redispatch-caravan", "autarch")
	sphereStore.UpdateCaravanStatus(caravanID, "open")

	wi1, _ := worldStore.CreateWrit("redispatch-task-1", "desc1", "test", 1, nil)
	wi2, _ := worldStore.CreateWrit("redispatch-task-2", "desc2", "test", 1, nil)

	sphereStore.CreateCaravanItem(caravanID, wi1, worldName, 0)
	sphereStore.CreateCaravanItem(caravanID, wi2, worldName, 0)

	// wi1 has a failed MR — simulates forge marking MR as failed and reopening the writ.
	mrID, _ := worldStore.CreateMergeRequest(wi1, "outpost/agent/worktree", 1)
	worldStore.ClaimMergeRequest("forge")
	worldStore.UpdateMergeRequestPhase(mrID, "failed")

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            config.Home(),
	}

	var dispatched []mockDispatchResult
	logger := events.NewLogger(config.Home())
	d := New(cfg, sphereStore, sessions, nil, logger)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})
	d.SetDispatchFunc(newMockDispatchFunc(&dispatched))

	fed, err := d.feedStrandedCaravans(context.Background())
	if err != nil {
		t.Fatalf("feedStrandedCaravans failed: %v", err)
	}

	// Only wi2 should be dispatched; wi1 should be skipped (existing MRs).
	if fed != 1 {
		t.Errorf("fed = %d, want 1 (only fresh item dispatched)", fed)
	}
	if len(dispatched) != 1 {
		t.Fatalf("dispatch calls = %d, want 1", len(dispatched))
	}
	if dispatched[0].WritID != wi2 {
		t.Errorf("dispatched writ = %s, want %s (fresh item)", dispatched[0].WritID, wi2)
	}
}

func TestFeedStrandedCaravansDrydockIgnored(t *testing.T) {
	setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "drift-drydock"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	// Create a drydock caravan — should NOT be dispatched.
	caravanID, _ := sphereStore.CreateCaravan("drydock-caravan", "autarch")
	// Status remains "drydock" (default from CreateCaravan).

	wi1, _ := worldStore.CreateWrit("drydock-task-1", "desc1", "test", 1, nil)
	sphereStore.CreateCaravanItem(caravanID, wi1, worldName, 0)

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            config.Home(),
	}

	var dispatched []mockDispatchResult
	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})
	d.SetDispatchFunc(newMockDispatchFunc(&dispatched))

	fed, err := d.feedStrandedCaravans(context.Background())
	if err != nil {
		t.Fatalf("feedStrandedCaravans failed: %v", err)
	}
	if fed != 0 {
		t.Errorf("fed = %d, want 0 (drydock caravan ignored)", fed)
	}
	if len(dispatched) != 0 {
		t.Errorf("dispatch calls = %d, want 0", len(dispatched))
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
	sphereStore.SendProtocolMessage("autarch", "sphere/consul", "SHUTDOWN", nil)

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

	sphereStore.SendProtocolMessage("autarch", "sphere/consul", "CYCLE", nil)

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

	sphereStore.SendProtocolMessage("autarch", "sphere/consul", "UNKNOWN_CMD", nil)

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
	sphereStore.CreateAgent("Stale", worldName, "outpost")
	wiStale, _ := worldStore.CreateWrit("stale-task", "desc", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/Stale", "working", wiStale)
	worldStore.UpdateWrit(wiStale, store.WritUpdates{Status: "tethered", Assignee: worldName + "/Stale"})
	tether.Write(worldName, "Stale", wiStale, "outpost")
	sphereStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), worldName+"/Stale")

	// 2. Open caravan with ready items.
	caravanID, _ := sphereStore.CreateCaravan("patrol-caravan", "autarch")
	sphereStore.UpdateCaravanStatus(caravanID, "open")
	wiCaravan, _ := worldStore.CreateWrit("caravan-ready", "desc", "test", 1, nil)
	sphereStore.CreateCaravanItem(caravanID, wiCaravan, worldName, 0)

	// 3. Healthy working agent (session alive).
	sphereStore.CreateAgent("Healthy", worldName, "outpost")
	wiHealthy, _ := worldStore.CreateWrit("healthy-task", "desc", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/Healthy", "working", wiHealthy)
	worldStore.UpdateWrit(wiHealthy, store.WritUpdates{Status: "tethered", Assignee: worldName + "/Healthy"})
	tether.Write(worldName, "Healthy", wiHealthy, "outpost")

	sessions := newMockSessions()
	sessions.alive["sol-"+worldName+"-Healthy"] = true

	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:             solHome,
		PatrolInterval:     5 * time.Minute,
	}

	var dispatched []mockDispatchResult
	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})
	d.SetDispatchFunc(newMockDispatchFunc(&dispatched))

	err = d.Patrol(context.Background())
	if err != nil {
		t.Fatalf("Patrol failed: %v", err)
	}

	// Verify: stale tether recovered.
	agentStale, _ := sphereStore.GetAgent(worldName + "/Stale")
	if agentStale.State != "idle" {
		t.Errorf("Stale agent state = %q, want idle", agentStale.State)
	}

	// Verify: caravan item was auto-dispatched (not just a message).
	if len(dispatched) != 1 {
		t.Errorf("dispatched = %d, want 1", len(dispatched))
	} else if dispatched[0].WritID != wiCaravan {
		t.Errorf("dispatched writ = %q, want %q", dispatched[0].WritID, wiCaravan)
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
	if !tether.IsTethered(worldName, "Healthy", "outpost") {
		t.Error("Healthy agent tether file should still exist")
	}

	// Clean up.
	os.RemoveAll(solHome)
}

func TestPatrolExitsEarlyOnCancelledContext(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "cancel-patrol"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	// Create a stale tethered agent — should NOT be recovered if context is cancelled.
	sphereStore.CreateAgent("ShouldSkip", worldName, "outpost")
	wiStale, _ := worldStore.CreateWrit("stale-cancel", "desc", "test", 1, nil)
	sphereStore.UpdateAgentState(worldName+"/ShouldSkip", "working", wiStale)
	worldStore.UpdateWrit(wiStale, store.WritUpdates{Status: "tethered", Assignee: worldName + "/ShouldSkip"})
	tether.Write(worldName, "ShouldSkip", wiStale, "outpost")
	sphereStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), worldName+"/ShouldSkip")

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            solHome,
		PatrolInterval:     5 * time.Minute,
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})

	// Cancel context before calling Patrol.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = d.Patrol(ctx)
	if err == nil {
		t.Fatal("expected Patrol to return error with cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}

	// Verify patrol count was incremented (patrol started).
	if d.patrolCount != 1 {
		t.Errorf("patrolCount = %d, want 1", d.patrolCount)
	}
}

func TestRecoverStaleTethersExitsOnCancelledContext(t *testing.T) {
	solHome := setupSolHome(t)
	_ = solHome

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "cancel-recover"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	// Create multiple stale agents — only some should be processed.
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("Agent%d", i)
		sphereStore.CreateAgent(name, worldName, "outpost")
		wi, _ := worldStore.CreateWrit(fmt.Sprintf("task-%d", i), "desc", "test", 1, nil)
		sphereStore.UpdateAgentState(worldName+"/"+name, "working", wi)
		worldStore.UpdateWrit(wi, store.WritUpdates{Status: "tethered", Assignee: worldName + "/" + name})
		tether.Write(worldName, name, wi, "outpost")
		sphereStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
			time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), worldName+"/"+name)
	}

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            config.Home(),
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})

	// Cancel context immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	recovered, err := d.recoverStaleTethers(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	// Should have recovered 0 (cancelled before processing).
	if recovered != 0 {
		t.Errorf("recovered = %d, want 0 (cancelled before processing)", recovered)
	}
}

func TestFeedStrandedCaravansExitsOnCancelledContext(t *testing.T) {
	setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "cancel-feed"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	// Create an open caravan with ready items.
	caravanID, _ := sphereStore.CreateCaravan("cancel-caravan", "autarch")
	sphereStore.UpdateCaravanStatus(caravanID, "open")
	wi1, _ := worldStore.CreateWrit("cancel-task-1", "desc1", "test", 1, nil)
	sphereStore.CreateCaravanItem(caravanID, wi1, worldName, 0)

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            config.Home(),
	}

	var dispatched []mockDispatchResult
	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})
	d.SetDispatchFunc(newMockDispatchFunc(&dispatched))

	// Cancel context before calling.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fed, err := d.feedStrandedCaravans(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	if fed != 0 {
		t.Errorf("fed = %d, want 0 (cancelled before processing)", fed)
	}
	if len(dispatched) != 0 {
		t.Errorf("dispatch calls = %d, want 0", len(dispatched))
	}
}

func TestDispatchWorldItemsBreaksOnCapacityExhausted(t *testing.T) {
	setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "full-world"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	// Write world config so dispatchWorldItems can load it.
	worldDir := filepath.Join(config.Home(), worldName)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configContent := `[world]
source_repo = "/tmp/fake-repo"
[agents]
capacity = 2
`
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set up managed repo directory so ResolveSourceRepo doesn't fail.
	repoDir := filepath.Join(config.Home(), worldName, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create caravan with 3 ready items.
	caravanID, _ := sphereStore.CreateCaravan("cap-test-caravan", "autarch")
	sphereStore.UpdateCaravanStatus(caravanID, "open")

	wi1, _ := worldStore.CreateWrit("cap-task-1", "desc1", "test", 1, nil)
	wi2, _ := worldStore.CreateWrit("cap-task-2", "desc2", "test", 1, nil)
	wi3, _ := worldStore.CreateWrit("cap-task-3", "desc3", "test", 1, nil)

	sphereStore.CreateCaravanItem(caravanID, wi1, worldName, 0)
	sphereStore.CreateCaravanItem(caravanID, wi2, worldName, 0)
	sphereStore.CreateCaravanItem(caravanID, wi3, worldName, 0)

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            config.Home(),
	}

	// Dispatch func: succeeds on first call, returns capacity exhausted on second.
	dispatchCount := 0
	capDispatchFunc := func(ctx context.Context, opts dispatch.CastOpts, ws dispatch.WorldStore, ss dispatch.SphereStore, mgr dispatch.SessionManager, logger *events.Logger) (*dispatch.CastResult, error) {
		dispatchCount++
		if dispatchCount >= 2 {
			return nil, fmt.Errorf("world %q at capacity: %w", worldName, dispatch.ErrCapacityExhausted)
		}
		return &dispatch.CastResult{
			WritID:      opts.WritID,
			AgentName:   "MockAgent",
			SessionName: "sol-mock-session",
			WorktreeDir: "/tmp/mock",
		}, nil
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})
	d.SetDispatchFunc(capDispatchFunc)

	fed, err := d.feedStrandedCaravans(context.Background())
	if err != nil {
		t.Fatalf("feedStrandedCaravans failed: %v", err)
	}

	// Should have dispatched 1 item, then hit capacity on the 2nd and broken.
	// The 3rd item should NOT have been tried.
	if fed != 1 {
		t.Errorf("fed = %d, want 1 (broke after capacity exhaustion)", fed)
	}
	if dispatchCount != 2 {
		t.Errorf("dispatchCount = %d, want 2 (1 success + 1 capacity error, then break)", dispatchCount)
	}
}

func TestDispatchWorldItemsSkipsSleepingWorld(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "sleepy"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	// Mark the world as sleeping.
	worldDir := fmt.Sprintf("%s/%s", solHome, worldName)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatalf("failed to create world dir: %v", err)
	}
	if err := os.WriteFile(worldDir+"/world.toml", []byte("[world]\nsleeping = true\n"), 0o644); err != nil {
		t.Fatalf("failed to write world.toml: %v", err)
	}

	// Create a caravan with ready items.
	caravanID, _ := sphereStore.CreateCaravan("test-sleep-caravan", "autarch")
	sphereStore.UpdateCaravanStatus(caravanID, "open")

	wi1, _ := worldStore.CreateWrit("sleep-task-1", "desc1", "test", 1, nil)
	sphereStore.CreateCaravanItem(caravanID, wi1, worldName, 0)

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            solHome,
	}

	var dispatched []mockDispatchResult
	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})
	d.SetDispatchFunc(newMockDispatchFunc(&dispatched))

	fed, err := d.feedStrandedCaravans(context.Background())
	if err != nil {
		t.Fatalf("feedStrandedCaravans failed: %v", err)
	}
	if fed != 0 {
		t.Errorf("fed = %d, want 0 (sleeping world should be skipped)", fed)
	}
	if len(dispatched) != 0 {
		t.Errorf("dispatch calls = %d, want 0 (sleeping world should be skipped)", len(dispatched))
	}
}

func TestDetectOrphanedSessionsIdentifiesOrphan(t *testing.T) {
	setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "orphan-test"
	sphereStore.RegisterWorld(worldName, "/tmp/repo")

	// Create a known agent.
	sphereStore.CreateAgent("KnownAgent", worldName, "outpost")

	sessions := newMockSessions()
	// Known session: has an agent record.
	sessions.alive["sol-"+worldName+"-KnownAgent"] = true
	// Orphaned session: no agent record.
	sessions.alive["sol-"+worldName+"-GhostAgent"] = true
	// Infrastructure session: always known.
	sessions.alive["sol-chronicle"] = true

	cfg := Config{SolHome: config.Home()}
	d := New(cfg, sphereStore, sessions, nil, nil)

	// First patrol: detect the orphan, start tracking.
	stopped, err := d.detectOrphanedSessions(context.Background())
	if err != nil {
		t.Fatalf("detectOrphanedSessions failed: %v", err)
	}
	if stopped != 0 {
		t.Errorf("stopped = %d, want 0 (first detection, grace period)", stopped)
	}

	// Verify it's being tracked.
	if _, ok := d.orphanedSessions["sol-"+worldName+"-GhostAgent"]; !ok {
		t.Error("expected orphan to be tracked after first detection")
	}

	// Known sessions should NOT be tracked.
	if _, ok := d.orphanedSessions["sol-"+worldName+"-KnownAgent"]; ok {
		t.Error("known agent session should not be tracked as orphan")
	}
	if _, ok := d.orphanedSessions["sol-chronicle"]; ok {
		t.Error("infrastructure session should not be tracked as orphan")
	}
}

func TestDetectOrphanedSessionsGracePeriod(t *testing.T) {
	setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "orphan-grace"
	sphereStore.RegisterWorld(worldName, "/tmp/repo")

	sessions := newMockSessions()
	sessions.alive["sol-"+worldName+"-GhostAgent"] = true

	cfg := Config{SolHome: config.Home()}
	d := New(cfg, sphereStore, sessions, nil, nil)

	// First patrol: detect orphan.
	stopped, _ := d.detectOrphanedSessions(context.Background())
	if stopped != 0 {
		t.Errorf("patrol 1: stopped = %d, want 0", stopped)
	}

	// Second patrol: still within grace period (first-seen is moments ago).
	stopped, _ = d.detectOrphanedSessions(context.Background())
	if stopped != 0 {
		t.Errorf("patrol 2: stopped = %d, want 0 (grace period)", stopped)
	}

	// Verify session is still alive.
	if !sessions.alive["sol-"+worldName+"-GhostAgent"] {
		t.Error("session should not have been killed during grace period")
	}

	// Verify no stops were recorded.
	if len(sessions.stopped) != 0 {
		t.Errorf("stopped sessions = %v, want none", sessions.stopped)
	}
}

func TestDetectOrphanedSessionsKilledAfterThreshold(t *testing.T) {
	setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "orphan-kill"
	sphereStore.RegisterWorld(worldName, "/tmp/repo")

	sessions := newMockSessions()
	sessions.alive["sol-"+worldName+"-GhostAgent"] = true

	cfg := Config{SolHome: config.Home()}
	d := New(cfg, sphereStore, sessions, nil, nil)

	// First patrol: detect orphan, start tracking (count=1).
	d.detectOrphanedSessions(context.Background())

	// Backdate firstSeen to simulate 31 minutes having passed since first detection.
	d.orphanedSessions["sol-"+worldName+"-GhostAgent"].firstSeen = time.Now().Add(-31 * time.Minute)

	// Second patrol: grace period expired, count=2 → should be stopped.
	stopped, err := d.detectOrphanedSessions(context.Background())
	if err != nil {
		t.Fatalf("detectOrphanedSessions failed: %v", err)
	}
	if stopped != 1 {
		t.Errorf("stopped = %d, want 1", stopped)
	}

	// Verify the session was stopped.
	if sessions.alive["sol-"+worldName+"-GhostAgent"] {
		t.Error("orphaned session should have been stopped")
	}
	found := false
	for _, name := range sessions.stopped {
		if name == "sol-"+worldName+"-GhostAgent" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected GhostAgent in stopped list, got %v", sessions.stopped)
	}

	// Verify tracking entry was removed.
	if _, ok := d.orphanedSessions["sol-"+worldName+"-GhostAgent"]; ok {
		t.Error("stopped orphan should be removed from tracking map")
	}
}

func TestDetectOrphanedSessionsKnownNotFlagged(t *testing.T) {
	setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "orphan-known"
	sphereStore.RegisterWorld(worldName, "/tmp/repo")

	// Create various known agents.
	sphereStore.CreateAgent("Toast", worldName, "outpost")
	sphereStore.CreateAgent("sentinel", worldName, "sentinel")
	sphereStore.CreateAgent("forge", worldName, "forge")
	sphereStore.CreateAgent("governor", worldName, "governor")
	sphereStore.CreateAgent("MyEnvoy", worldName, "envoy")
	sphereStore.EnsureAgent("consul", "sphere", "consul")

	sessions := newMockSessions()
	// All of these are known and should NOT be flagged.
	// Note: sentinel is a direct process (no tmux session), so it's not listed here.
	sessions.alive["sol-"+worldName+"-Toast"] = true
	sessions.alive["sol-"+worldName+"-forge"] = true
	sessions.alive["sol-"+worldName+"-governor"] = true
	sessions.alive["sol-"+worldName+"-MyEnvoy"] = true
	sessions.alive["sol-chronicle"] = true
	sessions.alive["sol-broker"] = true

	cfg := Config{SolHome: config.Home()}
	d := New(cfg, sphereStore, sessions, nil, nil)

	// Run multiple patrols — none should be flagged.
	for i := 0; i < 5; i++ {
		stopped, err := d.detectOrphanedSessions(context.Background())
		if err != nil {
			t.Fatalf("patrol %d: detectOrphanedSessions failed: %v", i+1, err)
		}
		if stopped != 0 {
			t.Errorf("patrol %d: stopped = %d, want 0", i+1, stopped)
		}
	}

	// Verify no sessions tracked as orphans.
	if len(d.orphanedSessions) != 0 {
		t.Errorf("orphanedSessions map has %d entries, want 0",
			len(d.orphanedSessions))
	}

	// Verify no stops recorded.
	if len(sessions.stopped) != 0 {
		t.Errorf("stopped sessions = %v, want none", sessions.stopped)
	}
}

func TestDetectOrphanedSessionsMapPruned(t *testing.T) {
	setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "orphan-prune"
	sphereStore.RegisterWorld(worldName, "/tmp/repo")

	sessions := newMockSessions()
	sessions.alive["sol-"+worldName+"-GhostA"] = true
	sessions.alive["sol-"+worldName+"-GhostB"] = true

	cfg := Config{SolHome: config.Home()}
	d := New(cfg, sphereStore, sessions, nil, nil)

	// First patrol: both detected.
	d.detectOrphanedSessions(context.Background())
	if len(d.orphanedSessions) != 2 {
		t.Fatalf("expected 2 tracked orphans, got %d", len(d.orphanedSessions))
	}

	// GhostA disappears (maybe someone killed it manually).
	delete(sessions.alive, "sol-"+worldName+"-GhostA")

	// Second patrol: GhostA should be pruned, GhostB still tracked.
	d.detectOrphanedSessions(context.Background())

	if _, ok := d.orphanedSessions["sol-"+worldName+"-GhostA"]; ok {
		t.Error("GhostA should have been pruned from tracking map")
	}
	if _, ok := d.orphanedSessions["sol-"+worldName+"-GhostB"]; !ok {
		t.Error("GhostB should still be tracked")
	}
}

func TestDetectOrphanedSessionsListError(t *testing.T) {
	setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	sessions := newMockSessions()
	sessions.listErr = fmt.Errorf("tmux server not running")

	cfg := Config{SolHome: config.Home()}
	d := New(cfg, sphereStore, sessions, nil, nil)

	// Should degrade gracefully — return 0 stopped, no error.
	stopped, err := d.detectOrphanedSessions(context.Background())
	if err != nil {
		t.Errorf("expected nil error on List() failure (DEGRADE), got: %v", err)
	}
	if stopped != 0 {
		t.Errorf("stopped = %d, want 0", stopped)
	}
}

// --- State persistence tests ---

func TestOrphanCounterSurvivesRestart(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "orphan-persist"
	sphereStore.RegisterWorld(worldName, "/tmp/repo")

	sessions := newMockSessions()
	sessions.alive["sol-"+worldName+"-GhostAgent"] = true

	cfg := Config{SolHome: solHome}

	// First consul instance: detect orphan (count=1), then save state via patrol.
	d1 := New(cfg, sphereStore, sessions, nil, nil)
	d1.detectOrphanedSessions(context.Background())

	entry, ok := d1.orphanedSessions["sol-"+worldName+"-GhostAgent"]
	if !ok {
		t.Fatal("expected orphan to be tracked after first detection")
	}
	if entry.count != 1 {
		t.Errorf("count = %d, want 1", entry.count)
	}

	// Backdate firstSeen to simulate 31 minutes having passed since first detection.
	entry.firstSeen = time.Now().Add(-31 * time.Minute)

	// Persist state (simulates what Patrol does).
	d1.saveState()

	// Second consul instance: restore state, then detect again.
	d2 := New(cfg, sphereStore, sessions, nil, nil)
	d2.restoreState()

	// Verify the counter was restored.
	entry2, ok := d2.orphanedSessions["sol-"+worldName+"-GhostAgent"]
	if !ok {
		t.Fatal("expected orphan counter to survive restart")
	}
	if entry2.count != 1 {
		t.Errorf("restored count = %d, want 1", entry2.count)
	}

	// Second detection should bring count to 2, which meets the threshold.
	stopped, err := d2.detectOrphanedSessions(context.Background())
	if err != nil {
		t.Fatalf("detectOrphanedSessions failed: %v", err)
	}
	if stopped != 1 {
		t.Errorf("stopped = %d, want 1 (counter persisted, threshold met)", stopped)
	}
}

func TestAlertDebounceSurvivesRestart(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            solHome,
	}

	// First consul: set a recent alert time and persist.
	d1 := New(cfg, sphereStore, sessions, nil, nil)
	d1.lastEscalationAlert = time.Now().Add(-10 * time.Minute) // 10 min ago
	d1.saveState()

	// Second consul: restore and check that debounce is preserved.
	d2 := New(cfg, sphereStore, sessions, nil, nil)
	d2.restoreState()

	if d2.lastEscalationAlert.IsZero() {
		t.Fatal("expected lastEscalationAlert to survive restart")
	}
	// Should be within the last 15 minutes (was set to 10 min ago).
	if time.Since(d2.lastEscalationAlert) > 15*time.Minute {
		t.Errorf("lastEscalationAlert age = %v, expected ~10 minutes", time.Since(d2.lastEscalationAlert))
	}
}

func TestStatePersistenceNoFileReturnsDefaults(t *testing.T) {
	solHome := setupSolHome(t)

	sessions := newMockSessions()
	cfg := Config{SolHome: solHome}

	// No state file exists — should load zero-value defaults without error.
	d := New(cfg, nil, sessions, nil, nil)
	d.restoreState()

	if len(d.orphanedSessions) != 0 {
		t.Errorf("expected empty orphanedSessions, got %d entries", len(d.orphanedSessions))
	}
	if !d.lastEscalationAlert.IsZero() {
		t.Errorf("expected zero lastEscalationAlert, got %v", d.lastEscalationAlert)
	}
}

func TestStatePersistenceCorruptFile(t *testing.T) {
	solHome := setupSolHome(t)

	// Write corrupt state file.
	dir := filepath.Join(solHome, "consul")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "state.json"), []byte("{{not json}}"), 0o644)

	sessions := newMockSessions()
	cfg := Config{SolHome: solHome}

	// Should handle gracefully — return zero defaults.
	d := New(cfg, nil, sessions, nil, nil)
	d.restoreState()

	if len(d.orphanedSessions) != 0 {
		t.Errorf("expected empty orphanedSessions on corrupt file, got %d entries", len(d.orphanedSessions))
	}
	if !d.lastEscalationAlert.IsZero() {
		t.Errorf("expected zero lastEscalationAlert on corrupt file, got %v", d.lastEscalationAlert)
	}
}
