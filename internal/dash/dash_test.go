package dash

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nevinsm/sol/internal/events"
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

	output := sm.view(data, time.Now(), 0, false)

	checks := []string{
		"Sol Sphere",
		"healthy",
		"Processes",
		"Prefect",
		"Consul",
		"Chronicle",
		"Ledger",
		"Broker",
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

	output := sm.view(data, time.Now(), 0, false)

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

	output := sm.view(data, time.Now(), 0, false)

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

	output := sm.view(data, time.Now(), 0, false)

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

	output := sm.view(data, time.Now(), 0, false)

	checks := []string{
		"q quit",
		"select",
		"drill in",
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
	output := sm.view(nil, time.Time{}, 0, false)
	if !strings.Contains(output, "Gathering sphere status...") {
		t.Error("sphere view with nil data should show loading message")
	}
}

func TestWorldViewRendersProcesses(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:     "testworld",
		Prefect:   status.PrefectInfo{Running: true, PID: 42},
		Forge:     status.ForgeInfo{Running: true, PID: 12345},
		Sentinel:  status.SentinelInfo{Running: true, PID: 200, PatrolCount: 5},
		Chronicle: status.ChronicleInfo{Running: true, PID: 100},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now(), 0, nil, false)

	// Sphere processes shown in compact grid; world processes shown as section summary.
	checks := []string{
		"World: testworld",
		"Sphere Processes",
		"World Processes",
		"Prefect",
		"Chronicle",
		"2/2 running", // world processes summary
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
			{Name: "Toast", State: "working", SessionAlive: true, ActiveWrit: "sol-aaa", WorkTitle: "fix bug"},
			{Name: "Crisp", State: "idle"},
		},
		Summary: status.Summary{Total: 2, Working: 1, Idle: 1},
	}
	wm.updateData(data)

	// Default (no focus): always-expanded table shows detail rows.
	output := wm.view(data, time.Now(), 0, nil, false)

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

	// Focused: should also show detail rows with focus indicator.
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts
	output = wm.view(data, time.Now(), 0, nil, false)

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("focused world view missing %q", check)
		}
	}
	if !strings.Contains(output, focusIndicator) {
		t.Error("focused outposts should show focus indicator")
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
			{Name: "Scout", State: "working", SessionAlive: true, ActiveWrit: "sol-bbb", WorkTitle: "Design review", BriefAge: "45m"},
		},
	}
	wm.updateData(data)

	// Default (no focus): always-expanded table shows detail rows.
	output := wm.view(data, time.Now(), 0, nil, false)

	checks := []string{
		"Envoys (1)",
		"Scout",
		"BRIEF",
		"45m ago",
		"Design review",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("envoy view missing %q", check)
		}
	}

	// Focused: should also show detail rows with focus indicator.
	wm.hasFocus = true
	wm.focusedSection = sectionEnvoys
	output = wm.view(data, time.Now(), 0, nil, false)

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("focused envoy view missing %q", check)
		}
	}
	if !strings.Contains(output, focusIndicator) {
		t.Error("focused envoys should show focus indicator")
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

	output := wm.view(data, time.Now(), 0, nil, false)

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

	output := wm.view(data, time.Now(), 0, nil, false)

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

	output := wm.view(data, time.Now(), 0, nil, false)

	if !strings.Contains(output, "empty") {
		t.Error("world view should show 'empty' for empty merge queue")
	}
}

func TestWorldViewNilData(t *testing.T) {
	wm := newWorldModel()
	output := wm.view(nil, time.Time{}, 0, nil, false)
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

	output := wm.view(data, time.Now(), 0, nil, false)

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
	wm.outpostLen = 2
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts

	// Move down.
	wm, _ = wm.update(keyMsg("j"), nil)
	if wm.outpostCursor != 1 {
		t.Errorf("cursor should be 1, got %d", wm.outpostCursor)
	}

	// Can't go past.
	wm, _ = wm.update(keyMsg("j"), nil)
	if wm.outpostCursor != 1 {
		t.Errorf("cursor should remain 1, got %d", wm.outpostCursor)
	}

	// Move up.
	wm, _ = wm.update(keyMsg("k"), nil)
	if wm.outpostCursor != 0 {
		t.Errorf("cursor should be 0, got %d", wm.outpostCursor)
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

	output := sm.view(data, time.Now(), 0, false)

	if !strings.Contains(output, "sleepy") {
		t.Error("sleeping world name should appear in output")
	}
	if !strings.Contains(output, "sleeping") {
		t.Error("sleeping world should show sleeping badge")
	}
}

// --- New tests for p1 features ---

func TestSphereDrillIn(t *testing.T) {
	sm := newSphereModel()
	sm.worldRows = 2

	data := &status.SphereStatus{
		Worlds: []status.WorldSummary{
			{Name: "alpha"},
			{Name: "beta"},
		},
	}

	// Move to beta.
	sm, _ = sm.update(keyMsg("down"), data)

	// Press enter to drill in.
	sm, cmd := sm.update(keyMsg("enter"), data)
	if cmd == nil {
		t.Fatal("enter on world row should produce a command")
	}

	// Execute the command and check the message.
	msg := cmd()
	drill, ok := msg.(drillMsg)
	if !ok {
		t.Fatalf("expected drillMsg, got %T", msg)
	}
	if drill.world != "beta" {
		t.Errorf("drillMsg.world = %q, want %q", drill.world, "beta")
	}
}

func TestSphereDrillInVimL(t *testing.T) {
	sm := newSphereModel()
	sm.worldRows = 1

	data := &status.SphereStatus{
		Worlds: []status.WorldSummary{
			{Name: "alpha"},
		},
	}

	// Press l (vim right) to drill in.
	_, cmd := sm.update(keyMsg("l"), data)
	if cmd == nil {
		t.Fatal("l key on world row should produce a drill command")
	}
	msg := cmd()
	drill, ok := msg.(drillMsg)
	if !ok {
		t.Fatalf("expected drillMsg, got %T", msg)
	}
	if drill.world != "alpha" {
		t.Errorf("drillMsg.world = %q, want %q", drill.world, "alpha")
	}
}

func TestWorldViewPopBack(t *testing.T) {
	wm := newWorldModel()
	wm.outpostLen = 1

	// With no focus, esc pops back.
	_, cmd := wm.update(keyMsg("esc"), nil)
	if cmd == nil {
		t.Fatal("esc without focus should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(popMsg); !ok {
		t.Fatalf("expected popMsg, got %T", msg)
	}
}

func TestWorldViewPopBackH(t *testing.T) {
	wm := newWorldModel()

	// Press h to pop back.
	_, cmd := wm.update(keyMsg("h"), nil)
	if cmd == nil {
		t.Fatal("h in world view should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(popMsg); !ok {
		t.Fatalf("expected popMsg, got %T", msg)
	}
}

func TestWorldViewSectionFocusCycle(t *testing.T) {
	wm := newWorldModel()
	wm.processLen = 3
	wm.outpostLen = 2
	wm.envoyLen = 1

	// Tab to focus — default is sectionProcesses (iota 0), cycles forward to outposts.
	wm, _ = wm.update(tabKeyMsg(), nil)
	if !wm.hasFocus {
		t.Fatal("tab should set hasFocus")
	}
	if wm.focusedSection != sectionOutposts {
		t.Errorf("first tab should focus outposts (cycles from default processes), got %d", wm.focusedSection)
	}

	// Tab again cycles to envoys.
	wm, _ = wm.update(tabKeyMsg(), nil)
	if wm.focusedSection != sectionEnvoys {
		t.Errorf("tab should cycle to envoys, got %d", wm.focusedSection)
	}

	// Tab again wraps to processes.
	wm, _ = wm.update(tabKeyMsg(), nil)
	if wm.focusedSection != sectionProcesses {
		t.Errorf("tab should wrap around to processes, got %d", wm.focusedSection)
	}
}

func TestWorldViewSectionFocusReverseTab(t *testing.T) {
	wm := newWorldModel()
	wm.processLen = 3
	wm.outpostLen = 2
	wm.envoyLen = 1

	// Shift-tab sets focus and wraps backward from processes to envoys.
	wm, _ = wm.update(shiftTabKeyMsg(), nil)
	if !wm.hasFocus {
		t.Error("shift-tab should set hasFocus")
	}
	if wm.focusedSection != sectionEnvoys {
		t.Errorf("shift-tab should wrap to envoys, got %d", wm.focusedSection)
	}
}

func TestWorldViewPerSectionCursors(t *testing.T) {
	wm := newWorldModel()
	wm.outpostLen = 3
	wm.envoyLen = 2
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts

	// Move outpost cursor down.
	wm, _ = wm.update(keyMsg("j"), nil)
	if wm.outpostCursor != 1 {
		t.Errorf("outpost cursor should be 1, got %d", wm.outpostCursor)
	}

	// Tab to envoys and move cursor.
	wm, _ = wm.update(tabKeyMsg(), nil)
	wm, _ = wm.update(keyMsg("j"), nil)
	if wm.envoyCursor != 1 {
		t.Errorf("envoy cursor should be 1, got %d", wm.envoyCursor)
	}

	// Tab back to outposts — cursor should be preserved.
	wm, _ = wm.update(tabKeyMsg(), nil)
	if wm.outpostCursor != 1 {
		t.Errorf("outpost cursor should still be 1, got %d", wm.outpostCursor)
	}
}

func TestWorldViewMergeQueueRows(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:      "testworld",
		Prefect:    status.PrefectInfo{Running: true, PID: 42},
		MergeQueue: status.MergeQueueInfo{Total: 2, Ready: 1, Claimed: 1},
		MergeRequests: []status.MergeRequestInfo{
			{ID: "mr-abc123", WritID: "sol-aaa", Phase: "ready", Title: "fix things"},
			{ID: "mr-def456", WritID: "sol-bbb", Phase: "claimed", Title: "add feature"},
		},
	}
	wm.updateData(data)

	// Focus on merge queue to see detail rows.
	wm.hasFocus = true
	wm.focusedSection = sectionMergeQueue

	output := wm.view(data, time.Now(), 0, nil, false)

	checks := []string{
		"Merge Queue",
		"mr-abc123",
		"mr-def456",
		"sol-aaa",
		"sol-bbb",
		"fix things",
		"add feature",
		"ID",
		"WRIT",
		"STATUS",
		"TITLE",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("world view merge queue rows missing %q", check)
		}
	}
}

func TestWorldViewEnterProducesPeekMsg(t *testing.T) {
	wm := newWorldModel()
	wm.outpostLen = 1
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts

	data := &status.WorldStatus{
		World: "testworld",
		Agents: []status.AgentStatus{
			{Name: "Toast", State: "working", SessionAlive: true},
		},
	}

	// Press enter to peek.
	_, cmd := wm.update(keyMsg("enter"), data)
	if cmd == nil {
		t.Fatal("enter on live agent should produce a command")
	}
	msg := cmd()
	peek, ok := msg.(peekMsg)
	if !ok {
		t.Fatalf("expected peekMsg, got %T", msg)
	}
	if len(peek.items) == 0 {
		t.Fatal("peekMsg should have items")
	}
	if peek.items[0].sessionName != "sol-testworld-Toast" {
		t.Errorf("peekMsg item sessionName = %q, want %q", peek.items[0].sessionName, "sol-testworld-Toast")
	}
}

func TestWorldViewDirectAttachProducesMsg(t *testing.T) {
	wm := newWorldModel()
	wm.outpostLen = 1
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts

	data := &status.WorldStatus{
		World: "testworld",
		Agents: []status.AgentStatus{
			{Name: "Toast", State: "working", SessionAlive: true},
		},
	}

	// Press 'a' to direct attach.
	_, cmd := wm.update(keyMsg("a"), data)
	if cmd == nil {
		t.Fatal("'a' on live agent should produce a command")
	}
	msg := cmd()
	attach, ok := msg.(attachMsg)
	if !ok {
		t.Fatalf("expected attachMsg, got %T", msg)
	}
	if attach.sessionName != "sol-testworld-Toast" {
		t.Errorf("attachMsg.sessionName = %q, want %q", attach.sessionName, "sol-testworld-Toast")
	}
}

func TestWorldViewAttachNoSession(t *testing.T) {
	wm := newWorldModel()
	wm.outpostLen = 1
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts

	data := &status.WorldStatus{
		World: "testworld",
		Agents: []status.AgentStatus{
			{Name: "Toast", State: "idle", SessionAlive: false},
		},
	}

	// Press 'a' on dead agent for direct attach.
	_, cmd := wm.update(keyMsg("a"), data)
	if cmd == nil {
		t.Fatal("'a' on dead agent should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(noSessionMsg); !ok {
		t.Fatalf("expected noSessionMsg, got %T", msg)
	}
}

func TestWorldViewDirectAttachEnvoy(t *testing.T) {
	wm := newWorldModel()
	wm.outpostLen = 0
	wm.envoyLen = 1
	wm.hasFocus = true
	wm.focusedSection = sectionEnvoys

	data := &status.WorldStatus{
		World: "testworld",
		Envoys: []status.EnvoyStatus{
			{Name: "Scout", State: "working", SessionAlive: true},
		},
	}

	// Press 'a' to direct attach to envoy.
	_, cmd := wm.update(keyMsg("a"), data)
	if cmd == nil {
		t.Fatal("'a' on live envoy should produce a command")
	}
	msg := cmd()
	attach, ok := msg.(attachMsg)
	if !ok {
		t.Fatalf("expected attachMsg, got %T", msg)
	}
	if attach.sessionName != "sol-testworld-Scout" {
		t.Errorf("attachMsg.sessionName = %q, want %q", attach.sessionName, "sol-testworld-Scout")
	}
}

func TestWorldViewNoSessionDismiss(t *testing.T) {
	wm := newWorldModel()
	wm.showNoSession = true

	// Any key should dismiss.
	wm, _ = wm.update(keyMsg("j"), nil)
	if wm.showNoSession {
		t.Error("showNoSession should be false after key press")
	}
}

func TestWorldViewFocusIndicator(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:   "testworld",
		Prefect: status.PrefectInfo{Running: true, PID: 42},
		Agents: []status.AgentStatus{
			{Name: "Toast", State: "idle"},
		},
		MergeQueue: status.MergeQueueInfo{Total: 1, Ready: 1},
		MergeRequests: []status.MergeRequestInfo{
			{ID: "mr-001", WritID: "sol-aaa", Phase: "ready", Title: "test"},
		},
	}
	wm.updateData(data)

	// Without focus: always-expanded but no focus indicator.
	output := wm.view(data, time.Now(), 0, nil, false)
	if !strings.Contains(output, "Outposts") {
		t.Error("view should contain Outposts section")
	}
	if strings.Contains(output, focusIndicator) {
		t.Error("unfocused view should not contain focus indicator")
	}

	// With focus: shows focus indicator on the focused section.
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts
	output = wm.view(data, time.Now(), 0, nil, false)
	if !strings.Contains(output, focusIndicator) {
		t.Error("focused view should contain focus indicator")
	}
}

func TestWorldViewFooter(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:   "testworld",
		Prefect: status.PrefectInfo{Running: true, PID: 42},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now(), 0, nil, false)

	checks := []string{
		"q quit",
		"select",
		"tab section",
		"enter peek",
		"a attach",
		"esc back",
		"r refresh",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("world view footer missing %q", check)
		}
	}
}

func TestModelActiveView(t *testing.T) {
	m := NewModel(Config{})
	if m.activeView() != viewSphere {
		t.Errorf("default activeView should be viewSphere")
	}

	m2 := NewModel(Config{World: "test"})
	if m2.activeView() != viewWorld {
		t.Errorf("activeView with world should be viewWorld")
	}
}

func TestModelDrillInPopPreservesCursor(t *testing.T) {
	m := NewModel(Config{})
	m.ready = true
	m.width = 120
	m.height = 40
	m.sphereData = &status.SphereStatus{
		SOLHome: "/test",
		Health:  "healthy",
		Worlds: []status.WorldSummary{
			{Name: "alpha"},
			{Name: "beta"},
		},
	}
	m.sphereView.updateData(m.sphereData)
	m.sphereView.width = 120
	m.sphereView.height = 40

	// Move cursor to beta.
	m.sphereView.cursor = 1

	// Drill into beta.
	m2, _ := m.Update(drillMsg{world: "beta"})
	model := m2.(Model)
	if model.activeView() != viewWorld {
		t.Error("after drill, activeView should be viewWorld")
	}
	if model.world != "beta" {
		t.Errorf("after drill, world = %q, want %q", model.world, "beta")
	}

	// Pop back.
	m3, _ := model.Update(popMsg{})
	model2 := m3.(Model)
	if model2.activeView() != viewSphere {
		t.Error("after pop, activeView should be viewSphere")
	}
	if model2.sphereView.cursor != 1 {
		t.Errorf("after pop, sphere cursor should be preserved at 1, got %d", model2.sphereView.cursor)
	}
}

func TestWorldViewNoSessionMessage(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40
	wm.showNoSession = true

	data := &status.WorldStatus{
		World:   "testworld",
		Prefect: status.PrefectInfo{Running: true, PID: 42},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now(), 0, nil, false)

	if !strings.Contains(output, "no active session") {
		t.Error("world view should show 'no active session' when showNoSession is true")
	}
}

// keyMsg helper to create tea.KeyMsg for testing.
func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

func tabKeyMsg() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyTab}
}

func shiftTabKeyMsg() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyShiftTab}
}

// --- Feed tests ---

func TestFormatEventCast(t *testing.T) {
	ev := events.Event{
		Timestamp: time.Date(2025, 1, 1, 14, 32, 0, 0, time.UTC),
		Type:      events.EventCast,
		Actor:     "Toast",
		Payload:   map[string]any{"writ_id": "sol-abc123", "world": "sol-dev"},
	}
	line := formatEvent(ev, 120)
	checks := []string{"Toast", "dispatched", "sol-abc123", "sol-dev"}
	for _, check := range checks {
		if !strings.Contains(line, check) {
			t.Errorf("formatEvent(cast) missing %q, got %q", check, line)
		}
	}
}

func TestFormatEventResolve(t *testing.T) {
	ev := events.Event{
		Timestamp: time.Date(2025, 1, 1, 14, 31, 0, 0, time.UTC),
		Type:      events.EventResolve,
		Actor:     "Toast",
		Payload:   map[string]any{"writ_id": "sol-abc123"},
	}
	line := formatEvent(ev, 120)
	if !strings.Contains(line, "resolved") {
		t.Errorf("formatEvent(resolve) missing 'resolved', got %q", line)
	}
	if !strings.Contains(line, "sol-abc123") {
		t.Errorf("formatEvent(resolve) missing writ_id, got %q", line)
	}
}

func TestFormatEventMerged(t *testing.T) {
	ev := events.Event{
		Timestamp: time.Date(2025, 1, 1, 14, 30, 0, 0, time.UTC),
		Type:      events.EventMerged,
		Actor:     "Forge",
		Payload:   map[string]any{"merge_request_id": "42", "world": "sol-dev"},
	}
	line := formatEvent(ev, 120)
	if !strings.Contains(line, "merged") {
		t.Errorf("formatEvent(merged) missing 'merged', got %q", line)
	}
	if !strings.Contains(line, "MR 42") {
		t.Errorf("formatEvent(merged) missing MR ID, got %q", line)
	}
}

func TestFormatEventTruncation(t *testing.T) {
	ev := events.Event{
		Timestamp: time.Date(2025, 1, 1, 14, 30, 0, 0, time.UTC),
		Type:      events.EventCast,
		Actor:     "SomeLongAgentName",
		Payload:   map[string]any{"writ_id": "sol-verylongwritid1234567890", "world": "some-world-name"},
	}
	line := formatEvent(ev, 40)
	if len(line) > 40 {
		t.Errorf("formatEvent should truncate to maxWidth, got len=%d", len(line))
	}
	if !strings.HasSuffix(line, "...") {
		t.Errorf("truncated line should end with ..., got %q", line)
	}
}

func TestEventVerb(t *testing.T) {
	tests := []struct {
		eventType string
		want      string
	}{
		{events.EventCast, "dispatched"},
		{events.EventResolve, "resolved"},
		{events.EventMerged, "merged"},
		{events.EventMergeFailed, "merge failed"},
		{events.EventRespawn, "respawned"},
		{events.EventStalled, "stalled"},
		{events.EventEscalationCreated, "escalated"},
		{events.EventHandoff, "handed off"},
		{events.EventDegraded, "entered degraded mode"},
		{events.EventRecovered, "recovered"},
	}

	for _, tt := range tests {
		got := eventVerb(tt.eventType)
		if got != tt.want {
			t.Errorf("eventVerb(%q) = %q, want %q", tt.eventType, got, tt.want)
		}
	}
}

func TestEventVerbUnknownType(t *testing.T) {
	got := eventVerb("unknown_type")
	if got != "unknown_type" {
		t.Errorf("eventVerb(unknown) should return the type itself, got %q", got)
	}
}

func TestFeedViewEmpty(t *testing.T) {
	fm := newFeedModel(t.TempDir(), "")
	output := fm.view(120)
	if !strings.Contains(output, "No recent activity") {
		t.Error("empty feed should show 'No recent activity'")
	}
	if !strings.Contains(output, "─") {
		t.Error("feed should have separator line")
	}
}

func TestFeedViewWithEvents(t *testing.T) {
	fm := newFeedModel(t.TempDir(), "")
	fm.events = []events.Event{
		{
			Timestamp: time.Date(2025, 1, 1, 14, 30, 0, 0, time.UTC),
			Type:      events.EventCast,
			Actor:     "Toast",
			Payload:   map[string]any{"writ_id": "sol-aaa", "world": "dev"},
		},
		{
			Timestamp: time.Date(2025, 1, 1, 14, 31, 0, 0, time.UTC),
			Type:      events.EventResolve,
			Actor:     "Toast",
			Payload:   map[string]any{"writ_id": "sol-aaa"},
		},
	}
	output := fm.view(120)
	if !strings.Contains(output, "Toast") {
		t.Error("feed should show actor name")
	}
	if strings.Contains(output, "No recent activity") {
		t.Error("feed with events should not show empty message")
	}
}

func TestFeedWorldFilter(t *testing.T) {
	fm := newFeedModel(t.TempDir(), "alpha")

	evts := []events.Event{
		{
			Timestamp: time.Now(),
			Type:      events.EventCast,
			Actor:     "Toast",
			Source:    "sol",
			Payload:   map[string]any{"writ_id": "sol-aaa", "world": "alpha"},
		},
		{
			Timestamp: time.Now(),
			Type:      events.EventCast,
			Actor:     "Crisp",
			Source:    "sol",
			Payload:   map[string]any{"writ_id": "sol-bbb", "world": "beta"},
		},
		{
			Timestamp: time.Now(),
			Type:      events.EventPatrol,
			Actor:     "sentinel",
			Source:    "alpha/sentinel",
			Payload:   map[string]any{},
		},
	}

	filtered := fm.filterWorld(evts)
	if len(filtered) != 2 {
		t.Errorf("expected 2 events for world 'alpha', got %d", len(filtered))
	}
}

func TestFeedSetHeight(t *testing.T) {
	fm := newFeedModel(t.TempDir(), "")

	fm.setHeight(60)
	if fm.feedLines != 8 {
		t.Errorf("height 60 should give 8 feed lines, got %d", fm.feedLines)
	}

	fm.setHeight(45)
	if fm.feedLines != 7 {
		t.Errorf("height 45 should give 7 feed lines, got %d", fm.feedLines)
	}

	fm.setHeight(35)
	if fm.feedLines != 6 {
		t.Errorf("height 35 should give 6 feed lines, got %d", fm.feedLines)
	}

	fm.setHeight(20)
	if fm.feedLines != 5 {
		t.Errorf("height 20 should give 5 feed lines, got %d", fm.feedLines)
	}
}

func TestFeedMostRecentFirst(t *testing.T) {
	fm := newFeedModel(t.TempDir(), "")
	fm.events = []events.Event{
		{
			Timestamp: time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC),
			Type:      events.EventCast,
			Actor:     "OldActor",
			Payload:   map[string]any{"writ_id": "sol-old"},
		},
		{
			Timestamp: time.Date(2025, 1, 1, 14, 0, 0, 0, time.UTC),
			Type:      events.EventResolve,
			Actor:     "NewActor",
			Payload:   map[string]any{"writ_id": "sol-new"},
		},
	}
	output := fm.view(120)
	// Most recent event should appear first (higher in the output).
	newIdx := strings.Index(output, "NewActor")
	oldIdx := strings.Index(output, "OldActor")
	if newIdx == -1 || oldIdx == -1 {
		t.Fatal("both events should appear in output")
	}
	if newIdx > oldIdx {
		t.Error("most recent event should appear before older event")
	}
}

// --- Help overlay tests ---

func TestHelpOverlayContent(t *testing.T) {
	output := helpOverlay(120, 40)

	checks := []string{
		"Sol Dash",
		"Keyboard Shortcuts",
		"Navigation",
		"j/k",
		"Move selection",
		"enter or l",
		"Actions",
		"Force refresh",
		"Toggle this help",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("help overlay missing %q", check)
		}
	}
}

func TestHelpToggle(t *testing.T) {
	m := NewModel(Config{})
	m.ready = true
	m.width = 120
	m.height = 40

	// Toggle help on.
	m2, _ := m.Update(keyMsg("?"))
	model := m2.(Model)
	if !model.showHelp {
		t.Error("? should toggle help on")
	}

	// View should show help overlay.
	output := model.View()
	if !strings.Contains(output, "Keyboard Shortcuts") {
		t.Error("view should show help overlay when showHelp is true")
	}

	// Any key dismisses it.
	m3, _ := model.Update(keyMsg("x"))
	model2 := m3.(Model)
	if model2.showHelp {
		t.Error("any key should dismiss help overlay")
	}
}

// --- Terminal size tests ---

func TestMinTerminalSize(t *testing.T) {
	m := NewModel(Config{})
	m.ready = true
	m.width = 60
	m.height = 20

	output := m.View()
	if !strings.Contains(output, "Terminal too small") {
		t.Error("should show 'Terminal too small' message for small terminals")
	}
}

func TestMinTerminalSizeWidthOnly(t *testing.T) {
	m := NewModel(Config{})
	m.ready = true
	m.width = 60
	m.height = 40

	output := m.View()
	if !strings.Contains(output, "Terminal too small") {
		t.Error("should show 'Terminal too small' when width is below minimum")
	}
}

// --- State-change highlight tests ---

func TestHealthHighlightDecay(t *testing.T) {
	m := NewModel(Config{})

	// Simulate initial data.
	m.prevSphereHealth = "healthy"

	// Health changes — should start at max level.
	m.trackSphereHighlights(&status.SphereStatus{Health: "degraded"})
	if m.healthHighlight != highlightMaxLevel {
		t.Errorf("healthHighlight should be %d after change, got %d", highlightMaxLevel, m.healthHighlight)
	}

	// Decay through all levels.
	for i := highlightMaxLevel - 1; i >= 0; i-- {
		m.decayHighlights()
		if m.healthHighlight != i {
			t.Errorf("healthHighlight should be %d after decay, got %d", i, m.healthHighlight)
		}
	}

	// Should stay at 0.
	m.decayHighlights()
	if m.healthHighlight != 0 {
		t.Errorf("healthHighlight should remain 0, got %d", m.healthHighlight)
	}
}

func TestAgentStateHighlight(t *testing.T) {
	m := NewModel(Config{})

	// First data — establishes baseline.
	m.trackWorldHighlights(&status.WorldStatus{
		World: "test",
		Agents: []status.AgentStatus{
			{Name: "Alpha", State: "idle"},
		},
	})
	if _, ok := m.agentHighlights["Alpha"]; ok {
		t.Error("first data should not trigger highlights")
	}

	// State changes — should start at max level.
	m.trackWorldHighlights(&status.WorldStatus{
		World: "test",
		Agents: []status.AgentStatus{
			{Name: "Alpha", State: "working"},
		},
	})
	if level, ok := m.agentHighlights["Alpha"]; !ok {
		t.Error("state change should trigger highlight")
	} else if level != highlightMaxLevel {
		t.Errorf("highlight should start at %d, got %d", highlightMaxLevel, level)
	}

	// Decay through all levels until removed.
	for i := 0; i < highlightMaxLevel; i++ {
		m.decayHighlights()
	}
	if _, ok := m.agentHighlights["Alpha"]; ok {
		t.Error("highlight should be removed after full decay")
	}
}

func TestHighlightProgressiveFade(t *testing.T) {
	m := NewModel(Config{})

	m.prevSphereHealth = "healthy"
	m.trackSphereHighlights(&status.SphereStatus{Health: "degraded"})

	// Verify each intermediate level during decay.
	for expected := highlightMaxLevel; expected > 0; expected-- {
		if m.healthHighlight != expected {
			t.Errorf("expected level %d, got %d", expected, m.healthHighlight)
		}
		m.decayHighlights()
	}
	if m.healthHighlight != 0 {
		t.Errorf("expected level 0 after full decay, got %d", m.healthHighlight)
	}
}

func TestHighlightAtLevel(t *testing.T) {
	// Level 0 should produce no background.
	s := highlightAtLevel(0)
	if s.GetBackground() != (lipgloss.NoColor{}) {
		t.Error("level 0 should have no background color")
	}

	// Levels 1-5 should produce progressively brighter backgrounds.
	for level := 1; level <= 5; level++ {
		s := highlightAtLevel(level)
		bg := s.GetBackground()
		if bg == (lipgloss.NoColor{}) {
			t.Errorf("level %d should have a background color", level)
		}
	}

	// Out of range should produce no background.
	s = highlightAtLevel(6)
	if s.GetBackground() != (lipgloss.NoColor{}) {
		t.Error("level 6 should have no background color")
	}
}

func TestHealthEmphasisInSphereView(t *testing.T) {
	sm := newSphereModel()
	sm.width = 120
	sm.height = 40

	data := &status.SphereStatus{
		SOLHome: "/test",
		Health:  "healthy",
	}
	sm.updateData(data)

	// Both with and without emphasis should render.
	output1 := sm.view(data, time.Now(), 0, false)
	output2 := sm.view(data, time.Now(), highlightMaxLevel, false)
	if !strings.Contains(output1, "healthy") {
		t.Error("sphere view should show health badge")
	}
	if !strings.Contains(output2, "healthy") {
		t.Error("sphere view with emphasis should show health badge")
	}
}

func TestAgentHighlightInWorldView(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:   "test",
		Prefect: status.PrefectInfo{Running: true, PID: 1},
		Agents: []status.AgentStatus{
			{Name: "Alpha", State: "working", SessionAlive: true},
		},
	}
	wm.updateData(data)

	// Focus on outposts to see agent rows with highlights.
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts

	// Test at various highlight levels.
	for _, level := range []int{1, 3, 5} {
		highlights := map[string]int{"Alpha": level}
		output := wm.view(data, time.Now(), 0, highlights, false)
		if !strings.Contains(output, "Alpha") {
			t.Errorf("world view should still show agent name with highlight level %d", level)
		}
	}
}

// --- Event matching tests ---

func TestEventMatchesWorld(t *testing.T) {
	tests := []struct {
		name  string
		ev    events.Event
		world string
		want  bool
	}{
		{
			name:  "source prefix match",
			ev:    events.Event{Source: "alpha/sentinel"},
			world: "alpha",
			want:  true,
		},
		{
			name:  "source exact match",
			ev:    events.Event{Source: "alpha"},
			world: "alpha",
			want:  true,
		},
		{
			name:  "payload world match",
			ev:    events.Event{Source: "sol", Payload: map[string]any{"world": "alpha"}},
			world: "alpha",
			want:  true,
		},
		{
			name:  "no match",
			ev:    events.Event{Source: "sol", Payload: map[string]any{"world": "beta"}},
			world: "alpha",
			want:  false,
		},
		{
			name:  "nil payload no match",
			ev:    events.Event{Source: "sol"},
			world: "alpha",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eventMatchesWorld(tt.ev, tt.world)
			if got != tt.want {
				t.Errorf("eventMatchesWorld = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPadRightWithStyledStrings(t *testing.T) {
	// Plain string should be padded normally.
	plain := padRight("hello", 10)
	if len(plain) != 10 {
		t.Errorf("padRight plain: expected len 10, got %d", len(plain))
	}
	if plain != "hello     " {
		t.Errorf("padRight plain: got %q", plain)
	}

	// Styled string should be padded based on visible width, not byte length.
	styled := okStyle.Render("working")
	padded := padRight(styled, 18)
	// The visible width should be 18 (7 chars + 11 spaces).
	visible := lipgloss.Width(padded)
	if visible != 18 {
		t.Errorf("padRight styled: visible width = %d, want 18", visible)
	}

	// Already wider string should not be truncated.
	wide := padRight("very long string here", 5)
	if wide != "very long string here" {
		t.Errorf("padRight wider: should not truncate, got %q", wide)
	}
}

func TestWorldViewSphereProcessSpinners(t *testing.T) {
	wm := newWorldModel()

	data := &status.WorldStatus{
		World:     "test",
		Prefect:   status.PrefectInfo{Running: true, PID: 100},
		Chronicle: status.ChronicleInfo{Running: true},
		Ledger:    status.LedgerInfo{Running: false},
		Broker:    status.BrokerInfo{Running: true, PatrolCount: 3},
		Forge:     status.ForgeInfo{Running: true},
		Sentinel:  status.SentinelInfo{Running: true},
	}
	wm.updateData(data)

	// Running sphere processes should have spinners.
	for _, name := range []string{"Prefect", "Chronicle", "Broker"} {
		if _, ok := wm.processSpinners[name]; !ok {
			t.Errorf("running sphere process %q should have a spinner", name)
		}
	}
	// Non-running should not.
	for _, name := range []string{"Ledger"} {
		if _, ok := wm.processSpinners[name]; ok {
			t.Errorf("stopped sphere process %q should not have a spinner", name)
		}
	}
	// World processes should also have spinners.
	for _, name := range []string{"Forge", "Sentinel"} {
		if _, ok := wm.processSpinners[name]; !ok {
			t.Errorf("running world process %q should have a spinner", name)
		}
	}
}

func TestWorldViewSummary(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:   "testworld",
		Prefect: status.PrefectInfo{Running: true, PID: 42},
		Agents: []status.AgentStatus{
			{Name: "Toast", State: "working", SessionAlive: true},
			{Name: "Crisp", State: "idle"},
			{Name: "Burnt", State: "stalled"},
		},
		Envoys: []status.EnvoyStatus{
			{Name: "Scout", State: "working", SessionAlive: true},
		},
		Summary: status.Summary{Total: 3, Working: 1, Idle: 1, Stalled: 1},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now(), 0, nil, false)

	checks := []string{
		"3 agents",
		"1 envoys",
		"1 working",
		"1 idle",
		"1 stalled",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("world view summary missing %q", check)
		}
	}
}

func TestWorldViewSectionOrdering(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:      "testworld",
		Prefect:    status.PrefectInfo{Running: true, PID: 42},
		Forge:      status.ForgeInfo{Running: true},
		Agents:     []status.AgentStatus{{Name: "A", State: "idle"}},
		Envoys:     []status.EnvoyStatus{{Name: "E", State: "idle"}},
		MergeQueue: status.MergeQueueInfo{Total: 1, Ready: 1},
		Caravans:   []status.CaravanInfo{{ID: "c1", Name: "batch", TotalItems: 5}},
		Summary:    status.Summary{Total: 1, Idle: 1},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now(), 0, nil, false)

	// Verify order: Sphere Processes → World Processes → Outposts → Envoys → Caravans → Merge Queue → Summary
	sphereIdx := strings.Index(output, "Sphere Processes")
	worldProcIdx := strings.Index(output, "World Processes")
	outpostsIdx := strings.Index(output, "Outposts")
	envoysIdx := strings.Index(output, "Envoys")
	caravansIdx := strings.Index(output, "Caravans")
	mqIdx := strings.Index(output, "Merge Queue")

	if sphereIdx == -1 || worldProcIdx == -1 || outpostsIdx == -1 || envoysIdx == -1 || caravansIdx == -1 || mqIdx == -1 {
		t.Fatalf("missing sections in output:\n%s", output)
	}

	if sphereIdx >= worldProcIdx {
		t.Error("Sphere Processes should come before World Processes")
	}
	if worldProcIdx >= outpostsIdx {
		t.Error("World Processes should come before Outposts")
	}
	if outpostsIdx >= envoysIdx {
		t.Error("Outposts should come before Envoys")
	}
	if envoysIdx >= caravansIdx {
		t.Error("Envoys should come before Caravans")
	}
	if caravansIdx >= mqIdx {
		t.Error("Caravans should come before Merge Queue")
	}
}

func TestProcessDetailFormats(t *testing.T) {
	// Prefect detail.
	if d := formatPrefectDetail(status.PrefectInfo{Running: true, PID: 123}); d != "pid 123" {
		t.Errorf("prefect detail = %q, want %q", d, "pid 123")
	}
	if d := formatPrefectDetail(status.PrefectInfo{Running: false}); d != "" {
		t.Errorf("stopped prefect detail should be empty, got %q", d)
	}

	// Forge detail.
	if d := formatForgeDetail(status.ForgeInfo{Running: true, PID: 12345}); d != "pid 12345" {
		t.Errorf("forge detail = %q, want %q", d, "pid 12345")
	}
	if d := formatForgeDetail(status.ForgeInfo{Running: false}); d != "" {
		t.Errorf("stopped forge detail should be empty, got %q", d)
	}

	// Sentinel detail.
	if d := formatSentinelDetail(status.SentinelInfo{Running: true, PID: 123, PatrolCount: 10, AgentsChecked: 5, HeartbeatAge: "2m"}); d == "" {
		t.Error("running sentinel detail should not be empty")
	}
	if d := formatSentinelDetail(status.SentinelInfo{Running: true, PID: 123}); d != "pid 123" {
		t.Errorf("sentinel detail with PID only = %q, want %q", d, "pid 123")
	}

	// Chronicle detail.
	if d := formatChronicleDetail(status.ChronicleInfo{Running: true, PID: 456}); d != "pid 456" {
		t.Errorf("chronicle detail with PID = %q, want %q", d, "pid 456")
	}

	// Ledger detail.
	if d := formatLedgerDetail(status.LedgerInfo{Running: true, PID: 789}); d != "pid 789" {
		t.Errorf("ledger detail with PID = %q, want %q", d, "pid 789")
	}
	if d := formatLedgerDetail(status.LedgerInfo{Running: true, PID: 789, HeartbeatAge: "30s"}); d != "pid 789  hb 30s" {
		t.Errorf("ledger detail with heartbeat = %q, want %q", d, "pid 789  hb 30s")
	}

	// Broker detail.
	if d := formatBrokerDetail(status.BrokerInfo{Running: true, PatrolCount: 5}); d != "5 patrols" {
		t.Errorf("broker detail = %q, want %q", d, "5 patrols")
	}

}

func TestFeedLoadInitial(t *testing.T) {
	dir := t.TempDir()

	// Write some events to the curated feed file.
	feedFile := dir + "/.feed.jsonl"
	var lines []string
	for i := 0; i < 15; i++ {
		ts := time.Now().Add(time.Duration(i) * time.Minute).UTC().Format(time.RFC3339Nano)
		lines = append(lines, fmt.Sprintf(
			`{"ts":"%s","source":"sol","type":"cast","actor":"op%d","visibility":"feed","payload":{"writ_id":"sol-%d"}}`,
			ts, i, i,
		))
	}
	if err := os.WriteFile(feedFile, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	fm := newFeedModel(dir, "")
	fm.loadInitial()

	if len(fm.events) != 10 {
		t.Errorf("loadInitial should load 10 events, got %d", len(fm.events))
	}
	if fm.lastSeen.IsZero() {
		t.Error("lastSeen should be set after loadInitial")
	}
}

// --- Tests for merged MR filtering, work truncation, truncateStr ---

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 8, "hello..."},
		{"hello world", 3, "hel"},
		{"hello world", 2, "he"},
		{"abcdefghij", 7, "abcd..."},
		{"", 5, ""},
		{"ab", 5, "ab"},
	}

	for _, tt := range tests {
		got := truncateStr(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestWorldViewMergedMRsFiltered(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:      "testworld",
		Prefect:    status.PrefectInfo{Running: true, PID: 42},
		MergeQueue: status.MergeQueueInfo{Total: 4, Ready: 1, Claimed: 1, Merged: 2},
		MergeRequests: []status.MergeRequestInfo{
			{ID: "mr-001", WritID: "sol-aaa", Phase: "ready", Title: "ready MR"},
			{ID: "mr-002", WritID: "sol-bbb", Phase: "claimed", Title: "in progress MR"},
			{ID: "mr-003", WritID: "sol-ccc", Phase: "merged", Title: "merged MR one"},
			{ID: "mr-004", WritID: "sol-ddd", Phase: "merged", Title: "merged MR two"},
		},
	}
	wm.updateData(data)

	// Focus on merge queue to see detail rows.
	wm.hasFocus = true
	wm.focusedSection = sectionMergeQueue

	output := wm.view(data, time.Now(), 0, nil, false)

	// Active MRs should appear.
	if !strings.Contains(output, "mr-001") {
		t.Error("ready MR should appear in detail rows")
	}
	if !strings.Contains(output, "mr-002") {
		t.Error("claimed MR should appear in detail rows")
	}
	// Merged MRs should NOT appear as detail rows.
	if strings.Contains(output, "mr-003") {
		t.Error("merged MR should not appear in detail rows")
	}
	if strings.Contains(output, "mr-004") {
		t.Error("merged MR should not appear in detail rows")
	}
	// But the summary should still show merged count.
	if !strings.Contains(output, "2 merged") {
		t.Error("summary should still show merged count")
	}
}

func TestWorldViewWorkColumnTruncated(t *testing.T) {
	wm := newWorldModel()
	wm.width = 80
	wm.height = 40

	data := &status.WorldStatus{
		World:   "testworld",
		Prefect: status.PrefectInfo{Running: true, PID: 42},
		Agents: []status.AgentStatus{
			{
				Name:         "Toast",
				State:        "working",
				SessionAlive: true,
				ActiveWrit:   "sol-a1b2c3d4e5f6a7b8",
				WorkTitle:    "this is a very long work title that should be truncated",
			},
		},
		Summary: status.Summary{Total: 1, Working: 1},
	}
	wm.updateData(data)

	// Focus on outposts to see agent detail rows.
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts

	output := wm.view(data, time.Now(), 0, nil, false)

	// The full title should not appear — it would overflow.
	if strings.Contains(output, "this is a very long work title that should be truncated") {
		t.Error("long work title should be truncated on narrow terminal")
	}
	// But the truncated version should contain "..." suffix.
	if !strings.Contains(output, "...") {
		t.Error("truncated work title should contain '...'")
	}
}

func TestWorldViewMRSectionFocusable(t *testing.T) {
	wm := newWorldModel()
	wm.outpostLen = 1
	wm.envoyLen = 1
	wm.mrLen = 2

	// MR section should be in available sections when it has rows.
	sections := wm.availableSections()
	hasMQ := false
	for _, s := range sections {
		if s == sectionMergeQueue {
			hasMQ = true
		}
	}
	if !hasMQ {
		t.Error("MR section should be in available sections when it has active MRs")
	}

	// Tab through: outposts → envoys → merge queue → outposts.
	wm.focusedSection = sectionOutposts
	wm, _ = wm.update(tabKeyMsg(), nil)
	if wm.focusedSection != sectionEnvoys {
		t.Errorf("tab from outposts should go to envoys, got %d", wm.focusedSection)
	}
	wm, _ = wm.update(tabKeyMsg(), nil)
	if wm.focusedSection != sectionMergeQueue {
		t.Errorf("tab from envoys should go to merge queue, got %d", wm.focusedSection)
	}
	wm, _ = wm.update(tabKeyMsg(), nil)
	if wm.focusedSection != sectionOutposts {
		t.Errorf("tab from merge queue should wrap to outposts, got %d", wm.focusedSection)
	}
}

// --- New tests for summary-first, scroll, process grid, esc unfocus ---

func TestOutpostSummary(t *testing.T) {
	agents := []status.AgentStatus{
		{Name: "A", State: "working", SessionAlive: true},
		{Name: "B", State: "working", SessionAlive: true},
		{Name: "C", State: "idle"},
		{Name: "D", State: "stalled"},
	}
	s := outpostSummary(agents)
	if !strings.Contains(s, "2 working") {
		t.Errorf("outpostSummary missing '2 working', got %q", s)
	}
	if !strings.Contains(s, "1 idle") {
		t.Errorf("outpostSummary missing '1 idle', got %q", s)
	}
	if !strings.Contains(s, "1 stalled") {
		t.Errorf("outpostSummary missing '1 stalled', got %q", s)
	}
}

func TestOutpostSummaryDead(t *testing.T) {
	agents := []status.AgentStatus{
		{Name: "A", State: "working", SessionAlive: false},
	}
	s := outpostSummary(agents)
	if !strings.Contains(s, "1 dead") {
		t.Errorf("outpostSummary missing '1 dead', got %q", s)
	}
}

func TestEnvoySummarySingle(t *testing.T) {
	envoys := []status.EnvoyStatus{
		{Name: "Polaris", State: "working", SessionAlive: true},
	}
	s := envoySummary(envoys)
	if s != "Polaris (working)" {
		t.Errorf("envoySummary single = %q, want %q", s, "Polaris (working)")
	}
}

func TestEnvoySummaryDeadSingle(t *testing.T) {
	envoys := []status.EnvoyStatus{
		{Name: "Polaris", State: "working", SessionAlive: false},
	}
	s := envoySummary(envoys)
	if s != "Polaris (dead)" {
		t.Errorf("envoySummary dead single = %q, want %q", s, "Polaris (dead)")
	}
}

func TestEnvoySummaryMultiple(t *testing.T) {
	envoys := []status.EnvoyStatus{
		{Name: "A", State: "working", SessionAlive: true},
		{Name: "B", State: "idle"},
		{Name: "C", State: "working", SessionAlive: true},
	}
	s := envoySummary(envoys)
	if !strings.Contains(s, "2 working") {
		t.Errorf("envoySummary multi missing '2 working', got %q", s)
	}
	if !strings.Contains(s, "1 idle") {
		t.Errorf("envoySummary multi missing '1 idle', got %q", s)
	}
}

func TestMqSummaryLine(t *testing.T) {
	mq := status.MergeQueueInfo{Total: 5, Ready: 2, Claimed: 1, Failed: 1, Merged: 1}
	s := mqSummaryLine(mq)
	if !strings.Contains(s, "2 ready") {
		t.Errorf("mqSummaryLine missing '2 ready', got %q", s)
	}
	if !strings.Contains(s, "1 in progress") {
		t.Errorf("mqSummaryLine missing '1 in progress', got %q", s)
	}
	if !strings.Contains(s, "1 failed") {
		t.Errorf("mqSummaryLine missing '1 failed', got %q", s)
	}
	if !strings.Contains(s, "1 merged") {
		t.Errorf("mqSummaryLine missing '1 merged', got %q", s)
	}
}

func TestMqSummaryLineEmpty(t *testing.T) {
	mq := status.MergeQueueInfo{Total: 0}
	s := mqSummaryLine(mq)
	if !strings.Contains(s, "empty") {
		t.Errorf("mqSummaryLine empty = %q, should contain 'empty'", s)
	}
}

func TestScrollIndicator(t *testing.T) {
	// All visible — no indicator.
	if s := scrollIndicator(0, 10, 5); s != "" {
		t.Errorf("all visible: expected empty, got %q", s)
	}

	// At top, more below.
	s := scrollIndicator(0, 5, 10)
	if !strings.Contains(s, "1-5 of 10") || !strings.Contains(s, "↓") {
		t.Errorf("at top: expected '1-5 of 10 ↓', got %q", s)
	}

	// In middle, both above and below.
	s = scrollIndicator(3, 5, 10)
	if !strings.Contains(s, "4-8 of 10") || !strings.Contains(s, "↕") {
		t.Errorf("in middle: expected '4-8 of 10 ↕', got %q", s)
	}

	// At bottom, more above.
	s = scrollIndicator(5, 5, 10)
	if !strings.Contains(s, "6-10 of 10") || !strings.Contains(s, "↑") {
		t.Errorf("at bottom: expected '6-10 of 10 ↑', got %q", s)
	}
}

func TestEscUnfocusThenPop(t *testing.T) {
	wm := newWorldModel()
	wm.outpostLen = 2
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts

	// First esc: unfocus.
	wm, cmd := wm.update(keyMsg("esc"), nil)
	if cmd != nil {
		t.Error("first esc should not produce a command (just unfocus)")
	}
	if wm.hasFocus {
		t.Error("first esc should clear hasFocus")
	}

	// Second esc: pop back.
	_, cmd = wm.update(keyMsg("esc"), nil)
	if cmd == nil {
		t.Fatal("second esc should produce a pop command")
	}
	msg := cmd()
	if _, ok := msg.(popMsg); !ok {
		t.Fatalf("expected popMsg, got %T", msg)
	}
}

func TestProcessGridRendersThreeColumns(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	procs := []processEntry{
		{"Prefect", true, true},
		{"Chronicle", true, false},
		{"Ledger", false, false},
		{"Broker", true, true},
	}

	var b strings.Builder
	wm.renderProcessGrid(&b, procs, false)
	output := b.String()

	// Should contain all process names.
	for _, p := range procs {
		if !strings.Contains(output, p.name) {
			t.Errorf("process grid missing %q", p.name)
		}
	}

	// With 5 processes and 3 columns, should be 2 lines.
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 grid rows, got %d: %v", len(lines), lines)
	}
}

func TestDefaultViewFitsTerminal(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:   "testworld",
		Prefect: status.PrefectInfo{Running: true, PID: 42},
		Forge:   status.ForgeInfo{Running: true},
		Agents: []status.AgentStatus{
			{Name: "A", State: "working", SessionAlive: true},
			{Name: "B", State: "idle"},
			{Name: "C", State: "idle"},
			{Name: "D", State: "working", SessionAlive: true},
		},
		Envoys: []status.EnvoyStatus{
			{Name: "Scout", State: "working", SessionAlive: true},
		},
		MergeQueue:    status.MergeQueueInfo{Total: 3, Ready: 1, Claimed: 1, Merged: 1},
		MergeRequests: []status.MergeRequestInfo{{ID: "mr-1", Phase: "ready"}, {ID: "mr-2", Phase: "claimed"}, {ID: "mr-3", Phase: "merged"}},
		Summary:       status.Summary{Total: 4, Working: 2, Idle: 2},
	}
	wm.updateData(data)

	// Default view: both agent sections always expanded as tables.
	output := wm.view(data, time.Now(), 0, nil, false)
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	// With always-expanded tables for both sections, more lines expected.
	if len(lines) > 40 {
		t.Errorf("default view should fit in terminal height, got %d lines", len(lines))
	}
}

func TestScrollFollowsCursor(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 30 // small height to trigger viewport limits
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts
	wm.outpostLen = 30 // more than viewport

	// Move cursor down past viewport.
	vpHeight := wm.agentSectionViewport()
	for i := 0; i < vpHeight+2; i++ {
		wm, _ = wm.update(keyMsg("j"), nil)
	}

	// outpostScroll should have adjusted.
	if wm.outpostScroll == 0 {
		t.Error("outpostScroll should have increased when cursor moved past viewport")
	}
	cur := wm.cursor(wm.focusedSection)
	if cur < wm.outpostScroll || cur >= wm.outpostScroll+vpHeight {
		t.Errorf("cursor %d should be within scroll window [%d, %d)", cur, wm.outpostScroll, wm.outpostScroll+vpHeight)
	}
}

func TestUpDownNoOpWithoutFocus(t *testing.T) {
	wm := newWorldModel()
	wm.outpostLen = 5
	wm.hasFocus = false

	// Down should be no-op without focus.
	wm, _ = wm.update(keyMsg("j"), nil)
	if wm.outpostCursor != 0 {
		t.Errorf("cursor should remain 0 without focus, got %d", wm.outpostCursor)
	}

	// Up should be no-op without focus.
	wm, _ = wm.update(keyMsg("k"), nil)
	if wm.outpostCursor != 0 {
		t.Errorf("cursor should remain 0 without focus, got %d", wm.outpostCursor)
	}
}

func TestEnterNoOpWithoutFocus(t *testing.T) {
	wm := newWorldModel()
	wm.outpostLen = 1
	wm.hasFocus = false

	data := &status.WorldStatus{
		World: "testworld",
		Agents: []status.AgentStatus{
			{Name: "Toast", State: "working", SessionAlive: true},
		},
	}

	// Enter should be no-op without focus.
	_, cmd := wm.update(keyMsg("enter"), data)
	if cmd != nil {
		t.Error("enter without focus should be no-op")
	}
}

// --- Peek mode tests ---

func TestPeekModelNavigation(t *testing.T) {
	pm := newPeekModel(nil, "")
	pm.width = 120
	pm.height = 40

	items := []peekItem{
		{name: "Toast", sessionName: "sol-dev-Toast", category: "Outposts", state: "working", alive: true, peekable: true},
		{name: "Vega", sessionName: "sol-dev-Vega", category: "Outposts", state: "idle", alive: false, peekable: false},
		{name: "Forge", sessionName: "sol-dev-forge", category: "Processes", state: "alive", alive: true, peekable: true},
	}
	pm.enter(peekMsg{items: items, initialCursor: 0, fromView: viewWorld, world: "dev"})

	if pm.cursor != 0 {
		t.Errorf("initial cursor should be 0, got %d", pm.cursor)
	}

	// Move down.
	pm, _ = pm.update(keyMsg("j"))
	if pm.cursor != 1 {
		t.Errorf("cursor after down should be 1, got %d", pm.cursor)
	}

	// Move down again.
	pm, _ = pm.update(keyMsg("j"))
	if pm.cursor != 2 {
		t.Errorf("cursor after second down should be 2, got %d", pm.cursor)
	}

	// Move down past end (should stay at 2).
	pm, _ = pm.update(keyMsg("j"))
	if pm.cursor != 2 {
		t.Errorf("cursor past end should stay at 2, got %d", pm.cursor)
	}

	// Move up.
	pm, _ = pm.update(keyMsg("k"))
	if pm.cursor != 1 {
		t.Errorf("cursor after up should be 1, got %d", pm.cursor)
	}
}

func TestPeekModelAttachAlive(t *testing.T) {
	pm := newPeekModel(nil, "")
	pm.width = 120
	pm.height = 40

	items := []peekItem{
		{name: "Toast", sessionName: "sol-dev-Toast", category: "Outposts", state: "working", alive: true, peekable: true},
	}
	pm.enter(peekMsg{items: items, initialCursor: 0, fromView: viewWorld, world: "dev"})

	_, cmd := pm.update(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("enter on alive item should produce a command")
	}
	msg := cmd()
	attach, ok := msg.(attachMsg)
	if !ok {
		t.Fatalf("expected attachMsg, got %T", msg)
	}
	if attach.sessionName != "sol-dev-Toast" {
		t.Errorf("attachMsg.sessionName = %q, want %q", attach.sessionName, "sol-dev-Toast")
	}
}

func TestPeekModelAttachDead(t *testing.T) {
	pm := newPeekModel(nil, "")
	pm.width = 120
	pm.height = 40

	items := []peekItem{
		{name: "Vega", sessionName: "sol-dev-Vega", category: "Outposts", state: "idle", alive: false, peekable: false},
	}
	pm.enter(peekMsg{items: items, initialCursor: 0, fromView: viewWorld, world: "dev"})

	_, cmd := pm.update(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("enter on dead item should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(noSessionMsg); !ok {
		t.Fatalf("expected noSessionMsg, got %T", msg)
	}
}

func TestPeekModelEscPops(t *testing.T) {
	pm := newPeekModel(nil, "")
	pm.width = 120
	pm.height = 40

	items := []peekItem{
		{name: "Toast", sessionName: "sol-dev-Toast", category: "Outposts", state: "working", alive: true, peekable: true},
	}
	pm.enter(peekMsg{items: items, initialCursor: 0, fromView: viewWorld, world: "dev"})

	_, cmd := pm.update(keyMsg("esc"))
	if cmd == nil {
		t.Fatal("esc should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(peekPopMsg); !ok {
		t.Fatalf("expected peekPopMsg, got %T", msg)
	}
}

func TestPeekModelViewRendersItems(t *testing.T) {
	pm := newPeekModel(nil, "")
	pm.width = 120
	pm.height = 40

	items := []peekItem{
		{name: "Toast", sessionName: "sol-dev-Toast", category: "Outposts", state: "working", alive: true, peekable: true},
		{name: "Vega", sessionName: "sol-dev-Vega", category: "Outposts", state: "idle", alive: false, peekable: false},
		{name: "Forge", sessionName: "sol-dev-forge", category: "Processes", state: "alive", alive: true, peekable: true},
	}
	pm.enter(peekMsg{items: items, initialCursor: 0, fromView: viewWorld, world: "dev"})

	feedView := dimStyle.Render(strings.Repeat("─", 120)) + "\n" + dimStyle.Render("  No recent activity") + "\n"
	output := pm.view(feedView)

	// Should contain category headers and item names.
	checks := []string{
		"Outposts",
		"Toast",
		"Vega",
		"Processes",
		"Forge",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("peek view missing %q", check)
		}
	}

	// Should contain footer.
	if !strings.Contains(output, "esc back") {
		t.Error("peek view should contain footer with 'esc back'")
	}
}

func TestPeekModelViewDeadSession(t *testing.T) {
	pm := newPeekModel(nil, "")
	pm.width = 120
	pm.height = 40

	items := []peekItem{
		{name: "Vega", sessionName: "sol-dev-Vega", category: "Outposts", state: "idle", alive: false, peekable: false},
	}
	pm.enter(peekMsg{items: items, initialCursor: 0, fromView: viewWorld, world: "dev"})

	feedView := dimStyle.Render(strings.Repeat("─", 120)) + "\n" + dimStyle.Render("  No recent activity") + "\n"
	output := pm.view(feedView)

	if !strings.Contains(output, "No active session") {
		t.Error("peek view should show 'No active session' for dead items")
	}
}

func TestBuildWorldPeekItems(t *testing.T) {
	data := &status.WorldStatus{
		World: "testworld",
		Agents: []status.AgentStatus{
			{Name: "Toast", State: "working", SessionAlive: true},
			{Name: "Vega", State: "idle", SessionAlive: false},
		},
		Envoys: []status.EnvoyStatus{
			{Name: "Scout", State: "working", SessionAlive: true},
		},
		Forge:    status.ForgeInfo{Running: true, PID: 12345},
		Sentinel: status.SentinelInfo{Running: true, PID: 200, PatrolCount: 5},
	}

	items := buildWorldPeekItems(data)

	if len(items) < 5 {
		t.Fatalf("expected at least 5 items, got %d", len(items))
	}

	// Verify categories.
	categories := make(map[string]int)
	for _, item := range items {
		categories[item.category]++
	}
	if categories["Outposts"] != 2 {
		t.Errorf("expected 2 Outposts, got %d", categories["Outposts"])
	}
	if categories["Envoys"] != 1 {
		t.Errorf("expected 1 Envoy, got %d", categories["Envoys"])
	}
	if categories["Processes"] < 2 {
		t.Errorf("expected at least 2 Processes, got %d", categories["Processes"])
	}

	// Verify first item.
	if items[0].name != "Toast" || items[0].sessionName != "sol-testworld-Toast" {
		t.Errorf("first item = %+v, want Toast with sol-testworld-Toast", items[0])
	}
}

func TestPeekModelInitialCursor(t *testing.T) {
	pm := newPeekModel(nil, "")
	pm.width = 120
	pm.height = 40

	items := []peekItem{
		{name: "Toast", category: "Outposts"},
		{name: "Vega", category: "Outposts"},
		{name: "Forge", category: "Processes"},
	}
	pm.enter(peekMsg{items: items, initialCursor: 2, fromView: viewWorld, world: "dev"})

	if pm.cursor != 2 {
		t.Errorf("initial cursor should be 2, got %d", pm.cursor)
	}
}

func TestBuildWorldPeekItemsForgeSessionName(t *testing.T) {
	data := &status.WorldStatus{
		World:    "myworld",
		Forge:    status.ForgeInfo{Running: true},
		Sentinel: status.SentinelInfo{Running: true, PID: 200},
	}

	items := buildWorldPeekItems(data)

	// Find forge item.
	var forgeItem *peekItem
	for i := range items {
		if items[i].name == "Forge" {
			forgeItem = &items[i]
			break
		}
	}
	if forgeItem == nil {
		t.Fatal("expected Forge item in peek items")
	}

	// Forge should use the merge session, not the forge process session.
	expectedSess := "sol-myworld-forge-merge"
	if forgeItem.sessionName != expectedSess {
		t.Errorf("Forge sessionName = %q, want %q", forgeItem.sessionName, expectedSess)
	}
	if !forgeItem.isForge {
		t.Error("Forge item should have isForge = true")
	}
	if forgeItem.source != "forge" {
		t.Errorf("Forge item source = %q, want %q", forgeItem.source, "forge")
	}
	if !forgeItem.peekable {
		t.Error("Forge item should be peekable")
	}
}

func TestPeekModelForgeView(t *testing.T) {
	t.Run("idle", func(t *testing.T) {
		pm := newPeekModel(nil, "")
		pm.width = 120
		pm.height = 40

		items := []peekItem{
			{name: "Forge", sessionName: "sol-dev-forge-merge", category: "Processes",
				state: "idle", alive: false, peekable: true, isForge: true, source: "forge"},
		}
		pm.enter(peekMsg{items: items, initialCursor: 0, fromView: viewWorld, world: "dev"})

		if !pm.selectedIsForge() {
			t.Error("selected item should be forge")
		}

		// Set forge info for idle state display.
		pm.forgeInfo = &status.ForgeInfo{
			Status:      "idle",
			QueueDepth:  3,
			MergesTotal: 42,
			LastMerge:   "5m",
		}

		feedView := dimStyle.Render(strings.Repeat("─", 120)) + "\n"
		output := pm.view(feedView)

		// Standard layout: sidebar (with item list) + capture panel (with idle info).
		checks := []string{
			"Forge",
			"No active merge session",
			"Last merge: 5m ago",
			"Queue: 3 ready",
			"Total merges: 42",
		}
		for _, check := range checks {
			if !strings.Contains(output, check) {
				t.Errorf("forge peek view missing %q", check)
			}
		}

		// Should use standard layout with sidebar separator.
		if !strings.Contains(output, "│") {
			t.Error("forge peek should use standard sidebar layout with separator")
		}
	})

	t.Run("active_merge", func(t *testing.T) {
		pm := newPeekModel(nil, "")
		pm.width = 120
		pm.height = 40

		items := []peekItem{
			{name: "Forge", sessionName: "sol-dev-forge-merge", category: "Processes",
				state: "merging", alive: true, peekable: true, isForge: true, source: "forge"},
		}
		pm.enter(peekMsg{items: items, initialCursor: 0, fromView: viewWorld, world: "dev"})

		// Simulate captured merge output.
		pm.capture = "Merging branch feature/foo into main..."

		feedView := dimStyle.Render(strings.Repeat("─", 120)) + "\n"
		output := pm.view(feedView)

		// Should show merge agent terminal output in capture panel.
		if !strings.Contains(output, "Merging branch feature/foo") {
			t.Error("forge peek should show merge agent capture output")
		}

		// Standard layout with sidebar.
		if !strings.Contains(output, "│") {
			t.Error("forge peek should use standard sidebar layout")
		}
	})
}

func TestPeekModelForgeFeedSync(t *testing.T) {
	pm := newPeekModel(nil, "")
	pm.width = 120
	pm.height = 40

	items := []peekItem{
		{name: "Toast", sessionName: "sol-dev-Toast", category: "Outposts",
			state: "working", alive: true, peekable: true},
		{name: "Forge", sessionName: "sol-dev-forge-merge", category: "Processes",
			state: "alive", alive: true, peekable: true, isForge: true, source: "forge"},
	}
	pm.enter(peekMsg{items: items, initialCursor: 0, fromView: viewWorld, world: "dev"})

	// Initially on Toast, no forge feed.
	if pm.forgeFeed != nil {
		t.Error("forge feed should be nil when not on forge item")
	}

	// Move to forge item.
	pm, _ = pm.update(keyMsg("j"))
	if pm.forgeFeed != nil {
		// Without solHome, no forge feed is created (needs solHome).
		// That's correct — the feed can't load without a real solHome.
	}

	// With solHome set, moving to forge should initialize feed.
	pm.solHome = "/tmp/test-sol"
	pm.syncForgeFeed()
	if pm.forgeFeed == nil {
		t.Error("forge feed should be initialized when on forge item with solHome")
	}

	// Move back to non-forge item.
	pm, _ = pm.update(keyMsg("k"))
	if pm.forgeFeed != nil {
		t.Error("forge feed should be cleared when moving off forge item")
	}
}

func TestPeekModelSourceFeedSync(t *testing.T) {
	pm := newPeekModel(nil, "")
	pm.width = 120
	pm.height = 40

	items := []peekItem{
		{name: "Toast", sessionName: "sol-dev-Toast", category: "Outposts",
			state: "working", alive: true, peekable: true},
		{name: "Consul", category: "Processes",
			state: "alive", alive: true, peekable: false, source: "consul"},
		{name: "Chronicle", category: "Processes",
			state: "stopped", alive: false, peekable: false, source: "chronicle"},
	}
	pm.enter(peekMsg{items: items, initialCursor: 0, fromView: viewSphere})

	// Initially on Toast (peekable), no source feed.
	if pm.sourceFeed != nil {
		t.Error("source feed should be nil when on a peekable item")
	}

	// Move to Consul (non-peekable, has source) — without solHome, no feed.
	pm, _ = pm.update(keyMsg("j"))
	if pm.sourceFeed != nil {
		t.Log("source feed correctly nil without solHome")
	}

	// With solHome set, moving to source item should initialize feed.
	pm.solHome = "/tmp/test-sol"
	pm.syncSourceFeed()
	if pm.sourceFeed == nil {
		t.Error("source feed should be initialized when on source item with solHome")
	}

	// Move to Chronicle — should get a new source feed for "chronicle".
	pm, _ = pm.update(keyMsg("j"))
	if pm.sourceFeed == nil {
		t.Error("source feed should be initialized for Chronicle")
	}

	// Move back to peekable item — source feed should clear.
	pm, _ = pm.update(keyMsg("k"))
	pm, _ = pm.update(keyMsg("k"))
	if pm.sourceFeed != nil {
		t.Error("source feed should be cleared when moving to peekable item")
	}
}

func TestPeekModelSourceFeedRendering(t *testing.T) {
	pm := newPeekModel(nil, "/tmp/test-sol")
	pm.width = 120
	pm.height = 40

	items := []peekItem{
		{name: "Consul", category: "Processes",
			state: "alive", alive: true, peekable: false, source: "consul"},
	}
	pm.enter(peekMsg{items: items, initialCursor: 0, fromView: viewSphere})

	// Source feed is initialized but empty (no real events dir).
	if pm.sourceFeed == nil {
		t.Fatal("source feed should be initialized")
	}

	// Render capture — should show "No recent consul events" instead of "No active session".
	lines := pm.renderCapture(20)
	content := strings.Join(lines, "\n")

	if strings.Contains(content, "No active session") {
		t.Error("should not show 'No active session' for items with source feed")
	}
	if !strings.Contains(content, "No recent consul events") {
		t.Errorf("should show 'No recent consul events', got:\n%s", content)
	}
}

func TestPeekModelSourceFeedWithEvents(t *testing.T) {
	pm := newPeekModel(nil, "/tmp/test-sol")
	pm.width = 120
	pm.height = 40

	items := []peekItem{
		{name: "Prefect", category: "Processes",
			state: "alive", alive: true, peekable: false, source: "prefect"},
	}
	pm.enter(peekMsg{items: items, initialCursor: 0, fromView: viewSphere})

	// Manually inject events into the source feed.
	if pm.sourceFeed == nil {
		t.Fatal("source feed should be initialized")
	}
	pm.sourceFeed.events = []events.Event{
		{Timestamp: time.Now().Add(-2 * time.Minute), Type: "respawn", Actor: "prefect", Source: "prefect"},
		{Timestamp: time.Now().Add(-1 * time.Minute), Type: "patrol", Actor: "prefect", Source: "prefect"},
	}

	lines := pm.renderCapture(20)
	content := strings.Join(lines, "\n")

	if strings.Contains(content, "No recent prefect events") {
		t.Error("should not show 'No recent' when events exist")
	}
	if strings.Contains(content, "No active session") {
		t.Error("should not show 'No active session' for items with source feed")
	}
	// The events should be rendered (format: "HH:MM actor verb").
	if !strings.Contains(content, "prefect") {
		t.Errorf("should contain event actor 'prefect', got:\n%s", content)
	}
}

func TestBuildSpherePeekItemsCopiesSource(t *testing.T) {
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

	items := buildSpherePeekItems(sm)

	// Verify all items have source fields.
	expected := map[string]string{
		"Prefect":   "prefect",
		"Consul":    "consul",
		"Chronicle": "chronicle",
		"Ledger":    "ledger",
		"Broker":    "broker",
	}
	for _, item := range items {
		wantSource, ok := expected[item.name]
		if !ok {
			t.Errorf("unexpected item %q", item.name)
			continue
		}
		if item.source != wantSource {
			t.Errorf("item %q source = %q, want %q", item.name, item.source, wantSource)
		}
	}
}

func TestEventMatchesSource(t *testing.T) {
	tests := []struct {
		name   string
		ev     events.Event
		source string
		want   bool
	}{
		{
			name:   "exact source match",
			ev:     events.Event{Source: "forge", Actor: "forge"},
			source: "forge",
			want:   true,
		},
		{
			name:   "world-prefixed source match",
			ev:     events.Event{Source: "myworld/forge", Actor: "forge"},
			source: "forge",
			want:   true,
		},
		{
			name:   "actor match",
			ev:     events.Event{Source: "other", Actor: "forge"},
			source: "forge",
			want:   true,
		},
		{
			name:   "no match",
			ev:     events.Event{Source: "sentinel", Actor: "sentinel"},
			source: "forge",
			want:   false,
		},
		{
			name:   "partial source no match",
			ev:     events.Event{Source: "myforge", Actor: "other"},
			source: "forge",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eventMatchesSource(tt.ev, tt.source)
			if got != tt.want {
				t.Errorf("eventMatchesSource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorldViewSectionOrderingWithSummaries(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:      "testworld",
		Prefect:    status.PrefectInfo{Running: true, PID: 42},
		Forge:      status.ForgeInfo{Running: true},
		Agents:     []status.AgentStatus{{Name: "A", State: "idle"}},
		Envoys:     []status.EnvoyStatus{{Name: "E", State: "idle"}},
		MergeQueue: status.MergeQueueInfo{Total: 1, Ready: 1},
		Caravans:   []status.CaravanInfo{{ID: "c1", Name: "batch", TotalItems: 5}},
		Summary:    status.Summary{Total: 1, Idle: 1},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now(), 0, nil, false)

	// Verify order: Sphere Processes → World Processes → Outposts → Envoys → Caravans → Merge Queue
	sphereIdx := strings.Index(output, "Sphere Processes")
	worldProcIdx := strings.Index(output, "World Processes")
	outpostsIdx := strings.Index(output, "Outposts")
	envoysIdx := strings.Index(output, "Envoys")
	caravansIdx := strings.Index(output, "Caravans")
	mqIdx := strings.Index(output, "Merge Queue")

	if sphereIdx == -1 || worldProcIdx == -1 || outpostsIdx == -1 || envoysIdx == -1 || caravansIdx == -1 || mqIdx == -1 {
		t.Fatalf("missing sections in output:\n%s", output)
	}

	if sphereIdx >= worldProcIdx {
		t.Error("Sphere Processes should come before World Processes")
	}
	if worldProcIdx >= outpostsIdx {
		t.Error("World Processes should come before Outposts")
	}
	if outpostsIdx >= envoysIdx {
		t.Error("Outposts should come before Envoys")
	}
	if envoysIdx >= caravansIdx {
		t.Error("Envoys should come before Caravans")
	}
	if caravansIdx >= mqIdx {
		t.Error("Caravans should come before Merge Queue")
	}
}

// --- Confirmation overlay tests ---

func TestConfirmModelShowAndDismiss(t *testing.T) {
	var c confirmModel

	if c.active {
		t.Fatal("confirm should start inactive")
	}

	called := false
	c.show("Delete item?", "This cannot be undone.", func() tea.Msg {
		called = true
		return nil
	})

	if !c.active {
		t.Fatal("confirm should be active after show()")
	}
	if c.title != "Delete item?" {
		t.Errorf("title = %q, want %q", c.title, "Delete item?")
	}
	if c.detail != "This cannot be undone." {
		t.Errorf("detail = %q, want %q", c.detail, "This cannot be undone.")
	}

	c.dismiss()
	if c.active {
		t.Fatal("confirm should be inactive after dismiss()")
	}
	if called {
		t.Fatal("onYes should not have been called on dismiss")
	}
}

func TestConfirmModelUpdateYes(t *testing.T) {
	var c confirmModel
	executed := false
	c.show("Confirm?", "Detail.", func() tea.Msg {
		executed = true
		return nil
	})

	consumed, cmd := c.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !consumed {
		t.Fatal("y key should be consumed")
	}
	if cmd == nil {
		t.Fatal("y key should return the onYes command")
	}
	if c.active {
		t.Fatal("overlay should be dismissed after y")
	}

	// Execute the returned command.
	cmd()
	if !executed {
		t.Fatal("onYes command should have executed")
	}
}

func TestConfirmModelUpdateEnter(t *testing.T) {
	var c confirmModel
	executed := false
	c.show("Confirm?", "", func() tea.Msg {
		executed = true
		return nil
	})

	consumed, cmd := c.update(tea.KeyMsg{Type: tea.KeyEnter})
	if !consumed {
		t.Fatal("enter key should be consumed")
	}
	if cmd == nil {
		t.Fatal("enter key should return the onYes command")
	}
	if c.active {
		t.Fatal("overlay should be dismissed after enter")
	}

	cmd()
	if !executed {
		t.Fatal("onYes command should have executed")
	}
}

func TestConfirmModelUpdateNo(t *testing.T) {
	var c confirmModel
	c.show("Confirm?", "Detail.", func() tea.Msg {
		t.Fatal("onYes should not be called on n")
		return nil
	})

	consumed, cmd := c.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !consumed {
		t.Fatal("n key should be consumed")
	}
	if cmd != nil {
		t.Fatal("n key should not return a command")
	}
	if c.active {
		t.Fatal("overlay should be dismissed after n")
	}
}

func TestConfirmModelUpdateEsc(t *testing.T) {
	var c confirmModel
	c.show("Confirm?", "", func() tea.Msg {
		t.Fatal("onYes should not be called on esc")
		return nil
	})

	consumed, cmd := c.update(tea.KeyMsg{Type: tea.KeyEscape})
	if !consumed {
		t.Fatal("esc key should be consumed")
	}
	if cmd != nil {
		t.Fatal("esc should not return a command")
	}
	if c.active {
		t.Fatal("overlay should be dismissed after esc")
	}
}

func TestConfirmModelUpdateQ(t *testing.T) {
	var c confirmModel
	c.show("Confirm?", "", func() tea.Msg {
		t.Fatal("onYes should not be called on q")
		return nil
	})

	consumed, cmd := c.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !consumed {
		t.Fatal("q key should be consumed")
	}
	if cmd != nil {
		t.Fatal("q should not return a command")
	}
	if c.active {
		t.Fatal("overlay should be dismissed after q")
	}
}

func TestConfirmModelCapturesUnrelatedKeys(t *testing.T) {
	var c confirmModel
	c.show("Confirm?", "", func() tea.Msg { return nil })

	// Other keys should be consumed but do nothing.
	consumed, cmd := c.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if !consumed {
		t.Fatal("unrelated key should be consumed while overlay is active")
	}
	if cmd != nil {
		t.Fatal("unrelated key should not return a command")
	}
	if !c.active {
		t.Fatal("overlay should remain active on unrelated keys")
	}
}

func TestConfirmModelInactiveDoesNotConsume(t *testing.T) {
	var c confirmModel // not active

	consumed, cmd := c.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if consumed {
		t.Fatal("inactive overlay should not consume keys")
	}
	if cmd != nil {
		t.Fatal("inactive overlay should not return commands")
	}
}

func TestConfirmModelViewRendersContent(t *testing.T) {
	var c confirmModel
	c.show("Restart Nova?", "This will kill the session.", func() tea.Msg { return nil })

	output := c.view(80, 24)

	checks := []string{
		"Restart Nova?",
		"This will kill the session.",
		"y confirm",
		"n cancel",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("confirm view missing %q", check)
		}
	}
}

func TestConfirmModelViewInactive(t *testing.T) {
	var c confirmModel // not active

	output := c.view(80, 24)
	if output != "" {
		t.Errorf("inactive confirm view should return empty string, got %q", output)
	}
}

func TestConfirmModelViewWordWraps(t *testing.T) {
	var c confirmModel
	c.show("Title", "This is a really long detail message that should be word wrapped to multiple lines for readability.", func() tea.Msg { return nil })

	output := c.view(80, 30)

	if !strings.Contains(output, "Title") {
		t.Error("confirm view missing title")
	}
	if !strings.Contains(output, "word wrapped") {
		t.Error("confirm view missing detail text")
	}
}

func TestWordWrap(t *testing.T) {
	tests := []struct {
		input string
		width int
		want  int // expected line count
	}{
		{"short", 20, 1},
		{"this is a longer text that needs wrapping", 15, 4},
		{"", 20, 0},
		{"single", 100, 1},
	}

	for _, tt := range tests {
		result := wordWrap(tt.input, tt.width)
		if tt.want == 0 {
			if result != "" {
				t.Errorf("wordWrap(%q, %d) = %q, want empty", tt.input, tt.width, result)
			}
			continue
		}
		lines := strings.Split(result, "\n")
		if len(lines) != tt.want {
			t.Errorf("wordWrap(%q, %d) = %d lines, want %d", tt.input, tt.width, len(lines), tt.want)
		}
	}
}

func TestModelConfirmOverlayBlocksKeys(t *testing.T) {
	m := NewModel(Config{
		SOLHome: t.TempDir(),
	})
	m.ready = true
	m.width = 120
	m.height = 40

	// Activate the confirm overlay.
	m.confirm.show("Confirm?", "Detail.", func() tea.Msg { return nil })

	// 'r' should be consumed by confirm, not trigger a refresh.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	updated := result.(Model)
	if !updated.confirm.active {
		t.Error("confirm should still be active — r is not a dismiss key")
	}

	// '?' should be consumed too, not toggle help.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	updated = result.(Model)
	if updated.showHelp {
		t.Error("help should not be shown while confirm is active")
	}
}

func TestModelConfirmOverlayRendered(t *testing.T) {
	m := NewModel(Config{
		SOLHome: t.TempDir(),
	})
	m.ready = true
	m.width = 120
	m.height = 40

	m.confirm.show("Restart Agent?", "This is destructive.", func() tea.Msg { return nil })

	output := m.View()
	if !strings.Contains(output, "Restart Agent?") {
		t.Error("View() should render the confirm overlay when active")
	}
	if !strings.Contains(output, "This is destructive.") {
		t.Error("View() should render confirm detail")
	}
}

// --- Sphere process restart tests ---

func TestSphereProcessRestartMsgTriggersConfirmation(t *testing.T) {
	m := NewModel(Config{
		SOLHome: t.TempDir(),
	})
	m.ready = true
	m.width = 120
	m.height = 40

	// Send a restartProcessMsg.
	result, _ := m.Update(restartProcessMsg{processName: "Prefect"})
	updated := result.(Model)

	if !updated.confirm.active {
		t.Error("restartProcessMsg should activate the confirmation overlay")
	}
	if !strings.Contains(updated.confirm.title, "Restart Prefect?") {
		t.Errorf("confirm title should contain 'Restart Prefect?', got %q", updated.confirm.title)
	}
}

func TestSphereProcessRestartDoneErrorShowsOverlay(t *testing.T) {
	m := NewModel(Config{
		SOLHome: t.TempDir(),
	})
	m.ready = true
	m.width = 120
	m.height = 40

	// Simulate a restart failure.
	result, _ := m.Update(restartDoneMsg{processName: "Consul", err: fmt.Errorf("process exited")})
	updated := result.(Model)

	if !updated.confirm.active {
		t.Error("restartDoneMsg with error should show error overlay")
	}
	if !strings.Contains(updated.confirm.title, "failed") {
		t.Errorf("confirm title should mention failure, got %q", updated.confirm.title)
	}
}

func TestSphereProcessRestartDoneSuccessNoOverlay(t *testing.T) {
	m := NewModel(Config{
		SOLHome: t.TempDir(),
	})
	m.ready = true
	m.width = 120
	m.height = 40

	// Simulate a successful restart.
	result, _ := m.Update(restartDoneMsg{processName: "Chronicle", err: nil})
	updated := result.(Model)

	if updated.confirm.active {
		t.Error("successful restart should not show a confirmation overlay")
	}
}

func TestSphereRestartKeyOnProcessSection(t *testing.T) {
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

	// Focus on processes section.
	sm.hasFocus = true
	sm.focusedSection = sphereSectionProcesses
	sm.processCursor = 0 // Prefect

	// Press 'R' to restart.
	updated, cmd := sm.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}}, data)

	if cmd == nil {
		t.Fatal("pressing R on a process should return a command")
	}

	// Execute the command and verify it produces a restartProcessMsg.
	msg := cmd()
	rpMsg, ok := msg.(restartProcessMsg)
	if !ok {
		t.Fatalf("expected restartProcessMsg, got %T", msg)
	}
	if rpMsg.processName != "Prefect" {
		t.Errorf("expected processName 'Prefect', got %q", rpMsg.processName)
	}
	_ = updated
}

func TestSphereRestartKeyNotFocused(t *testing.T) {
	sm := newSphereModel()
	sm.width = 120
	sm.height = 40

	data := &status.SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: status.PrefectInfo{Running: true},
	}
	sm.updateData(data)

	// Not focused.
	sm.hasFocus = false

	// Press 'R' — should do nothing.
	_, cmd := sm.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}}, data)

	if cmd != nil {
		t.Error("R without focus should not produce a command")
	}
}

func TestSphereRestartKeyOnWorldsSection(t *testing.T) {
	sm := newSphereModel()
	sm.width = 120
	sm.height = 40

	data := &status.SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: status.PrefectInfo{Running: true},
		Worlds: []status.WorldSummary{
			{Name: "alpha", Health: "healthy"},
		},
	}
	sm.updateData(data)

	// Focus on worlds section.
	sm.hasFocus = true
	sm.focusedSection = sphereSectionWorlds

	// Press 'R' — should do nothing (R only works in processes section).
	_, cmd := sm.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}}, data)

	if cmd != nil {
		t.Error("R on worlds section should not produce a command")
	}
}

func TestSphereProcessMapCoversAllProcesses(t *testing.T) {
	// All 5 sphere processes should be in the map.
	expected := []string{"Prefect", "Consul", "Chronicle", "Ledger", "Broker"}
	for _, name := range expected {
		if _, ok := sphereProcessMap[name]; !ok {
			t.Errorf("sphereProcessMap missing %q", name)
		}
	}
}

func TestRestartSphereProcessUnknown(t *testing.T) {
	err := restartSphereProcess("/usr/bin/sol", "Unknown")
	if err == nil {
		t.Error("expected error for unknown process")
	}
	if !strings.Contains(err.Error(), "unknown sphere process") {
		t.Errorf("error should mention unknown process, got %q", err.Error())
	}
}

func TestWorldStartPopBackToSphere(t *testing.T) {
	// Starting with a world should allow popping back to sphere view.
	m := NewModel(Config{World: "myworld"})
	m.ready = true
	m.width = 120
	m.height = 40

	// Should start in world view.
	if m.activeView() != viewWorld {
		t.Fatalf("expected viewWorld, got %v", m.activeView())
	}
	// View stack should have sphere at the base.
	if len(m.viewStack) != 2 {
		t.Fatalf("viewStack length = %d, want 2", len(m.viewStack))
	}
	if m.viewStack[0] != viewSphere {
		t.Errorf("viewStack[0] = %v, want viewSphere", m.viewStack[0])
	}

	// Pop back to sphere.
	m2, _ := m.Update(popMsg{})
	model := m2.(Model)
	if model.activeView() != viewSphere {
		t.Errorf("after pop, activeView = %v, want viewSphere", model.activeView())
	}
	if model.world != "" {
		t.Errorf("after pop, world = %q, want empty", model.world)
	}
}

func TestWorldStartPopGathersSphereData(t *testing.T) {
	// When popping from world-started dash, refresh should gather sphere data.
	m := NewModel(Config{World: "myworld"})
	m.ready = true
	m.width = 120
	m.height = 40

	// Verify starting state: no sphere data.
	if m.sphereData != nil {
		t.Fatal("sphereData should be nil on start")
	}

	// Pop triggers refresh which would gather sphere data.
	m2, cmd := m.Update(popMsg{})
	model := m2.(Model)
	if model.activeView() != viewSphere {
		t.Fatalf("after pop, activeView = %v, want viewSphere", model.activeView())
	}
	// The refresh command should be produced.
	if cmd == nil {
		t.Fatal("pop should produce a refresh command")
	}
}

func TestWorldStartPopClearsFeedWorldFilter(t *testing.T) {
	// When popping from world-started dash, the feed world filter should clear.
	m := NewModel(Config{World: "myworld", SOLHome: t.TempDir()})

	// Feed should start with world filter.
	if m.feed.world != "myworld" {
		t.Fatalf("feed.world = %q, want %q", m.feed.world, "myworld")
	}

	// Pop back to sphere.
	m2, _ := m.Update(popMsg{})
	model := m2.(Model)

	// Feed world filter should be cleared.
	if model.feed.world != "" {
		t.Errorf("after pop, feed.world = %q, want empty", model.feed.world)
	}
}

func TestDefaultModelViewStackHasSphereBase(t *testing.T) {
	// Starting without a world should have sphere as the single view.
	m := NewModel(Config{})
	if len(m.viewStack) != 1 {
		t.Fatalf("viewStack length = %d, want 1", len(m.viewStack))
	}
	if m.viewStack[0] != viewSphere {
		t.Errorf("viewStack[0] = %v, want viewSphere", m.viewStack[0])
	}
}

// --- Caravan peek tests ---

func TestBuildCaravanPeekItems(t *testing.T) {
	caravans := []status.CaravanInfo{
		{ID: "car-1", Name: "deploy-batch", Status: "open", TotalItems: 5, ClosedItems: 2, DispatchedItems: 1},
		{ID: "car-2", Name: "cleanup", Status: "drydock", TotalItems: 3},
		{ID: "car-3", Name: "migration", Status: "open", TotalItems: 10, ClosedItems: 8, ReadyItems: 2},
	}

	items := buildCaravanPeekItems(caravans)

	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	// Active caravans come first.
	if items[0].name != "deploy-batch" {
		t.Errorf("items[0].name = %q, want deploy-batch", items[0].name)
	}
	if items[0].category != "Active" {
		t.Errorf("items[0].category = %q, want Active", items[0].category)
	}
	if !items[0].alive {
		t.Error("items[0].alive = false, want true (open caravan)")
	}
	if !items[0].isCaravan {
		t.Error("items[0].isCaravan = false, want true")
	}
	if items[0].caravanID != "car-1" {
		t.Errorf("items[0].caravanID = %q, want car-1", items[0].caravanID)
	}
	if items[0].peekable {
		t.Error("items[0].peekable = true, want false")
	}

	// Second active caravan.
	if items[1].name != "migration" {
		t.Errorf("items[1].name = %q, want migration", items[1].name)
	}
	if items[1].category != "Active" {
		t.Errorf("items[1].category = %q, want Active", items[1].category)
	}

	// Drydocked caravan comes last.
	if items[2].name != "cleanup" {
		t.Errorf("items[2].name = %q, want cleanup", items[2].name)
	}
	if items[2].category != "Drydocked" {
		t.Errorf("items[2].category = %q, want Drydocked", items[2].category)
	}
	if items[2].alive {
		t.Error("items[2].alive = true, want false (drydocked caravan)")
	}
}

func TestCaravanStateSummary(t *testing.T) {
	tests := []struct {
		name     string
		caravan  status.CaravanInfo
		contains []string
	}{
		{
			name:     "merged and in progress",
			caravan:  status.CaravanInfo{TotalItems: 5, ClosedItems: 2, DispatchedItems: 1},
			contains: []string{"2/5 merged", "1 in progress"},
		},
		{
			name:     "ready only",
			caravan:  status.CaravanInfo{TotalItems: 3, ReadyItems: 3},
			contains: []string{"3 ready"},
		},
		{
			name:     "empty caravan",
			caravan:  status.CaravanInfo{TotalItems: 0},
			contains: []string{"0 items"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := caravanStateSummary(tt.caravan)
			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf("caravanStateSummary = %q, missing %q", result, want)
				}
			}
		})
	}
}

func TestCaravanPeekDetailRendering(t *testing.T) {
	pm := newPeekModel(nil, "")
	pm.width = 120
	pm.height = 40

	caravans := []status.CaravanInfo{
		{
			ID: "car-1", Name: "deploy-batch", Status: "open",
			TotalItems: 3, ClosedItems: 1,
			Items: []status.CaravanItemDetail{
				{WritID: "sol-aaa1111122222222", World: "dev", Phase: 0, Status: "closed", Title: "Fix bug A"},
				{WritID: "sol-bbb1111122222222", World: "dev", Phase: 0, Status: "tethered", Assignee: "sol-dev/Nova", Title: "Implement feature B"},
				{WritID: "sol-ccc1111122222222", World: "dev", Phase: 1, Status: "open", Ready: true, Title: "Add tests for C"},
			},
		},
	}

	items := buildCaravanPeekItems(caravans)
	pm.enter(peekMsg{items: items, initialCursor: 0, fromView: viewWorld, world: "dev"})
	pm.caravanData = caravans

	// Render the detail panel.
	detail := pm.renderCaravanDetail(items[0], 30, 100)

	joined := strings.Join(detail, "\n")

	// Check header.
	if !strings.Contains(joined, "deploy-batch") {
		t.Error("detail missing caravan name")
	}

	// Check column headers.
	if !strings.Contains(joined, "WRIT") {
		t.Error("detail missing WRIT column header")
	}
	if !strings.Contains(joined, "STATUS") {
		t.Error("detail missing STATUS column header")
	}
	if !strings.Contains(joined, "ASSIGNEE") {
		t.Error("detail missing ASSIGNEE column header")
	}
	if !strings.Contains(joined, "TITLE") {
		t.Error("detail missing TITLE column header")
	}

	// Check item data.
	if !strings.Contains(joined, "Fix bug A") {
		t.Error("detail missing title 'Fix bug A'")
	}
	if !strings.Contains(joined, "Nova") {
		t.Error("detail missing assignee 'Nova' (should strip world prefix)")
	}
	if !strings.Contains(joined, "Add tests for C") {
		t.Error("detail missing title 'Add tests for C'")
	}
}

func TestCaravanPeekDetailNoData(t *testing.T) {
	pm := newPeekModel(nil, "")
	pm.width = 120
	pm.height = 40

	item := peekItem{
		name:      "missing-caravan",
		isCaravan: true,
		caravanID: "car-nonexistent",
	}

	// No caravanData set — should show "No caravan data".
	detail := pm.renderCaravanDetail(item, 20, 80)
	joined := strings.Join(detail, "\n")

	if !strings.Contains(joined, "No caravan data") {
		t.Error("detail should show 'No caravan data' when caravanData is nil")
	}
}

func TestWorldViewCaravanSubheadings(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:   "testworld",
		Prefect: status.PrefectInfo{Running: true, PID: 42},
		Caravans: []status.CaravanInfo{
			{ID: "car-1", Name: "active-batch", Status: "open", TotalItems: 5, ClosedItems: 2},
			{ID: "car-2", Name: "parked-batch", Status: "drydock", TotalItems: 3},
		},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now(), 0, nil, false)

	// Should have both subheadings when both active and drydocked exist.
	if !strings.Contains(output, "Active") {
		t.Error("world view missing 'Active' subheading")
	}
	if !strings.Contains(output, "Drydocked") {
		t.Error("world view missing 'Drydocked' subheading")
	}
	if !strings.Contains(output, "active-batch") {
		t.Error("world view missing active caravan name")
	}
	if !strings.Contains(output, "parked-batch") {
		t.Error("world view missing drydocked caravan name")
	}
}

func TestSphereViewCaravanSubheadings(t *testing.T) {
	sm := newSphereModel()
	sm.width = 120
	sm.height = 40

	data := &status.SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: status.PrefectInfo{Running: true, PID: 100},
		Caravans: []status.CaravanInfo{
			{ID: "car-1", Name: "active-batch", Status: "open", TotalItems: 5, ClosedItems: 2},
			{ID: "car-2", Name: "parked-batch", Status: "drydock", TotalItems: 3},
		},
	}
	sm.updateData(data)

	output := sm.view(data, time.Now(), 0, false)

	if !strings.Contains(output, "Active") {
		t.Error("sphere view missing 'Active' subheading")
	}
	if !strings.Contains(output, "Drydocked") {
		t.Error("sphere view missing 'Drydocked' subheading")
	}
}

func TestWorldCaravanSectionFocusable(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:   "testworld",
		Prefect: status.PrefectInfo{Running: true, PID: 42},
		Agents: []status.AgentStatus{
			{Name: "Toast", State: "idle"},
		},
		Caravans: []status.CaravanInfo{
			{ID: "car-1", Name: "batch-1", Status: "open", TotalItems: 5},
		},
	}
	wm.updateData(data)

	// Caravans should appear in available sections.
	sections := wm.availableSections()
	found := false
	for _, s := range sections {
		if s == sectionCaravans {
			found = true
			break
		}
	}
	if !found {
		t.Error("sectionCaravans not in availableSections when caravans exist")
	}

	// Test sectionLen and cursor for caravans.
	if wm.sectionLen(sectionCaravans) != 1 {
		t.Errorf("sectionLen(sectionCaravans) = %d, want 1", wm.sectionLen(sectionCaravans))
	}
	wm.setCursor(sectionCaravans, 0)
	if wm.cursor(sectionCaravans) != 0 {
		t.Errorf("cursor(sectionCaravans) = %d, want 0", wm.cursor(sectionCaravans))
	}
}

func TestSphereCaravanSectionFocusable(t *testing.T) {
	sm := newSphereModel()
	sm.width = 120
	sm.height = 40

	data := &status.SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: status.PrefectInfo{Running: true, PID: 100},
		Caravans: []status.CaravanInfo{
			{ID: "car-1", Name: "batch-1", Status: "open", TotalItems: 5},
		},
	}
	sm.updateData(data)

	// Tab should cycle through sections including caravans.
	sm.hasFocus = true
	sm.focusedSection = sphereSectionWorlds
	sm.cycleFocus(1)

	if sm.focusedSection != sphereSectionCaravans {
		t.Errorf("after cycle from worlds, focusedSection = %d, want sphereSectionCaravans(%d)",
			sm.focusedSection, sphereSectionCaravans)
	}
}

func TestCaravanPeekViewLayout(t *testing.T) {
	pm := newPeekModel(nil, "")
	pm.width = 120
	pm.height = 30

	caravans := []status.CaravanInfo{
		{
			ID: "car-1", Name: "deploy-batch", Status: "open",
			TotalItems: 2, ClosedItems: 1,
			Items: []status.CaravanItemDetail{
				{WritID: "sol-aaa111112222", World: "dev", Phase: 0, Status: "closed", Title: "Done task"},
				{WritID: "sol-bbb111112222", World: "dev", Phase: 0, Status: "tethered", Assignee: "Nova", Title: "Active task"},
			},
		},
	}

	items := buildCaravanPeekItems(caravans)
	pm.enter(peekMsg{items: items, initialCursor: 0, fromView: viewWorld, world: "dev"})
	pm.caravanData = caravans

	// Render the full caravan view.
	output := pm.viewCaravan()

	// Should have the sidebar separator.
	if !strings.Contains(output, "│") {
		t.Error("caravan peek view missing separator")
	}

	// Should have the footer.
	if !strings.Contains(output, "esc back") {
		t.Error("caravan peek view missing footer")
	}

	// Should NOT have the normal feed-based footer.
	if strings.Contains(output, "enter attach") {
		t.Error("caravan peek view should not have 'enter attach' footer")
	}
}

func TestSphereViewRendersInbox(t *testing.T) {
	sm := newSphereModel()
	sm.width = 120
	sm.height = 40

	data := &status.SphereStatus{
		SOLHome:   "/home/test/sol",
		Health:    "healthy",
		Prefect:   status.PrefectInfo{Running: true, PID: 1234},
		MailCount: 3,
		Escalations: &status.EscalationSummary{
			Total:      2,
			BySeverity: map[string]int{"high": 1, "medium": 1},
		},
	}
	sm.updateData(data)

	output := sm.view(data, time.Now(), 0, false)

	// Should show inbox with combined count (3 mail + 2 escalations = 5).
	if !strings.Contains(output, "Inbox: 5 items need attention") {
		t.Errorf("sphere view should show 'Inbox: 5 items need attention', got:\n%s", output)
	}
}

func TestSphereViewInboxSingular(t *testing.T) {
	sm := newSphereModel()
	sm.width = 120
	sm.height = 40

	data := &status.SphereStatus{
		SOLHome:   "/home/test/sol",
		Health:    "healthy",
		Prefect:   status.PrefectInfo{Running: true, PID: 1234},
		MailCount: 1,
	}
	sm.updateData(data)

	output := sm.view(data, time.Now(), 0, false)

	if !strings.Contains(output, "Inbox: 1 item needs attention") {
		t.Errorf("sphere view should show singular 'item needs attention', got:\n%s", output)
	}
}

func TestSphereViewInboxAbsentWhenZero(t *testing.T) {
	sm := newSphereModel()
	sm.width = 120
	sm.height = 40

	data := &status.SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: status.PrefectInfo{Running: true, PID: 1234},
	}
	sm.updateData(data)

	output := sm.view(data, time.Now(), 0, false)

	if strings.Contains(output, "Inbox") {
		t.Error("sphere view should not show Inbox when count is zero")
	}
}

func TestSphereViewRendersTokenSummary(t *testing.T) {
	sm := newSphereModel()
	sm.width = 120
	sm.height = 40

	data := &status.SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: status.PrefectInfo{Running: true, PID: 1234},
		Tokens: status.TokenInfo{
			InputTokens:  150000,
			OutputTokens: 45000,
			AgentCount:   5,
			CostUSD:      12.50,
		},
	}
	sm.updateData(data)

	output := sm.view(data, time.Now(), 0, false)

	checks := []string{
		"Tokens (24h)",
		"150K in",
		"45K out",
		"$12.50",
		"5 agents",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("sphere view token summary missing %q", check)
		}
	}
}

func TestSphereViewTokensAbsentWhenZero(t *testing.T) {
	sm := newSphereModel()
	sm.width = 120
	sm.height = 40

	data := &status.SphereStatus{
		SOLHome: "/home/test/sol",
		Health:  "healthy",
		Prefect: status.PrefectInfo{Running: true, PID: 1234},
	}
	sm.updateData(data)

	output := sm.view(data, time.Now(), 0, false)

	if strings.Contains(output, "Tokens") {
		t.Error("sphere view should not show Tokens section when there's no usage data")
	}
}

func TestWorldViewRendersTokens(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:   "testworld",
		Prefect: status.PrefectInfo{Running: true, PID: 42},
		Tokens: status.TokenInfo{
			InputTokens:  2500000,
			OutputTokens: 800000,
			AgentCount:   3,
			CostUSD:      7.25,
			RuntimeBreakdown: []status.RuntimeTokenInfo{
				{Runtime: "claude", InputTokens: 2000000, OutputTokens: 600000, CostUSD: 5.50},
				{Runtime: "gpt-4", InputTokens: 500000, OutputTokens: 200000, CostUSD: 1.75},
			},
		},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now(), 0, nil, false)

	checks := []string{
		"Tokens (24h)",
		"2.5M in",
		"800K out",
		"$7.25",
		"3 agents",
		"claude:",
		"gpt-4:",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("world view token section missing %q", check)
		}
	}
}

func TestWorldViewTokensAbsentWhenZero(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:   "testworld",
		Prefect: status.PrefectInfo{Running: true, PID: 42},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now(), 0, nil, false)

	if strings.Contains(output, "Tokens") {
		t.Error("world view should not show Tokens section when there's no usage data")
	}
}
