package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHomeFromEnv(t *testing.T) {
	t.Setenv("SOL_HOME", "/custom/sol/home")
	got := Home()
	if got != "/custom/sol/home" {
		t.Fatalf("expected /custom/sol/home, got %q", got)
	}
}

func TestHomeDefault(t *testing.T) {
	t.Setenv("SOL_HOME", "")
	got := Home()
	if !strings.HasSuffix(got, "/sol") {
		t.Fatalf("expected path ending with /sol, got %q", got)
	}
}

func TestStoreDir(t *testing.T) {
	t.Setenv("SOL_HOME", "/tmp/test-sol")
	got := StoreDir()
	if got != "/tmp/test-sol/.store" {
		t.Fatalf("expected /tmp/test-sol/.store, got %q", got)
	}
}

func TestRuntimeDir(t *testing.T) {
	t.Setenv("SOL_HOME", "/tmp/test-sol")
	got := RuntimeDir()
	if got != "/tmp/test-sol/.runtime" {
		t.Fatalf("expected /tmp/test-sol/.runtime, got %q", got)
	}
}

func TestWorldDir(t *testing.T) {
	t.Setenv("SOL_HOME", "/tmp/test-sol")
	got := WorldDir("myworld")
	if got != "/tmp/test-sol/myworld" {
		t.Fatalf("expected /tmp/test-sol/myworld, got %q", got)
	}
}

func TestEnsureDirs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error: %v", err)
	}

	// Verify .store was created.
	if info, err := os.Stat(filepath.Join(dir, ".store")); err != nil {
		t.Fatalf("expected .store dir to exist: %v", err)
	} else if !info.IsDir() {
		t.Fatal("expected .store to be a directory")
	}

	// Verify .runtime was created.
	if info, err := os.Stat(filepath.Join(dir, ".runtime")); err != nil {
		t.Fatalf("expected .runtime dir to exist: %v", err)
	} else if !info.IsDir() {
		t.Fatal("expected .runtime to be a directory")
	}
}

func TestEnsureDirsAlreadyExist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Create subdirs manually.
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755)

	// Should be idempotent.
	if err := EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error on existing dirs: %v", err)
	}
}

func TestValidateWorldNameEmpty(t *testing.T) {
	err := ValidateWorldName("")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("expected 'must not be empty' in error, got: %v", err)
	}
}

func TestValidateWorldNameInvalid(t *testing.T) {
	invalid := []string{".hidden", "has spaces", "-starts-dash", "foo/bar", "foo.bar"}
	for _, name := range invalid {
		t.Run(name, func(t *testing.T) {
			err := ValidateWorldName(name)
			if err == nil {
				t.Fatalf("expected error for invalid name %q", name)
			}
			if !strings.Contains(err.Error(), "invalid world name") {
				t.Fatalf("expected 'invalid world name' in error for %q, got: %v", name, err)
			}
		})
	}
}

func TestValidateWorldNameValid(t *testing.T) {
	valid := []string{"myworld", "test-world", "World_01", "a1"}
	for _, name := range valid {
		t.Run(name, func(t *testing.T) {
			if err := ValidateWorldName(name); err != nil {
				t.Fatalf("expected name %q to be valid, got: %v", name, err)
			}
		})
	}
}
