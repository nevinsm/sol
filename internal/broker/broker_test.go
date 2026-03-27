package broker

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/account"
)

// ptrTime is a helper to create a *time.Time.
func ptrTime(t time.Time) *time.Time { return &t }

func TestCheckTokenExpiry_NoExpiry(t *testing.T) {
	tok := &account.Token{
		Type:  "api_key",
		Token: "sk-test",
	}
	th := checkTokenExpiry("alice", tok, nil)
	if th.Status != "no_expiry" {
		t.Errorf("expected no_expiry, got %q", th.Status)
	}
	if th.Type != "api_key" {
		t.Errorf("expected type api_key, got %q", th.Type)
	}
	if th.ExpiresAt != nil {
		t.Error("ExpiresAt should be nil for no-expiry token")
	}
}

func TestCheckTokenExpiry_Expired(t *testing.T) {
	tok := &account.Token{
		Type:      "oauth_token",
		Token:     "tok",
		ExpiresAt: ptrTime(time.Now().Add(-1 * time.Hour)),
	}
	th := checkTokenExpiry("bob", tok, nil)
	if th.Status != "expired" {
		t.Errorf("expected expired, got %q", th.Status)
	}
	if th.Handle != "bob" {
		t.Errorf("expected handle bob, got %q", th.Handle)
	}
}

func TestCheckTokenExpiry_NearExpiry_Critical_Under1d(t *testing.T) {
	// Expires in 12 hours — within 1-day threshold.
	tok := &account.Token{
		Type:      "oauth_token",
		Token:     "tok",
		ExpiresAt: ptrTime(time.Now().Add(12 * time.Hour)),
	}
	th := checkTokenExpiry("carol", tok, nil)
	if th.Status != "critical" {
		t.Errorf("expected critical (under 1d), got %q", th.Status)
	}
}

func TestCheckTokenExpiry_NearExpiry_Warning_Under7d(t *testing.T) {
	// Expires in 3 days — within 7-day threshold but beyond 1-day threshold.
	tok := &account.Token{
		Type:      "oauth_token",
		Token:     "tok",
		ExpiresAt: ptrTime(time.Now().Add(3 * 24 * time.Hour)),
	}
	th := checkTokenExpiry("dave", tok, nil)
	if th.Status != "warning" {
		t.Errorf("expected warning (under 7d), got %q", th.Status)
	}
}

func TestCheckTokenExpiry_NearExpiry_ExpiringSoon(t *testing.T) {
	// Expires in 15 days — within 30-day threshold but beyond 7-day threshold.
	tok := &account.Token{
		Type:      "oauth_token",
		Token:     "tok",
		ExpiresAt: ptrTime(time.Now().Add(15 * 24 * time.Hour)),
	}
	th := checkTokenExpiry("eve", tok, nil)
	if th.Status != "expiring_soon" {
		t.Errorf("expected expiring_soon, got %q", th.Status)
	}
}

func TestCheckTokenExpiry_Ok(t *testing.T) {
	// Expires in 60 days — well beyond all thresholds.
	tok := &account.Token{
		Type:      "oauth_token",
		Token:     "tok",
		ExpiresAt: ptrTime(time.Now().Add(60 * 24 * time.Hour)),
	}
	th := checkTokenExpiry("frank", tok, nil)
	if th.Status != "ok" {
		t.Errorf("expected ok, got %q", th.Status)
	}
}

func TestCheckTokenExpiry_ExactlyAtBoundary_Expired(t *testing.T) {
	// Exactly at expiry (timeLeft = 0) should be "expired".
	tok := &account.Token{
		Type:      "oauth_token",
		Token:     "tok",
		ExpiresAt: ptrTime(time.Now()),
	}
	th := checkTokenExpiry("grace", tok, nil)
	// timeLeft = time.Until(now) will be ~0 or negative.
	if th.Status != "expired" && th.Status != "critical" {
		t.Errorf("expected expired or critical at boundary, got %q", th.Status)
	}
}

func TestBrokerPatrolWritesHeartbeat(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	b := New(Config{}, nil)

	// Mock health probe so no real HTTP call.
	ht := NewHealthTracker(nil)
	ht.SetProbeFn(func() error { return nil })
	b.SetHealthTracker(ht)

	b.patrol()

	hb, err := ReadHeartbeat()
	if err != nil {
		t.Fatal(err)
	}
	if hb == nil {
		t.Fatal("heartbeat should exist after patrol")
	}
	if hb.PatrolCount != 1 {
		t.Errorf("expected patrol_count 1, got %d", hb.PatrolCount)
	}
	if hb.Status != "running" {
		t.Errorf("expected status %q, got %q", "running", hb.Status)
	}
}

func TestHeartbeatStale(t *testing.T) {
	hb := &Heartbeat{
		Timestamp: time.Now().Add(-15 * time.Minute),
	}
	if !hb.IsStale(10 * time.Minute) {
		t.Error("heartbeat 15m old should be stale with 10m threshold")
	}

	hb.Timestamp = time.Now().Add(-5 * time.Minute)
	if hb.IsStale(10 * time.Minute) {
		t.Error("heartbeat 5m old should not be stale with 10m threshold")
	}
}

func TestMultiProviderDiscovery(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Create two world directories with different runtimes.
	world1Dir := filepath.Join(solHome, "world1")
	os.MkdirAll(world1Dir, 0o755)
	os.WriteFile(filepath.Join(world1Dir, "world.toml"), []byte(`
[agents]
default_runtime = "claude"
`), 0o644)

	world2Dir := filepath.Join(solHome, "world2")
	os.MkdirAll(world2Dir, 0o755)
	os.WriteFile(filepath.Join(world2Dir, "world.toml"), []byte(`
[agents]
default_runtime = "claude"
[agents.runtimes]
outpost = "codex"
`), 0o644)

	runtimes := DiscoverWorldRuntimes()

	// Should find both "claude" and "codex", deduplicated and sorted.
	if len(runtimes) != 2 {
		t.Fatalf("expected 2 runtimes, got %d: %v", len(runtimes), runtimes)
	}
	if runtimes[0] != "claude" {
		t.Errorf("expected first runtime to be claude, got %q", runtimes[0])
	}
	if runtimes[1] != "codex" {
		t.Errorf("expected second runtime to be codex, got %q", runtimes[1])
	}
}

func TestMultiProviderDiscoveryFallback(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// No world directories — should fall back to ["claude"].
	runtimes := DiscoverWorldRuntimes()
	if len(runtimes) != 1 || runtimes[0] != "claude" {
		t.Errorf("expected [claude] fallback, got %v", runtimes)
	}
}

func TestMultiProviderIndependentHealthTracking(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	claudeFailing := false
	codexFailing := true

	b := New(Config{
		DiscoverFn: func() []string { return []string{"claude", "codex"} },
	}, nil)

	// Set up independent probe functions.
	claudeHT := NewHealthTracker(nil)
	claudeHT.SetProbeFn(func() error {
		if claudeFailing {
			return errors.New("claude down")
		}
		return nil
	})

	codexHT := NewHealthTracker(nil)
	codexHT.SetProbeFn(func() error {
		if codexFailing {
			return errors.New("codex down")
		}
		return nil
	})

	b.SetHealthTrackerFor("claude", claudeHT)
	b.SetHealthTrackerFor("codex", codexHT)

	// Run patrol — claude healthy, codex failing.
	b.patrol()

	hb, err := ReadHeartbeat()
	if err != nil {
		t.Fatal(err)
	}
	if hb == nil {
		t.Fatal("expected heartbeat")
	}

	// With 2 providers, heartbeat should have per-provider entries.
	if len(hb.Providers) != 2 {
		t.Fatalf("expected 2 provider entries, got %d", len(hb.Providers))
	}

	// Claude should be healthy (probe succeeded).
	var claudeEntry, codexEntry *ProviderHealthEntry
	for i := range hb.Providers {
		switch hb.Providers[i].Provider {
		case "claude":
			claudeEntry = &hb.Providers[i]
		case "codex":
			codexEntry = &hb.Providers[i]
		}
	}
	if claudeEntry == nil || codexEntry == nil {
		t.Fatal("missing provider entries")
	}

	if claudeEntry.Health != HealthHealthy {
		t.Errorf("claude: expected healthy, got %s", claudeEntry.Health)
	}

	// Codex had 1 failure — should still be healthy (transient).
	if codexEntry.Health != HealthHealthy {
		t.Errorf("codex after 1 failure: expected healthy, got %s", codexEntry.Health)
	}
	if codexEntry.ConsecutiveFailures != 1 {
		t.Errorf("codex: expected 1 failure, got %d", codexEntry.ConsecutiveFailures)
	}

	// Directly probe the codex tracker to drive it to degraded (2 failures).
	// (patrol uses ShouldProbe which needs time to elapse, so we probe directly.)
	codexHT.Probe() // 2nd failure → degraded
	b.writeHeartbeat("running", nil)

	hb, _ = ReadHeartbeat()
	for i := range hb.Providers {
		if hb.Providers[i].Provider == "codex" {
			codexEntry = &hb.Providers[i]
		}
	}
	if codexEntry.Health != HealthDegraded {
		t.Errorf("codex after 2 failures: expected degraded, got %s", codexEntry.Health)
	}

	// Top-level ProviderHealth should reflect worst state.
	if hb.ProviderHealth != HealthDegraded {
		t.Errorf("top-level health: expected degraded (worst), got %s", hb.ProviderHealth)
	}
}

func TestSingleProviderNoProviderEntries(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	b := New(Config{}, nil)

	ht := NewHealthTracker(nil)
	ht.SetProbeFn(func() error { return nil })
	b.SetHealthTracker(ht)

	b.patrol()

	hb, err := ReadHeartbeat()
	if err != nil {
		t.Fatal(err)
	}
	if hb == nil {
		t.Fatal("expected heartbeat")
	}

	// Single provider — Providers slice should be empty (backward compat).
	if len(hb.Providers) != 0 {
		t.Errorf("single provider: expected empty Providers slice, got %d entries", len(hb.Providers))
	}

	// Top-level fields should still be populated.
	if hb.ProviderHealth != HealthHealthy {
		t.Errorf("expected healthy, got %s", hb.ProviderHealth)
	}
}

func TestMultiProviderWorstHealth(t *testing.T) {
	tests := []struct {
		name    string
		entries []ProviderHealthEntry
		want    ProviderHealth
	}{
		{
			name:    "all healthy",
			entries: []ProviderHealthEntry{{Health: HealthHealthy}, {Health: HealthHealthy}},
			want:    HealthHealthy,
		},
		{
			name:    "one degraded",
			entries: []ProviderHealthEntry{{Health: HealthHealthy}, {Health: HealthDegraded}},
			want:    HealthDegraded,
		},
		{
			name:    "one down one degraded",
			entries: []ProviderHealthEntry{{Health: HealthDegraded}, {Health: HealthDown}},
			want:    HealthDown,
		},
		{
			name:    "empty entries",
			entries: nil,
			want:    HealthHealthy,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WorstHealth(tt.entries)
			if got != tt.want {
				t.Errorf("WorstHealth: got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestBrokerSyncTrackersAddsNewProviders(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	discovered := []string{"claude"}
	b := New(Config{
		DiscoverFn: func() []string { return discovered },
	}, nil)

	// Initially one tracker.
	if len(b.healthTrackers) != 1 {
		t.Fatalf("expected 1 tracker, got %d", len(b.healthTrackers))
	}

	// Add a new runtime.
	discovered = []string{"claude", "codex"}
	b.syncTrackers()

	if len(b.healthTrackers) != 2 {
		t.Fatalf("expected 2 trackers after sync, got %d", len(b.healthTrackers))
	}

	if _, ok := b.healthTrackers["codex"]; !ok {
		t.Error("expected codex tracker to be created")
	}
}

func TestBrokerMinNextProbeIn(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	b := New(Config{
		PatrolInterval: 5 * time.Minute,
		DiscoverFn:     func() []string { return []string{"claude", "codex"} },
	}, nil)

	// Both healthy — min should be patrol interval.
	got := b.minNextProbeIn()
	if got != 5*time.Minute {
		t.Errorf("both healthy: got %s, want 5m", got)
	}

	// Drive codex to degraded.
	codexHT := b.healthTrackers["codex"]
	codexHT.SetProbeFn(func() error { return errors.New("fail") })
	codexHT.Probe() // 1 failure
	codexHT.Probe() // 2 failures → degraded

	got = b.minNextProbeIn()
	if got != DegradedProbeInterval {
		t.Errorf("codex degraded: got %s, want %s", got, DegradedProbeInterval)
	}
}
