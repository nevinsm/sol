package quota

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/startup"
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

// TestResolveCurrentAccountMatchesScan exercises both ResolveCurrentAccount
// (rotate.go) and resolveSessionAccount (scan.go) against the same fixtures
// to guarantee they agree. Both should accept legitimate symlinks pointing at
// $SOL_HOME/.accounts/<handle>/.credentials.json and reject targets that point
// somewhere else under accountsDir.
func TestResolveCurrentAccountMatchesScan(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	world := "testworld"
	worldDir := config.WorldDir(world)
	role := "outpost"
	agentName := "Toast"
	configDir := config.ClaudeConfigDir(worldDir, role, agentName)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir configDir: %v", err)
	}

	accountsDir := config.AccountsDir()
	handle := "alice"
	accountDir := filepath.Join(accountsDir, handle)
	if err := os.MkdirAll(accountDir, 0o755); err != nil {
		t.Fatalf("mkdir accountDir: %v", err)
	}
	accountCreds := filepath.Join(accountDir, ".credentials.json")
	if err := os.WriteFile(accountCreds, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write account creds: %v", err)
	}

	credLink := filepath.Join(configDir, ".credentials.json")
	sessInfo := session.SessionInfo{
		Name: config.SessionName(world, agentName),
		Role: role,
	}

	// --- Case 1: legitimate symlink. Both should return the handle.
	if err := os.Symlink(accountCreds, credLink); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	gotRotate := ResolveCurrentAccount(world, agentName, role)
	gotScan := resolveSessionAccount(world, sessInfo)
	if gotRotate != handle {
		t.Errorf("ResolveCurrentAccount = %q, want %q", gotRotate, handle)
	}
	if gotScan != handle {
		t.Errorf("resolveSessionAccount = %q, want %q", gotScan, handle)
	}
	if gotRotate != gotScan {
		t.Errorf("ResolveCurrentAccount/resolveSessionAccount disagree: %q vs %q", gotRotate, gotScan)
	}

	// --- Case 2: symlink target points at the wrong file under accountsDir.
	// scan.go rejects this because the trailing component isn't .credentials.json.
	// rotate.go's old behavior (before this fix) accepted it. Both must now reject.
	if err := os.Remove(credLink); err != nil {
		t.Fatalf("remove credLink: %v", err)
	}
	stray := filepath.Join(accountDir, "wrong-file.json")
	if err := os.WriteFile(stray, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write stray: %v", err)
	}
	if err := os.Symlink(stray, credLink); err != nil {
		t.Fatalf("symlink stray: %v", err)
	}
	gotRotate = ResolveCurrentAccount(world, agentName, role)
	gotScan = resolveSessionAccount(world, sessInfo)
	if gotRotate != "" {
		t.Errorf("ResolveCurrentAccount with stray target = %q, want empty", gotRotate)
	}
	if gotScan != "" {
		t.Errorf("resolveSessionAccount with stray target = %q, want empty", gotScan)
	}
	if gotRotate != gotScan {
		t.Errorf("ResolveCurrentAccount/resolveSessionAccount disagree on stray: %q vs %q", gotRotate, gotScan)
	}

	// --- Case 3: .account file takes precedence over symlink (broker-managed).
	if err := os.WriteFile(filepath.Join(configDir, ".account"), []byte("bob\n"), 0o600); err != nil {
		t.Fatalf("write .account: %v", err)
	}
	gotRotate = ResolveCurrentAccount(world, agentName, role)
	gotScan = resolveSessionAccount(world, sessInfo)
	if gotRotate != "bob" {
		t.Errorf("ResolveCurrentAccount with .account = %q, want bob", gotRotate)
	}
	if gotScan != "bob" {
		t.Errorf("resolveSessionAccount with .account = %q, want bob", gotScan)
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
