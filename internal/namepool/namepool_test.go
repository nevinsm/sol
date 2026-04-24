package namepool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefault(t *testing.T) {
	pool, err := Load("")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	names := pool.Names()
	if len(names) < 50 {
		t.Errorf("expected at least 50 names, got %d", len(names))
	}
	// Verify no duplicates.
	seen := make(map[string]bool)
	for _, n := range names {
		if seen[n] {
			t.Errorf("duplicate name: %q", n)
		}
		seen[n] = true
	}
	// Verify no blank or comment-only entries.
	for _, n := range names {
		if n == "" {
			t.Error("found blank name")
		}
		if strings.HasPrefix(n, "#") {
			t.Errorf("found comment entry: %q", n)
		}
	}
}

func TestLoadOverride(t *testing.T) {
	dir := t.TempDir()
	overridePath := filepath.Join(dir, "names.txt")
	if err := os.WriteFile(overridePath, []byte("Alpha\nBravo\nCharlie\n"), 0o644); err != nil {
		t.Fatalf("failed to write override: %v", err)
	}

	pool, err := Load(overridePath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	names := pool.Names()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "Alpha" || names[1] != "Bravo" || names[2] != "Charlie" {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestLoadOverrideFallback(t *testing.T) {
	pool, err := Load("/nonexistent/path/names.txt")
	if err != nil {
		t.Fatalf("Load should not error on missing override, got: %v", err)
	}
	names := pool.Names()
	if len(names) < 50 {
		t.Errorf("expected fallback to default (50+ names), got %d", len(names))
	}
}

func TestAllocateName(t *testing.T) {
	pool, err := Load("")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	names := pool.Names()

	// Empty usedNames -> returns first name.
	name, err := pool.AllocateName(nil)
	if err != nil {
		t.Fatalf("AllocateName failed: %v", err)
	}
	if name != names[0] {
		t.Errorf("expected first name %q, got %q", names[0], name)
	}

	// First name used -> returns second name.
	name, err = pool.AllocateName([]string{names[0]})
	if err != nil {
		t.Fatalf("AllocateName failed: %v", err)
	}
	if name != names[1] {
		t.Errorf("expected second name %q, got %q", names[1], name)
	}

	// First N names used -> returns N+1th.
	n := 5
	name, err = pool.AllocateName(names[:n])
	if err != nil {
		t.Fatalf("AllocateName failed: %v", err)
	}
	if name != names[n] {
		t.Errorf("expected name %q, got %q", names[n], name)
	}
}

func TestAllocateNameExhaustion(t *testing.T) {
	dir := t.TempDir()
	overridePath := filepath.Join(dir, "names.txt")
	if err := os.WriteFile(overridePath, []byte("Alpha\nBravo\nCharlie\n"), 0o644); err != nil {
		t.Fatalf("failed to write override: %v", err)
	}

	pool, err := Load(overridePath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	_, err = pool.AllocateName([]string{"Alpha", "Bravo", "Charlie"})
	if err == nil {
		t.Fatal("expected exhaustion error")
	}
	if !strings.Contains(err.Error(), "exhausted") {
		t.Errorf("expected 'exhausted' error, got: %v", err)
	}
}

func TestLoadSkipsTooLongNames(t *testing.T) {
	dir := t.TempDir()
	overridePath := filepath.Join(dir, "names.txt")
	longName := strings.Repeat("A", 65) // exceeds MaxAgentNameLen (64)
	content := "Alpha\n" + longName + "\nBravo\n"
	if err := os.WriteFile(overridePath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write override: %v", err)
	}

	pool, err := Load(overridePath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	names := pool.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 names (too-long skipped), got %d: %v", len(names), names)
	}
	if names[0] != "Alpha" || names[1] != "Bravo" {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestLoadAcceptsMaxLengthName(t *testing.T) {
	dir := t.TempDir()
	overridePath := filepath.Join(dir, "names.txt")
	maxName := strings.Repeat("A", 64) // exactly MaxAgentNameLen
	content := maxName + "\nBravo\n"
	if err := os.WriteFile(overridePath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write override: %v", err)
	}

	pool, err := Load(overridePath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	names := pool.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d: %v", len(names), names)
	}
	if names[0] != maxName {
		t.Errorf("expected max-length name to be accepted, got %q", names[0])
	}
}

func TestLoadCommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	overridePath := filepath.Join(dir, "names.txt")
	content := "# This is a comment\n\nAlpha\n\n# Another comment\nBravo\n\n"
	if err := os.WriteFile(overridePath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write override: %v", err)
	}

	pool, err := Load(overridePath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	names := pool.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 names (comments/blanks skipped), got %d: %v", len(names), names)
	}
	if names[0] != "Alpha" || names[1] != "Bravo" {
		t.Errorf("unexpected names: %v", names)
	}
}
