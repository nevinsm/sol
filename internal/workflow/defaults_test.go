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

func TestInitWorkflowType(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	dir, err := Init("my-test", "workflow", "", false)
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	expectedDir := filepath.Join(solHome, "workflows", "my-test")
	if dir != expectedDir {
		t.Errorf("dir: got %q, want %q", dir, expectedDir)
	}

	// Check manifest.toml exists and contains correct content.
	manifestData, err := os.ReadFile(filepath.Join(dir, "manifest.toml"))
	if err != nil {
		t.Fatalf("read manifest.toml: %v", err)
	}
	manifest := string(manifestData)
	if !strings.Contains(manifest, `name = "my-test"`) {
		t.Errorf("manifest missing name field")
	}
	if !strings.Contains(manifest, `type = "workflow"`) {
		t.Errorf("manifest missing type field")
	}
	if !strings.Contains(manifest, `id = "start"`) {
		t.Errorf("manifest missing step definition")
	}
	if !strings.Contains(manifest, `instructions = "steps/01-start.md"`) {
		t.Errorf("manifest missing instructions field")
	}

	// Check steps/ directory and placeholder step file.
	stepPath := filepath.Join(dir, "steps", "01-start.md")
	if _, err := os.Stat(stepPath); os.IsNotExist(err) {
		t.Errorf("step file %q not created", stepPath)
	}

	// Validate the manifest can be loaded and is valid.
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}
	if err := Validate(m); err != nil {
		t.Errorf("Validate() error: %v", err)
	}
}

func TestInitExpansionType(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	dir, err := Init("my-expansion", "expansion", "", false)
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Check manifest.toml content.
	manifestData, err := os.ReadFile(filepath.Join(dir, "manifest.toml"))
	if err != nil {
		t.Fatalf("read manifest.toml: %v", err)
	}
	manifest := string(manifestData)
	if !strings.Contains(manifest, `name = "my-expansion"`) {
		t.Errorf("manifest missing name field")
	}
	if !strings.Contains(manifest, `type = "expansion"`) {
		t.Errorf("manifest missing type field")
	}
	if !strings.Contains(manifest, `[[template]]`) {
		t.Errorf("manifest missing template section")
	}

	// No steps/ directory for expansion type.
	stepsDir := filepath.Join(dir, "steps")
	if _, err := os.Stat(stepsDir); !os.IsNotExist(err) {
		t.Errorf("steps/ directory should not exist for expansion type")
	}

	// Validate the manifest can be loaded.
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}
	if err := Validate(m); err != nil {
		t.Errorf("Validate() error: %v", err)
	}
}

func TestInitConvoyType(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	dir, err := Init("my-convoy", "convoy", "", false)
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Check manifest.toml content.
	manifestData, err := os.ReadFile(filepath.Join(dir, "manifest.toml"))
	if err != nil {
		t.Fatalf("read manifest.toml: %v", err)
	}
	manifest := string(manifestData)
	if !strings.Contains(manifest, `name = "my-convoy"`) {
		t.Errorf("manifest missing name field")
	}
	if !strings.Contains(manifest, `type = "convoy"`) {
		t.Errorf("manifest missing type field")
	}
	if !strings.Contains(manifest, `[[legs]]`) {
		t.Errorf("manifest missing legs section")
	}
	if !strings.Contains(manifest, `[synthesis]`) {
		t.Errorf("manifest missing synthesis section")
	}

	// No steps/ directory for convoy type.
	stepsDir := filepath.Join(dir, "steps")
	if _, err := os.Stat(stepsDir); !os.IsNotExist(err) {
		t.Errorf("steps/ directory should not exist for convoy type")
	}

	// Validate the manifest can be loaded.
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}
	if err := Validate(m); err != nil {
		t.Errorf("Validate() error: %v", err)
	}
}

func TestInitProjectTier(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	repoPath := t.TempDir()

	dir, err := Init("proj-workflow", "workflow", repoPath, true)
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	expectedDir := filepath.Join(repoPath, ".sol", "workflows", "proj-workflow")
	if dir != expectedDir {
		t.Errorf("dir: got %q, want %q", dir, expectedDir)
	}

	// Verify manifest exists.
	if _, err := os.Stat(filepath.Join(dir, "manifest.toml")); os.IsNotExist(err) {
		t.Errorf("manifest.toml not created in project tier")
	}

	// Verify steps/ directory exists for workflow type.
	if _, err := os.Stat(filepath.Join(dir, "steps", "01-start.md")); os.IsNotExist(err) {
		t.Errorf("step file not created in project tier")
	}
}

func TestInitErrorsOnExistingDirectory(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Create the first workflow successfully.
	_, err := Init("existing", "workflow", "", false)
	if err != nil {
		t.Fatalf("first Init() error: %v", err)
	}

	// Second attempt should fail.
	_, err = Init("existing", "workflow", "", false)
	if err == nil {
		t.Fatal("Init() expected error for existing directory")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestInitErrorsOnInvalidName(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	invalidNames := []string{
		"../escape",
		".hidden",
		"foo/bar",
		"",
		"-leading-hyphen",
	}
	for _, name := range invalidNames {
		t.Run(name, func(t *testing.T) {
			_, err := Init(name, "workflow", "", false)
			if err == nil {
				t.Errorf("Init(%q) expected error for invalid name", name)
			}
		})
	}
}

func TestInitProjectRequiresRepoPath(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	_, err := Init("test-proj", "workflow", "", true)
	if err == nil {
		t.Fatal("Init() expected error when project=true without repoPath")
	}
	if !strings.Contains(err.Error(), "--project requires --world") {
		t.Errorf("error should mention --project requires --world, got: %v", err)
	}
}

func TestInitInvalidType(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	_, err := Init("test-bad-type", "invalid", "", false)
	if err == nil {
		t.Fatal("Init() expected error for invalid type")
	}
	if !strings.Contains(err.Error(), "invalid workflow type") {
		t.Errorf("error should mention invalid workflow type, got: %v", err)
	}
}

func TestShowFromPath(t *testing.T) {
	// Create a workflow at an arbitrary path and load it via LoadManifest.
	dir := t.TempDir()

	// Write a valid workflow manifest.
	manifest := `name = "path-test"
type = "workflow"
description = "A test workflow"

[[steps]]
id = "start"
title = "Start"
instructions = "steps/01-start.md"
`
	if err := os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// Load and validate.
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}
	if m.Name != "path-test" {
		t.Errorf("name: got %q, want %q", m.Name, "path-test")
	}
	if err := Validate(m); err != nil {
		t.Errorf("Validate() error: %v", err)
	}

	// Verify TierLocal constant is usable.
	res := &Resolution{Path: dir, Tier: TierLocal}
	if res.Tier != "local" {
		t.Errorf("tier: got %q, want %q", res.Tier, "local")
	}
}

func TestShowFromPathInvalidManifest(t *testing.T) {
	dir := t.TempDir()

	// Write an invalid manifest (convoy without synthesis).
	manifest := `name = "bad-convoy"
type = "convoy"
description = "Missing synthesis"

[[legs]]
id = "first"
title = "First"
description = ""
`
	if err := os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}

	err = Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for convoy without synthesis")
	}
	if !strings.Contains(err.Error(), "synthesis") {
		t.Errorf("error should mention synthesis, got: %v", err)
	}
}

func TestShowFromPathMissingManifest(t *testing.T) {
	dir := t.TempDir()

	_, err := LoadManifest(dir)
	if err == nil {
		t.Fatal("LoadManifest() expected error for missing manifest.toml")
	}
}
