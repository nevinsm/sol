package consul

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// ---------------------------------------------------------------------------
// gcRouteFailures
// ---------------------------------------------------------------------------

func TestGCRouteFailures_RemovesClosedEscalations(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Create two escalations — keep one open, resolve the other.
	openID, err := sphereStore.CreateEscalation("low", "test", "open esc")
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}
	closedID, err := sphereStore.CreateEscalation("low", "test", "closed esc")
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}
	if err := sphereStore.ResolveEscalation(closedID); err != nil {
		t.Fatalf("failed to resolve escalation: %v", err)
	}

	cfg := Config{SolHome: solHome}
	d := New(cfg, sphereStore, newMockSessions(), nil, nil)

	// Seed route failures for both escalations.
	now := time.Now()
	d.routeFailures[openID] = &routeFailure{count: 3, lastFail: now}
	d.routeFailures[closedID] = &routeFailure{count: 5, lastFail: now}

	d.gcRouteFailures()

	if _, ok := d.routeFailures[openID]; !ok {
		t.Error("route failure entry for open escalation should be retained")
	}
	if _, ok := d.routeFailures[closedID]; ok {
		t.Error("route failure entry for closed escalation should be removed")
	}
}

func TestGCRouteFailures_CapEvictsOldest(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	cfg := Config{SolHome: solHome}
	d := New(cfg, sphereStore, newMockSessions(), nil, nil)

	// Create maxRouteFailureEntries + 10 escalations (all open) with route failures.
	// Give each a distinct lastFail so we can verify the oldest are evicted.
	baseTime := time.Now().Add(-24 * time.Hour)
	total := maxRouteFailureEntries + 10

	for i := range total {
		escID, err := sphereStore.CreateEscalation("low", "test", fmt.Sprintf("esc-%d", i))
		if err != nil {
			t.Fatalf("failed to create escalation %d: %v", i, err)
		}
		d.routeFailures[escID] = &routeFailure{
			count:    1,
			lastFail: baseTime.Add(time.Duration(i) * time.Second),
		}
	}

	if len(d.routeFailures) != total {
		t.Fatalf("expected %d route failure entries, got %d", total, len(d.routeFailures))
	}

	d.gcRouteFailures()

	if len(d.routeFailures) != maxRouteFailureEntries {
		t.Errorf("after gc, route failures = %d, want %d", len(d.routeFailures), maxRouteFailureEntries)
	}

	// The 10 oldest entries (smallest lastFail) should have been evicted.
	// Verify remaining entries all have lastFail >= baseTime + 10s.
	cutoff := baseTime.Add(time.Duration(10) * time.Second)
	for id, rf := range d.routeFailures {
		if rf.lastFail.Before(cutoff) {
			t.Errorf("entry %s has lastFail %v which is before cutoff %v — should have been evicted",
				id, rf.lastFail, cutoff)
		}
	}
}

// ---------------------------------------------------------------------------
// Route failure persistence (saveState / loadState / restoreState)
// ---------------------------------------------------------------------------

func TestRouteFailurePersistence_RoundTrip(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	cfg := Config{SolHome: solHome}
	d := New(cfg, sphereStore, newMockSessions(), nil, nil)

	// Populate route failures.
	now := time.Now().UTC().Truncate(time.Second)
	d.routeFailures["sol-aaaa"] = &routeFailure{count: 3, lastFail: now}
	d.routeFailures["sol-bbbb"] = &routeFailure{count: 7, lastFail: now.Add(-5 * time.Minute)}

	d.saveState()

	// Read persisted file and verify route failures are present.
	data, err := os.ReadFile(statePath(solHome))
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}

	var st consulState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("failed to parse state JSON: %v", err)
	}

	if len(st.RouteFailures) != 2 {
		t.Fatalf("persisted route failures = %d, want 2", len(st.RouteFailures))
	}
	if st.RouteFailures["sol-aaaa"].Count != 3 {
		t.Errorf("sol-aaaa count = %d, want 3", st.RouteFailures["sol-aaaa"].Count)
	}
	if st.RouteFailures["sol-bbbb"].Count != 7 {
		t.Errorf("sol-bbbb count = %d, want 7", st.RouteFailures["sol-bbbb"].Count)
	}

	// Verify loadState round-trip.
	loaded := loadState(solHome, nil)
	if len(loaded.RouteFailures) != 2 {
		t.Fatalf("loaded route failures = %d, want 2", len(loaded.RouteFailures))
	}
	if !loaded.RouteFailures["sol-aaaa"].LastFail.Equal(now) {
		t.Errorf("sol-aaaa lastFail = %v, want %v", loaded.RouteFailures["sol-aaaa"].LastFail, now)
	}
}

func TestRouteFailurePersistence_RestoreState(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	cfg := Config{SolHome: solHome}

	// First consul: save route failures.
	d1 := New(cfg, sphereStore, newMockSessions(), nil, nil)
	now := time.Now().UTC().Truncate(time.Second)
	d1.routeFailures["sol-cccc"] = &routeFailure{count: 2, lastFail: now}
	d1.saveState()

	// Second consul: restore state (simulates restart).
	d2 := New(cfg, sphereStore, newMockSessions(), nil, nil)
	d2.restoreState()

	if len(d2.routeFailures) != 1 {
		t.Fatalf("restored route failures = %d, want 1", len(d2.routeFailures))
	}
	rf, ok := d2.routeFailures["sol-cccc"]
	if !ok {
		t.Fatal("sol-cccc not found in restored route failures")
	}
	if rf.count != 2 {
		t.Errorf("restored count = %d, want 2", rf.count)
	}
	if !rf.lastFail.Equal(now) {
		t.Errorf("restored lastFail = %v, want %v", rf.lastFail, now)
	}
}

// ---------------------------------------------------------------------------
// isPrefectAlive — PID file recency fallback
// ---------------------------------------------------------------------------

func TestIsPrefectAlive_FreshPIDFile(t *testing.T) {
	solHome := setupSolHome(t)

	runtimeDir := filepath.Join(solHome, ".runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	// Write current process PID (guaranteed to be running).
	pidPath := filepath.Join(runtimeDir, "prefect.pid")
	if err := os.WriteFile(pidPath, fmt.Appendf(nil, "%d\n", os.Getpid()), 0o644); err != nil {
		t.Fatalf("failed to write PID file: %v", err)
	}

	// No heartbeat file — isPrefectAlive should fall back to PID file mtime.
	// PID file was just written (fresh), so should return true.

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	cfg := Config{
		SolHome:             solHome,
		PrefectHeartbeatMax: 10 * time.Minute,
	}
	d := New(cfg, sphereStore, newMockSessions(), nil, nil)

	if !d.isPrefectAlive() {
		t.Error("isPrefectAlive should return true for a fresh PID file with running process")
	}
}

func TestIsPrefectAlive_StalePIDFile(t *testing.T) {
	solHome := setupSolHome(t)

	runtimeDir := filepath.Join(solHome, ".runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	// Write current process PID (guaranteed to be running).
	pidPath := filepath.Join(runtimeDir, "prefect.pid")
	if err := os.WriteFile(pidPath, fmt.Appendf(nil, "%d\n", os.Getpid()), 0o644); err != nil {
		t.Fatalf("failed to write PID file: %v", err)
	}

	// Backdate the PID file mtime to make it stale.
	staleTime := time.Now().Add(-20 * time.Minute)
	if err := os.Chtimes(pidPath, staleTime, staleTime); err != nil {
		t.Fatalf("failed to set PID file mtime: %v", err)
	}

	// No heartbeat file — isPrefectAlive should fall back to PID file mtime.
	// PID file is stale (20 min old, threshold 10 min), so should return false.

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	cfg := Config{
		SolHome:             solHome,
		PrefectHeartbeatMax: 10 * time.Minute,
	}
	d := New(cfg, sphereStore, newMockSessions(), nil, nil)

	if d.isPrefectAlive() {
		t.Error("isPrefectAlive should return false for a stale PID file")
	}
}
