package stamp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashDeterministic(t *testing.T) {
	h1 := Hash([]byte("hello"))
	h2 := Hash([]byte("hello"))
	if h1 != h2 {
		t.Errorf("Hash() not deterministic: %q != %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("Hash() length = %d, want 64", len(h1))
	}
	if h1 == Hash([]byte("world")) {
		t.Error("Hash() collided for different inputs")
	}
}

func TestLoadMissingFileReturnsEmptyMap(t *testing.T) {
	dir := t.TempDir()
	stamps, err := Load(filepath.Join(dir, ".stamps.json"))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(stamps) != 0 {
		t.Errorf("Load() on missing file = %v, want empty map", stamps)
	}
}

func TestLoadInvalidJSONErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".stamps.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("Load() expected error for invalid JSON, got nil")
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", ".stamps.json")
	want := map[string]string{"a.md": Hash([]byte("a")), "b/c.md": Hash([]byte("bc"))}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("Load() = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Load()[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestSaveOverwritesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".stamps.json")

	if err := Save(path, map[string]string{"a.md": "1"}); err != nil {
		t.Fatalf("first Save() error: %v", err)
	}
	if err := Save(path, map[string]string{"a.md": "2"}); err != nil {
		t.Fatalf("second Save() error: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if got["a.md"] != "2" {
		t.Errorf("Load()[\"a.md\"] = %q, want %q", got["a.md"], "2")
	}

	// No leftover temp files.
	matches, err := filepath.Glob(filepath.Join(dir, ".tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("leftover temp files after Save(): %v", matches)
	}
}
