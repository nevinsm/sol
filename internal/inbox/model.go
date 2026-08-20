package inbox

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/events"
)

const refreshInterval = 3 * time.Second

// highlightTickInterval controls how often action flash highlights decay.
const highlightTickInterval = 400 * time.Millisecond

// detailPageSize is how many lines pgup/pgdn scroll the detail viewport.
const detailPageSize = 10

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
	// Identity is the caller identity items are scoped to — see
	// FetchItems for the autarch-vs-other-identity behavior split.
	Identity string
}

// Model is the root Bubble Tea model for the inbox TUI.
type Model struct {
	config Config
	ready  bool
	width  int
	height int

	// Data. items is the TUI-display list: FetchItems's result run through
	// groupThreads, so mail sharing a thread_id collapses to one row. The
	// --json path (cmd/inbox.go) calls FetchItems directly and never sees
	// this grouping.
	items     []InboxItem
	fetchErr  string // non-empty when the last fetch encountered errors
	actionErr string // non-empty when the last action encountered an error

	// Navigation.
	view         viewMode
	cursor       int
	scrollOffset int // list view: offset into buildListRows(items), not items directly

	// Detail view pinning (see updateListKeys "enter" and the refreshMsg
	// handler below). pinnedID identifies the item by ID rather than by
	// list position, so a refresh that reorders or removes other items
	// never silently swaps which item the detail pane shows.
	pinnedID     string
	detailScroll int    // line offset into the pinned item's detail content
	detailNotice string // one-shot dim notice shown in list view, e.g. "item resolved elsewhere"

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
		m.clampDetailScroll()

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
			m.clampDetailScroll()
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
		// Re-locate the pinned item by ID rather than trusting the cursor
		// position — the list can reorder or shrink between refreshes as
		// other items are acked/resolved/dismissed elsewhere. Only when the
		// pinned item itself is gone do we drop back to the list.
		if m.view == viewDetail {
			if _, ok := findItemByID(m.items, m.pinnedID); !ok {
				m.view = viewList
				m.pinnedID = ""
				m.detailScroll = 0
				m.detailNotice = "item resolved elsewhere"
			}
		}
		if m.cursor >= len(m.items) {
			m.cursor = max(0, len(m.items)-1)
		}
		m.clampScroll()
		m.clampDetailScroll()

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

// selectedItem returns the item under the list-view cursor, if any.
func (m *Model) selectedItem() (InboxItem, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return InboxItem{}, false
	}
	return m.items[m.cursor], true
}

// pinnedItem returns the item pinned in detail view, re-located by ID.
func (m *Model) pinnedItem() (InboxItem, bool) {
	return findItemByID(m.items, m.pinnedID)
}

// updateListKeys handles key presses in list view.
func (m *Model) updateListKeys(msg tea.KeyMsg) tea.Cmd {
	// Any keypress dismisses the one-shot "item resolved elsewhere" notice.
	m.detailNotice = ""

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
		if it, ok := m.selectedItem(); ok {
			m.view = viewDetail
			m.pinnedID = it.ID
			m.detailScroll = 0
			// Mark the item as read in the underlying store when the
			// operator opens it. readCmd is a no-op for escalations.
			return readCmd(m.config.Store, it)
		}

	case "a":
		if it, ok := m.selectedItem(); ok {
			return ackCmd(m.config.Store, it, m.config.EventLogger)
		}

	case "r":
		// Resolve only applies to escalations — the footer only advertises
		// it for that selection, so a mail selection is a silent no-op.
		if it, ok := m.selectedItem(); ok && it.Type == ItemEscalation {
			return resolveCmd(m.config.Store, it, m.config.EventLogger)
		}

	case "d":
		// Dismiss only applies to mail — the footer only advertises it for
		// that selection, so an escalation selection is a silent no-op.
		if it, ok := m.selectedItem(); ok && it.Type == ItemMail {
			return dismissCmd(m.config.Store, it)
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
		m.pinnedID = ""
		m.detailScroll = 0

	case "up", "k":
		if m.detailScroll > 0 {
			m.detailScroll--
		}

	case "down", "j":
		m.detailScroll++

	case "pgup":
		m.detailScroll -= detailPageSize
		if m.detailScroll < 0 {
			m.detailScroll = 0
		}

	case "pgdown":
		m.detailScroll += detailPageSize

	case "a":
		if it, ok := m.pinnedItem(); ok {
			return ackCmd(m.config.Store, it, m.config.EventLogger)
		}

	case "r":
		if it, ok := m.pinnedItem(); ok && it.Type == ItemEscalation {
			return resolveCmd(m.config.Store, it, m.config.EventLogger)
		}

	case "d":
		if it, ok := m.pinnedItem(); ok && it.Type == ItemMail {
			return dismissCmd(m.config.Store, it)
		}
	}

	return nil
}

// View renders the active view.
func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	if m.view == viewDetail {
		if it, ok := findItemByID(m.items, m.pinnedID); ok {
			return renderDetailView(it, m.width, m.height, m.actionErr, m.detailScroll, m.config.Identity)
		}
		// Safety fallback: pinned item vanished but view wasn't flipped yet.
		// (The refreshMsg handler transitions m.view to viewList when this
		// happens; this path exists only in case some other code path
		// reaches here first.)
	}
	return renderListView(m.items, m.cursor, m.scrollOffset, m.width, m.height, m.highlights, m.fetchErr, m.actionErr, m.detailNotice, m.config.Identity)
}

// refreshMsg carries fetched items back to the model.
type refreshMsg struct {
	items []InboxItem
	err   error
}

// refresh fetches fresh data in a tea.Cmd. groupThreads and sectionOrder
// are applied here — after FetchItems, before the model stores the result
// — so the TUI's notion of "items" (what the cursor addresses, what
// actions operate on, what buildListRows renders) already reflects thread
// grouping and is escalations-block-then-mail-block ordered.
func (m Model) refresh() tea.Cmd {
	return func() tea.Msg {
		items, err := FetchItems(m.config.Store, m.config.Identity)
		return refreshMsg{items: sectionOrder(groupThreads(items)), err: err}
	}
}

// clampScroll adjusts scrollOffset so the cursor's row stays visible
// within the list viewport. scrollOffset and the viewport are measured in
// display rows (buildListRows), which include section header rows, not
// raw item indices.
func (m *Model) clampScroll() {
	rows := buildListRows(m.items)
	viewportHeight := max(1, m.height-5)

	cursorRow := rowIndexForItem(rows, m.cursor)
	if cursorRow < m.scrollOffset {
		m.scrollOffset = cursorRow
	}
	if cursorRow >= m.scrollOffset+viewportHeight {
		m.scrollOffset = cursorRow - viewportHeight + 1
	}

	maxOffset := max(0, len(rows)-viewportHeight)
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

// clampDetailScroll keeps detailScroll within the pinned item's content
// bounds, e.g. after a resize or after content changes on refresh.
func (m *Model) clampDetailScroll() {
	it, ok := m.pinnedItem()
	if !ok {
		m.detailScroll = 0
		return
	}
	lines := detailContentLines(it, m.width)
	viewportHeight := detailViewportHeight(m.height)

	maxScroll := len(lines) - viewportHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.detailScroll > maxScroll {
		m.detailScroll = maxScroll
	}
	if m.detailScroll < 0 {
		m.detailScroll = 0
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
