package statusformat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/broker"
)

// containsAll asserts that out contains every substring in want.
func containsAll(t *testing.T, name, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("%s: output %q missing %q", name, out, w)
		}
	}
}

// containsNone asserts that out contains none of the substrings in unwanted.
func containsNone(t *testing.T, name, out string, unwanted ...string) {
	t.Helper()
	for _, w := range unwanted {
		if strings.Contains(out, w) {
			t.Errorf("%s: output %q should not contain %q", name, out, w)
		}
	}
}

func TestFormatPrefectDetail(t *testing.T) {
	if got := FormatPrefectDetail(PrefectDetail{Running: false}); got != "" {
		t.Errorf("not running = %q, want empty", got)
	}
	if got := FormatPrefectDetail(PrefectDetail{Running: true, PID: 1234}); got != "pid 1234" {
		t.Errorf("running = %q, want %q", got, "pid 1234")
	}
}

func TestFormatConsulDetail(t *testing.T) {
	if got := FormatConsulDetail(ConsulDetail{Running: false}); got != "" {
		t.Errorf("not running = %q, want empty", got)
	}

	out := FormatConsulDetail(ConsulDetail{Running: true, PatrolCount: 7, HeartbeatAge: "30s"})
	containsAll(t, "running", out, "7 patrols", "last 30s ago")
	containsNone(t, "running", out, "(stale)")

	out = FormatConsulDetail(ConsulDetail{Running: true, PatrolCount: 2, HeartbeatAge: "5m", Stale: true})
	containsAll(t, "stale", out, "2 patrols", "last 5m ago", "(stale)")
}

func TestFormatChronicleDetail(t *testing.T) {
	if got := FormatChronicleDetail(ChronicleDetail{Running: false}); got != "" {
		t.Errorf("not running = %q, want empty", got)
	}

	// PID only.
	out := FormatChronicleDetail(ChronicleDetail{Running: true, PID: 12345})
	containsAll(t, "pid", out, "pid 12345")

	// PID + heartbeat + events.
	out = FormatChronicleDetail(ChronicleDetail{
		Running:         true,
		PID:             12345,
		HeartbeatAge:    "30s",
		EventsProcessed: 42,
	})
	containsAll(t, "full", out, "pid 12345", "hb 30s", "ev 42")
	containsNone(t, "full", out, "(stale)")

	// Stale flag adds (stale) marker.
	out = FormatChronicleDetail(ChronicleDetail{Running: true, PID: 12345, Stale: true})
	containsAll(t, "stale", out, "pid 12345", "(stale)")

	// Events only (no PID, no heartbeat).
	out = FormatChronicleDetail(ChronicleDetail{Running: true, EventsProcessed: 9001})
	containsAll(t, "events only", out, "ev 9001")
}

func TestFormatLedgerDetail(t *testing.T) {
	if got := FormatLedgerDetail(LedgerDetail{Running: false}); got != "" {
		t.Errorf("not running = %q, want empty", got)
	}

	// Running with no PID/heartbeat → "running".
	if got := FormatLedgerDetail(LedgerDetail{Running: true}); got != "running" {
		t.Errorf("bare running = %q, want %q", got, "running")
	}

	// PID only.
	if got := FormatLedgerDetail(LedgerDetail{Running: true, PID: 789}); got != "pid 789" {
		t.Errorf("pid only = %q, want %q", got, "pid 789")
	}

	// PID + heartbeat: "pid 789  hb 30s" (two-space separator is intentional).
	if got := FormatLedgerDetail(LedgerDetail{Running: true, PID: 789, HeartbeatAge: "30s"}); got != "pid 789  hb 30s" {
		t.Errorf("pid+hb = %q, want %q", got, "pid 789  hb 30s")
	}

	// Stale flag adds (stale) marker.
	out := FormatLedgerDetail(LedgerDetail{Running: true, PID: 789, Stale: true})
	containsAll(t, "stale", out, "pid 789", "(stale)")
}

func TestFormatBrokerDetail(t *testing.T) {
	if got := FormatBrokerDetail(BrokerDetail{Running: false}); got != "" {
		t.Errorf("not running = %q, want empty", got)
	}

	out := FormatBrokerDetail(BrokerDetail{Running: true, PatrolCount: 5, HeartbeatAge: "1m"})
	containsAll(t, "basic", out, "5 patrols", "last 1m ago")

	out = FormatBrokerDetail(BrokerDetail{Running: true, PatrolCount: 5, Stale: true})
	containsAll(t, "stale", out, "5 patrols", "(stale)")

	// Unreachable runtime shown inline.
	out = FormatBrokerDetail(BrokerDetail{
		Running:     true,
		PatrolCount: 3,
		Runtimes: []broker.RuntimeLiveness{
			{Runtime: "claude", OK: false},
		},
	})
	containsAll(t, "unreachable", out, "3 patrols", "[claude: unreachable]")

	// Healthy runtimes: no inline marker.
	out = FormatBrokerDetail(BrokerDetail{
		Running:     true,
		PatrolCount: 3,
		Runtimes: []broker.RuntimeLiveness{
			{Runtime: "claude", OK: true},
			{Runtime: "codex", OK: true},
		},
	})
	containsAll(t, "all-ok", out, "3 patrols")
	containsNone(t, "all-ok", out, "unreachable")
}

func TestFormatForgeDetail(t *testing.T) {
	if got := FormatForgeDetail(ForgeDetail{Running: false}); got != "" {
		t.Errorf("not running = %q, want empty", got)
	}

	// Paused.
	out := FormatForgeDetail(ForgeDetail{Running: true, Paused: true, PID: 5})
	containsAll(t, "paused", out, "paused", "(pid 5)")

	// PID only (idle, no merges yet).
	if got := FormatForgeDetail(ForgeDetail{Running: true, PID: 12345}); got != "pid 12345" {
		t.Errorf("pid only = %q, want %q", got, "pid 12345")
	}

	// PID + merging marker.
	out = FormatForgeDetail(ForgeDetail{Running: true, PID: 12345, Merging: true})
	containsAll(t, "pid+merging", out, "pid 12345", "[merging]")

	// Active forge with patrols, merges, queue, heartbeat.
	out = FormatForgeDetail(ForgeDetail{
		Running:      true,
		PID:          42,
		PatrolCount:  10,
		MergesTotal:  3,
		HeartbeatAge: "15s",
		QueueDepth:   2,
	})
	containsAll(t, "active", out,
		"pid 42",
		"10 patrols",
		"3 merged",
		"last 15s ago",
		"2 queued",
	)
	containsNone(t, "active", out, "(stale)", "[merging]")

	// Stale flag adds (stale) marker; merging adds [merging].
	out = FormatForgeDetail(ForgeDetail{
		Running:     true,
		PID:         42,
		PatrolCount: 1,
		MergesTotal: 1,
		Stale:       true,
		Merging:     true,
	})
	containsAll(t, "stale+merging", out, "pid 42", "(stale)", "[merging]")

	// Status="merging" alone (without Merging flag) must also render the
	// merging badge. Guards against ORCH-H1: when forge is running and the
	// heartbeat reports an active merge, the operator-facing display must
	// surface it even if the caller forgot to set Merging.
	out = FormatForgeDetail(ForgeDetail{
		Running: true,
		PID:     123,
		Status:  "merging",
	})
	containsAll(t, "status=merging pid only", out, "pid 123", "[merging]")

	// Status="merging" combined with patrols/merges (the main running branch).
	out = FormatForgeDetail(ForgeDetail{
		Running:     true,
		PID:         123,
		PatrolCount: 7,
		MergesTotal: 2,
		Status:      "merging",
	})
	containsAll(t, "status=merging active", out, "pid 123", "7 patrols", "2 merged", "[merging]")
}

// TestFormatForgeDetail_RemoteDegraded verifies the degraded marker for
// persistent forge remote-git failures (Task B: sol-0ec6b898c083264f) —
// absent below threshold, present with the failure count and reason at or
// above it, and gone again once the count resets (recovery).
func TestFormatForgeDetail_RemoteDegraded(t *testing.T) {
	// Below threshold: no degraded marker even with an active, otherwise
	// healthy-looking forge.
	out := FormatForgeDetail(ForgeDetail{
		Running:                   true,
		PID:                       42,
		PatrolCount:               10,
		MergesTotal:               3,
		ConsecutiveRemoteFailures: ForgeRemoteFailureThreshold - 1,
	})
	containsNone(t, "below threshold", out, "degraded")

	// At threshold: degraded marker appears with count and reason.
	out = FormatForgeDetail(ForgeDetail{
		Running:                   true,
		PID:                       42,
		PatrolCount:               10,
		MergesTotal:               3,
		ConsecutiveRemoteFailures: ForgeRemoteFailureThreshold,
		LastRemoteError:           "fatal: Authentication failed",
	})
	containsAll(t, "at threshold", out, "pid 42", "10 patrols", "degraded",
		fmt.Sprintf("%d consecutive remote-git failures", ForgeRemoteFailureThreshold),
		"fatal: Authentication failed")

	// Above threshold, PID-only path (no patrols/merges yet) still renders
	// the marker — degradation must be visible regardless of which branch of
	// FormatForgeDetail is taken.
	out = FormatForgeDetail(ForgeDetail{
		Running:                   true,
		PID:                       7,
		ConsecutiveRemoteFailures: ForgeRemoteFailureThreshold + 5,
	})
	containsAll(t, "pid-only above threshold", out, "pid 7", "degraded")

	// Recovery: counter reset to 0 clears the marker.
	out = FormatForgeDetail(ForgeDetail{
		Running:                   true,
		PID:                       42,
		PatrolCount:               11,
		MergesTotal:               3,
		ConsecutiveRemoteFailures: 0,
	})
	containsNone(t, "recovered", out, "degraded")
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
		got := FormatCompactTokens(tt.input)
		if got != tt.want {
			t.Errorf("FormatCompactTokens(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatCost(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{0.001, "$0.0010"},
		{0.0099, "$0.0099"},
		{0.01, "$0.01"},
		{1.50, "$1.50"},
		{12.345, "$12.35"},
	}

	for _, tt := range tests {
		got := FormatCost(tt.input)
		if got != tt.want {
			t.Errorf("FormatCost(%f) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatInboxLine(t *testing.T) {
	if got := FormatInboxLine(0); got != "" {
		t.Errorf("zero count = %q, want empty", got)
	}
	if got := FormatInboxLine(-1); got != "" {
		t.Errorf("negative count = %q, want empty", got)
	}
	if got := FormatInboxLine(1); got != "Inbox: 1 item needs attention\n" {
		t.Errorf("single = %q, want singular form", got)
	}
	if got := FormatInboxLine(3); got != "Inbox: 3 items need attention\n" {
		t.Errorf("plural = %q, want plural form", got)
	}
}

func TestFormatMaxActive(t *testing.T) {
	// No limit — only show active count.
	if got := FormatMaxActive(0, 4); got != "4" {
		t.Errorf("unlimited = %q, want %q", got, "4")
	}
	// With limit — show active/max.
	if got := FormatMaxActive(8, 5); got != "5/8" {
		t.Errorf("limited = %q, want %q", got, "5/8")
	}
	// Zero active with limit.
	if got := FormatMaxActive(4, 0); got != "0/4" {
		t.Errorf("zero active = %q, want %q", got, "0/4")
	}
}

func TestFormatTokenSection(t *testing.T) {
	var b strings.Builder

	// Zero tokens: no output.
	FormatTokenSection(&b, TokenDetail{})
	if b.String() != "" {
		t.Errorf("zero tokens: want empty, got %q", b.String())
	}

	// Non-zero input tokens only.
	b.Reset()
	FormatTokenSection(&b, TokenDetail{InputTokens: 1500, OutputTokens: 200})
	out := b.String()
	containsAll(t, "basic tokens", out, "Tokens (24h)", "1.5K in", "200 out")

	// With cost.
	b.Reset()
	FormatTokenSection(&b, TokenDetail{InputTokens: 1000, OutputTokens: 500, CostUSD: 0.05})
	out = b.String()
	containsAll(t, "with cost", out, "$0.05")

	// With agent count.
	b.Reset()
	FormatTokenSection(&b, TokenDetail{InputTokens: 1000, OutputTokens: 200, AgentCount: 3})
	out = b.String()
	containsAll(t, "agent count", out, "3 agents")

	// With runtime breakdown.
	b.Reset()
	FormatTokenSection(&b, TokenDetail{
		InputTokens:  2000,
		OutputTokens: 400,
		RuntimeBreakdown: []RuntimeTokenDetail{
			{Runtime: "claude", InputTokens: 1500, OutputTokens: 300},
			{Runtime: "codex", InputTokens: 500, OutputTokens: 100, CostUSD: 0.01},
		},
	})
	out = b.String()
	containsAll(t, "runtime breakdown", out, "claude", "codex", "$0.01")
	// Output must end with a blank line.
	if !strings.HasSuffix(out, "\n\n") {
		t.Errorf("token section must end with blank line, got %q", out)
	}
}

func TestRenderCaravanRows(t *testing.T) {
	caravans := []CaravanDetail{
		{ID: "c1", Name: "alpha", Status: "active", TotalItems: 10, ClosedItems: 3},
		{ID: "c2", Name: "beta", Status: "drydock", TotalItems: 5, ClosedItems: 5},
	}

	var b strings.Builder
	RenderCaravanRows(&b, caravans, CaravanRenderOpts{MaxProgressWidth: 20})
	out := b.String()

	// Both caravans appear.
	containsAll(t, "basic render", out, "alpha", "beta", "3/10 merged", "5/5 merged")
	// Sub-headers appear when both active and drydocked are present.
	containsAll(t, "sub-headers", out, "Active", "Drydocked")

	// Only active caravans: no sub-headers.
	b.Reset()
	activeOnly := []CaravanDetail{
		{ID: "c3", Name: "gamma", Status: "active", TotalItems: 4, ClosedItems: 2},
	}
	RenderCaravanRows(&b, activeOnly, CaravanRenderOpts{MaxProgressWidth: 20})
	out = b.String()
	containsAll(t, "active only", out, "gamma", "2/4 merged")
	containsNone(t, "active only", out, "Active", "Drydocked")

	// Cursor selection calls SelectFn.
	var selected string
	b.Reset()
	selectFn := func(line string) string {
		selected = line
		return "[SEL]" + line
	}
	RenderCaravanRows(&b, caravans, CaravanRenderOpts{
		MaxProgressWidth: 20,
		ShowCursor:       true,
		Cursor:           0,
		SelectFn:         selectFn,
	})
	out = b.String()
	if selected == "" {
		t.Error("SelectFn was not called for the selected row")
	}
	containsAll(t, "cursor", out, "[SEL]")

	// Phase summary rendered.
	b.Reset()
	withPhases := []CaravanDetail{
		{
			ID: "c4", Name: "delta", Status: "active", TotalItems: 6, ClosedItems: 2,
			Phases: []PhaseProgressDetail{
				{Phase: 1, Total: 3, Closed: 2},
				{Phase: 2, Total: 3, Closed: 0},
			},
		},
	}
	RenderCaravanRows(&b, withPhases, CaravanRenderOpts{MaxProgressWidth: 20})
	out = b.String()
	containsAll(t, "phases", out, "p1: 2/3", "p2: 0/3")
}

func TestFormatSentinelDetail(t *testing.T) {
	if got := FormatSentinelDetail(SentinelDetail{Running: false}); got != "" {
		t.Errorf("not running = %q, want empty", got)
	}

	// PID only (no patrols yet).
	if got := FormatSentinelDetail(SentinelDetail{Running: true, PID: 123}); got != "pid 123" {
		t.Errorf("pid only = %q, want %q", got, "pid 123")
	}

	// Active with patrols.
	out := FormatSentinelDetail(SentinelDetail{
		Running:       true,
		PID:           123,
		PatrolCount:   10,
		AgentsChecked: 5,
		HeartbeatAge:  "2m",
	})
	containsAll(t, "active", out, "10 patrols", "5 checked", "last 2m ago")
	containsNone(t, "active", out, "(stale)")

	// Stale flag adds (stale) marker. This is the regression we're guarding
	// against — dash's old copy did not render this.
	out = FormatSentinelDetail(SentinelDetail{
		Running:     true,
		PID:         123,
		PatrolCount: 4,
		Stale:       true,
	})
	containsAll(t, "stale", out, "4 patrols", "(stale)")
}
