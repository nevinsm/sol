package dash

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/status"
)

// captureInterval is how frequently we refresh the tmux pane capture.
const captureInterval = 250 * time.Millisecond

// defaultListWidth is the character width of the left panel in peek mode.
const defaultListWidth = 22

// peekMsg signals a transition into peek mode.
type peekMsg struct {
	items         []peekItem
	initialCursor int
	fromView      viewMode // viewSphere or viewWorld (for esc return)
	world         string   // world name (empty if from sphere)
}

// captureTickMsg triggers a capture refresh in peek mode.
type captureTickMsg time.Time

// captureResultMsg delivers the capture output asynchronously.
type captureResultMsg struct {
	content string
	err     error
}

// peekItem represents one item in the peek list.
type peekItem struct {
	name        string
	sessionName string
	category    string // "Outposts", "Envoys", "Processes"
	state       string
	alive       bool
	peekable    bool   // has a tmux session that is alive
	isForge     bool   // forge item — shows idle state info when no active merge
	source      string // event source filter for service peek (e.g., "forge", "sentinel")
}

// peekModel handles the peek split-pane view.
type peekModel struct {
	width, height int

	// Items to peek at, grouped by category.
	items  []peekItem
	cursor int

	// Capture state.
	capture    string    // latest capture content
	captureAge time.Time // when captured
	sessionMgr *session.Manager

	// Source context.
	fromView viewMode // viewSphere or viewWorld (for esc return)
	world    string   // world name (for world-sourced peeks)

	// Left panel width.
	listWidth int

	// Scroll offset for the item list.
	scrollOffset int

	// Spinners for alive items.
	itemSpinners map[string]spinner.Model

	// Forge peek state.
	forgeFeed  *feedModel          // dedicated forge-filtered feed (nil when not forge peek)
	forgeInfo  *status.ForgeInfo   // forge heartbeat data for idle state display
	solHome    string              // needed for forge feed initialization

	// Source-filtered feed for non-peekable items with a source (e.g., sphere processes).
	sourceFeed *feedModel // nil when selected item has no source or is peekable
}

func newPeekModel(mgr *session.Manager, solHome string) peekModel {
	return peekModel{
		sessionMgr:   mgr,
		listWidth:    defaultListWidth,
		itemSpinners: make(map[string]spinner.Model),
		solHome:      solHome,
	}
}

// enter sets up the peek model with items and initial cursor, returning
// a tea.Cmd to schedule the initial spinner tick.
func (pm *peekModel) enter(msg peekMsg) tea.Cmd {
	pm.items = msg.items
	pm.cursor = msg.initialCursor
	pm.fromView = msg.fromView
	pm.world = msg.world
	pm.capture = ""
	pm.captureAge = time.Time{}
	pm.scrollOffset = 0
	pm.forgeFeed = nil
	pm.forgeInfo = nil
	pm.sourceFeed = nil

	// Clamp cursor.
	if pm.cursor >= len(pm.items) {
		pm.cursor = len(pm.items) - 1
	}
	if pm.cursor < 0 {
		pm.cursor = 0
	}

	// Initialize forge feed if the selected item is forge.
	pm.syncForgeFeed()
	// Initialize source feed for non-peekable items with a source.
	pm.syncSourceFeed()

	// Sync spinners for alive items.
	pm.itemSpinners = make(map[string]spinner.Model)
	for _, item := range pm.items {
		if item.alive {
			s := spinner.New()
			s.Spinner = spinner.Dot
			pm.itemSpinners[item.name] = s
		}
	}

	pm.adjustScroll()

	// Schedule initial spinner tick from one representative spinner.
	// s.Tick is a method value (func() tea.Msg) which satisfies tea.Cmd.
	for _, s := range pm.itemSpinners {
		return s.Tick
	}
	return nil
}

// syncForgeFeed initializes or clears the forge-specific feed based on the
// currently selected item.
func (pm *peekModel) syncForgeFeed() {
	if pm.cursor >= len(pm.items) {
		pm.forgeFeed = nil
		return
	}
	item := pm.items[pm.cursor]
	if item.isForge && pm.solHome != "" {
		fm := newFeedModelWithSource(pm.solHome, pm.world, "forge")
		fm.loadInitial()
		pm.forgeFeed = &fm
	} else {
		pm.forgeFeed = nil
	}
}

// syncSourceFeed initializes or clears the source-filtered feed based on the
// currently selected item. Used for non-peekable items with a source field
// (e.g., sphere processes) to show their event feed in the right panel.
func (pm *peekModel) syncSourceFeed() {
	if pm.cursor >= len(pm.items) {
		pm.sourceFeed = nil
		return
	}
	item := pm.items[pm.cursor]
	// Only create a source feed for non-peekable items with a source that
	// aren't handled by the dedicated forge feed.
	if item.source != "" && !item.peekable && !item.isForge && pm.solHome != "" {
		fm := newFeedModelWithSource(pm.solHome, pm.world, item.source)
		fm.loadInitial()
		pm.sourceFeed = &fm
	} else {
		pm.sourceFeed = nil
	}
}

// selectedIsForge returns true if the currently selected peek item is forge.
func (pm peekModel) selectedIsForge() bool {
	if pm.cursor >= len(pm.items) {
		return false
	}
	return pm.items[pm.cursor].isForge
}

// updateForgeData updates forge heartbeat data from world status.
func (pm *peekModel) updateForgeData(data *status.WorldStatus) {
	if data != nil {
		pm.forgeInfo = &data.Forge
	}
}

func (pm peekModel) update(msg tea.KeyMsg) (peekModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if pm.cursor > 0 {
			pm.cursor--
			pm.capture = "" // clear stale capture while switching
			pm.adjustScroll()
			pm.syncForgeFeed()
			pm.syncSourceFeed()
		}

	case "down", "j":
		max := len(pm.items) - 1
		if max < 0 {
			max = 0
		}
		if pm.cursor < max {
			pm.cursor++
			pm.capture = ""
			pm.adjustScroll()
			pm.syncForgeFeed()
			pm.syncSourceFeed()
		}

	case "enter", "a":
		// Attach to the selected item's session.
		return pm.handleAttach()

	case "esc", "h":
		// Return to previous view.
		return pm, pm.popCmd()

	case "r":
		// Force capture refresh — handled by returning a capture command.
		return pm, pm.captureCmd()
	}

	return pm, nil
}

// handleAttach returns an attach command for the selected item.
func (pm peekModel) handleAttach() (peekModel, tea.Cmd) {
	if pm.cursor >= len(pm.items) {
		return pm, nil
	}
	item := pm.items[pm.cursor]
	if !item.peekable || !item.alive {
		return pm, func() tea.Msg { return noSessionMsg{} }
	}
	sessName := item.sessionName
	return pm, func() tea.Msg {
		return attachMsg{sessionName: sessName}
	}
}

// popCmd returns a command to exit peek mode back to the previous view.
func (pm peekModel) popCmd() tea.Cmd {
	return func() tea.Msg { return peekPopMsg{} }
}

// peekPopMsg signals exiting peek mode back to the previous view.
type peekPopMsg struct{}

// captureCmd returns a command that captures the selected item's pane.
func (pm peekModel) captureCmd() tea.Cmd {
	if pm.sessionMgr == nil || pm.cursor >= len(pm.items) {
		return nil
	}
	item := pm.items[pm.cursor]
	if !item.peekable || item.sessionName == "" {
		return nil
	}
	mgr := pm.sessionMgr
	sessName := item.sessionName
	return func() tea.Msg {
		content, err := mgr.Capture(sessName, 0)
		return captureResultMsg{content: content, err: err}
	}
}

// captureTickCmd schedules the next capture tick.
func captureTickCmd() tea.Cmd {
	return tea.Tick(captureInterval, func(t time.Time) tea.Msg {
		return captureTickMsg(t)
	})
}

// updateSpinner routes spinner ticks to peek item spinners.
func (pm peekModel) updateSpinner(msg spinner.TickMsg) (peekModel, tea.Cmd) {
	var cmds []tea.Cmd
	for name, s := range pm.itemSpinners {
		var cmd tea.Cmd
		s, cmd = s.Update(msg)
		pm.itemSpinners[name] = s
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return pm, tea.Batch(cmds...)
}

// view renders the peek split-pane layout.
func (pm peekModel) view(feedView string) string {
	if len(pm.items) == 0 {
		return "No items to peek.\n" + feedView
	}

	// When forge is selected, use its dedicated filtered feed instead of the
	// parent's world feed.
	fv := feedView
	if pm.selectedIsForge() && pm.forgeFeed != nil {
		fv = pm.forgeFeed.view(pm.width)
	}

	// Calculate panel dimensions.
	// Feed takes some lines at the bottom — estimate from feedView.
	feedLines := strings.Count(fv, "\n")
	if feedLines < 2 {
		feedLines = 2
	}

	// Footer line.
	footerLines := 2

	// Available height for the split pane.
	contentHeight := pm.height - feedLines - footerLines
	if contentHeight < 6 {
		contentHeight = 6
	}

	var b strings.Builder

	// Render the split pane line by line.
	leftLines := pm.renderItemList(contentHeight)
	rightLines := pm.renderCapture(contentHeight)

	rightWidth := pm.width - pm.listWidth - 3 // 3 for "│" separator + padding
	if rightWidth < 10 {
		rightWidth = 10
	}

	for i := 0; i < contentHeight; i++ {
		left := ""
		if i < len(leftLines) {
			left = leftLines[i]
		}
		right := ""
		if i < len(rightLines) {
			right = rightLines[i]
		}

		// Pad left to listWidth, add separator, then right.
		b.WriteString(padRight(left, pm.listWidth))
		b.WriteString(dimStyle.Render("│"))
		b.WriteString(padRight(right, rightWidth))
		b.WriteString("\n")
	}

	// Footer.
	b.WriteString(pm.renderFooter())

	// Feed.
	b.WriteString(fv)

	return b.String()
}

// renderSourceFeedPane renders a source-filtered event feed in the right panel
// for non-peekable items that have a source field (e.g., sphere processes).
func (pm peekModel) renderSourceFeedPane(maxHeight, maxWidth int) []string {
	if pm.cursor >= len(pm.items) {
		return nil
	}
	item := pm.items[pm.cursor]

	header := " " + focusStyle.Render(item.name) + "  " + headerStyle.Render("Events")
	lines := []string{header}

	if pm.sourceFeed == nil || len(pm.sourceFeed.events) == 0 {
		lines = append(lines, "")
		lines = append(lines, " "+dimStyle.Render(fmt.Sprintf("No recent %s events", item.source)))
		for len(lines) < maxHeight {
			lines = append(lines, "")
		}
		return lines
	}

	// Show events most-recent-first, filling available height.
	availHeight := maxHeight - 1 // minus header
	shown := availHeight
	if shown > len(pm.sourceFeed.events) {
		shown = len(pm.sourceFeed.events)
	}

	level := pm.sourceFeed.fadeLevel()
	highlightThreshold := len(pm.sourceFeed.events) - pm.sourceFeed.newCount

	for i := len(pm.sourceFeed.events) - 1; i >= len(pm.sourceFeed.events)-shown; i-- {
		line := formatEvent(pm.sourceFeed.events[i], maxWidth)
		if pm.sourceFeed.newCount > 0 && i >= highlightThreshold && level > 0 {
			lines = append(lines, feedHighlightAtLevel(level).Render(line))
		} else {
			lines = append(lines, dimStyle.Render(line))
		}
	}

	// Pad to maxHeight.
	for len(lines) < maxHeight {
		lines = append(lines, "")
	}

	return lines
}


// renderItemList renders the left panel item list with categories.
func (pm peekModel) renderItemList(maxHeight int) []string {
	var lines []string

	// Build all lines first (with category headers).
	type lineEntry struct {
		text       string
		isHeader   bool
		itemIndex  int // -1 for headers
	}
	var allEntries []lineEntry
	lastCategory := ""
	for i, item := range pm.items {
		if item.category != lastCategory {
			// Category header.
			allEntries = append(allEntries, lineEntry{
				text:      headerStyle.Render("── " + item.category + " "),
				isHeader:  true,
				itemIndex: -1,
			})
			lastCategory = item.category
		}
		allEntries = append(allEntries, lineEntry{
			text:      pm.renderItem(item, i == pm.cursor),
			isHeader:  false,
			itemIndex: i,
		})
	}

	// Find the line index of the cursor.
	cursorLine := 0
	for i, entry := range allEntries {
		if entry.itemIndex == pm.cursor {
			cursorLine = i
			break
		}
	}

	// Apply viewport windowing around the cursor.
	start := 0
	if len(allEntries) > maxHeight {
		// Center the cursor in the viewport.
		start = cursorLine - maxHeight/2
		if start < 0 {
			start = 0
		}
		if start+maxHeight > len(allEntries) {
			start = len(allEntries) - maxHeight
		}
		if start < 0 {
			start = 0
		}
	}

	end := start + maxHeight
	if end > len(allEntries) {
		end = len(allEntries)
	}

	for i := start; i < end; i++ {
		lines = append(lines, allEntries[i].text)
	}

	// Scroll indicators.
	if start > 0 && len(lines) > 0 {
		lines[0] = padRight(lines[0], pm.listWidth-2) + dimStyle.Render("↑")
	}
	if end < len(allEntries) && len(lines) > 0 {
		lines[len(lines)-1] = padRight(lines[len(lines)-1], pm.listWidth-2) + dimStyle.Render("↓")
	}

	return lines
}

// renderItem renders a single item line for the left panel.
func (pm peekModel) renderItem(item peekItem, selected bool) string {
	// Build the indicator.
	indicator := " "
	if item.alive {
		if s, ok := pm.itemSpinners[item.name]; ok {
			indicator = s.View()
		}
	}

	// State suffix.
	state := ""
	if item.state != "" {
		state = " " + item.state
	}

	// Truncate name to fit in the list width.
	maxNameLen := pm.listWidth - 6 // space for indicator + padding
	name := item.name
	if len(name) > maxNameLen {
		name = name[:maxNameLen-1] + "…"
	}

	line := fmt.Sprintf(" %s %s", indicator, name)
	if state != "" {
		// Only add state if there's room.
		remaining := pm.listWidth - len(line) - 1
		if remaining > 2 && len(state) <= remaining {
			line += dimStyle.Render(state)
		}
	}

	if selected {
		return selectStyle.Render(padRight(line, pm.listWidth))
	}
	if !item.alive && item.peekable {
		return dimStyle.Render(line)
	}
	return line
}

// renderCapture renders the right panel with captured terminal content.
func (pm peekModel) renderCapture(maxHeight int) []string {
	if pm.cursor >= len(pm.items) {
		return nil
	}

	item := pm.items[pm.cursor]
	rightWidth := pm.width - pm.listWidth - 3
	if rightWidth < 10 {
		rightWidth = 10
	}

	// Header line: item name.
	header := " " + focusStyle.Render(item.name)
	lines := []string{header}

	if !item.peekable || !item.alive {
		if item.isForge {
			// Forge idle state — show heartbeat info.
			lines = append(lines, "")
			lines = append(lines, " "+dimStyle.Render("No active merge session"))
			if pm.forgeInfo != nil {
				lines = append(lines, "")
				if pm.forgeInfo.LastMerge != "" {
					lines = append(lines, " "+dimStyle.Render(fmt.Sprintf("Last merge: %s ago", pm.forgeInfo.LastMerge)))
				}
				if pm.forgeInfo.CurrentMR != "" {
					lines = append(lines, " "+dimStyle.Render(fmt.Sprintf("Current: %s (%s)", pm.forgeInfo.CurrentMR, pm.forgeInfo.CurrentWrit)))
				}
				if pm.forgeInfo.QueueDepth > 0 {
					lines = append(lines, " "+dimStyle.Render(fmt.Sprintf("Queue: %d ready", pm.forgeInfo.QueueDepth)))
				}
				if pm.forgeInfo.MergesTotal > 0 {
					lines = append(lines, " "+dimStyle.Render(fmt.Sprintf("Total merges: %d", pm.forgeInfo.MergesTotal)))
				}
				if pm.forgeInfo.LastError != "" {
					lines = append(lines, " "+errorStyle.Render(fmt.Sprintf("Last error: %s", truncateStr(pm.forgeInfo.LastError, rightWidth-14))))
				}
				if pm.forgeInfo.Paused {
					lines = append(lines, " "+warnStyle.Render("⏸ Forge is paused"))
				}
			}
		} else if item.source != "" && pm.sourceFeed != nil {
			// If the item has a source, show a source-filtered event feed
			// instead of the generic "No active session" message.
			return pm.renderSourceFeedPane(maxHeight, rightWidth)
		} else {
			// Show "No active session" message.
			lines = append(lines, "")
			lines = append(lines, " "+dimStyle.Render("No active session"))
		}
		// Pad to maxHeight.
		for len(lines) < maxHeight {
			lines = append(lines, "")
		}
		return lines
	}

	if pm.capture == "" {
		lines = append(lines, "")
		lines = append(lines, " "+dimStyle.Render("Capturing..."))
		for len(lines) < maxHeight {
			lines = append(lines, "")
		}
		return lines
	}

	// Split capture into lines and show tail (most recent output).
	capLines := strings.Split(pm.capture, "\n")
	availHeight := maxHeight - 1 // minus header

	// Tail behavior: show last N lines.
	if len(capLines) > availHeight {
		capLines = capLines[len(capLines)-availHeight:]
	}

	for _, cl := range capLines {
		// Truncate to fit panel width.
		if len(cl) > rightWidth-1 {
			cl = cl[:rightWidth-1]
		}
		lines = append(lines, " "+cl)
	}

	// Pad to maxHeight.
	for len(lines) < maxHeight {
		lines = append(lines, "")
	}

	return lines
}

// renderFooter renders the peek mode footer.
func (pm peekModel) renderFooter() string {
	return dimStyle.Render("  ↑↓ cycle · enter attach · a attach · esc back · r refresh") + "\n"
}

// refreshItems updates the peek item list from fresh data without resetting
// cursor position or causing spinner flicker. Called when the underlying view's
// data refreshes while peek mode is active.
func (pm *peekModel) refreshItems(items []peekItem) {
	pm.items = items

	// Clamp cursor if list shrank.
	if pm.cursor >= len(pm.items) {
		pm.cursor = len(pm.items) - 1
	}
	if pm.cursor < 0 {
		pm.cursor = 0
	}

	// Sync spinners: keep existing, add new, remove gone.
	active := make(map[string]bool)
	for _, item := range items {
		if item.alive {
			active[item.name] = true
			if _, ok := pm.itemSpinners[item.name]; !ok {
				s := spinner.New()
				s.Spinner = spinner.Dot
				pm.itemSpinners[item.name] = s
			}
		}
	}
	for name := range pm.itemSpinners {
		if !active[name] {
			delete(pm.itemSpinners, name)
		}
	}

	pm.adjustScroll()
}

// adjustScroll updates the scroll offset for the item list.
func (pm *peekModel) adjustScroll() {
	vpHeight := pm.viewportHeight()
	if pm.cursor < pm.scrollOffset {
		pm.scrollOffset = pm.cursor
	}
	if pm.cursor >= pm.scrollOffset+vpHeight {
		pm.scrollOffset = pm.cursor - vpHeight + 1
	}
}

// viewportHeight returns the number of visible item rows.
func (pm peekModel) viewportHeight() int {
	// Conservative estimate — will be refined during render.
	vp := pm.height - 10
	if vp < 6 {
		vp = 6
	}
	return vp
}
