package forge

import (
	"testing"

	"github.com/nevinsm/sol/internal/maildeliver"
)

// noopDeliverMail is the default deliverMail stub for forge's test suite.
// Forge's tests are unit-style — mockSphereStore/mockWorldStore, no real
// tmux server or sphere store — so the real maildeliver.Deliver (which
// opens the sphere store and can nudge/wake a live session by name) must
// never run here. A mock writ's CreatedBy is often a plausible-looking
// identity (e.g. "sol-dev/Nova") for readability, and if that identity
// happens to match a real live session, an unstubbed deliverMail would
// nudge or wake it for real — exactly the kind of test-environment leak
// this package must not cause. init() installs this default for every test
// in the package; tests that need to assert delivery-helper wiring use
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
