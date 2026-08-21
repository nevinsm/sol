package dash

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/jsoncontract"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
)

// --- Writs section rendering ---

func TestWorldViewWritsSectionHiddenWhenEmpty(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:   "testworld",
		Prefect: status.PrefectInfo{Running: true, PID: 42},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now(), 0, nil, false)
	if strings.Contains(output, "Writs (") {
		t.Error("world view should not show a Writs section when the backlog is empty")
	}
}

func TestWorldViewWritsSectionCollapsed(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:   "testworld",
		Prefect: status.PrefectInfo{Running: true, PID: 42},
		Writs: []status.WritSummary{
			{ID: "sol-aaa", Title: "fix things", Priority: 2, Kind: "code", CreatedAt: time.Now().Add(-time.Hour)},
			{ID: "sol-bbb", Title: "add feature", Priority: 1, Kind: "code", CreatedAt: time.Now()},
		},
	}
	wm.updateData(data)

	output := wm.view(data, time.Now(), 0, nil, false)
	if !strings.Contains(output, "Writs (2 open)") {
		t.Error("world view should show collapsed Writs count header")
	}
	// Collapsed mode should not show table column headers.
	if strings.Contains(output, "PRIORITY") {
		t.Error("collapsed Writs section should not show table columns")
	}
}

func TestWorldViewWritsSectionExpanded(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World:   "testworld",
		Prefect: status.PrefectInfo{Running: true, PID: 42},
		Writs: []status.WritSummary{
			{ID: "sol-aaa", Title: "fix things", Priority: 2, Kind: "code", CreatedAt: time.Now().Add(-time.Hour)},
			{ID: "sol-bbb", Title: "add feature", Priority: 1, Kind: "analysis", CreatedAt: time.Now()},
		},
	}
	wm.updateData(data)
	wm.hasFocus = true
	wm.focusedSection = sectionWrits

	output := wm.view(data, time.Now(), 0, nil, false)

	checks := []string{
		"Writs (2 open)",
		"ID", "PRIORITY", "KIND", "AGE", "TITLE",
		"sol-aaa", "fix things",
		"sol-bbb", "add feature", "analysis",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("expanded world view writs section missing %q", check)
		}
	}
}

// --- Peek ---

func TestWorldViewWritsEnterProducesWritPeekMsg(t *testing.T) {
	wm := newWorldModel()
	wm.writsLen = 1
	wm.hasFocus = true
	wm.focusedSection = sectionWrits

	data := &status.WorldStatus{
		World: "testworld",
		Writs: []status.WritSummary{
			{ID: "sol-aaa", Title: "fix things", Description: "the full description", Priority: 2, Kind: "code"},
		},
	}

	_, cmd := wm.update(keyMsg("enter"), data)
	if cmd == nil {
		t.Fatal("enter on a writ row should produce a command")
	}
	msg := cmd()
	peek, ok := msg.(peekMsg)
	if !ok {
		t.Fatalf("expected peekMsg, got %T", msg)
	}
	if len(peek.items) != 1 {
		t.Fatalf("expected 1 peek item, got %d", len(peek.items))
	}
	item := peek.items[0]
	if !item.isWrit || item.writID != "sol-aaa" || item.description != "the full description" {
		t.Errorf("unexpected peek item: %+v", item)
	}
}

func TestBuildWritPeekItems(t *testing.T) {
	writs := []status.WritSummary{
		{ID: "sol-aaa", Title: "fix things", Description: "desc one", Priority: 2, Kind: "code"},
		{ID: "sol-bbb", Title: "add feature", Description: "desc two", Priority: 1, Kind: "analysis"},
	}
	items := buildWritPeekItems(writs)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].writID != "sol-aaa" || items[0].description != "desc one" || !items[0].isWrit {
		t.Errorf("item 0 = %+v, unexpected", items[0])
	}
	if items[0].peekable || items[0].alive {
		t.Error("writ peek items should not be peekable/alive (no tmux session)")
	}
	if items[0].category != "Writs" {
		t.Errorf("category = %q, want %q", items[0].category, "Writs")
	}
}

// --- Cast action confirmation building ---

func TestHandleCastBuildsConfirmRequest(t *testing.T) {
	wm := newWorldModel()
	wm.hasFocus = true
	wm.focusedSection = sectionWrits
	wm.writsCursor = 0

	data := &status.WorldStatus{
		World: "testworld",
		Writs: []status.WritSummary{
			{ID: "sol-aaa", Title: "fix things", Priority: 2, Kind: "code"},
		},
	}

	_, cmd := wm.update(keyMsg("c"), data)
	if cmd == nil {
		t.Fatal("'c' on a focused writ row should produce a command")
	}
	msg := cmd()
	req, ok := msg.(requestCastMsg)
	if !ok {
		t.Fatalf("expected requestCastMsg, got %T", msg)
	}
	if req.world != "testworld" || req.writID != "sol-aaa" {
		t.Errorf("unexpected requestCastMsg: %+v", req)
	}
	if !strings.Contains(req.confirmTitle, "sol-aaa") {
		t.Errorf("confirmTitle = %q, should mention writ ID", req.confirmTitle)
	}
	if !strings.Contains(req.confirmDetail, "advisory") {
		t.Errorf("confirmDetail = %q, should note deps are advisory", req.confirmDetail)
	}
}

func TestHandleCastNoOpWhenNotFocused(t *testing.T) {
	wm := newWorldModel()
	wm.hasFocus = false
	wm.focusedSection = sectionWrits

	data := &status.WorldStatus{
		World: "testworld",
		Writs: []status.WritSummary{{ID: "sol-aaa", Title: "fix things"}},
	}

	_, cmd := wm.update(keyMsg("c"), data)
	if cmd != nil {
		t.Error("'c' without section focus should be a no-op")
	}
}

func TestHandleCastNoOpWrongSection(t *testing.T) {
	wm := newWorldModel()
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts

	data := &status.WorldStatus{
		World: "testworld",
		Agents: []status.AgentStatus{{Name: "Nova", State: "idle"}},
		Writs:  []status.WritSummary{{ID: "sol-aaa", Title: "fix things"}},
	}

	wm2, cmd := wm.handleCast(data)
	_ = wm2
	if cmd != nil {
		t.Error("handleCast should be a no-op unless the Writs section is focused")
	}
}

// --- wrapTextLines ---

func TestWrapTextLinesWrapsAtWidth(t *testing.T) {
	out := wrapTextLines("one two three four five", 10)
	for _, l := range out {
		if len(l) > 10 {
			t.Errorf("line %q exceeds width 10", l)
		}
	}
	joined := strings.Join(out, " ")
	for _, word := range []string{"one", "two", "three", "four", "five"} {
		if !strings.Contains(joined, word) {
			t.Errorf("wrapped output missing word %q: %v", word, out)
		}
	}
}

func TestWrapTextLinesPreservesBlankLines(t *testing.T) {
	out := wrapTextLines("first paragraph\n\nsecond paragraph", 40)
	if len(out) != 3 {
		t.Fatalf("expected 3 lines (para, blank, para), got %d: %v", len(out), out)
	}
	if out[1] != "" {
		t.Errorf("expected blank separator line, got %q", out[1])
	}
}

// --- renderWritDetail ---

func TestRenderWritDetailShowsDescription(t *testing.T) {
	pm := newPeekModel(nil, "")
	pm.width = 100
	pm.height = 30
	item := peekItem{name: "fix things", writID: "sol-aaa", isWrit: true, description: "do the thing carefully"}

	lines := pm.renderWritDetail(item, 10, 60)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "sol-aaa") {
		t.Error("detail should show writ ID in header")
	}
	if !strings.Contains(joined, "do the thing carefully") {
		t.Error("detail should show the description text")
	}
}

func TestRenderWritDetailEmptyDescription(t *testing.T) {
	pm := newPeekModel(nil, "")
	pm.width = 100
	pm.height = 30
	item := peekItem{name: "fix things", writID: "sol-aaa", isWrit: true, description: ""}

	lines := pm.renderWritDetail(item, 10, 60)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "no description") {
		t.Error("detail should show a placeholder for an empty description")
	}
}

func TestRenderWritDetailScrollClampsToContent(t *testing.T) {
	pm := newPeekModel(nil, "")
	pm.width = 100
	pm.height = 30
	// A long description that wraps to well more lines than fit in the panel.
	var words []string
	for i := 0; i < 200; i++ {
		words = append(words, "word")
	}
	item := peekItem{name: "long", writID: "sol-aaa", isWrit: true, description: strings.Join(words, " ")}

	// Scroll far past the end — should clamp instead of showing blank content.
	pm.writScroll = 100000
	lines := pm.renderWritDetail(item, 8, 60)
	nonEmpty := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty++
		}
	}
	if nonEmpty == 0 {
		t.Error("scrolling past the end should clamp to show the tail of the content, not go blank")
	}
}

// --- castCmd integration (mock session manager, real stores) ---

// castTestSessionMgr is a minimal dispatch.SessionManager fake for castCmd
// tests — unlike fakeHandoffSessionMgr (handoff_test.go), Exists tracks
// actual start/stop calls instead of hardcoding true, which matters here
// since dispatch.Cast/startup.Launch check Exists to decide whether a
// session is already running.
type castTestSessionMgr struct {
	started map[string]bool
	stopped map[string]bool
}

func newCastTestSessionMgr() *castTestSessionMgr {
	return &castTestSessionMgr{started: map[string]bool{}, stopped: map[string]bool{}}
}

func (m *castTestSessionMgr) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	m.started[name] = true
	m.stopped[name] = false
	return nil
}

func (m *castTestSessionMgr) Stop(name string, force bool) error {
	m.stopped[name] = true
	return nil
}

func (m *castTestSessionMgr) Exists(name string) bool { return m.started[name] && !m.stopped[name] }

func (m *castTestSessionMgr) Capture(name string, lines int) (string, error) { return "", nil }

func (m *castTestSessionMgr) Cycle(name, workdir, cmd string, env map[string]string, role, world string) error {
	return nil
}

func (m *castTestSessionMgr) NudgeSession(name string, message string) error { return nil }

func (m *castTestSessionMgr) WaitForIdle(name string, timeout time.Duration) error { return nil }

func (m *castTestSessionMgr) CountSessions(prefix string) (int, error) { return 0, nil }

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	allArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", allArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
	}
}

func setupCastTestStores(t *testing.T, world string) (*store.WorldStore, *store.SphereStore) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(dir+"/.store", 0o755); err != nil {
		t.Fatalf("create store dir: %v", err)
	}
	jsoncontract.WriteTestToken(t, dir)

	worldStore, err := store.OpenWorld(world)
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	t.Cleanup(func() { worldStore.Close() })

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	t.Cleanup(func() { sphereStore.Close() })

	// Managed-clone repo path: dispatch.ResolveSourceRepo prefers this over
	// world.toml's source_repo / CWD discovery.
	repoDir := config.RepoPath(world)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("create repo dir: %v", err)
	}
	runGitCmd(t, repoDir, "init")
	runGitCmd(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	return worldStore, sphereStore
}

func TestCastCmdHappyPath(t *testing.T) {
	world := "ember"
	worldStore, sphereStore := setupCastTestStores(t, world)

	writID, err := worldStore.CreateWrit("Add README", "Create a README file", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Alpha", world, "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	mgr := newCastTestSessionMgr()
	msg := castCmd(world, writID, mgr)()
	done, ok := msg.(castDoneMsg)
	if !ok {
		t.Fatalf("expected castDoneMsg, got %T", msg)
	}
	if done.err != nil {
		t.Fatalf("castCmd failed: %v", done.err)
	}
	if done.writID != writID {
		t.Errorf("writID = %q, want %q", done.writID, writID)
	}
	if done.agentName != "Alpha" {
		t.Errorf("agentName = %q, want %q", done.agentName, "Alpha")
	}

	// Verify the writ actually moved off the open backlog.
	item, err := worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("GetWrit: %v", err)
	}
	if item.Status != "tethered" {
		t.Errorf("writ status = %q, want tethered", item.Status)
	}
	if item.Assignee != world+"/Alpha" {
		t.Errorf("writ assignee = %q, want %q", item.Assignee, world+"/Alpha")
	}
}

func TestCastCmdSleepingWorldBlocked(t *testing.T) {
	world := "ember"
	worldStore, sphereStore := setupCastTestStores(t, world)

	writID, err := worldStore.CreateWrit("Add README", "desc", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Alpha", world, "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	// Write a world.toml marking the world sleeping.
	worldToml := "[world]\nsleeping = true\n"
	if err := os.WriteFile(config.WorldConfigPath(world), []byte(worldToml), 0o644); err != nil {
		t.Fatalf("write world.toml: %v", err)
	}

	mgr := newCastTestSessionMgr()
	msg := castCmd(world, writID, mgr)()
	done, ok := msg.(castDoneMsg)
	if !ok {
		t.Fatalf("expected castDoneMsg, got %T", msg)
	}
	if done.err == nil {
		t.Fatal("castCmd should refuse to dispatch into a sleeping world")
	}
	if !strings.Contains(done.err.Error(), "sleeping") {
		t.Errorf("error = %v, want it to mention sleeping", done.err)
	}
}

// --- Model wiring ---

func TestModelRequestCastMsgShowsConfirm(t *testing.T) {
	m := NewModel(Config{})
	updated, _ := m.Update(requestCastMsg{
		world:         "testworld",
		writID:        "sol-aaa",
		confirmTitle:  "Cast sol-aaa?",
		confirmDetail: "some detail",
	})
	mm := updated.(Model)
	if !mm.confirm.active {
		t.Fatal("requestCastMsg should activate the confirm overlay")
	}
	if mm.confirm.title != "Cast sol-aaa?" {
		t.Errorf("confirm title = %q, want %q", mm.confirm.title, "Cast sol-aaa?")
	}
}

func TestModelCastDoneMsgSetsFeedback(t *testing.T) {
	m := NewModel(Config{})
	updated, _ := m.Update(castDoneMsg{writID: "sol-aaa", agentName: "Nova"})
	mm := updated.(Model)
	if mm.worldView.restartFeedbackErr {
		t.Error("successful cast should not set the error flag")
	}
	if !strings.Contains(mm.worldView.restartFeedback, "sol-aaa") || !strings.Contains(mm.worldView.restartFeedback, "Nova") {
		t.Errorf("restartFeedback = %q, want it to mention writ and agent", mm.worldView.restartFeedback)
	}
}

func TestModelCastDoneMsgErrorSetsFeedback(t *testing.T) {
	m := NewModel(Config{})
	updated, _ := m.Update(castDoneMsg{writID: "sol-aaa", err: context.DeadlineExceeded})
	mm := updated.(Model)
	if !mm.worldView.restartFeedbackErr {
		t.Error("failed cast should set the error flag")
	}
	if !strings.Contains(mm.worldView.restartFeedback, "sol-aaa") {
		t.Errorf("restartFeedback = %q, want it to mention the writ ID", mm.worldView.restartFeedback)
	}
}
