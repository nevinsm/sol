package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/gt/internal/config"
)

func TestAcquireRelease(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock, err := AcquireWorkItemLock("gt-aabbccdd")
	if err != nil {
		t.Fatalf("AcquireWorkItemLock failed: %v", err)
	}

	// Verify lock file exists.
	lockPath := filepath.Join(config.RuntimeDir(), "locks", "gt-aabbccdd.lock")
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Error("expected lock file to exist")
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	// Verify lock file removed.
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("expected lock file to be removed after release")
	}
}

func TestDoubleAcquire(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock1, err := AcquireWorkItemLock("gt-11223344")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer lock1.Release()

	_, err = AcquireWorkItemLock("gt-11223344")
	if err == nil {
		t.Fatal("expected contention error on second acquire")
	}
	if !strings.Contains(err.Error(), "being dispatched by another process") {
		t.Errorf("expected contention error message, got: %v", err)
	}
}

func TestDifferentItems(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock1, err := AcquireWorkItemLock("gt-aaaaaaaa")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	lock2, err := AcquireWorkItemLock("gt-bbbbbbbb")
	if err != nil {
		t.Fatalf("second acquire (different item) failed: %v", err)
	}

	if err := lock1.Release(); err != nil {
		t.Errorf("release lock1 failed: %v", err)
	}
	if err := lock2.Release(); err != nil {
		t.Errorf("release lock2 failed: %v", err)
	}
}

func TestReleaseIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock, err := AcquireWorkItemLock("gt-cccccccc")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("first release failed: %v", err)
	}

	// Second release should not error.
	if err := lock.Release(); err != nil {
		t.Fatalf("second release should not error, got: %v", err)
	}
}

// --- Merge slot lock tests ---

func TestMergeSlotAcquireRelease(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock, err := AcquireMergeSlotLock("testrig")
	if err != nil {
		t.Fatalf("AcquireMergeSlotLock failed: %v", err)
	}

	// Verify lock file exists.
	lockPath := filepath.Join(config.RuntimeDir(), "locks", "testrig-merge-slot.lock")
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Error("expected lock file to exist")
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	// Verify lock file removed.
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("expected lock file to be removed after release")
	}
}

func TestMergeSlotDoubleAcquire(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock1, err := AcquireMergeSlotLock("testrig")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	_, err = AcquireMergeSlotLock("testrig")
	if err == nil {
		t.Fatal("expected contention error on second acquire")
	}
	if !strings.Contains(err.Error(), "busy") {
		t.Errorf("expected 'busy' error message, got: %v", err)
	}

	// Release first, acquire again -> succeeds.
	lock1.Release()

	lock3, err := AcquireMergeSlotLock("testrig")
	if err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	lock3.Release()
}

func TestMergeSlotDifferentRigs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock1, err := AcquireMergeSlotLock("rig1")
	if err != nil {
		t.Fatalf("acquire rig1 failed: %v", err)
	}

	lock2, err := AcquireMergeSlotLock("rig2")
	if err != nil {
		t.Fatalf("acquire rig2 failed (different rigs should not conflict): %v", err)
	}

	if err := lock1.Release(); err != nil {
		t.Errorf("release rig1 failed: %v", err)
	}
	if err := lock2.Release(); err != nil {
		t.Errorf("release rig2 failed: %v", err)
	}
}
