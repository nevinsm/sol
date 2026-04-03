package protocol

import (
	"testing"
)

func TestRoleGuardsOutpost(t *testing.T) {
	guards := RoleGuards("outpost")
	// Outpost: 2 common + 4 dangerous-command + 3 workflow-bypass = 9
	if len(guards) != 9 {
		t.Fatalf("expected 9 guards for outpost, got %d", len(guards))
	}
}

func TestRoleGuardsForge(t *testing.T) {
	guards := RoleGuards("forge")
	// Forge: 2 dangerous-command only (force push, rm -rf)
	if len(guards) != 2 {
		t.Fatalf("expected 2 guards for forge, got %d", len(guards))
	}
}
