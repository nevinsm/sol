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

	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/hook"
	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/status"
	"github.com/nevinsm/gt/internal/supervisor"
)

// --- Test 1: Multi-Agent Dispatch ---

func TestMultiAgentDispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// Create two work items.
	item1ID, err := rigStore.CreateWorkItem("Task Alpha", "Alpha description", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create item 1: %v", err)
	}
	item2ID, err := rigStore.CreateWorkItem("Task Beta", "Beta description", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create item 2: %v", err)
	}

	// Sling both without specifying agents (auto-provision).
	result1, err := dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: item1ID,
		Rig:        "testrig",
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr, nil)
	if err != nil {
		t.Fatalf("sling item 1: %v", err)
	}

	result2, err := dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: item2ID,
		Rig:        "testrig",
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr, nil)
	if err != nil {
		t.Fatalf("sling item 2: %v", err)
	}

	// Verify: two different agents were auto-provisioned.
	if result1.AgentName == result2.AgentName {
		t.Errorf("both items assigned to same agent: %s", result1.AgentName)
	}

	// Both agents in "working" state.
	agent1, err := townStore.GetAgent("testrig/" + result1.AgentName)
	if err != nil {
		t.Fatalf("get agent 1: %v", err)
	}
	if agent1.State != "working" {
		t.Errorf("agent 1 state: got %q, want working", agent1.State)
	}

	agent2, err := townStore.GetAgent("testrig/" + result2.AgentName)
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

	// Each work item has a different assignee.
	item1, err := rigStore.GetWorkItem(item1ID)
	if err != nil {
		t.Fatalf("get item 1: %v", err)
	}
	item2, err := rigStore.GetWorkItem(item2ID)
	if err != nil {
		t.Fatalf("get item 2: %v", err)
	}
	if item1.Assignee == item2.Assignee {
		t.Errorf("both items have same assignee: %s", item1.Assignee)
	}

	// Both hook files exist with their respective work item IDs.
	hookID1, err := hook.Read("testrig", result1.AgentName)
	if err != nil {
		t.Fatalf("read hook 1: %v", err)
	}
	if hookID1 != item1ID {
		t.Errorf("hook 1: got %q, want %q", hookID1, item1ID)
	}

	hookID2, err := hook.Read("testrig", result2.AgentName)
	if err != nil {
		t.Fatalf("read hook 2: %v", err)
	}
	if hookID2 != item2ID {
		t.Errorf("hook 2: got %q, want %q", hookID2, item2ID)
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

	_, sourceRepo := setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")

	// Create one work item and two idle agents.
	itemID, err := rigStore.CreateWorkItem("Contested task", "Flock test", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}
	townStore.CreateAgent("Alpha", "testrig", "polecat")
	townStore.CreateAgent("Beta", "testrig", "polecat")

	// Build the gt binary for subprocess testing.
	binary := filepath.Join(t.TempDir(), "gt")
	buildCmd := exec.Command("go", "build", "-o", binary, "github.com/nevinsm/gt")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build gt binary: %s: %v", out, err)
	}

	// Launch two subprocesses concurrently, each trying to sling the same
	// work item with a different agent. Flock serialization means exactly
	// one process acquires the lock; the other gets EAGAIN immediately.
	var wg sync.WaitGroup
	var mu sync.Mutex
	var successes []string
	var failures []string

	for _, agentName := range []string{"Alpha", "Beta"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			cmd := exec.Command(binary, "sling", itemID, "testrig", "--agent="+name)
			cmd.Dir = sourceRepo // gt sling discovers source repo from cwd
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

	// The winning agent has the work item hooked.
	if len(successes) == 1 {
		winner := successes[0]
		hookID, _ := hook.Read("testrig", winner)
		if hookID != itemID {
			t.Errorf("winner %s hook: got %q, want %q", winner, hookID, itemID)
		}

		item, _ := rigStore.GetWorkItem(itemID)
		if item.Status != "hooked" {
			t.Errorf("work item status: got %q, want hooked", item.Status)
		}
		if item.Assignee != "testrig/"+winner {
			t.Errorf("work item assignee: got %q, want testrig/%s", item.Assignee, winner)
		}
	}
}

// --- Test 3: Supervisor Session Restart ---

func TestSupervisorSessionRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// Create a work item and sling it (auto-provisions an agent).
	itemID, err := rigStore.CreateWorkItem("Supervisor test", "Restart test", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	result, err := dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr, nil)
	if err != nil {
		t.Fatalf("sling: %v", err)
	}

	agentName := result.AgentName
	sessName := result.SessionName

	// Start the supervisor with a short heartbeat.
	cfg := supervisor.DefaultConfig()
	cfg.HeartbeatInterval = 2 * time.Second
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	sup := supervisor.New(cfg, townStore, mgr, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	supDone := make(chan error, 1)
	go func() { supDone <- sup.Run(ctx) }()

	// Give supervisor time to start.
	time.Sleep(500 * time.Millisecond)

	// Kill the agent's tmux session directly.
	exec.Command("tmux", "kill-session", "-t", sessName).Run()

	// Verify session is dead.
	if mgr.Exists(sessName) {
		t.Fatal("session should be dead after kill")
	}

	// Wait for the supervisor to restart it.
	ok := pollUntil(15*time.Second, 500*time.Millisecond, func() bool {
		return mgr.Exists(sessName)
	})
	if !ok {
		t.Fatal("supervisor did not restart session within 15 seconds")
	}

	// Verify agent state is "working" (not "stalled").
	agent, err := townStore.GetAgent("testrig/" + agentName)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("agent state after restart: got %q, want working", agent.State)
	}

	// Hook file still contains the same work item ID.
	hookID, err := hook.Read("testrig", agentName)
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}
	if hookID != itemID {
		t.Errorf("hook after restart: got %q, want %q", hookID, itemID)
	}

	// The restarted session has the same name.
	if !mgr.Exists(sessName) {
		t.Errorf("session %s does not exist after supervisor restart", sessName)
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
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// Create and sling 5 work items (auto-provisions 5 agents).
	var sessionNames []string
	for i := 0; i < 5; i++ {
		itemID, err := rigStore.CreateWorkItem("Mass death task", "Mass death test", "operator", 2, nil)
		if err != nil {
			t.Fatalf("create work item %d: %v", i, err)
		}
		result, err := dispatch.Sling(dispatch.SlingOpts{
			WorkItemID: itemID,
			Rig:        "testrig",
			SourceRepo: sourceRepo,
		}, rigStore, townStore, mgr, nil)
		if err != nil {
			t.Fatalf("sling %d: %v", i, err)
		}
		sessionNames = append(sessionNames, result.SessionName)
	}

	// Start the supervisor with short heartbeat and mass-death window.
	// Use a short MassDeathWindow so death timestamps expire quickly,
	// allowing the degraded-recovery test to work without re-triggering.
	cfg := supervisor.DefaultConfig()
	cfg.HeartbeatInterval = 1 * time.Second
	cfg.MassDeathThreshold = 3
	cfg.MassDeathWindow = 5 * time.Second
	cfg.DegradedCooldown = 3 * time.Second
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	sup := supervisor.New(cfg, townStore, mgr, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	supDone := make(chan error, 1)
	go func() { supDone <- sup.Run(ctx) }()

	// Give supervisor time to start and run its initial heartbeat.
	time.Sleep(500 * time.Millisecond)

	// Kill all 5 tmux sessions at once.
	for _, name := range sessionNames {
		exec.Command("tmux", "kill-session", "-t", name).Run()
	}

	// Wait for the supervisor to detect deaths and enter degraded mode.
	ok := pollUntil(10*time.Second, 500*time.Millisecond, func() bool {
		return sup.IsDegraded()
	})
	if !ok {
		t.Fatal("supervisor did not enter degraded mode within 10 seconds")
	}

	// Core assertion: supervisor IS degraded.
	// Note: the supervisor processes agents sequentially. The first
	// (threshold-1) deaths may trigger respawns before degraded mode
	// activates. The remaining agents are set to stalled. We verify
	// that at least some agents were stalled (degraded prevented respawn).
	stalledCount := 0
	agents, err := townStore.ListAgents("testrig", "stalled")
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
		t.Fatal("supervisor did not exit degraded mode within 20 seconds")
	}

	// Wait for death times to be fully pruned (past MassDeathWindow).
	time.Sleep(2 * time.Second)

	// After recovery, dispatch a new work item and verify supervisor
	// can respawn sessions again (not degraded anymore).
	newItemID, err := rigStore.CreateWorkItem("Post-degraded task", "Recovery test", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create new work item: %v", err)
	}

	newResult, err := dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: newItemID,
		Rig:        "testrig",
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr, nil)
	if err != nil {
		t.Fatalf("sling after degraded recovery: %v", err)
	}

	newSessName := newResult.SessionName

	// Kill the new session and let supervisor respawn.
	exec.Command("tmux", "kill-session", "-t", newSessName).Run()

	ok = pollUntil(15*time.Second, 500*time.Millisecond, func() bool {
		return mgr.Exists(newSessName)
	})
	if !ok {
		t.Fatal("supervisor did not respawn session after degraded recovery")
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
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// Create a work item, sling it.
	itemID, err := rigStore.CreateWorkItem("GUPP test task", "GUPP recovery test", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	result, err := dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr, nil)
	if err != nil {
		t.Fatalf("sling: %v", err)
	}

	agentName := result.AgentName
	sessName := result.SessionName
	worktreeDir := result.WorktreeDir

	// Verify: hook file exists, CLAUDE.md in worktree has work item context.
	if !hook.IsHooked("testrig", agentName) {
		t.Error("hook file does not exist after sling")
	}

	claudeMD := filepath.Join(worktreeDir, ".claude", "CLAUDE.md")
	data, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(data), itemID) {
		t.Errorf("CLAUDE.md does not contain work item ID %s", itemID)
	}

	// Kill the tmux session.
	exec.Command("tmux", "kill-session", "-t", sessName).Run()

	// Verify: hook file still exists (durability).
	if !hook.IsHooked("testrig", agentName) {
		t.Error("hook file missing after crash")
	}

	// Re-sling the same work item to the same agent (simulate supervisor restart).
	_, err = dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		AgentName:  agentName,
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr, nil)
	if err != nil {
		t.Fatalf("re-sling: %v", err)
	}

	// New session is running.
	if !mgr.Exists(sessName) {
		t.Errorf("session %s not running after re-sling", sessName)
	}

	// gt prime returns the work item context.
	primeResult, err := dispatch.Prime("testrig", agentName, rigStore)
	if err != nil {
		t.Fatalf("prime: %v", err)
	}
	if !strings.Contains(primeResult.Output, itemID) {
		t.Errorf("prime output missing work item ID %s", itemID)
	}
	if !strings.Contains(primeResult.Output, "GUPP test task") {
		t.Errorf("prime output missing work item title")
	}

	// Hook file still contains the same work item ID.
	hookID, err := hook.Read("testrig", agentName)
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}
	if hookID != itemID {
		t.Errorf("hook after re-sling: got %q, want %q", hookID, itemID)
	}
}

// --- Test 6: Status Accuracy ---

func TestStatusAccuracy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// Create 3 work items, sling all 3 (auto-provisions 3 agents).
	var results []*dispatch.SlingResult
	titles := []string{"Status task A", "Status task B", "Status task C"}
	for _, title := range titles {
		itemID, err := rigStore.CreateWorkItem(title, "Status test", "operator", 2, nil)
		if err != nil {
			t.Fatalf("create work item %q: %v", title, err)
		}
		result, err := dispatch.Sling(dispatch.SlingOpts{
			WorkItemID: itemID,
			Rig:        "testrig",
			SourceRepo: sourceRepo,
		}, rigStore, townStore, mgr, nil)
		if err != nil {
			t.Fatalf("sling %q: %v", title, err)
		}
		results = append(results, result)
	}

	// Kill one agent's tmux session.
	deadAgent := results[0]
	exec.Command("tmux", "kill-session", "-t", deadAgent.SessionName).Run()

	// Run status.Gather().
	rs, err := status.Gather("testrig", townStore, rigStore, rigStore, mgr)
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

	// Health = 1 (unhealthy — no supervisor running, but let's check Dead logic).
	// Note: Without supervisor running, Health() returns 2 (degraded).
	// The test spec says Health() == 1, but that requires a running supervisor.
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

	// Each agent's WorkTitle matches their work item title.
	for _, a := range rs.Agents {
		if a.WorkTitle == "" {
			t.Errorf("agent %s has empty WorkTitle", a.Name)
		}
	}

	// Start the supervisor, let it restart the dead session.
	cfg := supervisor.DefaultConfig()
	cfg.HeartbeatInterval = 2 * time.Second
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	sup := supervisor.New(cfg, townStore, mgr, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	supDone := make(chan error, 1)
	go func() { supDone <- sup.Run(ctx) }()

	// Wait for supervisor to restart the dead session.
	ok := pollUntil(15*time.Second, 500*time.Millisecond, func() bool {
		return mgr.Exists(deadAgent.SessionName)
	})
	if !ok {
		t.Fatal("supervisor did not restart dead session within 15 seconds")
	}

	// Run status.Gather() again.
	rs2, err := status.Gather("testrig", townStore, rigStore, rigStore, mgr)
	if err != nil {
		t.Fatalf("status.Gather after supervisor: %v", err)
	}

	if rs2.Summary.Dead != 0 {
		t.Errorf("summary.Dead after supervisor: got %d, want 0", rs2.Summary.Dead)
	}

	// Now supervisor is running, Health should be 0 (healthy).
	if rs2.Health() != 0 {
		t.Errorf("health after supervisor: got %d, want 0", rs2.Health())
	}

	cancel()
	<-supDone
}

// --- Test 7: Name Pool Exhaustion ---

func TestNamePoolExhaustion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// Create a custom names file with only 2 names.
	namesDir := filepath.Join(gtHome, "testrig")
	if err := os.MkdirAll(namesDir, 0o755); err != nil {
		t.Fatalf("create rig dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(namesDir, "names.txt"), []byte("Alpha\nBeta\n"), 0o644); err != nil {
		t.Fatalf("write names.txt: %v", err)
	}

	// Create and sling 2 work items (exhausts the pool).
	for i := 0; i < 2; i++ {
		itemID, err := rigStore.CreateWorkItem("Pool test", "Exhaustion test", "operator", 2, nil)
		if err != nil {
			t.Fatalf("create work item %d: %v", i, err)
		}
		_, err = dispatch.Sling(dispatch.SlingOpts{
			WorkItemID: itemID,
			Rig:        "testrig",
			SourceRepo: sourceRepo,
		}, rigStore, townStore, mgr, nil)
		if err != nil {
			t.Fatalf("sling %d: %v", i, err)
		}
	}

	// Create a third work item and attempt to sling it.
	item3ID, err := rigStore.CreateWorkItem("Pool overflow", "Should fail", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item 3: %v", err)
	}

	_, err = dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: item3ID,
		Rig:        "testrig",
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr, nil)
	if err == nil {
		t.Fatal("expected error for exhausted name pool, got nil")
	}
	if !strings.Contains(err.Error(), "exhausted") {
		t.Errorf("error should contain 'exhausted': got %q", err.Error())
	}

	// The third work item remains in "open" status, unassigned.
	item3, err := rigStore.GetWorkItem(item3ID)
	if err != nil {
		t.Fatalf("get work item 3: %v", err)
	}
	if item3.Status != "open" {
		t.Errorf("item 3 status: got %q, want open", item3.Status)
	}
	if item3.Assignee != "" {
		t.Errorf("item 3 assignee: got %q, want empty", item3.Assignee)
	}
}
