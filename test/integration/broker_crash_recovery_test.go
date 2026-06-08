package integration

// broker_crash_recovery_test.go — Integration test for the broker crash-recovery
// path documented in docs/failure-modes.md (lines 148-155):
//
//   "State lost: Per-runtime probe state and in-memory health trackers. Recovery
//    is a single patrol interval (< 5 min by default)."
//
// The broker holds no persistent health state — its in-memory probe results are
// rebuilt from scratch on every restart. This test verifies that the restart
// path correctly re-probes runtimes and does not carry stale pre-crash state
// into the new process.

import (
	"context"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/broker"
)

// TestBrokerCrashRecovery exercises the broker crash-recovery path documented
// in docs/failure-modes.md (lines 148-155): when the broker crashes, its
// in-memory probe results are lost. On restart the broker re-probes all
// configured runtimes from scratch.
//
// Test flow:
//
//	Phase 1 — pre-crash failing state:
//	  Run a broker with a failing mock probe (PatrolInterval=10ms) until a
//	  heartbeat appears showing a failing runtime. Capture the pre-crash heartbeat.
//
//	Phase 2 — simulate crash:
//	  Cancel the broker's context. The heartbeat file on disk still holds the
//	  stale failing state (as a real OS-level crash would leave it).
//
//	Phase 3 — restart and verify health rebuilt:
//	  Create a new broker instance with a healthy mock probe. On startup, Run()
//	  calls patrol() immediately, which probes all configured runtimes. Verify
//	  the fresh heartbeat shows:
//	  - AllOK == true          (probe succeeded; not reading stale disk state)
//	  - Timestamp after pre-crash  (patrol actually ran on restart)
//	  - Status == "running"        (from the restarted broker, not a stale write)
//
// Uses DiscoverFn and SetProbeFn to avoid needing real AI provider
// binaries in CI. Stable under make test: no real I/O, poll window is 3s for
// a 10ms tick interval.
func TestBrokerCrashRecovery(t *testing.T) {
	skipUnlessIntegration(t)

	// setupTestEnv isolates SOL_HOME so the broker heartbeat file is written
	// under a temp directory instead of the real runtime dir.
	_, _ = setupTestEnv(t)

	// =======================================================================
	// Phase 1: Run broker with a failing mock probe → write a failing heartbeat
	// =======================================================================

	b1 := broker.New(broker.Config{
		PatrolInterval: 10 * time.Millisecond,
		DiscoverFn:     func() []string { return []string{"mock"} },
	}, nil)

	// Override probe to always fail.
	b1.SetProbeFn("mock", func() bool { return false })

	ctx1, cancel1 := context.WithCancel(context.Background())
	done1 := make(chan error, 1)
	go func() { done1 <- b1.Run(ctx1) }()

	// Wait for the broker to write a heartbeat showing a failing runtime.
	if !pollUntil(3*time.Second, 20*time.Millisecond, func() bool {
		hb, err := broker.ReadHeartbeat()
		return err == nil && hb != nil && len(hb.Runtimes) > 0 && !hb.Runtimes[0].OK
	}) {
		cancel1()
		<-done1
		t.Fatal("broker did not write a failing heartbeat within 3s")
	}

	// Capture the pre-crash heartbeat.
	precrashHB, err := broker.ReadHeartbeat()
	if err != nil || precrashHB == nil {
		cancel1()
		<-done1
		t.Fatalf("read pre-crash heartbeat: err=%v", err)
	}
	if precrashHB.AllOK() {
		cancel1()
		<-done1
		t.Fatal("pre-crash heartbeat should show a failing runtime")
	}

	// =======================================================================
	// Phase 2: Simulate crash — cancel the broker's context
	// =======================================================================

	cancel1()
	select {
	case <-done1:
	case <-time.After(5 * time.Second):
		t.Fatal("broker (phase 1) did not shut down within 5s")
	}

	// =======================================================================
	// Phase 3: Restart with a healthy probe — verify state rebuilt from scratch
	// =======================================================================

	b2 := broker.New(broker.Config{
		PatrolInterval: 10 * time.Millisecond,
		DiscoverFn:     func() []string { return []string{"mock"} },
	}, nil)

	// The runtime has now recovered — probe succeeds on the restarted broker.
	b2.SetProbeFn("mock", func() bool { return true })

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	done2 := make(chan error, 1)
	go func() { done2 <- b2.Run(ctx2) }()

	// Wait for the restarted broker to write a fresh healthy heartbeat.
	// Must check Status == "running" to avoid matching the stale "stopping"
	// heartbeat written by the phase-1 broker on shutdown (which also has
	// AllOK()==true because it carries nil runtimes, making the for loop vacuous).
	if !pollUntil(3*time.Second, 20*time.Millisecond, func() bool {
		hb, err := broker.ReadHeartbeat()
		return err == nil && hb != nil &&
			hb.Status == "running" &&
			hb.AllOK() &&
			hb.Timestamp.After(precrashHB.Timestamp)
	}) {
		t.Fatal("restarted broker did not write a healthy heartbeat within 3s")
	}

	freshHB, err := broker.ReadHeartbeat()
	if err != nil || freshHB == nil {
		t.Fatalf("read fresh heartbeat after restart: err=%v", err)
	}

	// --- Verify health state was rebuilt correctly after the crash ---

	// 1. All runtimes OK — the fresh probe succeeded.
	if !freshHB.AllOK() {
		t.Error("fresh heartbeat should show all runtimes OK (probe succeeded after restart)")
	}

	// 2. Timestamp is after the pre-crash heartbeat — confirms that a real
	//    patrol ran on restart (re-probe happened, not just a stale disk read).
	if !freshHB.Timestamp.After(precrashHB.Timestamp) {
		t.Errorf("fresh heartbeat timestamp %v is not after pre-crash timestamp %v "+
			"(patrol must run on restart to rebuild health state)",
			freshHB.Timestamp, precrashHB.Timestamp)
	}

	// 3. Status is "running" — confirms this heartbeat is from the restarted
	//    broker, not the final "stopping" write from the phase-1 shutdown.
	if freshHB.Status != "running" {
		t.Errorf("fresh heartbeat status = %q, want %q (must be from restarted broker)",
			freshHB.Status, "running")
	}

	cancel2()
	select {
	case <-done2:
	case <-time.After(5 * time.Second):
		t.Logf("restarted broker did not shut down within 5s (non-fatal cleanup timeout)")
	}
}
