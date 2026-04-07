package statusformat

import (
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

	// Single-provider inline degraded marker.
	out = FormatBrokerDetail(BrokerDetail{
		Running:        true,
		PatrolCount:    3,
		ProviderHealth: "degraded",
	})
	containsAll(t, "degraded", out, "3 patrols", "[provider: degraded]")

	out = FormatBrokerDetail(BrokerDetail{
		Running:        true,
		PatrolCount:    3,
		ProviderHealth: "down",
	})
	containsAll(t, "down", out, "3 patrols", "[provider: down]")

	// With per-provider entries, the inline marker is suppressed.
	out = FormatBrokerDetail(BrokerDetail{
		Running:        true,
		PatrolCount:    3,
		ProviderHealth: "degraded",
		Providers: []broker.ProviderHealthEntry{
			{Provider: "claude", Health: broker.HealthHealthy},
			{Provider: "codex", Health: broker.HealthDegraded},
		},
	})
	containsAll(t, "multi-provider", out, "3 patrols")
	containsNone(t, "multi-provider", out, "[provider: degraded]")
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
