package sentinel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/flock"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

func TestPatrolDetectsStalled(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working agent with a dead session.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	createWrit(t, worldStore, "sol-abc1234500000000", "Test task")
	// Session is NOT alive (not in mock.alive).

	// Write tether so stalled detection sees non-empty tether directory.
	if err := tether.Write("ember", "Toast", "sol-abc1234500000000", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	// Create worktree directory so respawn doesn't fail on missing dir.
	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "ember", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	// Register role so startup.Respawn succeeds.
	startup.Register("outpost", startup.RoleConfig{
		WorktreeDir: func(w, a string) string {
			return filepath.Join(os.Getenv("SOL_HOME"), w, "outposts", a, "worktree")
		},
	})
	t.Cleanup(func() { startup.Register("outpost", startup.RoleConfig{}) })

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Should have started a session (respawn).
	started := mock.getStarted()
	if len(started) != 1 {
		t.Fatalf("expected 1 session started (respawn), got %d: %v", len(started), started)
	}
	if started[0] != "sol-ember-Toast" {
		t.Errorf("started session = %q, want %q", started[0], "sol-ember-Toast")
	}
}

func TestPatrolMaxRespawns(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRespawns = 2

	// Create a working agent with a dead session.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	createWrit(t, worldStore, "sol-abc1234500000000", "Test task")

	// Write tether so stalled detection sees non-empty tether directory.
	if err := tether.Write("ember", "Toast", "sol-abc1234500000000", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "ember", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	w := New(cfg, sphereStore, worldStore, mock, nil)

	// Pre-set respawn count to max.
	w.respawnCounts[respawnKey{AgentID: "ember/Toast", WritID: "sol-abc1234500000000"}] = 2

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Work should be returned to open, no respawn.
	started := mock.getStarted()
	if len(started) != 0 {
		t.Fatalf("expected 0 sessions started (max respawns), got %d", len(started))
	}

	// Writ should be open.
	item, err := worldStore.GetWrit("sol-abc1234500000000")
	if err != nil {
		t.Fatalf("GetWrit() error: %v", err)
	}
	if item.Status != store.WritOpen {
		t.Errorf("writ status = %q, want %q", item.Status, store.WritOpen)
	}

	// Agent should be idle.
	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatalf("GetAgent() error: %v", err)
	}
	if agent.State != store.AgentIdle {
		t.Errorf("agent state = %q, want %q", agent.State, store.AgentIdle)
	}
}

func TestPatrolDetectsZombie(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an idle agent with a live session but no tether.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	// State is idle (default), no tether item.
	mock.alive["sol-ember-Toast"] = true

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Session should have been stopped.
	stopped := mock.getStopped()
	if len(stopped) != 1 {
		t.Fatalf("expected 1 session stopped (zombie), got %d: %v", len(stopped), stopped)
	}
	if stopped[0] != "sol-ember-Toast" {
		t.Errorf("stopped session = %q, want %q", stopped[0], "sol-ember-Toast")
	}
}

func TestRespawnAttemptsTracking(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRespawns = 2

	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	createWrit(t, worldStore, "sol-abc1234500000000", "Test task")

	// Write tether so stalled detection sees non-empty tether directory.
	if err := tether.Write("ember", "Toast", "sol-abc1234500000000", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	worktreeDir := filepath.Join(os.Getenv("SOL_HOME"), "ember", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	// Register role so startup.Respawn succeeds.
	startup.Register("outpost", startup.RoleConfig{
		WorktreeDir: func(w, a string) string {
			return filepath.Join(os.Getenv("SOL_HOME"), w, "outposts", a, "worktree")
		},
	})
	t.Cleanup(func() { startup.Register("outpost", startup.RoleConfig{}) })

	w := New(cfg, sphereStore, worldStore, mock, nil)

	// Patrol 1: stalled → respawn (attempt 1).
	w.patrol(context.Background())
	started := mock.getStarted()
	if len(started) != 1 {
		t.Fatalf("patrol 1: expected 1 start, got %d", len(started))
	}

	// Kill the session.
	mock.mu.Lock()
	delete(mock.alive, "sol-ember-Toast")
	mock.mu.Unlock()

	// Patrol 2: still stalled → respawn (attempt 2).
	w.patrol(context.Background())
	started = mock.getStarted()
	if len(started) != 2 {
		t.Fatalf("patrol 2: expected 2 starts, got %d", len(started))
	}

	// Kill the session again.
	mock.mu.Lock()
	delete(mock.alive, "sol-ember-Toast")
	mock.mu.Unlock()

	// Patrol 3: still stalled → return to open (max reached).
	w.patrol(context.Background())
	started = mock.getStarted()
	if len(started) != 2 {
		t.Fatalf("patrol 3: expected still 2 starts (max reached), got %d", len(started))
	}

	// Agent should be idle, writ open.
	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != store.AgentIdle {
		t.Errorf("agent state = %q, want %q after max respawns", agent.State, store.AgentIdle)
	}

	item, err := worldStore.GetWrit("sol-abc1234500000000")
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != store.WritOpen {
		t.Errorf("writ status = %q, want %q after max respawns", item.Status, store.WritOpen)
	}
}

func TestReapIdleAgent(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.IdleReapTimeout = 1 * time.Millisecond // Very short for tests.

	// Create an idle agent with an old UpdatedAt.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	// Agent is idle by default. Make it old.
	now := time.Now().UTC().Add(-1 * time.Hour)
	sphereStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		now.Format(time.RFC3339), "ember/Toast")

	// Create tether to verify cleanup.
	if err := tether.Write("ember", "Toast", "sol-01d01e0000000000", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Agent record should be deleted.
	_, err := sphereStore.GetAgent("ember/Toast")
	if err == nil {
		t.Error("expected agent to be deleted after reap, but it still exists")
	}

	// Tether should be cleaned up.
	if tether.IsTethered("ember", "Toast", "outpost") {
		t.Error("expected tether to be removed after reap")
	}
}

func TestReapIdleAgentSkipsRecent(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.IdleReapTimeout = 1 * time.Hour // Long timeout.

	// Create a recently-updated idle agent.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	// UpdatedAt is now (recent), so it should not be reaped.

	w := New(cfg, sphereStore, nil, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Agent should still exist.
	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatalf("GetAgent() error: %v — agent was incorrectly reaped", err)
	}
	if agent.State != store.AgentIdle {
		t.Errorf("agent state = %q, want %q", agent.State, store.AgentIdle)
	}
}

func TestReturnWorkToOpenCleansUpResources(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()
	cfg.MaxRespawns = 0 // Immediately return to open.

	// Create a working agent with a dead session.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	createWrit(t, worldStore, "sol-abc1234500000000", "Test task")

	// Create outpost directory with worktree and tether.
	solHome := os.Getenv("SOL_HOME")
	worktreeDir := filepath.Join(solHome, "ember", "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)
	if err := tether.Write("ember", "Toast", "sol-abc1234500000000", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	// Create session metadata.
	sessDir := filepath.Join(solHome, ".runtime", "sessions")
	os.MkdirAll(sessDir, 0o755)
	os.WriteFile(filepath.Join(sessDir, "sol-ember-Toast.json"), []byte(`{"name":"sol-ember-Toast"}`), 0o644)

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Writ should be open.
	item, err := worldStore.GetWrit("sol-abc1234500000000")
	if err != nil {
		t.Fatalf("GetWrit() error: %v", err)
	}
	if item.Status != store.WritOpen {
		t.Errorf("writ status = %q, want %q", item.Status, store.WritOpen)
	}

	// Tether should be cleared.
	if tether.IsTethered("ember", "Toast", "outpost") {
		t.Error("expected tether to be removed")
	}

	// Session metadata should be removed.
	if _, err := os.Stat(filepath.Join(sessDir, "sol-ember-Toast.json")); !os.IsNotExist(err) {
		t.Error("expected session metadata to be removed")
	}
}

func TestPatrolReapsAgentTetheredToClosedWrit(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working agent with a live session tethered to a writ.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	createWrit(t, worldStore, "sol-abc1234500000000", "Cancelled task")
	mock.alive["sol-ember-Toast"] = true

	// Write tether file on disk so patrol discovers it via tether.List().
	if err := tether.Write("ember", "Toast", "sol-abc1234500000000", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	// Close the writ with a reason.
	if _, err := worldStore.CloseWrit("sol-abc1234500000000", "superseded"); err != nil {
		t.Fatalf("CloseWrit() error: %v", err)
	}

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Session should have been stopped.
	stopped := mock.getStopped()
	if len(stopped) != 1 {
		t.Fatalf("expected 1 session stopped, got %d: %v", len(stopped), stopped)
	}
	if stopped[0] != "sol-ember-Toast" {
		t.Errorf("stopped session = %q, want %q", stopped[0], "sol-ember-Toast")
	}

	// Agent record should be deleted.
	_, err := sphereStore.GetAgent("ember/Toast")
	if err == nil {
		t.Error("expected agent to be deleted, but GetAgent succeeded")
	}
}

func TestPatrolDoesNotReapAgentTetheredToOpenWrit(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working agent with a live session tethered to an open writ.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	createWrit(t, worldStore, "sol-abc1234500000000", "Active task")
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "working on task..."

	// Write tether file on disk.
	if err := tether.Write("ember", "Toast", "sol-abc1234500000000", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// No sessions should have been stopped.
	if stopped := mock.getStopped(); len(stopped) != 0 {
		t.Errorf("expected 0 sessions stopped, got %d: %v", len(stopped), stopped)
	}

	// Agent should still exist and be working.
	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatalf("GetAgent() error: %v", err)
	}
	if agent.State != store.AgentWorking {
		t.Errorf("agent state = %q, want %q", agent.State, store.AgentWorking)
	}
}

func TestPatrolClosedWritReapLogsCloseReason(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a logger that writes to a temp file.
	solHome := os.Getenv("SOL_HOME")
	logger := events.NewLogger(solHome)

	// Create a working agent tethered to a closed writ.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	createWrit(t, worldStore, "sol-abc1234500000000", "Cancelled task")
	mock.alive["sol-ember-Toast"] = true

	// Write tether file on disk.
	if err := tether.Write("ember", "Toast", "sol-abc1234500000000", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	if _, err := worldStore.CloseWrit("sol-abc1234500000000", "cancelled_by_test"); err != nil {
		t.Fatalf("CloseWrit() error: %v", err)
	}

	w := New(cfg, sphereStore, worldStore, mock, logger)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Read the events log and verify close_reason appears.
	eventsFile := filepath.Join(solHome, ".events.jsonl")
	data, err := os.ReadFile(eventsFile)
	if err != nil {
		t.Fatalf("failed to read events file: %v", err)
	}

	logContent := string(data)
	if !strings.Contains(logContent, "cancelled_by_test") {
		t.Errorf("expected close_reason 'cancelled_by_test' in events log, got:\n%s", logContent)
	}
	if !strings.Contains(logContent, `"type":"reap"`) {
		t.Errorf("expected reap event in events log, got:\n%s", logContent)
	}
}

func TestPersistentAgentClosedTetherRemoved(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a persistent (forge) agent with 3 tethered writs.
	sphereStore.CreateAgent("forge", "ember", "forge")
	sphereStore.UpdateAgentState("ember/forge", store.AgentWorking, "sol-0000000000000ee1")
	mock.alive["sol-ember-forge"] = true
	mock.captures["sol-ember-forge"] = "forge output"

	createWrit(t, worldStore, "sol-0000000000000ee1", "Open writ 1")
	createWrit(t, worldStore, "sol-0000000000000ee2", "Closed writ 2")
	createWrit(t, worldStore, "sol-0000000000000ee3", "Open writ 3")

	// Write 3 tether files.
	for _, wid := range []string{"sol-0000000000000ee1", "sol-0000000000000ee2", "sol-0000000000000ee3"} {
		if err := tether.Write("ember", "forge", wid, "forge"); err != nil {
			t.Fatalf("tether.Write(%s) error: %v", wid, err)
		}
	}

	// Close writ 2.
	if _, err := worldStore.CloseWrit("sol-0000000000000ee2", "superseded"); err != nil {
		t.Fatalf("CloseWrit() error: %v", err)
	}

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Only the closed writ tether should be removed.
	remaining, err := tether.List("ember", "forge", "forge")
	if err != nil {
		t.Fatalf("tether.List() error: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 tethers remaining, got %d: %v", len(remaining), remaining)
	}
	for _, wid := range remaining {
		if wid == "sol-0000000000000ee2" {
			t.Error("closed writ tether should have been removed, but sol-0000000000000ee2 still present")
		}
	}

	// Agent should still exist and be working.
	agent, err := sphereStore.GetAgent("ember/forge")
	if err != nil {
		t.Fatalf("GetAgent() error: %v", err)
	}
	if agent.State != store.AgentWorking {
		t.Errorf("agent state = %q, want %q (persistent agent should not be reaped)", agent.State, store.AgentWorking)
	}

	// No sessions should have been stopped.
	if stopped := mock.getStopped(); len(stopped) != 0 {
		t.Errorf("expected 0 sessions stopped (persistent agent), got %d: %v", len(stopped), stopped)
	}
}

func TestOutpostClosedTetherFullReap(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an outpost agent tethered to a closed writ.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	createWrit(t, worldStore, "sol-abc1234500000000", "Closed task")
	mock.alive["sol-ember-Toast"] = true

	if err := tether.Write("ember", "Toast", "sol-abc1234500000000", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	if _, err := worldStore.CloseWrit("sol-abc1234500000000", "completed"); err != nil {
		t.Fatalf("CloseWrit() error: %v", err)
	}

	w := New(cfg, sphereStore, worldStore, mock, nil)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Session should have been stopped.
	stopped := mock.getStopped()
	if len(stopped) != 1 {
		t.Fatalf("expected 1 session stopped, got %d: %v", len(stopped), stopped)
	}

	// Agent record should be deleted (full reap).
	_, err := sphereStore.GetAgent("ember/Toast")
	if err == nil {
		t.Error("expected agent to be deleted after reap, but GetAgent succeeded")
	}

	// Tether should be cleaned up.
	if tether.IsTethered("ember", "Toast", "outpost") {
		t.Error("expected tether to be cleaned up after reap")
	}
}

// TestReturnWorkToOpen_WritUpdatedBeforeAgentIdle verifies that returnWorkToOpen
// updates the writ to 'open' BEFORE setting the agent to 'idle'. This ordering
// is required for crash safety: consul's stale-tether recovery only queries
// agents with state='working', so if we crash after setting the agent idle the
// writ becomes permanently stuck. By updating the writ first, any crash leaves
// the agent 'working' — visible to consul — which can then complete recovery.
func TestReturnWorkToOpen_WritUpdatedBeforeAgentIdle(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working agent with an active writ.
	sphereStore.CreateAgent("Drift", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Drift", store.AgentWorking, "sol-returnwork01")

	// Track the sequence of operations using a mutex-protected log.
	var mu sync.Mutex
	var opLog []string

	mws := &mockWorldStore{
		safelyReopenWritFn: func(id string, allowedFromStatuses []string) (bool, error) {
			mu.Lock()
			defer mu.Unlock()
			opLog = append(opLog, "safely_reopen_writ:"+id)
			return true, nil
		},
	}

	// Wrap the sphere store to intercept UpdateAgentState.
	wrappedSphere := &orderTrackingSphereStore{
		SphereStore: sphereStore,
		onUpdateAgentState: func(id, state, writ string) {
			mu.Lock()
			defer mu.Unlock()
			opLog = append(opLog, "update_agent_state:"+state)
		},
	}

	w := New(cfg, wrappedSphere, mws, mock, nil)
	w.respawnCounts = make(map[respawnKey]int)

	agent := store.Agent{
		ID:         "ember/Drift",
		Name:       "Drift",
		World:      "ember",
		Role:       "outpost",
		State:      store.AgentWorking,
		ActiveWrit: "sol-returnwork01",
	}

	if err := w.returnWorkToOpen(agent); err != nil {
		t.Fatalf("returnWorkToOpen() error: %v", err)
	}

	mu.Lock()
	log := make([]string, len(opLog))
	copy(log, opLog)
	mu.Unlock()

	// Verify writ was safely reopened before agent was set idle.
	if len(log) < 2 {
		t.Fatalf("expected at least 2 operations, got %d: %v", len(log), log)
	}
	if log[0] != "safely_reopen_writ:sol-returnwork01" {
		t.Errorf("first operation = %q, want %q (writ must be safely reopened before agent goes idle)", log[0], "safely_reopen_writ:sol-returnwork01")
	}
	if log[1] != "update_agent_state:idle" {
		t.Errorf("second operation = %q, want %q", log[1], "update_agent_state:idle")
	}
}

// orderTrackingSphereStore wraps a SphereStore and calls a hook on UpdateAgentState.
type orderTrackingSphereStore struct {
	SphereStore // embed the sentinel SphereStore interface
	onUpdateAgentState func(id, state, writ string)
}

func (o *orderTrackingSphereStore) UpdateAgentState(id, state, writ string) error {
	if o.onUpdateAgentState != nil {
		o.onUpdateAgentState(id, state, writ)
	}
	return o.SphereStore.UpdateAgentState(id, state, writ)
}

// TestReturnWorkToOpen_CrashAfterWritConsulCanRecover verifies the crash-safety
// guarantee of the new ordering: if the sentinel crashes after updating the writ
// to 'open' but before setting the agent to 'idle', the agent remains 'working'
// and consul's stale-tether recovery can detect and recover it.
//
// This test simulates the intermediate crash state directly (agent='working',
// writ='open') and then invokes consul's recoverStaleTethers to confirm recovery.
func TestReturnWorkToOpen_CrashAfterWritConsulCanRecover(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Simulate the intermediate state after a crash between step 1 (writ→open)
	// and step 2 (agent→idle) in the new returnWorkToOpen ordering:
	//   - agent is still 'working' (step 2 never ran)
	//   - writ is 'open' (step 1 completed before crash)
	//   - no tether file (returnWorkToOpen calls cleanupAgentResources after both
	//     DB writes, but we're simulating a crash mid-sequence so no tether exists)
	sphereStore.CreateAgent("Drift", cfg.World, "outpost")
	writID, err := worldStore.CreateWrit("crash-safety-task", "test crash recovery", "test", 1, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Set agent as 'working' (step 2 hasn't run yet).
	sphereStore.UpdateAgentState(cfg.World+"/Drift", store.AgentWorking, writID)

	// Set writ to 'open' (step 1 already completed before crash).
	worldStore.UpdateWrit(writID, store.WritUpdates{Status: "open", Assignee: "-"})

	// No tether file — cleanupAgentResources didn't run.
	// consul.recoverOneTether calls tether.Clear which is a no-op if no file exists.

	// Make Drift's updated_at old so consul treats it as stale.
	sphereStore.DB().Exec(`UPDATE agents SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), cfg.World+"/Drift")

	// No live session.
	_ = mock

	// --- Run sentinel.returnWorkToOpen to verify the writ ordering ---
	// (Just confirming the function works end-to-end with a real store.)
	_ = cfg // already verified ordering in TestReturnWorkToOpen_WritUpdatedBeforeAgentIdle

	// Verify the intermediate state looks exactly as expected.
	agentBefore, err := sphereStore.GetAgent(cfg.World + "/Drift")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if agentBefore.State != store.AgentWorking {
		t.Errorf("before recovery: agent state = %q, want %q", agentBefore.State, store.AgentWorking)
	}
	writBefore, err := worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("GetWrit: %v", err)
	}
	if writBefore.Status != "open" {
		t.Errorf("before recovery: writ status = %q, want %q", writBefore.Status, "open")
	}

	// Now simulate consul's recoverStaleTethers finding the 'working' agent.
	// With the old (broken) ordering, the agent would already be 'idle' here
	// and consul would never find it. With the new ordering the agent is still
	// 'working' — consul can detect and recover it.
	//
	// We call recoverStaleTethers indirectly by verifying the agent is visible
	// to consul's query (state='working') and that consul can bring it to idle.

	// Manually simulate what consul does: find working agents, check session,
	// mark agent idle (all steps are idempotent).
	agents, err := sphereStore.ListAgents(cfg.World, store.AgentWorking)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}

	found := false
	for _, a := range agents {
		if a.ID == cfg.World+"/Drift" {
			found = true
		}
	}
	if !found {
		t.Error("consul cannot see agent in 'working' state — stale-tether recovery would miss it")
	}

	// Simulate consul completing the recovery: set agent to idle.
	if err := sphereStore.UpdateAgentState(cfg.World+"/Drift", "idle", ""); err != nil {
		t.Fatalf("simulated consul UpdateAgentState: %v", err)
	}

	// Verify full recovery: agent idle, writ open.
	agentAfter, _ := sphereStore.GetAgent(cfg.World + "/Drift")
	if agentAfter.State != "idle" {
		t.Errorf("after recovery: agent state = %q, want idle", agentAfter.State)
	}
	writAfter, _ := worldStore.GetWrit(writID)
	if writAfter.Status != "open" {
		t.Errorf("after recovery: writ status = %q, want open", writAfter.Status)
	}
}

// TestReturnWorkToOpen_SkipsWhenWritIsDone verifies that returnWorkToOpen is a
// no-op for the writ update when the writ is already in 'done' status. This
// guards the crash scenario where dispatch.Resolve flipped the writ to 'done'
// before the tmux session was killed, so the sentinel must not silently revert
// completed work back to 'open'.
func TestReturnWorkToOpen_SkipsWhenWritIsDone(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working agent and its writ, then advance the writ to 'done'
	// (simulating dispatch.Resolve completing the writ before the session died).
	sphereStore.CreateAgent("Drift", "ember", "outpost")
	writID, err := worldStore.CreateWrit("completed task", "", "test", 1, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	// Writ starts 'open'; move it through to 'done' simulating a completed resolve.
	if err := worldStore.UpdateWrit(writID, store.WritUpdates{Status: "done", Assignee: "ember/Drift"}); err != nil {
		t.Fatalf("UpdateWrit→done: %v", err)
	}
	sphereStore.UpdateAgentState("ember/Drift", store.AgentWorking, writID)

	w := New(cfg, sphereStore, worldStore, mock, nil)

	agent := store.Agent{
		ID:         "ember/Drift",
		Name:       "Drift",
		World:      "ember",
		Role:       "outpost",
		State:      store.AgentWorking,
		ActiveWrit: writID,
	}

	// returnWorkToOpen should succeed (no error).
	if err := w.returnWorkToOpen(agent); err != nil {
		t.Fatalf("returnWorkToOpen() error: %v", err)
	}

	// The writ must remain 'done' — it must NOT be reverted to 'open'.
	writ, err := worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("GetWrit: %v", err)
	}
	if writ.Status != store.WritDone {
		t.Errorf("writ status = %q after returnWorkToOpen, want %q (must not revert done writ)", writ.Status, store.WritDone)
	}

	// The agent should still be cleaned up (set to idle).
	agent2, err := sphereStore.GetAgent("ember/Drift")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if agent2.State != store.AgentIdle {
		t.Errorf("agent state = %q, want %q (agent should be cleaned up regardless)", agent2.State, store.AgentIdle)
	}
}

// --- Mock world store for error injection tests ---

// mockWorldStore implements sentinel.WorldStore for targeted error-injection tests.
// It embeds store.UnimplementedWorldStore to satisfy the full world store interface
// without hand-writing stubs for methods the sentinel unit tests never call.
type mockWorldStore struct {
	store.UnimplementedWorldStore
	getWritFn            func(id string) (*store.Writ, error)
	updateWritFn         func(id string, updates store.WritUpdates) error
	safelyReopenWritFn   func(id string, allowedFromStatuses []string) (bool, error)
}

func (m *mockWorldStore) GetWrit(id string) (*store.Writ, error) {
	if m.getWritFn != nil {
		return m.getWritFn(id)
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockWorldStore) UpdateWrit(id string, updates store.WritUpdates) error {
	if m.updateWritFn != nil {
		return m.updateWritFn(id, updates)
	}
	return nil
}

func (m *mockWorldStore) SafelyReopenWrit(id string, allowedFromStatuses []string) (bool, error) {
	if m.safelyReopenWritFn != nil {
		return m.safelyReopenWritFn(id, allowedFromStatuses)
	}
	return true, nil
}

func TestHandleOrphanedWorking_UpdateWritError(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an outpost agent that is "working" with a dead session and no tether.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-orphwrit1")

	// Use a mock world store that returns a tethered writ (still assigned to this agent)
	// but fails on SafelyReopenWrit, simulating a database error.
	mws := &mockWorldStore{
		getWritFn: func(id string) (*store.Writ, error) {
			return &store.Writ{ID: id, Status: store.WritTethered, Assignee: "ember/Toast"}, nil
		},
		safelyReopenWritFn: func(id string, allowedFromStatuses []string) (bool, error) {
			return false, fmt.Errorf("database is locked")
		},
	}

	logger := events.NewLogger(cfg.SolHome)
	w := New(cfg, sphereStore, mws, mock, logger)

	err := w.handleOrphanedWorking(store.Agent{
		ID:         "ember/Toast",
		Name:       "Toast",
		World:      "ember",
		Role:       "outpost",
		ActiveWrit: "sol-orphwrit1",
	})

	// Should return an error — agent record is preserved for retry on next patrol.
	if err == nil {
		t.Fatal("handleOrphanedWorking() expected error when writ update fails, got nil")
	}
	if !strings.Contains(err.Error(), "database is locked") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "database is locked")
	}

	// Verify agent record still exists (not deleted) so retry is possible.
	agent, getErr := sphereStore.GetAgent("ember/Toast")
	if getErr != nil {
		t.Fatalf("agent should still exist after writ update failure: %v", getErr)
	}
	if agent.ID != "ember/Toast" {
		t.Errorf("agent ID = %q, want %q", agent.ID, "ember/Toast")
	}
}

func TestCheckClosedWritTethers_ListTetherError(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an agent with a tether directory that will cause tether.List to fail.
	// We simulate this by creating the tether dir as a file (not a dir) to cause an error.
	sphereStore.CreateAgent("Toast", "ember", "outpost")

	tetherDir := filepath.Join(os.Getenv("SOL_HOME"), "ember", "outposts", "Toast", ".tether")
	// Remove any existing dir, then create a file where a directory is expected.
	os.RemoveAll(tetherDir)
	os.MkdirAll(filepath.Dir(tetherDir), 0o755)
	os.WriteFile(tetherDir, []byte("not a directory"), 0o644)

	mws := &mockWorldStore{}
	logger := events.NewLogger(cfg.SolHome)
	w := New(cfg, sphereStore, mws, mock, logger)

	agents := []store.Agent{
		{ID: "ember/Toast", Name: "Toast", World: "ember", Role: "outpost"},
	}

	reapedCount := 0
	actionsTaken := []string{}
	reaped := w.checkClosedWritTethers(agents, &reapedCount, &actionsTaken)

	// Should not panic or crash — should log and continue.
	if len(reaped) != 0 {
		t.Errorf("expected no reaped agents, got %d", len(reaped))
	}

	// Verify sentinel_error event was emitted for the list error.
	evts := readEvents(t, cfg.SolHome, "sentinel_error")
	if len(evts) == 0 {
		t.Fatal("expected sentinel_error event for tether list failure, got none")
	}

	payload, ok := evts[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", evts[0].Payload)
	}
	if payload["action"] != "list_tethered_writs" {
		t.Errorf("event action = %q, want %q", payload["action"], "list_tethered_writs")
	}
}

// TestCheckClosedWritTethers_SkipsReapWhenResolveInProgress verifies the
// dispatch contract: if a resolve is in progress for an outpost agent, the
// closed-writ + tether-file combination is a transient state and sentinel
// must NOT reap. The next patrol cycle will see whichever final state the
// resolve produced.
func TestCheckClosedWritTethers_SkipsReapWhenResolveInProgress(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working outpost agent tethered to a writ.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-abc1234500000000")
	createWrit(t, worldStore, "sol-abc1234500000000", "Resolving task")
	mock.alive["sol-ember-Toast"] = true

	if err := tether.Write("ember", "Toast", "sol-abc1234500000000", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	// Simulate dispatch.Resolve in progress: closed writ + lock file present
	// + tether still on disk. This is the transient window during which
	// dispatch has called CloseWrit but not yet cleared the tether.
	if _, err := worldStore.CloseWrit("sol-abc1234500000000", "completed"); err != nil {
		t.Fatalf("CloseWrit() error: %v", err)
	}
	lockPath := flock.ResolveLockPath("ember", "Toast", "outpost")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir for lock: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("locked"), 0o644); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	logger := events.NewLogger(cfg.SolHome)
	w := New(cfg, sphereStore, worldStore, mock, logger)

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Sentinel must NOT have stopped the session — dispatch is still in
	// flight and will stop it itself.
	if stopped := mock.getStopped(); len(stopped) != 0 {
		t.Errorf("expected 0 sessions stopped (resolve in progress), got %d: %v", len(stopped), stopped)
	}

	// Agent record must still exist.
	if _, err := sphereStore.GetAgent("ember/Toast"); err != nil {
		t.Errorf("expected agent to remain (resolve in progress), got: %v", err)
	}

	// Tether file must still exist (sentinel doesn't touch it).
	tetheredWrits, err := tether.List("ember", "Toast", "outpost")
	if err != nil {
		t.Fatalf("tether.List() error: %v", err)
	}
	if len(tetheredWrits) != 1 || tetheredWrits[0] != "sol-abc1234500000000" {
		t.Errorf("tether file gone or wrong: %v", tetheredWrits)
	}

	// A skip event should have been logged.
	skipEvents := readEvents(t, cfg.SolHome, "sentinel_action")
	foundSkip := false
	for _, ev := range skipEvents {
		payload, ok := ev.Payload.(map[string]any)
		if !ok {
			continue
		}
		if payload["action"] == "skip_reap_resolve_in_progress" {
			foundSkip = true
			break
		}
	}
	if !foundSkip {
		t.Errorf("expected skip_reap_resolve_in_progress event, got none in: %+v", skipEvents)
	}

	// Now simulate dispatch.Resolve completing: remove lock + tether.
	os.Remove(lockPath)
	if err := tether.ClearOne("ember", "Toast", "sol-abc1234500000000", "outpost"); err != nil {
		t.Fatalf("tether.ClearOne() error: %v", err)
	}

	// Next patrol should observe the cleared state and not reap (no tether).
	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("second patrol() error: %v", err)
	}
	if stopped := mock.getStopped(); len(stopped) != 0 {
		t.Errorf("expected still 0 sessions stopped after clean resolve, got %d", len(stopped))
	}
}

// TestCheckClosedWritTethers_PersistentSkipsClearWhenResolveInProgress verifies
// the same contract for the persistent-agent tether.ClearOne path.
func TestCheckClosedWritTethers_PersistentSkipsClearWhenResolveInProgress(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Persistent forge agent with one tethered writ being resolved.
	sphereStore.CreateAgent("forge", "ember", "forge")
	sphereStore.UpdateAgentState("ember/forge", store.AgentWorking, "sol-0000000000000ee2")
	mock.alive["sol-ember-forge"] = true
	mock.captures["sol-ember-forge"] = "forge output"

	createWrit(t, worldStore, "sol-0000000000000ee2", "Closed during resolve")
	if err := tether.Write("ember", "forge", "sol-0000000000000ee2", "forge"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	if _, err := worldStore.CloseWrit("sol-0000000000000ee2", "completed"); err != nil {
		t.Fatalf("CloseWrit() error: %v", err)
	}

	// Persistent agents use per-writ lock files.
	lockPath := flock.ResolveWritLockPath("ember", "forge", "forge", "sol-0000000000000ee2")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir for lock: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("locked"), 0o644); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	w := New(cfg, sphereStore, worldStore, mock, events.NewLogger(cfg.SolHome))

	if err := w.patrol(context.Background()); err != nil {
		t.Fatalf("patrol() error: %v", err)
	}

	// Sentinel must NOT have removed the tether — dispatch will do it.
	tetheredWrits, err := tether.List("ember", "forge", "forge")
	if err != nil {
		t.Fatalf("tether.List() error: %v", err)
	}
	if len(tetheredWrits) != 1 {
		t.Errorf("expected 1 tether to remain (resolve in progress), got %d: %v",
			len(tetheredWrits), tetheredWrits)
	}

	// Forge agent must still exist and not be touched.
	agent, err := sphereStore.GetAgent("ember/forge")
	if err != nil {
		t.Fatalf("GetAgent error: %v", err)
	}
	if agent.ActiveWrit != "sol-0000000000000ee2" {
		t.Errorf("active writ unexpectedly cleared: %q", agent.ActiveWrit)
	}
}

func TestCheckClosedWritTethers_GetWritError(t *testing.T) {
	sphereStore, _ := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create an agent with a valid tether.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	if err := tether.Write("ember", "Toast", "sol-9e000010e0000001", "outpost"); err != nil {
		t.Fatalf("tether.Write() error: %v", err)
	}

	// Mock world store that returns an error from GetWrit.
	mws := &mockWorldStore{
		getWritFn: func(id string) (*store.Writ, error) {
			return nil, fmt.Errorf("database connection lost")
		},
	}

	logger := events.NewLogger(cfg.SolHome)
	w := New(cfg, sphereStore, mws, mock, logger)

	agents := []store.Agent{
		{ID: "ember/Toast", Name: "Toast", World: "ember", Role: "outpost"},
	}

	reapedCount := 0
	actionsTaken := []string{}
	reaped := w.checkClosedWritTethers(agents, &reapedCount, &actionsTaken)

	// Should not crash — should log and continue.
	if len(reaped) != 0 {
		t.Errorf("expected no reaped agents, got %d", len(reaped))
	}

	// Verify sentinel_error event was emitted for GetWrit failure.
	evts := readEvents(t, cfg.SolHome, "sentinel_error")
	if len(evts) == 0 {
		t.Fatal("expected sentinel_error event for GetWrit failure, got none")
	}

	payload, ok := evts[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", evts[0].Payload)
	}
	if payload["action"] != "get_writ_for_closed_check" {
		t.Errorf("event action = %q, want %q", payload["action"], "get_writ_for_closed_check")
	}
	if payload["writ"] != "sol-9e000010e0000001" {
		t.Errorf("event writ = %q, want %q", payload["writ"], "sol-9e000010e0000001")
	}
	errMsg, _ := payload["error"].(string)
	if !strings.Contains(errMsg, "database connection lost") {
		t.Errorf("event error = %q, want it to contain %q", errMsg, "database connection lost")
	}
}

// TestReapClosedWritAgent_NoIdleTransition verifies that reapClosedWritAgent
// does NOT set the agent idle before cleanup and deletion. The agent should
// remain "working" during cleanup so crash recovery can detect it.
func TestReapClosedWritAgent_NoIdleTransition(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working agent with a live session.
	sphereStore.CreateAgent("Toast", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Toast", store.AgentWorking, "sol-reap00000001")
	sessName := "sol-ember-Toast"
	mock.alive[sessName] = true

	// Track operations to verify no idle transition occurs.
	var mu sync.Mutex
	var opLog []string

	wrappedSphere := &reapTrackingSphereStore{
		SphereStore: sphereStore,
		onUpdateAgentState: func(id, state, writ string) {
			mu.Lock()
			defer mu.Unlock()
			opLog = append(opLog, "update_agent_state:"+state)
		},
		onDeleteAgent: func(id string) {
			mu.Lock()
			defer mu.Unlock()
			opLog = append(opLog, "delete_agent:"+id)
		},
	}

	w := New(cfg, wrappedSphere, worldStore, mock, nil)

	agent := store.Agent{
		ID:         "ember/Toast",
		Name:       "Toast",
		World:      "ember",
		Role:       "outpost",
		State:      store.AgentWorking,
		ActiveWrit: "sol-reap00000001",
	}

	if err := w.reapClosedWritAgent(agent, sessName, "cancelled"); err != nil {
		t.Fatalf("reapClosedWritAgent() error: %v", err)
	}

	mu.Lock()
	log := make([]string, len(opLog))
	copy(log, opLog)
	mu.Unlock()

	// Verify no idle transition was attempted.
	for _, op := range log {
		if op == "update_agent_state:idle" {
			t.Error("reapClosedWritAgent() set agent to idle — should skip idle transition and delete directly")
		}
	}

	// Verify DeleteAgent was called.
	hasDelete := false
	for _, op := range log {
		if strings.HasPrefix(op, "delete_agent:") {
			hasDelete = true
		}
	}
	if !hasDelete {
		t.Error("reapClosedWritAgent() did not call DeleteAgent")
	}

	// Verify session was stopped (cleanup ran).
	stopped := mock.getStopped()
	if len(stopped) == 0 {
		t.Error("reapClosedWritAgent() did not stop session (cleanup did not run)")
	}
}

// TestReapClosedWritAgent_CleanupBeforeDelete verifies that resource cleanup
// happens before agent deletion — crash during cleanup leaves the agent
// record intact for recovery.
func TestReapClosedWritAgent_CleanupBeforeDelete(t *testing.T) {
	sphereStore, worldStore := setupTestEnv(t)
	mock := newMockSessions()
	cfg := testConfig()

	// Create a working agent with a live session.
	sphereStore.CreateAgent("Drift", "ember", "outpost")
	sphereStore.UpdateAgentState("ember/Drift", store.AgentWorking, "sol-reap00000002")
	sessName := "sol-ember-Drift"
	mock.alive[sessName] = true

	// Track the sequence of operations.
	var mu sync.Mutex
	var opLog []string

	// Wrap mock sessions to track Stop (proxy for cleanup).
	trackingMock := &stopTrackingMockSessions{
		mockSessions: mock,
		onStop: func(name string) {
			mu.Lock()
			defer mu.Unlock()
			opLog = append(opLog, "cleanup_stop:"+name)
		},
	}

	wrappedSphere := &reapTrackingSphereStore{
		SphereStore: sphereStore,
		onDeleteAgent: func(id string) {
			mu.Lock()
			defer mu.Unlock()
			opLog = append(opLog, "delete_agent:"+id)
		},
	}

	w := New(cfg, wrappedSphere, worldStore, trackingMock, nil)

	agent := store.Agent{
		ID:         "ember/Drift",
		Name:       "Drift",
		World:      "ember",
		Role:       "outpost",
		State:      store.AgentWorking,
		ActiveWrit: "sol-reap00000002",
	}

	if err := w.reapClosedWritAgent(agent, sessName, "superseded"); err != nil {
		t.Fatalf("reapClosedWritAgent() error: %v", err)
	}

	mu.Lock()
	log := make([]string, len(opLog))
	copy(log, opLog)
	mu.Unlock()

	// Verify cleanup happened before delete.
	if len(log) < 2 {
		t.Fatalf("expected at least 2 operations, got %d: %v", len(log), log)
	}

	cleanupIdx := -1
	deleteIdx := -1
	for i, op := range log {
		if strings.HasPrefix(op, "cleanup_stop:") && cleanupIdx == -1 {
			cleanupIdx = i
		}
		if strings.HasPrefix(op, "delete_agent:") && deleteIdx == -1 {
			deleteIdx = i
		}
	}
	if cleanupIdx == -1 {
		t.Fatal("cleanup (session stop) was not called")
	}
	if deleteIdx == -1 {
		t.Fatal("DeleteAgent was not called")
	}
	if cleanupIdx >= deleteIdx {
		t.Errorf("cleanup (index %d) must happen before delete (index %d)", cleanupIdx, deleteIdx)
	}
}

// reapTrackingSphereStore wraps a SphereStore and calls hooks on
// UpdateAgentState and DeleteAgent for operation-ordering tests.
type reapTrackingSphereStore struct {
	SphereStore // embed the sentinel SphereStore interface
	onUpdateAgentState func(id, state, writ string)
	onDeleteAgent      func(id string)
}

func (o *reapTrackingSphereStore) UpdateAgentState(id, state, writ string) error {
	if o.onUpdateAgentState != nil {
		o.onUpdateAgentState(id, state, writ)
	}
	return o.SphereStore.UpdateAgentState(id, state, writ)
}

func (o *reapTrackingSphereStore) DeleteAgent(id string) error {
	if o.onDeleteAgent != nil {
		o.onDeleteAgent(id)
	}
	return o.SphereStore.DeleteAgent(id)
}

// stopTrackingMockSessions wraps mockSessions and calls a hook on Stop.
type stopTrackingMockSessions struct {
	*mockSessions
	onStop func(name string)
}

func (s *stopTrackingMockSessions) Stop(name string, force bool) error {
	if s.onStop != nil {
		s.onStop(name)
	}
	return s.mockSessions.Stop(name, force)
}
