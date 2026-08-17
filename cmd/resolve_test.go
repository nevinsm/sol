package cmd

import (
	"strings"
	"testing"
)

// resetResolveFlags resets resolve package-level flag vars between test runs
// (resolveCmd is a package-level singleton *cobra.Command).
func resetResolveFlags() {
	resolveWorld = ""
	resolveAgent = ""
	resolveJSON = false
}

// TestResolveNonexistentAgentErrorNotDoubleWrapped verifies confirmed fix #8
// (sol-8d4afcfa0390dd73): `sol resolve --agent=<nonexistent>` used to
// produce a double-wrapped, misleadingly-framed error —
// "failed to resolve writ: failed to get agent \"...\": agent \"...\": not found"
// — restating the agent ID three times and leading with "failed to resolve
// writ" even though no writ lookup was ever attempted. cmd/resolve.go no
// longer adds its own wrap on top of dispatch.Resolve's already-contextual
// error.
func TestResolveNonexistentAgentErrorNotDoubleWrapped(t *testing.T) {
	world := "resolveerrtest"
	initTestWorld(t, world)
	resetResolveFlags()
	t.Cleanup(resetResolveFlags)

	rootCmd.SetArgs([]string{"resolve", "--world=" + world, "--agent=NoSuchAgent"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}

	msg := err.Error()
	if strings.Contains(msg, "failed to resolve writ") {
		t.Errorf("error should not be framed as a writ-resolution failure (no writ was ever looked up), got: %s", msg)
	}
	if !strings.Contains(msg, "failed to get agent") {
		t.Errorf("expected the underlying agent-lookup error to surface, got: %s", msg)
	}
	if !strings.Contains(msg, "not found") {
		t.Errorf("expected a not-found error, got: %s", msg)
	}
}
