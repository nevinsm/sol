package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/config"
)

func TestAcquireRelease(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock, err := AcquireWritLock("sol-aabbccdd")
	if err != nil {
		t.Fatalf("AcquireWritLock failed: %v", err)
	}

	// Verify lock file exists.
	lockPath := filepath.Join(config.RuntimeDir(), "locks", "sol-aabbccdd.lock")
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
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock1, err := AcquireWritLock("sol-11223344")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer lock1.Release()

	_, err = AcquireWritLock("sol-11223344")
	if err == nil {
		t.Fatal("expected contention error on second acquire")
	}
	if !strings.Contains(err.Error(), "being dispatched by another process") {
		t.Errorf("expected contention error message, got: %v", err)
	}
}

func TestDifferentItems(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock1, err := AcquireWritLock("sol-aaaaaaaa")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	lock2, err := AcquireWritLock("sol-bbbbbbbb")
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
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock, err := AcquireWritLock("sol-cccccccc")
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

func TestAgentLockReleaseIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock, err := AcquireAgentLock("ember/Toast")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("first release failed: %v", err)
	}

	// Second release must be a no-op — this mirrors the restart path where
	// both restartSession's unlockFn and defer agentLock.Release() fire.
	if err := lock.Release(); err != nil {
		t.Fatalf("second release should be idempotent, got: %v", err)
	}

	// Third call also safe (nil receiver edge case already covered by guard).
	if err := lock.Release(); err != nil {
		t.Fatalf("third release should be idempotent, got: %v", err)
	}
}

func TestMergeSlotReleaseIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock, err := AcquireMergeSlotLock("ember")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("first release failed: %v", err)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("second release should be idempotent, got: %v", err)
	}
}

func TestProvisionLockReleaseIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock, err := AcquireProvisionLock("ember")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("first release failed: %v", err)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("second release should be idempotent, got: %v", err)
	}
}

// --- Merge slot lock tests ---

func TestMergeSlotAcquireRelease(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock, err := AcquireMergeSlotLock("ember")
	if err != nil {
		t.Fatalf("AcquireMergeSlotLock failed: %v", err)
	}

	// Verify lock file exists.
	lockPath := filepath.Join(config.RuntimeDir(), "locks", "ember-merge-slot.lock")
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
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock1, err := AcquireMergeSlotLock("ember")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	_, err = AcquireMergeSlotLock("ember")
	if err == nil {
		t.Fatal("expected contention error on second acquire")
	}
	if !strings.Contains(err.Error(), "busy") {
		t.Errorf("expected 'busy' error message, got: %v", err)
	}

	// Release first, acquire again -> succeeds.
	lock1.Release()

	lock3, err := AcquireMergeSlotLock("ember")
	if err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	lock3.Release()
}

func TestMergeSlotDifferentWorlds(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock1, err := AcquireMergeSlotLock("alpha")
	if err != nil {
		t.Fatalf("acquire alpha failed: %v", err)
	}

	lock2, err := AcquireMergeSlotLock("beta")
	if err != nil {
		t.Fatalf("acquire beta failed (different worlds should not conflict): %v", err)
	}

	if err := lock1.Release(); err != nil {
		t.Errorf("release alpha failed: %v", err)
	}
	if err := lock2.Release(); err != nil {
		t.Errorf("release beta failed: %v", err)
	}
}
