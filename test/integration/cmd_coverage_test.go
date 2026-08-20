package integration

// cmd_coverage_test.go — Integration smoke + happy-path tests for shipped
// subcommands that previously had no integration coverage:
//   sol cost
//   sol dash (TUI: smoke via --help; state-build via dash.NewModel)
//   sol inbox (--json non-interactive surface; FetchItems state-build)
//   sol agent postmortem
//   sol writ trace
//   sol writ clean
//
// All tests use the setupTestEnv() helpers from helpers_test.go so that
// SOL_HOME, the tmux server, and the session command are isolated. None of
// these tests spawn a real claude process or touch the real tmux server.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dash"
	"github.com/nevinsm/sol/internal/inbox"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
)

// ---------- sol cost ----------

func TestCLICostSphereSmoke(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)

	// No worlds, no token data — should still exit cleanly with helpful text.
	out, err := runGT(t, gtHome, "cost")
	if err != nil {
		t.Fatalf("sol cost failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "No token usage data") {
		t.Errorf("expected empty-state message, got: %s", out)
	}
}

func TestCLICostWorldHappyPath(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// World view works against a freshly-initialised world (no tokens yet).
	out, err := runGT(t, gtHome, "cost", "--world=ember")
	if err != nil {
		t.Fatalf("sol cost --world=ember failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "ember") {
		t.Errorf("expected world name in output, got: %s", out)
	}

	// JSON output should be valid JSON (sphere view).
	out, err = runGT(t, gtHome, "cost", "--json")
	if err != nil {
		t.Fatalf("sol cost --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("sol cost --json produced invalid JSON: %s", out)
	}

	// Invalid flag combinations should error (not panic).
	if _, err := runGT(t, gtHome, "cost", "--agent=Foo"); err == nil {
		t.Errorf("expected --agent without --world to fail")
	}
}

// ---------- sol dash (TUI) ----------

func TestCLIDashHelpSmoke(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)

	out, err := runGT(t, gtHome, "dash", "--help")
	if err != nil {
		t.Fatalf("sol dash --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "dashboard") {
		t.Errorf("dash help missing 'dashboard': %s", out)
	}
}

// TestDashStateBuildHappyPath exercises the non-interactive surface of the
// dashboard by constructing a dash.Config + dash.Model against a real sphere
// store. The bubbletea program is intentionally not driven — we only assert
// that NewModel does not panic and that the dependency wiring is sound.
func TestDashStateBuildHappyPath(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	mgr := session.New()
	cfg := dash.Config{
		SphereStore:      sphereStore,
		EscalationLister: sphereStore,
		WorldOpener:      func(w string) (*store.WorldStore, error) { return store.OpenWorld(w) },
		SessionCheck:     mgr,
		CaravanStore:     sphereStore,
		SessionMgr:       mgr,
		SOLHome:          gtHome,
	}

	m := dash.NewModel(cfg)
	// Call Init to ensure command graph builds without panicking.
	if cmd := m.Init(); cmd == nil {
		t.Errorf("dash.Model.Init returned nil command")
	}
}

// ---------- sol inbox ----------

func TestCLIInboxJSONEmpty(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)

	// Force operator identity regardless of the ambient shell's own
	// SOL_WORLD/SOL_AGENT (e.g. when this suite runs inside a sol agent
	// session) -- runGT's subprocess inherits os.Environ(), and identity
	// resolution now (post identity-aware inbox) reads those vars, so an
	// unrelated ambient identity would otherwise leak into a "default
	// autarch" test.
	t.Setenv("SOL_WORLD", "")
	t.Setenv("SOL_AGENT", "")

	out, err := runGT(t, gtHome, "inbox", "--json")
	if err != nil {
		t.Fatalf("sol inbox --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("sol inbox --json output is not valid JSON: %q", out)
	}
}

func TestCLIInboxJSONHappyPath(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)

	// Force operator identity -- see TestCLIInboxJSONEmpty for why.
	t.Setenv("SOL_WORLD", "")
	t.Setenv("SOL_AGENT", "")

	// Seed an unread message via the store so FetchItems has work to do.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere: %v", err)
	}
	defer sphereStore.Close()

	if _, err := sphereStore.SendMessage("ember/Toast", "autarch", "hello", "body", 2, "notification"); err != nil {
		t.Fatalf("send message: %v", err)
	}

	// FetchItems direct (state-build path).
	items, err := inbox.FetchItems(sphereStore, config.Autarch)
	if err != nil {
		t.Fatalf("inbox.FetchItems: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("expected at least 1 inbox item, got 0")
	}

	// CLI path.
	out, err := runGT(t, gtHome, "inbox", "--json")
	if err != nil {
		t.Fatalf("sol inbox --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("sol inbox --json output not valid JSON: %s", out)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected message subject in inbox output, got: %s", out)
	}
}

// TestCLIInboxIdentityScoping verifies "sol inbox --json" scopes results to
// the caller identity: an agent identity (SOL_WORLD/SOL_AGENT set) sees
// only its own pending mail and none of the operator's, while the operator
// (identity vars unset) keeps seeing its own mail and none of the agent's.
func TestCLIInboxIdentityScoping(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere: %v", err)
	}
	defer sphereStore.Close()

	if _, err := sphereStore.SendMessage("agent-x", "autarch", "for the operator", "", 2, "notification"); err != nil {
		t.Fatalf("send message to autarch: %v", err)
	}
	if _, err := sphereStore.SendMessage("agent-y", "ember/Toast", "for toast", "", 2, "notification"); err != nil {
		t.Fatalf("send message to ember/Toast: %v", err)
	}

	// Agent identity: only its own mail, never the operator's.
	t.Setenv("SOL_WORLD", "ember")
	t.Setenv("SOL_AGENT", "Toast")
	out, err := runGT(t, gtHome, "inbox", "--json")
	if err != nil {
		t.Fatalf("sol inbox --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("sol inbox --json output not valid JSON: %s", out)
	}
	if !strings.Contains(out, "for toast") {
		t.Errorf("expected the agent's own message in its scoped inbox output, got: %s", out)
	}
	if strings.Contains(out, "for the operator") {
		t.Errorf("expected the operator's mail excluded from the agent's scoped inbox, got: %s", out)
	}

	// Operator identity (SOL_WORLD/SOL_AGENT unset): unchanged autarch behavior.
	t.Setenv("SOL_WORLD", "")
	t.Setenv("SOL_AGENT", "")
	out, err = runGT(t, gtHome, "inbox", "--json")
	if err != nil {
		t.Fatalf("sol inbox --json failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "for the operator") {
		t.Errorf("expected the operator's own message in the unscoped inbox output, got: %s", out)
	}
	if strings.Contains(out, "for toast") {
		t.Errorf("expected the agent's mail excluded from the operator's inbox, got: %s", out)
	}
}

// ---------- sol agent postmortem ----------

func TestCLIAgentPostmortemHappyPath(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// Create an agent (no tmux session needed — we want the dead-agent path).
	out, err := runGT(t, gtHome, "agent", "create", "Smoke", "--world=ember")
	if err != nil {
		t.Fatalf("agent create: %v: %s", err, out)
	}

	// Run postmortem against the freshly-created agent.
	out, err = runGT(t, gtHome, "agent", "postmortem", "Smoke", "--world=ember")
	if err != nil {
		t.Fatalf("agent postmortem failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Agent Postmortem: Smoke") {
		t.Errorf("expected postmortem header, got: %s", out)
	}
	if !strings.Contains(out, "Session") {
		t.Errorf("expected Session section, got: %s", out)
	}

	// JSON path.
	out, err = runGT(t, gtHome, "agent", "postmortem", "Smoke", "--world=ember", "--json")
	if err != nil {
		t.Fatalf("agent postmortem --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("agent postmortem --json output is not valid JSON: %s", out)
	}

	// Unknown agent should error cleanly.
	if _, err := runGT(t, gtHome, "agent", "postmortem", "Ghost", "--world=ember"); err == nil {
		t.Errorf("expected error for unknown agent")
	}
}

// ---------- sol writ trace ----------

func TestCLIWritTraceHappyPath(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// Create a writ to trace.
	out, err := runGT(t, gtHome, "writ", "create", "--world=ember", "--title=traceme")
	if err != nil {
		t.Fatalf("writ create: %v: %s", err, out)
	}
	writID := strings.TrimSpace(out)
	if !strings.HasPrefix(writID, "sol-") {
		t.Fatalf("expected writ ID, got: %q", writID)
	}

	// Default rendering.
	out, err = runGT(t, gtHome, "writ", "trace", writID, "--world=ember")
	if err != nil {
		t.Fatalf("writ trace failed: %v: %s", err, out)
	}
	if !strings.Contains(out, writID) {
		t.Errorf("expected writ ID in trace output, got: %s", out)
	}
	if !strings.Contains(out, "Timeline") {
		t.Errorf("expected Timeline section in trace output, got: %s", out)
	}

	// Timeline-only rendering.
	if _, err := runGT(t, gtHome, "writ", "trace", writID, "--world=ember", "--timeline"); err != nil {
		t.Errorf("writ trace --timeline failed: %v", err)
	}

	// Cost-only rendering.
	if _, err := runGT(t, gtHome, "writ", "trace", writID, "--world=ember", "--cost"); err != nil {
		t.Errorf("writ trace --cost failed: %v", err)
	}

	// JSON path.
	out, err = runGT(t, gtHome, "writ", "trace", writID, "--world=ember", "--json")
	if err != nil {
		t.Fatalf("writ trace --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("writ trace --json output is not valid JSON: %s", out)
	}

	// Invalid writ ID format should error.
	if _, err := runGT(t, gtHome, "writ", "trace", "not-a-writ-id", "--world=ember"); err == nil {
		t.Errorf("expected error for invalid writ ID")
	}
}

// ---------- sol writ clean ----------

func TestCLIWritCleanSmoke(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// No closed writs at all — should report nothing to clean.
	out, err := runGT(t, gtHome, "writ", "clean", "--world=ember")
	if err != nil {
		t.Fatalf("writ clean (empty) failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "No eligible") {
		t.Errorf("expected empty-state message, got: %s", out)
	}
}

func TestCLIWritCleanInvalidOlderThan(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// Bad --older-than value should error with a helpful message.
	out, err := runGT(t, gtHome, "writ", "clean", "--world=ember", "--older-than=garbage")
	if err == nil {
		t.Fatalf("expected error for bad --older-than, got: %s", out)
	}
	if !strings.Contains(out, "older-than") && !strings.Contains(out, "format") {
		t.Errorf("expected error to mention older-than/format, got: %s", out)
	}
}
