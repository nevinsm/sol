package dash

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/style"
)

const refreshInterval = 3 * time.Second

// tokenRefreshWorld is the cadence for token queries in world detail view.
const tokenRefreshWorld = 15 * time.Second

// tokenRefreshSphere is the cadence for token queries in sphere view.
const tokenRefreshSphere = 30 * time.Second

// animInterval is the animation tick cadence (~30 FPS).
const animInterval = 33 * time.Millisecond

// highlightTickInterval is how often highlight levels decay (5 levels × 400ms = ~2s total fade).
const highlightTickInterval = 400 * time.Millisecond

// pulseFrames is the total frames in one pulse cycle (~1 second at 30 FPS).
const pulseFrames = 30

// Minimum terminal dimensions.
const (
	minTermWidth  = 80
	minTermHeight = 24
)

// viewMode tracks which view is currently active.
type viewMode int

const (
	viewSphere viewMode = iota
	viewWorld
	viewPeek
)

// Config holds dependencies for the dashboard, mirroring cmd/status.go.
type Config struct {
	SphereStore      sphereStore
	EscalationLister status.EscalationLister
	WorldOpener      func(string) (*store.WorldStore, error)
	SessionCheck     status.SessionChecker
	CaravanStore     caravanStore
	SessionMgr       *session.Manager

	// SOLHome is the runtime root directory for reading event feeds.
	SOLHome string

	// World is non-empty when starting in world detail view.
	World string
}

// sphereStore combines the interfaces the status package needs for sphere gathering.
type sphereStore interface {
	store.AgentReader
	store.WorldReader
	store.CaravanReader
	CountPending(recipient string) (int, error)
}

// caravanStore abstracts caravan queries.
type caravanStore interface {
	store.CaravanReader
}

// animTickMsg fires at ~30 FPS to drive visual animation state
// (spinners, highlight decay, pulse phase, refresh counter display).
type animTickMsg time.Time

// dataTickMsg triggers a data refresh (database queries, status gathering).
type dataTickMsg time.Time

// highlightTickMsg triggers highlight level decay.
type highlightTickMsg time.Time

// dataMsg delivers refreshed data to the model.
type dataMsg struct {
	sphere *status.SphereStatus
	world  *status.WorldStatus

	// tokensRefreshed indicates that token queries actually ran (not served from cache).
	tokensRefreshed bool

	// refreshErr carries the reason a refresh failed. Non-nil means the
	// request did not produce sphere/world data this tick. The model still
	// advances lastRefresh (so the UI doesn't freeze) but records the error
	// for the renderer to surface. Cleared on the next successful refresh.
	refreshErr error
}

// drillMsg signals that the sphere view wants to drill into a world.
type drillMsg struct {
	world string
}

// popMsg signals the world view wants to return to sphere.
type popMsg struct{}

// pendingWorldFocus carries a deferred cross-panel navigation target applied
// once fresh world data lands — e.g. a feed event fired from sphere view
// names a world that isn't the one currently displayed, so the MR/agent
// list to search isn't known until status.Gather returns. See
// handleFeedNav and the dataMsg case's pendingFocus handling.
type pendingWorldFocus struct {
	kind string // "mr" or "agent"
	id   string
}

// pendingFocusMissingNotice formats the dim degrade-gracefully notice shown
// when a deferred pendingWorldFocus target (see above) isn't found once the
// world data it was waiting on arrives.
func pendingFocusMissingNotice(kind, id string) string {
	switch kind {
	case "mr":
		return fmt.Sprintf("MR %s no longer exists", id)
	case "agent":
		return fmt.Sprintf("agent %s no longer exists", id)
	default:
		return fmt.Sprintf("%s no longer exists", id)
	}
}

// attachMsg signals that the world view wants to attach to an agent session.
type attachMsg struct {
	sessionName string
}

// attachDoneMsg fires when an agent tmux attach completes (user detached).
type attachDoneMsg struct {
	err error
}

// inboxMsg signals that the sphere or world view wants to suspend the
// dashboard and exec into `sol inbox`.
type inboxMsg struct{}

// inboxDoneMsg fires when the exec'd `sol inbox` process exits and control
// returns to the dashboard.
type inboxDoneMsg struct {
	err error
}

// noSessionMsg signals an inline "no active session" message.
// message is optional; when non-empty it replaces the default "no active session" text.
type noSessionMsg struct {
	message string
}

// restartProcessMsg signals a request to restart a sphere process.
type restartProcessMsg struct {
	processName string
}

// Model is the root Bubble Tea model for the dashboard.
type Model struct {
	// viewStack tracks navigation depth. Last element is the active view.
	viewStack []viewMode
	world     string // populated in world view mode
	config    Config
	ready     bool
	width     int
	height    int
	lastRefresh time.Time

	// lastRefreshError carries the most recent refresh failure for display
	// (ORCH-M5). Non-empty means the most recent refresh attempt did not
	// produce data; the renderer surfaces it so a stuck dashboard doesn't
	// keep showing "fresh" data forever. Cleared on next successful refresh.
	lastRefreshError string

	// Connection cache — keeps world stores open across refresh cycles.
	storeCache *worldStoreCache

	// Token query throttling.
	lastTokenRefresh time.Time
	cachedSphereTokens status.TokenInfo
	cachedWorldTokens  status.TokenInfo

	// Data.
	sphereData *status.SphereStatus
	worldData  *status.WorldStatus

	// Sub-views.
	sphereView sphereModel
	worldView  worldModel
	peekView   peekModel

	// Activity feed.
	feed feedModel

	// feedFocused is true when tab-cycling has moved focus onto the feed —
	// treated as one additional stop past the active view's last section
	// (see cycleFocus). Only meaningful in sphere/world view; peek mode
	// renders its own feed panel with no cursor.
	feedFocused bool

	// pendingFocus carries a cross-panel navigation target (from a feed
	// event or elsewhere) that couldn't be applied immediately because it
	// targets a world whose data hasn't loaded yet — see
	// pendingWorldFocus's doc comment and the dataMsg handler below.
	pendingFocus *pendingWorldFocus

	// Help overlay.
	showHelp bool

	// Dirty flag — prevents unnecessary re-renders at 30 FPS.
	// viewCache is a pointer so View() (value receiver) can write through it.
	dirty     bool
	viewCache *string

	// Confirmation overlay.
	confirm confirmModel

	// State-change highlight tracking (progressive fade).
	prevSphereHealth string
	prevWorldHealth  string
	healthHighlight  int // highlight level (5→0) for health emphasis

	// Agent state tracking for highlights.
	prevAgentStates map[string]string // agentName -> previous state
	agentHighlights map[string]int   // agentName -> highlight level (5→0)

	// Animation pulse phase — incremented on each animTickMsg, wraps at pulseFrames.
	pulsePhase int
}

// NewModel creates a dashboard model. If world is empty, starts in sphere view.
func NewModel(cfg Config) Model {
	viewCache := ""
	// Always seed sphere as the base of the view stack so the user can
	// navigate back to it even when starting directly in world view.
	m := Model{
		viewStack:       []viewMode{viewSphere},
		world:           cfg.World,
		config:          cfg,
		storeCache:      newWorldStoreCache(cfg.WorldOpener),
		feed:            newFeedModel(cfg.SOLHome, cfg.World),
		prevAgentStates: make(map[string]string),
		agentHighlights: make(map[string]int),
		viewCache:       &viewCache,
	}

	if cfg.World != "" {
		m.viewStack = append(m.viewStack, viewWorld)
	}

	m.sphereView = newSphereModel()
	m.worldView = newWorldModel()
	m.peekView = newPeekModel(cfg.SessionMgr, cfg.SOLHome)

	return m
}

// activeView returns the current top-of-stack view mode.
func (m Model) activeView() viewMode {
	if len(m.viewStack) == 0 {
		return viewSphere
	}
	return m.viewStack[len(m.viewStack)-1]
}

// cycleFocus advances (dir=1) or retreats (dir=-1) the combined focus order
// for the active view: its own sections, plus the feed as one additional
// stop past the last section. sphereModel/worldModel.cycleFocus still own
// their own section-to-section movement (and its existing quirks — see
// each's doc comment) unchanged; this only adds the boundary crossing into
// and out of feed focus, so within-section tab behavior is not disturbed.
//
// Final tab order — sphere view: Processes, Worlds, [Caravans], Feed, back
// to Processes. World view: Processes, [Writs], Outposts, Envoys,
// [Merge Queue], [Caravans], Feed, back to Processes (bracketed sections
// only appear when they have rows, same as today). shift+tab reverses.
func (m *Model) cycleFocus(dir int) {
	switch m.activeView() {
	case viewSphere:
		if m.feedFocused {
			m.feedFocused = false
			sections := m.sphereView.availableSections()
			if len(sections) == 0 {
				m.feedFocused = true // nothing else focusable — stay put
				return
			}
			if dir > 0 {
				m.sphereView.focusedSection = sections[0]
			} else {
				m.sphereView.focusedSection = sections[len(sections)-1]
			}
			m.sphereView.hasFocus = true
			return
		}
		if m.sphereView.cycleFocus(dir) {
			m.sphereView.hasFocus = false
			m.feedFocused = true
			return
		}
		m.sphereView.hasFocus = true

	case viewWorld:
		if m.feedFocused {
			m.feedFocused = false
			sections := m.worldView.availableSections()
			if len(sections) == 0 {
				m.feedFocused = true
				return
			}
			if dir > 0 {
				m.worldView.focusedSection = sections[0]
			} else {
				m.worldView.focusedSection = sections[len(sections)-1]
			}
			m.worldView.hasFocus = true
			return
		}
		if m.worldView.cycleFocus(dir) {
			m.worldView.hasFocus = false
			m.feedFocused = true
			return
		}
		m.worldView.hasFocus = true
	}
}

// enterPeek pushes viewPeek with the given items and returns the commands to
// kick off the initial capture/spinner ticks. Shared by the normal peekMsg
// path (sphere/world sections entering peek from already-loaded data) and
// the 'w' agent-writ-detail path (agentWritResultMsg), which builds its
// peekMsg asynchronously after a store fetch instead.
func (m *Model) enterPeek(msg peekMsg) []tea.Cmd {
	m.peekView.width = m.width
	m.peekView.height = m.height
	spinnerTickCmd := m.peekView.enter(msg)
	m.viewStack = append(m.viewStack, viewPeek)

	// Store caravan data if entering caravan peek.
	if len(msg.items) > 0 && msg.items[0].isCaravan {
		if msg.fromView == viewWorld && m.worldData != nil {
			m.peekView.caravanData = m.worldData.Caravans
		} else if msg.fromView == viewSphere && m.sphereData != nil {
			m.peekView.caravanData = m.sphereData.Caravans
		}
	}

	return []tea.Cmd{captureTickCmd(), m.peekView.captureCmd(), spinnerTickCmd}
}

// handleFeedNav routes a feedNavMsg (enter on a focused feed event) to its
// destination — world view with a merge-queue/agent row focused, or a
// caravan peek. See feednav.go's navTarget for how the target is computed
// and its doc comment for which event types can (and cannot, today) resolve
// a target world from their payload alone.
func (m Model) handleFeedNav(msg feedNavMsg) (Model, tea.Cmd) {
	m.dirty = true

	if msg.kind == "caravan" {
		return m.handleFeedNavCaravan(msg)
	}

	// mr/agent: both need a target world to search within.
	if msg.world == "" {
		// The event's payload never carried a world, and we're not already
		// inside a world-filtered feed to fall back on (see navTarget) — no
		// world to search, so there's nowhere to land. Surface this on
		// whichever view is currently active rather than guess.
		notice := "event is missing world info — can't navigate (see resolution notes)"
		switch m.activeView() {
		case viewWorld:
			m.worldView.navNotice = notice
		case viewSphere:
			m.sphereView.navNotice = notice
		}
		return m, scheduleClearFeedback()
	}

	if m.activeView() == viewWorld && m.world == msg.world && m.worldData != nil {
		// Already showing the right world with data in hand — apply now.
		// Either way we're landing on a section, so feed focus is done.
		m.feedFocused = false
		var found bool
		switch msg.kind {
		case "mr":
			found = m.worldView.applyMRFocus(m.worldData, msg.mrID)
		case "agent":
			found = m.worldView.applyAgentFocus(m.worldData, msg.agentName)
		}
		if !found {
			id := msg.mrID
			if msg.kind == "agent" {
				id = msg.agentName
			}
			m.worldView.navNotice = pendingFocusMissingNotice(msg.kind, id)
			return m, scheduleClearFeedback()
		}
		return m, nil
	}

	// Need to (re)navigate to msg.world — defer focus application until its
	// data arrives (see the dataMsg case's pendingFocus handling).
	id := msg.mrID
	if msg.kind == "agent" {
		id = msg.agentName
	}
	m.pendingFocus = &pendingWorldFocus{kind: msg.kind, id: id}
	m.navigateToWorld(msg.world)
	return m, tea.Batch(m.refresh(), m.worldView.init())
}

// handleFeedNavCaravan handles the "caravan" case of handleFeedNav. Unlike
// mr/agent, caravan peek is reachable directly from either sphere or world
// view without switching world context (buildCaravanPeekItems only needs
// the already-loaded caravan list for whichever view is active), and all
// three caravan event types reliably carry caravan_id — see navTarget.
func (m Model) handleFeedNavCaravan(msg feedNavMsg) (Model, tea.Cmd) {
	m.feedFocused = false // either branch below lands somewhere concrete
	var caravans []status.CaravanInfo
	switch m.activeView() {
	case viewWorld:
		if m.worldData != nil {
			caravans = m.worldData.Caravans
		}
	case viewSphere:
		if m.sphereData != nil {
			caravans = m.sphereData.Caravans
		}
	}

	items := buildCaravanPeekItems(caravans)
	idx := -1
	for i, it := range items {
		if it.caravanID == msg.caravanID {
			idx = i
			break
		}
	}
	if idx == -1 {
		notice := "caravan no longer exists"
		switch m.activeView() {
		case viewWorld:
			m.worldView.hasFocus = true
			m.worldView.focusedSection = sectionCaravans
			m.worldView.navNotice = notice
		case viewSphere:
			m.sphereView.hasFocus = true
			m.sphereView.focusedSection = sphereSectionCaravans
			m.sphereView.navNotice = notice
		}
		return m, scheduleClearFeedback()
	}

	pm := peekMsg{items: items, initialCursor: idx, fromView: m.activeView(), world: m.world}
	cmds := m.enterPeek(pm)
	return m, tea.Batch(cmds...)
}

// navigateToWorld switches world context to world, reusing drillMsg's data
// reset when coming from sphere view. When already in world view showing a
// different world, it replaces context in place instead of pushing a second
// viewWorld frame onto the stack — drillMsg is only ever invoked from
// sphere view's worlds table, so it never had to handle "already in world
// view" itself; a feed event can fire from either view.
func (m *Model) navigateToWorld(world string) {
	m.world = world
	m.worldData = nil
	m.lastTokenRefresh = time.Time{}
	m.worldView = newWorldModel()
	m.worldView.width = m.width
	m.worldView.height = m.height
	m.feedFocused = false
	if m.activeView() != viewWorld {
		m.viewStack = append(m.viewStack, viewWorld)
	}
}

// Init starts the first data fetch and both tick schedulers.
func (m Model) Init() tea.Cmd {
	m.feed.loadInitial()
	return tea.Batch(
		m.refresh(),
		m.sphereView.init(),
		m.worldView.init(),
		animTickCmd(),
		m.highlightTickCmd(),
	)
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Reset dirty; individual handlers set it when visual state changes.
	m.dirty = false
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.sphereView.width = msg.Width
		m.sphereView.height = msg.Height
		m.worldView.width = msg.Width
		m.worldView.height = msg.Height
		m.peekView.width = msg.Width
		m.peekView.height = msg.Height
		m.feed.setHeight(msg.Height)
		if m.peekView.forgeFeed != nil {
			m.peekView.forgeFeed.setHeight(msg.Height)
		}
		if m.peekView.sourceFeed != nil {
			m.peekView.sourceFeed.setHeight(msg.Height)
		}
		m.ready = true
		m.dirty = true

	case tea.KeyMsg:
		m.dirty = true

		// Confirmation overlay: captures all input while active.
		if m.confirm.active {
			consumed, cmd := m.confirm.update(msg)
			if consumed {
				return m, cmd
			}
		}

		// Help overlay: any key dismisses it.
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			m.storeCache.CloseAll()
			return m, tea.Quit
		case "r":
			// In peek mode, r is handled by the peek view (force capture refresh).
			if m.activeView() != viewPeek {
				return m, m.refresh()
			}
		case "?":
			m.showHelp = true
			return m, nil
		case "i":
			// Exec into sol inbox from sphere or world view only — peek mode
			// routes its own keys, and inbox itself is out of scope here.
			if m.activeView() == viewSphere || m.activeView() == viewWorld {
				return m, func() tea.Msg { return inboxMsg{} }
			}

		// The feed is only focusable from sphere/world view (peek renders its
		// own feed panel with no cursor). tab/shift+tab, when NOT already
		// feed-focused, fall through unhandled to sphereView/worldView's own
		// section cycling below — cycleFocus decides there (via the wrapped
		// bool each cycleFocus returns) whether that move stays within the
		// view's sections or hands off to the feed as one extra stop past
		// the last section. See the "final tab order" note in the
		// resolution report.
		case "tab":
			if m.activeView() == viewSphere || m.activeView() == viewWorld {
				m.cycleFocus(1)
				return m, nil
			}
		case "shift+tab":
			if m.activeView() == viewSphere || m.activeView() == viewWorld {
				m.cycleFocus(-1)
				return m, nil
			}
		case "esc":
			// First esc while the feed has focus just drops feed focus,
			// mirroring how a focused section's first esc unfocuses it
			// (handled below, unchanged, once this case doesn't return).
			if m.feedFocused && (m.activeView() == viewSphere || m.activeView() == viewWorld) {
				m.feedFocused = false
				return m, nil
			}
		case "up", "k":
			if m.feedFocused {
				m.feed.moveCursor(-1)
				return m, nil
			}
		case "down", "j":
			if m.feedFocused {
				m.feed.moveCursor(1)
				return m, nil
			}
		case "enter":
			if m.feedFocused {
				return m, m.feed.navigateCmd()
			}
		}

		// Route navigation keys to active view.
		switch m.activeView() {
		case viewSphere:
			sv, cmd := m.sphereView.update(msg, m.sphereData)
			m.sphereView = sv
			cmds = append(cmds, cmd)
		case viewWorld:
			wv, cmd := m.worldView.update(msg, m.worldData)
			m.worldView = wv
			cmds = append(cmds, cmd)
		case viewPeek:
			pv, cmd := m.peekView.update(msg)
			m.peekView = pv
			cmds = append(cmds, cmd)
		}

	case drillMsg:
		m.dirty = true
		// Push world view onto the stack.
		m.world = msg.world
		m.worldData = nil // clear stale data
		m.lastTokenRefresh = time.Time{} // force fresh token query
		m.worldView = newWorldModel()
		m.worldView.width = m.width
		m.worldView.height = m.height
		m.viewStack = append(m.viewStack, viewWorld)
		m.feedFocused = false // new view's sections start unfocused, same as feed
		cmds = append(cmds, m.refresh(), m.worldView.init())

	case popMsg:
		m.dirty = true
		// Pop back to sphere view.
		if len(m.viewStack) > 1 {
			m.viewStack = m.viewStack[:len(m.viewStack)-1]
			m.world = ""
			m.worldData = nil
			m.lastTokenRefresh = time.Time{} // force fresh token query
			m.feedFocused = false
			// Clear feed world filter so sphere view shows all events,
			// and reload since cached events may have been world-filtered.
			m.feed.world = ""
			m.feed.loadInitial()
			// Re-gather sphere data (ZFC — fresh on return).
			cmds = append(cmds, m.refresh())
		}

	case peekMsg:
		m.feedFocused = false // peek mode has no feed cursor
		cmds = append(cmds, m.enterPeek(msg)...)

	case peekPopMsg:
		// Pop back from peek to the previous view.
		if len(m.viewStack) > 1 {
			m.viewStack = m.viewStack[:len(m.viewStack)-1]
			cmds = append(cmds, m.refresh())
		}

	case captureTickMsg:
		// Only tick while in peek mode.
		if m.activeView() == viewPeek {
			cmds = append(cmds, m.peekView.captureCmd(), captureTickCmd())
		}

	case captureResultMsg:
		if msg.err == nil {
			m.peekView.capture = msg.content
			m.peekView.captureAge = time.Now()
		} else {
			m.peekView.capture = ""
		}

	case attachMsg:
		// Suspend TUI and attach to tmux session.
		cmd := exec.Command("tmux", "attach-session", "-t", "="+msg.sessionName+":")
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return attachDoneMsg{err: err}
		})

	case attachDoneMsg:
		m.dirty = true
		// Resume after detach — force immediate refresh.
		cmds = append(cmds, m.refresh())
		// Surface attach failures so they are not silently discarded.
		if msg.err != nil {
			switch m.activeView() {
			case viewSphere:
				m.sphereView.showNoSession = true
				m.sphereView.noSessionMessage = fmt.Sprintf("attach failed: %s", msg.err)
			case viewWorld:
				m.worldView.showNoSession = true
				m.worldView.noSessionMessage = fmt.Sprintf("attach failed: %s", msg.err)
			}
		}

	case inboxMsg:
		// Suspend TUI and exec `sol inbox` — same mechanism as attach
		// (tea.ExecProcess), but launching the sol binary itself rather
		// than tmux, mirroring restart.go's os.Executable() lookup.
		solBin, err := os.Executable()
		if err != nil {
			return m, func() tea.Msg {
				return inboxDoneMsg{err: fmt.Errorf("find sol binary: %w", err)}
			}
		}
		cmd := exec.Command(solBin, "inbox")
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return inboxDoneMsg{err: err}
		})

	case inboxDoneMsg:
		m.dirty = true
		// Resume after the inbox TUI exits — force immediate refresh so
		// cleared items disappear right away.
		cmds = append(cmds, m.refresh())
		if msg.err != nil {
			switch m.activeView() {
			case viewSphere:
				m.sphereView.showNoSession = true
				m.sphereView.noSessionMessage = fmt.Sprintf("inbox failed: %s", msg.err)
			case viewWorld:
				m.worldView.showNoSession = true
				m.worldView.noSessionMessage = fmt.Sprintf("inbox failed: %s", msg.err)
			}
		}

	case noSessionMsg:
		m.dirty = true
		// Route to the active view to show inline message.
		switch m.activeView() {
		case viewSphere:
			m.sphereView.showNoSession = true
			m.sphereView.noSessionMessage = msg.message
		case viewWorld:
			m.worldView.showNoSession = true
			m.worldView.noSessionMessage = msg.message
		case viewPeek:
			// In peek mode, the capture panel already shows "No active session".
		}

	case restartProcessMsg:
		m.dirty = true
		// Sphere process restart — check systemd guard before showing confirmation.
		info, ok := sphereProcessMap[msg.processName]
		if ok && systemdManaged(info.cliName) {
			m.confirm.show(
				fmt.Sprintf("Cannot restart %s", msg.processName),
				fmt.Sprintf("Managed by systemd — use systemctl --user restart sol-%s", info.cliName),
				nil,
			)
		} else {
			m.confirm.show(
				fmt.Sprintf("Restart %s?", msg.processName),
				"This will stop and re-launch the process.",
				sphereRestartCmd(msg.processName),
			)
		}

	case requestRestartMsg:
		m.dirty = true
		// World-level restart — show confirmation using the confirmModel.
		target := msg.target
		m.confirm.show(
			target.confirmTitle,
			target.confirmDetail,
			worldRestartCmd(target),
		)

	case requestHandoffMsg:
		m.dirty = true
		// World-level handoff — show confirmation using the confirmModel.
		target := msg.target
		m.confirm.show(
			target.confirmTitle,
			target.confirmDetail,
			worldHandoffCmd(target),
		)

	case restartDoneMsg:
		m.dirty = true
		// Sphere process restart result.
		if msg.err != nil {
			// Show error in confirmation overlay.
			m.confirm.show(
				fmt.Sprintf("Restart %s failed", msg.processName),
				msg.err.Error(),
				nil,
			)
		}
		// Force refresh to pick up new state.
		cmds = append(cmds, m.refresh())

	case requestForgeToggleMsg:
		m.dirty = true
		// Forge pause/resume — show confirmation using the confirmModel.
		target := msg
		m.confirm.show(
			target.confirmTitle,
			target.confirmDetail,
			forgeToggleCmd(target.world, target.pause),
		)

	case requestCastMsg:
		m.dirty = true
		// Writ cast — show confirmation using the confirmModel, same
		// mechanism as the other world-level actions.
		m.confirm.show(msg.confirmTitle, msg.confirmDetail, castCmd(msg.world, msg.writID, m.config.SessionMgr))

	case castDoneMsg:
		m.dirty = true
		// Cast result — show inline feedback (same mechanism as world-level
		// restarts and MR requeue/supersede).
		if msg.err != nil {
			m.worldView.restartFeedback = fmt.Sprintf("cast %s failed: %s", msg.writID, msg.err)
			m.worldView.restartFeedbackErr = true
		} else {
			m.worldView.restartFeedback = fmt.Sprintf("cast %s -> %s", msg.writID, msg.agentName)
			m.worldView.restartFeedbackErr = false
		}
		cmds = append(cmds, scheduleClearFeedback(), m.refresh())

	case feedNavMsg:
		var cmd tea.Cmd
		m, cmd = m.handleFeedNav(msg)
		cmds = append(cmds, cmd)

	case requestAgentWritMsg:
		m.dirty = true
		cmds = append(cmds, fetchAgentWritCmd(m.storeCache, msg.world, msg.writID))

	case agentWritResultMsg:
		m.dirty = true
		if msg.peek != nil {
			cmds = append(cmds, m.enterPeek(*msg.peek)...)
		} else {
			m.worldView.navNotice = msg.notice
			cmds = append(cmds, scheduleClearFeedback())
		}

	case requestMRActionMsg:
		m.dirty = true
		// Merge-queue requeue/supersede — the guard-check query already ran
		// (mrGuardCmd); confirmDetail carries its findings. A blocked
		// request (closed writ, requeue only) shows the reason with no
		// action wired to 'y', matching the systemd-managed restart guard.
		if msg.blocked {
			m.confirm.show(msg.confirmTitle, msg.confirmDetail, nil)
			break
		}
		var onYes tea.Cmd
		switch msg.action {
		case mrActionRequeue:
			onYes = mrRequeueCmd(msg.world, msg.mrID)
		case mrActionSupersede:
			onYes = mrSupersedeCmd(msg.world, msg.mrID)
		}
		m.confirm.show(msg.confirmTitle, msg.confirmDetail, onYes)

	case mrActionDoneMsg:
		m.dirty = true
		// Requeue/supersede result — show inline feedback (same mechanism
		// as world-level restarts and forge pause/resume).
		verb := "requeued"
		if msg.action == mrActionSupersede {
			verb = "superseded"
		}
		if msg.err != nil {
			m.worldView.restartFeedback = fmt.Sprintf("%s %s failed: %s", msg.mrID, verb, msg.err)
			m.worldView.restartFeedbackErr = true
		} else {
			m.worldView.restartFeedback = fmt.Sprintf("%s %s", msg.mrID, verb)
			m.worldView.restartFeedbackErr = false
		}
		cmds = append(cmds, scheduleClearFeedback(), m.refresh())

	case worldForgeToggleDoneMsg:
		m.dirty = true
		// Forge pause/resume result — show inline feedback (same mechanism
		// as world-level restarts).
		if msg.err != nil {
			verb := "pause"
			if !msg.pause {
				verb = "resume"
			}
			m.worldView.restartFeedback = fmt.Sprintf("forge %s failed: %s", verb, msg.err)
			m.worldView.restartFeedbackErr = true
		} else if msg.pause {
			m.worldView.restartFeedback = "forge paused"
			m.worldView.restartFeedbackErr = false
		} else {
			m.worldView.restartFeedback = "forge resumed"
			m.worldView.restartFeedbackErr = false
		}
		cmds = append(cmds, scheduleClearFeedback(), m.refresh())

	case worldRestartDoneMsg:
		m.dirty = true
		// World-level restart result — show inline feedback.
		if msg.err != nil {
			m.worldView.restartFeedback = fmt.Sprintf("restart failed: %s", msg.err)
			m.worldView.restartFeedbackErr = true
		} else {
			m.worldView.restartFeedback = fmt.Sprintf("%s restarted", msg.name)
			m.worldView.restartFeedbackErr = false
		}
		cmds = append(cmds, scheduleClearFeedback(), m.refresh())

	case worldHandoffDoneMsg:
		m.dirty = true
		// World-level handoff result — show inline feedback (same mechanism
		// as world-level restarts).
		if msg.err != nil {
			m.worldView.restartFeedback = fmt.Sprintf("handoff failed: %s", msg.err)
			m.worldView.restartFeedbackErr = true
		} else {
			m.worldView.restartFeedback = fmt.Sprintf("%s handed off", msg.name)
			m.worldView.restartFeedbackErr = false
		}
		cmds = append(cmds, scheduleClearFeedback(), m.refresh())

	case clearRestartFeedbackMsg:
		m.dirty = true
		m.worldView.restartFeedback = ""
		m.worldView.restartFeedbackErr = false
		m.worldView.navNotice = ""
		m.sphereView.navNotice = ""

	case animTickMsg:
		// Animation tick (~30 FPS) — drives visual state.
		// Only set dirty when animation actually affects visible output.
		m.pulsePhase = (m.pulsePhase + 1) % pulseFrames

		// Track whether any feed has an active fade animation.
		feedAnimating := m.feed.fadeLevel() > 0
		m.feed.decayAnimation()

		// Decay forge feed animation if active.
		if m.peekView.forgeFeed != nil {
			if m.peekView.forgeFeed.fadeLevel() > 0 {
				feedAnimating = true
			}
			m.peekView.forgeFeed.decayAnimation()
		}
		// Decay source feed animation if active.
		if m.peekView.sourceFeed != nil {
			if m.peekView.sourceFeed.fadeLevel() > 0 {
				feedAnimating = true
			}
			m.peekView.sourceFeed.decayAnimation()
		}

		// Route to active sub-view for spinner frame updates.
		viewAnimating := false
		switch m.activeView() {
		case viewSphere:
			viewAnimating = m.sphereView.updateAnim()
		case viewWorld:
			viewAnimating = m.worldView.updateAnim()
		}

		// Check if highlights are actively fading.
		highlightsActive := m.healthHighlight > 0 || len(m.agentHighlights) > 0

		// Check if pulse phase affects visible output — only when a required
		// process is down and pulsing in the current view.
		pulseVisible := m.hasPulsingIndicator()

		if viewAnimating || feedAnimating || highlightsActive || pulseVisible {
			m.dirty = true
		}

		cmds = append(cmds, animTickCmd())

	case dataTickMsg:
		// Data refresh tick (3s) — database queries and status gathering.
		cmds = append(cmds, m.refresh())

	case highlightTickMsg:
		m.dirty = true
		m.decayHighlights()
		cmds = append(cmds, m.highlightTickCmd())

	case dataMsg:
		m.dirty = true
		m.lastRefresh = time.Now()
		// Track refresh-error banner state. Keep the existing "advance
		// lastRefresh" behavior so the UI stays responsive — but record
		// the error so the renderer can warn the operator that the data
		// they're staring at is not fresh.
		if msg.refreshErr != nil {
			m.lastRefreshError = msg.refreshErr.Error()
		} else if msg.sphere != nil || msg.world != nil {
			// Only clear on a confirmed successful refresh (data delivered).
			// An empty msg with no error and no data — e.g. a peek-mode tick
			// that didn't fall through to a real fetch — must not silently
			// erase a previously-recorded failure.
			m.lastRefreshError = ""
		}
		if msg.sphere != nil {
			m.trackSphereHighlights(msg.sphere)
			// Update token cache when tokens were actually refreshed.
			if msg.tokensRefreshed {
				m.cachedSphereTokens = msg.sphere.Tokens
				m.lastTokenRefresh = time.Now()
			}
			m.sphereData = msg.sphere
			cmds = append(cmds, m.sphereView.updateData(m.sphereData))
		}
		if msg.world != nil {
			m.trackWorldHighlights(msg.world)
			// Update token cache when tokens were actually refreshed.
			if msg.tokensRefreshed {
				m.cachedWorldTokens = msg.world.Tokens
				m.lastTokenRefresh = time.Now()
			}
			m.worldData = msg.world
			cmds = append(cmds, m.worldView.updateData(m.worldData))

			// Apply a deferred feed-nav focus target now that real world
			// data has landed — see pendingWorldFocus's doc comment.
			if m.pendingFocus != nil {
				var found bool
				switch m.pendingFocus.kind {
				case "mr":
					found = m.worldView.applyMRFocus(m.worldData, m.pendingFocus.id)
				case "agent":
					found = m.worldView.applyAgentFocus(m.worldData, m.pendingFocus.id)
				}
				if !found {
					m.worldView.navNotice = pendingFocusMissingNotice(m.pendingFocus.kind, m.pendingFocus.id)
					cmds = append(cmds, scheduleClearFeedback())
				}
				m.pendingFocus = nil
			}
		}

		// Refresh peek items if peek mode is active, so the list
		// reflects agents that started or stopped since peek entry.
		if m.activeView() == viewPeek {
			if m.peekView.caravanData != nil {
				// Caravan peek — refresh caravan data and items.
				if m.peekView.fromView == viewWorld && msg.world != nil {
					m.peekView.caravanData = msg.world.Caravans
					m.peekView.refreshItems(buildCaravanPeekItems(msg.world.Caravans))
				} else if m.peekView.fromView == viewSphere && msg.sphere != nil {
					m.peekView.caravanData = msg.sphere.Caravans
					m.peekView.refreshItems(buildCaravanPeekItems(msg.sphere.Caravans))
				}
			} else if len(m.peekView.items) > 0 && m.peekView.items[0].isWrit {
				// Writ backlog peek — refresh from the world's open-writ list.
				if m.peekView.fromView == viewWorld && msg.world != nil {
					m.peekView.refreshItems(buildWritPeekItems(msg.world.Writs))
				}
			} else {
				if m.peekView.fromView == viewWorld && msg.world != nil {
					m.peekView.refreshItems(buildWorldPeekItems(msg.world))
					m.peekView.updateForgeData(msg.world)
				} else if m.peekView.fromView == viewSphere && msg.sphere != nil {
					m.peekView.refreshItems(buildSpherePeekItems(m.sphereView))
				}
			}
			// Only refresh the currently visible peek feed(s).
			if m.peekView.forgeFeed != nil {
				// Forge feed replaces the main feed at the bottom — only refresh forge.
				m.peekView.forgeFeed.refresh()
			} else {
				// Main feed is visible at the bottom — refresh it.
				m.feed.refresh()
				// Source feed is shown in the right panel alongside the main feed.
				if m.peekView.sourceFeed != nil {
					m.peekView.sourceFeed.refresh()
				}
			}
		}

		// Refresh main feed when not in peek mode.
		if m.activeView() != viewPeek {
			m.feed.refresh()
		}

		// Schedule next data tick.
		cmds = append(cmds, dataTickCmd())

	case spinner.TickMsg:
		// Route spinner ticks to ALL sub-views, not just the active one.
		// bubbles spinners self-perpetuate: each processed tick re-arms the
		// next one, and a tick only re-arms for the spinner whose id/tag it
		// matches (mismatches are free no-ops). Spinner maps survive
		// navigation (worldModel across peek round-trips, sphereModel across
		// drill/pop), so a background view's chain must still receive its
		// own ticks or it dies permanently and never animates again once
		// that view becomes active again. Ticks for spinners belonging to
		// models that got discarded (e.g. a fresh worldModel from drillMsg)
		// simply have no target left and die naturally.
		// Don't set dirty — next animTickMsg will pick up the new frame.
		sv, svCmd := m.sphereView.updateSpinner(msg)
		m.sphereView = sv
		cmds = append(cmds, svCmd)

		wv, wvCmd := m.worldView.updateSpinner(msg)
		m.worldView = wv
		cmds = append(cmds, wvCmd)

		pv, pvCmd := m.peekView.updateSpinner(msg)
		m.peekView = pv
		cmds = append(cmds, pvCmd)
	}

	return m, tea.Batch(cmds...)
}

// View renders the active view. Uses a dirty flag to skip re-rendering
// when no visual state has changed (safeguard at 30 FPS).
func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Minimum terminal size check.
	if m.width < minTermWidth || m.height < minTermHeight {
		return fmt.Sprintf(
			"\n  Terminal too small (%dx%d).\n  Minimum size: %dx%d.\n",
			m.width, m.height, minTermWidth, minTermHeight,
		)
	}

	// Confirmation overlay.
	if m.confirm.active {
		return m.confirm.view(m.width, m.height)
	}

	// Help overlay.
	if m.showHelp {
		return helpOverlay(m.width, m.height)
	}

	// Short-circuit if nothing changed since last render.
	if !m.dirty && m.viewCache != nil && *m.viewCache != "" {
		return *m.viewCache
	}

	pulseBright := m.isPulseBright()

	var content string
	switch m.activeView() {
	case viewSphere:
		content = m.sphereView.view(m.sphereData, m.lastRefresh, m.healthHighlight, pulseBright)
	case viewWorld:
		content = m.worldView.view(m.worldData, m.lastRefresh, m.healthHighlight, m.agentHighlights, pulseBright)
	case viewPeek:
		// Peek mode renders its own layout including the feed.
		content = m.peekView.view(m.feed.view(m.width))
	default:
		content = "Unknown view"
	}

	// Refresh-error banner (ORCH-M5) — surface the most recent failure as a
	// terse line right after the view content so the operator sees that the
	// "fresh" data they're looking at isn't actually fresh. Cleared on the
	// next successful refresh by the dataMsg handler.
	if m.lastRefreshError != "" {
		content += renderRefreshErrorBanner(m.lastRefreshError)
	}

	// Append feed panel (peek mode handles its own feed, with no cursor).
	if m.activeView() != viewPeek {
		if m.feedFocused {
			content += m.feed.viewFocused(m.width)
		} else {
			content += m.feed.view(m.width)
		}
	}

	// Cache the rendered view for dirty-flag optimization.
	if m.viewCache != nil {
		*m.viewCache = content
	}

	return content
}

// renderRefreshErrorBanner formats the transient refresh-failure notice
// shown in the dashboard footer. Truncated so a runaway error message
// can't dominate the screen.
func renderRefreshErrorBanner(errMsg string) string {
	const maxLen = 200
	errMsg = style.TruncateBytes(errMsg, maxLen)
	return errorStyle.Render("⚠ refresh failed: ") + errMsg + "\n"
}

// trackSphereHighlights detects health changes in sphere data.
func (m *Model) trackSphereHighlights(data *status.SphereStatus) {
	if m.prevSphereHealth != "" && data.Health != m.prevSphereHealth {
		m.healthHighlight = highlightMaxLevel
	}
	m.prevSphereHealth = data.Health
}

// trackWorldHighlights detects health and agent state changes in world data.
func (m *Model) trackWorldHighlights(data *status.WorldStatus) {
	newHealth := data.HealthString()
	if m.prevWorldHealth != "" && newHealth != m.prevWorldHealth {
		m.healthHighlight = highlightMaxLevel
	}
	m.prevWorldHealth = newHealth

	// Track agent state changes.
	for _, a := range data.Agents {
		prev, exists := m.prevAgentStates[a.Name]
		if exists && prev != a.State {
			m.agentHighlights[a.Name] = highlightMaxLevel
		}
		m.prevAgentStates[a.Name] = a.State
	}

	// Prune stale entries for agents no longer in the current list.
	current := make(map[string]bool, len(data.Agents))
	for _, a := range data.Agents {
		current[a.Name] = true
	}
	for name := range m.prevAgentStates {
		if !current[name] {
			delete(m.prevAgentStates, name)
			delete(m.agentHighlights, name)
		}
	}
}

// decayHighlights decrements all highlight levels by one step.
// Called on each highlightTickMsg (~400ms), producing a progressive fade over ~2 seconds.
func (m *Model) decayHighlights() {
	if m.healthHighlight > 0 {
		m.healthHighlight--
	}
	for name, level := range m.agentHighlights {
		if level <= 1 {
			delete(m.agentHighlights, name)
		} else {
			m.agentHighlights[name] = level - 1
		}
	}
}

// refresh gathers fresh data in a command.
func (m Model) refresh() tea.Cmd {
	// Capture cache and token state for the closure.
	cache := m.storeCache
	lastTokenRefresh := m.lastTokenRefresh
	cachedSphereTokens := m.cachedSphereTokens
	cachedWorldTokens := m.cachedWorldTokens

	return func() tea.Msg {
		var msg dataMsg

		// Prune stale cache entries on each refresh.
		cache.Prune()

		// In peek mode, refresh the underlying view's data.
		view := m.activeView()
		if view == viewPeek {
			if m.peekView.world != "" {
				view = viewWorld
			} else {
				view = viewSphere
			}
		}

		switch view {
		case viewSphere:
			result := status.GatherSphere(
				m.config.SphereStore,
				m.config.SphereStore,
				m.config.SessionCheck,
				cache.Opener(),
				m.config.WorldOpener,
				m.config.SphereStore,
				m.config.EscalationLister,
			)
			if count, err := m.config.SphereStore.CountPending(config.Autarch); err == nil && count > 0 {
				result.MailCount = count
			}

			// Token throttling: GatherSphere already computed tokens.
			// Override with cached values if within the throttle window.
			if time.Since(lastTokenRefresh) < tokenRefreshSphere {
				result.Tokens = cachedSphereTokens
			} else {
				msg.tokensRefreshed = true
			}

			msg.sphere = result

		case viewWorld:
			ws, err := cache.Get(m.world)
			if err != nil {
				msg.refreshErr = fmt.Errorf("open world store %q: %w", m.world, err)
				return msg
			}
			// Do NOT close ws — the cache owns its lifecycle.

			result, err := status.Gather(
				m.world,
				m.config.SphereStore,
				ws,
				ws,
				m.config.SessionCheck,
			)
			if err != nil {
				msg.refreshErr = fmt.Errorf("gather world status %q: %w", m.world, err)
				return msg
			}
			// Reuse the store cache for cross-world writ-title lookups
			// (GatherCaravans/buildCaravanInfo never close what they look
			// up — the cache owns store lifecycle). CheckCaravanReadiness
			// still opens/closes its own store per readiness check, so it
			// gets the original raw opener instead.
			status.GatherCaravans(result, m.config.CaravanStore, cache.Opener(), m.config.WorldOpener)

			// Consul (sphere-level — same data shown in sphere view).
			result.Consul = status.GatherConsulInfo()

			// Inbox: mail count and open escalations (both sphere-level).
			if count, err := m.config.SphereStore.CountPending(config.Autarch); err == nil && count > 0 {
				result.MailCount = count
			}
			if m.config.EscalationLister != nil {
				if escs, err := m.config.EscalationLister.ListOpenEscalations(); err == nil && len(escs) > 0 {
					summary := &status.EscalationSummary{
						Total:      len(escs),
						BySeverity: make(map[string]int),
					}
					for _, esc := range escs {
						summary.BySeverity[esc.Severity]++
					}
					result.Escalations = summary
				}
			}

			// Token throttling: only refresh tokens every tokenRefreshWorld.
			if time.Since(lastTokenRefresh) >= tokenRefreshWorld {
				status.GatherTokens(result, ws)
				msg.tokensRefreshed = true
			} else {
				result.Tokens = cachedWorldTokens
			}

			// Load world config for max_active (non-fatal).
			if worldCfg, err := config.LoadWorldConfig(m.world); err == nil {
				result.MaxActive = worldCfg.Agents.MaxActive
			}

			msg.world = result
		}

		return msg
	}
}

// animTickCmd schedules the next animation tick (~30 FPS).
func animTickCmd() tea.Cmd {
	return tea.Tick(animInterval, func(t time.Time) tea.Msg {
		return animTickMsg(t)
	})
}

// dataTickCmd schedules the next data refresh tick.
func dataTickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return dataTickMsg(t)
	})
}

// highlightTickCmd schedules the next highlight decay tick.
func (m Model) highlightTickCmd() tea.Cmd {
	return tea.Tick(highlightTickInterval, func(t time.Time) tea.Msg {
		return highlightTickMsg(t)
	})
}

// isPulseBright returns true during the bright phase of the pulse cycle.
func (m Model) isPulseBright() bool {
	return m.pulsePhase%pulseFrames < pulseFrames/2
}

// hasPulsingIndicator returns true if the pulse animation affects visible
// output — i.e., any element uses pulsingStatusIndicator or pulseStyle
// and we just crossed a pulse phase boundary (bright↔dim transition).
func (m Model) hasPulsingIndicator() bool {
	// Pulse only matters at the boundary where bright/dim transitions.
	// Half-cycle boundary: phase 0 (dim→bright) and pulseFrames/2 (bright→dim).
	if m.pulsePhase != 0 && m.pulsePhase != pulseFrames/2 {
		return false
	}

	switch m.activeView() {
	case viewSphere:
		if m.sphereData == nil {
			return false
		}
		// Required sphere processes pulse when down.
		return !m.sphereData.Prefect.Running ||
			!m.sphereData.Consul.Running ||
			!m.sphereData.Broker.Running
	case viewWorld:
		if m.worldData == nil {
			return false
		}
		// Required sphere processes in world view: Prefect, Broker, Consul.
		if !m.worldData.Prefect.Running || !m.worldData.Broker.Running || !m.worldData.Consul.Running {
			return true
		}
		// Stalled or dead-session agents/envoys also pulse.
		for _, a := range m.worldData.Agents {
			if a.State == "stalled" || (a.State == "working" && !a.SessionAlive) {
				return true
			}
		}
		for _, e := range m.worldData.Envoys {
			if e.State == "stalled" || (e.State == "working" && !e.SessionAlive) {
				return true
			}
		}
		// Failed MRs pulse.
		for _, mr := range m.worldData.MergeRequests {
			if mr.Phase == "failed" {
				return true
			}
		}
	}
	return false
}

// sessionName returns the tmux session name for an agent in the current world.
func (m Model) sessionName(agentName string) string {
	return config.SessionName(m.world, agentName)
}
