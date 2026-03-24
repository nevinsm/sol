package protocol

import (
	"testing"
)

func TestRoleGuardsOutpost(t *testing.T) {
	guards := RoleGuards("outpost")
	// Outpost: 3 common + 4 dangerous-command + 2 workflow-bypass = 9
	if len(guards) != 9 {
		t.Fatalf("expected 9 guards for outpost, got %d", len(guards))
	}
}

func TestRoleGuardsForge(t *testing.T) {
	guards := RoleGuards("forge")
	// Forge: 3 dangerous-command only (force push, checkout -b, rm -rf)
	if len(guards) != 3 {
		t.Fatalf("expected 3 guards for forge, got %d", len(guards))
	}
}
