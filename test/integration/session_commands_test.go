package integration

// session_commands_test.go — Integration tests for session CLI subcommands.
//
// Covers all 7 session subcommands:
//   sol session start   — argument validation, world resolution, happy path
//   sol session stop    — argument validation, nonexistent session, happy path
//   sol session list    — empty list, --json output, with sessions, --role filter
//   sol session health  — argument validation, nonexistent (exit 1), alive (exit 0)
//   sol session capture — argument validation, nonexistent, happy path
//   sol session attach  — argument validation (can't test syscall.Exec)
//   sol session inject  — argument validation, missing --message, nonexistent, happy path
//
// All behavioral tests use setupTestEnv() from helpers_test.go for isolation.
// None spawn real claude processes or touch the real tmux server.

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---------- sol session start ----------

func TestCLISessionStartMissingArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "session", "start")
	if err == nil {
		t.Fatalf("expected error for missing session name, got: %s", out)
	}
	// cobra.ExactArgs(1) produces an "accepts 1 arg(s)" error.
	if !strings.Contains(out, "accepts 1 arg") {
		t.Errorf("expected args error, got: %s", out)
	}
}

func TestCLISessionStartMissingWorld(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)

	// Clear SOL_WORLD so world resolution falls through all sources.
	t.Setenv("SOL_WORLD", "")

	out, err := runGT(t, gtHome, "session", "start", "test-session")
	if err == nil {
		t.Fatalf("expected error for missing world, got: %s", out)
	}
	if !strings.Contains(out, "--world") {
		t.Errorf("expected error mentioning --world, got: %s", out)
	}
}

func TestCLISessionStartHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	out, err := runGT(t, gtHome, "session", "start", "sol-ember-TestStart",
		"--world=ember", "--cmd=sleep 300")
	if err != nil {
		t.Fatalf("session start failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Session sol-ember-TestStart started") {
		t.Errorf("expected start confirmation, got: %s", out)
	}
}

func TestCLISessionStartDuplicate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// First start should succeed.
	_, err := runGT(t, gtHome, "session", "start", "sol-ember-TestDup",
		"--world=ember", "--cmd=sleep 300")
	if err != nil {
		t.Fatalf("first start failed: %v", err)
	}

	// Second start with the same name should fail.
	out, err := runGT(t, gtHome, "session", "start", "sol-ember-TestDup",
		"--world=ember", "--cmd=sleep 300")
	if err == nil {
		t.Fatalf("expected error for duplicate session, got: %s", out)
	}
	if !strings.Contains(out, "already exists") {
		t.Errorf("expected 'already exists' error, got: %s", out)
	}
}

// ---------- sol session stop ----------

func TestCLISessionStopMissingArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "session", "stop")
	if err == nil {
		t.Fatalf("expected error for missing session name, got: %s", out)
	}
	if !strings.Contains(out, "accepts 1 arg") {
		t.Errorf("expected args error, got: %s", out)
	}
}

func TestCLISessionStopNonexistent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)

	out, err := runGT(t, gtHome, "session", "stop", "sol-no-such-session")
	if err == nil {
		t.Fatalf("expected error for nonexistent session, got: %s", out)
	}
	if !strings.Contains(out, "not found") {
		t.Errorf("expected 'not found' error, got: %s", out)
	}
}

func TestCLISessionStopHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// Start a session first.
	_, err := runGT(t, gtHome, "session", "start", "sol-ember-TestStop",
		"--world=ember", "--cmd=sleep 300")
	if err != nil {
		t.Fatalf("session start failed: %v", err)
	}

	// Stop it.
	out, err := runGT(t, gtHome, "session", "stop", "sol-ember-TestStop")
	if err != nil {
		t.Fatalf("session stop failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Session sol-ember-TestStop stopped") {
		t.Errorf("expected stop confirmation, got: %s", out)
	}
}

func TestCLISessionStopForce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// Start then force-stop.
	_, err := runGT(t, gtHome, "session", "start", "sol-ember-TestForce",
		"--world=ember", "--cmd=sleep 300")
	if err != nil {
		t.Fatalf("session start failed: %v", err)
	}

	out, err := runGT(t, gtHome, "session", "stop", "sol-ember-TestForce", "--force")
	if err != nil {
		t.Fatalf("session stop --force failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "stopped") {
		t.Errorf("expected stop confirmation, got: %s", out)
	}
}

// ---------- sol session list ----------

func TestCLISessionListEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)

	out, err := runGT(t, gtHome, "session", "list")
	if err != nil {
		t.Fatalf("session list failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "No sessions found") {
		t.Errorf("expected 'No sessions found', got: %s", out)
	}
}

func TestCLISessionListJSONEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)

	out, err := runGT(t, gtHome, "session", "list", "--json")
	if err != nil {
		t.Fatalf("session list --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("session list --json output is not valid JSON: %s", out)
	}
	// Empty list should render as [].
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("expected empty JSON array, got: %s", out)
	}
}

func TestCLISessionListWithSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// Start a session.
	_, err := runGT(t, gtHome, "session", "start", "sol-ember-TestList",
		"--world=ember", "--cmd=sleep 300", "--role=outpost")
	if err != nil {
		t.Fatalf("session start failed: %v", err)
	}

	// List should show the session.
	out, err := runGT(t, gtHome, "session", "list")
	if err != nil {
		t.Fatalf("session list failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "sol-ember-TestList") {
		t.Errorf("expected session name in list, got: %s", out)
	}
	if !strings.Contains(out, "outpost") {
		t.Errorf("expected role 'outpost' in list, got: %s", out)
	}
	if !strings.Contains(out, "ember") {
		t.Errorf("expected world 'ember' in list, got: %s", out)
	}
	if !strings.Contains(out, "1 session") {
		t.Errorf("expected '1 session' footer, got: %s", out)
	}
}

func TestCLISessionListJSONWithSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	_, err := runGT(t, gtHome, "session", "start", "sol-ember-TestJSON",
		"--world=ember", "--cmd=sleep 300", "--role=outpost")
	if err != nil {
		t.Fatalf("session start failed: %v", err)
	}

	out, err := runGT(t, gtHome, "session", "list", "--json")
	if err != nil {
		t.Fatalf("session list --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("session list --json output is not valid JSON: %s", out)
	}
	if !strings.Contains(out, "sol-ember-TestJSON") {
		t.Errorf("expected session name in JSON output, got: %s", out)
	}
	if !strings.Contains(out, `"role"`) {
		t.Errorf("expected 'role' key in JSON output, got: %s", out)
	}
}

func TestCLISessionListRoleFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// Start a session with role=outpost.
	_, err := runGT(t, gtHome, "session", "start", "sol-ember-TestRole",
		"--world=ember", "--cmd=sleep 300", "--role=outpost")
	if err != nil {
		t.Fatalf("session start failed: %v", err)
	}

	// Filter by matching role should show the session.
	out, err := runGT(t, gtHome, "session", "list", "--role=outpost")
	if err != nil {
		t.Fatalf("session list --role=outpost failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "sol-ember-TestRole") {
		t.Errorf("expected session in --role=outpost filter, got: %s", out)
	}

	// Filter by non-matching role should not show the session.
	out, err = runGT(t, gtHome, "session", "list", "--role=envoy")
	if err != nil {
		t.Fatalf("session list --role=envoy failed: %v: %s", err, out)
	}
	if strings.Contains(out, "sol-ember-TestRole") {
		t.Errorf("session should not appear with --role=envoy, got: %s", out)
	}
	if !strings.Contains(out, "No sessions found") {
		t.Errorf("expected 'No sessions found' for non-matching role, got: %s", out)
	}
}

// ---------- sol session health ----------

func TestCLISessionHealthMissingArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "session", "health")
	if err == nil {
		t.Fatalf("expected error for missing session name, got: %s", out)
	}
	if !strings.Contains(out, "accepts 1 arg") {
		t.Errorf("expected args error, got: %s", out)
	}
}

func TestCLISessionHealthNonexistent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)

	out, err := runGT(t, gtHome, "session", "health", "sol-no-such-session")
	if err == nil {
		t.Fatalf("expected non-zero exit for dead session, got: %s", out)
	}
	// Dead session → exit 1, output "dead".
	if exitCode(err) != 1 {
		t.Errorf("expected exit code 1 for dead session, got %d: %s", exitCode(err), out)
	}
	if !strings.Contains(out, "dead") {
		t.Errorf("expected 'dead' in health output, got: %s", out)
	}
}

func TestCLISessionHealthAlive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// Start a session.
	_, err := runGT(t, gtHome, "session", "start", "sol-ember-TestHealth",
		"--world=ember", "--cmd=sleep 300")
	if err != nil {
		t.Fatalf("session start failed: %v", err)
	}

	// Health should report healthy (exit 0).
	out, err := runGT(t, gtHome, "session", "health", "sol-ember-TestHealth")
	if err != nil {
		t.Fatalf("expected exit 0 for healthy session, got error: %v: %s", err, out)
	}
	if !strings.Contains(out, "healthy") {
		t.Errorf("expected 'healthy' in output, got: %s", out)
	}
}

// ---------- sol session capture ----------

func TestCLISessionCaptureMissingArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "session", "capture")
	if err == nil {
		t.Fatalf("expected error for missing session name, got: %s", out)
	}
	if !strings.Contains(out, "accepts 1 arg") {
		t.Errorf("expected args error, got: %s", out)
	}
}

func TestCLISessionCaptureNonexistent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)

	out, err := runGT(t, gtHome, "session", "capture", "sol-no-such-session")
	if err == nil {
		t.Fatalf("expected error for nonexistent session, got: %s", out)
	}
	if !strings.Contains(out, "not found") {
		t.Errorf("expected 'not found' error, got: %s", out)
	}
}

func TestCLISessionCaptureHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// Start a session.
	_, err := runGT(t, gtHome, "session", "start", "sol-ember-TestCapture",
		"--world=ember", "--cmd=sleep 300")
	if err != nil {
		t.Fatalf("session start failed: %v", err)
	}

	// Capture should succeed (output may be empty for sleep, but no error).
	_, err = runGT(t, gtHome, "session", "capture", "sol-ember-TestCapture")
	if err != nil {
		t.Fatalf("session capture failed: %v", err)
	}
}

func TestCLISessionCaptureLinesFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	_, err := runGT(t, gtHome, "session", "start", "sol-ember-TestCapLines",
		"--world=ember", "--cmd=sleep 300")
	if err != nil {
		t.Fatalf("session start failed: %v", err)
	}

	// --lines flag should be accepted without error.
	_, err = runGT(t, gtHome, "session", "capture", "sol-ember-TestCapLines", "--lines=10")
	if err != nil {
		t.Fatalf("session capture --lines=10 failed: %v", err)
	}
}

// ---------- sol session attach ----------

func TestCLISessionAttachMissingArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "session", "attach")
	if err == nil {
		t.Fatalf("expected error for missing session name, got: %s", out)
	}
	if !strings.Contains(out, "accepts 1 arg") {
		t.Errorf("expected args error, got: %s", out)
	}
}

func TestCLISessionAttachNonexistent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)

	out, err := runGT(t, gtHome, "session", "attach", "sol-no-such-session")
	if err == nil {
		t.Fatalf("expected error for nonexistent session, got: %s", out)
	}
	if !strings.Contains(out, "not found") {
		t.Errorf("expected 'not found' error, got: %s", out)
	}
}

// Note: Cannot test actual attach behavior — it calls syscall.Exec which
// replaces the running process. Argument validation and nonexistent checks
// are sufficient for CLI-level coverage.

// ---------- sol session inject ----------

func TestCLISessionInjectMissingArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "session", "inject")
	if err == nil {
		t.Fatalf("expected error for missing session name, got: %s", out)
	}
	if !strings.Contains(out, "accepts 1 arg") {
		t.Errorf("expected args error, got: %s", out)
	}
}

func TestCLISessionInjectMissingMessage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)

	out, err := runGT(t, gtHome, "session", "inject", "sol-any-session")
	if err == nil {
		t.Fatalf("expected error for missing --message, got: %s", out)
	}
	if !strings.Contains(out, "message") {
		t.Errorf("expected error mentioning --message, got: %s", out)
	}
}

func TestCLISessionInjectNonexistent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)

	out, err := runGT(t, gtHome, "session", "inject", "sol-no-such-session", "--message=hello")
	if err == nil {
		t.Fatalf("expected error for nonexistent session, got: %s", out)
	}
	if !strings.Contains(out, "not found") {
		t.Errorf("expected 'not found' error, got: %s", out)
	}
}

func TestCLISessionInjectHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// Start a session.
	_, err := runGT(t, gtHome, "session", "start", "sol-ember-TestInject",
		"--world=ember", "--cmd=sleep 300")
	if err != nil {
		t.Fatalf("session start failed: %v", err)
	}

	// Inject a message.
	out, err := runGT(t, gtHome, "session", "inject", "sol-ember-TestInject",
		"--message=hello world")
	if err != nil {
		t.Fatalf("session inject failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Injected message into session sol-ember-TestInject") {
		t.Errorf("expected inject confirmation, got: %s", out)
	}
}

func TestCLISessionInjectNoSubmit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	_, err := runGT(t, gtHome, "session", "start", "sol-ember-TestNoSub",
		"--world=ember", "--cmd=sleep 300")
	if err != nil {
		t.Fatalf("session start failed: %v", err)
	}

	// --no-submit should be accepted without error.
	out, err := runGT(t, gtHome, "session", "inject", "sol-ember-TestNoSub",
		"--message=staged text", "--no-submit")
	if err != nil {
		t.Fatalf("session inject --no-submit failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Injected message") {
		t.Errorf("expected inject confirmation, got: %s", out)
	}
}

// ---------- sol session subcommand help ----------

func TestCLISessionSubcommandHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	tests := []struct {
		subcmd   string
		expected string
	}{
		{"start", "Start a tmux session"},
		{"stop", "Stop a tmux session"},
		{"list", "List all tmux sessions"},
		{"health", "Check session health"},
		{"capture", "Capture pane output"},
		{"attach", "Attach to a tmux session"},
		{"inject", "Inject text into a session"},
	}

	for _, tt := range tests {
		t.Run(tt.subcmd, func(t *testing.T) {
			out, err := runGT(t, gtHome, "session", tt.subcmd, "--help")
			if err != nil {
				t.Fatalf("sol session %s --help failed: %v: %s", tt.subcmd, err, out)
			}
			if !strings.Contains(out, tt.expected) {
				t.Errorf("sol session %s --help missing %q, got: %s", tt.subcmd, tt.expected, out)
			}
		})
	}
}
