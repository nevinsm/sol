package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/nudge"
)

// setupNudgeCmdWorld creates a minimal SOL_HOME + world directory (just
// enough for config.ResolveWorld/RequireWorld to succeed) and points
// SOL_WORLD/SOL_AGENT at it via env vars, mirroring how a live agent session
// has them set. Returns the resolved session name.
func setupNudgeCmdWorld(t *testing.T, world, agent string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	t.Setenv("SOL_WORLD", world)
	t.Setenv("SOL_AGENT", agent)

	worldDir := filepath.Join(dir, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[world]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reset the flag-bound package vars so a value left over from another
	// test (or a real flag parse elsewhere in the process) doesn't leak in —
	// this test relies purely on the SOL_WORLD/SOL_AGENT env fallback.
	nudgeWorld = ""
	nudgeAgent = ""

	return config.SessionName(world, agent)
}

// runNudgeDrain invokes nudgeDrainCmd's RunE directly (mid-session CLI
// invocation, not a full cobra.Execute()) with the given --json value, and
// captures stdout produced during the call.
func runNudgeDrain(t *testing.T, asJSON bool) (string, error) {
	t.Helper()
	if err := nudgeDrainCmd.Flags().Set("json", boolStr(asJSON)); err != nil {
		t.Fatalf("failed to set --json flag: %v", err)
	}
	t.Cleanup(func() { _ = nudgeDrainCmd.Flags().Set("json", "false") })

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	runErr := nudgeDrainCmd.RunE(nudgeDrainCmd, nil)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return buf.String(), runErr
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// TestNudgeDrainCmdRoundTrip covers Task B's core requirement: an agent can
// run `sol nudge drain` mid-session (not just at a turn boundary) to
// retrieve queued content and clear the queue, with the same receipt
// protocol as the automatic drain.
func TestNudgeDrainCmdRoundTrip(t *testing.T) {
	sess := setupNudgeCmdWorld(t, "sol-dev", "Nova")

	if err := nudge.Enqueue(sess, nudge.Message{
		Sender: "autarch", Type: "MAIL", Subject: "first", Body: "hello",
	}); err != nil {
		t.Fatalf("Enqueue #1 failed: %v", err)
	}
	if err := nudge.Enqueue(sess, nudge.Message{
		Sender: "sentinel", Type: "nudge", Subject: "second",
	}); err != nil {
		t.Fatalf("Enqueue #2 failed: %v", err)
	}

	out, err := runNudgeDrain(t, false)
	if err != nil {
		t.Fatalf("nudge drain failed: %v", err)
	}

	// Sender/via metadata must be visible, not just subject/body.
	if !strings.Contains(out, "autarch") || !strings.Contains(out, "sentinel") {
		t.Errorf("expected drain output to include sender metadata, got: %q", out)
	}
	if !strings.Contains(out, "first") || !strings.Contains(out, "second") {
		t.Errorf("expected drain output to include both subjects, got: %q", out)
	}

	// Batch-level continuation framing must appear exactly once, not once
	// per message: a non-empty drain should not read as N separate
	// corrections of whatever the agent was doing.
	if got := strings.Count(out, nudge.DrainFraming); got != 1 {
		t.Errorf("expected DrainFraming exactly once in a non-empty drain, got %d in: %q", got, out)
	}

	// Round trip: queue must be empty after drain.
	count, err := nudge.Peek(sess)
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected queue empty after drain, got count=%d", count)
	}

	// Draining again mid-session (no new messages) must be a silent no-op.
	out2, err := runNudgeDrain(t, false)
	if err != nil {
		t.Fatalf("second nudge drain failed: %v", err)
	}
	if out2 != "" {
		t.Errorf("expected empty output draining an already-empty queue, got: %q", out2)
	}
}

func TestNudgeDrainCmdJSON(t *testing.T) {
	sess := setupNudgeCmdWorld(t, "sol-dev", "Nova")

	if err := nudge.Enqueue(sess, nudge.Message{
		Sender: "autarch", Type: "MAIL", Subject: "json-test", Body: "body",
	}); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	out, err := runNudgeDrain(t, true)
	if err != nil {
		t.Fatalf("nudge drain --json failed: %v", err)
	}

	// --json is a programmatic surface: it must never carry the text-mode
	// continuation framing.
	if strings.Contains(out, nudge.DrainFraming) {
		t.Errorf("expected --json output to be free of DrainFraming, got: %q", out)
	}

	var decoded []map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("failed to parse JSON output %q: %v", out, err)
	}
	if len(decoded) != 1 {
		t.Fatalf("expected 1 message in JSON output, got %d", len(decoded))
	}
	if decoded[0]["source"] != "autarch" {
		t.Errorf("source = %v, want %q", decoded[0]["source"], "autarch")
	}
	if decoded[0]["body"] != "body" {
		t.Errorf("body = %v, want %q", decoded[0]["body"], "body")
	}

	count, err := nudge.Peek(sess)
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected queue empty after JSON drain, got count=%d", count)
	}
}

func TestNudgeDrainCmdJSONEmptyQueueIsEmptyArray(t *testing.T) {
	setupNudgeCmdWorld(t, "sol-dev", "Nova")

	out, err := runNudgeDrain(t, true)
	if err != nil {
		t.Fatalf("nudge drain --json failed: %v", err)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("expected JSON empty array for an empty queue, got: %q", out)
	}
}

// TestNudgeDrainCmdReadErrorExitsNonZero documents the exit-code contract in
// nudgeDrainCmd's Long text: a genuine queue read failure (not "empty
// queue") must surface as a non-nil error, which cobra's Execute() maps to
// exit 1.
func TestNudgeDrainCmdReadErrorExitsNonZero(t *testing.T) {
	sess := setupNudgeCmdWorld(t, "sol-dev", "Nova")

	if err := nudge.Enqueue(sess, nudge.Message{Sender: "test", Type: "info", Subject: "x"}); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Make the queue directory unreadable to force a genuine read error
	// (distinct from "queue does not exist", which is a silent no-op).
	queueDir := config.NudgeQueueDir(sess)
	if err := os.Chmod(queueDir, 0o000); err != nil {
		t.Fatalf("Chmod failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(queueDir, 0o755) })

	// Skip if running as a user that can bypass directory permissions
	// (e.g. root in some CI containers) — os.ReadDir would still succeed.
	if _, err := os.ReadDir(queueDir); err == nil {
		t.Skip("directory permissions are not enforced for this process (likely running as root)")
	}

	_, err := runNudgeDrain(t, false)
	if err == nil {
		t.Fatal("expected an error when the nudge queue directory is unreadable")
	}
}
