package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/workflow"
)

// --- CheckWorkflowsStale tests ---

func TestCheckWorkflowsStaleNoExtracts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	results := CheckWorkflowsStale()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	r := results[0]
	if !r.Passed || r.Warning {
		t.Errorf("expected Passed=true, Warning=false, got %+v", r)
	}
	if r.Name != "workflows:stale" {
		t.Errorf("Name = %q, want %q", r.Name, "workflows:stale")
	}
}

func TestCheckWorkflowsStaleHandEditedWarns(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Extract the embedded default normally (writes files + per-file stamps).
	res, err := workflow.Resolve("code-review", "")
	if err != nil {
		t.Fatalf("Resolve(code-review): %v", err)
	}
	manifestPath := filepath.Join(res.Path, "manifest.toml")

	// Simulate an operator hand-edit: content no longer matches either the
	// embedded template or the recorded stamp hash.
	if err := os.WriteFile(manifestPath, []byte("# My Custom Manifest\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckWorkflowsStale()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	r := results[0]
	if !r.Passed || !r.Warning {
		t.Errorf("expected Passed=true, Warning=true, got %+v", r)
	}
	if r.Name != "workflows:stale:code-review:manifest.toml" {
		t.Errorf("Name = %q, want %q", r.Name, "workflows:stale:code-review:manifest.toml")
	}
	if !strings.Contains(r.Message, "hand-edited") {
		t.Errorf("Message should mention hand-editing: %q", r.Message)
	}
	if !strings.Contains(r.Message, manifestPath) {
		t.Errorf("Message should reference the file path: %q", r.Message)
	}
	if r.Fix == "" {
		t.Error("expected a non-empty Fix with remediation guidance")
	}
	if !strings.Contains(r.Fix, "eject") && !strings.Contains(r.Fix, "merge") {
		t.Errorf("Fix should suggest diff/eject/merge remediation: %q", r.Fix)
	}
	if r.Remediate != nil {
		t.Error("workflows:stale must never auto-fix — hand-edited content is operator data")
	}
}

func TestCheckWorkflowsStaleLegacyUnverifiableWarns(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	res, err := workflow.Resolve("code-review", "")
	if err != nil {
		t.Fatalf("Resolve(code-review): %v", err)
	}

	// A pre-stamping extract: hand-edit a file, then drop the stamp sidecar
	// entirely to simulate a directory extracted before per-file stamping.
	manifestPath := filepath.Join(res.Path, "manifest.toml")
	if err := os.WriteFile(manifestPath, []byte("# Legacy Custom Manifest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(res.Path, ".stamps.json")); err != nil {
		t.Fatal(err)
	}

	results := CheckWorkflowsStale()
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

func TestCheckWorkflowsStaleUpToDateNoWarning(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if _, err := workflow.Resolve("code-review", ""); err != nil {
		t.Fatalf("Resolve(code-review): %v", err)
	}

	results := CheckWorkflowsStale()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	if results[0].Warning {
		t.Errorf("freshly-extracted, untouched extract should not warn: %+v", results[0])
	}
}

func TestCheckWorkflowsStaleUserOwnedWorkflowNotChecked(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// A user-created workflow with the same name as an embedded one but no
	// version marker — out of scope for this check entirely.
	userDir := workflow.Dir("code-review")
	if err := os.MkdirAll(filepath.Join(userDir, "steps"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "name = \"code-review\"\ntype = \"workflow\"\ndescription = \"custom\"\n"
	if err := os.WriteFile(filepath.Join(userDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckWorkflowsStale()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	if results[0].Warning {
		t.Errorf("user-owned workflow directory should not be flagged: %+v", results[0])
	}
}

func TestCheckWorkflowsStaleWiredIntoRunAll(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	report := RunAll()
	found := false
	for _, c := range report.Checks {
		if c.Name == "workflows:stale" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RunAll() to include a workflows:stale check")
	}
}
