package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/cliapi/mail"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
)

func TestResolveMailIdentity(t *testing.T) {
	tests := []struct {
		name      string
		flagValue string
		solAgent  string
		solWorld  string
		expected  string
	}{
		{
			name:      "explicit flag value takes precedence",
			flagValue: "explicit-identity",
			solAgent:  "Nova",
			solWorld:  "sol-dev",
			expected:  "explicit-identity",
		},
		{
			name:      "world/agent from env vars when flag empty",
			flagValue: "",
			solAgent:  "Nova",
			solWorld:  "sol-dev",
			expected:  "sol-dev/Nova",
		},
		{
			name:      "autarch when env vars unset",
			flagValue: "",
			solAgent:  "",
			solWorld:  "",
			expected:  config.Autarch,
		},
		{
			name:      "autarch when only SOL_AGENT set",
			flagValue: "",
			solAgent:  "Nova",
			solWorld:  "",
			expected:  config.Autarch,
		},
		{
			name:      "autarch when only SOL_WORLD set",
			flagValue: "",
			solAgent:  "",
			solWorld:  "sol-dev",
			expected:  config.Autarch,
		},
		{
			name:      "explicit autarch flag returned as-is",
			flagValue: config.Autarch,
			solAgent:  "Nova",
			solWorld:  "sol-dev",
			expected:  config.Autarch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SOL_AGENT", tt.solAgent)
			t.Setenv("SOL_WORLD", tt.solWorld)
			got := resolveMailIdentity(tt.flagValue)
			if got != tt.expected {
				t.Errorf("resolveMailIdentity(%q) with SOL_AGENT=%q SOL_WORLD=%q = %q, want %q",
					tt.flagValue, tt.solAgent, tt.solWorld, got, tt.expected)
			}
		})
	}
}

func TestCanonicalizeRecipient(t *testing.T) {
	tests := []struct {
		name      string
		to        string
		worldHint string
		expected  string
	}{
		{
			name:      "autarch stays plain",
			to:        "autarch",
			worldHint: "sol-dev",
			expected:  "autarch",
		},
		{
			name:      "world/agent format preserved",
			to:        "ember/Toast",
			worldHint: "sol-dev",
			expected:  "ember/Toast",
		},
		{
			name:      "plain name with world hint becomes world/agent",
			to:        "Toast",
			worldHint: "sol-dev",
			expected:  "sol-dev/Toast",
		},
		{
			name:      "plain name without world hint returned as-is",
			to:        "Toast",
			worldHint: "",
			expected:  "Toast",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalizeRecipient(tt.to, tt.worldHint)
			if got != tt.expected {
				t.Errorf("canonicalizeRecipient(%q, %q) = %q, want %q",
					tt.to, tt.worldHint, got, tt.expected)
			}
		})
	}
}

func TestResolveMailIdentitySenderAutoDetect(t *testing.T) {
	// When SOL_AGENT and SOL_WORLD are set, sender should be world/agent.
	t.Setenv("SOL_AGENT", "Polaris")
	t.Setenv("SOL_WORLD", "sol-dev")
	got := resolveMailIdentity("")
	if got != "sol-dev/Polaris" {
		t.Errorf("expected sender sol-dev/Polaris, got %q", got)
	}
}

func TestResolveMailIdentitySenderFallsBackToAutarch(t *testing.T) {
	// When env vars are unset, sender should be autarch.
	t.Setenv("SOL_AGENT", "")
	t.Setenv("SOL_WORLD", "")
	got := resolveMailIdentity("")
	if got != config.Autarch {
		t.Errorf("expected sender %q, got %q", config.Autarch, got)
	}
}

// setupMailTestEnv creates an isolated SOL_HOME with a sphere store.
func setupMailTestEnv(t *testing.T) *store.SphereStore {
	t.Helper()
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	t.Setenv("SOL_AGENT", "")
	t.Setenv("SOL_WORLD", "")
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMailReadWarnsMismatchedRecipient(t *testing.T) {
	s := setupMailTestEnv(t)

	// Send a message addressed to "sol-dev/OtherAgent".
	msgID, err := s.SendMessage(config.Autarch, "sol-dev/OtherAgent", "Hello", "body text", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}

	// Intercept stderr.
	r, w, _ := os.Pipe()
	origStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	// Run mail read as "sol-dev/CallerAgent" — recipient mismatch should warn.
	t.Setenv("SOL_AGENT", "CallerAgent")
	t.Setenv("SOL_WORLD", "sol-dev")
	rootCmd.SetArgs([]string{"mail", "read", msgID})
	// Ignore the error (message may still be read successfully).
	rootCmd.Execute() //nolint:errcheck

	w.Close()
	os.Stderr = origStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	stderrOutput := buf.String()

	if !strings.Contains(stderrOutput, "warning: message") {
		t.Errorf("expected warning in stderr about recipient mismatch, got: %q", stderrOutput)
	}
	if !strings.Contains(stderrOutput, "sol-dev/OtherAgent") {
		t.Errorf("expected 'sol-dev/OtherAgent' in warning, got: %q", stderrOutput)
	}
	if !strings.Contains(stderrOutput, "sol-dev/CallerAgent") {
		t.Errorf("expected 'sol-dev/CallerAgent' in warning, got: %q", stderrOutput)
	}
}

func TestMailReadNoWarnMatchingRecipient(t *testing.T) {
	s := setupMailTestEnv(t)

	// Send a message addressed to "sol-dev/MyAgent".
	msgID, err := s.SendMessage(config.Autarch, "sol-dev/MyAgent", "Hello", "body text", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}

	// Intercept stderr.
	r, w, _ := os.Pipe()
	origStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	// Run mail read as the same identity — no warning expected.
	t.Setenv("SOL_AGENT", "MyAgent")
	t.Setenv("SOL_WORLD", "sol-dev")
	rootCmd.SetArgs([]string{"mail", "read", msgID})
	rootCmd.Execute() //nolint:errcheck

	w.Close()
	os.Stderr = origStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	stderrOutput := buf.String()

	if strings.Contains(stderrOutput, "warning: message") {
		t.Errorf("unexpected warning in stderr when recipient matches: %q", stderrOutput)
	}
}

// TestMailSendPlainRecipientSOLWORLD_NudgeFires verifies that when a plain agent
// name is sent with no --world flag but SOL_WORLD is set, bridgeMailToNudge
// receives the canonicalized "world/agent" form and does not bail with a world
// resolution error.
func TestMailSendPlainRecipientSOLWORLD_NudgeFires(t *testing.T) {
	setupMailTestEnv(t)
	t.Setenv("SOL_WORLD", "test-world")

	// Intercept stderr to verify no "skipping nudge" error.
	r, w, _ := os.Pipe()
	origStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	rootCmd.SetArgs([]string{"mail", "send", "--to=PlainAgent", "--subject=Test nudge"})
	_ = rootCmd.Execute()

	w.Close()
	os.Stderr = origStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	stderrOutput := buf.String()

	// Before the fix, bridgeMailToNudge received "PlainAgent" (no world),
	// failed to resolve world, and printed "skipping nudge".
	// After the fix, it receives "test-world/PlainAgent", splits correctly,
	// and silently skips (no active session) without printing the error.
	if strings.Contains(stderrOutput, "skipping nudge") {
		t.Errorf("expected nudge path to proceed silently, got: %q", stderrOutput)
	}
}

// TestMailSendRejectsNonCanonicalRecipient verifies that `mail send` refuses
// to persist a recipient that lacks a "world/" prefix when no --world flag or
// SOL_WORLD env var is provided. The store must remain empty.
func TestMailSendRejectsNonCanonicalRecipient(t *testing.T) {
	s := setupMailTestEnv(t)

	rootCmd.SetArgs([]string{"mail", "send", "--to=foo", "--subject=hi", "--body=bye"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error from mail send with no world prefix, got nil")
	}
	if !strings.Contains(err.Error(), "world prefix") {
		t.Errorf("expected error to mention world prefix, got: %v", err)
	}

	// Verify no row was written for the bogus recipient.
	msgs, err := s.Inbox("foo")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected no rows for non-canonical recipient, got %d", len(msgs))
	}
}

// TestMailSendAutarchNoWorldPrefix verifies that sending to "autarch" works
// without any world context.
func TestMailSendAutarchNoWorldPrefix(t *testing.T) {
	s := setupMailTestEnv(t)

	rootCmd.SetArgs([]string{"mail", "send", "--to=autarch", "--subject=hi", "--body=bye"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs, err := s.Inbox(config.Autarch)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message in autarch inbox, got %d", len(msgs))
	}
}

// TestMailSendCanonicalRecipientPersists verifies that an explicit
// "world/agent" recipient is stored as-is.
func TestMailSendCanonicalRecipientPersists(t *testing.T) {
	s := setupMailTestEnv(t)

	rootCmd.SetArgs([]string{"mail", "send", "--to=myworld/Toast", "--subject=hi", "--body=bye"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs, err := s.Inbox("myworld/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message for myworld/Toast, got %d", len(msgs))
	}
}

// TestMailSendBodyFile verifies --body-file reads the message body from a
// file, per confirmed fix #6's extension to sol mail send --body.
func TestMailSendBodyFile(t *testing.T) {
	s := setupMailTestEnv(t)
	// mailSendCmd is a package-level singleton: reset both --body and
	// --body-file, since an earlier test in this file may have left --body
	// set, which would otherwise trip the mutual-exclusion check below.
	mailSendCmd.Flags().Set("body", "")
	t.Cleanup(func() {
		mailSendCmd.Flags().Set("body", "")
		mailSendCmd.Flags().Set("body-file", "")
	})

	bodyPath := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(bodyPath, []byte("a long message body\nspanning lines\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"mail", "send", "--to=myworld/Toast", "--subject=hi", "--body-file", bodyPath})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("mail send --body-file: %v", err)
	}

	msgs, err := s.Inbox("myworld/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for myworld/Toast, got %d", len(msgs))
	}
	want := "a long message body\nspanning lines"
	if msgs[0].Body != want {
		t.Errorf("body = %q, want %q", msgs[0].Body, want)
	}
}

// TestMailSendBodyMutuallyExclusiveWithFile verifies --body and --body-file
// cannot both be set.
func TestMailSendBodyMutuallyExclusiveWithFile(t *testing.T) {
	setupMailTestEnv(t)
	t.Cleanup(func() { mailSendCmd.Flags().Set("body-file", "") })

	rootCmd.SetArgs([]string{"mail", "send", "--to=myworld/Toast", "--subject=hi", "--body=inline", "--body-file=/nonexistent/whatever.txt"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when both --body and --body-file are set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutually exclusive error, got: %v", err)
	}
}

// TestMailSendWorldFlagCanonicalizes verifies --world prefixes a plain agent.
func TestMailSendWorldFlagCanonicalizes(t *testing.T) {
	s := setupMailTestEnv(t)

	rootCmd.SetArgs([]string{"mail", "send", "--world=myworld", "--to=Toast", "--subject=hi", "--body=bye"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs, err := s.Inbox("myworld/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message for myworld/Toast via --world flag, got %d", len(msgs))
	}
}

// TestBridgeMailToNudgeMalformedRecipient verifies that bridgeMailToNudge
// does not panic on a malformed (non-canonical) recipient and instead logs
// a warning to stderr.
func TestBridgeMailToNudgeMalformedRecipient(t *testing.T) {
	cases := []string{"foo", "", "/agent", "world/"}
	for _, to := range cases {
		t.Run(to, func(t *testing.T) {
			r, w, _ := os.Pipe()
			origStderr := os.Stderr
			os.Stderr = w
			defer func() { os.Stderr = origStderr }()

			// Must not panic.
			bridgeMailToNudge(to, "subj", "body", 2)

			w.Close()
			os.Stderr = origStderr

			var buf bytes.Buffer
			buf.ReadFrom(r)
			out := buf.String()
			if !strings.Contains(out, "non-canonical recipient") {
				t.Errorf("expected non-canonical warning for %q, got: %q", to, out)
			}
		})
	}
}

// readMailEvents reads $SOL_HOME/.events.jsonl and returns events whose
// Type matches eventType. Missing file yields nil.
func readMailEvents(t *testing.T, solHome, eventType string) []events.Event {
	t.Helper()
	f, err := os.Open(filepath.Join(solHome, ".events.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("open events file: %v", err)
	}
	defer f.Close()

	var out []events.Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var ev events.Event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type == eventType {
			out = append(out, ev)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan events file: %v", err)
	}
	return out
}

func TestResolveVia(t *testing.T) {
	tests := []struct {
		name      string
		flagValue string
		solVia    string
		expected  string
	}{
		{"explicit flag takes precedence", "cli-tool", "env-tool", "cli-tool"},
		{"falls back to SOL_VIA when flag empty", "", "env-tool", "env-tool"},
		{"empty when both unset", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SOL_VIA", tt.solVia)
			got := resolveVia(tt.flagValue)
			if got != tt.expected {
				t.Errorf("resolveVia(%q) with SOL_VIA=%q = %q, want %q", tt.flagValue, tt.solVia, got, tt.expected)
			}
		})
	}
}

func TestValidateVia(t *testing.T) {
	tests := []struct {
		name    string
		via     string
		wantErr bool
	}{
		{"empty is valid (no origin channel)", "", false},
		{"simple name is valid", "notify-bridge", false},
		{"name with dots and underscores is valid", "ci_pipeline.v2", false},
		{"slash is rejected (compound identity)", "world/agent", true},
		{"leading digit is rejected", "1bridge", true},
		{"space is rejected", "notify bridge", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateVia(tt.via)
			if tt.wantErr && err == nil {
				t.Errorf("validateVia(%q): expected error, got nil", tt.via)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateVia(%q): unexpected error: %v", tt.via, err)
			}
		})
	}
}

// TestMailSendViaFlagOverridesEnv verifies --via takes precedence over
// SOL_VIA and that the recorded via round-trips through the store.
func TestMailSendViaFlagOverridesEnv(t *testing.T) {
	s := setupMailTestEnv(t)
	t.Setenv("SOL_VIA", "env-tool")
	t.Cleanup(func() { mailSendCmd.Flags().Set("via", "") })

	rootCmd.SetArgs([]string{"mail", "send", "--to=myworld/Toast", "--subject=hi", "--body=bye", "--via=cli-tool"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs, err := s.Inbox("myworld/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Via != "cli-tool" {
		t.Errorf("expected via 'cli-tool', got %q", msgs[0].Via)
	}
}

// TestMailSendRejectsSlashVia verifies --via containing "/" is rejected —
// compound identities are explicitly out of scope per ADR-0043.
func TestMailSendRejectsSlashVia(t *testing.T) {
	s := setupMailTestEnv(t)
	t.Cleanup(func() { mailSendCmd.Flags().Set("via", "") })

	rootCmd.SetArgs([]string{"mail", "send", "--to=myworld/Toast", "--subject=hi", "--body=bye", "--via=world/tool"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for --via containing '/', got nil")
	}
	if !strings.Contains(err.Error(), "--via") {
		t.Errorf("expected error to mention --via, got: %v", err)
	}

	msgs, err := s.Inbox("myworld/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected no rows written for rejected via, got %d", len(msgs))
	}
}

// TestMailSendThreadExplicit verifies --thread is stored as given.
func TestMailSendThreadExplicit(t *testing.T) {
	s := setupMailTestEnv(t)
	t.Cleanup(func() { mailSendCmd.Flags().Set("thread", "") })

	rootCmd.SetArgs([]string{"mail", "send", "--to=myworld/Toast", "--subject=hi", "--body=bye", "--thread=thread-abc"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs, err := s.Inbox("myworld/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].ThreadID != "thread-abc" {
		t.Errorf("expected thread_id 'thread-abc', got %q", msgs[0].ThreadID)
	}
}

// TestMailSendThreadTwiceWhilePendingSucceeds is the CLI-level repro for
// the bug this writ fixes: `sol mail send --thread=<id>` a second time
// before the first message in that thread is acked used to fail with
// "failed to send message: constraint failed: UNIQUE constraint failed:
// messages.thread_id" (exit 1). Both sends here target the same thread
// while both stay pending — the exact sequence that was broken.
func TestMailSendThreadTwiceWhilePendingSucceeds(t *testing.T) {
	s := setupMailTestEnv(t)
	t.Cleanup(func() { mailSendCmd.Flags().Set("thread", "") })

	rootCmd.SetArgs([]string{"mail", "send", "--to=myworld/Toast", "--subject=hi", "--body=first", "--thread=thread-cli-multi"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error on first send: %v", err)
	}

	rootCmd.SetArgs([]string{"mail", "send", "--to=myworld/Toast", "--subject=hi", "--body=second", "--thread=thread-cli-multi"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error on second send into the same pending thread: %v", err)
	}

	msgs, err := s.ListMessages(store.MessageFilters{ThreadID: "thread-cli-multi", Delivery: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 pending messages in the thread, got %d", len(msgs))
	}
}

// TestMailSendThreadAutoAssignedFromID verifies that omitting --thread
// makes sol assign the message's own ID as its thread — the "fresh thread
// id" ADR-0043 requires, and the JSON output reflects the resolved value.
func TestMailSendThreadAutoAssignedFromID(t *testing.T) {
	s := setupMailTestEnv(t)

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"mail", "send", "--to=myworld/Toast", "--subject=hi", "--body=bye", "--json"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	var got struct {
		ID       string `json:"id"`
		ThreadID string `json:"thread_id"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput: %s", err, out)
	}
	if got.ThreadID != got.ID {
		t.Errorf("expected auto-assigned thread_id to equal id %q, got %q", got.ID, got.ThreadID)
	}

	msgs, err := s.Inbox("myworld/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].ThreadID != msgs[0].ID {
		t.Fatalf("expected stored thread_id to equal message id, got msgs=%+v", msgs)
	}
}

// TestMailSendOmitsViaFromJSONWhenEmpty verifies the "empty via = omitted
// in JSON" rule (ADR-0043 decision 1 as implemented by this writ).
func TestMailSendOmitsViaFromJSONWhenEmpty(t *testing.T) {
	setupMailTestEnv(t)

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"mail", "send", "--to=myworld/Toast", "--subject=hi", "--body=bye", "--json"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if strings.Contains(out, `"via"`) {
		t.Errorf("expected via field to be omitted from JSON when empty, got: %s", out)
	}
}

// TestMailSendEmitsMailSentEvent verifies `mail send` emits EventMailSent
// (ADR-0043 decision 3) with sender/recipient/subject/via/thread_id/id.
func TestMailSendEmitsMailSentEvent(t *testing.T) {
	s := setupMailTestEnv(t)
	solHome := os.Getenv("SOL_HOME")
	t.Cleanup(func() { mailSendCmd.Flags().Set("via", "") })

	rootCmd.SetArgs([]string{"mail", "send", "--to=myworld/Toast", "--subject=Ping", "--body=bye", "--via=bridge-tool", "--thread=thread-ev-1"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs, err := s.Inbox("myworld/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	msgID := msgs[0].ID

	evs := readMailEvents(t, solHome, events.EventMailSent)
	if len(evs) != 1 {
		t.Fatalf("expected 1 mail_sent event, got %d", len(evs))
	}
	payload, ok := evs[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", evs[0].Payload)
	}
	want := map[string]string{
		"id":        msgID,
		"sender":    config.Autarch,
		"recipient": "myworld/Toast",
		"subject":   "Ping",
		"via":       "bridge-tool",
		"thread_id": "thread-ev-1",
	}
	for k, v := range want {
		if payload[k] != v {
			t.Errorf("payload[%q] = %v, want %q", k, payload[k], v)
		}
	}
}

// TestMailReadShowsViaAndThread verifies the human `mail read` output
// includes Via and Thread lines, blank when via is unset (ADR-0043: "empty
// via = ... blank in human output").
func TestMailReadShowsViaAndThread(t *testing.T) {
	s := setupMailTestEnv(t)

	withVia, err := s.SendMessageWithOrigin(config.Autarch, "sol-dev/MyAgent", "Hello", "body", 2, "notification", "bridge-tool", "thread-read-1")
	if err != nil {
		t.Fatal(err)
	}
	noVia, err := s.SendMessageWithOrigin(config.Autarch, "sol-dev/MyAgent", "Hello2", "body2", 2, "notification", "", "")
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("SOL_AGENT", "MyAgent")
	t.Setenv("SOL_WORLD", "sol-dev")

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"mail", "read", withVia})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "Via:     bridge-tool\n") {
		t.Errorf("expected via line in output, got: %q", out)
	}
	if !strings.Contains(out, "Thread:  thread-read-1\n") {
		t.Errorf("expected thread line in output, got: %q", out)
	}

	out2 := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"mail", "read", noVia})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out2, "Via:     \n") {
		t.Errorf("expected blank via line in output, got: %q", out2)
	}
}

func TestMailReadJSON(t *testing.T) {
	s := setupMailTestEnv(t)

	id, err := s.SendMessageWithOrigin(config.Autarch, "sol-dev/MyAgent", "Hello", "body text", 2, "notification", "bridge-tool", "thread-read-json")
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("SOL_AGENT", "MyAgent")
	t.Setenv("SOL_WORLD", "sol-dev")

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"mail", "read", id, "--json"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	var msg mail.Message
	if err := json.Unmarshal([]byte(out), &msg); err != nil {
		t.Fatalf("expected JSON output, got %q: %v", out, err)
	}
	if msg.ID != id {
		t.Errorf("ID = %q, want %q", msg.ID, id)
	}
	if msg.Via != "bridge-tool" {
		t.Errorf("Via = %q, want %q", msg.Via, "bridge-tool")
	}
	if msg.ThreadID != "thread-read-json" {
		t.Errorf("ThreadID = %q, want %q", msg.ThreadID, "thread-read-json")
	}
	if msg.ReadAt == nil {
		t.Error("ReadAt = nil, want set (read marks the message read)")
	}
}

// TestMailReadCrossIdentityLeavesReadFalse verifies a cross-identity "mail
// read" does not consume unread state: the message's actual recipient must
// still see it as unread (read=0) afterward.
func TestMailReadCrossIdentityLeavesReadFalse(t *testing.T) {
	s := setupMailTestEnv(t)
	t.Cleanup(func() { resetMailReadAckFlags(t) })

	msgID, err := s.SendMessage(config.Autarch, "sol-dev/OtherAgent", "Hello", "body", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("SOL_AGENT", "CallerAgent")
	t.Setenv("SOL_WORLD", "sol-dev")

	rootCmd.SetArgs([]string{"mail", "read", msgID})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg, err := s.GetMessage(msgID)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Read {
		t.Error("expected read=0 to survive a cross-identity read")
	}
}

// TestMailReadSameIdentityMarksRead verifies the non-mismatch path still
// marks the message read, unlike the cross-identity case above.
func TestMailReadSameIdentityMarksRead(t *testing.T) {
	s := setupMailTestEnv(t)
	t.Cleanup(func() { resetMailReadAckFlags(t) })

	msgID, err := s.SendMessage(config.Autarch, "sol-dev/MyAgent", "Hello", "body", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("SOL_AGENT", "MyAgent")
	t.Setenv("SOL_WORLD", "sol-dev")

	rootCmd.SetArgs([]string{"mail", "read", msgID})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg, err := s.GetMessage(msgID)
	if err != nil {
		t.Fatal(err)
	}
	if !msg.Read {
		t.Error("expected read=1 after a same-identity read")
	}
}

// resetMailReadAckFlags restores mailReadCmd's and mailAckCmd's persistent
// pflag state to defaults between tests, mirroring resetMailArchiveFlags.
func resetMailReadAckFlags(t *testing.T) {
	t.Helper()
	mailReadCmd.Flags().Set("identity", "")
	mailReadCmd.Flags().Set("json", "false")
	mailAckCmd.Flags().Set("identity", "")
	mailAckCmd.Flags().Set("json", "false")
}

// --- mail ack ---

// TestMailAckRefusesCrossIdentityWithoutIdentityFlag verifies acking a
// message addressed to another identity is refused (exit 1) and leaves the
// message unacked, unless the caller is the autarch.
func TestMailAckRefusesCrossIdentityWithoutIdentityFlag(t *testing.T) {
	s := setupMailTestEnv(t)
	t.Cleanup(func() { resetMailReadAckFlags(t) })

	msgID, err := s.SendMessage(config.Autarch, "sol-dev/OtherAgent", "Hello", "body", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("SOL_AGENT", "CallerAgent")
	t.Setenv("SOL_WORLD", "sol-dev")

	rootCmd.SetArgs([]string{"mail", "ack", msgID})
	err = rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error acking a message belonging to another identity")
	}
	if !strings.Contains(err.Error(), "sol-dev/OtherAgent") {
		t.Errorf("expected error naming the recipient, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "--identity=sol-dev/OtherAgent") {
		t.Errorf("expected error suggesting --identity=<recipient>, got %q", err.Error())
	}

	msg, err := s.GetMessage(msgID)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Delivery != "pending" {
		t.Errorf("expected delivery to remain 'pending', got %q", msg.Delivery)
	}
	if msg.Read {
		t.Error("expected a refused ack to leave read=0 (no side effect)")
	}
}

// TestMailAckSucceedsWithExplicitMatchingIdentity verifies passing
// --identity=<recipient> lets the caller ack on that identity's behalf.
func TestMailAckSucceedsWithExplicitMatchingIdentity(t *testing.T) {
	s := setupMailTestEnv(t)
	t.Cleanup(func() { resetMailReadAckFlags(t) })

	msgID, err := s.SendMessage(config.Autarch, "sol-dev/OtherAgent", "Hello", "body", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("SOL_AGENT", "CallerAgent")
	t.Setenv("SOL_WORLD", "sol-dev")

	rootCmd.SetArgs([]string{"mail", "ack", msgID, "--identity=sol-dev/OtherAgent"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg, err := s.GetMessage(msgID)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Delivery != "acked" {
		t.Errorf("expected delivery 'acked', got %q", msg.Delivery)
	}
}

// TestMailAckAutarchCanAckAnyMessage verifies the autarch identity keeps
// universal ack access, mirroring "mail archive"'s precedent.
func TestMailAckAutarchCanAckAnyMessage(t *testing.T) {
	s := setupMailTestEnv(t)
	t.Cleanup(func() { resetMailReadAckFlags(t) })

	msgID, err := s.SendMessage("sol-dev/Someone", "sol-dev/OtherAgent", "Hello", "body", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}

	// No SOL_AGENT/SOL_WORLD set -> resolves to autarch.
	rootCmd.SetArgs([]string{"mail", "ack", msgID})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg, err := s.GetMessage(msgID)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Delivery != "acked" {
		t.Errorf("expected delivery 'acked', got %q", msg.Delivery)
	}
}

// TestMailAckSameIdentitySucceeds verifies the ordinary (non-mismatch) ack
// path still works.
func TestMailAckSameIdentitySucceeds(t *testing.T) {
	s := setupMailTestEnv(t)
	t.Cleanup(func() { resetMailReadAckFlags(t) })

	msgID, err := s.SendMessage(config.Autarch, "sol-dev/MyAgent", "Hello", "body", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("SOL_AGENT", "MyAgent")
	t.Setenv("SOL_WORLD", "sol-dev")

	rootCmd.SetArgs([]string{"mail", "ack", msgID})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg, err := s.GetMessage(msgID)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Delivery != "acked" {
		t.Errorf("expected delivery 'acked', got %q", msg.Delivery)
	}
}

// TestMailThreadReturnsAllMessagesInOrder verifies `mail thread` prints
// every message in a thread chronologically, regardless of read status,
// and does not mutate read state (a pure read).
func TestMailThreadReturnsAllMessagesInOrder(t *testing.T) {
	s := setupMailTestEnv(t)

	// Ack the first before sending the second, mirroring a real
	// back-and-forth conversation. Not required by the store (multiple
	// pending messages per thread_id are allowed since sphere schema v19),
	// just a realistic conversation shape for this test.
	firstID, err := s.SendMessageWithThread("sol-dev/Nova", "autarch", "First", "body1", 2, "notification", "thread-cli-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AckMessage(firstID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendMessageWithThread("autarch", "sol-dev/Nova", "Second", "body2", 2, "notification", "thread-cli-1"); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SOL_AGENT", "Nova")
	t.Setenv("SOL_WORLD", "sol-dev")

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"mail", "thread", "thread-cli-1"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	firstIdx := strings.Index(out, "First")
	secondIdx := strings.Index(out, "Second")
	if firstIdx == -1 || secondIdx == -1 || firstIdx > secondIdx {
		t.Fatalf("expected First before Second in chronological order, got: %q", out)
	}
	if !strings.Contains(out, "body1") || !strings.Contains(out, "body2") {
		t.Fatalf("expected both message bodies in output, got: %q", out)
	}

	// Reading the thread must not mark messages as read.
	msgs, err := s.Thread("thread-cli-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if m.Read {
			t.Errorf("message %s marked read by thread view; thread view must be a pure read", m.ID)
		}
	}
}

// --- envoyWakeEligible (wake-on-mail gating) unit tests ---

// TestEnvoyWakeEligiblePriorityGate verifies priority 3 (low) is rejected
// before any sphere store lookup happens — no SOL_HOME/.store is set up
// here, so a store open attempt would fail loudly if the priority gate
// didn't short-circuit first.
func TestEnvoyWakeEligiblePriorityGate(t *testing.T) {
	if envoyWakeEligible("world", "agent", 3) {
		t.Error("expected priority 3 (low) to never be wake-eligible")
	}
}

func TestEnvoyWakeEligibleEnvoyRolePriority1And2(t *testing.T) {
	s := setupMailTestEnv(t)
	if _, err := s.CreateAgent("Envoy1", "world", "envoy"); err != nil {
		t.Fatal(err)
	}

	for _, p := range []int{1, 2} {
		if !envoyWakeEligible("world", "Envoy1", p) {
			t.Errorf("expected envoy recipient to be wake-eligible at priority %d", p)
		}
	}
}

// TestMailThreadAccessRuleDeniesNonParticipant verifies the caller must be a
// sender or recipient of at least one message in the thread; otherwise the
// command exits 1 as "not found".
func TestMailThreadAccessRuleDeniesNonParticipant(t *testing.T) {
	s := setupMailTestEnv(t)

	_, err := s.SendMessageWithThread("sol-dev/Nova", "autarch", "Private", "secret", 2, "notification", "thread-cli-2")
	if err != nil {
		t.Fatal(err)
	}

	// Caller is neither sender nor recipient of any message in the thread.
	t.Setenv("SOL_AGENT", "Toast")
	t.Setenv("SOL_WORLD", "sol-dev")

	rootCmd.SetArgs([]string{"mail", "thread", "thread-cli-2"})
	err = rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-participant caller, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error containing 'not found', got %q", err.Error())
	}
}

// TestMailThreadUnknownExitsNotFound verifies an unknown thread ID returns
// an error (exit 1 via main.go's default error path).
func TestMailThreadUnknownExitsNotFound(t *testing.T) {
	setupMailTestEnv(t)

	t.Setenv("SOL_AGENT", "Nova")
	t.Setenv("SOL_WORLD", "sol-dev")

	rootCmd.SetArgs([]string{"mail", "thread", "thread-does-not-exist"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error for unknown thread, got nil")
	}
}

// TestMailThreadJSON verifies --json outputs the message array shape.
func TestMailThreadJSON(t *testing.T) {
	s := setupMailTestEnv(t)

	id1, err := s.SendMessageWithThread("sol-dev/Nova", "autarch", "First", "body1", 2, "notification", "thread-cli-json")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AckMessage(id1); err != nil {
		t.Fatal(err)
	}
	id2, err := s.SendMessageWithThread("autarch", "sol-dev/Nova", "Second", "body2", 2, "notification", "thread-cli-json")
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("SOL_AGENT", "Nova")
	t.Setenv("SOL_WORLD", "sol-dev")

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"mail", "thread", "thread-cli-json", "--json"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	var msgs []mail.Message
	if err := json.Unmarshal([]byte(out), &msgs); err != nil {
		t.Fatalf("expected JSON array output, got %q: %v", out, err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].ID != id1 || msgs[1].ID != id2 {
		t.Fatalf("expected chronological order [%s %s], got [%s %s]", id1, id2, msgs[0].ID, msgs[1].ID)
	}
}

// TestEnvoyWakeEligibleOutpostRoleRejected verifies outposts are never
// wake-eligible regardless of priority — outpost lifecycle is exclusively
// cast/dispatch-owned.
func TestEnvoyWakeEligibleOutpostRoleRejected(t *testing.T) {
	s := setupMailTestEnv(t)
	if _, err := s.CreateAgent("Out1", "world", "outpost"); err != nil {
		t.Fatal(err)
	}

	if envoyWakeEligible("world", "Out1", 1) {
		t.Error("expected outpost recipient to never be wake-eligible")
	}
}

// TestEnvoyWakeEligibleUnknownAgentRejected verifies an unresolvable
// recipient (not registered in the sphere store) is treated as "do not
// wake" rather than erroring.
func TestEnvoyWakeEligibleUnknownAgentRejected(t *testing.T) {
	setupMailTestEnv(t)

	if envoyWakeEligible("world", "Ghost", 1) {
		t.Error("expected unknown recipient to never be wake-eligible")
	}
}

// --- mail archive ---

// resetMailArchiveFlags restores mailArchiveCmd's and mailInboxCmd's
// persistent pflag state to defaults. mailArchiveCmd/mailInboxCmd are
// package-level cobra command singletons, so a flag value set by one test
// (e.g. --thread, --all) otherwise leaks into the next test's Execute call.
func resetMailArchiveFlags(t *testing.T) {
	t.Helper()
	mailArchiveCmd.Flags().Set("thread", "")
	mailArchiveCmd.Flags().Set("unarchive", "false")
	mailArchiveCmd.Flags().Set("json", "false")
	mailArchiveCmd.Flags().Set("identity", "")
	mailInboxCmd.Flags().Set("all", "false")
}

// TestMailArchiveHidesThreadFromInboxAndAllShowsIt verifies "mail archive
// --thread=..." removes the thread from "mail inbox" and "mail inbox --all"
// still surfaces it.
func TestMailArchiveHidesThreadFromInboxAndAllShowsIt(t *testing.T) {
	s := setupMailTestEnv(t)
	resetMailArchiveFlags(t)
	t.Cleanup(func() { resetMailArchiveFlags(t) })

	if _, err := s.SendMessageWithThread("sol-dev/Nova", "autarch", "Test", "body", 2, "notification", "thread-cmd-arc-1"); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"mail", "archive", "--thread=thread-cmd-arc-1"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs, err := s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected archived thread excluded from inbox, got %d", len(msgs))
	}

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"mail", "inbox", "--all"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "Test") {
		t.Errorf("expected --all to show the archived message, got: %q", out)
	}
}

// TestMailArchiveUnreadDoesNotCountInCheck verifies design point 4:
// archiving a thread with an unread message removes it from "mail check"'s
// unread count.
func TestMailArchiveUnreadDoesNotCountInCheck(t *testing.T) {
	s := setupMailTestEnv(t)
	resetMailArchiveFlags(t)
	t.Cleanup(func() { resetMailArchiveFlags(t) })

	if _, err := s.SendMessageWithThread("sol-dev/Nova", "autarch", "Test", "body", 2, "notification", "thread-cmd-arc-2"); err != nil {
		t.Fatal(err)
	}

	// Before archiving: check reports unread (exit 0).
	rootCmd.SetArgs([]string{"mail", "check"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected exit 0 (unread present) before archive: %v", err)
	}

	rootCmd.SetArgs([]string{"mail", "archive", "--thread=thread-cmd-arc-2"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error archiving: %v", err)
	}

	// After archiving: check reports no unread (exit 1).
	rootCmd.SetArgs([]string{"mail", "check"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected exit 1 (no unread) after archiving the only thread")
	}
	if ExitCode(err) != 1 {
		t.Errorf("expected exit code 1, got %d (%v)", ExitCode(err), err)
	}
}

// TestMailArchiveUnarchiveRestoresListing verifies --unarchive reverses a
// prior archive.
func TestMailArchiveUnarchiveRestoresListing(t *testing.T) {
	s := setupMailTestEnv(t)
	resetMailArchiveFlags(t)
	t.Cleanup(func() { resetMailArchiveFlags(t) })

	if _, err := s.SendMessageWithThread("sol-dev/Nova", "autarch", "Test", "body", 2, "notification", "thread-cmd-arc-3"); err != nil {
		t.Fatal(err)
	}
	rootCmd.SetArgs([]string{"mail", "archive", "--thread=thread-cmd-arc-3"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error archiving: %v", err)
	}

	rootCmd.SetArgs([]string{"mail", "archive", "--thread=thread-cmd-arc-3", "--unarchive"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error unarchiving: %v", err)
	}

	msgs, err := s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected thread restored to inbox after unarchive, got %d messages", len(msgs))
	}
}

// TestMailArchiveThreadViewStillShowsArchivedContent verifies "mail thread"
// (a pure read) is unaffected by archiving.
func TestMailArchiveThreadViewStillShowsArchivedContent(t *testing.T) {
	s := setupMailTestEnv(t)
	resetMailArchiveFlags(t)
	t.Cleanup(func() { resetMailArchiveFlags(t) })

	if _, err := s.SendMessageWithThread("sol-dev/Nova", "autarch", "Test", "secret body", 2, "notification", "thread-cmd-arc-4"); err != nil {
		t.Fatal(err)
	}
	rootCmd.SetArgs([]string{"mail", "archive", "--thread=thread-cmd-arc-4"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error archiving: %v", err)
	}

	t.Setenv("SOL_AGENT", "Nova")
	t.Setenv("SOL_WORLD", "sol-dev")

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"mail", "thread", "thread-cmd-arc-4"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "secret body") {
		t.Errorf("expected archived thread content still visible via thread view, got: %q", out)
	}
}

// TestMailArchiveAuthorizationDeniedForNonParticipant verifies the caller
// must be a sender/recipient of the thread (or autarch); otherwise the
// command reports "not found" and archives nothing.
func TestMailArchiveAuthorizationDeniedForNonParticipant(t *testing.T) {
	s := setupMailTestEnv(t)
	resetMailArchiveFlags(t)
	t.Cleanup(func() { resetMailArchiveFlags(t) })

	if _, err := s.SendMessageWithThread("sol-dev/Nova", "sol-dev/Owner", "Private", "secret", 2, "notification", "thread-cmd-arc-5"); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SOL_AGENT", "Toast")
	t.Setenv("SOL_WORLD", "sol-dev")

	rootCmd.SetArgs([]string{"mail", "archive", "--thread=thread-cmd-arc-5"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-participant caller, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error containing 'not found', got %q", err.Error())
	}

	// Verify nothing was archived.
	msgs, err := s.Thread("thread-cmd-arc-5")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].ArchivedAt != nil {
		t.Fatalf("expected thread to remain unarchived after denied access, got %+v", msgs)
	}
}

// TestMailArchiveAutarchCanArchiveAnyThread verifies the autarch override:
// unlike "mail thread"'s participant-only rule, autarch may archive a
// thread it is not a sender/recipient of.
func TestMailArchiveAutarchCanArchiveAnyThread(t *testing.T) {
	s := setupMailTestEnv(t)
	resetMailArchiveFlags(t)
	t.Cleanup(func() { resetMailArchiveFlags(t) })

	if _, err := s.SendMessageWithThread("sol-dev/Nova", "sol-dev/Owner", "Between agents", "body", 2, "notification", "thread-cmd-arc-6"); err != nil {
		t.Fatal(err)
	}

	// No SOL_AGENT/SOL_WORLD set -> caller resolves to autarch.
	rootCmd.SetArgs([]string{"mail", "archive", "--thread=thread-cmd-arc-6"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected autarch to archive any thread, got error: %v", err)
	}

	msgs, err := s.Thread("thread-cmd-arc-6")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].ArchivedAt == nil {
		t.Fatalf("expected thread archived by autarch, got %+v", msgs)
	}
}

// TestMailArchiveUnknownThreadExitsNotFound verifies an unknown thread ID
// is reported as "not found" (exit 1).
func TestMailArchiveUnknownThreadExitsNotFound(t *testing.T) {
	setupMailTestEnv(t)
	resetMailArchiveFlags(t)
	t.Cleanup(func() { resetMailArchiveFlags(t) })

	rootCmd.SetArgs([]string{"mail", "archive", "--thread=thread-does-not-exist"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown thread, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error containing 'not found', got %q", err.Error())
	}
}

// TestMailArchiveMissingThreadFlag verifies --thread is required.
func TestMailArchiveMissingThreadFlag(t *testing.T) {
	setupMailTestEnv(t)
	resetMailArchiveFlags(t)
	t.Cleanup(func() { resetMailArchiveFlags(t) })

	rootCmd.SetArgs([]string{"mail", "archive"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --thread is omitted, got nil")
	}
}

// TestMailArchiveJSON verifies --json output shape.
func TestMailArchiveJSON(t *testing.T) {
	s := setupMailTestEnv(t)
	resetMailArchiveFlags(t)
	t.Cleanup(func() { resetMailArchiveFlags(t) })

	if _, err := s.SendMessageWithThread("sol-dev/Nova", "autarch", "Test", "body", 2, "notification", "thread-cmd-arc-json"); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"mail", "archive", "--thread=thread-cmd-arc-json", "--json"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	var got struct {
		ThreadID string `json:"thread_id"`
		Archived bool   `json:"archived"`
		Messages int64  `json:"messages"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput: %s", err, out)
	}
	if got.ThreadID != "thread-cmd-arc-json" {
		t.Errorf("thread_id = %q, want %q", got.ThreadID, "thread-cmd-arc-json")
	}
	if !got.Archived {
		t.Error("expected archived=true")
	}
	if got.Messages != 1 {
		t.Errorf("messages = %d, want 1", got.Messages)
	}
}

// --- mail purge --archived/--older-than ---

// TestMailPurgeArchivedDeletesArchivedThread verifies "mail purge
// --archived --confirm" deletes an archived, unacked message.
func TestMailPurgeArchivedDeletesArchivedThread(t *testing.T) {
	s := setupMailTestEnv(t)
	resetMailArchiveFlags(t)
	t.Cleanup(func() {
		resetMailArchiveFlags(t)
		mailPurgeCmd.Flags().Set("archived", "false")
		mailPurgeCmd.Flags().Set("older-than", "")
		mailPurgeCmd.Flags().Set("confirm", "false")
	})

	if _, err := s.SendMessageWithThread("agent1", "autarch", "Test", "", 2, "notification", "thread-cmd-purge-1"); err != nil {
		t.Fatal(err)
	}
	rootCmd.SetArgs([]string{"mail", "archive", "--thread=thread-cmd-purge-1"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error archiving: %v", err)
	}

	// Without --confirm: preview only, nothing deleted, exit 1.
	rootCmd.SetArgs([]string{"mail", "purge", "--archived"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected exit 1 for preview without --confirm")
	}
	if ExitCode(err) != 1 {
		t.Errorf("expected exit code 1, got %d", ExitCode(err))
	}
	all, err := s.InboxAll("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected message to survive dry-run preview, got %d", len(all))
	}

	rootCmd.SetArgs([]string{"mail", "purge", "--archived", "--confirm"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	all, err = s.InboxAll("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("expected archived message purged, got %d remaining", len(all))
	}
}

// TestMailPurgeOlderThanRequiresArchived verifies --older-than without
// --archived is rejected.
func TestMailPurgeOlderThanRequiresArchived(t *testing.T) {
	setupMailTestEnv(t)
	t.Cleanup(func() { mailPurgeCmd.Flags().Set("older-than", "") })

	rootCmd.SetArgs([]string{"mail", "purge", "--older-than=30d"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "--older-than requires --archived") {
		t.Errorf("expected error about --older-than requiring --archived, got %q", err.Error())
	}
}

// TestMailPurgeRequiresASelector verifies purge still refuses to run with
// no selector at all (pre-existing invariant, now covering --archived too).
func TestMailPurgeRequiresASelector(t *testing.T) {
	setupMailTestEnv(t)

	rootCmd.SetArgs([]string{"mail", "purge"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "must specify") {
		t.Errorf("expected 'must specify' error, got %q", err.Error())
	}
}

// TestMailPurgeArchivedAndAllAckedComposeByIntersection verifies passing
// both --all-acked and --archived only deletes messages matching both.
func TestMailPurgeArchivedAndAllAckedComposeByIntersection(t *testing.T) {
	s := setupMailTestEnv(t)
	resetMailArchiveFlags(t)
	t.Cleanup(func() {
		resetMailArchiveFlags(t)
		mailPurgeCmd.Flags().Set("archived", "false")
		mailPurgeCmd.Flags().Set("all-acked", "false")
		mailPurgeCmd.Flags().Set("confirm", "false")
	})

	// Archived but not acked.
	if _, err := s.SendMessageWithThread("agent1", "autarch", "Archived only", "", 2, "notification", "thread-cmd-purge-2"); err != nil {
		t.Fatal(err)
	}
	rootCmd.SetArgs([]string{"mail", "archive", "--thread=thread-cmd-purge-2"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error archiving: %v", err)
	}

	rootCmd.SetArgs([]string{"mail", "purge", "--all-acked", "--archived", "--confirm"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	all, err := s.InboxAll("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected archived-but-unacked message to survive an --all-acked --archived purge, got %d remaining", len(all))
	}
}

// TestMailPurgeDismissedPreviewAndConfirm verifies "mail purge --dismissed"
// previews without --confirm (exit 1, nothing deleted) and deletes with
// --confirm.
func TestMailPurgeDismissedPreviewAndConfirm(t *testing.T) {
	s := setupMailTestEnv(t)
	t.Cleanup(func() {
		mailPurgeCmd.Flags().Set("dismissed", "false")
		mailPurgeCmd.Flags().Set("confirm", "false")
	})

	msgID, err := s.SendMessage("agent1", "autarch", "Test", "", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DismissMessage(msgID); err != nil {
		t.Fatal(err)
	}

	// Without --confirm: preview only, nothing deleted, exit 1.
	rootCmd.SetArgs([]string{"mail", "purge", "--dismissed"})
	err = rootCmd.Execute()
	if err == nil {
		t.Fatal("expected exit 1 for preview without --confirm")
	}
	if ExitCode(err) != 1 {
		t.Errorf("expected exit code 1, got %d", ExitCode(err))
	}
	all, err := s.ListMessages(store.MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected message to survive dry-run preview, got %d", len(all))
	}

	rootCmd.SetArgs([]string{"mail", "purge", "--dismissed", "--confirm"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	all, err = s.ListMessages(store.MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("expected dismissed message purged, got %d remaining", len(all))
	}
}

// TestMailPurgeDismissedNotTouchedByOtherSelectorsAlone verifies a
// dismissed message survives --all-acked/--before/--archived used without
// --dismissed.
func TestMailPurgeDismissedNotTouchedByOtherSelectorsAlone(t *testing.T) {
	s := setupMailTestEnv(t)
	resetMailArchiveFlags(t)
	t.Cleanup(func() {
		resetMailArchiveFlags(t)
		mailPurgeCmd.Flags().Set("all-acked", "false")
		mailPurgeCmd.Flags().Set("archived", "false")
		mailPurgeCmd.Flags().Set("confirm", "false")
	})

	msgID, err := s.SendMessage("agent1", "autarch", "Test", "", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DismissMessage(msgID); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"mail", "purge", "--all-acked", "--archived", "--confirm"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	all, err := s.ListMessages(store.MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected dismissed message to survive --all-acked --archived purge, got %d remaining", len(all))
	}
}

func TestParseHumanDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		wantErr  bool
	}{
		// Standard Go durations.
		{"24h", 24 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"1h30m", 90 * time.Minute, false},

		// Day-based durations.
		{"7d", 7 * 24 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},

		// Days + standard suffix.
		{"7d12h", 7*24*time.Hour + 12*time.Hour, false},
		{"1d6h30m", 24*time.Hour + 6*time.Hour + 30*time.Minute, false},

		// Invalid inputs.
		{"", 0, true},
		{"abc", 0, true},
		{"d", 0, true},
		{"7x", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseHumanDuration(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Fatalf("parseHumanDuration(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
