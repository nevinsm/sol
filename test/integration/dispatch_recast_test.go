package integration

// dispatch_recast_test.go — integration tests for Cast rollback on re-cast failure.
//
// Covers CD-2 / V9: when a re-cast (crash-recovery) attempt fails after the
// state-update steps, rollback must restore the pre-existing valid binding
// rather than wipe it with hardcoded "open"/"idle" defaults.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/tether"
)

// failOnStartMgr is a session.SessionManager that always fails on Start.
// Used to simulate a tmux hiccup during Cast's Launch phase while all other
// session operations (Stop, Exists, etc.) succeed.
type failOnStartMgr struct{}

func (f *failOnStartMgr) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	return errors.New("simulated tmux start failure")
}
func (f *failOnStartMgr) Stop(name string, force bool) error                                   { return nil }
func (f *failOnStartMgr) Exists(name string) bool                                              { return false }
func (f *failOnStartMgr) Inject(name string, text string, submit bool) error                   { return nil }
func (f *failOnStartMgr) Capture(name string, lines int) (string, error)                       { return "", nil }
func (f *failOnStartMgr) Cycle(name, workdir, cmd string, env map[string]string, role, world string) error {
	return nil
}
func (f *failOnStartMgr) NudgeSession(name string, message string) error { return nil }
func (f *failOnStartMgr) WaitForIdle(name string, timeout time.Duration) error {
	return nil
}
func (f *failOnStartMgr) CountSessions(prefix string) (int, error) { return 0, nil }

// Compile-time check: *failOnStartMgr implements session.SessionManager.
var _ session.SessionManager = (*failOnStartMgr)(nil)

// TestReCastRollbackPreservesBinding covers the failure scenario documented
// in CD-2 / V9: when a re-cast attempt fails after tether.Write (e.g. a tmux
// hiccup during Launch), rollback must preserve the pre-existing valid binding.
//
// Pre-state:  writ=tethered, agent=working+writA, tether=writA (all consistent)
// Trigger:    re-cast → Launch fails after agentUpdated+tetherWritten+writUpdated
// Expected:   writ STILL tethered, tether file STILL present, agent STILL working+writA
func TestReCastRollbackPreservesBinding(t *testing.T) {
	skipUnlessIntegration(t)

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")

	const (
		agentName = "Toast"
		world     = "ember"
	)

	// Phase 1: Set up a valid initial binding via a successful cast.
	// Uses the mock session checker (succeeds on Start) to avoid real tmux sessions.
	if _, err := sphereStore.CreateAgent(agentName, world, "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	writID, err := worldStore.CreateWrit("Re-cast rollback test", "Test CD-2 fix", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	successMgr := newMockSessionChecker()
	_, err = dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     writID,
		World:      world,
		AgentName:  agentName,
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, successMgr, nil)
	if err != nil {
		t.Fatalf("initial cast: %v", err)
	}

	// Verify the initial binding is fully in place.
	item, err := worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("GetWrit (pre-recast): %v", err)
	}
	if item.Status != "tethered" {
		t.Fatalf("pre-recast writ status: got %q, want \"tethered\"", item.Status)
	}
	agentRec, err := sphereStore.GetAgent(world + "/" + agentName)
	if err != nil {
		t.Fatalf("GetAgent (pre-recast): %v", err)
	}
	if agentRec.State != "working" || agentRec.ActiveWrit != writID {
		t.Fatalf("pre-recast agent: state=%q activeWrit=%q, want working+%s", agentRec.State, agentRec.ActiveWrit, writID)
	}
	if !tether.IsTetheredTo(world, agentName, writID, "outpost") {
		t.Fatal("tether file missing after initial cast")
	}

	// Phase 2: Simulate outpost crash — the session is gone but the binding
	// remains (writ=tethered, agent=working+writA, tether=writA).
	// The mock session checker tracks the session as alive; for the re-cast we
	// use a separate failing manager that reports Exists=false (session is dead).

	// Phase 3: Re-cast using a session manager that fails on Start.
	// This exercises the rollback path after all three state-update steps
	// have already run (agentUpdated, tetherWritten, writUpdated all true).
	_, err = dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     writID,
		World:      world,
		AgentName:  agentName,
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, &failOnStartMgr{}, nil)
	if err == nil {
		t.Fatal("expected re-cast to fail (simulated tmux failure), but it succeeded")
	}

	// Phase 4: Verify rollback preserved the pre-existing valid binding.

	item, err = worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("GetWrit after rollback: %v", err)
	}
	if item.Status != "tethered" {
		t.Errorf("writ status after rollback: got %q, want \"tethered\" — rollback incorrectly opened the writ", item.Status)
	}
	if item.Assignee != world+"/"+agentName {
		t.Errorf("writ assignee after rollback: got %q, want %q — rollback incorrectly cleared assignee", item.Assignee, world+"/"+agentName)
	}

	if !tether.IsTetheredTo(world, agentName, writID, "outpost") {
		t.Error("tether file missing after rollback — rollback incorrectly cleared the pre-existing tether")
	}

	agentRec, err = sphereStore.GetAgent(world + "/" + agentName)
	if err != nil {
		t.Fatalf("GetAgent after rollback: %v", err)
	}
	if agentRec.State != "working" {
		t.Errorf("agent state after rollback: got %q, want \"working\" — rollback incorrectly idled the agent", agentRec.State)
	}
	if agentRec.ActiveWrit != writID {
		t.Errorf("agent active_writ after rollback: got %q, want %q — rollback incorrectly cleared active writ", agentRec.ActiveWrit, writID)
	}
}

// TestFreshCastRollbackResetsToIdle verifies that the fresh-cast path is
// unaffected: when an originally-idle agent's cast fails at Launch, rollback
// still produces idle/open/cleared (no regression from the re-cast fix).
func TestFreshCastRollbackResetsToIdle(t *testing.T) {
	skipUnlessIntegration(t)

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "casttest")

	const (
		agentName = "Finch"
		world     = "casttest"
	)

	if _, err := sphereStore.CreateAgent(agentName, world, "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	writID, err := worldStore.CreateWrit("Fresh cast rollback test", "Test fresh-cast path unaffected", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Verify agent is idle and writ is open before cast.
	agentRec, err := sphereStore.GetAgent(world + "/" + agentName)
	if err != nil {
		t.Fatalf("GetAgent (pre-cast): %v", err)
	}
	if agentRec.State != "idle" {
		t.Fatalf("pre-cast agent state: got %q, want \"idle\"", agentRec.State)
	}

	// Fresh cast with a failing session manager.
	_, err = dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     writID,
		World:      world,
		AgentName:  agentName,
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, &failOnStartMgr{}, nil)
	if err == nil {
		t.Fatal("expected cast to fail, but it succeeded")
	}

	// Rollback must restore original idle/open state.
	item, err := worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("GetWrit after rollback: %v", err)
	}
	if item.Status != "open" {
		t.Errorf("writ status after rollback: got %q, want \"open\"", item.Status)
	}
	if item.Assignee != "" {
		t.Errorf("writ assignee after rollback: got %q, want empty", item.Assignee)
	}

	if tether.IsTetheredTo(world, agentName, writID, "outpost") {
		t.Error("tether file present after rollback — should have been cleared for fresh-cast")
	}

	agentRec, err = sphereStore.GetAgent(world + "/" + agentName)
	if err != nil {
		t.Fatalf("GetAgent after rollback: %v", err)
	}
	if agentRec.State != "idle" {
		t.Errorf("agent state after rollback: got %q, want \"idle\"", agentRec.State)
	}
	if agentRec.ActiveWrit != "" {
		t.Errorf("agent active_writ after rollback: got %q, want empty", agentRec.ActiveWrit)
	}
}
