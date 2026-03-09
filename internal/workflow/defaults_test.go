package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	valid := []string{
		"standard",
		"my-workflow",
		"v2_build",
		"default-work",
		"A",
		"rule-of-five",
		"code-review",
		"thorough-work",
		"idea-to-plan",
		"deep-scan",
	}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []struct {
		name string
		desc string
	}{
		{"../escape", "dot-dot traversal"},
		{"../../etc/passwd", "multi-level traversal"},
		{"foo/bar", "forward slash"},
		{"foo\\bar", "backslash"},
		{".hidden", "leading dot"},
		{"..sneaky", "leading double dot"},
		{"", "empty string"},
		{"-leading-hyphen", "leading hyphen"},
		{"_leading-underscore", "leading underscore"},
		{"hello world", "space in name"},
		{"name\ttab", "tab in name"},
		{"with.dot", "dot in middle"},
	}
	for _, tc := range invalid {
		t.Run(tc.desc, func(t *testing.T) {
			err := ValidateName(tc.name)
			if err == nil {
				t.Errorf("ValidateName(%q) = nil, want error (%s)", tc.name, tc.desc)
			}
		})
	}
}

func TestEjectToUserTier(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	targetDir, err := Eject("code-review", "", false)
	if err != nil {
		t.Fatalf("Eject() error: %v", err)
	}

	expectedDir := filepath.Join(solHome, "workflows", "code-review")
	if targetDir != expectedDir {
		t.Errorf("Eject() returned %q, want %q", targetDir, expectedDir)
	}

	// Verify manifest.toml was created.
	manifestPath := filepath.Join(targetDir, "manifest.toml")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Errorf("manifest.toml not found at %s", manifestPath)
	}

	// Verify the ejected workflow is loadable via Resolve.
	res, err := Resolve("code-review", "")
	if err != nil {
		t.Fatalf("Resolve() after eject error: %v", err)
	}
	if res.Tier != TierUser {
		t.Errorf("Resolve() tier = %q, want %q", res.Tier, TierUser)
	}
}

func TestEjectToProjectTier(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	repoDir := t.TempDir()

	targetDir, err := Eject("code-review", repoDir, false)
	if err != nil {
		t.Fatalf("Eject() error: %v", err)
	}

	expectedDir := filepath.Join(repoDir, ".sol", "workflows", "code-review")
	if targetDir != expectedDir {
		t.Errorf("Eject() returned %q, want %q", targetDir, expectedDir)
	}

	// Verify manifest.toml was created.
	manifestPath := filepath.Join(targetDir, "manifest.toml")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Errorf("manifest.toml not found at %s", manifestPath)
	}

	// Verify the ejected workflow resolves from project tier.
	res, err := Resolve("code-review", repoDir)
	if err != nil {
		t.Fatalf("Resolve() after eject error: %v", err)
	}
	if res.Tier != TierProject {
		t.Errorf("Resolve() tier = %q, want %q", res.Tier, TierProject)
	}
}

func TestEjectNonEmbeddedWorkflow(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	_, err := Eject("nonexistent", "", false)
	if err == nil {
		t.Fatal("Eject() expected error for non-embedded workflow, got nil")
	}
	if !strings.Contains(err.Error(), "not an embedded workflow") {
		t.Errorf("Eject() error = %q, want error containing 'not an embedded workflow'", err.Error())
	}
}

func TestEjectExistingWithoutForce(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// First eject.
	_, err := Eject("code-review", "", false)
	if err != nil {
		t.Fatalf("first Eject() error: %v", err)
	}

	// Second eject without force should error.
	_, err = Eject("code-review", "", false)
	if err == nil {
		t.Fatal("Eject() expected error when target exists, got nil")
	}
	if !strings.Contains(err.Error(), "workflow directory already exists") {
		t.Errorf("Eject() error = %q, want error containing 'workflow directory already exists'", err.Error())
	}
}

func TestEjectWithForce(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// First eject.
	targetDir, err := Eject("code-review", "", false)
	if err != nil {
		t.Fatalf("first Eject() error: %v", err)
	}

	// Write a marker file to the ejected directory so we can verify it gets backed up.
	markerPath := filepath.Join(targetDir, "custom-marker.txt")
	if err := os.WriteFile(markerPath, []byte("custom"), 0o644); err != nil {
		t.Fatalf("failed to write marker: %v", err)
	}

	// Eject with force.
	targetDir2, err := Eject("code-review", "", true)
	if err != nil {
		t.Fatalf("Eject(force=true) error: %v", err)
	}
	if targetDir2 != targetDir {
		t.Errorf("Eject(force) returned %q, want %q", targetDir2, targetDir)
	}

	// New directory should have manifest.toml but NOT the custom marker.
	if _, err := os.Stat(filepath.Join(targetDir2, "manifest.toml")); os.IsNotExist(err) {
		t.Error("manifest.toml not found after force eject")
	}
	if _, err := os.Stat(filepath.Join(targetDir2, "custom-marker.txt")); !os.IsNotExist(err) {
		t.Error("custom-marker.txt should not exist in fresh eject")
	}

	// Backup directory should exist with .bak- prefix.
	parentDir := filepath.Dir(targetDir)
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		t.Fatalf("failed to read parent dir: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "code-review.bak-") {
			found = true
			// Verify the backup contains the marker file.
			backupMarker := filepath.Join(parentDir, e.Name(), "custom-marker.txt")
			if _, err := os.Stat(backupMarker); os.IsNotExist(err) {
				t.Error("backup directory does not contain custom-marker.txt")
			}
			break
		}
	}
	if !found {
		t.Error("no backup directory found with .bak- prefix")
	}
}

func TestEjectedWorkflowResolvesFromCorrectTier(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Eject to user tier.
	_, err := Eject("code-review", "", false)
	if err != nil {
		t.Fatalf("Eject() error: %v", err)
	}

	// Resolve should find it at user tier, not embedded.
	res, err := Resolve("code-review", "")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if res.Tier != TierUser {
		t.Errorf("Resolve() tier = %q, want %q", res.Tier, TierUser)
	}

	// Verify it shows as user tier in List.
	entries, err := List("")
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	for _, e := range entries {
		if e.Name == "code-review" && !e.Shadowed {
			if e.Tier != TierUser {
				t.Errorf("List() code-review tier = %q, want %q", e.Tier, TierUser)
			}
			break
		}
	}
}

func TestResolveRejectsTraversal(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	cases := []string{
		"../escape",
		"../../etc/passwd",
		"foo/bar",
		".hidden",
		"foo\\bar",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Resolve(name, "")
			if err == nil {
				t.Errorf("Resolve(%q, \"\") = nil error, want validation error", name)
			}
		})
	}
}
