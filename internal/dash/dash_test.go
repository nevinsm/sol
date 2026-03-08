package dash

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/status"
)

func TestSphereViewRendersProcesses(t *testing.T) {
	sm := newSphereModel()
	sm.width = 120
	sm.height = 40

	data := &status.SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: status.PrefectInfo{Running: true, PID: 1234},
		Consul:  status.ConsulInfo{Running: true},
	}
	sm.updateData(data)

	output := sm.view(data, time.Now())

	checks := []string{
		"Sol Sphere",
		"healthy",
		"Processes",
		"Prefect",
		"Consul",
		"Chronicle",
		"Ledger",
		"Broker",
		"Senate",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("sphere view missing %q", check)
		}
	}
}

func TestSphereViewRendersWorlds(t *testing.T) {
	sm := newSphereModel()
	sm.width = 120
	sm.height = 40

	data := &status.SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: status.PrefectInfo{Running: true, PID: 1234},
		Worlds: []status.WorldSummary{
			{Name: "alpha", Agents: 3, Working: 2, Forge: true, Sentinel: true, Health: "healthy"},
			{Name: "beta", Agents: 1, Working: 0, Health: "healthy"},
		},
	}
	sm.updateData(data)

	output := sm.view(data, time.Now())

	checks := []string{
		"Worlds",
		"alpha",
		"beta",
		"WORLD",
		"HEALTH",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("sphere view missing %q", check)
		}
	}
}

func TestSphereViewNoWorlds(t *testing.T) {
	sm := newSphereModel()
	sm.width = 120
	sm.height = 40

	data := &status.SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "degraded",
	}
	sm.updateData(data)

	output := sm.view(data, time.Now())

	if !strings.Contains(output, "No worlds initialized.") {
		t.Error("sphere view should show 'No worlds initialized.' when empty")
	}
}

func TestSphereViewCaravans(t *testing.T) {
	sm := newSphereModel()
	sm.width = 120
	sm.height = 40

	data := &status.SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: status.PrefectInfo{Running: true, PID: 100},
		Caravans: []status.CaravanInfo{
			{ID: "sol-abc123", Name: "deploy-batch", TotalItems: 5, ClosedItems: 2},
		},
	}
	sm.updateData(data)

	output := sm.view(data, time.Now())

	checks := []string{
		"Caravans",
		"deploy-batch",
		"2/5 merged",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("sphere view with caravans missing %q", check)
		}
	}
}

func TestSphereViewFooter(t *testing.T) {
	sm := newSphereModel()
	sm.width = 120
	sm.height = 40

	data := &status.SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
	}
	sm.updateData(data)

	output := sm.view(data, time.Now())

	checks := []string{
		"q quit",
		"select",
		"r refresh",
		"refreshed",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("sphere view footer missing %q", check)
		}
	}
}

func TestSphereViewNilData(t *testing.T) {
	sm := newSphereModel()
	output := sm.view(nil, time.Time{})
	if !strings.Contains(output, "Gathering sphere status...") {
		t.Error("sphere view with nil data should show loading message")
	}
}

func TestWorldViewRendersProcesses(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:   "testworld",
		Prefect: status.PrefectInfo{Running: true, PID: 42},
		Forge:   status.ForgeInfo{Running: true, SessionName: "sol-testworld-forge"},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now())

	checks := []string{
		"World: testworld",
		"Processes",
		"Forge",
		"Sentinel",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("world view missing %q", check)
		}
	}
}

func TestWorldViewRendersAgents(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:   "testworld",
		Prefect: status.PrefectInfo{Running: true, PID: 42},
		Agents: []status.AgentStatus{
			{Name: "Toast", State: "working", SessionAlive: true, TetherItem: "sol-aaa", WorkTitle: "fix bug"},
			{Name: "Crisp", State: "idle"},
		},
		Summary: status.Summary{Total: 2, Working: 1, Idle: 1},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now())

	checks := []string{
		"Outposts (2)",
		"Toast",
		"Crisp",
		"NAME",
		"STATE",
		"SESSION",
		"WORK",
		"fix bug",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("world view missing %q", check)
		}
	}
}

func TestWorldViewRendersEnvoys(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:   "testworld",
		Prefect: status.PrefectInfo{Running: true, PID: 42},
		Envoys: []status.EnvoyStatus{
			{Name: "Scout", State: "working", SessionAlive: true, TetherItem: "sol-bbb", WorkTitle: "Design review", BriefAge: "45m"},
		},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now())

	checks := []string{
		"Envoys (1)",
		"Scout",
		"BRIEF",
		"45m ago",
		"Design review",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("world view with envoys missing %q", check)
		}
	}
}

func TestWorldViewNoAgents(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:   "emptyworld",
		Prefect: status.PrefectInfo{Running: true, PID: 1},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now())

	if !strings.Contains(output, "No agents registered.") {
		t.Error("world view should show 'No agents registered.' when empty")
	}
}

func TestWorldViewMergeQueue(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:      "testworld",
		Prefect:    status.PrefectInfo{Running: true, PID: 42},
		MergeQueue: status.MergeQueueInfo{Total: 3, Ready: 1, Claimed: 1, Failed: 1},
		Summary:    status.Summary{},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now())

	checks := []string{
		"Merge Queue",
		"1 ready",
		"1 in progress",
		"1 failed",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("world view merge queue missing %q", check)
		}
	}
}

func TestWorldViewMergeQueueEmpty(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:      "testworld",
		Prefect:    status.PrefectInfo{Running: true, PID: 42},
		MergeQueue: status.MergeQueueInfo{Total: 0},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now())

	if !strings.Contains(output, "empty") {
		t.Error("world view should show 'empty' for empty merge queue")
	}
}

func TestWorldViewNilData(t *testing.T) {
	wm := newWorldModel()
	output := wm.view(nil, time.Time{})
	if !strings.Contains(output, "Gathering world status...") {
		t.Error("world view with nil data should show loading message")
	}
}

func TestWorldViewCaravans(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:   "testworld",
		Prefect: status.PrefectInfo{Running: true, PID: 42},
		Caravans: []status.CaravanInfo{
			{ID: "sol-abc123", Name: "batch-deploy", TotalItems: 8, ClosedItems: 3},
		},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now())

	checks := []string{
		"Caravans",
		"batch-deploy",
		"3/8 merged",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("world view caravan missing %q", check)
		}
	}
}

func TestModelViewSphere(t *testing.T) {
	m := NewModel(Config{})
	m.ready = true
	m.width = 120
	m.height = 40
	m.sphereView.width = 120
	m.sphereView.height = 40
	m.sphereData = &status.SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: status.PrefectInfo{Running: true, PID: 100},
	}
	m.sphereView.updateData(m.sphereData)
	m.lastRefresh = time.Now()

	output := m.View()
	if !strings.Contains(output, "Sol Sphere") {
		t.Error("Model.View in sphere mode should render sphere view")
	}
}

func TestModelViewWorld(t *testing.T) {
	m := NewModel(Config{World: "myworld"})
	m.ready = true
	m.width = 120
	m.height = 40
	m.worldView.width = 120
	m.worldView.height = 40
	m.worldData = &status.WorldStatus{
		World:   "myworld",
		Prefect: status.PrefectInfo{Running: true, PID: 100},
	}
	m.worldView.updateData(m.worldData)
	m.lastRefresh = time.Now()

	output := m.View()
	if !strings.Contains(output, "World: myworld") {
		t.Error("Model.View in world mode should render world view")
	}
}

func TestModelViewNotReady(t *testing.T) {
	m := NewModel(Config{})
	output := m.View()
	if output != "Loading..." {
		t.Errorf("Model.View when not ready should return 'Loading...', got %q", output)
	}
}

func TestCaravanPhaseSummary(t *testing.T) {
	c := status.CaravanInfo{
		ID:         "test",
		Name:       "test-caravan",
		TotalItems: 5,
		Phases: []status.PhaseProgress{
			{Phase: 0, Total: 3, Closed: 2},
			{Phase: 1, Total: 2, Closed: 0},
		},
	}

	result := caravanPhaseSummary(c)
	if !strings.Contains(result, "p0: 2/3") {
		t.Errorf("caravanPhaseSummary missing p0, got %q", result)
	}
	if !strings.Contains(result, "p1: 0/2") {
		t.Errorf("caravanPhaseSummary missing p1, got %q", result)
	}
}

func TestCaravanPhaseSummaryNoPhases(t *testing.T) {
	c := status.CaravanInfo{
		ID:         "test",
		Name:       "simple",
		TotalItems: 3,
	}

	result := caravanPhaseSummary(c)
	if result != "" {
		t.Errorf("caravanPhaseSummary with no phases should return empty, got %q", result)
	}
}

func TestStylesHealthBadge(t *testing.T) {
	tests := []struct {
		input    string
		contains string
	}{
		{"healthy", "healthy"},
		{"unhealthy", "unhealthy"},
		{"degraded", "degraded"},
		{"sleeping", "sleeping"},
		{"something-else", "unknown"},
	}

	for _, tt := range tests {
		result := healthBadge(tt.input)
		if !strings.Contains(result, tt.contains) {
			t.Errorf("healthBadge(%q) = %q, want it to contain %q", tt.input, result, tt.contains)
		}
	}
}

func TestStylesStatusIndicator(t *testing.T) {
	running := statusIndicator(true)
	if !strings.Contains(running, checkMark) {
		t.Errorf("statusIndicator(true) should contain %q, got %q", checkMark, running)
	}

	stopped := statusIndicator(false)
	if !strings.Contains(stopped, crossMark) {
		t.Errorf("statusIndicator(false) should contain %q, got %q", crossMark, stopped)
	}
}

func TestSphereViewCursorBounds(t *testing.T) {
	sm := newSphereModel()
	sm.worldRows = 3

	data := &status.SphereStatus{}

	// Move down.
	sm, _ = sm.update(keyMsg("down"), data)
	if sm.cursor != 1 {
		t.Errorf("cursor should be 1, got %d", sm.cursor)
	}

	// Move past end.
	sm, _ = sm.update(keyMsg("down"), data)
	sm, _ = sm.update(keyMsg("down"), data) // at max now
	if sm.cursor != 2 {
		t.Errorf("cursor should be 2, got %d", sm.cursor)
	}

	// Try to go past.
	sm, _ = sm.update(keyMsg("down"), data)
	if sm.cursor != 2 {
		t.Errorf("cursor should remain 2, got %d", sm.cursor)
	}

	// Move up.
	sm, _ = sm.update(keyMsg("up"), data)
	if sm.cursor != 1 {
		t.Errorf("cursor should be 1, got %d", sm.cursor)
	}

	// Move to top and try to go past.
	sm, _ = sm.update(keyMsg("up"), data)
	sm, _ = sm.update(keyMsg("up"), data)
	if sm.cursor != 0 {
		t.Errorf("cursor should remain 0, got %d", sm.cursor)
	}
}

func TestWorldViewCursorBounds(t *testing.T) {
	wm := newWorldModel()
	wm.agentLen = 2

	// Move down.
	wm, _ = wm.update(keyMsg("j"), nil)
	if wm.cursor != 1 {
		t.Errorf("cursor should be 1, got %d", wm.cursor)
	}

	// Can't go past.
	wm, _ = wm.update(keyMsg("j"), nil)
	if wm.cursor != 1 {
		t.Errorf("cursor should remain 1, got %d", wm.cursor)
	}

	// Move up.
	wm, _ = wm.update(keyMsg("k"), nil)
	if wm.cursor != 0 {
		t.Errorf("cursor should be 0, got %d", wm.cursor)
	}
}

func TestSpinnerSyncOnRunningProcess(t *testing.T) {
	sm := newSphereModel()

	data := &status.SphereStatus{
		SOLHome: "/test",
		Health:  "healthy",
		Prefect: status.PrefectInfo{Running: true},
		Consul:  status.ConsulInfo{Running: false},
	}
	sm.updateData(data)

	if _, ok := sm.processSpinners["Prefect"]; !ok {
		t.Error("running process should have a spinner")
	}
	if _, ok := sm.processSpinners["Consul"]; ok {
		t.Error("stopped process should not have a spinner")
	}

	// Now mark consul as running.
	data.Consul.Running = true
	sm.updateData(data)

	if _, ok := sm.processSpinners["Consul"]; !ok {
		t.Error("consul should now have a spinner")
	}

	// Mark prefect as stopped.
	data.Prefect.Running = false
	sm.updateData(data)

	if _, ok := sm.processSpinners["Prefect"]; ok {
		t.Error("prefect spinner should be removed when stopped")
	}
}

func TestWorldSpinnerSyncOnWorkingAgents(t *testing.T) {
	wm := newWorldModel()

	data := &status.WorldStatus{
		World: "test",
		Agents: []status.AgentStatus{
			{Name: "Alpha", State: "working", SessionAlive: true},
			{Name: "Beta", State: "idle"},
		},
	}
	wm.updateData(data)

	if _, ok := wm.agentSpinners["Alpha"]; !ok {
		t.Error("working agent should have a spinner")
	}
	if _, ok := wm.agentSpinners["Beta"]; ok {
		t.Error("idle agent should not have a spinner")
	}

	// Alpha goes idle.
	data.Agents[0].State = "idle"
	data.Agents[0].SessionAlive = false
	wm.updateData(data)

	if _, ok := wm.agentSpinners["Alpha"]; ok {
		t.Error("newly idle agent should have spinner removed")
	}
}

func TestSleepingWorldRow(t *testing.T) {
	sm := newSphereModel()
	sm.width = 120
	sm.height = 40

	data := &status.SphereStatus{
		SOLHome: "/test",
		Health:  "healthy",
		Worlds: []status.WorldSummary{
			{Name: "sleepy", Sleeping: true, Health: "sleeping"},
		},
	}
	sm.updateData(data)

	output := sm.view(data, time.Now())

	if !strings.Contains(output, "sleepy") {
		t.Error("sleeping world name should appear in output")
	}
	if !strings.Contains(output, "sleeping") {
		t.Error("sleeping world should show sleeping badge")
	}
}

// keyMsg helper to create tea.KeyMsg for testing.
func keyMsg(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}
