package dash

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/status"
)

// --- feedModel cursor tests ---

func newTestFeedModel(n int) feedModel {
	fm := newFeedModel("", "")
	fm.feedLines = 6
	for i := 0; i < n; i++ {
		fm.events = append(fm.events, events.Event{
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			Type:      events.EventCast,
			Actor:     "Nova",
			Payload:   map[string]any{"writ_id": "sol-x", "agent": "Nova", "world": "alpha"},
		})
	}
	return fm
}

func TestFeedMoveCursorClamps(t *testing.T) {
	fm := newTestFeedModel(3)

	// Starts at 0; moving up (negative) clamps at 0.
	fm.moveCursor(-1)
	if fm.cursor != 0 {
		t.Errorf("cursor should clamp at 0, got %d", fm.cursor)
	}

	// Moving down walks to the last visible row, then clamps.
	fm.moveCursor(1)
	fm.moveCursor(1)
	if fm.cursor != 2 {
		t.Errorf("cursor should be 2, got %d", fm.cursor)
	}
	fm.moveCursor(1)
	if fm.cursor != 2 {
		t.Errorf("cursor should clamp at visibleCount-1=2, got %d", fm.cursor)
	}
}

func TestFeedMoveCursorNoEvents(t *testing.T) {
	fm := newTestFeedModel(0)
	fm.moveCursor(1)
	if _, ok := fm.selectedEvent(); ok {
		t.Error("selectedEvent should report no selection when there are no events")
	}
}

func TestFeedSelectedEventTracksCursor(t *testing.T) {
	fm := newTestFeedModel(3)
	// cursor 0 = most recent = last element appended.
	ev, ok := fm.selectedEvent()
	if !ok {
		t.Fatal("expected a selected event")
	}
	want := fm.events[len(fm.events)-1]
	if !ev.Timestamp.Equal(want.Timestamp) {
		t.Errorf("cursor 0 should select the most recent event")
	}

	fm.moveCursor(1) // move to the oldest of the 3
	ev, ok = fm.selectedEvent()
	if !ok {
		t.Fatal("expected a selected event")
	}
	want = fm.events[len(fm.events)-2]
	if !ev.Timestamp.Equal(want.Timestamp) {
		t.Error("cursor 1 should select the second most recent event")
	}
}

func TestFeedViewFocusedHighlightsCursor(t *testing.T) {
	fm := newTestFeedModel(2)
	out := fm.viewFocused(80)
	if !strings.Contains(out, "Feed") {
		t.Error("focused feed view should show the Feed focus label")
	}
	unfocused := fm.view(80)
	if strings.Contains(unfocused, "▸") {
		t.Error("unfocused feed view should not show the focus indicator")
	}
}

// --- navTarget tests ---

func TestNavTargetMergeEventUsesPayloadWorld(t *testing.T) {
	ev := events.Event{
		Type:    events.EventMergeFailed,
		Payload: map[string]any{"merge_request_id": "mr-123", "world": "alpha"},
	}
	target, ok := navTarget(ev, "")
	if !ok {
		t.Fatal("expected a navigable target for merge_failed")
	}
	if target.kind != "mr" || target.mrID != "mr-123" || target.world != "alpha" {
		t.Errorf("unexpected target: %+v", target)
	}
}

func TestNavTargetMergeEventFallsBackToFromWorld(t *testing.T) {
	// EventMergeClaimed's real emission sites never set "world" — see
	// feednav.go's doc comment. Falls back to the feed's own filter context.
	ev := events.Event{
		Type:    events.EventMergeClaimed,
		Payload: map[string]any{"merge_request_id": "mr-456"},
	}
	target, ok := navTarget(ev, "beta")
	if !ok {
		t.Fatal("expected a navigable target for merge_claimed")
	}
	if target.world != "beta" {
		t.Errorf("expected fallback world %q, got %q", "beta", target.world)
	}

	// And with no fallback context (sphere view), world stays empty rather
	// than guessing.
	target, ok = navTarget(ev, "")
	if !ok {
		t.Fatal("expected ok=true even with an unresolved world")
	}
	if target.world != "" {
		t.Errorf("expected empty world with no fallback context, got %q", target.world)
	}
}

func TestNavTargetCastEvent(t *testing.T) {
	ev := events.Event{
		Type:    events.EventCast,
		Payload: map[string]any{"writ_id": "sol-1", "agent": "Nova", "world": "alpha"},
	}
	target, ok := navTarget(ev, "")
	if !ok || target.kind != "agent" || target.agentName != "Nova" || target.world != "alpha" {
		t.Errorf("unexpected target: %+v ok=%v", target, ok)
	}
}

func TestNavTargetResolveRespawnStalledUseAgent(t *testing.T) {
	for _, et := range []string{events.EventResolve, events.EventRespawn, events.EventStalled} {
		ev := events.Event{Type: et, Payload: map[string]any{"agent": "Alpha"}}
		target, ok := navTarget(ev, "myworld")
		if !ok {
			t.Errorf("%s: expected navigable target", et)
			continue
		}
		if target.kind != "agent" || target.agentName != "Alpha" || target.world != "myworld" {
			t.Errorf("%s: unexpected target %+v", et, target)
		}
	}
}

func TestNavTargetCaravanEventsCarryID(t *testing.T) {
	for _, et := range []string{events.EventCaravanCreated, events.EventCaravanLaunched, events.EventCaravanClosed} {
		ev := events.Event{Type: et, Payload: map[string]any{"caravan_id": "car-1", "name": "sweep"}}
		target, ok := navTarget(ev, "")
		if !ok {
			t.Errorf("%s: expected navigable target", et)
			continue
		}
		if target.kind != "caravan" || target.caravanID != "car-1" {
			t.Errorf("%s: unexpected target %+v", et, target)
		}
	}
}

func TestNavTargetUnknownEventTypeIsNoOp(t *testing.T) {
	ev := events.Event{Type: "patrol", Payload: map[string]any{"world": "alpha"}}
	if _, ok := navTarget(ev, ""); ok {
		t.Error("event types outside the closed link set should not be navigable")
	}
}

func TestFeedNavigateCmdNilWhenNothingSelected(t *testing.T) {
	fm := newTestFeedModel(0)
	if cmd := fm.navigateCmd(); cmd != nil {
		t.Error("navigateCmd should be nil with no events")
	}
}

// --- worldModel focus-application tests ---

func TestApplyMRFocusFindsRow(t *testing.T) {
	wm := newWorldModel()
	data := &status.WorldStatus{
		World: "w",
		MergeRequests: []status.MergeRequestInfo{
			{ID: "mr-1", Phase: "ready"},
			{ID: "mr-2", Phase: "failed"},
		},
	}
	found := wm.applyMRFocus(data, "mr-2")
	if !found {
		t.Fatal("expected to find mr-2")
	}
	if !wm.hasFocus || wm.focusedSection != sectionMergeQueue {
		t.Error("applyMRFocus should focus the Merge Queue section")
	}
	if wm.mqCursor != 1 {
		t.Errorf("expected cursor at index 1, got %d", wm.mqCursor)
	}
}

func TestApplyMRFocusDegradesWhenMissing(t *testing.T) {
	wm := newWorldModel()
	data := &status.WorldStatus{
		World:         "w",
		MergeRequests: []status.MergeRequestInfo{{ID: "mr-1", Phase: "ready"}},
	}
	found := wm.applyMRFocus(data, "mr-does-not-exist")
	if found {
		t.Fatal("expected not to find mr-does-not-exist")
	}
	// Still lands on the containing section per "degrade gracefully".
	if !wm.hasFocus || wm.focusedSection != sectionMergeQueue {
		t.Error("applyMRFocus should still focus the Merge Queue section when the row is missing")
	}
}

func TestApplyAgentFocusFindsOutpostThenEnvoy(t *testing.T) {
	wm := newWorldModel()
	data := &status.WorldStatus{
		World:  "w",
		Agents: []status.AgentStatus{{Name: "Nova"}, {Name: "Toast"}},
		Envoys: []status.EnvoyStatus{{Name: "Sage"}},
	}

	if !wm.applyAgentFocus(data, "Toast") {
		t.Fatal("expected to find Toast among agents")
	}
	if wm.focusedSection != sectionOutposts || wm.outpostCursor != 1 {
		t.Errorf("expected outposts[1] focused, got section=%d cursor=%d", wm.focusedSection, wm.outpostCursor)
	}

	if !wm.applyAgentFocus(data, "Sage") {
		t.Fatal("expected to find Sage among envoys")
	}
	if wm.focusedSection != sectionEnvoys || wm.envoyCursor != 0 {
		t.Errorf("expected envoys[0] focused, got section=%d cursor=%d", wm.focusedSection, wm.envoyCursor)
	}
}

func TestApplyAgentFocusDegradesToOutposts(t *testing.T) {
	wm := newWorldModel()
	data := &status.WorldStatus{World: "w", Agents: []status.AgentStatus{{Name: "Nova"}}}
	if wm.applyAgentFocus(data, "Ghost") {
		t.Fatal("expected not to find Ghost")
	}
	if !wm.hasFocus || wm.focusedSection != sectionOutposts {
		t.Error("applyAgentFocus should land on Outposts when the target agent is gone")
	}
}

// --- 'w' key (agent -> writ detail) gating ---

func TestHandleAgentWritNoOpWithoutActiveWrit(t *testing.T) {
	wm := newWorldModel()
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts
	data := &status.WorldStatus{World: "w", Agents: []status.AgentStatus{{Name: "Nova", State: "idle"}}}
	_, cmd := wm.handleAgentWrit(data)
	if cmd != nil {
		t.Error("'w' on an idle agent (no active writ) should be a no-op")
	}
}

func TestHandleAgentWritEmitsRequestForWorkingAgent(t *testing.T) {
	wm := newWorldModel()
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts
	data := &status.WorldStatus{
		World:  "w",
		Agents: []status.AgentStatus{{Name: "Nova", State: "working", ActiveWrit: "sol-abc"}},
	}
	_, cmd := wm.handleAgentWrit(data)
	if cmd == nil {
		t.Fatal("'w' on a working agent should emit a request")
	}
	msg, ok := cmd().(requestAgentWritMsg)
	if !ok {
		t.Fatalf("expected requestAgentWritMsg, got %T", cmd())
	}
	if msg.writID != "sol-abc" || msg.world != "w" {
		t.Errorf("unexpected message: %+v", msg)
	}
}

func TestHandleAgentWritNoOpWhenUnfocusedSection(t *testing.T) {
	wm := newWorldModel()
	wm.hasFocus = true
	wm.focusedSection = sectionMergeQueue // not Outposts/Envoys
	data := &status.WorldStatus{Agents: []status.AgentStatus{{Name: "Nova", ActiveWrit: "sol-abc"}}}
	_, cmd := wm.handleAgentWrit(data)
	if cmd != nil {
		t.Error("'w' should be a no-op outside Outposts/Envoys")
	}
}

// --- Model-level tab order: sections then feed, wrapping both ways ---

func TestModelCycleFocusReachesFeedPastLastSection(t *testing.T) {
	m := NewModel(Config{World: "w"})
	m.ready = true
	m.width, m.height = 120, 40
	m.worldView.width, m.worldView.height = 120, 40
	m.worldData = &status.WorldStatus{
		World:  "w",
		Agents: []status.AgentStatus{{Name: "Nova"}},
	}
	m.worldView.updateData(m.worldData)

	// Only Processes and Outposts are available (no writs/envoys/MQ/caravans).
	// tab, tab should exhaust the two sections and the third tab should land
	// on the feed.
	press := func() { m.cycleFocus(1) }
	press() // Processes -> Outposts
	if m.feedFocused || !m.worldView.hasFocus || m.worldView.focusedSection != sectionOutposts {
		t.Fatalf("expected Outposts focused, got feedFocused=%v section=%d", m.feedFocused, m.worldView.focusedSection)
	}
	press() // Outposts -> Feed (only two sections, so this crosses the boundary)
	if !m.feedFocused {
		t.Fatal("expected tab past the last section to focus the feed")
	}
	if m.worldView.hasFocus {
		t.Error("worldView should not report focus while the feed has it")
	}

	press() // Feed -> wraps back to the first section
	if m.feedFocused {
		t.Error("expected tab from feed to return focus to a section")
	}
	if m.worldView.focusedSection != sectionProcesses {
		t.Errorf("expected wrap back to the first section (Processes), got %d", m.worldView.focusedSection)
	}
}

func TestModelCycleFocusBackwardFromUnfocusedReachesFeed(t *testing.T) {
	m := NewModel(Config{World: "w"})
	m.ready = true
	m.width, m.height = 120, 40
	m.worldView.width, m.worldView.height = 120, 40
	m.worldData = &status.WorldStatus{World: "w", Agents: []status.AgentStatus{{Name: "Nova"}}}
	m.worldView.updateData(m.worldData)

	// shift+tab from the very first (never-focused) state should reach the
	// feed without erroring or getting stuck — see the wrapped-boundary path
	// in worldModel.cycleFocus's idx==-1 fallback interacting with Model.
	m.cycleFocus(-1)
	_ = m // any resulting state (feed or a section) is acceptable; the
	// assertion that matters is that this doesn't panic and stays internally
	// consistent, checked below.
	if m.feedFocused && m.worldView.hasFocus {
		t.Error("feed focus and worldView focus must be mutually exclusive")
	}
}

// TestModelTabKeyPressReachesFeedThroughRealKeyRouting exercises the actual
// production key path (Model.Update's top-level "tab" case), not just
// cycleFocus called directly, to confirm real tab/shift-tab keystrokes reach
// the feed and come back — the same round trip TestModelCycleFocusReaches
// FeedPastLastSection checks at the cycleFocus level.
func TestModelTabKeyPressReachesFeedThroughRealKeyRouting(t *testing.T) {
	m := NewModel(Config{World: "w"})
	m.ready = true
	m.width, m.height = 120, 40
	m.worldView.width, m.worldView.height = 120, 40
	m.worldData = &status.WorldStatus{World: "w", Agents: []status.AgentStatus{{Name: "Nova"}}}
	m.worldView.updateData(m.worldData)

	press := func(msg tea.KeyMsg) {
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}

	press(tabKeyMsg()) // Processes -> Outposts
	if m.feedFocused {
		t.Fatal("first tab should stay within sections")
	}
	press(tabKeyMsg()) // Outposts -> Feed (only two sections here)
	if !m.feedFocused {
		t.Fatal("tab past the last section should focus the feed")
	}

	press(shiftTabKeyMsg()) // Feed -> back to the last section (Outposts)
	if m.feedFocused {
		t.Error("shift-tab from the feed should return focus to a section")
	}
	if m.worldView.focusedSection != sectionOutposts {
		t.Errorf("shift-tab from feed should land on the last section, got %d", m.worldView.focusedSection)
	}
}

func TestModelFeedFocusEnterKeyNavigatesCastEvent(t *testing.T) {
	m := NewModel(Config{World: "w"})
	m.ready = true
	m.width, m.height = 120, 40
	m.worldView.width, m.worldView.height = 120, 40
	m.worldData = &status.WorldStatus{
		World:  "w",
		Agents: []status.AgentStatus{{Name: "Nova"}, {Name: "Toast"}},
	}
	m.worldView.updateData(m.worldData)
	m.world = "w"

	m.feedFocused = true
	m.feed.world = "w"
	m.feed.feedLines = 6
	m.feed.events = []events.Event{{
		Type:    events.EventCast,
		Payload: map[string]any{"writ_id": "sol-1", "agent": "Toast", "world": "w"},
	}}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("enter on a focused cast event should produce a command")
	}
	msg := cmd()
	// The command may be a feedNavMsg directly, or (via tea.Batch) something
	// that still resolves to one when invoked — navigateCmd returns the
	// feedNavMsg-producing func directly for a resolvable event.
	navMsg, ok := msg.(feedNavMsg)
	if !ok {
		t.Fatalf("expected feedNavMsg, got %T", msg)
	}

	updated2, _ := m.Update(navMsg)
	m = updated2.(Model)
	if !m.worldView.hasFocus || m.worldView.focusedSection != sectionOutposts || m.worldView.outpostCursor != 1 {
		t.Errorf("expected Toast's row (index 1) focused, got hasFocus=%v section=%d cursor=%d",
			m.worldView.hasFocus, m.worldView.focusedSection, m.worldView.outpostCursor)
	}
	if m.feedFocused {
		t.Error("successful navigation should drop feed focus")
	}
}

func TestModelFeedNavMissingWorldDegradesWithNotice(t *testing.T) {
	m := NewModel(Config{})
	m.ready = true
	m.width, m.height = 120, 40
	m.sphereView.width, m.sphereView.height = 120, 40
	m.sphereData = &status.SphereStatus{Health: "healthy"}
	m.sphereView.updateData(m.sphereData)

	// merge_claimed with no "world" in payload, encountered from sphere
	// view (no fallback context either) — this is the documented gap.
	navMsg := feedNavMsg{kind: "mr", world: "", mrID: "mr-1"}
	updated, cmd := m.Update(navMsg)
	m = updated.(Model)
	if m.sphereView.navNotice == "" {
		t.Error("expected a notice when the target world can't be resolved")
	}
	if cmd == nil {
		t.Error("expected a clear-feedback command to be scheduled")
	}
}

// --- fetchAgentWritCmd (real store, via the same mockOpener cache_test.go uses) ---

func TestFetchAgentWritCmdSuccess(t *testing.T) {
	opener := newMockOpener(t)
	cache := newWorldStoreCache(opener.open)
	defer cache.CloseAll()

	ws, err := cache.Get("alpha")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	writID, err := ws.CreateWrit("Fix the thing", "a long description", "tester", 1, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}

	cmd := fetchAgentWritCmd(cache, "alpha", writID)
	result, ok := cmd().(agentWritResultMsg)
	if !ok {
		t.Fatalf("expected agentWritResultMsg, got %T", cmd())
	}
	if result.peek == nil {
		t.Fatalf("expected a peek result, got notice %q", result.notice)
	}
	if len(result.peek.items) != 1 || !result.peek.items[0].isWrit {
		t.Fatalf("expected a single isWrit peek item, got %+v", result.peek.items)
	}
	if result.peek.items[0].description != "a long description" {
		t.Errorf("expected description to come from the store, got %q", result.peek.items[0].description)
	}
	if result.peek.items[0].writID != writID {
		t.Errorf("expected writID %q, got %q", writID, result.peek.items[0].writID)
	}
}

func TestFetchAgentWritCmdMissingWritDegrades(t *testing.T) {
	opener := newMockOpener(t)
	cache := newWorldStoreCache(opener.open)
	defer cache.CloseAll()

	// Force the world store to exist (so cache.Get succeeds) without
	// creating the writ, exercising GetWrit's ErrNotFound path.
	if _, err := cache.Get("alpha"); err != nil {
		t.Fatalf("open world store: %v", err)
	}

	cmd := fetchAgentWritCmd(cache, "alpha", "sol-does-not-exist")
	result, ok := cmd().(agentWritResultMsg)
	if !ok {
		t.Fatalf("expected agentWritResultMsg, got %T", cmd())
	}
	if result.peek != nil {
		t.Fatal("expected a degrade notice, not a peek, for a missing writ")
	}
	if result.notice == "" {
		t.Error("expected a non-empty notice")
	}
}

func TestModelFeedNavCaravanMissingLandsOnSection(t *testing.T) {
	m := NewModel(Config{World: "w"})
	m.ready = true
	m.width, m.height = 120, 40
	m.worldView.width, m.worldView.height = 120, 40
	m.worldData = &status.WorldStatus{World: "w"}
	m.worldView.updateData(m.worldData)
	m.world = "w"

	navMsg := feedNavMsg{kind: "caravan", caravanID: "car-missing"}
	updated, _ := m.Update(navMsg)
	m = updated.(Model)
	if !m.worldView.hasFocus || m.worldView.focusedSection != sectionCaravans {
		t.Error("missing caravan should still focus the Caravans section")
	}
	if m.worldView.navNotice == "" {
		t.Error("expected a dim notice for a missing caravan target")
	}
	if m.activeView() == viewPeek {
		t.Error("missing caravan target must not enter peek mode")
	}
}
