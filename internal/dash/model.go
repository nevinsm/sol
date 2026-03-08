package dash

import (
	"os/exec"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
)

const refreshInterval = 3 * time.Second

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

// tickMsg triggers a data refresh.
type tickMsg time.Time

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
}

// NewModel creates a dashboard model. If world is empty, starts in sphere view.
func NewModel(cfg Config) Model {
	startView := viewSphere
	if cfg.World != "" {
		startView = viewWorld
	}

	m := Model{
		viewStack: []viewMode{startView},
		world:     cfg.World,
		config:    cfg,
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

// Init starts the first data fetch and tick timer.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.refresh(),
		m.sphereView.init(),
		m.worldView.init(),
	)
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.sphereView.width = msg.Width
		m.sphereView.height = msg.Height
		m.worldView.width = msg.Width
		m.worldView.height = msg.Height
		m.ready = true

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			return m, m.refresh()
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
		// Push world view onto the stack.
		m.world = msg.world
		m.worldData = nil // clear stale data
		m.worldView = newWorldModel()
		m.worldView.width = m.width
		m.worldView.height = m.height
		m.viewStack = append(m.viewStack, viewWorld)
		cmds = append(cmds, m.refresh(), m.worldView.init())

	case popMsg:
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
		// Resume after detach — force immediate refresh.
		cmds = append(cmds, m.refresh())

	case noSessionMsg:
		// Route to world view to show inline message.
		m.worldView.showNoSession = true

	case tickMsg:
		cmds = append(cmds, m.refresh())

	case dataMsg:
		m.lastRefresh = time.Now()
		if msg.sphere != nil {
			m.sphereData = msg.sphere
			m.sphereView.updateData(m.sphereData)
		}
		if msg.world != nil {
			m.worldData = msg.world
			m.worldView.updateData(m.worldData)
		}
		// Schedule next tick.
		cmds = append(cmds, m.tickCmd())

	case spinner.TickMsg:
		// Route spinner ticks to active view.
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

// View renders the active view.
func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	switch m.activeView() {
	case viewSphere:
		return m.sphereView.view(m.sphereData, m.lastRefresh)
	case viewWorld:
		return m.worldView.view(m.worldData, m.lastRefresh)
	default:
		return "Unknown view"
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

// tickCmd schedules the next refresh tick.
func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// sessionName returns the tmux session name for an agent in the current world.
func (m Model) sessionName(agentName string) string {
	return config.SessionName(m.world, agentName)
}
