package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/guidelines"
)

// --- CheckGuidelinesStale tests ---

func TestCheckGuidelinesStaleNoExtracts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	results := CheckGuidelinesStale()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	r := results[0]
	if !r.Passed || r.Warning {
		t.Errorf("expected Passed=true, Warning=false, got %+v", r)
	}
	if r.Name != "guidelines:stale" {
		t.Errorf("Name = %q, want %q", r.Name, "guidelines:stale")
	}
}

func TestCheckGuidelinesStaleCustomizedWarns(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Extract the embedded default normally (writes file + stamp)...
	if _, err := guidelines.Resolve("default", ""); err != nil {
		t.Fatalf("Resolve(default): %v", err)
	}
	userPath := filepath.Join(dir, "guidelines", "default.md")

	// ...then simulate an operator customization: content no longer matches
	// either the embedded template or the recorded stamp hash.
	if err := os.WriteFile(userPath, []byte("# My Custom Guidelines\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckGuidelinesStale()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	r := results[0]
	if !r.Passed || !r.Warning {
		t.Errorf("expected Passed=true, Warning=true, got %+v", r)
	}
	if r.Name != "guidelines:stale:default" {
		t.Errorf("Name = %q, want %q", r.Name, "guidelines:stale:default")
	}
	if !strings.Contains(r.Message, "customized") {
		t.Errorf("Message should mention customization: %q", r.Message)
	}
	if !strings.Contains(r.Message, userPath) {
		t.Errorf("Message should reference the extract path: %q", r.Message)
	}
	if r.Fix == "" {
		t.Error("expected a non-empty Fix with remediation guidance")
	}
	if !strings.Contains(r.Fix, "delete") && !strings.Contains(r.Fix, "merge") {
		t.Errorf("Fix should suggest diff/delete/merge remediation: %q", r.Fix)
	}
	if r.Remediate != nil {
		t.Error("guidelines:stale must never auto-fix — customized content is operator data")
	}
}

func TestCheckGuidelinesStaleLegacyUnverifiableWarns(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// A pre-stamping extract: file exists, differs from embedded, but no
	// .stamps.json sidecar was ever written for it.
	userPath := filepath.Join(dir, "guidelines", "default.md")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("# Legacy Extract\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckGuidelinesStale()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	r := results[0]
	if !r.Passed || !r.Warning {
		t.Errorf("expected Passed=true, Warning=true, got %+v", r)
	}
	if !strings.Contains(r.Message, "unverifiable") {
		t.Errorf("Message should flag this as unverifiable: %q", r.Message)
	}
}

func TestCheckGuidelinesStaleUpToDateNoWarning(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if _, err := guidelines.Resolve("default", ""); err != nil {
		t.Fatalf("Resolve(default): %v", err)
	}

	results := CheckGuidelinesStale()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	if results[0].Warning {
		t.Errorf("freshly-extracted, untouched extract should not warn: %+v", results[0])
	}
}

func TestCheckGuidelinesStaleUnreadableStampFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	stampsPath := filepath.Join(dir, "guidelines", ".stamps.json")
	if err := os.MkdirAll(filepath.Dir(stampsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stampsPath, []byte("not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckGuidelinesStale()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	r := results[0]
	if r.Passed {
		t.Errorf("expected Passed=false when the stamp sidecar can't be parsed, got %+v", r)
	}
}

func TestCheckGuidelinesStaleWiredIntoRunAll(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	report := RunAll()
	found := false
	for _, c := range report.Checks {
		if c.Name == "guidelines:stale" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RunAll() to include a guidelines:stale check")
	}
}
