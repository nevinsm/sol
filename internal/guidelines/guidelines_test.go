package guidelines

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	valid := []string{"default", "analysis", "investigation", "my-custom", "custom_1"}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{"", "../traversal", "/absolute", ".hidden", "has space", "a/b"}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", name)
		}
	}
}

func TestReadEmbedded(t *testing.T) {
	for _, name := range []string{"default", "analysis", "investigation"} {
		data, err := readEmbedded(name)
		if err != nil {
			t.Fatalf("readEmbedded(%q) error: %v", name, err)
		}
		if len(data) == 0 {
			t.Errorf("readEmbedded(%q) returned empty content", name)
		}
		if !strings.Contains(string(data), "# Execution Guidelines") {
			t.Errorf("readEmbedded(%q) missing expected header", name)
		}
	}
}

func TestReadEmbeddedUnknown(t *testing.T) {
	_, err := readEmbedded("nonexistent")
	if err == nil {
		t.Error("readEmbedded(nonexistent) = nil, want error")
	}
}

func TestResolveTemplateName(t *testing.T) {
	tests := []struct {
		explicit string
		kind     string
		mapping  map[string]string
		want     string
	}{
		// Explicit always wins.
		{"custom", "code", nil, "custom"},
		{"custom", "analysis", map[string]string{"analysis": "deep"}, "custom"},

		// World mapping.
		{"", "analysis", map[string]string{"analysis": "deep"}, "deep"},
		{"", "research", map[string]string{"research": "deep-investigation"}, "deep-investigation"},

		// Built-in fallbacks.
		{"", "code", nil, "default"},
		{"", "", nil, "default"},
		{"", "analysis", nil, "analysis"},
		{"", "research", nil, "analysis"},

		// Kind in mapping but no match — fall through.
		{"", "code", map[string]string{"analysis": "deep"}, "default"},
	}

	for _, tt := range tests {
		got := ResolveTemplateName(tt.explicit, tt.kind, tt.mapping)
		if got != tt.want {
			t.Errorf("ResolveTemplateName(%q, %q, %v) = %q, want %q",
				tt.explicit, tt.kind, tt.mapping, got, tt.want)
		}
	}
}

func TestRender(t *testing.T) {
	tmpl := "Working on {{issue}} for branch {{base_branch}}."
	vars := map[string]string{
		"issue":       "sol-abc123",
		"base_branch": "main",
	}
	got := Render(tmpl, vars)
	want := "Working on sol-abc123 for branch main."
	if got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

// TestRenderDoesNotReSubstitute ensures that substitution values containing
// `{{x}}` markers are emitted verbatim and never re-interpreted as another
// substitution. Without atomic single-pass replacement, the result depends
// on Go's randomized map iteration order (CF-M5).
func TestRenderDoesNotReSubstitute(t *testing.T) {
	tmpl := "A={{a}} B={{b}}"
	vars := map[string]string{
		"a": "{{b}}", // value contains another marker
		"b": "{{a}}", // and vice versa
	}

	// Run many iterations: with the buggy serial loop, map iteration
	// order would yield two different results (e.g. "A={{a}} B={{a}}"
	// vs "A={{b}} B={{b}}"). With NewReplacer, the result is fixed.
	first := Render(tmpl, vars)
	want := "A={{b}} B={{a}}"
	if first != want {
		t.Errorf("Render() = %q, want %q", first, want)
	}
	for i := range 100 {
		got := Render(tmpl, vars)
		if got != first {
			t.Fatalf("Render() nondeterministic: iteration %d returned %q, first call returned %q",
				i, got, first)
		}
	}
}

func TestRenderUnknownVars(t *testing.T) {
	tmpl := "Hello {{name}}, unknown {{other}}."
	vars := map[string]string{"name": "world"}
	got := Render(tmpl, vars)
	want := "Hello world, unknown {{other}}."
	if got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

func TestResolveThreeTier(t *testing.T) {
	// Use a temp dir as SOL_HOME.
	tmpDir := t.TempDir()
	t.Setenv("SOL_HOME", tmpDir)

	// Tier 3: Embedded — should extract to user tier.
	res, err := Resolve("default", "")
	if err != nil {
		t.Fatalf("Resolve(default) error: %v", err)
	}
	if res.Tier != TierEmbedded {
		t.Errorf("Resolve(default) tier = %q, want %q", res.Tier, TierEmbedded)
	}
	if !strings.Contains(string(res.Content), "Execution Guidelines") {
		t.Error("Resolve(default) content missing expected header")
	}

	// After extraction, user tier file should exist.
	userPath := filepath.Join(tmpDir, "guidelines", "default.md")
	if _, err := os.Stat(userPath); err != nil {
		t.Errorf("expected user-tier file at %s after resolve", userPath)
	}

	// Tier 2: Now modify the user file — should resolve from user tier.
	if err := os.WriteFile(userPath, []byte("# Custom User Guidelines\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = Resolve("default", "")
	if err != nil {
		t.Fatalf("Resolve(default) after user edit: %v", err)
	}
	if res.Tier != TierUser {
		t.Errorf("Resolve(default) tier = %q, want %q", res.Tier, TierUser)
	}
	if !strings.Contains(string(res.Content), "Custom User") {
		t.Error("Resolve(default) should return user-tier content")
	}

	// Tier 1: Create project-level file.
	repoDir := t.TempDir()
	projectPath := filepath.Join(repoDir, ".sol", "guidelines", "default.md")
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte("# Project Guidelines\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = Resolve("default", repoDir)
	if err != nil {
		t.Fatalf("Resolve(default, repo) error: %v", err)
	}
	if res.Tier != TierProject {
		t.Errorf("Resolve(default, repo) tier = %q, want %q", res.Tier, TierProject)
	}
	if !strings.Contains(string(res.Content), "Project Guidelines") {
		t.Error("Resolve(default, repo) should return project-tier content")
	}
}

func TestResolveUnknown(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SOL_HOME", tmpDir)

	_, err := Resolve("nonexistent", "")
	if err == nil {
		t.Error("Resolve(nonexistent) = nil, want error")
	}
}
