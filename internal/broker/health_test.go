package broker

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHealthStateTransitions(t *testing.T) {
	failCount := 0
	ht := NewHealthTracker()
	ht.SetProbeFn(func() error {
		failCount++
		return errors.New("provider down")
	})

	// Initial state: healthy.
	if ht.State().Health != HealthHealthy {
		t.Fatalf("expected healthy, got %s", ht.State().Health)
	}

	// 1 failure: stay healthy (transient).
	changed := ht.Probe()
	if ht.State().Health != HealthHealthy {
		t.Errorf("after 1 failure: expected healthy, got %s", ht.State().Health)
	}
	if changed {
		t.Error("1 failure should not change state from healthy")
	}
	if ht.State().ConsecutiveFailures != 1 {
		t.Errorf("expected 1 consecutive failure, got %d", ht.State().ConsecutiveFailures)
	}

	// 2 failures: degraded.
	changed = ht.Probe()
	if ht.State().Health != HealthDegraded {
		t.Errorf("after 2 failures: expected degraded, got %s", ht.State().Health)
	}
	if !changed {
		t.Error("2 failures should change state from healthy to degraded")
	}

	// 3 failures: still degraded.
	changed = ht.Probe()
	if ht.State().Health != HealthDegraded {
		t.Errorf("after 3 failures: expected degraded, got %s", ht.State().Health)
	}
	if changed {
		t.Error("3 failures should not change state (still degraded)")
	}

	// 4 failures: down.
	changed = ht.Probe()
	if ht.State().Health != HealthDown {
		t.Errorf("after 4 failures: expected down, got %s", ht.State().Health)
	}
	if !changed {
		t.Error("4 failures should change state from degraded to down")
	}
	if ht.State().ConsecutiveFailures != 4 {
		t.Errorf("expected 4 consecutive failures, got %d", ht.State().ConsecutiveFailures)
	}

	// 5 failures: still down.
	changed = ht.Probe()
	if ht.State().Health != HealthDown {
		t.Errorf("after 5 failures: expected down, got %s", ht.State().Health)
	}
	if changed {
		t.Error("5 failures should not change state (still down)")
	}
}

func TestHealthRecovery(t *testing.T) {
	failing := true
	ht := NewHealthTracker()
	ht.SetProbeFn(func() error {
		if failing {
			return errors.New("provider down")
		}
		return nil
	})

	// Drive to down state (4 failures).
	for i := 0; i < 4; i++ {
		ht.Probe()
	}
	if ht.State().Health != HealthDown {
		t.Fatalf("expected down after 4 failures, got %s", ht.State().Health)
	}

	// 1 success: back to healthy.
	failing = false
	changed := ht.Probe()
	if ht.State().Health != HealthHealthy {
		t.Errorf("after recovery: expected healthy, got %s", ht.State().Health)
	}
	if !changed {
		t.Error("recovery should change state from down to healthy")
	}
	if ht.State().ConsecutiveFailures != 0 {
		t.Errorf("after recovery: expected 0 failures, got %d", ht.State().ConsecutiveFailures)
	}
}

func TestHealthRecoveryFromDegraded(t *testing.T) {
	failing := true
	ht := NewHealthTracker()
	ht.SetProbeFn(func() error {
		if failing {
			return errors.New("provider error")
		}
		return nil
	})

	// Drive to degraded (2 failures).
	ht.Probe() // 1 failure: healthy
	ht.Probe() // 2 failures: degraded
	if ht.State().Health != HealthDegraded {
		t.Fatalf("expected degraded, got %s", ht.State().Health)
	}

	// 1 success: back to healthy.
	failing = false
	changed := ht.Probe()
	if ht.State().Health != HealthHealthy {
		t.Errorf("after recovery: expected healthy, got %s", ht.State().Health)
	}
	if !changed {
		t.Error("recovery should change state from degraded to healthy")
	}
}

func TestHealthLastHealthyUpdated(t *testing.T) {
	ht := NewHealthTracker()
	ht.SetProbeFn(func() error { return nil })

	before := ht.State().LastHealthy
	time.Sleep(time.Millisecond) // ensure time advances
	ht.Probe()
	after := ht.State().LastHealthy

	if !after.After(before) {
		t.Error("LastHealthy should advance after successful probe")
	}
}

func TestHealthLastProbeUpdated(t *testing.T) {
	ht := NewHealthTracker()
	ht.SetProbeFn(func() error { return errors.New("fail") })

	if !ht.State().LastProbe.IsZero() {
		t.Error("LastProbe should be zero before first probe")
	}

	ht.Probe()
	if ht.State().LastProbe.IsZero() {
		t.Error("LastProbe should be set after probe")
	}
}

func TestShouldProbeHealthy(t *testing.T) {
	now := time.Now()
	ht := NewHealthTracker()
	ht.now = func() time.Time { return now }
	ht.SetProbeFn(func() error { return nil })

	// Never probed — should probe.
	if !ht.ShouldProbe(5 * time.Minute) {
		t.Error("should probe when never probed")
	}

	// Probe, then check again immediately.
	ht.Probe()
	if ht.ShouldProbe(5 * time.Minute) {
		t.Error("should not probe immediately after probing (healthy)")
	}

	// Advance past patrol interval.
	now = now.Add(6 * time.Minute)
	if !ht.ShouldProbe(5 * time.Minute) {
		t.Error("should probe after patrol interval elapses (healthy)")
	}
}

func TestShouldProbeDegraded(t *testing.T) {
	now := time.Now()
	ht := NewHealthTracker()
	ht.now = func() time.Time { return now }
	ht.SetProbeFn(func() error { return errors.New("fail") })

	// Drive to degraded.
	ht.Probe() // 1 failure
	ht.Probe() // 2 failures → degraded

	if ht.State().Health != HealthDegraded {
		t.Fatalf("expected degraded, got %s", ht.State().Health)
	}

	// Should not probe immediately.
	if ht.ShouldProbe(5 * time.Minute) {
		t.Error("should not probe immediately after probing (degraded)")
	}

	// Advance 31 seconds — past DegradedProbeInterval.
	now = now.Add(31 * time.Second)
	if !ht.ShouldProbe(5 * time.Minute) {
		t.Error("should probe after 30s in degraded state")
	}
}

func TestShouldProbeDown(t *testing.T) {
	now := time.Now()
	ht := NewHealthTracker()
	ht.now = func() time.Time { return now }
	ht.SetProbeFn(func() error { return errors.New("fail") })

	// Drive to down (4 failures).
	for i := 0; i < 4; i++ {
		ht.Probe()
	}
	if ht.State().Health != HealthDown {
		t.Fatalf("expected down, got %s", ht.State().Health)
	}

	// Should not probe immediately.
	if ht.ShouldProbe(5 * time.Minute) {
		t.Error("should not probe immediately after probing (down)")
	}

	// First backoff interval is 30s.
	now = now.Add(31 * time.Second)
	if !ht.ShouldProbe(5 * time.Minute) {
		t.Error("should probe after 30s in down state (first backoff)")
	}
}

func TestBackoffProgression(t *testing.T) {
	ht := NewHealthTracker()
	ht.SetProbeFn(func() error { return errors.New("fail") })

	// Drive to down (4 failures) — backoff index starts at 0.
	for i := 0; i < 4; i++ {
		ht.Probe()
	}

	// After entering down state, NextProbeIn should reflect backoff schedule.
	expected := []time.Duration{
		30 * time.Second, // index 0 (just entered down)
		1 * time.Minute,  // index 1 (after one more failure in down)
		2 * time.Minute,  // index 2
		5 * time.Minute,  // index 3 (cap)
		5 * time.Minute,  // still capped
	}

	for i, want := range expected {
		got := ht.NextProbeIn(5 * time.Minute)
		if got != want {
			t.Errorf("backoff step %d: got %s, want %s", i, got, want)
		}
		ht.Probe() // another failure advances backoff
	}
}

func TestNextProbeInHealthy(t *testing.T) {
	ht := NewHealthTracker()
	got := ht.NextProbeIn(5 * time.Minute)
	if got != 5*time.Minute {
		t.Errorf("healthy NextProbeIn: got %s, want 5m", got)
	}
}

func TestNextProbeInDegraded(t *testing.T) {
	ht := NewHealthTracker()
	ht.SetProbeFn(func() error { return errors.New("fail") })
	ht.Probe() // 1 failure
	ht.Probe() // 2 failures → degraded

	got := ht.NextProbeIn(5 * time.Minute)
	if got != DegradedProbeInterval {
		t.Errorf("degraded NextProbeIn: got %s, want %s", got, DegradedProbeInterval)
	}
}

func TestReadProviderHealth(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// No heartbeat — returns nil.
	info, err := ReadProviderHealth()
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Error("expected nil when no heartbeat")
	}

	// Write a heartbeat with health data.
	runtimeDir := filepath.Join(solHome, ".runtime")
	os.MkdirAll(runtimeDir, 0o755)

	now := time.Now().UTC()
	hb := Heartbeat{
		Timestamp:           now,
		PatrolCount:         5,
		Status:              "running",
		ProviderHealth:      HealthDegraded,
		ConsecutiveFailures: 2,
		LastProbe:           now,
		LastHealthy:         now.Add(-5 * time.Minute),
	}
	data, _ := json.MarshalIndent(hb, "", "  ")
	os.WriteFile(filepath.Join(runtimeDir, "broker-heartbeat.json"), append(data, '\n'), 0o644)

	info, err = ReadProviderHealth()
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected non-nil health info")
	}
	if info.Health != HealthDegraded {
		t.Errorf("expected degraded, got %s", info.Health)
	}
	if info.ConsecutiveFailures != 2 {
		t.Errorf("expected 2 failures, got %d", info.ConsecutiveFailures)
	}
}

func TestReadProviderHealthDefaultsToHealthy(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Write a legacy heartbeat without provider_health field.
	runtimeDir := filepath.Join(solHome, ".runtime")
	os.MkdirAll(runtimeDir, 0o755)

	hb := map[string]any{
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
		"patrol_count": 1,
		"status":       "running",
	}
	data, _ := json.Marshal(hb)
	os.WriteFile(filepath.Join(runtimeDir, "broker-heartbeat.json"), data, 0o644)

	info, err := ReadProviderHealth()
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected non-nil health info")
	}
	if info.Health != HealthHealthy {
		t.Errorf("expected healthy (default), got %s", info.Health)
	}
}

func TestHeartbeatIncludesHealthFields(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Set up minimal account registry.
	accountsDir := filepath.Join(solHome, ".accounts")
	os.MkdirAll(accountsDir, 0o755)
	registry := map[string]any{
		"accounts": map[string]any{},
		"default":  "",
	}
	regData, _ := json.Marshal(registry)
	os.WriteFile(filepath.Join(accountsDir, "accounts.json"), regData, 0o644)

	// Create broker with a mock health tracker.
	b := New(Config{RefreshMargin: 30 * time.Minute}, nil)
	ht := NewHealthTracker()
	ht.SetProbeFn(func() error { return nil }) // always healthy
	b.SetHealthTracker(ht)

	// Run one patrol.
	b.patrol()

	// Read heartbeat and verify health fields.
	hb, err := ReadHeartbeat()
	if err != nil {
		t.Fatal(err)
	}
	if hb == nil {
		t.Fatal("expected heartbeat")
	}
	if hb.ProviderHealth != HealthHealthy {
		t.Errorf("expected healthy, got %s", hb.ProviderHealth)
	}
	if !hb.LastProbe.IsZero() && hb.LastHealthy.IsZero() {
		t.Error("LastHealthy should be set when probed successfully")
	}
}

func TestHealthTrackerBackoffResetsOnRecovery(t *testing.T) {
	failing := true
	ht := NewHealthTracker()
	ht.SetProbeFn(func() error {
		if failing {
			return errors.New("fail")
		}
		return nil
	})

	// Drive to down with advanced backoff.
	for i := 0; i < 6; i++ { // 4 to enter down, 2 more to advance backoff
		ht.Probe()
	}
	if ht.State().Health != HealthDown {
		t.Fatalf("expected down, got %s", ht.State().Health)
	}

	// Recover.
	failing = false
	ht.Probe()
	if ht.State().Health != HealthHealthy {
		t.Fatalf("expected healthy after recovery, got %s", ht.State().Health)
	}

	// Backoff should be reset — NextProbeIn should return patrol interval.
	got := ht.NextProbeIn(5 * time.Minute)
	if got != 5*time.Minute {
		t.Errorf("after recovery NextProbeIn: got %s, want 5m", got)
	}

	// Fail again — backoff should start from beginning.
	failing = true
	for i := 0; i < 4; i++ {
		ht.Probe()
	}
	got = ht.NextProbeIn(5 * time.Minute)
	if got != 30*time.Second {
		t.Errorf("after re-entering down: got %s, want 30s", got)
	}
}
