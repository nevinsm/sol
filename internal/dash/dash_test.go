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

	output := sm.view(data, time.Time{})

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
	wm.outpostLen = 2

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

	output := sm.view(data, time.Now())

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

	// Press esc to pop back.
	_, cmd := wm.update(keyMsg("esc"), nil)
	if cmd == nil {
		t.Fatal("esc in world view should produce a command")
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
	wm.outpostLen = 2
	wm.envoyLen = 1
	wm.mrLen = 3

	// Start at outposts (default).
	if wm.focusedSection != sectionOutposts {
		t.Fatalf("initial focus should be outposts, got %d", wm.focusedSection)
	}

	// Tab to envoys.
	wm, _ = wm.update(tabKeyMsg(), nil)
	if wm.focusedSection != sectionEnvoys {
		t.Errorf("after tab, focus should be envoys, got %d", wm.focusedSection)
	}

	// Tab to merge queue.
	wm, _ = wm.update(tabKeyMsg(), nil)
	if wm.focusedSection != sectionMergeQueue {
		t.Errorf("after second tab, focus should be merge queue, got %d", wm.focusedSection)
	}

	// Tab wraps to outposts.
	wm, _ = wm.update(tabKeyMsg(), nil)
	if wm.focusedSection != sectionOutposts {
		t.Errorf("tab should wrap around to outposts, got %d", wm.focusedSection)
	}
}

func TestWorldViewSectionFocusReverseTab(t *testing.T) {
	wm := newWorldModel()
	wm.outpostLen = 2
	wm.envoyLen = 1
	wm.mrLen = 3

	// Shift-tab wraps to merge queue.
	wm, _ = wm.update(shiftTabKeyMsg(), nil)
	if wm.focusedSection != sectionMergeQueue {
		t.Errorf("shift-tab should wrap to merge queue, got %d", wm.focusedSection)
	}
}

func TestWorldViewPerSectionCursors(t *testing.T) {
	wm := newWorldModel()
	wm.outpostLen = 3
	wm.envoyLen = 2
	wm.mrLen = 4

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

	// Tab to MR queue and move cursor.
	wm, _ = wm.update(tabKeyMsg(), nil)
	wm, _ = wm.update(keyMsg("j"), nil)
	wm, _ = wm.update(keyMsg("j"), nil)
	if wm.mrCursor != 2 {
		t.Errorf("mr cursor should be 2, got %d", wm.mrCursor)
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

	output := wm.view(data, time.Now())

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

func TestWorldViewAttachProducesMsg(t *testing.T) {
	wm := newWorldModel()
	wm.outpostLen = 1

	data := &status.WorldStatus{
		World: "testworld",
		Agents: []status.AgentStatus{
			{Name: "Toast", State: "working", SessionAlive: true},
		},
	}

	// Press enter to attach.
	_, cmd := wm.update(keyMsg("enter"), data)
	if cmd == nil {
		t.Fatal("enter on live agent should produce a command")
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

	data := &status.WorldStatus{
		World: "testworld",
		Agents: []status.AgentStatus{
			{Name: "Toast", State: "idle", SessionAlive: false},
		},
	}

	// Press enter on dead agent.
	_, cmd := wm.update(keyMsg("enter"), data)
	if cmd == nil {
		t.Fatal("enter on dead agent should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(noSessionMsg); !ok {
		t.Fatalf("expected noSessionMsg, got %T", msg)
	}
}

func TestWorldViewAttachEnvoy(t *testing.T) {
	wm := newWorldModel()
	wm.outpostLen = 0
	wm.envoyLen = 1
	wm.focusedSection = sectionEnvoys

	data := &status.WorldStatus{
		World: "testworld",
		Envoys: []status.EnvoyStatus{
			{Name: "Scout", State: "working", SessionAlive: true},
		},
	}

	// Press enter to attach to envoy.
	_, cmd := wm.update(keyMsg("enter"), data)
	if cmd == nil {
		t.Fatal("enter on live envoy should produce a command")
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

	// Default focus is outposts — the "Outposts" header should use focus style.
	output := wm.view(data, time.Now())
	if !strings.Contains(output, "Outposts") {
		t.Error("world view should contain Outposts section")
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

	output := wm.view(data, time.Now())

	checks := []string{
		"q quit",
		"select",
		"tab section",
		"enter attach",
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

	output := wm.view(data, time.Now())

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
