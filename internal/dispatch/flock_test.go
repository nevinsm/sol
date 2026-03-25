package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	// Lock file should persist after release to preserve mutual exclusion.
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Error("expected lock file to persist after release")
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

// --- Sphere session lock tests ---

func TestSphereSessionLockAcquireRelease(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock, err := AcquireSphereSessionLock()
	if err != nil {
		t.Fatalf("AcquireSphereSessionLock failed: %v", err)
	}

	// Verify lock file exists at expected path.
	lockPath := filepath.Join(config.RuntimeDir(), "locks", "sphere-session.lock")
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Error("expected lock file to exist")
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	// Lock file should persist after release to preserve mutual exclusion.
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Error("expected lock file to persist after release")
	}
}

func TestSphereSessionLockMutualExclusion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock1, err := AcquireSphereSessionLock()
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// Second acquire in a goroutine should block until lock1 is released.
	acquired := make(chan struct{})
	go func() {
		lock2, err := AcquireSphereSessionLock()
		if err != nil {
			t.Errorf("second acquire failed: %v", err)
			close(acquired)
			return
		}
		close(acquired)
		lock2.Release()
	}()

	// Give the goroutine time to block on the lock.
	select {
	case <-acquired:
		t.Fatal("second acquire should have blocked while first lock is held")
	case <-time.After(100 * time.Millisecond):
		// Expected: goroutine is blocked.
	}

	// Release the first lock — goroutine should now acquire.
	lock1.Release()

	select {
	case <-acquired:
		// Success: second acquire completed after release.
	case <-time.After(2 * time.Second):
		t.Fatal("second acquire should have succeeded after first lock released")
	}
}

func TestSphereSessionLockReleaseIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	lock, err := AcquireSphereSessionLock()
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

func TestSphereSessionLockNilRelease(t *testing.T) {
	var lock *SphereSessionLock
	if err := lock.Release(); err != nil {
		t.Fatalf("nil receiver release should not error, got: %v", err)
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

	// Lock file should persist after release to preserve mutual exclusion.
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Error("expected lock file to persist after release")
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
