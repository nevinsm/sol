package status

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/store"
)

// --- Mock implementations ---

type mockChecker struct {
	alive map[string]bool
}

func (m *mockChecker) Exists(name string) bool { return m.alive[name] }

type mockTownStore struct {
	agents []store.Agent
	err    error
}

func (m *mockTownStore) ListAgents(rig, state string) ([]store.Agent, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []store.Agent
	for _, a := range m.agents {
		if rig != "" && a.Rig != rig {
			continue
		}
		if state != "" && a.State != state {
			continue
		}
		result = append(result, a)
	}
	return result, nil
}

type mockRigStore struct {
	items map[string]*store.WorkItem
}

func (m *mockRigStore) GetWorkItem(id string) (*store.WorkItem, error) {
	item, ok := m.items[id]
	if !ok {
		return nil, fmt.Errorf("work item %q not found", id)
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

// writeSupervisorPID writes a PID file for testing. Returns cleanup func.
func writeSupervisorPID(t *testing.T, pid int) func() {
	t.Helper()
	dir := config.RuntimeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "supervisor.pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	return func() { os.Remove(path) }
}

// clearSupervisorPID removes the PID file to simulate supervisor not running.
func clearSupervisorPID(t *testing.T) {
	t.Helper()
	path := filepath.Join(config.RuntimeDir(), "supervisor.pid")
	os.Remove(path)
}

// setupTestHome sets GT_HOME to a temp dir and returns cleanup func.
func setupTestHome(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	old := os.Getenv("GT_HOME")
	os.Setenv("GT_HOME", dir)
	return func() {
		if old == "" {
			os.Unsetenv("GT_HOME")
		} else {
			os.Setenv("GT_HOME", old)
		}
	}
}

func TestGatherHealthy(t *testing.T) {
	cleanup := setupTestHome(t)
	defer cleanup()

	// Write a PID file with our own PID (we know we're running).
	pidCleanup := writeSupervisorPID(t, os.Getpid())
	defer pidCleanup()

	town := &mockTownStore{
		agents: []store.Agent{
			{ID: "myrig/Toast", Name: "Toast", Rig: "myrig", State: "working", HookItem: "gt-a1b2c3d4"},
			{ID: "myrig/Sage", Name: "Sage", Rig: "myrig", State: "working", HookItem: "gt-11223344"},
		},
	}
	rig := &mockRigStore{
		items: map[string]*store.WorkItem{
			"gt-a1b2c3d4": {ID: "gt-a1b2c3d4", Title: "Implement login page"},
			"gt-11223344": {ID: "gt-11223344", Title: "Add unit tests"},
		},
	}
	checker := &mockChecker{
		alive: map[string]bool{
			"gt-myrig-Toast": true,
			"gt-myrig-Sage":  true,
		},
	}

	result, err := Gather("myrig", town, rig, emptyMQStore(), checker)
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
	cleanup := setupTestHome(t)
	defer cleanup()

	pidCleanup := writeSupervisorPID(t, os.Getpid())
	defer pidCleanup()

	town := &mockTownStore{
		agents: []store.Agent{
			{ID: "myrig/Toast", Name: "Toast", Rig: "myrig", State: "working", HookItem: "gt-a1b2c3d4"},
			{ID: "myrig/Jasper", Name: "Jasper", Rig: "myrig", State: "working", HookItem: "gt-c5d6e7f8"},
		},
	}
	rig := &mockRigStore{
		items: map[string]*store.WorkItem{
			"gt-a1b2c3d4": {ID: "gt-a1b2c3d4", Title: "Implement login page"},
			"gt-c5d6e7f8": {ID: "gt-c5d6e7f8", Title: "Fix CSS regression"},
		},
	}
	checker := &mockChecker{
		alive: map[string]bool{
			"gt-myrig-Toast":  true,
			"gt-myrig-Jasper": false, // dead session
		},
	}

	result, err := Gather("myrig", town, rig, emptyMQStore(), checker)
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
	cleanup := setupTestHome(t)
	defer cleanup()

	// No PID file — supervisor not running.
	clearSupervisorPID(t)

	town := &mockTownStore{
		agents: []store.Agent{
			{ID: "myrig/Toast", Name: "Toast", Rig: "myrig", State: "working", HookItem: "gt-a1b2c3d4"},
		},
	}
	rig := &mockRigStore{
		items: map[string]*store.WorkItem{
			"gt-a1b2c3d4": {ID: "gt-a1b2c3d4", Title: "Test task"},
		},
	}
	checker := &mockChecker{
		alive: map[string]bool{"gt-myrig-Toast": true},
	}

	result, err := Gather("myrig", town, rig, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.Health() != 2 {
		t.Errorf("Health() = %d, want 2", result.Health())
	}
	if result.Supervisor.Running {
		t.Errorf("Supervisor.Running = true, want false")
	}
}

func TestGatherNoAgents(t *testing.T) {
	cleanup := setupTestHome(t)
	defer cleanup()

	pidCleanup := writeSupervisorPID(t, os.Getpid())
	defer pidCleanup()

	town := &mockTownStore{agents: nil}
	rig := &mockRigStore{items: nil}
	checker := &mockChecker{alive: nil}

	result, err := Gather("myrig", town, rig, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.Summary.Total != 0 {
		t.Errorf("Summary.Total = %d, want 0", result.Summary.Total)
	}
	if result.Health() != 0 {
		t.Errorf("Health() = %d, want 0 (supervisor running, no agents)", result.Health())
	}
}

func TestGatherNoAgentsDegraded(t *testing.T) {
	cleanup := setupTestHome(t)
	defer cleanup()

	// No supervisor running.
	clearSupervisorPID(t)

	town := &mockTownStore{agents: nil}
	rig := &mockRigStore{items: nil}
	checker := &mockChecker{alive: nil}

	result, err := Gather("myrig", town, rig, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.Summary.Total != 0 {
		t.Errorf("Summary.Total = %d, want 0", result.Summary.Total)
	}
	if result.Health() != 2 {
		t.Errorf("Health() = %d, want 2 (supervisor not running)", result.Health())
	}
}

func TestGatherWithHookedWork(t *testing.T) {
	cleanup := setupTestHome(t)
	defer cleanup()

	pidCleanup := writeSupervisorPID(t, os.Getpid())
	defer pidCleanup()

	town := &mockTownStore{
		agents: []store.Agent{
			{ID: "myrig/Toast", Name: "Toast", Rig: "myrig", State: "working", HookItem: "gt-a1b2c3d4"},
		},
	}
	rig := &mockRigStore{
		items: map[string]*store.WorkItem{
			"gt-a1b2c3d4": {ID: "gt-a1b2c3d4", Title: "Implement login page"},
		},
	}
	checker := &mockChecker{
		alive: map[string]bool{"gt-myrig-Toast": true},
	}

	result, err := Gather("myrig", town, rig, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if len(result.Agents) != 1 {
		t.Fatalf("len(Agents) = %d, want 1", len(result.Agents))
	}
	as := result.Agents[0]
	if as.HookItem != "gt-a1b2c3d4" {
		t.Errorf("HookItem = %q, want %q", as.HookItem, "gt-a1b2c3d4")
	}
	if as.WorkTitle != "Implement login page" {
		t.Errorf("WorkTitle = %q, want %q", as.WorkTitle, "Implement login page")
	}
}

func TestGatherMissingWorkItem(t *testing.T) {
	cleanup := setupTestHome(t)
	defer cleanup()

	pidCleanup := writeSupervisorPID(t, os.Getpid())
	defer pidCleanup()

	town := &mockTownStore{
		agents: []store.Agent{
			{ID: "myrig/Toast", Name: "Toast", Rig: "myrig", State: "working", HookItem: "gt-nonexist"},
		},
	}
	rig := &mockRigStore{items: map[string]*store.WorkItem{}} // item not found
	checker := &mockChecker{
		alive: map[string]bool{"gt-myrig-Toast": true},
	}

	result, err := Gather("myrig", town, rig, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v (should not fail on missing work item)", err)
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
	cleanup := setupTestHome(t)
	defer cleanup()

	pidCleanup := writeSupervisorPID(t, os.Getpid())
	defer pidCleanup()

	town := &mockTownStore{
		agents: []store.Agent{
			{ID: "myrig/Toast", Name: "Toast", Rig: "myrig", State: "working", HookItem: "gt-a1b2c3d4"},
			{ID: "myrig/Jasper", Name: "Jasper", Rig: "myrig", State: "working", HookItem: "gt-c5d6e7f8"},
			{ID: "myrig/Sage", Name: "Sage", Rig: "myrig", State: "idle"},
			{ID: "myrig/Copper", Name: "Copper", Rig: "myrig", State: "stalled", HookItem: "gt-11223344"},
		},
	}
	rig := &mockRigStore{
		items: map[string]*store.WorkItem{
			"gt-a1b2c3d4": {ID: "gt-a1b2c3d4", Title: "Implement login page"},
			"gt-c5d6e7f8": {ID: "gt-c5d6e7f8", Title: "Fix CSS regression"},
			"gt-11223344": {ID: "gt-11223344", Title: "Add unit tests"},
		},
	}
	checker := &mockChecker{
		alive: map[string]bool{
			"gt-myrig-Toast":  true,
			"gt-myrig-Jasper": false, // dead session
			"gt-myrig-Sage":   false, // idle — no session expected
			"gt-myrig-Copper": false, // stalled, dead session
		},
	}

	result, err := Gather("myrig", town, rig, emptyMQStore(), checker)
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
		supervisor SupervisorInfo
		summary    Summary
		want       int
	}{
		{
			name:       "supervisor running, all healthy",
			supervisor: SupervisorInfo{Running: true, PID: 123},
			summary:    Summary{Total: 3, Working: 2, Idle: 1},
			want:       0,
		},
		{
			name:       "supervisor running, dead session",
			supervisor: SupervisorInfo{Running: true, PID: 123},
			summary:    Summary{Total: 3, Working: 2, Idle: 1, Dead: 1},
			want:       1,
		},
		{
			name:       "supervisor not running",
			supervisor: SupervisorInfo{Running: false},
			summary:    Summary{Total: 3, Working: 2, Idle: 1},
			want:       2,
		},
		{
			name:       "supervisor not running trumps dead sessions",
			supervisor: SupervisorInfo{Running: false},
			summary:    Summary{Total: 3, Working: 2, Idle: 1, Dead: 1},
			want:       2,
		},
		{
			name:       "no agents, supervisor running",
			supervisor: SupervisorInfo{Running: true, PID: 123},
			summary:    Summary{},
			want:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := &RigStatus{
				Rig:        "test",
				Supervisor: tt.supervisor,
				Summary:    tt.summary,
			}
			if got := rs.Health(); got != tt.want {
				t.Errorf("Health() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGatherWithRefinery(t *testing.T) {
	cleanup := setupTestHome(t)
	defer cleanup()

	pidCleanup := writeSupervisorPID(t, os.Getpid())
	defer pidCleanup()

	town := &mockTownStore{agents: nil}
	rig := &mockRigStore{items: nil}
	checker := &mockChecker{
		alive: map[string]bool{
			"gt-myrig-refinery": true, // refinery session alive
		},
	}

	result, err := Gather("myrig", town, rig, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if !result.Refinery.Running {
		t.Error("Refinery.Running = false, want true")
	}
	if result.Refinery.SessionName != "gt-myrig-refinery" {
		t.Errorf("Refinery.SessionName = %q, want %q", result.Refinery.SessionName, "gt-myrig-refinery")
	}
}

func TestGatherWithoutRefinery(t *testing.T) {
	cleanup := setupTestHome(t)
	defer cleanup()

	pidCleanup := writeSupervisorPID(t, os.Getpid())
	defer pidCleanup()

	town := &mockTownStore{agents: nil}
	rig := &mockRigStore{items: nil}
	checker := &mockChecker{alive: nil} // refinery session not alive

	result, err := Gather("myrig", town, rig, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.Refinery.Running {
		t.Error("Refinery.Running = true, want false")
	}
	if result.Refinery.SessionName != "" {
		t.Errorf("Refinery.SessionName = %q, want empty", result.Refinery.SessionName)
	}
}

func TestGatherMergeQueue(t *testing.T) {
	cleanup := setupTestHome(t)
	defer cleanup()

	pidCleanup := writeSupervisorPID(t, os.Getpid())
	defer pidCleanup()

	town := &mockTownStore{agents: nil}
	rig := &mockRigStore{items: nil}
	checker := &mockChecker{alive: nil}

	mqStore := &mockMergeQueueStore{
		mrs: []store.MergeRequest{
			{ID: "mr-11111111", Phase: "ready"},
			{ID: "mr-22222222", Phase: "ready"},
			{ID: "mr-33333333", Phase: "claimed"},
		},
	}

	result, err := Gather("myrig", town, rig, mqStore, checker)
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
	cleanup := setupTestHome(t)
	defer cleanup()

	pidCleanup := writeSupervisorPID(t, os.Getpid())
	defer pidCleanup()

	town := &mockTownStore{agents: nil}
	rig := &mockRigStore{items: nil}
	checker := &mockChecker{alive: nil}

	result, err := Gather("myrig", town, rig, emptyMQStore(), checker)
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
