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
		"Ledger",
		"Broker",
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
			{Name: "Toast", State: "working", SessionAlive: true, ActiveWrit: "sol-aaa", WorkTitle: "fix bug"},
			{Name: "Crisp", State: "idle"},
		},
		MergeQueue: MergeQueueInfo{Total: 2, Ready: 1, Claimed: 1},
		Summary:    Summary{Total: 2, Working: 1, Idle: 1},
	}

	output := RenderWorld(ws)

	checks := []string{
		"testworld",
		"Processes",
		"Outposts",
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

func TestRenderWorldWithEnvoys(t *testing.T) {
	ws := &WorldStatus{
		World:   "haven",
		Prefect: PrefectInfo{Running: true, PID: 42},
		Agents: []AgentStatus{
			{Name: "Toast", State: "working", SessionAlive: true, ActiveWrit: "sol-aaa", WorkTitle: "fix bug"},
		},
		Envoys: []EnvoyStatus{
			{Name: "Scout", State: "working", SessionAlive: true, ActiveWrit: "sol-bbb", WorkTitle: "Design review", BriefAge: "45m"},
		},
		Summary: Summary{Total: 1, Working: 1},
	}

	output := RenderWorld(ws)

	checks := []string{
		"Outposts (1)",
		"Envoys (1)",
		"Scout",
		"Toast",
		"BRIEF",
		"45m ago",
		"Design review",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("RenderWorld with envoys missing %q", check)
		}
	}
}

func TestRenderWorldWithGovernor(t *testing.T) {
	ws := &WorldStatus{
		World:    "haven",
		Prefect:  PrefectInfo{Running: true, PID: 42},
		Governor: GovernorInfo{Running: true, SessionAlive: true, BriefAge: "2h"},
		Summary:  Summary{},
	}

	output := RenderWorld(ws)

	checks := []string{
		"Governor",
		"brief: 2h ago",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("RenderWorld with governor missing %q", check)
		}
	}
}

func TestRenderWorldNoEnvoys(t *testing.T) {
	ws := &WorldStatus{
		World:   "haven",
		Prefect: PrefectInfo{Running: true, PID: 42},
		Agents: []AgentStatus{
			{Name: "Toast", State: "idle"},
		},
		Summary: Summary{Total: 1, Idle: 1},
	}

	output := RenderWorld(ws)

	if strings.Contains(output, "Envoys") {
		t.Error("RenderWorld with no envoys should not contain 'Envoys' section")
	}
	if !strings.Contains(output, "Outposts (1)") {
		t.Error("RenderWorld should show Outposts section")
	}
}

func TestRenderSphereNewColumns(t *testing.T) {
	s := &SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: PrefectInfo{Running: true, PID: 1234},
		Worlds: []WorldSummary{
			{Name: "alpha", Agents: 3, Envoys: 1, Governor: true, Working: 2, Forge: true, Sentinel: true, Health: "healthy"},
			{Name: "beta", Agents: 2, Envoys: 0, Governor: false, Working: 1, Health: "healthy"},
		},
	}

	output := RenderSphere(s)

	checks := []string{
		"ENVOYS",
		"GOV",
		"alpha",
		"beta",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("RenderSphere new columns missing %q", check)
		}
	}

	// Governor column should have ● for alpha.
	if !strings.Contains(output, "●") {
		t.Error("RenderSphere should show ● for active governor")
	}
}

func TestRenderCaravanPhases(t *testing.T) {
	ws := &WorldStatus{
		World:   "haven",
		Prefect: PrefectInfo{Running: true, PID: 42},
		Caravans: []CaravanInfo{
			{
				ID:         "car-abc123",
				Name:       "auth-overhaul",
				Status:     "open",
				TotalItems: 3,
				DoneItems:  2,
				ReadyItems: 0,
				Phases: []PhaseProgress{
					{Phase: 0, Total: 2, Done: 2},
					{Phase: 1, Total: 1, Done: 0, Ready: 1},
				},
			},
		},
		Summary: Summary{},
	}

	output := RenderWorld(ws)

	checks := []string{
		"Caravans",
		"car-abc123",
		"auth-overhaul",
		"phase 0",
		"phase 1",
		"3 items",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("RenderCaravanPhases missing %q", check)
		}
	}
}

func TestFormatLedgerDetail(t *testing.T) {
	tests := []struct {
		name string
		info LedgerInfo
		want string
	}{
		{
			name: "not running",
			info: LedgerInfo{Running: false},
			want: "",
		},
		{
			name: "session-based",
			info: LedgerInfo{Running: true, SessionName: "sol-ledger"},
			want: "sol-ledger",
		},
		{
			name: "pid-based",
			info: LedgerInfo{Running: true, PID: 12345},
			want: "pid 12345",
		},
		{
			name: "session preferred over pid",
			info: LedgerInfo{Running: true, SessionName: "sol-ledger", PID: 12345},
			want: "sol-ledger",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatLedgerDetail(tt.info)
			if got != tt.want {
				t.Errorf("formatLedgerDetail() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderEnvoyMultiTether(t *testing.T) {
	ws := &WorldStatus{
		World:   "haven",
		Prefect: PrefectInfo{Running: true, PID: 42},
		Envoys: []EnvoyStatus{
			{
				Name:          "Scout",
				State:         "working",
				SessionAlive:  true,
				ActiveWrit:    "sol-abc12345",
				WorkTitle:     "Design review",
				TetheredCount: 3,
				BriefAge:      "45m",
			},
		},
		Summary: Summary{},
	}

	output := RenderWorld(ws)

	checks := []string{
		"Scout",
		"Design review",
		"+2 tethered",
		"45m ago",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("RenderWorld with multi-tether envoy missing %q", check)
		}
	}
}

func TestRenderEnvoySingleTether(t *testing.T) {
	ws := &WorldStatus{
		World:   "haven",
		Prefect: PrefectInfo{Running: true, PID: 42},
		Envoys: []EnvoyStatus{
			{
				Name:          "Scout",
				State:         "working",
				SessionAlive:  true,
				ActiveWrit:    "sol-abc12345",
				WorkTitle:     "Design review",
				TetheredCount: 1,
				BriefAge:      "45m",
			},
		},
		Summary: Summary{},
	}

	output := RenderWorld(ws)

	// Should show work title but NOT tethered count for single tether.
	if !strings.Contains(output, "Design review") {
		t.Error("RenderWorld with single-tether envoy should show work title")
	}
	if strings.Contains(output, "tethered") {
		t.Error("RenderWorld with single-tether envoy should not show tethered count")
	}
}

func TestRenderEnvoyNoTether(t *testing.T) {
	ws := &WorldStatus{
		World:   "haven",
		Prefect: PrefectInfo{Running: true, PID: 42},
		Envoys: []EnvoyStatus{
			{
				Name:          "Scout",
				State:         "idle",
				SessionAlive:  false,
				TetheredCount: 0,
				BriefAge:      "2h",
			},
		},
		Summary: Summary{},
	}

	output := RenderWorld(ws)

	if strings.Contains(output, "tethered") {
		t.Error("RenderWorld with no-tether envoy should not show tethered count")
	}
}

func TestFormatChronicleDetailPID(t *testing.T) {
	tests := []struct {
		name string
		info ChronicleInfo
		want string
	}{
		{
			name: "not running",
			info: ChronicleInfo{Running: false},
			want: "",
		},
		{
			name: "session-based",
			info: ChronicleInfo{Running: true, SessionName: "sol-chronicle"},
			want: "sol-chronicle",
		},
		{
			name: "pid-based",
			info: ChronicleInfo{Running: true, PID: 12345},
			want: "pid 12345",
		},
		{
			name: "session preferred over pid",
			info: ChronicleInfo{Running: true, SessionName: "sol-chronicle", PID: 12345},
			want: "sol-chronicle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatChronicleDetail(tt.info)
			if got != tt.want {
				t.Errorf("formatChronicleDetail() = %q, want %q", got, tt.want)
			}
		})
	}
}
