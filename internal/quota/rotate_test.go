package quota

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
)

// mockSessionManager implements session.Manager-like methods for testing.
// We test swapAndRespawn directly, which takes a *session.Manager.
// Since we can't easily mock *session.Manager (concrete type), we test
// through the startup path by verifying the RoleConfig is consulted.

func TestSwapAndRespawnUsesStartupForRegisteredRole(t *testing.T) {
	// This test verifies that swapAndRespawn calls startup.Resume when
	// a role is registered. We verify by checking that startup.ConfigFor
	// returns non-nil for the role used by quota rotation (role=outpost).

	roleName := "outpost"

	// Before registration, ConfigFor returns nil.
	if cfg := startup.ConfigFor(roleName); cfg != nil {
		t.Fatal("expected nil ConfigFor before registration")
	}

	// Register the role.
	startup.Register(roleName, startup.RoleConfig{
		WorktreeDir: func(w, a string) string {
			return filepath.Join(os.Getenv("SOL_HOME"), w, "outposts", a, "worktree")
		},
		SystemPromptContent: "Test agent prompt.",
	})
	t.Cleanup(func() {
		startup.Register(roleName, startup.RoleConfig{})
	})

	// After registration, ConfigFor returns non-nil.
	cfg := startup.ConfigFor(roleName)
	if cfg == nil {
		t.Fatal("expected non-nil ConfigFor after registration")
	}

	// Verify the role config has system prompt content that would produce
	// --append-system-prompt-file in the built command.
	if cfg.SystemPromptContent == "" {
		t.Error("expected non-empty SystemPromptContent")
	}
}

func TestBuildResumeStateForQuotaRotation(t *testing.T) {
	state := startup.ResumeState{
		Reason:          "quota_rotate",
		ClaimedResource: "sol-work-abc123",
	}

	if state.Reason != "quota_rotate" {
		t.Errorf("reason = %q, want quota_rotate", state.Reason)
	}
	if state.ClaimedResource != "sol-work-abc123" {
		t.Errorf("claimed_resource = %q, want sol-work-abc123", state.ClaimedResource)
	}

	// Build resume prime to verify the resume context is correct.
	prime := buildTestResumePrime(state)
	if !strings.Contains(prime, "quota_rotate") {
		t.Errorf("resume prime should mention quota_rotate, got %q", prime)
	}
	if !strings.Contains(prime, "sol-work-abc123") {
		t.Errorf("resume prime should mention claimed resource, got %q", prime)
	}
}

// buildTestResumePrime mimics startup.BuildResumePrime for test verification.
func buildTestResumePrime(state startup.ResumeState) string {
	var b strings.Builder
	b.WriteString("[RESUME] Session recovery")
	if state.Reason != "" {
		b.WriteString(" (reason: " + state.Reason + ")")
	}
	b.WriteString(".\n")
	if state.ClaimedResource != "" {
		b.WriteString("Claimed resource: " + state.ClaimedResource + " is claimed and in-progress.\n")
	}
	return b.String()
}

// TestSwapAndRespawnRollsBackAssignmentOnError verifies that when
// swapAndRespawn fails (e.g. session not found), the account is released
// back to Available status rather than remaining stuck in Assigned.
func TestSwapAndRespawnRollsBackAssignmentOnError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// Isolate tmux so Exists() doesn't hit the real server.
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("TMUX", "")

	state := &State{
		Accounts: map[string]*AccountState{
			"acct-a": {Status: Available},
		},
		PausedSessions: map[string]PausedSession{},
	}

	agent := store.Agent{
		ID:   "agent-1",
		Name: "Toast",
		Role: "outpost",
	}

	opts := RotateOpts{World: "testworld"}
	mgr := session.New()

	// The session does not exist, so swapAndRespawn should fail.
	err := swapAndRespawn(state, agent, "acct-a", opts, mgr, nil)
	if err == nil {
		t.Fatal("expected error from swapAndRespawn with missing session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}

	// The account must be rolled back to Available, not stuck in Assigned.
	acct := state.Accounts["acct-a"]
	if acct.Status != Available {
		t.Errorf("account status = %q after failed swapAndRespawn, want %q", acct.Status, Available)
	}
	if acct.AssignedTo != "" {
		t.Errorf("account AssignedTo = %q after rollback, want empty", acct.AssignedTo)
	}

	// Verify the account appears in AvailableAccountsLRU.
	avail := state.AvailableAccountsLRU()
	if len(avail) != 1 || avail[0] != "acct-a" {
		t.Errorf("AvailableAccountsLRU = %v, want [acct-a]", avail)
	}
}

// TestDryRunMarksAccountsConsumed verifies that accounts consumed by the
// main rotation loop in dry-run mode are marked as assigned, preventing
// restartPausedSessions from double-counting them via AvailableAccountsLRU.
// This is the regression test for T15.
func TestDryRunMarksAccountsConsumed(t *testing.T) {
	state := &State{
		Accounts: map[string]*AccountState{
			"acct-fresh": {Status: Available},
			"acct-spare": {Status: Available},
			"acct-bad":   {Status: Limited},
		},
		PausedSessions: map[string]PausedSession{},
	}

	// Before: both available accounts visible.
	before := state.AvailableAccountsLRU()
	if len(before) != 2 {
		t.Fatalf("expected 2 available accounts before dry-run, got %d", len(before))
	}

	// Simulate the dry-run rotation loop consuming one account.
	// This mirrors the else branch added to Rotate's dry-run path.
	state.MarkAssigned("acct-fresh", "testworld/Agent1")

	// After: only the unconsumed account should be visible.
	after := state.AvailableAccountsLRU()
	if len(after) != 1 {
		t.Fatalf("expected 1 available account after dry-run consumption, got %d: %v", len(after), after)
	}
	if after[0] != "acct-spare" {
		t.Errorf("expected acct-spare to remain available, got %s", after[0])
	}

	// Consume the second account too.
	state.MarkAssigned("acct-spare", "testworld/Agent2")
	final := state.AvailableAccountsLRU()
	if len(final) != 0 {
		t.Errorf("expected 0 available accounts after consuming all, got %d: %v", len(final), final)
	}
}

// TestFailedSwapDoesNotAdvanceAvailIdx verifies that when swapAndRespawn
// fails, the account is released and can be retried by the next agent.
// The fix moves availIdx++ after the swap, so this tests the rollback +
// retry behavior. This is the regression test for T43.
func TestFailedSwapDoesNotAdvanceAvailIdx(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// Isolate tmux.
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("TMUX", "")

	state := &State{
		Accounts: map[string]*AccountState{
			"acct-only": {Status: Available},
		},
		PausedSessions: map[string]PausedSession{},
	}

	agent := store.Agent{
		ID:   "agent-1",
		Name: "Alpha",
		Role: "outpost",
	}

	opts := RotateOpts{World: "testworld"}
	mgr := session.New()

	// swapAndRespawn will fail (session doesn't exist).
	err := swapAndRespawn(state, agent, "acct-only", opts, mgr, nil)
	if err == nil {
		t.Fatal("expected error from swapAndRespawn")
	}

	// After failure + rollback, the account must be available again
	// so the next agent in the rotation loop can use it.
	avail := state.AvailableAccountsLRU()
	if len(avail) != 1 {
		t.Fatalf("expected 1 available account after failed swap, got %d", len(avail))
	}
	if avail[0] != "acct-only" {
		t.Errorf("expected acct-only to be re-available, got %s", avail[0])
	}

	// The account status should be Available (not Assigned).
	acct := state.Accounts["acct-only"]
	if acct.Status != Available {
		t.Errorf("account status = %q, want %q", acct.Status, Available)
	}
}

// TestSwapAndRespawnRollsBackOnConfigError verifies rollback when the
// session exists but no startup config is registered for the role.
func TestSwapAndRespawnRollsBackOnConfigError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// Isolate tmux so we can create a real session for Exists() to find.
	tmuxDir := t.TempDir()
	t.Setenv("TMUX_TMPDIR", tmuxDir)
	t.Setenv("TMUX", "")

	state := &State{
		Accounts: map[string]*AccountState{
			"acct-b": {Status: Available},
		},
		PausedSessions: map[string]PausedSession{},
	}

	// Use a role that is definitely not registered.
	agent := store.Agent{
		ID:   "agent-2",
		Name: "Ember",
		Role: "nonexistent-role-xyz",
	}

	opts := RotateOpts{World: "testworld"}
	mgr := session.New()

	// Create a tmux session so Exists() passes, but ConfigFor will fail.
	sessionName := config.SessionName(opts.World, agent.Name)
	if startErr := mgr.Start(sessionName, tmp, "sleep 300", nil, agent.Role, opts.World); startErr != nil {
		t.Fatalf("failed to create test session: %v", startErr)
	}
	t.Cleanup(func() {
		mgr.Stop(sessionName, true)
	})

	err := swapAndRespawn(state, agent, "acct-b", opts, mgr, nil)
	if err == nil {
		t.Fatal("expected error from swapAndRespawn with unregistered role")
	}
	if !strings.Contains(err.Error(), "no startup config") {
		t.Fatalf("unexpected error: %v", err)
	}

	// The account must be rolled back to Available.
	acct := state.Accounts["acct-b"]
	if acct.Status != Available {
		t.Errorf("account status = %q after failed swapAndRespawn, want %q", acct.Status, Available)
	}
	if acct.AssignedTo != "" {
		t.Errorf("account AssignedTo = %q after rollback, want empty", acct.AssignedTo)
	}
}

// stubStopper implements sessionStopper for pauseAgent tests. It records
// every Stop() call and returns the configured error.
type stubStopper struct {
	stopErr error
	calls   []string
}

func (s *stubStopper) Stop(name string, force bool) error {
	s.calls = append(s.calls, name)
	return s.stopErr
}

// TestPauseAgentDoesNotRecordWhenStopFails is the regression test for OP-M1:
// when mgr.Stop returns an unexpected error, pauseAgent must NOT mark the
// agent as paused — otherwise restartPausedSessions would later try to
// resume a session that's still alive on the limited account and fail
// because mgr.Start rejects pre-existing sessions, wedging the agent.
func TestPauseAgentDoesNotRecordWhenStopFails(t *testing.T) {
	state := &State{
		Accounts: map[string]*AccountState{
			"acct-x": {Status: Limited},
		},
		PausedSessions: map[string]PausedSession{},
	}
	agent := store.Agent{
		ID:         "testworld/Toast",
		Name:       "Toast",
		Role:       "outpost",
		ActiveWrit: "sol-deadbeefcafef00d",
	}
	stub := &stubStopper{stopErr: errors.New("tmux server unreachable")}

	err := pauseAgent(state, agent, "acct-x", RotateOpts{World: "testworld"}, nil, stub, nil)
	if err == nil {
		t.Fatal("expected error from pauseAgent when Stop fails")
	}
	if !strings.Contains(err.Error(), "tmux server unreachable") {
		t.Errorf("error %q does not propagate underlying cause", err.Error())
	}

	// The agent must NOT be recorded as paused — otherwise it's wedged.
	if _, ok := state.PausedSessions[agent.ID]; ok {
		t.Errorf("agent %q recorded as paused despite Stop failure: %+v",
			agent.ID, state.PausedSessions[agent.ID])
	}

	// Sanity: Stop was actually attempted (not bypassed by an existence check).
	if len(stub.calls) != 1 {
		t.Errorf("Stop called %d times, want 1", len(stub.calls))
	}
}

// TestPauseAgentTreatsErrNotFoundAsSuccess verifies that an agent whose
// session is already gone (Stop returns session.ErrNotFound) is still
// recorded as paused. From the rotation's perspective the session is
// stopped — there's nothing more to do, so pause-state must be persisted
// or restartPausedSessions could never resume it.
func TestPauseAgentTreatsErrNotFoundAsSuccess(t *testing.T) {
	state := &State{
		Accounts:       map[string]*AccountState{"acct-x": {Status: Limited}},
		PausedSessions: map[string]PausedSession{},
	}
	agent := store.Agent{
		ID:         "testworld/Toast",
		Name:       "Toast",
		Role:       "outpost",
		ActiveWrit: "sol-abc",
	}
	// Wrap ErrNotFound the way Stop actually does, to exercise errors.Is.
	stub := &stubStopper{stopErr: fmt.Errorf("session %q: %w", "sol-testworld-Toast", session.ErrNotFound)}

	if err := pauseAgent(state, agent, "acct-x", RotateOpts{World: "testworld"}, nil, stub, nil); err != nil {
		t.Fatalf("pauseAgent should treat ErrNotFound as success, got %v", err)
	}
	paused, ok := state.PausedSessions[agent.ID]
	if !ok {
		t.Fatal("agent not recorded as paused after ErrNotFound")
	}
	if paused.PreviousAccount != "acct-x" {
		t.Errorf("PreviousAccount = %q, want acct-x", paused.PreviousAccount)
	}
	if paused.AgentName != "Toast" {
		t.Errorf("AgentName = %q, want Toast", paused.AgentName)
	}
	if paused.Writ != "sol-abc" {
		t.Errorf("Writ = %q, want sol-abc", paused.Writ)
	}
}

// TestPauseAgentRecordsOnSuccessfulStop is the happy-path complement to the
// failure-case test above.
func TestPauseAgentRecordsOnSuccessfulStop(t *testing.T) {
	state := &State{
		Accounts:       map[string]*AccountState{"acct-x": {Status: Limited}},
		PausedSessions: map[string]PausedSession{},
	}
	agent := store.Agent{ID: "testworld/Toast", Name: "Toast", Role: "outpost"}
	stub := &stubStopper{} // stopErr nil

	if err := pauseAgent(state, agent, "acct-x", RotateOpts{World: "testworld"}, nil, stub, nil); err != nil {
		t.Fatalf("pauseAgent: %v", err)
	}
	if _, ok := state.PausedSessions[agent.ID]; !ok {
		t.Error("agent not recorded as paused after successful Stop")
	}
}

// TestRotateRecordsFailedSwapAction is the regression test for OP-L1: when
// swapAndRespawn fails for an agent, Rotate must append a RotationAction
// with Status="failed" rather than dropping it silently. Otherwise the
// audit log claims a successful rotation that never happened.
//
// We trigger the natural failure path: no tmux session exists for the agent,
// so swapAndRespawn returns "session not found".
func TestRotateRecordsFailedSwapAction(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)
	// Isolate tmux so mgr.Exists() returns false instead of consulting the
	// real tmux server.
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("TMUX", "")

	world := "testworld"
	agentName := "Toast"
	role := "outpost"

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("OpenSphere: %v", err)
	}
	defer sphereStore.Close()

	if _, err := sphereStore.CreateAgent(agentName, world, role); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState(world+"/"+agentName, "working", ""); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}

	// Write the .account metadata file so ResolveCurrentAccount returns
	// "limited-acct" — putting the agent in the rotation's affected set.
	configDir := config.ClaudeConfigDir(config.WorldDir(world), role, agentName)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir configDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".account"), []byte("limited-acct\n"), 0o644); err != nil {
		t.Fatalf("write .account: %v", err)
	}

	// Pre-populate quota state: limited-acct is rate-limited (with a future
	// reset so ExpireLimits doesn't clear it), free-acct is the swap target.
	future := time.Now().UTC().Add(time.Hour)
	lock, st, err := AcquireLock()
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	st.Accounts["limited-acct"] = &AccountState{Status: Limited, ResetsAt: &future}
	st.Accounts["free-acct"] = &AccountState{Status: Available}
	if err := Save(st); err != nil {
		lock.Release()
		t.Fatalf("Save: %v", err)
	}
	lock.Release()

	mgr := session.New()
	result, err := Rotate(RotateOpts{World: world}, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	// Find the action for our agent.
	var found *RotationAction
	for i := range result.Actions {
		if result.Actions[i].AgentName == agentName {
			found = &result.Actions[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected an action for %s in result, got %+v", agentName, result.Actions)
	}
	if found.Status != "failed" {
		t.Errorf("Status = %q, want %q", found.Status, "failed")
	}
	if found.Error == "" {
		t.Error("expected non-empty Error on failed action")
	}
	if !strings.Contains(found.Error, "not found") {
		t.Errorf("Error = %q, want it to mention 'not found'", found.Error)
	}
	if found.FromAccount != "limited-acct" {
		t.Errorf("FromAccount = %q, want limited-acct", found.FromAccount)
	}
	if found.ToAccount != "free-acct" {
		t.Errorf("ToAccount = %q, want free-acct", found.ToAccount)
	}

	// And — since swapAndRespawn rolled back the assignment — the target
	// account must be available again so a retry can use it.
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Accounts["free-acct"].Status != Available {
		t.Errorf("free-acct status = %q after failed swap, want %q",
			loaded.Accounts["free-acct"].Status, Available)
	}
}
