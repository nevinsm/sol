package status

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/broker"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/consul"
	"github.com/nevinsm/sol/internal/store"
)

// --- Sphere-level mock implementations ---

type mockWorldLister struct {
	worlds []store.World
	err    error
}

func (m *mockWorldLister) ListWorlds() ([]store.World, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.worlds, nil
}

// mockWorldOpener returns a function that either fails or returns a mock store.
// For sphere tests we typically want it to fail since we can't create real SQLite
// databases in unit tests without more setup. Use failingWorldOpener for degraded tests.
func failingWorldOpener(world string) (*store.Store, error) {
	return nil, fmt.Errorf("mock: cannot open world %q", world)
}

// --- Tests ---

func TestGatherSphereEmpty(t *testing.T) {
	setupTestHome(t)
	clearPrefectPID(t)

	lister := &mockWorldLister{}
	sphere := &mockSphereStore{}
	checker := &mockChecker{alive: map[string]bool{}}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	if len(result.Worlds) != 0 {
		t.Errorf("Worlds = %d, want 0", len(result.Worlds))
	}
	// Prefect not running → degraded.
	if result.Health != "degraded" {
		t.Errorf("Health = %q, want %q", result.Health, "degraded")
	}
	if result.SOLHome != config.Home() {
		t.Errorf("SOLHome = %q, want %q", result.SOLHome, config.Home())
	}
}

func TestGatherSphereWithWorlds(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	lister := &mockWorldLister{
		worlds: []store.World{
			{Name: "alpha", SourceRepo: "/repos/alpha"},
			{Name: "beta", SourceRepo: "/repos/beta"},
		},
	}
	sphere := &mockSphereStore{}
	checker := &mockChecker{alive: map[string]bool{}}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	if len(result.Worlds) != 2 {
		t.Fatalf("Worlds = %d, want 2", len(result.Worlds))
	}
	if result.Worlds[0].Name != "alpha" {
		t.Errorf("Worlds[0].Name = %q, want %q", result.Worlds[0].Name, "alpha")
	}
	if result.Worlds[1].Name != "beta" {
		t.Errorf("Worlds[1].Name = %q, want %q", result.Worlds[1].Name, "beta")
	}
	// Both worlds used failingWorldOpener → health "unknown".
	if result.Worlds[0].Health != "unknown" {
		t.Errorf("Worlds[0].Health = %q, want %q", result.Worlds[0].Health, "unknown")
	}
}

func TestGatherSphereProcessChecks(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	lister := &mockWorldLister{
		worlds: []store.World{
			{Name: "alpha"},
			{Name: "beta"},
		},
	}
	sphere := &mockSphereStore{}
	checker := &mockChecker{
		alive: map[string]bool{
			"sol-chronicle":      true,
			"sol-alpha-forge":    true,
			"sol-beta-forge":     false,
			"sol-alpha-sentinel": true,
			"sol-beta-sentinel":  false,
		},
	}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	// Chronicle.
	if !result.Chronicle.Running {
		t.Error("Chronicle.Running = false, want true")
	}

	// Alpha: forge running, sentinel running.
	if !result.Worlds[0].Forge {
		t.Error("Worlds[0].Forge = false, want true")
	}
	if !result.Worlds[0].Sentinel {
		t.Error("Worlds[0].Sentinel = false, want true")
	}

	// Beta: forge not running, sentinel not running.
	if result.Worlds[1].Forge {
		t.Error("Worlds[1].Forge = true, want false")
	}
	if result.Worlds[1].Sentinel {
		t.Error("Worlds[1].Sentinel = true, want false")
	}
}

func TestSphereHealthComputation(t *testing.T) {
	tests := []struct {
		name   string
		status SphereStatus
		want   string
	}{
		{
			name: "healthy: prefect running, consul fresh, no issues",
			status: SphereStatus{
				Prefect: PrefectInfo{Running: true},
				Consul:  ConsulInfo{Stale: false},
				Worlds: []WorldSummary{
					{Health: "healthy"},
				},
			},
			want: "healthy",
		},
		{
			name: "degraded: prefect not running",
			status: SphereStatus{
				Prefect: PrefectInfo{Running: false},
				Consul:  ConsulInfo{Stale: false},
			},
			want: "degraded",
		},
		{
			name: "degraded: consul stale",
			status: SphereStatus{
				Prefect: PrefectInfo{Running: true},
				Consul:  ConsulInfo{Stale: true},
			},
			want: "degraded",
		},
		{
			name: "unhealthy: world has dead sessions",
			status: SphereStatus{
				Prefect: PrefectInfo{Running: true},
				Consul:  ConsulInfo{Stale: false},
				Worlds: []WorldSummary{
					{Health: "healthy", Dead: 1},
				},
			},
			want: "unhealthy",
		},
		{
			name: "unhealthy: world health is unhealthy",
			status: SphereStatus{
				Prefect: PrefectInfo{Running: true},
				Consul:  ConsulInfo{Stale: false},
				Worlds: []WorldSummary{
					{Health: "unhealthy"},
				},
			},
			want: "unhealthy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeSphereHealth(&tt.status)
			if got != tt.want {
				t.Errorf("computeSphereHealth() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWorldSummaryDegrades(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	lister := &mockWorldLister{
		worlds: []store.World{
			{Name: "broken"},
		},
	}
	sphere := &mockSphereStore{}
	checker := &mockChecker{alive: map[string]bool{}}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	if len(result.Worlds) != 1 {
		t.Fatalf("Worlds = %d, want 1", len(result.Worlds))
	}
	if result.Worlds[0].Health != "unknown" {
		t.Errorf("Health = %q, want %q", result.Worlds[0].Health, "unknown")
	}
}

func TestGatherSphereAgentCounts(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	lister := &mockWorldLister{
		worlds: []store.World{
			{Name: "haven"},
		},
	}
	sphere := &mockSphereStore{
		agents: []store.Agent{
			{ID: "haven/Toast", Name: "Toast", World: "haven", State: "working"},
			{ID: "haven/Sage", Name: "Sage", World: "haven", State: "working"},
			{ID: "haven/Copper", Name: "Copper", World: "haven", State: "idle"},
			{ID: "haven/Jade", Name: "Jade", World: "haven", State: "stalled"},
		},
	}
	checker := &mockChecker{
		alive: map[string]bool{
			"sol-haven-Toast":  true,
			"sol-haven-Sage":   false, // dead session
			"sol-haven-Copper": false, // idle — no session expected
			"sol-haven-Jade":   false,
		},
	}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	if len(result.Worlds) != 1 {
		t.Fatalf("Worlds = %d, want 1", len(result.Worlds))
	}
	w := result.Worlds[0]
	if w.Agents != 4 {
		t.Errorf("Agents = %d, want 4", w.Agents)
	}
	if w.Working != 2 {
		t.Errorf("Working = %d, want 2", w.Working)
	}
	if w.Idle != 1 {
		t.Errorf("Idle = %d, want 1", w.Idle)
	}
	if w.Stalled != 1 {
		t.Errorf("Stalled = %d, want 1", w.Stalled)
	}
	if w.Dead != 1 {
		t.Errorf("Dead = %d, want 1 (only working agents with dead sessions)", w.Dead)
	}
}

func TestGatherSphereConsulInfo(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	// Write a fresh consul heartbeat.
	hb := &consul.Heartbeat{
		Timestamp:   time.Now().UTC(),
		PatrolCount: 42,
		Status:      "running",
	}
	if err := consul.WriteHeartbeat(config.Home(), hb); err != nil {
		t.Fatal(err)
	}

	lister := &mockWorldLister{}
	sphere := &mockSphereStore{}
	checker := &mockChecker{alive: map[string]bool{}}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	if !result.Consul.Running {
		t.Error("Consul.Running = false, want true")
	}
	if result.Consul.PatrolCount != 42 {
		t.Errorf("Consul.PatrolCount = %d, want 42", result.Consul.PatrolCount)
	}
	if result.Consul.Stale {
		t.Error("Consul.Stale = true, want false (fresh heartbeat)")
	}
	if result.Consul.HeartbeatAge == "" {
		t.Error("Consul.HeartbeatAge is empty, want non-empty")
	}
}

func TestGatherSphereConsulStale(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	// Write a stale consul heartbeat (>10 minutes old).
	hb := &consul.Heartbeat{
		Timestamp:   time.Now().UTC().Add(-15 * time.Minute),
		PatrolCount: 10,
		Status:      "running",
	}
	if err := consul.WriteHeartbeat(config.Home(), hb); err != nil {
		t.Fatal(err)
	}

	lister := &mockWorldLister{}
	sphere := &mockSphereStore{}
	checker := &mockChecker{alive: map[string]bool{}}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	if !result.Consul.Stale {
		t.Error("Consul.Stale = false, want true (heartbeat >10m old)")
	}
	// Stale consul → degraded sphere.
	if result.Health != "degraded" {
		t.Errorf("Health = %q, want %q", result.Health, "degraded")
	}
}

func TestGatherSphereBrokerInfo(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	// Write a fresh broker heartbeat.
	hb := &broker.Heartbeat{
		Timestamp:   time.Now().UTC(),
		PatrolCount: 7,
		Status:      "running",
		Accounts:    2,
		AgentDirs:   5,
	}
	writeBrokerHeartbeat(t, hb)

	lister := &mockWorldLister{}
	sphere := &mockSphereStore{}
	checker := &mockChecker{alive: map[string]bool{}}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	if !result.Broker.Running {
		t.Error("Broker.Running = false, want true")
	}
	if result.Broker.PatrolCount != 7 {
		t.Errorf("Broker.PatrolCount = %d, want 7", result.Broker.PatrolCount)
	}
	if result.Broker.Accounts != 2 {
		t.Errorf("Broker.Accounts = %d, want 2", result.Broker.Accounts)
	}
	if result.Broker.AgentDirs != 5 {
		t.Errorf("Broker.AgentDirs = %d, want 5", result.Broker.AgentDirs)
	}
	if result.Broker.Stale {
		t.Error("Broker.Stale = true, want false (fresh heartbeat)")
	}
	if result.Broker.HeartbeatAge == "" {
		t.Error("Broker.HeartbeatAge is empty, want non-empty")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{90 * time.Second, "1m"},
		{2 * time.Hour, "2h"},
		{3 * 24 * time.Hour, "3d"},
		{0, "0s"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := FormatDuration(tt.d)
			if got != tt.want {
				t.Errorf("FormatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestGatherSpherePrefectRunning(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	lister := &mockWorldLister{}
	sphere := &mockSphereStore{}
	checker := &mockChecker{alive: map[string]bool{}}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	if !result.Prefect.Running {
		t.Error("Prefect.Running = false, want true")
	}
	if result.Prefect.PID != os.Getpid() {
		t.Errorf("Prefect.PID = %d, want %d", result.Prefect.PID, os.Getpid())
	}
}

// writeBrokerHeartbeat writes a broker heartbeat file for testing.
func writeBrokerHeartbeat(t *testing.T, hb *broker.Heartbeat) {
	t.Helper()
	dir := filepath.Join(config.Home(), ".runtime")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(hb)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broker-heartbeat.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGatherSphereWithEnvoysAndGovernor(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	lister := &mockWorldLister{
		worlds: []store.World{
			{Name: "haven"},
		},
	}
	sphere := &mockSphereStore{
		agents: []store.Agent{
			{ID: "haven/Toast", Name: "Toast", World: "haven", Role: "agent", State: "working"},
			{ID: "haven/Crisp", Name: "Crisp", World: "haven", Role: "agent", State: "idle"},
			{ID: "haven/Scout", Name: "Scout", World: "haven", Role: "envoy", State: "working"},
			{ID: "haven/Ranger", Name: "Ranger", World: "haven", Role: "envoy", State: "idle"},
			{ID: "haven/governor", Name: "governor", World: "haven", Role: "governor", State: "idle"},
			{ID: "haven/forge", Name: "forge", World: "haven", Role: "forge", State: "idle"},
		},
	}
	checker := &mockChecker{
		alive: map[string]bool{
			"sol-haven-Toast":    true,
			"sol-haven-governor": true,
		},
	}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	if len(result.Worlds) != 1 {
		t.Fatalf("Worlds = %d, want 1", len(result.Worlds))
	}
	w := result.Worlds[0]

	// Only outpost agents counted.
	if w.Agents != 2 {
		t.Errorf("Agents = %d, want 2", w.Agents)
	}
	if w.Envoys != 2 {
		t.Errorf("Envoys = %d, want 2", w.Envoys)
	}
	if !w.Governor {
		t.Error("Governor = false, want true")
	}
	// Only outpost agents in working/idle counts.
	if w.Working != 1 {
		t.Errorf("Working = %d, want 1", w.Working)
	}
	if w.Idle != 1 {
		t.Errorf("Idle = %d, want 1", w.Idle)
	}
}
