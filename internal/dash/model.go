package dash

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
)

const refreshInterval = 3 * time.Second

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
	SphereStore  sphereStore
	WorldOpener  func(string) (*store.Store, error)
	SessionCheck status.SessionChecker
	CaravanStore caravanStore
	SessionMgr   *session.Manager

	// SOLHome is the runtime root directory for reading event feeds.
	SOLHome string

	// World is non-empty when starting in world detail view.
	World string
}

// sphereStore combines the interfaces the status package needs for sphere gathering.
type sphereStore interface {
	status.SphereStore
	status.WorldLister
	status.CaravanStore
}

// caravanStore abstracts caravan queries.
type caravanStore interface {
	status.CaravanStore
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
}

// drillMsg signals that the sphere view wants to drill into a world.
type drillMsg struct {
	world string
}

// popMsg signals the world view wants to return to sphere.
type popMsg struct{}

// attachMsg signals that the world view wants to attach to an agent session.
type attachMsg struct {
	sessionName string
}

// attachDoneMsg fires when an agent tmux attach completes (user detached).
type attachDoneMsg struct {
	err error
}

// noSessionMsg signals an inline "no active session" message.
type noSessionMsg struct{}

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

	// Data.
	sphereData *status.SphereStatus
	worldData  *status.WorldStatus

	// Sub-views.
	sphereView sphereModel
	worldView  worldModel
	peekView   peekModel

	// Activity feed.
	feed feedModel

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
	m.peekView = newPeekModel(cfg.SessionMgr)

	return m
}

// activeView returns the current top-of-stack view mode.
func (m Model) activeView() viewMode {
	if len(m.viewStack) == 0 {
		return viewSphere
	}
	return m.viewStack[len(m.viewStack)-1]
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
			return m, tea.Quit
		case "r":
			// In peek mode, r is handled by the peek view (force capture refresh).
			if m.activeView() != viewPeek {
				return m, m.refresh()
			}
		case "?":
			m.showHelp = true
			return m, nil
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
		m.worldView = newWorldModel()
		m.worldView.width = m.width
		m.worldView.height = m.height
		m.viewStack = append(m.viewStack, viewWorld)
		cmds = append(cmds, m.refresh(), m.worldView.init())

	case popMsg:
		m.dirty = true
		// Pop back to sphere view.
		if len(m.viewStack) > 1 {
			m.viewStack = m.viewStack[:len(m.viewStack)-1]
			m.world = ""
			m.worldData = nil
			// Clear feed world filter so sphere view shows all events,
			// and reload since cached events may have been world-filtered.
			m.feed.world = ""
			m.feed.loadInitial()
			// Re-gather sphere data (ZFC — fresh on return).
			cmds = append(cmds, m.refresh())
		}

	case peekMsg:
		// Enter peek mode — push viewPeek onto the stack.
		m.peekView.width = m.width
		m.peekView.height = m.height
		spinnerTickCmd := m.peekView.enter(msg)
		m.viewStack = append(m.viewStack, viewPeek)
		// Start capture tick, do an immediate capture, and schedule spinner tick.
		cmds = append(cmds, captureTickCmd(), m.peekView.captureCmd(), spinnerTickCmd)

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

	case noSessionMsg:
		m.dirty = true
		// Route to the active view to show inline message.
		switch m.activeView() {
		case viewSphere:
			m.sphereView.showNoSession = true
		case viewWorld:
			m.worldView.showNoSession = true
		case viewPeek:
			// In peek mode, the capture panel already shows "No active session".
		}

	case restartProcessMsg:
		m.dirty = true
		// Sphere process restart — check systemd guard before showing confirmation.
		info, ok := sphereProcessMap[msg.processName]
		if ok && checkSystemdManaged(info.cliName) {
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

	case clearRestartFeedbackMsg:
		m.dirty = true
		m.worldView.restartFeedback = ""
		m.worldView.restartFeedbackErr = false

	case animTickMsg:
		// Animation tick (~30 FPS) — drives visual state.
		m.dirty = true
		m.pulsePhase = (m.pulsePhase + 1) % pulseFrames
		m.feed.decayAnimation()

		// Route to active sub-view for spinner frame updates.
		switch m.activeView() {
		case viewSphere:
			m.sphereView.updateAnim()
		case viewWorld:
			m.worldView.updateAnim()
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
		if msg.sphere != nil {
			m.trackSphereHighlights(msg.sphere)
			m.sphereData = msg.sphere
			cmds = append(cmds, m.sphereView.updateData(m.sphereData))
		}
		if msg.world != nil {
			m.trackWorldHighlights(msg.world)
			m.worldData = msg.world
			cmds = append(cmds, m.worldView.updateData(m.worldData))
		}

		// Refresh peek items if peek mode is active, so the list
		// reflects agents that started or stopped since peek entry.
		if m.activeView() == viewPeek {
			if m.peekView.fromView == viewWorld && msg.world != nil {
				m.peekView.refreshItems(buildWorldPeekItems(msg.world))
			} else if m.peekView.fromView == viewSphere && msg.sphere != nil {
				m.peekView.refreshItems(buildSpherePeekItems(m.sphereView))
			}
		}

		// Refresh feed.
		m.feed.refresh()

		// Schedule next data tick.
		cmds = append(cmds, dataTickCmd())

	case spinner.TickMsg:
		// Route spinner ticks to active view (frame advancement).
		// Don't set dirty — next animTickMsg will pick up the new frame.
		switch m.activeView() {
		case viewSphere:
			sv, cmd := m.sphereView.updateSpinner(msg)
			m.sphereView = sv
			cmds = append(cmds, cmd)
		case viewWorld:
			wv, cmd := m.worldView.updateSpinner(msg)
			m.worldView = wv
			cmds = append(cmds, cmd)
		case viewPeek:
			pv, cmd := m.peekView.updateSpinner(msg)
			m.peekView = pv
			cmds = append(cmds, cmd)
		}
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

	// Append feed panel (peek mode handles its own feed).
	if m.activeView() != viewPeek {
		content += m.feed.view(m.width)
	}

	// Cache the rendered view for dirty-flag optimization.
	if m.viewCache != nil {
		*m.viewCache = content
	}

	return content
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
	return func() tea.Msg {
		var msg dataMsg

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
				m.config.WorldOpener,
				m.config.SphereStore,
			)
			msg.sphere = result

		case viewWorld:
			ws, err := m.config.WorldOpener(m.world)
			if err != nil {
				return msg
			}
			defer ws.Close()

			result, err := status.Gather(
				m.world,
				m.config.SphereStore,
				ws,
				ws,
				m.config.SessionCheck,
			)
			if err != nil {
				return msg
			}
			status.GatherCaravans(result, m.config.CaravanStore, m.config.WorldOpener)
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

// sessionName returns the tmux session name for an agent in the current world.
func (m Model) sessionName(agentName string) string {
	return config.SessionName(m.world, agentName)
}
