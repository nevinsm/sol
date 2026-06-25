package jsoncontract

import "testing"

// skipUnlessContractTest skips the calling test when -test.short is set. Every
// contract test in this package should call this as its first statement; it
// replaces the historical 2-line skip-if-short boilerplate so the gating
// condition lives in exactly one place.
func skipUnlessContractTest(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping contract test")
	}
}
