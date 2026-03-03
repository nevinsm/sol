package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTrustDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := "/home/ubuntu/sol/myworld/outposts/Agent1/worktree"

	if err := TrustDirectory(dir); err != nil {
		t.Fatalf("TrustDirectory failed: %v", err)
	}

	// Read back and verify.
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("failed to read .claude.json: %v", err)
	}

	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to parse .claude.json: %v", err)
	}

	projects, ok := state["projects"].(map[string]any)
	if !ok {
		t.Fatal("missing or invalid projects key")
	}

	entry, ok := projects[dir].(map[string]any)
	if !ok {
		t.Fatalf("missing project entry for %q", dir)
	}

	if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Error("hasTrustDialogAccepted should be true")
	}
}

func TestTrustDirectoryIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := "/some/worktree"

	// Trust twice.
	if err := TrustDirectory(dir); err != nil {
		t.Fatalf("first TrustDirectory failed: %v", err)
	}
	if err := TrustDirectory(dir); err != nil {
		t.Fatalf("second TrustDirectory failed: %v", err)
	}

	// Should still have one entry.
	data, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	var state map[string]any
	json.Unmarshal(data, &state)
	projects := state["projects"].(map[string]any)

	if len(projects) != 1 {
		t.Errorf("expected 1 project entry, got %d", len(projects))
	}
}

func TestTrustDirectoryPreservesExisting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Write a pre-existing .claude.json with other data.
	existing := map[string]any{
		"numStartups": float64(42),
		"projects": map[string]any{
			"/other/project": map[string]any{
				"hasTrustDialogAccepted": true,
				"lastCost":               1.5,
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(home, ".claude.json"), data, 0o600)

	// Trust a new directory.
	if err := TrustDirectory("/new/worktree"); err != nil {
		t.Fatalf("TrustDirectory failed: %v", err)
	}

	// Verify existing data preserved.
	data, _ = os.ReadFile(filepath.Join(home, ".claude.json"))
	var state map[string]any
	json.Unmarshal(data, &state)

	if state["numStartups"] != float64(42) {
		t.Error("numStartups was clobbered")
	}

	projects := state["projects"].(map[string]any)
	if len(projects) != 2 {
		t.Errorf("expected 2 project entries, got %d", len(projects))
	}

	other := projects["/other/project"].(map[string]any)
	if other["lastCost"] != 1.5 {
		t.Error("existing project data was clobbered")
	}
}
