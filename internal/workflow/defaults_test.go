package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/stamp"
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

func TestInitExpansionTypeRejected(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	_, err := Init("my-expansion", "expansion", "", false)
	if err == nil {
		t.Fatal("Init() expected error for expansion type")
	}
	if !strings.Contains(err.Error(), "invalid workflow type") {
		t.Errorf("error should mention invalid workflow type, got: %v", err)
	}
}

func TestInitConvoyTypeRejected(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	_, err := Init("my-convoy", "convoy", "", false)
	if err == nil {
		t.Fatal("Init() expected error for convoy type")
	}
	if !strings.Contains(err.Error(), "invalid workflow type") {
		t.Errorf("error should mention invalid workflow type, got: %v", err)
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

	// Write a manifest with a deprecated type.
	manifest := `name = "bad-convoy"
type = "convoy"
description = "Deprecated type"
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
		t.Fatal("Validate() expected error for convoy type")
	}
	if !strings.Contains(err.Error(), "no longer supported") {
		t.Errorf("error should mention 'no longer supported', got: %v", err)
	}
}

func TestShowFromPathMissingManifest(t *testing.T) {
	dir := t.TempDir()

	_, err := LoadManifest(dir)
	if err == nil {
		t.Fatal("LoadManifest() expected error for missing manifest.toml")
	}
}

// readWorkflowStamps is a test helper that reads and decodes the per-file
// stamp sidecar directly (bypassing stamp.Load, which tolerates a missing
// file) so tests can assert on its exact on-disk contents.
func readWorkflowStamps(t *testing.T, workflowDir string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(stampsFilePath(workflowDir))
	if err != nil {
		t.Fatalf("failed to read stamp file: %v", err)
	}
	var stamps map[string]string
	if err := json.Unmarshal(data, &stamps); err != nil {
		t.Fatalf("failed to parse stamp file: %v", err)
	}
	return stamps
}

// tamperVersionMarker forces the next Resolve to treat workflowDir as stale
// without actually changing the (fixed, compiled-in) embedded content —
// there's no way to compile two different embedded versions of the same
// workflow into one test binary, so every scenario here simulates "the
// embedded template moved on" by writing a bogus marker value directly,
// exactly like a real stale marker left behind by a binary upgrade.
func tamperVersionMarker(t *testing.T, workflowDir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(workflowDir, embeddedVersionFile), []byte("stale-hash"), 0o644); err != nil {
		t.Fatalf("failed to tamper version marker: %v", err)
	}
}

// TestResolveExtractionStampsEveryFile verifies that a fresh auto-extraction
// (Tier 3) records a stamp for every embedded file, not just a
// directory-level marker.
func TestResolveExtractionStampsEveryFile(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	res, err := Resolve("code-review", "")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	embedded, err := embeddedFileMap("code-review")
	if err != nil {
		t.Fatalf("embeddedFileMap() error: %v", err)
	}
	if len(embedded) == 0 {
		t.Fatal("expected code-review to have embedded files")
	}

	stamps := readWorkflowStamps(t, res.Path)
	for rel, data := range embedded {
		want := hashFor(data)
		if got := stamps[rel]; got != want {
			t.Errorf("stamp[%q] = %q, want %q", rel, got, want)
		}
	}
}

// TestResolveUntouchedDirectoryRefreshesTransparently verifies the clean
// case (nothing hand-edited) still behaves like the old "stale → refresh"
// path: a stale marker triggers a refresh, every file ends up matching the
// current embedded content, and the marker and per-file stamps are brought
// up to date — with no wholesale delete-and-recreate.
func TestResolveUntouchedDirectoryRefreshesTransparently(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	res, err := Resolve("code-review", "")
	if err != nil {
		t.Fatalf("first Resolve() error: %v", err)
	}
	manifestPath := filepath.Join(res.Path, "manifest.toml")
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest before refresh: %v", err)
	}

	tamperVersionMarker(t, res.Path)

	res2, err := Resolve("code-review", "")
	if err != nil {
		t.Fatalf("second Resolve() error: %v", err)
	}
	if res2.Tier != TierEmbedded {
		t.Errorf("second resolve tier: got %q, want %q", res2.Tier, TierEmbedded)
	}

	after, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest after refresh: %v", err)
	}
	if string(before) != string(after) {
		t.Error("untouched manifest.toml content changed unexpectedly")
	}

	stored, err := os.ReadFile(filepath.Join(res.Path, embeddedVersionFile))
	if err != nil {
		t.Fatalf("read version marker: %v", err)
	}
	if string(stored) != embeddedHash("code-review") {
		t.Error("version marker was not brought up to date after refresh")
	}

	stamps := readWorkflowStamps(t, res.Path)
	if got := stamps["manifest.toml"]; got != hashFor(after) {
		t.Errorf("manifest.toml stamp = %q, want %q", got, hashFor(after))
	}
}

// TestResolveRefreshesUntouchedFileToCurrentEmbedded proves untouched
// extracts still track embedded updates automatically: a file whose disk
// content matches its recorded stamp (i.e. hasn't been hand-edited since
// sol last wrote it) but no longer matches the current embedded content is
// overwritten with the current embedded content, exactly like the old
// directory-level behavior did for the whole directory — just scoped to the
// one file that's actually stale.
func TestResolveRefreshesUntouchedFileToCurrentEmbedded(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	res, err := Resolve("code-review", "")
	if err != nil {
		t.Fatalf("first Resolve() error: %v", err)
	}

	// Simulate "this file was extracted from an older embedded version and
	// never touched since": disk content is some old value, and the stamp
	// records that same old value (as it would have right after that older
	// extraction). Neither matches the current real embedded content.
	manifestPath := filepath.Join(res.Path, "manifest.toml")
	oldContent := []byte("name = \"code-review\"\ntype = \"workflow\"\n# old version\n")
	if err := os.WriteFile(manifestPath, oldContent, 0o644); err != nil {
		t.Fatalf("write old content: %v", err)
	}
	stamps := readWorkflowStamps(t, res.Path)
	stamps["manifest.toml"] = hashFor(oldContent)
	if err := saveWorkflowStamps(t, res.Path, stamps); err != nil {
		t.Fatalf("save stamps: %v", err)
	}

	tamperVersionMarker(t, res.Path)

	if _, err := Resolve("code-review", ""); err != nil {
		t.Fatalf("second Resolve() error: %v", err)
	}

	got, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest after refresh: %v", err)
	}
	embedded, err := embeddedFileMap("code-review")
	if err != nil {
		t.Fatalf("embeddedFileMap() error: %v", err)
	}
	if string(got) != string(embedded["manifest.toml"]) {
		t.Errorf("manifest.toml was not refreshed to current embedded content:\ngot:  %s\nwant: %s", got, embedded["manifest.toml"])
	}

	stampsAfter := readWorkflowStamps(t, res.Path)
	if want := hashFor(embedded["manifest.toml"]); stampsAfter["manifest.toml"] != want {
		t.Errorf("stamp not updated after refresh: got %q, want %q", stampsAfter["manifest.toml"], want)
	}
}

// TestResolvePreservesHandEditedFileOnStaleMarker is the core regression
// test for the bug this writ fixes: a hand-edited file inside an
// auto-extracted workflow directory must survive an embedded-version bump.
// The old behavior (os.RemoveAll + re-extract on any marker mismatch)
// silently destroyed it. Sibling files that were never touched must still
// refresh, proving the fix is per-file, not "never touch the directory
// again".
func TestResolvePreservesHandEditedFileOnStaleMarker(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	res, err := Resolve("code-review", "")
	if err != nil {
		t.Fatalf("first Resolve() error: %v", err)
	}

	// Hand-edit manifest.toml in place, without going through Eject.
	manifestPath := filepath.Join(res.Path, "manifest.toml")
	customContent := []byte("# operator hand-edit — do not clobber\nname = \"code-review\"\n")
	if err := os.WriteFile(manifestPath, customContent, 0o644); err != nil {
		t.Fatalf("write hand-edit: %v", err)
	}

	// An untouched sibling file, so we can prove it still refreshes.
	stylePath := filepath.Join(res.Path, "steps", "style.md")
	styleBefore, err := os.ReadFile(stylePath)
	if err != nil {
		t.Fatalf("read style.md: %v", err)
	}

	tamperVersionMarker(t, res.Path)

	res2, err := Resolve("code-review", "")
	if err != nil {
		t.Fatalf("second Resolve() error: %v", err)
	}
	if res2.Tier != TierEmbedded {
		t.Errorf("second resolve tier: got %q, want %q", res2.Tier, TierEmbedded)
	}

	// The hand-edit must survive, untouched.
	gotManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest after refresh: %v", err)
	}
	if string(gotManifest) != string(customContent) {
		t.Errorf("hand-edited manifest.toml was overwritten:\ngot:  %s\nwant: %s", gotManifest, customContent)
	}

	// The untouched sibling should be unaffected too (its content already
	// matches current embedded, so it's a no-op refresh).
	styleAfter, err := os.ReadFile(stylePath)
	if err != nil {
		t.Fatalf("read style.md after refresh: %v", err)
	}
	if string(styleBefore) != string(styleAfter) {
		t.Error("untouched sibling steps/style.md changed unexpectedly")
	}

	// Directory-level marker still gets brought up to date so future
	// resolves don't redo this work for nothing.
	stored, err := os.ReadFile(filepath.Join(res.Path, embeddedVersionFile))
	if err != nil {
		t.Fatalf("read version marker: %v", err)
	}
	if string(stored) != embeddedHash("code-review") {
		t.Error("version marker was not brought up to date after refresh")
	}

	// doctor should surface the hand-edited file as drifted.
	stale, err := CheckStaleFiles()
	if err != nil {
		t.Fatalf("CheckStaleFiles() error: %v", err)
	}
	found := false
	for _, s := range stale {
		if s.WorkflowName == "code-review" && s.RelPath == "manifest.toml" {
			found = true
			if !s.Verifiable {
				t.Error("hand-edit with a matching prior stamp should be reported as verifiable customization")
			}
		}
	}
	if !found {
		t.Errorf("CheckStaleFiles() did not report the hand-edited manifest.toml: %+v", stale)
	}
}

// TestResolveRemovedFromEmbeddedFileHandling covers files present on disk
// but no longer part of the current embedded set: untouched ones are
// deleted (they're pure extraction residue), hand-edited/unstamped ones are
// preserved and left for doctor to flag.
func TestResolveRemovedFromEmbeddedFileHandling(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	res, err := Resolve("code-review", "")
	if err != nil {
		t.Fatalf("first Resolve() error: %v", err)
	}

	// An "obsolete" file: not part of the real embedded set, but stamped as
	// if sol extracted it from an older embedded version and it was never
	// touched since — safe to remove once upstream drops it.
	obsoleteContent := []byte("old-version")
	obsoletePath := filepath.Join(res.Path, "obsolete.txt")
	if err := os.WriteFile(obsoletePath, obsoleteContent, 0o644); err != nil {
		t.Fatalf("write obsolete.txt: %v", err)
	}

	// An "orphan" file: also not part of the real embedded set, but with no
	// stamp recorded — either hand-created by the operator, or edited after
	// removal. Must be preserved.
	orphanContent := []byte("operator data")
	orphanPath := filepath.Join(res.Path, "orphan.txt")
	if err := os.WriteFile(orphanPath, orphanContent, 0o644); err != nil {
		t.Fatalf("write orphan.txt: %v", err)
	}

	stamps := readWorkflowStamps(t, res.Path)
	stamps["obsolete.txt"] = hashFor(obsoleteContent)
	// Deliberately no stamp entry for orphan.txt.
	if err := saveWorkflowStamps(t, res.Path, stamps); err != nil {
		t.Fatalf("save stamps: %v", err)
	}

	tamperVersionMarker(t, res.Path)

	if _, err := Resolve("code-review", ""); err != nil {
		t.Fatalf("second Resolve() error: %v", err)
	}

	if _, err := os.Stat(obsoletePath); !os.IsNotExist(err) {
		t.Error("untouched obsolete.txt should have been removed once dropped from the embedded set")
	}
	if _, err := os.Stat(orphanPath); err != nil {
		t.Fatalf("hand-edited/unstamped orphan.txt should have been preserved: %v", err)
	}
	gotOrphan, err := os.ReadFile(orphanPath)
	if err != nil {
		t.Fatalf("read orphan.txt: %v", err)
	}
	if string(gotOrphan) != string(orphanContent) {
		t.Error("orphan.txt content changed unexpectedly")
	}

	stampsAfter := readWorkflowStamps(t, res.Path)
	if _, ok := stampsAfter["obsolete.txt"]; ok {
		t.Error("stamp entry for removed obsolete.txt should have been dropped")
	}

	stale, err := CheckStaleFiles()
	if err != nil {
		t.Fatalf("CheckStaleFiles() error: %v", err)
	}
	foundOrphan, foundObsolete := false, false
	for _, s := range stale {
		if s.WorkflowName != "code-review" {
			continue
		}
		switch s.RelPath {
		case "orphan.txt":
			foundOrphan = true
			if s.Verifiable {
				t.Error("orphan.txt has no stamp and should be reported as unverifiable, not confirmed customization")
			}
		case "obsolete.txt":
			foundObsolete = true
		}
	}
	if !foundOrphan {
		t.Errorf("CheckStaleFiles() did not report orphan.txt: %+v", stale)
	}
	if foundObsolete {
		t.Error("obsolete.txt was deleted and should not be reported by CheckStaleFiles()")
	}
}

// TestResolveLegacyDirectoryMigratesOnFirstRefresh covers a directory
// extracted before per-file stamping existed: only the old directory-level
// .embedded-version marker is present, with no .stamps.json sidecar at all.
// On the first refresh after such a directory goes stale, untouched files
// must be backstamped (and refreshed if the embedded template moved on),
// while any pre-existing hand-edit is preserved rather than treated as
// "no stamp, so wipe it" — the exact inverse of what would keep this bug
// alive across the migration boundary.
func TestResolveLegacyDirectoryMigratesOnFirstRefresh(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	res, err := Resolve("code-review", "")
	if err != nil {
		t.Fatalf("first Resolve() error: %v", err)
	}

	// Simulate a pre-existing hand-edit made before stamping ever ran.
	stylePath := filepath.Join(res.Path, "steps", "style.md")
	customStyle := []byte("# operator's own style notes\n")
	if err := os.WriteFile(stylePath, customStyle, 0o644); err != nil {
		t.Fatalf("write hand-edit: %v", err)
	}

	// Drop the stamp sidecar entirely — this is what a directory extracted
	// by the pre-stamping code would look like: marker present, no
	// .stamps.json.
	if err := os.Remove(stampsFilePath(res.Path)); err != nil {
		t.Fatalf("remove stamps sidecar: %v", err)
	}

	tamperVersionMarker(t, res.Path)

	if _, err := Resolve("code-review", ""); err != nil {
		t.Fatalf("second Resolve() error: %v", err)
	}

	// The hand-edit predates stamping and can't be verified as untouched —
	// it must be preserved, not silently reverted to embedded content.
	gotStyle, err := os.ReadFile(stylePath)
	if err != nil {
		t.Fatalf("read style.md after refresh: %v", err)
	}
	if string(gotStyle) != string(customStyle) {
		t.Errorf("legacy hand-edit was overwritten:\ngot:  %s\nwant: %s", gotStyle, customStyle)
	}

	// An untouched file (manifest.toml, never modified) must be backstamped
	// with a stamp matching its (correct, current-embedded) content.
	manifestPath := filepath.Join(res.Path, "manifest.toml")
	gotManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest.toml: %v", err)
	}
	embedded, err := embeddedFileMap("code-review")
	if err != nil {
		t.Fatalf("embeddedFileMap() error: %v", err)
	}
	if string(gotManifest) != string(embedded["manifest.toml"]) {
		t.Error("untouched manifest.toml should still match current embedded content")
	}

	stamps := readWorkflowStamps(t, res.Path)
	if got, want := stamps["manifest.toml"], hashFor(embedded["manifest.toml"]); got != want {
		t.Errorf("manifest.toml was not backstamped: got %q, want %q", got, want)
	}
	if _, ok := stamps["steps/style.md"]; ok {
		t.Error("hand-edited legacy file should not gain a stamp that would misrepresent it as verified")
	}
}

// hashFor is a small test-local alias for stamp.Hash, kept for readability
// at call sites above.
func hashFor(data []byte) string {
	return stamp.Hash(data)
}

// saveWorkflowStamps is a test helper that writes the per-file stamp
// sidecar directly, for tests that need to set up a specific stamp state
// (e.g. simulating a stamp left behind by an older extraction) rather than
// exercising the normal extraction/refresh path.
func saveWorkflowStamps(t *testing.T, workflowDir string, stamps map[string]string) error {
	t.Helper()
	return stamp.Save(stampsFilePath(workflowDir), stamps)
}

func TestResolveDoesNotReExtractUserWorkflow(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Manually create a user workflow with the same name as an embedded one
	// but without the version marker (simulating an ejected/user-created workflow).
	userDir := Dir("code-review")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := `name = "code-review"
type = "workflow"
description = "Custom user version"

[[steps]]
id = "start"
title = "Start"
instructions = "steps/01-start.md"
`
	if err := os.WriteFile(filepath.Join(userDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	// Write a marker file to verify it survives.
	markerPath := filepath.Join(userDir, "custom-marker.txt")
	if err := os.WriteFile(markerPath, []byte("user content"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	// Resolve should return TierUser and NOT overwrite.
	res, err := Resolve("code-review", "")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if res.Tier != TierUser {
		t.Errorf("tier: got %q, want %q", res.Tier, TierUser)
	}

	// Custom marker should still exist.
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Error("custom-marker.txt should still exist for user workflow")
	}
}

// TestEmbeddedManifestsHaveType verifies that all six embedded workflow
// manifests load with a non-empty Type field. The embedded TOML files omit
// the type key, so loadEmbeddedManifest must apply the same defaulting that
// loadManifestFile does (ORCH-M4).
func TestEmbeddedManifestsHaveType(t *testing.T) {
	for name := range knownDefaults {
		t.Run(name, func(t *testing.T) {
			m, err := loadEmbeddedManifest(name)
			if err != nil {
				t.Fatalf("loadEmbeddedManifest(%q) error: %v", name, err)
			}
			if m.Type == "" {
				t.Errorf("loadEmbeddedManifest(%q): Type is empty, want non-empty", name)
			}
			if m.Type != "workflow" {
				t.Errorf("loadEmbeddedManifest(%q): Type = %q, want %q", name, m.Type, "workflow")
			}
		})
	}
}

// TestListEmbeddedEntriesHaveType verifies that entries returned by List for
// embedded workflows all have a non-empty Type. This exercises the full path
// from loadEmbeddedManifest through the List aggregation.
func TestListEmbeddedEntriesHaveType(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	entries, err := List("")
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}

	for _, e := range entries {
		if e.Tier == TierEmbedded && e.Type == "" {
			t.Errorf("List(): embedded entry %q has empty Type", e.Name)
		}
	}
}

func TestEmbeddedHashDeterministic(t *testing.T) {
	h1 := embeddedHash("code-review")
	h2 := embeddedHash("code-review")
	if h1 != h2 {
		t.Errorf("embeddedHash not deterministic: %q != %q", h1, h2)
	}
	if len(h1) != 64 { // SHA-256 hex
		t.Errorf("hash length: got %d, want 64", len(h1))
	}
}
