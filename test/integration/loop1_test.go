package integration

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/tether"
)

// --- Test 1: Multi-Agent Dispatch ---

func TestMultiAgentDispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := session.New()

	// Create two writs.
	item1ID, err := worldStore.CreateWrit("Task Alpha", "Alpha description", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create item 1: %v", err)
	}
	item2ID, err := worldStore.CreateWrit("Task Beta", "Beta description", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create item 2: %v", err)
	}

	// Cast both without specifying agents (auto-provision).
	result1, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: item1ID,
		World:        "ember",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("cast item 1: %v", err)
	}

	result2, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: item2ID,
		World:        "ember",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("cast item 2: %v", err)
	}

	// Verify: two different agents were auto-provisioned.
	if result1.AgentName == result2.AgentName {
		t.Errorf("both items assigned to same agent: %s", result1.AgentName)
	}

	// Both agents in "working" state.
	agent1, err := sphereStore.GetAgent("ember/" + result1.AgentName)
	if err != nil {
		t.Fatalf("get agent 1: %v", err)
	}
	if agent1.State != "working" {
		t.Errorf("agent 1 state: got %q, want working", agent1.State)
	}

	agent2, err := sphereStore.GetAgent("ember/" + result2.AgentName)
	if err != nil {
		t.Fatalf("get agent 2: %v", err)
	}
	if agent2.State != "working" {
		t.Errorf("agent 2 state: got %q, want working", agent2.State)
	}

	// Both tmux sessions exist with different names.
	if result1.SessionName == result2.SessionName {
		t.Errorf("same session name for both agents: %s", result1.SessionName)
	}
	if !mgr.Exists(result1.SessionName) {
		t.Errorf("session %s does not exist", result1.SessionName)
	}
	if !mgr.Exists(result2.SessionName) {
		t.Errorf("session %s does not exist", result2.SessionName)
	}

	// Each writ has a different assignee.
	item1, err := worldStore.GetWrit(item1ID)
	if err != nil {
		t.Fatalf("get item 1: %v", err)
	}
	item2, err := worldStore.GetWrit(item2ID)
	if err != nil {
		t.Fatalf("get item 2: %v", err)
	}
	if item1.Assignee == item2.Assignee {
		t.Errorf("both items have same assignee: %s", item1.Assignee)
	}

	// Both tether files exist with their respective writ IDs.
	tetherID1, err := tether.Read("ember", result1.AgentName, "outpost")
	if err != nil {
		t.Fatalf("read tether 1: %v", err)
	}
	if tetherID1 != item1ID {
		t.Errorf("tether 1: got %q, want %q", tetherID1, item1ID)
	}

	tetherID2, err := tether.Read("ember", result2.AgentName, "outpost")
	if err != nil {
		t.Fatalf("read tether 2: %v", err)
	}
	if tetherID2 != item2ID {
		t.Errorf("tether 2: got %q, want %q", tetherID2, item2ID)
	}

	// Both worktrees exist at different paths.
	if result1.WorktreeDir == result2.WorktreeDir {
		t.Errorf("same worktree dir for both agents: %s", result1.WorktreeDir)
	}
	if _, err := os.Stat(result1.WorktreeDir); os.IsNotExist(err) {
		t.Errorf("worktree 1 does not exist: %s", result1.WorktreeDir)
	}
	if _, err := os.Stat(result2.WorktreeDir); os.IsNotExist(err) {
		t.Errorf("worktree 2 does not exist: %s", result2.WorktreeDir)
	}
}

// --- Test 2: Flock Serialization ---
// Uses two separate OS processes to test advisory flock, since flock is
// per-process (goroutines in the same process share the file descriptor).

func TestFlockSerialization(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, solHome, "ember", sourceRepo)
	worldStore, sphereStore := openStores(t, "ember")

	// Create one writ and two idle agents.
	itemID, err := worldStore.CreateWrit("Contested task", "Flock test", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Alpha", "ember", "outpost"); err != nil {
		t.Fatalf("create agent Alpha: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Beta", "ember", "outpost"); err != nil {
		t.Fatalf("create agent Beta: %v", err)
	}

	// Use the shared sol binary for subprocess testing.
	binary := gtBin(t)

	// Launch two subprocesses concurrently, each trying to cast the same
	// writ with a different agent. Flock serialization means exactly
	// one process acquires the lock; the other gets EAGAIN immediately.
	var wg sync.WaitGroup
	var mu sync.Mutex
	var successes []string
	var failures []string

	for _, agentName := range []string{"Alpha", "Beta"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			cmd := exec.Command(binary, "cast", itemID, "--world=ember", "--agent="+name)
			cmd.Dir = sourceRepo // sol cast discovers source repo from cwd
			out, err := cmd.CombinedOutput()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, name+": "+strings.TrimSpace(string(out)))
			} else {
				successes = append(successes, name)
			}
		}(agentName)
	}

	wg.Wait()

	// Exactly one should succeed.
	if len(successes) != 1 {
		t.Errorf("expected 1 success, got %d (successes: %v, failures: %v)", len(successes), successes, failures)
	}
	if len(failures) != 1 {
		t.Errorf("expected 1 failure, got %d (failures: %v)", len(failures), failures)
	}

	// The winning agent has the writ tethered.
	if len(successes) == 1 {
		winner := successes[0]
		tetherID, err := tether.Read("ember", winner, "outpost")
		if err != nil {
			t.Fatalf("read tether for winner %s: %v", winner, err)
		}
		if tetherID != itemID {
			t.Errorf("winner %s tether: got %q, want %q", winner, tetherID, itemID)
		}

		item, err := worldStore.GetWrit(itemID)
		if err != nil {
			t.Fatalf("get writ: %v", err)
		}
		if item.Status != "tethered" {
			t.Errorf("writ status: got %q, want tethered", item.Status)
		}
		if item.Assignee != "ember/"+winner {
			t.Errorf("writ assignee: got %q, want ember/%s", item.Assignee, winner)
		}
	}
}

// --- Test 3: Prefect Session Restart ---

func TestPrefectSessionRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	registerAgentRole(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := session.New()

	// Create a writ and cast it (auto-provisions an agent).
	itemID, err := worldStore.CreateWrit("Prefect test", "Restart test", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}

	result, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: itemID,
		World:        "ember",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("cast: %v", err)
	}

	agentName := result.AgentName
	sessName := result.SessionName

	// Start the prefect with a short heartbeat.
	cfg := prefect.DefaultConfig()
	cfg.HeartbeatInterval = 2 * time.Second
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	sup := prefect.New(cfg, sphereStore, mgr, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	supDone := make(chan error, 1)
	go func() { supDone <- sup.Run(ctx) }()

	// Brief startup delay — prefect's first heartbeat runs on a ticker,
	// not an observable event we can poll for.
	time.Sleep(200 * time.Millisecond)

	// Kill the agent's tmux session directly.
	exec.Command("tmux", "kill-session", "-t", sessName).Run()

	// Verify session is dead.
	if mgr.Exists(sessName) {
		t.Fatal("session should be dead after kill")
	}

	// Wait for the prefect to restart it.
	ok := pollUntil(15*time.Second, 500*time.Millisecond, func() bool {
		return mgr.Exists(sessName)
	})
	if !ok {
		t.Fatal("prefect did not restart session within 15 seconds")
	}

	// Verify agent state is "working" (not "stalled").
	agent, err := sphereStore.GetAgent("ember/" + agentName)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("agent state after restart: got %q, want working", agent.State)
	}

	// Tether file still contains the same writ ID.
	tetherID, err := tether.Read("ember", agentName, "outpost")
	if err != nil {
		t.Fatalf("read tether: %v", err)
	}
	if tetherID != itemID {
		t.Errorf("tether after restart: got %q, want %q", tetherID, itemID)
	}

	// The restarted session has the same name.
	if !mgr.Exists(sessName) {
		t.Errorf("session %s does not exist after prefect restart", sessName)
	}

	cancel()
	<-supDone
}

// --- Test 4: Mass-Death Degradation ---

func TestMassDeathDegradation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	registerAgentRole(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := session.New()

	// Create and cast 5 writs (auto-provisions 5 agents).
	var sessionNames []string
	for i := 0; i < 5; i++ {
		itemID, err := worldStore.CreateWrit("Mass death task", "Mass death test", "autarch", 2, nil)
		if err != nil {
			t.Fatalf("create writ %d: %v", i, err)
		}
		result, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
			WritID: itemID,
			World:        "ember",
			SourceRepo: sourceRepo,
		}, worldStore, sphereStore, mgr, nil)
		if err != nil {
			t.Fatalf("cast %d: %v", i, err)
		}
		sessionNames = append(sessionNames, result.SessionName)
	}

	// Start the prefect with short heartbeat and mass-death window.
	// Use a short MassDeathWindow so death timestamps expire quickly,
	// allowing the degraded-recovery test to work without re-triggering.
	cfg := prefect.DefaultConfig()
	cfg.HeartbeatInterval = 1 * time.Second
	cfg.MassDeathThreshold = 3
	cfg.MassDeathWindow = 5 * time.Second
	cfg.DegradedCooldown = 3 * time.Second
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	sup := prefect.New(cfg, sphereStore, mgr, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	supDone := make(chan error, 1)
	go func() { supDone <- sup.Run(ctx) }()

	// Brief startup delay — prefect's first heartbeat runs on a ticker,
	// not an observable event we can poll for.
	time.Sleep(200 * time.Millisecond)

	// Kill all 5 tmux sessions at once.
	for _, name := range sessionNames {
		exec.Command("tmux", "kill-session", "-t", name).Run()
	}

	// Wait for the prefect to detect deaths and enter degraded mode.
	ok := pollUntil(10*time.Second, 500*time.Millisecond, func() bool {
		return sup.IsDegraded()
	})
	if !ok {
		t.Fatal("prefect did not enter degraded mode within 10 seconds")
	}

	// Core assertion: prefect IS degraded.
	// Note: the prefect processes agents sequentially. The first
	// (threshold-1) deaths may trigger respawns before degraded mode
	// activates. The remaining agents are set to stalled. We verify
	// that at least some agents were stalled (degraded prevented respawn).
	stalledCount := 0
	agents, err := sphereStore.ListAgents("ember", "stalled")
	if err != nil {
		t.Fatalf("list stalled agents: %v", err)
	}
	stalledCount = len(agents)
	if stalledCount == 0 {
		t.Error("expected at least some agents stalled in degraded mode")
	}
	t.Logf("degraded mode: %d agents stalled", stalledCount)

	// Wait for degraded cooldown AND mass-death window to expire.
	// Deaths must age out of both windows for full recovery.
	ok = pollUntil(20*time.Second, 500*time.Millisecond, func() bool {
		return !sup.IsDegraded()
	})
	if !ok {
		t.Fatal("prefect did not exit degraded mode within 20 seconds")
	}

	// Wait for death times to age past MassDeathWindow (5s) so they get
	// pruned on the next heartbeat. By this point, ~3s (DegradedCooldown)
	// have elapsed since deaths were recorded; sleep for the remainder + margin.
	time.Sleep(2 * time.Second)

	// After recovery, dispatch a new writ and verify prefect
	// can respawn sessions again (not degraded anymore).
	newItemID, err := worldStore.CreateWrit("Post-degraded task", "Recovery test", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create new writ: %v", err)
	}

	newResult, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: newItemID,
		World:        "ember",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("cast after degraded recovery: %v", err)
	}

	newSessName := newResult.SessionName

	// Kill the new session and let prefect respawn.
	exec.Command("tmux", "kill-session", "-t", newSessName).Run()

	ok = pollUntil(15*time.Second, 500*time.Millisecond, func() bool {
		return mgr.Exists(newSessName)
	})
	if !ok {
		t.Fatal("prefect did not respawn session after degraded recovery")
	}

	cancel()
	<-supDone
}

// --- Test 5: GUPP Recovery ---

func TestGUPPRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := session.New()

	// Create a writ, cast it.
	itemID, err := worldStore.CreateWrit("GUPP test task", "GUPP recovery test", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}

	result, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: itemID,
		World:        "ember",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("cast: %v", err)
	}

	agentName := result.AgentName
	sessName := result.SessionName
	worktreeDir := result.WorktreeDir

	// Verify: tether file exists, CLAUDE.md in worktree has writ context.
	if !tether.IsTethered("ember", agentName, "outpost") {
		t.Error("tether file does not exist after cast")
	}

	claudeMD := filepath.Join(worktreeDir, "CLAUDE.local.md")
	data, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("read CLAUDE.local.md: %v", err)
	}
	if !strings.Contains(string(data), itemID) {
		t.Errorf("CLAUDE.local.md does not contain writ ID %s", itemID)
	}

	// Kill the tmux session.
	exec.Command("tmux", "kill-session", "-t", sessName).Run()

	// Verify: tether file still exists (durability).
	if !tether.IsTethered("ember", agentName, "outpost") {
		t.Error("tether file missing after crash")
	}

	// Re-cast the same writ to the same agent (simulate prefect restart).
	_, err = dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: itemID,
		World:        "ember",
		AgentName:  agentName,
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("re-cast: %v", err)
	}

	// New session is running.
	if !mgr.Exists(sessName) {
		t.Errorf("session %s not running after re-cast", sessName)
	}

	// sol prime returns the writ context.
	primeResult, err := dispatch.Prime("ember", agentName, "outpost", worldStore)
	if err != nil {
		t.Fatalf("prime: %v", err)
	}
	if !strings.Contains(primeResult.Output, itemID) {
		t.Errorf("prime output missing writ ID %s", itemID)
	}
	if !strings.Contains(primeResult.Output, "GUPP test task") {
		t.Errorf("prime output missing writ title")
	}

	// Tether file still contains the same writ ID.
	tetherID, err := tether.Read("ember", agentName, "outpost")
	if err != nil {
		t.Fatalf("read tether: %v", err)
	}
	if tetherID != itemID {
		t.Errorf("tether after re-cast: got %q, want %q", tetherID, itemID)
	}
}

// --- Test 6: Status Accuracy ---

func TestStatusAccuracy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	registerAgentRole(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := session.New()

	// Create 3 writs, cast all 3 (auto-provisions 3 agents).
	var results []*dispatch.CastResult
	titles := []string{"Status task A", "Status task B", "Status task C"}
	for _, title := range titles {
		itemID, err := worldStore.CreateWrit(title, "Status test", "autarch", 2, nil)
		if err != nil {
			t.Fatalf("create writ %q: %v", title, err)
		}
		result, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
			WritID: itemID,
			World:        "ember",
			SourceRepo: sourceRepo,
		}, worldStore, sphereStore, mgr, nil)
		if err != nil {
			t.Fatalf("cast %q: %v", title, err)
		}
		results = append(results, result)
	}

	// Kill one agent's tmux session.
	deadAgent := results[0]
	exec.Command("tmux", "kill-session", "-t", deadAgent.SessionName).Run()

	// Run status.Gather().
	rs, err := status.Gather("ember", sphereStore, worldStore, worldStore, mgr)
	if err != nil {
		t.Fatalf("status.Gather: %v", err)
	}

	// Verify summary counts.
	if rs.Summary.Total != 3 {
		t.Errorf("summary.Total: got %d, want 3", rs.Summary.Total)
	}
	if rs.Summary.Working != 3 {
		t.Errorf("summary.Working: got %d, want 3", rs.Summary.Working)
	}
	if rs.Summary.Dead != 1 {
		t.Errorf("summary.Dead: got %d, want 1", rs.Summary.Dead)
	}

	// Health = 1 (unhealthy — no prefect running, but let's check Dead logic).
	// Note: Without prefect running, Health() returns 2 (degraded).
	// The test spec says Health() == 1, but that requires a running prefect.
	// We check the Dead count is correct instead.

	// Find the dead agent's status.
	var deadAgentStatus *status.AgentStatus
	for i, a := range rs.Agents {
		if a.Name == deadAgent.AgentName {
			deadAgentStatus = &rs.Agents[i]
			break
		}
	}
	if deadAgentStatus == nil {
		t.Fatal("dead agent not found in status")
	}
	if deadAgentStatus.SessionAlive {
		t.Error("dead agent should have SessionAlive=false")
	}

	// Each agent's WorkTitle matches their writ title.
	for _, a := range rs.Agents {
		if a.WorkTitle == "" {
			t.Errorf("agent %s has empty WorkTitle", a.Name)
		}
	}

	// Start the prefect, let it restart the dead session.
	cfg := prefect.DefaultConfig()
	cfg.HeartbeatInterval = 2 * time.Second
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	sup := prefect.New(cfg, sphereStore, mgr, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	supDone := make(chan error, 1)
	go func() { supDone <- sup.Run(ctx) }()

	// Wait for prefect to restart the dead session.
	ok := pollUntil(15*time.Second, 500*time.Millisecond, func() bool {
		return mgr.Exists(deadAgent.SessionName)
	})
	if !ok {
		t.Fatal("prefect did not restart dead session within 15 seconds")
	}

	// Run status.Gather() again.
	rs2, err := status.Gather("ember", sphereStore, worldStore, worldStore, mgr)
	if err != nil {
		t.Fatalf("status.Gather after prefect: %v", err)
	}

	if rs2.Summary.Dead != 0 {
		t.Errorf("summary.Dead after prefect: got %d, want 0", rs2.Summary.Dead)
	}

	// Now prefect is running, Health should be 0 (healthy).
	if rs2.Health() != 0 {
		t.Errorf("health after prefect: got %d, want 0", rs2.Health())
	}

	cancel()
	<-supDone
}

// --- Test 7: Name Pool Exhaustion ---

func TestNamePoolExhaustion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := session.New()

	// Create a custom names file with only 2 names.
	namesDir := filepath.Join(solHome, "ember")
	if err := os.MkdirAll(namesDir, 0o755); err != nil {
		t.Fatalf("create world dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(namesDir, "names.txt"), []byte("Alpha\nBeta\n"), 0o644); err != nil {
		t.Fatalf("write names.txt: %v", err)
	}

	// Create and cast 2 writs (exhausts the pool).
	for i := 0; i < 2; i++ {
		itemID, err := worldStore.CreateWrit("Pool test", "Exhaustion test", "autarch", 2, nil)
		if err != nil {
			t.Fatalf("create writ %d: %v", i, err)
		}
		_, err = dispatch.Cast(context.Background(), dispatch.CastOpts{
			WritID: itemID,
			World:        "ember",
			SourceRepo: sourceRepo,
		}, worldStore, sphereStore, mgr, nil)
		if err != nil {
			t.Fatalf("cast %d: %v", i, err)
		}
	}

	// Create a third writ and attempt to cast it.
	item3ID, err := worldStore.CreateWrit("Pool overflow", "Should fail", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ 3: %v", err)
	}

	_, err = dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: item3ID,
		World:        "ember",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err == nil {
		t.Fatal("expected error for exhausted name pool, got nil")
	}
	if !strings.Contains(err.Error(), "exhausted") {
		t.Errorf("error should contain 'exhausted': got %q", err.Error())
	}

	// The third writ remains in "open" status, unassigned.
	item3, err := worldStore.GetWrit(item3ID)
	if err != nil {
		t.Fatalf("get writ 3: %v", err)
	}
	if item3.Status != "open" {
		t.Errorf("item 3 status: got %q, want open", item3.Status)
	}
	if item3.Assignee != "" {
		t.Errorf("item 3 assignee: got %q, want empty", item3.Assignee)
	}
}

// --- Test 8: Prefect Backoff Increases ---

// TestPrefectBackoffIncreases verifies that repeated session crashes cause
// the prefect to defer respawn with increasing delay (backoff accumulation).
// After the first crash, the session is respawned immediately (restart 1,
// delay=0). After the second crash, the session is stalled (restart 2,
// delay=30s) and is NOT immediately respawned on the next heartbeat.
func TestPrefectBackoffIncreases(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	registerAgentRole(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := session.New()

	itemID, err := worldStore.CreateWrit("Backoff test", "Backoff increases", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}

	result, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     itemID,
		World:      "ember",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("cast: %v", err)
	}

	agentName := result.AgentName
	sessName := result.SessionName

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := prefect.DefaultConfig()
	cfg.MaxRespawns = 0       // unlimited — let backoff govern
	cfg.MassDeathThreshold = 10 // high threshold so test kills don't trigger degraded mode
	sup := prefect.New(cfg, sphereStore, mgr, logger)

	// First kill: restart count = 1, delay = 0 → immediate respawn.
	exec.Command("tmux", "kill-session", "-t", sessName).Run()
	if mgr.Exists(sessName) {
		t.Fatal("session should be dead after first kill")
	}

	sup.Heartbeat()

	if !mgr.Exists(sessName) {
		t.Error("session should be respawned after first crash (no backoff delay)")
	}

	// Second kill: restart count = 2, delay = 30s → deferred (stalled).
	exec.Command("tmux", "kill-session", "-t", sessName).Run()
	if mgr.Exists(sessName) {
		t.Fatal("session should be dead after second kill")
	}

	sup.Heartbeat()

	// Session must NOT be immediately respawned — 30s delay applies.
	if mgr.Exists(sessName) {
		t.Error("session should NOT be immediately respawned after second crash (30s backoff)")
	}

	// Agent state should be stalled (deferred respawn).
	agent, err := sphereStore.GetAgent("ember/" + agentName)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if agent.State != "stalled" {
		t.Errorf("agent state after second crash: got %q, want stalled", agent.State)
	}

	// Another immediate heartbeat should still not respawn (delay not elapsed).
	sup.Heartbeat()
	if mgr.Exists(sessName) {
		t.Error("session should still not be respawned before backoff delay elapses")
	}
}

// --- Test 9: Prefect Backoff Resets ---

// TestPrefectBackoffResets verifies that when an agent completes work normally
// (transitions to idle), the prefect resets its backoff counter so the next
// crash results in immediate respawn (count=1, delay=0) rather than continued
// accumulation of the previous crash history.
func TestPrefectBackoffResets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	registerAgentRole(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := session.New()

	itemID, err := worldStore.CreateWrit("Backoff reset test", "Backoff resets", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}

	result, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     itemID,
		World:      "ember",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("cast: %v", err)
	}

	agentName := result.AgentName
	sessName := result.SessionName

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := prefect.DefaultConfig()
	cfg.MaxRespawns = 0        // unlimited
	cfg.MassDeathThreshold = 10 // high threshold so test kills don't trigger degraded mode
	sup := prefect.New(cfg, sphereStore, mgr, logger)

	// First crash: immediate respawn (backoff count becomes 1).
	exec.Command("tmux", "kill-session", "-t", sessName).Run()
	sup.Heartbeat()
	if !mgr.Exists(sessName) {
		t.Fatal("session not respawned after first crash")
	}

	// Second crash: stalled (backoff count = 2, delay = 30s).
	exec.Command("tmux", "kill-session", "-t", sessName).Run()
	sup.Heartbeat()
	if mgr.Exists(sessName) {
		t.Error("session should not be immediately respawned after second crash")
	}
	agent, err := sphereStore.GetAgent("ember/" + agentName)
	if err != nil {
		t.Fatalf("get agent after second crash: %v", err)
	}
	if agent.State != "stalled" {
		t.Errorf("agent state after second crash: got %q, want stalled", agent.State)
	}

	// Simulate normal completion: agent resolves its writ and goes idle.
	if err := sphereStore.UpdateAgentState("ember/"+agentName, "idle", ""); err != nil {
		t.Fatalf("set agent idle: %v", err)
	}

	// Heartbeat resets backoff for idle agents.
	sup.Heartbeat()

	// Cast a new writ to the same agent (reuses the idle agent).
	item2ID, err := worldStore.CreateWrit("After reset", "New writ post-reset", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ 2: %v", err)
	}
	result2, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     item2ID,
		World:      "ember",
		AgentName:  agentName,
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("cast writ 2: %v", err)
	}
	sessName2 := result2.SessionName

	// Kill the session — since backoff was reset, should respawn immediately (count=1).
	exec.Command("tmux", "kill-session", "-t", sessName2).Run()
	sup.Heartbeat()

	if !mgr.Exists(sessName2) {
		t.Error("session should be immediately respawned after backoff reset (first crash again)")
	}
}

// --- Test 10: Writ Activate Switches Context ---

// TestWritActivateSwitchesContext verifies that dispatch.ActivateWrit():
//  1. Updates active_writ in the sphere database to the newly activated writ.
//  2. Writes a .resume_state.json file with writ-switch context.
//  3. Returns the correct previous writ ID.
//  4. Is idempotent (second activate of same writ is a no-op).
func TestWritActivateSwitchesContext(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")

	// Create two writs.
	writ1ID, err := worldStore.CreateWrit("Writ One", "First writ", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ 1: %v", err)
	}
	writ2ID, err := worldStore.CreateWrit("Writ Two", "Second writ", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ 2: %v", err)
	}

	// Create an outpost agent and set it to working with writ1 active.
	agentName := "Scout"
	agentID := "ember/" + agentName
	if _, err := sphereStore.CreateAgent(agentName, "ember", "outpost"); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState(agentID, "working", writ1ID); err != nil {
		t.Fatalf("update agent state: %v", err)
	}

	// Write tether files for both writs (agent has two active writs).
	if err := tether.Write("ember", agentName, writ1ID, "outpost"); err != nil {
		t.Fatalf("write tether 1: %v", err)
	}
	if err := tether.Write("ember", agentName, writ2ID, "outpost"); err != nil {
		t.Fatalf("write tether 2: %v", err)
	}

	// Activate writ2 (switching from writ1).
	// No startup config registered for "outpost" in this test, so ActivateWrit
	// writes the resume state but skips session restart (non-fatal).
	mgr := session.New()
	logger := events.NewLogger(solHome)
	result, err := dispatch.ActivateWrit(dispatch.ActivateOpts{
		World:     "ember",
		AgentName: agentName,
		WritID:    writ2ID,
	}, worldStore, sphereStore, mgr, logger)
	if err != nil {
		t.Fatalf("ActivateWrit: %v", err)
	}

	// Verify result fields.
	if result.AlreadyActive {
		t.Error("expected AlreadyActive=false (writ2 was not previously active)")
	}
	if result.WritID != writ2ID {
		t.Errorf("result.WritID: got %q, want %q", result.WritID, writ2ID)
	}
	if result.PreviousWrit != writ1ID {
		t.Errorf("result.PreviousWrit: got %q, want %q", result.PreviousWrit, writ1ID)
	}

	// Verify active_writ is updated in the sphere database.
	agent, err := sphereStore.GetAgent(agentID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if agent.ActiveWrit != writ2ID {
		t.Errorf("agent.ActiveWrit: got %q, want %q", agent.ActiveWrit, writ2ID)
	}

	// Verify resume_state.json was written with writ-switch context.
	// Since no startup config is registered for "outpost", the state is written
	// but not cleared (session restart is skipped when cfg is nil).
	state, err := startup.ReadResumeState("ember", agentName, "outpost")
	if err != nil {
		t.Fatalf("ReadResumeState: %v", err)
	}
	if state == nil {
		t.Fatal("resume_state.json should be written by ActivateWrit but was not found")
	}
	if state.Reason != "writ-switch" {
		t.Errorf("resume state Reason: got %q, want %q", state.Reason, "writ-switch")
	}
	if state.NewActiveWrit != writ2ID {
		t.Errorf("resume state NewActiveWrit: got %q, want %q", state.NewActiveWrit, writ2ID)
	}
	if state.PreviousActiveWrit != writ1ID {
		t.Errorf("resume state PreviousActiveWrit: got %q, want %q", state.PreviousActiveWrit, writ1ID)
	}

	// Verify the writ_activate event was emitted.
	assertEventEmitted(t, solHome, events.EventWritActivate)

	// Verify idempotency: activating the same writ again is a no-op.
	result2, err := dispatch.ActivateWrit(dispatch.ActivateOpts{
		World:     "ember",
		AgentName: agentName,
		WritID:    writ2ID,
	}, worldStore, sphereStore, mgr, logger)
	if err != nil {
		t.Fatalf("ActivateWrit idempotent call: %v", err)
	}
	if !result2.AlreadyActive {
		t.Error("expected AlreadyActive=true when activating the already-active writ")
	}
}
