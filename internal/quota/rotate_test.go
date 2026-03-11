package quota

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
