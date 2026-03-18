package store

// interfaces_test.go verifies compile-time interface satisfaction for both
// WorldStore and SphereStore. If any method is missing or has the wrong
// signature, this file will fail to compile.
//
// The actual assertions are in interfaces.go via var _ X = (*Y)(nil).
// This file exists to document that the checks are intentional and to
// provide a named test that confirms the package compiles with all assertions.

import "testing"

// TestInterfaceSatisfaction verifies that the package compiles with all
// compile-time interface checks in interfaces.go. If this test exists and
// the package builds, all assertions pass.
func TestInterfaceSatisfaction(t *testing.T) {
	t.Parallel()
	// WorldStore interface assertions (verified at compile time in interfaces.go):
	var _ WritReader = (*WorldStore)(nil)
	var _ WritWriter = (*WorldStore)(nil)
	var _ MRReader = (*WorldStore)(nil)
	var _ MRWriter = (*WorldStore)(nil)
	var _ DepReader = (*WorldStore)(nil)
	var _ DepWriter = (*WorldStore)(nil)
	var _ LedgerReader = (*WorldStore)(nil)
	var _ LedgerWriter = (*WorldStore)(nil)
	var _ HistoryStore = (*WorldStore)(nil)
	var _ AgentMemoryStore = (*WorldStore)(nil)

	// SphereStore interface assertions (verified at compile time in interfaces.go):
	var _ AgentReader = (*SphereStore)(nil)
	var _ AgentWriter = (*SphereStore)(nil)
	var _ CaravanReader = (*SphereStore)(nil)
	var _ CaravanWriter = (*SphereStore)(nil)
	var _ CaravanDepReader = (*SphereStore)(nil)
	var _ CaravanDepWriter = (*SphereStore)(nil)
	var _ MessageStore = (*SphereStore)(nil)
	var _ EscalationStore = (*SphereStore)(nil)
	var _ WorldRegistry = (*SphereStore)(nil)

	t.Log("All interface satisfaction assertions pass (verified at compile time)")
}
