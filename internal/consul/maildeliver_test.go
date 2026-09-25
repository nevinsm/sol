package consul

import (
	"testing"

	"github.com/nevinsm/sol/internal/maildeliver"
)

// noopDeliverMail is the default deliverMail stub for consul's test suite.
// Consul's tests open real sphere/world stores against a temp SOL_HOME
// (setupSolHome) but do nothing to isolate tmux, so the real
// maildeliver.Deliver — which queries the live tmux server and can nudge or
// even launch a real session for a priority<=2 envoy recipient — must never
// run here. init() installs this default for every test in the package;
// tests that need to assert delivery-helper wiring use
// installFakeDeliverMail instead.
func noopDeliverMail(maildeliver.Opts) error { return nil }

func init() {
	deliverMail = noopDeliverMail
}

// installFakeDeliverMail replaces deliverMail with a fake that records every
// call, restoring the package's no-op default (not whatever was previously
// installed) when the test ends — tests never leak a fake into a sibling
// test via shared package state.
func installFakeDeliverMail(t *testing.T) *[]maildeliver.Opts {
	t.Helper()
	var calls []maildeliver.Opts
	deliverMail = func(opts maildeliver.Opts) error {
		calls = append(calls, opts)
		return nil
	}
	t.Cleanup(func() { deliverMail = noopDeliverMail })
	return &calls
}
