package dispatch

// Regression tests for the dispatch lock-release ordering invariant fixed
// in commit 7ef9aa8 ("Fix dispatch and startup concurrency and code quality",
// writ sol-fb36ce737f16e3b7).
//
// The invariant: provision locks (the per-world ProvisionLock and, when
// configured, the sphere SphereSessionLock) MUST be released before
// startup.Launch is invoked. Holding them across Launch can deadlock the
// session being launched, which itself contends for those locks during its
// own startup sequence.
//
// Strategy: wrap the existing mockSessionManager with a startHook that runs
// when Launch reaches its session-start step (Launch step 14, mgr.Start).
// The hook attempts to acquire the same provision lock the dispatch caller
// held. If the caller released it (correct), the hook acquires it
// immediately; if not, the hook times out, the test fails with a clear
// message, and the eventual deferred Release in Cast unblocks the goroutine
// so the test does not leak.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/flock"
)

// concurrencyMgr embeds *mockSessionManager and exposes a startHook fired
// before the embedded Start runs. If the hook returns an error, Start
// returns it without recording the start — letting tests force a Launch
// failure to exercise the rollback path.
type concurrencyMgr struct {
	*mockSessionManager
	startHook func(name string) error
}

func (m *concurrencyMgr) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	if m.startHook != nil {
		if err := m.startHook(name); err != nil {
			return err
		}
	}
	return m.mockSessionManager.Start(name, workdir, cmd, env, role, world)
}

// tryAcquireProvLock attempts to acquire the per-world provision lock and
// release it. Returns true if both succeeded within timeout, false on
// timeout or acquisition error.
//
// AcquireProvisionLock uses LOCK_EX (blocking), so the acquire is run on a
// background goroutine and raced against a timer. If the dispatch caller
// has not released the lock, the goroutine stays parked until Cast's
// deferred safety-net release fires; the goroutine then completes and
// exits cleanly (the buffered channel absorbs the late signal). No leak.
func tryAcquireProvLock(world string, timeout time.Duration) bool {
	acquired := make(chan bool, 1)
	go func() {
		pl, err := flock.AcquireProvisionLock(world)
		if err != nil {
			acquired <- false
			return
		}
		_ = pl.Release()
		acquired <- true
	}()
	select {
	case ok := <-acquired:
		return ok
	case <-time.After(timeout):
		return false
	}
}

// TestCastReleasesProvLocksBeforeLaunch — happy path. autoProvision
// acquires the per-world provision lock. The mock's Start hook (invoked
// from inside Launch) attempts to acquire the same lock with a 500ms
// budget. If Cast released the lock before invoking Launch (correct
// ordering), the attempt succeeds immediately. A failure here means the
// release-before-launch fix in dispatch.go has been regressed.
func TestCastReleasesProvLocksBeforeLaunch(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	var lockObservedFree bool
	mgr := &concurrencyMgr{
		mockSessionManager: newMockSessionManager(),
		startHook: func(name string) error {
			lockObservedFree = tryAcquireProvLock("ember", 500*time.Millisecond)
			return nil
		},
	}

	itemID, err := worldStore.CreateWrit("Item", "desc", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	// No AgentName → Cast invokes autoProvision, which acquires the
	// per-world provision lock. The lock must be released before Launch
	// (the hook below runs inside Launch).
	if _, err := Cast(context.Background(), CastOpts{
		WritID:     itemID,
		World:      "ember",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("Cast failed: %v", err)
	}

	if !lockObservedFree {
		t.Fatal("provision lock still held when Launch reached Start: " +
			"dispatch caller did not release provLocks before Launch — " +
			"regression of the fix in commit 7ef9aa8")
	}
}

// TestCastReleasesProvLocksBeforeLaunchOnRollback — rollback path. When
// Launch fails, Cast runs rollback() which itself calls provLocks.Release()
// a second time. This test verifies two things:
//
//  1. The release-before-launch invariant holds even on the failing path
//     (the hook observes the lock free when Launch reaches Start).
//
//  2. After rollback, the lock is free (the redundant Release in rollback
//     is idempotent and does not get the lock stuck or panic).
func TestCastReleasesProvLocksBeforeLaunchOnRollback(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	var lockObservedFree bool
	sentinelErr := errors.New("synthetic launch failure")
	mgr := &concurrencyMgr{
		mockSessionManager: newMockSessionManager(),
		startHook: func(name string) error {
			lockObservedFree = tryAcquireProvLock("ember", 500*time.Millisecond)
			return sentinelErr
		},
	}

	itemID, err := worldStore.CreateWrit("Item", "desc", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	_, err = Cast(context.Background(), CastOpts{
		WritID:     itemID,
		World:      "ember",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)
	if err == nil {
		t.Fatal("expected Cast to fail when Launch hook returns error")
	}

	if !lockObservedFree {
		t.Fatal("provision lock still held when Launch reached Start (rollback path): " +
			"dispatch caller did not release provLocks before Launch — " +
			"regression of the fix in commit 7ef9aa8")
	}

	// After Cast returns, the lock must be free — the rollback's redundant
	// Release call should be idempotent, not leave the lock stuck.
	if !tryAcquireProvLock("ember", 500*time.Millisecond) {
		t.Fatal("provision lock not free after rollback: " +
			"rollback did not complete a clean release")
	}

	// Verify rollback restored agent state to idle (sanity check that the
	// rollback path actually executed and did not bail out before the
	// state-restoration steps).
	agents, lerr := sphereStore.ListAgents("ember", "")
	if lerr != nil {
		t.Fatalf("list agents: %v", lerr)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 auto-provisioned agent, got %d", len(agents))
	}
	if agents[0].State != "idle" {
		t.Errorf("expected agent state 'idle' after rollback, got %q", agents[0].State)
	}
}

// TestCastRollbackHandlesNilProvLocks — nil-provLocks rollback. When Cast
// is given an explicit AgentName, autoProvision is skipped and provLocks
// stays nil throughout. If Launch then fails, rollback unconditionally
// calls provLocks.Release() (dispatch.go around line 301). The Release
// method on a nil *provisionLocks must be a safe no-op, otherwise the
// rollback would panic and mask the original Launch error.
//
// This locks in the nil-safe contract of provisionLocks.Release.
func TestCastRollbackHandlesNilProvLocks(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	if _, err := sphereStore.CreateAgent("Toast", "ember", "outpost"); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	sentinelErr := errors.New("synthetic launch failure")
	mgr := &concurrencyMgr{
		mockSessionManager: newMockSessionManager(),
		startHook: func(name string) error {
			return sentinelErr
		},
	}

	itemID, err := worldStore.CreateWrit("Item", "desc", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	// AgentName set → Cast skips autoProvision → provLocks is nil for the
	// whole call. Launch failure must not panic in rollback.
	_, err = Cast(context.Background(), CastOpts{
		WritID:     itemID,
		World:      "ember",
		AgentName:  "Toast",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)
	if err == nil {
		t.Fatal("expected Cast to fail when Launch hook returns error")
	}
	if !errors.Is(err, sentinelErr) {
		t.Errorf("expected wrapped sentinel error, got: %v", err)
	}

	// Reaching here means rollback did not panic on nil provLocks.
	// Verify the rollback also restored agent state to idle.
	agent, gerr := sphereStore.GetAgent("ember/Toast")
	if gerr != nil {
		t.Fatalf("get agent: %v", gerr)
	}
	if agent.State != "idle" {
		t.Errorf("expected agent state 'idle' after rollback, got %q", agent.State)
	}

	// And the per-world provision lock should still be acquirable — it
	// was never held in this path, but verify the rollback didn't somehow
	// create a stuck lock.
	if !tryAcquireProvLock("ember", 500*time.Millisecond) {
		t.Fatal("provision lock unexpectedly held after rollback with nil provLocks")
	}
}
