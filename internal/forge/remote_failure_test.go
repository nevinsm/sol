package forge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// --- runPeriodicSweep / consecutive remote-failure counter (Task A) ---

// TestRunPeriodicSweep_IncrementsOnRemoteFailure verifies that a git fetch
// failure against origin during the periodic sweep increments the
// patrol-local consecutive-remote-failure counter and records a one-line
// error, and that repeated failures keep incrementing.
func TestRunPeriodicSweep_IncrementsOnRemoteFailure(t *testing.T) {
	state, _, cmdRunner := setupPatrolTest(t)
	defer state.fl.Close()
	state.forge.cmd = cmdRunner

	cmdRunner.SetResult("git fetch origin",
		[]byte("fatal: Authentication failed for 'https://example.invalid/repo.git'"),
		errors.New("exit status 128"))

	ctx := context.Background()
	state.runPeriodicSweep(ctx)

	if state.consecutiveRemoteFailures != 1 {
		t.Errorf("consecutiveRemoteFailures = %d, want 1", state.consecutiveRemoteFailures)
	}
	if !strings.Contains(state.lastRemoteError, "Authentication failed") {
		t.Errorf("lastRemoteError = %q, want to contain %q", state.lastRemoteError, "Authentication failed")
	}
	if !state.lastRemoteSuccess.IsZero() {
		t.Errorf("lastRemoteSuccess = %v, want zero (no success yet)", state.lastRemoteSuccess)
	}

	// A second consecutive failure increments again rather than resetting.
	state.runPeriodicSweep(ctx)
	if state.consecutiveRemoteFailures != 2 {
		t.Errorf("consecutiveRemoteFailures after 2nd failure = %d, want 2", state.consecutiveRemoteFailures)
	}
}

// TestRunPeriodicSweep_ResetsOnSuccess verifies that a successful fetch
// resets the counter and error, and records a last-success timestamp — even
// after a run of prior consecutive failures.
func TestRunPeriodicSweep_ResetsOnSuccess(t *testing.T) {
	state, _, cmdRunner := setupPatrolTest(t)
	defer state.fl.Close()
	state.forge.cmd = cmdRunner

	// Seed prior failures, as if earlier patrols had hit remote errors.
	state.consecutiveRemoteFailures = 2
	state.lastRemoteError = "previous failure"

	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git rev-parse --verify --quiet refs/remotes/origin/main", []byte("abc123\n"), nil)

	ctx := context.Background()
	state.runPeriodicSweep(ctx)

	if state.consecutiveRemoteFailures != 0 {
		t.Errorf("consecutiveRemoteFailures = %d, want 0 after success", state.consecutiveRemoteFailures)
	}
	if state.lastRemoteError != "" {
		t.Errorf("lastRemoteError = %q, want empty after success", state.lastRemoteError)
	}
	if state.lastRemoteSuccess.IsZero() {
		t.Error("expected lastRemoteSuccess to be set after a successful fetch")
	}
}

// TestRunPeriodicSweep_NonRemoteFailureDoesNotCountAsRemoteFailure verifies
// that a sweep failure unrelated to the remote fetch (e.g. the target ref
// missing after a successful fetch) does not increment the remote-failure
// counter — the fetch itself succeeded, so the remote was reachable.
func TestRunPeriodicSweep_NonRemoteFailureDoesNotCountAsRemoteFailure(t *testing.T) {
	state, _, cmdRunner := setupPatrolTest(t)
	defer state.fl.Close()
	state.forge.cmd = cmdRunner

	state.consecutiveRemoteFailures = 1
	state.lastRemoteError = "previous failure"

	// Fetch succeeds, but the target ref lookup fails (misconfiguration).
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git rev-parse --verify --quiet refs/remotes/origin/main", nil, errors.New("exit status 1"))

	ctx := context.Background()
	state.runPeriodicSweep(ctx)

	if state.consecutiveRemoteFailures != 0 {
		t.Errorf("consecutiveRemoteFailures = %d, want 0 (fetch succeeded)", state.consecutiveRemoteFailures)
	}
	if state.lastRemoteError != "" {
		t.Errorf("lastRemoteError = %q, want empty (fetch succeeded)", state.lastRemoteError)
	}
	if state.lastRemoteSuccess.IsZero() {
		t.Error("expected lastRemoteSuccess to be set — the fetch against origin succeeded")
	}
}

// TestHeartbeat_CarriesRemoteFailureFields verifies the forge heartbeat JSON
// round-trips the consecutive-failure counter, last error, and last-success
// timestamp written by writeHeartbeatWithMR.
func TestHeartbeat_CarriesRemoteFailureFields(t *testing.T) {
	state, _, cmdRunner := setupPatrolTest(t)
	defer state.fl.Close()
	state.forge.cmd = cmdRunner

	cmdRunner.SetResult("git fetch origin",
		[]byte("fatal: could not read Username for 'https://example.invalid': terminal prompts disabled"),
		errors.New("exit status 128"))

	ctx := context.Background()
	state.runPeriodicSweep(ctx)
	state.runPeriodicSweep(ctx)
	state.runPeriodicSweep(ctx)
	state.writeHeartbeat("idle", 0)

	hb, err := ReadHeartbeat("ember")
	if err != nil {
		t.Fatalf("ReadHeartbeat error: %v", err)
	}
	if hb == nil {
		t.Fatal("expected non-nil heartbeat")
	}
	if hb.ConsecutiveRemoteFailures != 3 {
		t.Errorf("hb.ConsecutiveRemoteFailures = %d, want 3", hb.ConsecutiveRemoteFailures)
	}
	if !strings.Contains(hb.LastRemoteError, "terminal prompts disabled") {
		t.Errorf("hb.LastRemoteError = %q, want to contain %q", hb.LastRemoteError, "terminal prompts disabled")
	}
	if !hb.LastRemoteSuccess.IsZero() {
		t.Errorf("hb.LastRemoteSuccess = %v, want zero (never succeeded in this test)", hb.LastRemoteSuccess)
	}

	// Now recover: a successful fetch should reset the heartbeat's fields too.
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult("git rev-parse --verify --quiet refs/remotes/origin/main", []byte("abc123\n"), nil)
	state.runPeriodicSweep(ctx)
	state.writeHeartbeat("idle", 0)

	hb, err = ReadHeartbeat("ember")
	if err != nil {
		t.Fatalf("ReadHeartbeat error: %v", err)
	}
	if hb.ConsecutiveRemoteFailures != 0 {
		t.Errorf("hb.ConsecutiveRemoteFailures = %d, want 0 after recovery", hb.ConsecutiveRemoteFailures)
	}
	if hb.LastRemoteError != "" {
		t.Errorf("hb.LastRemoteError = %q, want empty after recovery", hb.LastRemoteError)
	}
	if hb.LastRemoteSuccess.IsZero() {
		t.Error("expected hb.LastRemoteSuccess to be set after recovery")
	}
}

// --- RemoteGitError ---

func TestRemoteGitError_Unwrap(t *testing.T) {
	inner := errors.New("exit status 128")
	err := &RemoteGitError{Op: "fetch", Err: inner}

	if !errors.Is(err, inner) {
		t.Error("expected errors.Is to unwrap to the inner error")
	}

	// Wrap once more, as a caller further up the stack might, and confirm
	// errors.As still finds the RemoteGitError through the extra layer.
	wrapped := fmt.Errorf("periodic sweep failed: %w", error(err))
	var target *RemoteGitError
	if !errors.As(wrapped, &target) {
		t.Fatal("expected errors.As to find the RemoteGitError through a wrapping layer")
	}
	if target.Op != "fetch" {
		t.Errorf("target.Op = %q, want %q", target.Op, "fetch")
	}
}
