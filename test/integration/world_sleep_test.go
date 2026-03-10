package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

func TestWorldSleepForceStopsOutpostSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "sleeptest", sourceRepo)

	worldStore, sphereStore := openStores(t, "sleeptest")

	// Create an agent and a writ, simulate active work.
	_, err := sphereStore.CreateAgent("Toast", "sleeptest", "agent")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	writID, err := worldStore.CreateWrit("Test task", "description", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}

	// Set writ to tethered and assign to agent.
	if err := worldStore.UpdateWrit(writID, store.WritUpdates{
		Status:   "tethered",
		Assignee: "Toast",
	}); err != nil {
		t.Fatalf("update writ: %v", err)
	}

	// Set agent to working with active writ.
	if err := sphereStore.UpdateAgentState("sleeptest/Toast", "working", writID); err != nil {
		t.Fatalf("update agent state: %v", err)
	}

	// Create tether file.
	if err := tether.Write("sleeptest", "Toast", writID, "agent"); err != nil {
		t.Fatalf("write tether: %v", err)
	}

	// Start a tmux session to simulate the running agent.
	mgr := session.New()
	sessName := config.SessionName("sleeptest", "Toast")
	if err := mgr.Start(sessName, os.TempDir(), config.SessionCommand(), nil, "agent", "sleeptest"); err != nil {
		t.Fatalf("start session: %v", err)
	}

	// Verify session exists.
	if !mgr.Exists(sessName) {
		t.Fatal("session should exist before sleep")
	}

	// Run sol world sleep --force.
	out, err := runGT(t, gtHome, "world", "sleep", "--force", "sleeptest")
	if err != nil {
		t.Fatalf("world sleep --force failed: %v: %s", err, out)
	}

	// Verify output mentions agent stopped.
	if !strings.Contains(out, "stopped agent Toast") {
		t.Errorf("expected 'stopped agent Toast' in output: %s", out)
	}
	if !strings.Contains(out, "agent(s) stopped") {
		t.Errorf("expected 'agent(s) stopped' in output: %s", out)
	}

	// Verify session is gone.
	if mgr.Exists(sessName) {
		t.Error("session should not exist after force sleep")
	}

	// Verify writ returned to open with no assignee.
	writ, err := worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("get writ: %v", err)
	}
	if writ.Status != "open" {
		t.Errorf("expected writ status 'open', got %q", writ.Status)
	}
	if writ.Assignee != "" {
		t.Errorf("expected writ assignee cleared, got %q", writ.Assignee)
	}

	// Verify agent set to idle with no active writ.
	agent, err := sphereStore.GetAgent("sleeptest/Toast")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("expected agent state 'idle', got %q", agent.State)
	}
	if agent.ActiveWrit != "" {
		t.Errorf("expected agent active_writ cleared, got %q", agent.ActiveWrit)
	}

	// Verify tether cleared.
	if tether.IsTethered("sleeptest", "Toast", "agent") {
		t.Error("expected tether to be cleared")
	}

	// Verify world config is sleeping.
	cfg, err := config.LoadWorldConfig("sleeptest")
	if err != nil {
		t.Fatalf("load world config: %v", err)
	}
	if !cfg.World.Sleeping {
		t.Error("expected world to be sleeping")
	}
}

func TestWorldSleepForceWarnsEnvoys(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "sleeptest2", sourceRepo)

	_, sphereStore := openStores(t, "sleeptest2")

	// Create envoy agent.
	_, err := sphereStore.CreateAgent("Scout", "sleeptest2", "envoy")
	if err != nil {
		t.Fatalf("create envoy: %v", err)
	}

	// Set envoy to working state (simulating active envoy).
	if err := sphereStore.UpdateAgentState("sleeptest2/Scout", "working", ""); err != nil {
		t.Fatalf("update envoy state: %v", err)
	}

	// Create envoy directory for session.
	envoyDir := filepath.Join(gtHome, "sleeptest2", "envoys", "Scout")
	if err := os.MkdirAll(envoyDir, 0o755); err != nil {
		t.Fatalf("create envoy dir: %v", err)
	}

	// Start a tmux session for the envoy.
	mgr := session.New()
	sessName := config.SessionName("sleeptest2", "Scout")
	if err := mgr.Start(sessName, os.TempDir(), config.SessionCommand(), nil, "envoy", "sleeptest2"); err != nil {
		t.Fatalf("start envoy session: %v", err)
	}

	// Run sol world sleep --force.
	out, err := runGT(t, gtHome, "world", "sleep", "--force", "sleeptest2")
	if err != nil {
		t.Fatalf("world sleep --force failed: %v: %s", err, out)
	}

	// Verify output mentions envoy warned.
	if !strings.Contains(out, "warned envoy Scout") {
		t.Errorf("expected 'warned envoy Scout' in output: %s", out)
	}
	if !strings.Contains(out, "envoy(s) warned") {
		t.Errorf("expected 'envoy(s) warned' in output: %s", out)
	}

	// Verify envoy session is STILL RUNNING.
	if !mgr.Exists(sessName) {
		t.Error("envoy session should still exist after force sleep")
	}
}

func TestWorldSleepSoftReportsRunningAgents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "softtest", sourceRepo)

	_, sphereStore := openStores(t, "softtest")

	// Create agents — one working, one idle.
	_, err := sphereStore.CreateAgent("Alpha", "softtest", "agent")
	if err != nil {
		t.Fatalf("create agent Alpha: %v", err)
	}
	if err := sphereStore.UpdateAgentState("softtest/Alpha", "working", "sol-fake-writ"); err != nil {
		t.Fatalf("update agent state: %v", err)
	}

	_, err = sphereStore.CreateAgent("Beta", "softtest", "agent")
	if err != nil {
		t.Fatalf("create agent Beta: %v", err)
	}

	// Run soft sleep (no --force).
	out, err := runGT(t, gtHome, "world", "sleep", "softtest")
	if err != nil {
		t.Fatalf("world sleep failed: %v: %s", err, out)
	}

	// Verify output mentions running agent count.
	if !strings.Contains(out, "1 agent(s) still running") {
		t.Errorf("expected '1 agent(s) still running' in output: %s", out)
	}
}

func TestWorldSleepAlreadySleeping(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	initWorld(t, gtHome, "alreadysleeping")

	// Sleep once.
	out, err := runGT(t, gtHome, "world", "sleep", "alreadysleeping")
	if err != nil {
		t.Fatalf("first sleep failed: %v: %s", err, out)
	}

	// Sleep again — should report already sleeping.
	out, err = runGT(t, gtHome, "world", "sleep", "alreadysleeping")
	if err != nil {
		t.Fatalf("second sleep failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "already sleeping") {
		t.Errorf("expected 'already sleeping' message, got: %s", out)
	}
}

func TestWorldWakeVerifiesServices(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	isolateTmux(t)
	initWorld(t, gtHome, "wakeverify")

	// Sleep the world first.
	out, err := runGT(t, gtHome, "world", "sleep", "wakeverify")
	if err != nil {
		t.Fatalf("world sleep failed: %v: %s", err, out)
	}

	// Wake the world. Services will fail to start (no source repo for sentinel/forge)
	// but the command should still succeed (DEGRADE).
	out, err = runGT(t, gtHome, "world", "wake", "wakeverify")
	if err != nil {
		t.Fatalf("world wake failed: %v: %s", err, out)
	}

	// Verify output has the structured report format.
	if !strings.Contains(out, "is now active") {
		t.Errorf("expected 'is now active' in output: %s", out)
	}

	// Verify world config is no longer sleeping.
	t.Setenv("SOL_HOME", gtHome)
	cfg, err := config.LoadWorldConfig("wakeverify")
	if err != nil {
		t.Fatalf("load world config: %v", err)
	}
	if cfg.World.Sleeping {
		t.Error("expected world to not be sleeping after wake")
	}
}

func TestWorldWakeAlreadyActive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	initWorld(t, gtHome, "alreadyactive")

	// Wake without sleeping — should report already active.
	out, err := runGT(t, gtHome, "world", "wake", "alreadyactive")
	if err != nil {
		t.Fatalf("world wake failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "already active") {
		t.Errorf("expected 'already active' message, got: %s", out)
	}
}

func TestWorldSleepForceCrashRecoveryScenario(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "crashtest", sourceRepo)

	worldStore, sphereStore := openStores(t, "crashtest")

	// Simulate a partial force stop crash:
	// - Config says sleeping=true
	// - Agent still has working state and tether (cleanup didn't complete)
	cfg, err := config.LoadWorldConfig("crashtest")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.World.Sleeping = true
	if err := config.WriteWorldConfig("crashtest", cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Create agent in working state with a tether.
	_, err = sphereStore.CreateAgent("Orphan", "crashtest", "agent")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	writID, err := worldStore.CreateWrit("Orphan task", "desc", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}

	if err := worldStore.UpdateWrit(writID, store.WritUpdates{
		Status:   "tethered",
		Assignee: "Orphan",
	}); err != nil {
		t.Fatalf("update writ: %v", err)
	}

	if err := sphereStore.UpdateAgentState("crashtest/Orphan", "working", writID); err != nil {
		t.Fatalf("update agent: %v", err)
	}

	if err := tether.Write("crashtest", "Orphan", writID, "agent"); err != nil {
		t.Fatalf("write tether: %v", err)
	}

	// Verify the stale tether exists.
	if !tether.IsTethered("crashtest", "Orphan", "agent") {
		t.Fatal("expected tether to exist before crash scenario")
	}

	// Verify sleeping=true is already set (gates are active).
	cfg, err = config.LoadWorldConfig("crashtest")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.World.Sleeping {
		t.Fatal("expected sleeping=true in crash scenario")
	}

	// The key invariant: even with a partial crash, sleeping=true
	// means dispatch gates are active. The stale tether will be
	// recovered by consul on the next wake cycle. Verify that
	// wake can proceed even with stale state.
	out, err := runGT(t, gtHome, "world", "wake", "crashtest")
	if err != nil {
		t.Fatalf("world wake failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "is now active") {
		t.Errorf("expected wake to succeed, got: %s", out)
	}
}

func TestWorldSleepForceMultipleAgents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "multitest", sourceRepo)

	worldStore, sphereStore := openStores(t, "multitest")

	// Create 2 outpost agents and 1 envoy.
	for _, name := range []string{"Agent1", "Agent2"} {
		_, err := sphereStore.CreateAgent(name, "multitest", "agent")
		if err != nil {
			t.Fatalf("create agent %s: %v", name, err)
		}

		writID, err := worldStore.CreateWrit("Task for "+name, "desc", "autarch", 2, nil)
		if err != nil {
			t.Fatalf("create writ for %s: %v", name, err)
		}

		if err := worldStore.UpdateWrit(writID, store.WritUpdates{
			Status:   "tethered",
			Assignee: name,
		}); err != nil {
			t.Fatalf("update writ for %s: %v", name, err)
		}

		if err := sphereStore.UpdateAgentState("multitest/"+name, "working", writID); err != nil {
			t.Fatalf("update agent %s state: %v", name, err)
		}

		if err := tether.Write("multitest", name, writID, "agent"); err != nil {
			t.Fatalf("write tether for %s: %v", name, err)
		}

		// Start session.
		mgr := session.New()
		sessName := config.SessionName("multitest", name)
		if err := mgr.Start(sessName, os.TempDir(), config.SessionCommand(), nil, "agent", "multitest"); err != nil {
			t.Fatalf("start session for %s: %v", name, err)
		}
	}

	// Create envoy.
	_, err := sphereStore.CreateAgent("Scout", "multitest", "envoy")
	if err != nil {
		t.Fatalf("create envoy: %v", err)
	}

	envoyDir := filepath.Join(gtHome, "multitest", "envoys", "Scout")
	os.MkdirAll(envoyDir, 0o755)

	mgr := session.New()
	envoySess := config.SessionName("multitest", "Scout")
	if err := mgr.Start(envoySess, os.TempDir(), config.SessionCommand(), nil, "envoy", "multitest"); err != nil {
		t.Fatalf("start envoy session: %v", err)
	}

	// Run force sleep.
	out, err := runGT(t, gtHome, "world", "sleep", "--force", "multitest")
	if err != nil {
		t.Fatalf("world sleep --force failed: %v: %s", err, out)
	}

	// Verify output.
	if !strings.Contains(out, "2 agent(s) stopped") {
		t.Errorf("expected '2 agent(s) stopped' in output: %s", out)
	}
	if !strings.Contains(out, "1 envoy(s) warned") {
		t.Errorf("expected '1 envoy(s) warned' in output: %s", out)
	}

	// Verify all agent sessions gone, envoy still running.
	if mgr.Exists(config.SessionName("multitest", "Agent1")) {
		t.Error("Agent1 session should not exist after force sleep")
	}
	if mgr.Exists(config.SessionName("multitest", "Agent2")) {
		t.Error("Agent2 session should not exist after force sleep")
	}
	if !mgr.Exists(envoySess) {
		t.Error("envoy session should still exist after force sleep")
	}

	// Verify all writs returned to open.
	writs, err := worldStore.ListWrits(store.ListFilters{Status: "open"})
	if err != nil {
		t.Fatalf("list writs: %v", err)
	}
	if len(writs) != 2 {
		t.Errorf("expected 2 open writs, got %d", len(writs))
	}

	// Verify both agents are idle.
	for _, name := range []string{"Agent1", "Agent2"} {
		agent, err := sphereStore.GetAgent("multitest/" + name)
		if err != nil {
			t.Fatalf("get agent %s: %v", name, err)
		}
		if agent.State != "idle" {
			t.Errorf("expected agent %s state 'idle', got %q", name, agent.State)
		}
		if agent.ActiveWrit != "" {
			t.Errorf("expected agent %s active_writ cleared, got %q", name, agent.ActiveWrit)
		}
	}
}
