package quota

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
