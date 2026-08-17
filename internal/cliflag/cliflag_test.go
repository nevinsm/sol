package cliflag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTextInlineOnly(t *testing.T) {
	got, err := ResolveText("hello world", "", "description", "description-file")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestResolveTextEmpty(t *testing.T) {
	got, err := ResolveText("", "", "description", "description-file")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestResolveTextFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "desc.txt")
	if err := os.WriteFile(path, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	got, err := ResolveText("", path, "description", "description-file")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "line one\nline two"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveTextFromFileNoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "desc.txt")
	if err := os.WriteFile(path, []byte("no newline here"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	got, err := ResolveText("", path, "description", "description-file")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "no newline here" {
		t.Errorf("got %q, want %q", got, "no newline here")
	}
}

func TestResolveTextFromFileNotFound(t *testing.T) {
	_, err := ResolveText("", "/nonexistent/path/does-not-exist.txt", "description", "description-file")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
	if !strings.Contains(err.Error(), "description-file") {
		t.Errorf("error should mention the flag name %q: %v", "description-file", err)
	}
}

func TestResolveTextFromStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	go func() {
		_, _ = w.Write([]byte("from stdin\n"))
		_ = w.Close()
	}()

	got, err := ResolveText("", "-", "description", "description-file")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "from stdin" {
		t.Errorf("got %q, want %q", got, "from stdin")
	}
}

func TestResolveTextMutualExclusion(t *testing.T) {
	_, err := ResolveText("inline value", "/some/path", "description", "description-file")
	if err == nil {
		t.Fatal("expected mutual exclusion error, got nil")
	}
	if !strings.Contains(err.Error(), "--description") || !strings.Contains(err.Error(), "--description-file") {
		t.Errorf("error should name both flags: %v", err)
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error should say mutually exclusive: %v", err)
	}
}
