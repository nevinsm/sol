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

// mockEscalationLister implements EscalationLister for testing.
type mockEscalationLister struct {
	escalations []store.Escalation
	err         error
}

func (m *mockEscalationLister) ListOpenEscalations() ([]store.Escalation, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.escalations, nil
}

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

	// Write chronicle PID file (PID-based detection).
	chronicleCleanup := writeChroniclePID(t, os.Getpid())
	defer chronicleCleanup()

	lister := &mockWorldLister{
		worlds: []store.World{
			{Name: "alpha"},
			{Name: "beta"},
		},
	}
	sphere := &mockSphereStore{}
	checker := &mockChecker{
		alive: map[string]bool{
			"sol-alpha-sentinel": true,
			"sol-beta-sentinel":  false,
		},
	}

	// Write forge PID file for alpha (running), not for beta.
	forgePIDCleanup := writeForgePID(t, "alpha", os.Getpid())
	defer forgePIDCleanup()

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	// Chronicle (detected via PID file).
	if !result.Chronicle.Running {
		t.Error("Chronicle.Running = false, want true")
	}

	// Alpha: forge running (PID file), sentinel running (session).
	if !result.Worlds[0].Forge {
		t.Error("Worlds[0].Forge = false, want true")
	}
	if !result.Worlds[0].Sentinel {
		t.Error("Worlds[0].Sentinel = false, want true")
	}

	// Beta: forge not running (no PID file), sentinel not running.
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

func TestGatherSphereLedgerPID(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	// Write ledger PID file with our own PID (known-alive).
	ledgerCleanup := writeLedgerPID(t, os.Getpid())
	defer ledgerCleanup()

	lister := &mockWorldLister{}
	sphere := &mockSphereStore{}
	checker := &mockChecker{alive: map[string]bool{}}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	if !result.Ledger.Running {
		t.Error("Ledger.Running = false, want true")
	}
	if result.Ledger.PID != os.Getpid() {
		t.Errorf("Ledger.PID = %d, want %d", result.Ledger.PID, os.Getpid())
	}
}

func TestGatherSphereChroniclePIDFallback(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	// Write chronicle PID file with our own PID (known-alive).
	chronicleCleanup := writeChroniclePID(t, os.Getpid())
	defer chronicleCleanup()

	lister := &mockWorldLister{}
	sphere := &mockSphereStore{}
	// No tmux session for chronicle.
	checker := &mockChecker{alive: map[string]bool{}}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	if !result.Chronicle.Running {
		t.Error("Chronicle.Running = false, want true (PID fallback)")
	}
	if result.Chronicle.PID != os.Getpid() {
		t.Errorf("Chronicle.PID = %d, want %d", result.Chronicle.PID, os.Getpid())
	}
}

func TestWorldSummaryIncludesCapacity(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	// Write a world.toml with capacity = 5.
	worldName := "capped"
	worldDir := filepath.Join(config.Home(), worldName)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configContent := `[agents]
capacity = 5
model_tier = "sonnet"
`
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	lister := &mockWorldLister{
		worlds: []store.World{
			{Name: worldName},
		},
	}
	sphere := &mockSphereStore{
		agents: []store.Agent{
			{ID: worldName + "/A", Name: "A", World: worldName, State: "working"},
			{ID: worldName + "/B", Name: "B", World: worldName, State: "idle"},
		},
	}
	checker := &mockChecker{alive: map[string]bool{
		"sol-capped-A": true,
	}}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	if len(result.Worlds) != 1 {
		t.Fatalf("Worlds = %d, want 1", len(result.Worlds))
	}
	w := result.Worlds[0]
	if w.Capacity != 5 {
		t.Errorf("Capacity = %d, want 5", w.Capacity)
	}
	if w.Agents != 2 {
		t.Errorf("Agents = %d, want 2", w.Agents)
	}
}

func TestWorldSummaryUnlimitedCapacity(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	// No world.toml → capacity = 0 (unlimited/default).
	lister := &mockWorldLister{
		worlds: []store.World{
			{Name: "nocap"},
		},
	}
	sphere := &mockSphereStore{}
	checker := &mockChecker{alive: map[string]bool{}}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	if len(result.Worlds) != 1 {
		t.Fatalf("Worlds = %d, want 1", len(result.Worlds))
	}
	if result.Worlds[0].Capacity != 0 {
		t.Errorf("Capacity = %d, want 0 (unlimited)", result.Worlds[0].Capacity)
	}
}

func TestGatherSphereSleepingWorldShowsAgentCounts(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	// Mark the world as sleeping in world.toml.
	worldDir := filepath.Join(config.Home(), "slumber")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[world]\nsleeping = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lister := &mockWorldLister{
		worlds: []store.World{
			{Name: "slumber"},
		},
	}
	sphere := &mockSphereStore{
		agents: []store.Agent{
			{ID: "slumber/Alpha", Name: "Alpha", World: "slumber", Role: "agent", State: "working"},
			{ID: "slumber/Beta", Name: "Beta", World: "slumber", Role: "agent", State: "idle"},
			{ID: "slumber/Envoy1", Name: "Envoy1", World: "slumber", Role: "envoy", State: "working"},
		},
	}
	checker := &mockChecker{
		alive: map[string]bool{
			"sol-slumber-Alpha":  true,
			"sol-slumber-Beta":   false,
			"sol-slumber-Envoy1": true,
		},
	}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	if len(result.Worlds) != 1 {
		t.Fatalf("Worlds = %d, want 1", len(result.Worlds))
	}
	w := result.Worlds[0]

	// Sleeping flag should be set.
	if !w.Sleeping {
		t.Error("Sleeping = false, want true")
	}
	if w.Health != "sleeping" {
		t.Errorf("Health = %q, want %q", w.Health, "sleeping")
	}

	// Agent counts should still be populated.
	if w.Agents != 2 {
		t.Errorf("Agents = %d, want 2", w.Agents)
	}
	if w.Envoys != 1 {
		t.Errorf("Envoys = %d, want 1", w.Envoys)
	}
	if w.Working != 1 {
		t.Errorf("Working = %d, want 1", w.Working)
	}
	if w.Idle != 1 {
		t.Errorf("Idle = %d, want 1", w.Idle)
	}

	// Infrastructure columns should not be set.
	if w.Forge {
		t.Error("Forge = true, want false (suppressed for sleeping worlds)")
	}
	if w.Sentinel {
		t.Error("Sentinel = true, want false (suppressed for sleeping worlds)")
	}
	if w.Governor {
		t.Error("Governor = true, want false (suppressed for sleeping worlds)")
	}
}

func TestGatherSphereSleepingWorldNoAgents(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	// Mark the world as sleeping.
	worldDir := filepath.Join(config.Home(), "dormant")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[world]\nsleeping = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lister := &mockWorldLister{
		worlds: []store.World{
			{Name: "dormant"},
		},
	}
	sphere := &mockSphereStore{} // no agents
	checker := &mockChecker{alive: map[string]bool{}}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	if len(result.Worlds) != 1 {
		t.Fatalf("Worlds = %d, want 1", len(result.Worlds))
	}
	w := result.Worlds[0]

	if !w.Sleeping {
		t.Error("Sleeping = false, want true")
	}
	if w.Agents != 0 {
		t.Errorf("Agents = %d, want 0", w.Agents)
	}
	if w.Envoys != 0 {
		t.Errorf("Envoys = %d, want 0", w.Envoys)
	}
}

func TestEscalationSummaryAggregatesBySeverity(t *testing.T) {
	setupTestHome(t)
	clearPrefectPID(t)

	lister := &mockWorldLister{}
	sphere := &mockSphereStore{}
	checker := &mockChecker{alive: map[string]bool{}}
	escalations := &mockEscalationLister{
		escalations: []store.Escalation{
			{ID: "esc-1", Severity: "critical", Status: "open"},
			{ID: "esc-2", Severity: "high", Status: "open"},
			{ID: "esc-3", Severity: "high", Status: "acknowledged"},
			{ID: "esc-4", Severity: "low", Status: "open"},
		},
	}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil, escalations)

	if result.Escalations == nil {
		t.Fatal("Escalations is nil, want non-nil")
	}
	if result.Escalations.Total != 4 {
		t.Errorf("Escalations.Total = %d, want 4", result.Escalations.Total)
	}
	if result.Escalations.BySeverity["critical"] != 1 {
		t.Errorf("BySeverity[critical] = %d, want 1", result.Escalations.BySeverity["critical"])
	}
	if result.Escalations.BySeverity["high"] != 2 {
		t.Errorf("BySeverity[high] = %d, want 2", result.Escalations.BySeverity["high"])
	}
	if result.Escalations.BySeverity["low"] != 1 {
		t.Errorf("BySeverity[low] = %d, want 1", result.Escalations.BySeverity["low"])
	}
}

func TestEscalationSummaryOmittedWhenNone(t *testing.T) {
	setupTestHome(t)
	clearPrefectPID(t)

	lister := &mockWorldLister{}
	sphere := &mockSphereStore{}
	checker := &mockChecker{alive: map[string]bool{}}
	escalations := &mockEscalationLister{
		escalations: nil, // no escalations
	}

	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil, escalations)

	if result.Escalations != nil {
		t.Errorf("Escalations = %+v, want nil (no escalations)", result.Escalations)
	}
}

func TestEscalationSummaryOmittedWhenNoLister(t *testing.T) {
	setupTestHome(t)
	clearPrefectPID(t)

	lister := &mockWorldLister{}
	sphere := &mockSphereStore{}
	checker := &mockChecker{alive: map[string]bool{}}

	// No escalation lister passed.
	result := GatherSphere(sphere, lister, checker, failingWorldOpener, nil)

	if result.Escalations != nil {
		t.Errorf("Escalations = %+v, want nil (no lister)", result.Escalations)
	}
}
