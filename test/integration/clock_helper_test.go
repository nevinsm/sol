package integration

import (
	"fmt"
	"testing"
	"time"
)

// Shared polling/wait helpers for integration tests.
//
// Use this for any test that previously used `time.Sleep` to synchronize
// with the system under test. Prefer `waitForCondition` with an observable
// predicate; fall back to `waitForDuration` only when the wait is genuinely
// time-based (e.g., aggregation windows, mass-death windows aging out) and
// no observable event exists.
//
// These helpers exist so test authors do not roll their own polling
// patterns and so flakiness from ad-hoc sleeps is contained to a single
// auditable location.

// waitForCondition polls `predicate` every `interval` until it returns true
// or `timeout` elapses. On timeout, it fails the test with an optional
// message. Returns normally on success.
//
// Use this in preference to `time.Sleep` whenever there is an observable
// state change that indicates the system under test is ready.
func waitForCondition(t *testing.T, predicate func() bool, timeout, interval time.Duration, msgAndArgs ...any) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if predicate() {
			return
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(interval)
	}
	msg := "waitForCondition: predicate did not become true within timeout"
	if len(msgAndArgs) > 0 {
		if format, ok := msgAndArgs[0].(string); ok {
			msg = fmt.Sprintf("waitForCondition: "+format, msgAndArgs[1:]...)
		}
	}
	t.Fatalf("%s (timeout=%s, interval=%s)", msg, timeout, interval)
}

// waitForDuration blocks for `d`, but unlike a bare `time.Sleep` it takes a
// human-readable reason (for traceability) and derives `d` from a named
// value at the call site. Use this ONLY when the wait is genuinely
// time-based — e.g., waiting for an aggregation window to expire or for
// death timestamps to age past a configured window — and there is no
// observable predicate to poll.
//
// Call sites MUST derive `d` from a configured value (e.g.,
// `cfg.MassDeathWindow`) rather than a hardcoded constant, so that the
// wait automatically tracks production tuning.
func waitForDuration(t *testing.T, d time.Duration, reason string) {
	t.Helper()
	t.Logf("waitForDuration: sleeping %s — %s", d, reason)
	start := time.Now()
	deadline := start.Add(d)
	// Implemented via waitForCondition for consistency with the poll-based
	// helper and so test timing traces share a single code path.
	waitForCondition(t, func() bool {
		return !time.Now().Before(deadline)
	}, d+time.Second, 50*time.Millisecond, "waitForDuration(%s): %s", d, reason)
}
