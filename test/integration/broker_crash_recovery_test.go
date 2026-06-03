package integration

// broker_crash_recovery_test.go — Integration test for the broker crash-recovery
// path documented in docs/failure-modes.md (lines 148-155):
//
//   "State lost: Per-runtime probe state and in-memory health trackers. Recovery
//    is a single patrol interval (< 5 min by default)."
//
// The broker holds no persistent health state — its in-memory trackers are
// rebuilt from scratch on every restart. This test verifies that the restart
// path correctly re-probes providers and does not carry stale pre-crash state
// (ConsecutiveFailures, degraded health) into the new process.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/broker"
)

// TestBrokerCrashRecovery exercises the broker crash-recovery path documented
// in docs/failure-modes.md (lines 148-155): when the broker crashes, its
// in-memory probe state (ConsecutiveFailures, health trackers) is lost. On
// restart the broker re-probes all configured runtimes from scratch.
//
// Test flow:
//
//	Phase 1 — pre-crash health state:
//	  Run a broker with a failing mock probe (PatrolInterval=10ms) until health
//	  reaches degraded (≥2 consecutive failures). Capture the pre-crash heartbeat.
//
//	Phase 2 — simulate crash:
//	  Cancel the broker's context. The heartbeat file on disk still holds the
//	  stale degraded state (as a real OS-level crash would leave it).
//
//	Phase 3 — restart and verify health rebuild:
//	  Create a new broker instance with a healthy mock probe. On startup, Run()
//	  calls patrol() immediately, which probes all configured runtimes. Verify
//	  the fresh heartbeat shows:
//	  - ProviderHealth == healthy  (probe succeeded; not reading stale disk state)
//	  - ConsecutiveFailures == 0   (in-memory state rebuilt from scratch)
//	  - Timestamp after pre-crash  (patrol actually ran on restart)
//	  - Status == "running"        (from the restarted broker, not a stale write)
//
// Uses DiscoverFn and SetHealthTrackerFor to avoid needing real AI provider
// endpoints in CI. Stable under make test: no real I/O, poll window is 3s for
// a 10ms tick interval.
//
// Referenced by: docs/failure-modes.md — Broker (lines 148-155)
func TestBrokerCrashRecovery(t *testing.T) {
	skipUnlessIntegration(t)

	// setupTestEnv isolates SOL_HOME so the broker heartbeat file is written
	// under a temp directory instead of the real runtime dir.
	_, _ = setupTestEnv(t)

	// =======================================================================
	// Phase 1: Run broker with a failing mock probe → drive health to degraded
	// =======================================================================

	b1 := broker.New(broker.Config{
		PatrolInterval: 10 * time.Millisecond,
		DiscoverFn:     func() []string { return []string{"mock"} },
	}, nil)

	// Replace the auto-created health tracker with one that always fails.
	// "mock" is not in the provider registry → GetProvider returns nil (no-op
	// probe). SetHealthTrackerFor overrides it with our explicit failure.
	ht1 := broker.NewHealthTracker(nil)
	ht1.SetProbeFn(func() error { return errors.New("mock: provider unavailable") })
	b1.SetHealthTrackerFor("mock", ht1)

	ctx1, cancel1 := context.WithCancel(context.Background())
	done1 := make(chan error, 1)
	go func() { done1 <- b1.Run(ctx1) }()

	// Wait for the broker to write a heartbeat showing degraded health.
	// Degraded requires ≥2 consecutive failures:
	//   - Patrol 1 (t≈0ms):  1 failure → healthy (transient by design)
	//   - Patrol 2 (t≈10ms): 2 failures → degraded
	// With PatrolInterval=10ms the transition takes ~20ms; the 3s poll budget
	// is very conservative.
	if !pollUntil(3*time.Second, 20*time.Millisecond, func() bool {
		hb, err := broker.ReadHeartbeat()
		return err == nil && hb != nil && hb.ProviderHealth == broker.HealthDegraded
	}) {
		cancel1()
		<-done1
		t.Fatal("broker did not reach degraded health state within 3s")
	}

	// Capture the pre-crash heartbeat before cancelling the context.
	// (Cancellation causes a final "stopping" write with the same degraded state;
	// capturing here ensures we get the degraded patrol write, not a timing edge.)
	precrashHB, err := broker.ReadHeartbeat()
	if err != nil || precrashHB == nil {
		cancel1()
		<-done1
		t.Fatalf("read pre-crash heartbeat: err=%v", err)
	}
	if precrashHB.ProviderHealth != broker.HealthDegraded {
		cancel1()
		<-done1
		t.Fatalf("pre-crash heartbeat health = %s, want degraded", precrashHB.ProviderHealth)
	}
	if precrashHB.ConsecutiveFailures < 2 {
		cancel1()
		<-done1
		t.Fatalf("pre-crash ConsecutiveFailures = %d, want >= 2", precrashHB.ConsecutiveFailures)
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
	// The heartbeat file on disk still holds the stale degraded state.
	// A real OS-level crash would also leave stale state on disk without
	// running any cleanup — the graceful shutdown here is equivalent for
	// recovery purposes because the new broker starts with zero in-memory state
	// regardless of what is on disk.

	// =======================================================================
	// Phase 3: Restart with a healthy probe — verify health rebuilt from scratch
	// =======================================================================

	b2 := broker.New(broker.Config{
		PatrolInterval: 10 * time.Millisecond,
		DiscoverFn:     func() []string { return []string{"mock"} },
	}, nil)

	// The provider has now recovered — probe succeeds on the restarted broker.
	ht2 := broker.NewHealthTracker(nil)
	ht2.SetProbeFn(func() error { return nil }) // provider healthy after restart
	b2.SetHealthTrackerFor("mock", ht2)

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	done2 := make(chan error, 1)
	go func() { done2 <- b2.Run(ctx2) }()

	// Wait for the restarted broker to write a fresh heartbeat. Both conditions
	// must hold simultaneously: health=healthy AND timestamp after precrashHB
	// (the latter guards against accidentally matching the stale heartbeat file
	// or the phase-1 "stopping" write from the graceful shutdown).
	if !pollUntil(3*time.Second, 20*time.Millisecond, func() bool {
		hb, err := broker.ReadHeartbeat()
		return err == nil && hb != nil &&
			hb.ProviderHealth == broker.HealthHealthy &&
			hb.Timestamp.After(precrashHB.Timestamp)
	}) {
		t.Fatal("restarted broker did not write a healthy heartbeat within 3s")
	}

	freshHB, err := broker.ReadHeartbeat()
	if err != nil || freshHB == nil {
		t.Fatalf("read fresh heartbeat after restart: err=%v", err)
	}

	// --- Verify health state was rebuilt correctly after the crash ---

	// 1. Provider is healthy — the fresh probe succeeded.  The restarted broker
	//    did NOT carry the stale degraded state from Phase 1 into its trackers.
	if freshHB.ProviderHealth != broker.HealthHealthy {
		t.Errorf("fresh heartbeat ProviderHealth = %s, want healthy (must not carry stale pre-crash state)",
			freshHB.ProviderHealth)
	}

	// 2. ConsecutiveFailures reset to 0 — the new broker's in-memory state
	//    started from scratch (broker.New creates fresh HealthTrackers with no
	//    failure history; there is no persistence of the failure count).
	if freshHB.ConsecutiveFailures != 0 {
		t.Errorf("fresh heartbeat ConsecutiveFailures = %d, want 0 (rebuilt from scratch, not carried over from before crash)",
			freshHB.ConsecutiveFailures)
	}

	// 3. Timestamp is after the pre-crash heartbeat — confirms that a real
	//    patrol ran on restart (re-probe happened, not just a stale disk read).
	if !freshHB.Timestamp.After(precrashHB.Timestamp) {
		t.Errorf("fresh heartbeat timestamp %v is not after pre-crash timestamp %v "+
			"(patrol must run on restart to rebuild health state)",
			freshHB.Timestamp, precrashHB.Timestamp)
	}

	// 4. Status is "running" — confirms this heartbeat is from the restarted
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
