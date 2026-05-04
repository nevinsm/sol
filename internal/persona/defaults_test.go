package persona

import (
	"strings"
	"testing"
	"testing/fstest"
)

// TestKnownDefaultsCoversEmbedFS asserts that every .md file shipped under
// internal/persona/defaults/ is recognized by knownDefaults. This is the
// duplication-reconciliation check: knownDefaults is now derived from the
// embed FS at init, so any drift between the two would be a regression in
// loadKnownDefaults itself.
func TestKnownDefaultsCoversEmbedFS(t *testing.T) {
	entries, err := defaultPersonas.ReadDir("defaults")
	if err != nil {
		t.Fatalf("read embedded defaults/: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one embedded persona, got none")
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		if !knownDefaults[name] {
			t.Errorf("embedded persona %q (%s) is not in knownDefaults", name, e.Name())
		}
	}
}

// TestKnownDefaultsRejectsUnknown asserts that names that do not correspond to
// embedded files return false from knownDefaults. Pairs with the coverage
// check above to fully bracket the registry's contract.
func TestKnownDefaultsRejectsUnknown(t *testing.T) {
	for _, name := range []string{
		"nonexistent",
		"",
		"planner.md", // basename-with-suffix should not match
		"PLANNER",    // case-sensitive
	} {
		if knownDefaults[name] {
			t.Errorf("knownDefaults[%q] = true, want false", name)
		}
	}
}

// TestLoadKnownDefaultsRecognizesNewFiles is the regression test required by
// the writ: it drives loadKnownDefaults with a fake fs.ReadDirFS that contains
// a persona name not present in the real embed FS, and asserts the new name
// is recognized without any source edits to a static map. If a future refactor
// reintroduces a hand-maintained map, this test will fail because the new
// name will not appear in the result.
func TestLoadKnownDefaultsRecognizesNewFiles(t *testing.T) {
	fakeFS := fstest.MapFS{
		"defaults/planner.md":     {Data: []byte("# planner\n")},
		"defaults/engineer.md":    {Data: []byte("# engineer\n")},
		"defaults/brand-new.md":   {Data: []byte("# brand-new\n")},
		"defaults/another_one.md": {Data: []byte("# another_one\n")},
		// Non-.md files and subdirectories should be ignored.
		"defaults/README.txt":      {Data: []byte("not a persona\n")},
		"defaults/nested/inner.md": {Data: []byte("nested\n")},
	}

	got, err := loadKnownDefaults(fakeFS, "defaults")
	if err != nil {
		t.Fatalf("loadKnownDefaults: %v", err)
	}

	wantPresent := []string{"planner", "engineer", "brand-new", "another_one"}
	for _, name := range wantPresent {
		if !got[name] {
			t.Errorf("expected %q to be recognized, got map: %v", name, got)
		}
	}

	wantAbsent := []string{"README", "README.txt", "nested", "inner", "nonexistent"}
	for _, name := range wantAbsent {
		if got[name] {
			t.Errorf("expected %q to be absent, got map: %v", name, got)
		}
	}

	if len(got) != len(wantPresent) {
		t.Errorf("got %d entries, want %d (entries: %v)", len(got), len(wantPresent), got)
	}
}

// TestLoadKnownDefaultsMissingDir asserts loadKnownDefaults surfaces a
// non-nil error when the requested directory does not exist on the supplied
// FS. This guards the init() panic path: a malformed build that fails to
// embed the defaults/ tree must not silently produce an empty registry.
func TestLoadKnownDefaultsMissingDir(t *testing.T) {
	fakeFS := fstest.MapFS{
		"other/file.md": {Data: []byte("x")},
	}
	if _, err := loadKnownDefaults(fakeFS, "defaults"); err == nil {
		t.Fatal("expected error for missing defaults/ dir, got nil")
	}
}
