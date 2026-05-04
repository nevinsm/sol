package inbox

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/events"
)

const refreshInterval = 3 * time.Second

// highlightTickInterval controls how often action flash highlights decay.
const highlightTickInterval = 400 * time.Millisecond

// viewMode tracks list vs detail view.
type viewMode int

const (
	viewList viewMode = iota
	viewDetail
)

// dataTickMsg triggers a data refresh.
type dataTickMsg time.Time

// highlightTickMsg triggers highlight level decay.
type highlightTickMsg time.Time

// Config holds dependencies for the inbox TUI.
type Config struct {
	Store       DataSource
	EventLogger *events.Logger
}

// Model is the root Bubble Tea model for the inbox TUI.
type Model struct {
	config Config
	ready  bool
	width  int
	height int

	// Data.
	items     []InboxItem
	fetchErr  string // non-empty when the last fetch encountered errors
	actionErr string // non-empty when the last action encountered an error

	// Navigation.
	view         viewMode
	cursor       int
	scrollOffset int

	// Action flash highlights (item ID -> decay level).
	highlights          map[string]int
	highlightTickActive bool
}

// NewModel creates an inbox model.
func NewModel(cfg Config) Model {
	return Model{
		config:     cfg,
		highlights: make(map[string]int),
	}
}

// Init starts the first data fetch and tick schedulers.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.refresh(),
		dataTickCmd(),
	)
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.clampScroll()

	case tea.KeyMsg:
		switch m.view {
		case viewList:
			cmd := m.updateListKeys(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			m.clampScroll()
		case viewDetail:
			cmd := m.updateDetailKeys(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case dataTickMsg:
		cmds = append(cmds, m.refresh(), dataTickCmd())

	case highlightTickMsg:
		m.decayHighlights()
		if len(m.highlights) > 0 {
			cmds = append(cmds, highlightTickCmd())
		} else {
			m.highlightTickActive = false
		}

	case refreshMsg:
		m.items = msg.items
		if msg.err != nil {
			m.fetchErr = msg.err.Error()
		} else {
			m.fetchErr = ""
		}
		// Clamp cursor and transition out of detail view if the selected item
		// no longer exists (e.g. last escalation resolved while in detail view).
		if m.cursor >= len(m.items) {
			m.cursor = max(0, len(m.items)-1)
			m.view = viewList
		}
		m.clampScroll()

	case actionResultMsg:
		if msg.err == nil {
			m.actionErr = ""
			m.highlights[msg.itemID] = highlightMaxLevel
			// Start highlight decay if not already running.
			if !m.highlightTickActive {
				m.highlightTickActive = true
				cmds = append(cmds, highlightTickCmd())
			}
			// Immediate refresh to reflect changes.
			cmds = append(cmds, m.refresh())
		} else {
			m.actionErr = msg.action + " failed: " + msg.err.Error()
		}
	}

	return m, tea.Batch(cmds...)
}

// updateListKeys handles key presses in list view.
func (m *Model) updateListKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q", "ctrl+c":
		return tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}

	case "enter":
		if len(m.items) > 0 && m.cursor < len(m.items) {
			m.view = viewDetail
			// Mark the item as read in the underlying store when the
			// operator opens it. readCmd is a no-op for escalations.
			return readCmd(m.config.Store, m.items[m.cursor])
		}

	case "a":
		if len(m.items) > 0 && m.cursor < len(m.items) {
			return ackCmd(m.config.Store, m.items[m.cursor], m.config.EventLogger)
		}

	case "r":
		if len(m.items) > 0 && m.cursor < len(m.items) {
			return resolveCmd(m.config.Store, m.items[m.cursor], m.config.EventLogger)
		}

	case "d":
		if len(m.items) > 0 && m.cursor < len(m.items) {
			return dismissCmd(m.config.Store, m.items[m.cursor])
		}
	}

	return nil
}

// updateDetailKeys handles key presses in detail view.
func (m *Model) updateDetailKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q", "ctrl+c":
		return tea.Quit

	case "esc", "backspace":
		m.view = viewList

	case "a":
		if len(m.items) > 0 && m.cursor < len(m.items) {
			return ackCmd(m.config.Store, m.items[m.cursor], m.config.EventLogger)
		}

	case "r":
		if len(m.items) > 0 && m.cursor < len(m.items) {
			return resolveCmd(m.config.Store, m.items[m.cursor], m.config.EventLogger)
		}

	case "d":
		if len(m.items) > 0 && m.cursor < len(m.items) {
			return dismissCmd(m.config.Store, m.items[m.cursor])
		}
	}

	return nil
}

// View renders the active view.
func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	switch m.view {
	case viewDetail:
		if m.cursor < len(m.items) {
			return renderDetailView(m.items[m.cursor], m.width, m.height)
		}
		// Cursor is out of bounds — fall through to list view.
		// (The Update handler transitions m.view to viewList on refreshMsg;
		// this path is a safety fallback for any other code path.)
		return renderListView(m.items, m.cursor, m.scrollOffset, m.width, m.height, m.highlights, m.fetchErr, m.actionErr)
	default:
		return renderListView(m.items, m.cursor, m.scrollOffset, m.width, m.height, m.highlights, m.fetchErr, m.actionErr)
	}
}

// refreshMsg carries fetched items back to the model.
type refreshMsg struct {
	items []InboxItem
	err   error
}

// refresh fetches fresh data in a tea.Cmd.
func (m Model) refresh() tea.Cmd {
	return func() tea.Msg {
		items, err := FetchItems(m.config.Store)
		return refreshMsg{items: items, err: err}
	}
}

// clampScroll adjusts scrollOffset so the cursor stays visible within the viewport.
func (m *Model) clampScroll() {
	viewportHeight := max(1, m.height-5)
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+viewportHeight {
		m.scrollOffset = m.cursor - viewportHeight + 1
	}
}

// decayHighlights decrements all highlight levels by one step.
func (m *Model) decayHighlights() {
	for id, level := range m.highlights {
		if level <= 1 {
			delete(m.highlights, id)
		} else {
			m.highlights[id] = level - 1
		}
	}
}

// dataTickCmd schedules the next data refresh tick.
func dataTickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return dataTickMsg(t)
	})
}

// highlightTickCmd schedules the next highlight decay tick.
func highlightTickCmd() tea.Cmd {
	return tea.Tick(highlightTickInterval, func(t time.Time) tea.Msg {
		return highlightTickMsg(t)
	})
}
