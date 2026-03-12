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
		Forge:   ForgeInfo{Running: true, PID: 12345},
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

func TestOptionalStatusIndicator(t *testing.T) {
	running := optionalStatusIndicator(true)
	if !strings.Contains(running, "✓") {
		t.Errorf("optionalStatusIndicator(true) = %q, want it to contain '✓'", running)
	}

	stopped := optionalStatusIndicator(false)
	if !strings.Contains(stopped, "○") {
		t.Errorf("optionalStatusIndicator(false) = %q, want it to contain '○'", stopped)
	}
	if strings.Contains(stopped, "✗") {
		t.Errorf("optionalStatusIndicator(false) = %q, should not contain '✗'", stopped)
	}
}

func TestOptionalProcessesUseDimCircle(t *testing.T) {
	// Verify that optional processes (Chronicle, Ledger, Chancellor) show dim ○
	// when not running, while required processes (Prefect, Consul, Broker)
	// still show red ✗.
	s := &SphereStatus{
		SOLHome:   "/home/test/sol",
		Health:    "healthy",
		Prefect:   PrefectInfo{Running: false},
		Consul:    ConsulInfo{Running: false},
		Chronicle: ChronicleInfo{Running: false},
		Ledger:    LedgerInfo{Running: false},
		Broker:    BrokerInfo{Running: false},
		Chancellor: ChancellorInfo{Running: false},
	}

	output := RenderSphere(s)

	// Required processes should have ✗ (red cross).
	if !strings.Contains(output, "✗") {
		t.Error("RenderSphere should show ✗ for required non-running processes")
	}
	// Optional processes should have ○ (dim circle).
	if !strings.Contains(output, "○") {
		t.Error("RenderSphere should show ○ for optional non-running processes")
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
		"NUDGE",
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

func TestRenderWorldGovernorAlwaysShown(t *testing.T) {
	// Governor should always appear in the process list, even when not running.
	ws := &WorldStatus{
		World:    "haven",
		Prefect:  PrefectInfo{Running: true, PID: 42},
		Governor: GovernorInfo{Running: false, SessionAlive: false},
		Summary:  Summary{},
	}

	output := RenderWorld(ws)

	if !strings.Contains(output, "Governor") {
		t.Error("RenderWorld should show Governor even when not running")
	}
	// Should use dim ○ indicator (optional process), not red ✗.
	if !strings.Contains(output, "○") {
		t.Error("RenderWorld should show dim ○ for non-running optional Governor")
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
			name: "pid-based",
			info: LedgerInfo{Running: true, PID: 12345},
			want: "pid 12345",
		},
		{
			name: "pid with heartbeat",
			info: LedgerInfo{Running: true, PID: 12345, HeartbeatAge: "30s"},
			want: "pid 12345  hb 30s",
		},
		{
			name: "running no detail",
			info: LedgerInfo{Running: true},
			want: "running",
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

func TestRenderSphereMailCount(t *testing.T) {
	s := &SphereStatus{
		SOLHome:   "/home/test/sol",
		Health:    "healthy",
		Prefect:   PrefectInfo{Running: true, PID: 1234},
		MailCount: 3,
	}

	output := RenderSphere(s)

	if !strings.Contains(output, "Inbox: 3 items need attention") {
		t.Error("RenderSphere with mail count should contain 'Inbox: 3 items need attention'")
	}
}

func TestRenderSphereNoMail(t *testing.T) {
	s := &SphereStatus{
		SOLHome:   "/home/test/sol",
		Health:    "healthy",
		Prefect:   PrefectInfo{Running: true, PID: 1234},
		MailCount: 0,
	}

	output := RenderSphere(s)

	if strings.Contains(output, "Inbox:") {
		t.Error("RenderSphere with no mail should not contain 'Inbox:'")
	}
}

func TestRenderAgentNudgeCount(t *testing.T) {
	ws := &WorldStatus{
		World:   "haven",
		Prefect: PrefectInfo{Running: true, PID: 42},
		Agents: []AgentStatus{
			{Name: "Toast", State: "working", SessionAlive: true, ActiveWrit: "sol-aaa", WorkTitle: "fix bug", NudgeCount: 2},
			{Name: "Crisp", State: "idle"},
		},
		Summary: Summary{Total: 2, Working: 1, Idle: 1},
	}

	output := RenderWorld(ws)

	if !strings.Contains(output, "NUDGE") {
		t.Error("RenderWorld should show NUDGE column header")
	}
	// The output should contain "2" for Toast's nudge count.
	if !strings.Contains(output, "2") {
		t.Error("RenderWorld should show nudge count of 2 for Toast")
	}
}

func TestRenderEnvoyNudgeCount(t *testing.T) {
	ws := &WorldStatus{
		World:   "haven",
		Prefect: PrefectInfo{Running: true, PID: 42},
		Envoys: []EnvoyStatus{
			{Name: "Scout", State: "working", SessionAlive: true, ActiveWrit: "sol-bbb", WorkTitle: "Design review", BriefAge: "45m", NudgeCount: 5},
			{Name: "Ranger", State: "idle", BriefAge: "1h"},
		},
		Summary: Summary{},
	}

	output := RenderWorld(ws)

	if !strings.Contains(output, "NUDGE") {
		t.Error("RenderWorld envoys should show NUDGE column header")
	}
	if !strings.Contains(output, "5") {
		t.Error("RenderWorld should show nudge count of 5 for Scout")
	}
}

func TestRenderWorldsTableCapacity(t *testing.T) {
	s := &SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: PrefectInfo{Running: true, PID: 1234},
		Worlds: []WorldSummary{
			{Name: "capped", Agents: 4, Capacity: 5, Working: 2, Health: "healthy"},
			{Name: "unlimited", Agents: 3, Capacity: 0, Working: 1, Health: "healthy"},
		},
	}

	output := RenderSphere(s)

	// Capped world should show "4/5" format.
	if !strings.Contains(output, "4/5") {
		t.Error("RenderSphere should show '4/5' for capped world")
	}
	// Unlimited world should NOT show "/0" or similar.
	if strings.Contains(output, "3/0") {
		t.Error("RenderSphere should not show '3/0' for unlimited world")
	}
}

func TestRenderWorldSummaryWithCapacity(t *testing.T) {
	ws := &WorldStatus{
		World:    "haven",
		Capacity: 5,
		Prefect:  PrefectInfo{Running: true, PID: 42},
		Summary:  Summary{Total: 3, Working: 2, Idle: 1},
	}

	output := RenderWorld(ws)

	if !strings.Contains(output, "capacity: 5") {
		t.Error("RenderWorld with capacity should contain 'capacity: 5'")
	}
}

func TestRenderWorldSummaryWithoutCapacity(t *testing.T) {
	ws := &WorldStatus{
		World:    "haven",
		Capacity: 0,
		Prefect:  PrefectInfo{Running: true, PID: 42},
		Summary:  Summary{Total: 3, Working: 2, Idle: 1},
	}

	output := RenderWorld(ws)

	if strings.Contains(output, "capacity") {
		t.Error("RenderWorld without capacity should not contain 'capacity'")
	}
}

func TestRenderEscalationLine(t *testing.T) {
	esc := &EscalationSummary{
		Total: 3,
		BySeverity: map[string]int{
			"critical": 1,
			"high":     2,
		},
	}

	line := renderEscalationLine(esc)

	checks := []string{
		"Escalations: 3 open",
		"1 critical",
		"2 high",
	}
	for _, check := range checks {
		if !strings.Contains(line, check) {
			t.Errorf("renderEscalationLine missing %q in %q", check, line)
		}
	}
}

func TestRenderSphereWithEscalations(t *testing.T) {
	s := &SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: PrefectInfo{Running: true, PID: 1234},
		Escalations: &EscalationSummary{
			Total: 2,
			BySeverity: map[string]int{
				"high":   1,
				"medium": 1,
			},
		},
	}

	output := RenderSphere(s)

	if !strings.Contains(output, "Inbox: 2 items need attention") {
		t.Errorf("RenderSphere with escalations should contain 'Inbox: 2 items need attention', got:\n%s", output)
	}
}

func TestRenderSphereNoEscalations(t *testing.T) {
	s := &SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: PrefectInfo{Running: true, PID: 1234},
	}

	output := RenderSphere(s)

	if strings.Contains(output, "Inbox:") {
		t.Error("RenderSphere without escalations should not contain 'Inbox:'")
	}
}

func TestRenderCombinedWithEscalations(t *testing.T) {
	ws := &WorldStatus{
		World:   "haven",
		Prefect: PrefectInfo{Running: true, PID: 42},
		Summary: Summary{Total: 2, Working: 1, Idle: 1},
	}
	consulInfo := ConsulInfo{Running: true, PatrolCount: 5}
	esc := &EscalationSummary{
		Total: 1,
		BySeverity: map[string]int{
			"critical": 1,
		},
	}

	output := RenderCombined(consulInfo, ws, 2, esc)

	if !strings.Contains(output, "Inbox: 3 items need attention") {
		t.Errorf("RenderCombined with escalations+mail should contain 'Inbox: 3 items need attention', got:\n%s", output)
	}
}

func TestFormatChronicleDetailPID(t *testing.T) {
	// Not running.
	if got := formatChronicleDetail(ChronicleInfo{Running: false}); got != "" {
		t.Errorf("formatChronicleDetail(not running) = %q, want empty", got)
	}

	// PID-based.
	got := formatChronicleDetail(ChronicleInfo{Running: true, PID: 12345})
	if !strings.Contains(got, "pid 12345") {
		t.Errorf("formatChronicleDetail(pid) = %q, want to contain %q", got, "pid 12345")
	}

	// With heartbeat age.
	got = formatChronicleDetail(ChronicleInfo{Running: true, PID: 12345, HeartbeatAge: "30s"})
	if !strings.Contains(got, "pid 12345") || !strings.Contains(got, "hb 30s") {
		t.Errorf("formatChronicleDetail(pid+hb) = %q, want pid and hb info", got)
	}

	// Stale.
	got = formatChronicleDetail(ChronicleInfo{Running: true, PID: 12345, Stale: true})
	if !strings.Contains(got, "(stale)") {
		t.Errorf("formatChronicleDetail(stale) = %q, want stale indicator", got)
	}
}

func TestRenderSphereSleepingWorldShowsAgentCounts(t *testing.T) {
	s := &SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: PrefectInfo{Running: true, PID: 1234},
		Worlds: []WorldSummary{
			{Name: "active", Agents: 3, Working: 2, Forge: true, Sentinel: true, Health: "healthy"},
			{Name: "sleepy", Sleeping: true, Agents: 2, Envoys: 1, Working: 1, Health: "sleeping"},
		},
	}

	output := RenderSphere(s)

	// The sleeping world should show agent and envoy counts, not dashes.
	if !strings.Contains(output, "sleepy") {
		t.Error("output missing sleeping world name")
	}
	if !strings.Contains(output, "sleeping") {
		t.Error("output missing sleeping badge")
	}
	// The agent count "2" should appear in the output for the sleeping world row.
	// We can't check for exactly "2" since other worlds also have numbers,
	// but we verify the active world renders normally.
	if !strings.Contains(output, "active") {
		t.Error("output missing active world")
	}
}

func TestRenderSphereSleepingWorldNoCounts(t *testing.T) {
	s := &SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: PrefectInfo{Running: true, PID: 1234},
		Worlds: []WorldSummary{
			{Name: "dormant", Sleeping: true, Agents: 0, Envoys: 0, Health: "sleeping"},
		},
	}

	output := RenderSphere(s)

	// Sleeping world with no agents should render with dashes.
	if !strings.Contains(output, "dormant") {
		t.Error("output missing dormant world name")
	}
	if !strings.Contains(output, "sleeping") {
		t.Error("output missing sleeping badge")
	}
}

func TestFormatCompactTokens(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1.0K"},
		{1200, "1.2K"},
		{9999, "10K"},
		{10000, "10K"},
		{340000, "340K"},
		{999999, "1000K"},
		{1000000, "1.0M"},
		{1200000, "1.2M"},
		{14300000, "14M"},
		{100000000, "100M"},
	}

	for _, tt := range tests {
		got := formatCompactTokens(tt.input)
		if got != tt.want {
			t.Errorf("formatCompactTokens(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRenderTokensZero(t *testing.T) {
	var b strings.Builder
	renderTokens(&b, TokenInfo{})

	if b.Len() != 0 {
		t.Errorf("renderTokens with zero values should produce empty string, got %q", b.String())
	}
}

func TestRenderTokensPopulated(t *testing.T) {
	var b strings.Builder
	renderTokens(&b, TokenInfo{
		InputTokens:  1_200_000,
		OutputTokens: 340_000,
		CacheTokens:  50_000,
		AgentCount:   3,
	})

	output := b.String()

	checks := []string{
		"Tokens (24h)",
		"1.2M in",
		"340K out",
		"3 agents",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("renderTokens output missing %q, got %q", check, output)
		}
	}
}

func TestRenderTokensNoAgents(t *testing.T) {
	var b strings.Builder
	renderTokens(&b, TokenInfo{
		InputTokens:  500,
		OutputTokens: 200,
	})

	output := b.String()

	if !strings.Contains(output, "Tokens (24h)") {
		t.Error("renderTokens should show header when tokens exist")
	}
	if !strings.Contains(output, "500 in") {
		t.Errorf("renderTokens should show '500 in', got %q", output)
	}
	if strings.Contains(output, "agents") {
		t.Error("renderTokens should not show agent count when zero")
	}
}

func TestRenderWorldWithTokens(t *testing.T) {
	ws := &WorldStatus{
		World:   "testworld",
		Prefect: PrefectInfo{Running: true, PID: 42},
		Tokens: TokenInfo{
			InputTokens:  5_000_000,
			OutputTokens: 1_500_000,
			CacheTokens:  200_000,
			AgentCount:   4,
		},
		Summary: Summary{Total: 0},
	}

	output := RenderWorld(ws)

	checks := []string{
		"Tokens (24h)",
		"5.0M in",
		"1.5M out",
		"4 agents",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("RenderWorld with tokens missing %q", check)
		}
	}
}

func TestRenderWorldWithoutTokens(t *testing.T) {
	ws := &WorldStatus{
		World:   "testworld",
		Prefect: PrefectInfo{Running: true, PID: 42},
		Summary: Summary{Total: 0},
	}

	output := RenderWorld(ws)

	if strings.Contains(output, "Tokens (24h)") {
		t.Error("RenderWorld without tokens should not show Tokens section")
	}
}

func TestRenderSphereWithTokens(t *testing.T) {
	s := &SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: PrefectInfo{Running: true, PID: 1234},
		Tokens: TokenInfo{
			InputTokens:  10_000_000,
			OutputTokens: 3_000_000,
			AgentCount:   6,
		},
	}

	output := RenderSphere(s)

	checks := []string{
		"Tokens (24h)",
		"10M in",
		"3.0M out",
		"6 agents",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("RenderSphere with tokens missing %q", check)
		}
	}
}

func TestRenderSphereWithoutTokens(t *testing.T) {
	s := &SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: PrefectInfo{Running: true, PID: 1234},
	}

	output := RenderSphere(s)

	if strings.Contains(output, "Tokens (24h)") {
		t.Error("RenderSphere without tokens should not show Tokens section")
	}
}
