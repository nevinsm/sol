package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCloneWorldDataPreservesWritColumns(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create source world and populate writs with non-default values.
	src, err := OpenWorld("source")
	if err != nil {
		t.Fatal(err)
	}

	id1, err := src.CreateWritWithOpts(CreateWritOpts{
		Title:       "task with metadata",
		Description: "has kind, metadata, and close_reason",
		CreatedBy:   "test",
		Kind:        "research",
		Metadata:    map[string]any{"env": "staging", "count": float64(42)},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Close the writ with a reason so close_reason is populated.
	if _, err := src.CloseWrit(id1, "completed-successfully"); err != nil {
		t.Fatal(err)
	}

	// Create a second writ with defaults (kind=code, no metadata, no close_reason)
	// to ensure the clone also handles default values correctly.
	id2, err := src.CreateWritWithOpts(CreateWritOpts{
		Title:       "default writ",
		Description: "uses defaults",
		CreatedBy:   "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	src.Close()

	// Create target world.
	tgt, err := OpenWorld("target")
	if err != nil {
		t.Fatal(err)
	}
	tgt.Close()

	// Clone source → target.
	if err := CloneWorldData("source", "target", false); err != nil {
		t.Fatalf("CloneWorldData failed: %v", err)
	}

	// Reopen target and verify cloned data.
	tgt, err = OpenWorld("target")
	if err != nil {
		t.Fatal(err)
	}
	defer tgt.Close()

	// Verify writ with non-default kind, metadata, and close_reason.
	w1, err := tgt.GetWrit(id1)
	if err != nil {
		t.Fatalf("GetWrit(%q) failed: %v", id1, err)
	}
	if w1.Kind != "research" {
		t.Errorf("kind = %q, want %q", w1.Kind, "research")
	}
	if w1.CloseReason != "completed-successfully" {
		t.Errorf("close_reason = %q, want %q", w1.CloseReason, "completed-successfully")
	}
	if w1.Metadata == nil {
		t.Fatal("metadata is nil, want non-nil")
	}
	if w1.Metadata["env"] != "staging" {
		t.Errorf("metadata[env] = %v, want %q", w1.Metadata["env"], "staging")
	}
	if w1.Metadata["count"] != float64(42) {
		t.Errorf("metadata[count] = %v, want %v", w1.Metadata["count"], float64(42))
	}
	if w1.ClosedAt == nil {
		t.Error("closed_at is nil, want non-nil for closed writ")
	}

	// Verify writ with default values.
	w2, err := tgt.GetWrit(id2)
	if err != nil {
		t.Fatalf("GetWrit(%q) failed: %v", id2, err)
	}
	if w2.Kind != "code" {
		t.Errorf("kind = %q, want %q", w2.Kind, "code")
	}
	if w2.CloseReason != "" {
		t.Errorf("close_reason = %q, want empty", w2.CloseReason)
	}
	if w2.Metadata != nil {
		t.Errorf("metadata = %v, want nil", w2.Metadata)
	}
}
