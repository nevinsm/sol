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

	output := sm.view(data, time.Now(), false)

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

	output := sm.view(data, time.Now(), false)

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

	output := sm.view(data, time.Now(), false)

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

	output := sm.view(data, time.Now(), false)

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

	output := sm.view(data, time.Now(), false)

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
	output := sm.view(nil, time.Time{}, false)
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
		Forge:     status.ForgeInfo{Running: true, SessionName: "sol-testworld-forge"},
		Sentinel:  status.SentinelInfo{Running: true, SessionName: "sol-testworld-sentinel"},
		Chronicle: status.ChronicleInfo{Running: true, SessionName: "sol-testworld-chronicle"},
		Governor:  status.GovernorInfo{Running: true, BriefAge: "5m"},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now(), false, nil)

	checks := []string{
		"World: testworld",
		"Sphere Processes",
		"World Processes",
		"Prefect",
		"pid 42",
		"Forge",
		"sol-testworld-forge",
		"Sentinel",
		"sol-testworld-sentinel",
		"Chronicle",
		"sol-testworld-chronicle",
		"Governor",
		"brief: 5m ago",
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

	output := wm.view(data, time.Now(), false, nil)

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
			{Name: "Scout", State: "working", SessionAlive: true, ActiveWrit: "sol-bbb", WorkTitle: "Design review", BriefAge: "45m"},
		},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now(), false, nil)

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

	output := wm.view(data, time.Now(), false, nil)

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

	output := wm.view(data, time.Now(), false, nil)

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

	output := wm.view(data, time.Now(), false, nil)

	if !strings.Contains(output, "empty") {
		t.Error("world view should show 'empty' for empty merge queue")
	}
}

func TestWorldViewNilData(t *testing.T) {
	wm := newWorldModel()
	output := wm.view(nil, time.Time{}, false, nil)
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

	output := wm.view(data, time.Now(), false, nil)

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

	output := sm.view(data, time.Now(), false)

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

	output := wm.view(data, time.Now(), false, nil)

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
	output := wm.view(data, time.Now(), false, nil)
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

	output := wm.view(data, time.Now(), false, nil)

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

	output := wm.view(data, time.Now(), false, nil)

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

	// Health changes.
	m.trackSphereHighlights(&status.SphereStatus{Health: "degraded"})
	if m.healthHighlight != highlightTTL {
		t.Errorf("healthHighlight should be %d after change, got %d", highlightTTL, m.healthHighlight)
	}

	// Decay.
	m.decayHighlights()
	if m.healthHighlight != highlightTTL-1 {
		t.Errorf("healthHighlight should decay by 1, got %d", m.healthHighlight)
	}

	m.decayHighlights()
	if m.healthHighlight != 0 {
		t.Errorf("healthHighlight should be 0 after full decay, got %d", m.healthHighlight)
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

	// State changes.
	m.trackWorldHighlights(&status.WorldStatus{
		World: "test",
		Agents: []status.AgentStatus{
			{Name: "Alpha", State: "working"},
		},
	})
	if _, ok := m.agentHighlights["Alpha"]; !ok {
		t.Error("state change should trigger highlight")
	}

	// Decay.
	m.decayHighlights()
	m.decayHighlights()
	if _, ok := m.agentHighlights["Alpha"]; ok {
		t.Error("highlight should be removed after full decay")
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
	output1 := sm.view(data, time.Now(), false)
	output2 := sm.view(data, time.Now(), true)
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

	highlights := map[string]int{"Alpha": 2}
	output := wm.view(data, time.Now(), false, highlights)
	if !strings.Contains(output, "Alpha") {
		t.Error("world view should still show agent name with highlights")
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
		Broker:    status.BrokerInfo{Running: true, Accounts: 3},
		Senate:    status.SenateInfo{Running: false},
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
	for _, name := range []string{"Ledger", "Senate"} {
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

	output := wm.view(data, time.Now(), false, nil)

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

	output := wm.view(data, time.Now(), false, nil)

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
	if d := formatForgeDetail(status.ForgeInfo{Running: true, SessionName: "sol-dev-forge"}); d != "sol-dev-forge" {
		t.Errorf("forge detail = %q, want %q", d, "sol-dev-forge")
	}
	if d := formatForgeDetail(status.ForgeInfo{Running: false}); d != "" {
		t.Errorf("stopped forge detail should be empty, got %q", d)
	}

	// Sentinel detail.
	if d := formatSentinelDetail(status.SentinelInfo{Running: true, SessionName: "sol-dev-sentinel"}); d != "sol-dev-sentinel" {
		t.Errorf("sentinel detail = %q, want %q", d, "sol-dev-sentinel")
	}

	// Chronicle detail.
	if d := formatChronicleDetail(status.ChronicleInfo{Running: true, SessionName: "chronicle-sess"}); d != "chronicle-sess" {
		t.Errorf("chronicle detail = %q, want %q", d, "chronicle-sess")
	}
	if d := formatChronicleDetail(status.ChronicleInfo{Running: true, PID: 456}); d != "pid 456" {
		t.Errorf("chronicle detail with PID = %q, want %q", d, "pid 456")
	}

	// Ledger detail.
	if d := formatLedgerDetail(status.LedgerInfo{Running: true, SessionName: "ledger-sess"}); d != "ledger-sess" {
		t.Errorf("ledger detail = %q, want %q", d, "ledger-sess")
	}
	if d := formatLedgerDetail(status.LedgerInfo{Running: true, PID: 789}); d != "pid 789" {
		t.Errorf("ledger detail with PID = %q, want %q", d, "pid 789")
	}

	// Broker detail.
	if d := formatBrokerDetail(status.BrokerInfo{Running: true, Accounts: 5}); d != "5 accounts" {
		t.Errorf("broker detail = %q, want %q", d, "5 accounts")
	}

	// Senate detail.
	if d := formatSenateDetail(status.SenateInfo{Running: true, SessionName: "senate-sess"}); d != "senate-sess" {
		t.Errorf("senate detail = %q, want %q", d, "senate-sess")
	}

	// Governor detail.
	if d := formatGovernorDetail(status.GovernorInfo{Running: true, BriefAge: "10m"}); d != "brief: 10m ago" {
		t.Errorf("governor detail = %q, want %q", d, "brief: 10m ago")
	}
	if d := formatGovernorDetail(status.GovernorInfo{Running: true}); d != "" {
		t.Errorf("governor detail without brief age should be empty, got %q", d)
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
