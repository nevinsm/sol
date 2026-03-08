package dash

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
)

const refreshInterval = 3 * time.Second

// animInterval is the animation tick cadence (~30 FPS).
const animInterval = 33 * time.Millisecond

// captureInterval is reserved for peek-mode capture ticks.
const captureInterval = 250 * time.Millisecond

// highlightDuration is how long a state-change highlight persists.
const highlightDuration = 6 * time.Second

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
)

// Config holds dependencies for the dashboard, mirroring cmd/status.go.
type Config struct {
	SphereStore  sphereStore
	WorldOpener  func(string) (*store.Store, error)
	SessionCheck status.SessionChecker
	CaravanStore caravanStore

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

// captureTickMsg is reserved for peek-mode dashboard capture.
type captureTickMsg time.Time

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

	// Activity feed.
	feed feedModel

	// Help overlay.
	showHelp bool

	// Dirty flag — prevents unnecessary re-renders at 30 FPS.
	// viewCache is a pointer so View() (value receiver) can write through it.
	dirty     bool
	viewCache *string

	// State-change highlight tracking (time-based).
	prevSphereHealth   string
	prevWorldHealth    string
	healthHighlightEnd time.Time // zero value = no highlight

	// Agent state tracking for highlights.
	prevAgentStates   map[string]string    // agentName -> previous state
	agentHighlightEnd map[string]time.Time // agentName -> highlight expiry
}

// NewModel creates a dashboard model. If world is empty, starts in sphere view.
func NewModel(cfg Config) Model {
	startView := viewSphere
	if cfg.World != "" {
		startView = viewWorld
	}

	viewCache := ""
	m := Model{
		viewStack:         []viewMode{startView},
		world:             cfg.World,
		config:            cfg,
		feed:              newFeedModel(cfg.SOLHome, cfg.World),
		prevAgentStates:   make(map[string]string),
		agentHighlightEnd: make(map[string]time.Time),
		viewCache:         &viewCache,
	}

	m.sphereView = newSphereModel()
	m.worldView = newWorldModel()

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
		m.feed.setHeight(msg.Height)
		m.ready = true
		m.dirty = true

	case tea.KeyMsg:
		m.dirty = true

		// Help overlay: any key dismisses it.
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			return m, m.refresh()
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
			// Re-gather sphere data (ZFC — fresh on return).
			cmds = append(cmds, m.refresh())
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
		// Route to world view to show inline message.
		m.worldView.showNoSession = true

	case animTickMsg:
		// Animation tick (~30 FPS) — drives visual state.
		m.dirty = true
		m.decayHighlights()
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

	case dataMsg:
		m.dirty = true
		m.lastRefresh = time.Now()
		if msg.sphere != nil {
			m.trackSphereHighlights(msg.sphere)
			m.sphereData = msg.sphere
			m.sphereView.updateData(m.sphereData)
		}
		if msg.world != nil {
			m.trackWorldHighlights(msg.world)
			m.worldData = msg.world
			m.worldView.updateData(m.worldData)
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

	// Help overlay.
	if m.showHelp {
		return helpOverlay(m.width, m.height)
	}

	// Short-circuit if nothing changed since last render.
	if !m.dirty && m.viewCache != nil && *m.viewCache != "" {
		return *m.viewCache
	}

	now := time.Now()
	healthEmphasis := now.Before(m.healthHighlightEnd)

	// Compute active agent highlights from time-based map.
	agentHighlights := make(map[string]int)
	for name, end := range m.agentHighlightEnd {
		if now.Before(end) {
			agentHighlights[name] = 1
		}
	}

	var content string
	switch m.activeView() {
	case viewSphere:
		content = m.sphereView.view(m.sphereData, m.lastRefresh, healthEmphasis)
	case viewWorld:
		content = m.worldView.view(m.worldData, m.lastRefresh, healthEmphasis, agentHighlights)
	default:
		content = "Unknown view"
	}

	// Append feed panel.
	content += m.feed.view(m.width)

	// Cache the rendered view for dirty-flag optimization.
	if m.viewCache != nil {
		*m.viewCache = content
	}

	return content
}

// trackSphereHighlights detects health changes in sphere data.
func (m *Model) trackSphereHighlights(data *status.SphereStatus) {
	if m.prevSphereHealth != "" && data.Health != m.prevSphereHealth {
		m.healthHighlightEnd = time.Now().Add(highlightDuration)
	}
	m.prevSphereHealth = data.Health
}

// trackWorldHighlights detects health and agent state changes in world data.
func (m *Model) trackWorldHighlights(data *status.WorldStatus) {
	newHealth := data.HealthString()
	if m.prevWorldHealth != "" && newHealth != m.prevWorldHealth {
		m.healthHighlightEnd = time.Now().Add(highlightDuration)
	}
	m.prevWorldHealth = newHealth

	// Track agent state changes.
	for _, a := range data.Agents {
		prev, exists := m.prevAgentStates[a.Name]
		if exists && prev != a.State {
			m.agentHighlightEnd[a.Name] = time.Now().Add(highlightDuration)
		}
		m.prevAgentStates[a.Name] = a.State
	}
}

// decayHighlights removes expired highlight entries.
func (m *Model) decayHighlights() {
	now := time.Now()
	for name, end := range m.agentHighlightEnd {
		if now.After(end) {
			delete(m.agentHighlightEnd, name)
		}
	}
}

// refresh gathers fresh data in a command.
func (m Model) refresh() tea.Cmd {
	return func() tea.Msg {
		var msg dataMsg

		switch m.activeView() {
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

// sessionName returns the tmux session name for an agent in the current world.
func (m Model) sessionName(agentName string) string {
	return config.SessionName(m.world, agentName)
}
