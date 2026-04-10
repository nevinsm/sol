package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nevinsm/sol/internal/cliapi/escalations"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/spf13/pflag"
)

// setupEscalateTestEnv creates a SOL_HOME with a sphere store for testing.
func setupEscalateTestEnv(t *testing.T) (string, *store.SphereStore) {
	t.Helper()
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return solHome, s
}

// resetEscalateFlags resets cobra flag state so Changed() is accurate between tests.
func resetEscalateFlags() {
	escalateSource = "autarch"
	escalateSourceRef = ""
	escalateSeverity = "medium"
	escalateJSON = false
	escalateCmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
	})
}

func TestEscalateAutoSourceFromEnv(t *testing.T) {
	setupEscalateTestEnv(t)
	resetEscalateFlags()

	t.Setenv("SOL_WORLD", "sol-dev")
	t.Setenv("SOL_AGENT", "Rigel")

	rootCmd.SetArgs([]string{"escalate", "stuck on build"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("escalate failed: %v", err)
	}

	// Verify the escalation was created with auto-detected source.
	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	escs, err := s.ListEscalations("")
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) == 0 {
		t.Fatal("expected at least 1 escalation")
	}
	// Find the escalation we just created (latest by severity/time).
	found := false
	for _, e := range escs {
		if e.Description == "stuck on build" {
			if e.Source != "sol-dev/Rigel" {
				t.Fatalf("expected source 'sol-dev/Rigel', got %q", e.Source)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("escalation 'stuck on build' not found")
	}
}

func TestEscalateExplicitSourceOverridesEnv(t *testing.T) {
	setupEscalateTestEnv(t)
	resetEscalateFlags()

	t.Setenv("SOL_WORLD", "sol-dev")
	t.Setenv("SOL_AGENT", "Rigel")

	rootCmd.SetArgs([]string{"escalate", "--source", "custom-src", "test issue"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("escalate failed: %v", err)
	}

	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	escs, err := s.ListEscalations("")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range escs {
		if e.Description == "test issue" {
			if e.Source != "custom-src" {
				t.Fatalf("expected source 'custom-src', got %q", e.Source)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("escalation 'test issue' not found")
	}
}

func TestEscalateSourceRefFromTether(t *testing.T) {
	setupEscalateTestEnv(t)
	resetEscalateFlags()

	t.Setenv("SOL_WORLD", "testworld")
	t.Setenv("SOL_AGENT", "Toast")

	// Write a tether file so the auto-detection can find it.
	if err := tether.Write("testworld", "Toast", "sol-abc123def4560000", "outpost"); err != nil {
		t.Fatalf("write tether: %v", err)
	}

	rootCmd.SetArgs([]string{"escalate", "merge failed"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("escalate failed: %v", err)
	}

	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	escs, err := s.ListEscalations("")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range escs {
		if e.Description == "merge failed" {
			if e.SourceRef != "writ:sol-abc123def4560000" {
				t.Fatalf("expected source_ref 'writ:sol-abc123def4560000', got %q", e.SourceRef)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("escalation 'merge failed' not found")
	}
}

func TestEscalateExplicitSourceRefOverridesTether(t *testing.T) {
	setupEscalateTestEnv(t)
	resetEscalateFlags()

	t.Setenv("SOL_WORLD", "testworld")
	t.Setenv("SOL_AGENT", "Toast")

	// Write a tether file.
	if err := tether.Write("testworld", "Toast", "sol-abc123def4560000", "outpost"); err != nil {
		t.Fatalf("write tether: %v", err)
	}

	rootCmd.SetArgs([]string{"escalate", "--source-ref", "mr:mr-custom123", "custom ref test"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("escalate failed: %v", err)
	}

	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	escs, err := s.ListEscalations("")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range escs {
		if e.Description == "custom ref test" {
			if e.SourceRef != "mr:mr-custom123" {
				t.Fatalf("expected source_ref 'mr:mr-custom123', got %q", e.SourceRef)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("escalation 'custom ref test' not found")
	}
}

func TestEscalateMissingTetherStillCreatesEscalation(t *testing.T) {
	setupEscalateTestEnv(t)
	resetEscalateFlags()

	t.Setenv("SOL_WORLD", "nonexistent-world")
	t.Setenv("SOL_AGENT", "Ghost")

	// No tether file exists — should gracefully fall back.

	rootCmd.SetArgs([]string{"escalate", "graceful fallback test"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("escalate failed: %v", err)
	}

	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	escs, err := s.ListEscalations("")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range escs {
		if e.Description == "graceful fallback test" {
			// Source should be auto-detected from env.
			if e.Source != "nonexistent-world/Ghost" {
				t.Fatalf("expected source 'nonexistent-world/Ghost', got %q", e.Source)
			}
			// source_ref should be empty (no tether).
			if e.SourceRef != "" {
				t.Fatalf("expected empty source_ref, got %q", e.SourceRef)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("escalation 'graceful fallback test' not found")
	}
}

// TestEscalateRecordsLastNotifiedAt verifies that `sol escalate` records
// last_notified_at on the created escalation. Without this the aging loop
// would treat the escalation as never-notified and retry indefinitely
// (CF-M24).
func TestEscalateRecordsLastNotifiedAt(t *testing.T) {
	setupEscalateTestEnv(t)
	resetEscalateFlags()

	t.Setenv("SOL_WORLD", "")
	t.Setenv("SOL_AGENT", "")

	rootCmd.SetArgs([]string{"escalate", "aging loop test"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("escalate failed: %v", err)
	}

	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	escs, err := s.ListEscalations("")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range escs {
		if e.Description == "aging loop test" {
			if e.LastNotifiedAt == nil {
				t.Fatalf("expected LastNotifiedAt to be set after escalate, got nil (aging loop would spin)")
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("escalation 'aging loop test' not found")
	}
}

func TestEscalateNoEnvUsesDefaultSource(t *testing.T) {
	setupEscalateTestEnv(t)
	resetEscalateFlags()

	// Unset env vars.
	t.Setenv("SOL_WORLD", "")
	t.Setenv("SOL_AGENT", "")

	rootCmd.SetArgs([]string{"escalate", "no env test"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("escalate failed: %v", err)
	}

	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	escs, err := s.ListEscalations("")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range escs {
		if e.Description == "no env test" {
			if e.Source != "autarch" {
				t.Fatalf("expected default source 'autarch', got %q", e.Source)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("escalation 'no env test' not found")
	}
}

func TestEscalateJSONOutput(t *testing.T) {
	setupEscalateTestEnv(t)
	resetEscalateFlags()

	t.Setenv("SOL_WORLD", "sol-dev")
	t.Setenv("SOL_AGENT", "Rigel")

	// Capture stdout.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	rootCmd.SetArgs([]string{"escalate", "--json", "--severity", "high", "json test"})
	err := rootCmd.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	if err != nil {
		t.Fatalf("escalate --json failed: %v", err)
	}

	var result escalations.Escalation
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nraw: %s", err, buf.String())
	}

	if result.ID == "" {
		t.Error("expected non-empty ID in JSON output")
	}
	if result.Severity != "high" {
		t.Errorf("Severity = %q, want %q", result.Severity, "high")
	}
	if result.Status != "open" {
		t.Errorf("Status = %q, want %q", result.Status, "open")
	}
	if result.Message != "json test" {
		t.Errorf("Message = %q, want %q", result.Message, "json test")
	}
	if result.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}
