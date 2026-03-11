package status

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/envoy"
	"github.com/nevinsm/sol/internal/ledger"
	"github.com/nevinsm/sol/internal/store"
)

// --- Mock implementations ---

type mockChecker struct {
	alive map[string]bool
}

func (m *mockChecker) Exists(name string) bool { return m.alive[name] }

type mockSphereStore struct {
	agents []store.Agent
	err    error
}

func (m *mockSphereStore) ListAgents(world, state string) ([]store.Agent, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []store.Agent
	for _, a := range m.agents {
		if world != "" && a.World != world {
			continue
		}
		if state != "" && a.State != state {
			continue
		}
		result = append(result, a)
	}
	return result, nil
}

type mockWorldStore struct {
	items map[string]*store.Writ
}

func (m *mockWorldStore) GetWrit(id string) (*store.Writ, error) {
	item, ok := m.items[id]
	if !ok {
		return nil, fmt.Errorf("writ %q not found", id)
	}
	return item, nil
}

type mockMergeQueueStore struct {
	mrs []store.MergeRequest
	err error
}

func (m *mockMergeQueueStore) ListMergeRequests(phase string) ([]store.MergeRequest, error) {
	if m.err != nil {
		return nil, m.err
	}
	if phase == "" {
		return m.mrs, nil
	}
	var result []store.MergeRequest
	for _, mr := range m.mrs {
		if mr.Phase == phase {
			result = append(result, mr)
		}
	}
	return result, nil
}

// emptyMQStore returns a mock merge queue store with no items.
func emptyMQStore() *mockMergeQueueStore {
	return &mockMergeQueueStore{}
}

// writeLedgerPID writes a ledger PID file for testing. Returns cleanup func.
func writeLedgerPID(t *testing.T, pid int) func() {
	t.Helper()
	dir := config.RuntimeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ledger.pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	return func() { os.Remove(path) }
}

// writeChroniclePID writes a chronicle PID file for testing. Returns cleanup func.
func writeChroniclePID(t *testing.T, pid int) func() {
	t.Helper()
	dir := config.RuntimeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "chronicle.pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	return func() { os.Remove(path) }
}

// writePrefectPID writes a PID file for testing. Returns cleanup func.
func writePrefectPID(t *testing.T, pid int) func() {
	t.Helper()
	dir := config.RuntimeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "prefect.pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	return func() { os.Remove(path) }
}

// clearPrefectPID removes the PID file to simulate prefect not running.
func clearPrefectPID(t *testing.T) {
	t.Helper()
	path := filepath.Join(config.RuntimeDir(), "prefect.pid")
	os.Remove(path)
}

// writeForgePID writes a forge PID file for the given world. Returns cleanup func.
func writeForgePID(t *testing.T, world string, pid int) func() {
	t.Helper()
	dir := filepath.Join(config.Home(), world, "forge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "forge.pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	return func() { os.Remove(path) }
}

// setupTestHome sets SOL_HOME to a temp dir.
func setupTestHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
}

func TestGatherHealthy(t *testing.T) {
	setupTestHome(t)

	// Write a PID file with our own PID (we know we're running).
	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	sphere := &mockSphereStore{
		agents: []store.Agent{
			{ID: "haven/Toast", Name: "Toast", World: "haven", State: "working", ActiveWrit: "sol-a1b2c3d4"},
			{ID: "haven/Sage", Name: "Sage", World: "haven", State: "working", ActiveWrit: "sol-11223344"},
		},
	}
	world := &mockWorldStore{
		items: map[string]*store.Writ{
			"sol-a1b2c3d4": {ID: "sol-a1b2c3d4", Title: "Implement login page"},
			"sol-11223344": {ID: "sol-11223344", Title: "Add unit tests"},
		},
	}
	checker := &mockChecker{
		alive: map[string]bool{
			"sol-haven-Toast": true,
			"sol-haven-Sage":  true,
		},
	}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.Health() != 0 {
		t.Errorf("Health() = %d, want 0", result.Health())
	}
	if result.Summary.Dead != 0 {
		t.Errorf("Summary.Dead = %d, want 0", result.Summary.Dead)
	}
	if result.Summary.Working != 2 {
		t.Errorf("Summary.Working = %d, want 2", result.Summary.Working)
	}
}

func TestGatherUnhealthy(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	sphere := &mockSphereStore{
		agents: []store.Agent{
			{ID: "haven/Toast", Name: "Toast", World: "haven", State: "working", ActiveWrit: "sol-a1b2c3d4"},
			{ID: "haven/Jasper", Name: "Jasper", World: "haven", State: "working", ActiveWrit: "sol-c5d6e7f8"},
		},
	}
	world := &mockWorldStore{
		items: map[string]*store.Writ{
			"sol-a1b2c3d4": {ID: "sol-a1b2c3d4", Title: "Implement login page"},
			"sol-c5d6e7f8": {ID: "sol-c5d6e7f8", Title: "Fix CSS regression"},
		},
	}
	checker := &mockChecker{
		alive: map[string]bool{
			"sol-haven-Toast":  true,
			"sol-haven-Jasper": false, // dead session
		},
	}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.Health() != 1 {
		t.Errorf("Health() = %d, want 1", result.Health())
	}
	if result.Summary.Dead != 1 {
		t.Errorf("Summary.Dead = %d, want 1", result.Summary.Dead)
	}
}

func TestGatherDegraded(t *testing.T) {
	setupTestHome(t)

	// No PID file — prefect not running.
	clearPrefectPID(t)

	sphere := &mockSphereStore{
		agents: []store.Agent{
			{ID: "haven/Toast", Name: "Toast", World: "haven", State: "working", ActiveWrit: "sol-a1b2c3d4"},
		},
	}
	world := &mockWorldStore{
		items: map[string]*store.Writ{
			"sol-a1b2c3d4": {ID: "sol-a1b2c3d4", Title: "Test task"},
		},
	}
	checker := &mockChecker{
		alive: map[string]bool{"sol-haven-Toast": true},
	}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.Health() != 2 {
		t.Errorf("Health() = %d, want 2", result.Health())
	}
	if result.Prefect.Running {
		t.Errorf("Prefect.Running = true, want false")
	}
}

func TestGatherNoAgents(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	sphere := &mockSphereStore{agents: nil}
	world := &mockWorldStore{items: nil}
	checker := &mockChecker{alive: nil}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.Summary.Total != 0 {
		t.Errorf("Summary.Total = %d, want 0", result.Summary.Total)
	}
	if result.Health() != 0 {
		t.Errorf("Health() = %d, want 0 (prefect running, no agents)", result.Health())
	}
}

func TestGatherNoAgentsDegraded(t *testing.T) {
	setupTestHome(t)

	// No prefect running.
	clearPrefectPID(t)

	sphere := &mockSphereStore{agents: nil}
	world := &mockWorldStore{items: nil}
	checker := &mockChecker{alive: nil}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.Summary.Total != 0 {
		t.Errorf("Summary.Total = %d, want 0", result.Summary.Total)
	}
	if result.Health() != 2 {
		t.Errorf("Health() = %d, want 2 (prefect not running)", result.Health())
	}
}

func TestGatherWithHookedWork(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	sphere := &mockSphereStore{
		agents: []store.Agent{
			{ID: "haven/Toast", Name: "Toast", World: "haven", State: "working", ActiveWrit: "sol-a1b2c3d4"},
		},
	}
	world := &mockWorldStore{
		items: map[string]*store.Writ{
			"sol-a1b2c3d4": {ID: "sol-a1b2c3d4", Title: "Implement login page"},
		},
	}
	checker := &mockChecker{
		alive: map[string]bool{"sol-haven-Toast": true},
	}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if len(result.Agents) != 1 {
		t.Fatalf("len(Agents) = %d, want 1", len(result.Agents))
	}
	as := result.Agents[0]
	if as.ActiveWrit != "sol-a1b2c3d4" {
		t.Errorf("ActiveWrit = %q, want %q", as.ActiveWrit, "sol-a1b2c3d4")
	}
	if as.WorkTitle != "Implement login page" {
		t.Errorf("WorkTitle = %q, want %q", as.WorkTitle, "Implement login page")
	}
}

func TestGatherMissingWrit(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	sphere := &mockSphereStore{
		agents: []store.Agent{
			{ID: "haven/Toast", Name: "Toast", World: "haven", State: "working", ActiveWrit: "sol-nonexist"},
		},
	}
	world := &mockWorldStore{items: map[string]*store.Writ{}} // item not found
	checker := &mockChecker{
		alive: map[string]bool{"sol-haven-Toast": true},
	}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v (should not fail on missing writ)", err)
	}

	if len(result.Agents) != 1 {
		t.Fatalf("len(Agents) = %d, want 1", len(result.Agents))
	}
	as := result.Agents[0]
	if as.WorkTitle != "(unknown)" {
		t.Errorf("WorkTitle = %q, want %q", as.WorkTitle, "(unknown)")
	}
}

func TestGatherMixedStates(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	sphere := &mockSphereStore{
		agents: []store.Agent{
			{ID: "haven/Toast", Name: "Toast", World: "haven", State: "working", ActiveWrit: "sol-a1b2c3d4"},
			{ID: "haven/Jasper", Name: "Jasper", World: "haven", State: "working", ActiveWrit: "sol-c5d6e7f8"},
			{ID: "haven/Sage", Name: "Sage", World: "haven", State: "idle"},
			{ID: "haven/Copper", Name: "Copper", World: "haven", State: "stalled", ActiveWrit: "sol-11223344"},
		},
	}
	world := &mockWorldStore{
		items: map[string]*store.Writ{
			"sol-a1b2c3d4": {ID: "sol-a1b2c3d4", Title: "Implement login page"},
			"sol-c5d6e7f8": {ID: "sol-c5d6e7f8", Title: "Fix CSS regression"},
			"sol-11223344": {ID: "sol-11223344", Title: "Add unit tests"},
		},
	}
	checker := &mockChecker{
		alive: map[string]bool{
			"sol-haven-Toast":  true,
			"sol-haven-Jasper": false, // dead session
			"sol-haven-Sage":   false, // idle — no session expected
			"sol-haven-Copper": false, // stalled, dead session
		},
	}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.Summary.Total != 4 {
		t.Errorf("Summary.Total = %d, want 4", result.Summary.Total)
	}
	if result.Summary.Working != 2 {
		t.Errorf("Summary.Working = %d, want 2", result.Summary.Working)
	}
	if result.Summary.Idle != 1 {
		t.Errorf("Summary.Idle = %d, want 1", result.Summary.Idle)
	}
	if result.Summary.Stalled != 1 {
		t.Errorf("Summary.Stalled = %d, want 1", result.Summary.Stalled)
	}
	if result.Summary.Dead != 1 {
		t.Errorf("Summary.Dead = %d, want 1 (only working agents with dead sessions)", result.Summary.Dead)
	}
}

func TestHealthExitCodes(t *testing.T) {
	tests := []struct {
		name       string
		prefect    PrefectInfo
		summary    Summary
		mergeQueue MergeQueueInfo
		want       int
	}{
		{
			name:    "prefect running, all healthy",
			prefect: PrefectInfo{Running: true, PID: 123},
			summary: Summary{Total: 3, Working: 2, Idle: 1},
			want:    0,
		},
		{
			name:    "prefect running, dead session",
			prefect: PrefectInfo{Running: true, PID: 123},
			summary: Summary{Total: 3, Working: 2, Idle: 1, Dead: 1},
			want:    1,
		},
		{
			name:    "prefect not running",
			prefect: PrefectInfo{Running: false},
			summary: Summary{Total: 3, Working: 2, Idle: 1},
			want:    2,
		},
		{
			name:    "prefect not running trumps dead sessions",
			prefect: PrefectInfo{Running: false},
			summary: Summary{Total: 3, Working: 2, Idle: 1, Dead: 1},
			want:    2,
		},
		{
			name:    "no agents, prefect running",
			prefect: PrefectInfo{Running: true, PID: 123},
			summary: Summary{},
			want:    0,
		},
		{
			name:       "failed merge requests make world unhealthy",
			prefect:    PrefectInfo{Running: true, PID: 123},
			summary:    Summary{Total: 3, Working: 2, Idle: 1},
			mergeQueue: MergeQueueInfo{Failed: 1, Total: 1},
			want:       1,
		},
		{
			name:       "prefect not running trumps failed merge requests",
			prefect:    PrefectInfo{Running: false},
			summary:    Summary{Total: 3, Working: 2, Idle: 1},
			mergeQueue: MergeQueueInfo{Failed: 2, Total: 2},
			want:       2,
		},
		{
			name:       "non-failed merge requests do not affect health",
			prefect:    PrefectInfo{Running: true, PID: 123},
			summary:    Summary{Total: 3, Working: 2, Idle: 1},
			mergeQueue: MergeQueueInfo{Ready: 3, Merged: 5, Total: 8},
			want:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := &WorldStatus{
				World:      "test",
				Prefect:    tt.prefect,
				Summary:    tt.summary,
				MergeQueue: tt.mergeQueue,
			}
			if got := rs.Health(); got != tt.want {
				t.Errorf("Health() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGatherWithForge(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	// Write forge PID file with our own PID (known running).
	forgePIDCleanup := writeForgePID(t, "haven", os.Getpid())
	defer forgePIDCleanup()

	sphere := &mockSphereStore{agents: nil}
	world := &mockWorldStore{items: nil}
	checker := &mockChecker{alive: nil}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if !result.Forge.Running {
		t.Error("Forge.Running = false, want true")
	}
	if result.Forge.PID != os.Getpid() {
		t.Errorf("Forge.PID = %d, want %d", result.Forge.PID, os.Getpid())
	}
}

func TestGatherWithoutForge(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	sphere := &mockSphereStore{agents: nil}
	world := &mockWorldStore{items: nil}
	checker := &mockChecker{alive: nil} // no forge PID file either

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.Forge.Running {
		t.Error("Forge.Running = true, want false")
	}
	if result.Forge.PID != 0 {
		t.Errorf("Forge.PID = %d, want 0", result.Forge.PID)
	}
}

func TestGatherMergeQueue(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	sphere := &mockSphereStore{agents: nil}
	world := &mockWorldStore{items: nil}
	checker := &mockChecker{alive: nil}

	mqStore := &mockMergeQueueStore{
		mrs: []store.MergeRequest{
			{ID: "mr-11111111", Phase: "ready"},
			{ID: "mr-22222222", Phase: "ready"},
			{ID: "mr-33333333", Phase: "claimed"},
		},
	}

	result, err := Gather("haven", sphere, world, mqStore, checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.MergeQueue.Total != 3 {
		t.Errorf("MergeQueue.Total = %d, want 3", result.MergeQueue.Total)
	}
	if result.MergeQueue.Ready != 2 {
		t.Errorf("MergeQueue.Ready = %d, want 2", result.MergeQueue.Ready)
	}
	if result.MergeQueue.Claimed != 1 {
		t.Errorf("MergeQueue.Claimed = %d, want 1", result.MergeQueue.Claimed)
	}
	if result.MergeQueue.Failed != 0 {
		t.Errorf("MergeQueue.Failed = %d, want 0", result.MergeQueue.Failed)
	}
	if result.MergeQueue.Merged != 0 {
		t.Errorf("MergeQueue.Merged = %d, want 0", result.MergeQueue.Merged)
	}
}

func TestGatherMergeQueueEmpty(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	sphere := &mockSphereStore{agents: nil}
	world := &mockWorldStore{items: nil}
	checker := &mockChecker{alive: nil}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.MergeQueue.Total != 0 {
		t.Errorf("MergeQueue.Total = %d, want 0", result.MergeQueue.Total)
	}
	if result.MergeQueue.Ready != 0 {
		t.Errorf("MergeQueue.Ready = %d, want 0", result.MergeQueue.Ready)
	}
	if result.MergeQueue.Claimed != 0 {
		t.Errorf("MergeQueue.Claimed = %d, want 0", result.MergeQueue.Claimed)
	}
	if result.MergeQueue.Failed != 0 {
		t.Errorf("MergeQueue.Failed = %d, want 0", result.MergeQueue.Failed)
	}
	if result.MergeQueue.Merged != 0 {
		t.Errorf("MergeQueue.Merged = %d, want 0", result.MergeQueue.Merged)
	}
}

func TestGatherWithGovernor(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	sphere := &mockSphereStore{
		agents: []store.Agent{
			{ID: "haven/governor", Name: "governor", World: "haven", Role: "governor", State: "idle"},
		},
	}
	world := &mockWorldStore{items: nil}
	checker := &mockChecker{
		alive: map[string]bool{
			"sol-haven-governor": true,
		},
	}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if !result.Governor.Running {
		t.Error("Governor.Running = false, want true")
	}
	if !result.Governor.SessionAlive {
		t.Error("Governor.SessionAlive = false, want true")
	}
	// Governor should not count as an outpost agent.
	if result.Summary.Total != 0 {
		t.Errorf("Summary.Total = %d, want 0 (governor not counted)", result.Summary.Total)
	}
	if len(result.Agents) != 0 {
		t.Errorf("len(Agents) = %d, want 0", len(result.Agents))
	}
}

func TestGatherGovernorRunningReflectsSession(t *testing.T) {
	// When governor is registered in the DB but its session is dead,
	// Running should be false (reflects session state, not registration).
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	sphere := &mockSphereStore{
		agents: []store.Agent{
			{ID: "haven/governor", Name: "governor", World: "haven", Role: "governor", State: "idle"},
		},
	}
	world := &mockWorldStore{items: nil}
	checker := &mockChecker{
		alive: map[string]bool{
			"sol-haven-governor": false, // session dead
		},
	}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.Governor.Running {
		t.Error("Governor.Running = true, want false (session dead)")
	}
	if result.Governor.SessionAlive {
		t.Error("Governor.SessionAlive = true, want false")
	}
}

func TestGatherWithEnvoys(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	sphere := &mockSphereStore{
		agents: []store.Agent{
			{ID: "haven/Scout", Name: "Scout", World: "haven", Role: "envoy", State: "working", ActiveWrit: "sol-a1b2c3d4"},
			{ID: "haven/Toast", Name: "Toast", World: "haven", Role: "outpost", State: "working", ActiveWrit: "sol-11223344"},
		},
	}
	world := &mockWorldStore{
		items: map[string]*store.Writ{
			"sol-a1b2c3d4": {ID: "sol-a1b2c3d4", Title: "Design review"},
			"sol-11223344": {ID: "sol-11223344", Title: "Implement auth"},
		},
	}
	checker := &mockChecker{
		alive: map[string]bool{
			"sol-haven-Scout": true,
			"sol-haven-Toast": true,
		},
	}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	// Envoy separated from outpost agents.
	if len(result.Envoys) != 1 {
		t.Fatalf("len(Envoys) = %d, want 1", len(result.Envoys))
	}
	if result.Envoys[0].Name != "Scout" {
		t.Errorf("Envoys[0].Name = %q, want %q", result.Envoys[0].Name, "Scout")
	}
	if result.Envoys[0].WorkTitle != "Design review" {
		t.Errorf("Envoys[0].WorkTitle = %q, want %q", result.Envoys[0].WorkTitle, "Design review")
	}

	// Only outpost agent counted in Agents.
	if len(result.Agents) != 1 {
		t.Fatalf("len(Agents) = %d, want 1", len(result.Agents))
	}
	if result.Agents[0].Name != "Toast" {
		t.Errorf("Agents[0].Name = %q, want %q", result.Agents[0].Name, "Toast")
	}

	// Summary only counts outpost agents.
	if result.Summary.Total != 1 {
		t.Errorf("Summary.Total = %d, want 1", result.Summary.Total)
	}
}

func TestGatherMixedRoles(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	sphere := &mockSphereStore{
		agents: []store.Agent{
			{ID: "haven/Toast", Name: "Toast", World: "haven", Role: "outpost", State: "working", ActiveWrit: "sol-a1b2c3d4"},
			{ID: "haven/Crisp", Name: "Crisp", World: "haven", Role: "outpost", State: "idle"},
			{ID: "haven/Scout", Name: "Scout", World: "haven", Role: "envoy", State: "working", ActiveWrit: "sol-11223344"},
			{ID: "haven/governor", Name: "governor", World: "haven", Role: "governor", State: "idle"},
			{ID: "haven/forge", Name: "forge", World: "haven", Role: "forge", State: "idle"},
		},
	}
	world := &mockWorldStore{
		items: map[string]*store.Writ{
			"sol-a1b2c3d4": {ID: "sol-a1b2c3d4", Title: "Implement login"},
			"sol-11223344": {ID: "sol-11223344", Title: "Design review"},
		},
	}
	checker := &mockChecker{
		alive: map[string]bool{
			"sol-haven-Toast":    true,
			"sol-haven-Scout":    true,
			"sol-haven-governor": true,
			"sol-haven-forge":    true,
		},
	}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if len(result.Agents) != 2 {
		t.Errorf("len(Agents) = %d, want 2 (outpost agents only)", len(result.Agents))
	}
	if len(result.Envoys) != 1 {
		t.Errorf("len(Envoys) = %d, want 1", len(result.Envoys))
	}
	if !result.Governor.Running {
		t.Error("Governor.Running = false, want true")
	}
	// Summary counts only outpost agents.
	if result.Summary.Total != 2 {
		t.Errorf("Summary.Total = %d, want 2", result.Summary.Total)
	}
}

func TestGatherEnvoyBriefAge(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	// Create a brief file for the envoy.
	briefPath := envoy.BriefPath("haven", "Scout")
	if err := os.MkdirAll(filepath.Dir(briefPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(briefPath, []byte("# Scout's brief"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Set mtime to 2 hours ago.
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(briefPath, twoHoursAgo, twoHoursAgo); err != nil {
		t.Fatal(err)
	}

	sphere := &mockSphereStore{
		agents: []store.Agent{
			{ID: "haven/Scout", Name: "Scout", World: "haven", Role: "envoy", State: "idle"},
		},
	}
	world := &mockWorldStore{items: nil}
	checker := &mockChecker{alive: map[string]bool{}}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if len(result.Envoys) != 1 {
		t.Fatalf("len(Envoys) = %d, want 1", len(result.Envoys))
	}
	if result.Envoys[0].BriefAge != "2h" {
		t.Errorf("Envoys[0].BriefAge = %q, want %q", result.Envoys[0].BriefAge, "2h")
	}
}

func TestHealthIgnoresEnvoyGovernor(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	sphere := &mockSphereStore{
		agents: []store.Agent{
			{ID: "haven/Toast", Name: "Toast", World: "haven", Role: "outpost", State: "working", ActiveWrit: "sol-a1b2c3d4"},
			{ID: "haven/Scout", Name: "Scout", World: "haven", Role: "envoy", State: "working", ActiveWrit: "sol-11223344"},
			{ID: "haven/governor", Name: "governor", World: "haven", Role: "governor", State: "idle"},
		},
	}
	world := &mockWorldStore{
		items: map[string]*store.Writ{
			"sol-a1b2c3d4": {ID: "sol-a1b2c3d4", Title: "Task 1"},
			"sol-11223344": {ID: "sol-11223344", Title: "Task 2"},
		},
	}
	checker := &mockChecker{
		alive: map[string]bool{
			"sol-haven-Toast":    true,
			"sol-haven-Scout":    false, // envoy dead — should NOT affect health
			"sol-haven-governor": false, // governor dead — should NOT affect health
		},
	}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	// Health should be 0 (healthy) — envoy/governor dead sessions ignored.
	if result.Health() != 0 {
		t.Errorf("Health() = %d, want 0 (envoy/governor dead should not affect health)", result.Health())
	}
	if result.Summary.Dead != 0 {
		t.Errorf("Summary.Dead = %d, want 0 (only outpost agents counted)", result.Summary.Dead)
	}
}

func TestGatherChroniclePIDFallback(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	// Write chronicle PID file with our own PID (known-alive).
	chronicleCleanup := writeChroniclePID(t, os.Getpid())
	defer chronicleCleanup()

	sphere := &mockSphereStore{agents: nil}
	world := &mockWorldStore{items: nil}
	// No tmux session for chronicle — PID fallback should activate.
	checker := &mockChecker{alive: map[string]bool{}}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if !result.Chronicle.Running {
		t.Error("Chronicle.Running = false, want true (PID fallback)")
	}
	if result.Chronicle.PID != os.Getpid() {
		t.Errorf("Chronicle.PID = %d, want %d", result.Chronicle.PID, os.Getpid())
	}
}

func TestGatherChronicleIgnoresSession(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	// PID file exists — chronicle detection is PID-based only now.
	chronicleCleanup := writeChroniclePID(t, os.Getpid())
	defer chronicleCleanup()

	sphere := &mockSphereStore{agents: nil}
	world := &mockWorldStore{items: nil}
	// Even if a session checker reports sol-chronicle alive, we use PID.
	checker := &mockChecker{alive: map[string]bool{
		"sol-chronicle": true,
	}}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if !result.Chronicle.Running {
		t.Error("Chronicle.Running = false, want true")
	}
	// PID should be set (not session-based).
	if result.Chronicle.PID != os.Getpid() {
		t.Errorf("Chronicle.PID = %d, want %d", result.Chronicle.PID, os.Getpid())
	}
}

func TestGatherLedgerPID(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	// Write ledger PID file with our own PID (known-alive).
	ledgerCleanup := writeLedgerPID(t, os.Getpid())
	defer ledgerCleanup()

	sphere := &mockSphereStore{agents: nil}
	world := &mockWorldStore{items: nil}
	checker := &mockChecker{alive: map[string]bool{}}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if !result.Ledger.Running {
		t.Error("Ledger.Running = false, want true")
	}
	if result.Ledger.PID != os.Getpid() {
		t.Errorf("Ledger.PID = %d, want %d", result.Ledger.PID, os.Getpid())
	}
}

func TestGatherLedgerWithHeartbeat(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	// Write ledger PID file with our own PID (known-alive).
	ledgerCleanup := writeLedgerPID(t, os.Getpid())
	defer ledgerCleanup()

	// Write a fresh heartbeat.
	hb := ledger.Heartbeat{
		Timestamp:       time.Now().UTC(),
		Status:          "running",
		RequestsTotal:   42,
		TokensProcessed: 10000,
		WorldsWritten:   2,
	}
	if err := ledger.WriteHeartbeat(hb); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ledger.RemoveHeartbeat)

	sphere := &mockSphereStore{agents: nil}
	world := &mockWorldStore{items: nil}
	checker := &mockChecker{alive: map[string]bool{}}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if !result.Ledger.Running {
		t.Error("Ledger.Running = false, want true")
	}
	if result.Ledger.PID != os.Getpid() {
		t.Errorf("Ledger.PID = %d, want %d", result.Ledger.PID, os.Getpid())
	}
	if result.Ledger.HeartbeatAge == "" {
		t.Error("Ledger.HeartbeatAge = empty, want non-empty")
	}
	if result.Ledger.Stale {
		t.Error("Ledger.Stale = true, want false (fresh heartbeat)")
	}
}

func TestGatherLedgerNeitherRunning(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	// No session, no PID file.
	sphere := &mockSphereStore{agents: nil}
	world := &mockWorldStore{items: nil}
	checker := &mockChecker{alive: map[string]bool{}}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.Ledger.Running {
		t.Error("Ledger.Running = true, want false (nothing running)")
	}
}

func TestGatherChronicleNeitherRunning(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	// No session, no PID file.
	sphere := &mockSphereStore{agents: nil}
	world := &mockWorldStore{items: nil}
	checker := &mockChecker{alive: map[string]bool{}}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.Chronicle.Running {
		t.Error("Chronicle.Running = true, want false (nothing running)")
	}
}
