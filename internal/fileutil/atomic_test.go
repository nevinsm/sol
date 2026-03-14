package fileutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	data := []byte("hello, world")

	if err := AtomicWrite(path, data, 0o644); err != nil {
		t.Fatalf("AtomicWrite() error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("AtomicWrite() wrote %q, want %q", got, data)
	}

	// Verify permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("AtomicWrite() permissions = %o, want %o", info.Mode().Perm(), 0o644)
	}
}

func TestAtomicWriteErrorOnWriteFailure(t *testing.T) {
	// Target is inside a non-existent directory that cannot be created.
	err := AtomicWrite("/nonexistent/dir/that/does/not/exist/file.txt", []byte("data"), 0o644)
	if err == nil {
		t.Fatal("AtomicWrite() expected error for non-existent directory, got nil")
	}
}

// TestAtomicWriteAtomicity verifies that the target path is not visible until
// the write completes, and that the temp file is cleaned up on success.
func TestAtomicWriteAtomicity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	tmp := path + ".tmp"

	// Before writing, neither path nor .tmp should exist.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("target path should not exist before write")
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("temp file should not exist before write")
	}

	data := []byte("atomic content")
	if err := AtomicWrite(path, data, 0o644); err != nil {
		t.Fatalf("AtomicWrite() error: %v", err)
	}

	// After writing, the target should exist with correct content.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("target path should exist after write: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("content mismatch: got %q, want %q", got, data)
	}

	// The temp file must not persist after a successful write.
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("temp file should be cleaned up after successful write")
	}
}

// TestAtomicWriteTmpCleanupOnError verifies the temp file is removed when the
// rename step fails (e.g. the target path is an existing directory).
func TestAtomicWriteTmpCleanupOnError(t *testing.T) {
	dir := t.TempDir()
	// Create a directory at the rename destination — rename(src_file, dest_dir)
	// fails with EISDIR on Linux so we can observe the cleanup path.
	path := filepath.Join(dir, "target")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	tmp := path + ".tmp"

	err := AtomicWrite(path, []byte("data"), 0o644)
	if err == nil {
		t.Fatal("expected error when renaming over a directory, got nil")
	}

	// Temp file must be cleaned up after the rename failure.
	if _, statErr := os.Stat(tmp); !os.IsNotExist(statErr) {
		t.Error("temp file should be cleaned up after rename failure")
	}
}

// unmarshalable cannot be serialised by encoding/json.
type unmarshalable struct {
	Ch chan int
}

func TestAtomicWriteJSONValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")

	v := map[string]int{"x": 1, "y": 2}
	if err := AtomicWriteJSON(path, v, 0o644); err != nil {
		t.Fatalf("AtomicWriteJSON() error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("AtomicWriteJSON() wrote an empty file")
	}
}

func TestAtomicWriteJSONInvalidStruct(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")

	// Channels cannot be marshalled to JSON → should return a marshal error.
	v := unmarshalable{Ch: make(chan int)}
	err := AtomicWriteJSON(path, v, 0o644)
	if err == nil {
		t.Fatal("AtomicWriteJSON() expected marshal error, got nil")
	}

	// The target file must not be created when marshalling fails.
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("target file should not be created on marshal error")
	}
}
