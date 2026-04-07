package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
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
			name:     "explicit flag value takes precedence",
			flagValue: "explicit-identity",
			solAgent: "Nova",
			solWorld: "sol-dev",
			expected: "explicit-identity",
		},
		{
			name:     "world/agent from env vars when flag empty",
			flagValue: "",
			solAgent: "Nova",
			solWorld: "sol-dev",
			expected: "sol-dev/Nova",
		},
		{
			name:     "autarch when env vars unset",
			flagValue: "",
			solAgent: "",
			solWorld: "",
			expected: config.Autarch,
		},
		{
			name:     "autarch when only SOL_AGENT set",
			flagValue: "",
			solAgent: "Nova",
			solWorld: "",
			expected: config.Autarch,
		},
		{
			name:     "autarch when only SOL_WORLD set",
			flagValue: "",
			solAgent: "",
			solWorld: "sol-dev",
			expected: config.Autarch,
		},
		{
			name:     "explicit autarch flag returned as-is",
			flagValue: config.Autarch,
			solAgent: "Nova",
			solWorld: "sol-dev",
			expected: config.Autarch,
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
