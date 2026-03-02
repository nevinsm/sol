package status

import (
	"strings"
	"testing"
)

func TestRenderSphereBasic(t *testing.T) {
	s := &SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: PrefectInfo{Running: true, PID: 1234},
		Consul:  ConsulInfo{Running: false},
		Worlds: []WorldSummary{
			{Name: "alpha", Agents: 3, Working: 2, Forge: true, Sentinel: true, Health: "healthy"},
			{Name: "beta", Agents: 1, Working: 1, Health: "healthy"},
		},
	}

	output := RenderSphere(s)

	checks := []string{
		"Sol Sphere",
		"healthy",
		"Prefect",
		"Consul",
		"Chronicle",
		"alpha",
		"beta",
		"Worlds",
		"Processes",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("RenderSphere output missing %q", check)
		}
	}
}

func TestRenderSphereNoWorlds(t *testing.T) {
	s := &SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "degraded",
	}

	output := RenderSphere(s)

	if !strings.Contains(output, "No worlds initialized.") {
		t.Error("RenderSphere with no worlds should contain 'No worlds initialized.'")
	}
	if strings.Contains(output, "Worlds") && !strings.Contains(output, "No worlds") {
		t.Error("RenderSphere with no worlds should not show Worlds header")
	}
}

func TestRenderSphereCaravans(t *testing.T) {
	s := &SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: PrefectInfo{Running: true, PID: 100},
		Caravans: []CaravanInfo{
			{ID: "sol-abc123", Name: "deploy-batch", TotalItems: 5, DoneItems: 2, ReadyItems: 1},
			{ID: "sol-def456", Name: "refactor", TotalItems: 3, DoneItems: 0, ReadyItems: 3},
		},
	}

	output := RenderSphere(s)

	checks := []string{
		"Caravans",
		"sol-abc123",
		"sol-def456",
		"deploy-batch",
		"refactor",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("RenderSphere with caravans missing %q", check)
		}
	}
}

func TestRenderWorldBasic(t *testing.T) {
	ws := &WorldStatus{
		World:   "testworld",
		Prefect: PrefectInfo{Running: true, PID: 42},
		Forge:   ForgeInfo{Running: true, SessionName: "sol-testworld-forge"},
		Agents: []AgentStatus{
			{Name: "Toast", State: "working", SessionAlive: true, TetherItem: "sol-aaa", WorkTitle: "fix bug"},
			{Name: "Crisp", State: "idle"},
		},
		MergeQueue: MergeQueueInfo{Total: 2, Ready: 1, Claimed: 1},
		Summary:    Summary{Total: 2, Working: 1, Idle: 1},
	}

	output := RenderWorld(ws)

	checks := []string{
		"testworld",
		"Processes",
		"Agents",
		"Merge Queue",
		"Toast",
		"Crisp",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("RenderWorld output missing %q", check)
		}
	}
}

func TestRenderWorldNoAgents(t *testing.T) {
	ws := &WorldStatus{
		World:   "emptyworld",
		Prefect: PrefectInfo{Running: true, PID: 1},
		Summary: Summary{Total: 0},
	}

	output := RenderWorld(ws)

	if !strings.Contains(output, "No agents registered.") {
		t.Error("RenderWorld with no agents should contain 'No agents registered.'")
	}
}

func TestRenderWorldAgentStates(t *testing.T) {
	ws := &WorldStatus{
		World:   "multi",
		Prefect: PrefectInfo{Running: true, PID: 1},
		Agents: []AgentStatus{
			{Name: "Alpha", State: "working", SessionAlive: true},
			{Name: "Beta", State: "idle"},
			{Name: "Gamma", State: "stalled"},
			{Name: "Delta", State: "working", SessionAlive: false},
		},
		Summary: Summary{Total: 4, Working: 2, Idle: 1, Stalled: 1, Dead: 1},
	}

	output := RenderWorld(ws)

	checks := []string{
		"Alpha",
		"Beta",
		"Gamma",
		"Delta",
		"working",
		"idle",
		"stalled",
		"dead",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("RenderWorld agent states missing %q", check)
		}
	}
}

func TestHealthBadge(t *testing.T) {
	tests := []struct {
		input    string
		contains string
	}{
		{"healthy", "healthy"},
		{"unhealthy", "unhealthy"},
		{"degraded", "degraded"},
		{"something-else", "unknown"},
	}

	for _, tt := range tests {
		result := healthBadge(tt.input)
		if result == "" {
			t.Errorf("healthBadge(%q) returned empty string", tt.input)
		}
		if !strings.Contains(result, tt.contains) {
			t.Errorf("healthBadge(%q) = %q, want it to contain %q", tt.input, result, tt.contains)
		}
	}
}

func TestStatusIndicator(t *testing.T) {
	running := statusIndicator(true)
	if !strings.Contains(running, "✓") {
		t.Errorf("statusIndicator(true) = %q, want it to contain '✓'", running)
	}

	stopped := statusIndicator(false)
	if !strings.Contains(stopped, "✗") {
		t.Errorf("statusIndicator(false) = %q, want it to contain '✗'", stopped)
	}
}
