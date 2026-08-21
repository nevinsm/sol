package dash

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/style"
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
	category    string // "Outposts", "Envoys", "Processes", "Active", "Drydocked"
	state       string
	alive       bool
	peekable    bool   // has a tmux session that is alive
	isForge     bool   // forge item — shows idle state info when no active merge
	source      string // event source filter for service peek (e.g., "forge", "sentinel")
	isCaravan   bool   // caravan uses a dedicated item detail table layout
	caravanID   string // caravan ID for looking up detail data
	isWrit      bool   // writ backlog item — shows a scrollable description panel
	writID      string // writ ID for the detail header
	description string // writ description text (isWrit only)
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

	// captureScroll is how many lines above the live tail the capture view
	// is scrolled (0 = live-follow, showing the tail). It is pinned across
	// capture refreshes so incoming output doesn't yank a scrolled-away view
	// back to the bottom — see scrollCapture and renderCapture.
	captureScroll int

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

	// Caravan peek state.
	caravanData []status.CaravanInfo // set when entering caravan peek

	// writScroll is the line offset from the top of the wrapped writ
	// description text for the currently selected writ item (isWrit).
	// Unlike captureScroll (offset from the live tail), 0 here means "top of
	// the description" since there is no live tail for static text — see
	// scrollWritDetail.
	writScroll int
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
	pm.captureScroll = 0
	pm.writScroll = 0
	pm.forgeFeed = nil
	pm.forgeInfo = nil
	pm.sourceFeed = nil
	pm.caravanData = nil

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
// currently selected item. Reuses the existing feed instance when the source
// matches to avoid creating a new feed on every cursor movement.
func (pm *peekModel) syncForgeFeed() {
	if pm.cursor >= len(pm.items) {
		pm.forgeFeed = nil
		return
	}
	item := pm.items[pm.cursor]
	if item.isForge && pm.solHome != "" {
		// Reuse existing forge feed if already initialized.
		if pm.forgeFeed != nil {
			return
		}
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
// Reuses the existing feed instance when the source matches.
func (pm *peekModel) syncSourceFeed() {
	if pm.cursor >= len(pm.items) {
		pm.sourceFeed = nil
		return
	}
	item := pm.items[pm.cursor]
	// Only create a source feed for non-peekable items with a source that
	// aren't handled by the dedicated forge feed.
	if item.source != "" && !item.peekable && !item.isForge && pm.solHome != "" {
		// Reuse existing source feed if the source matches.
		if pm.sourceFeed != nil && pm.sourceFeed.source == item.source {
			return
		}
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

// selectedIsCaravan returns true if the currently selected peek item is a caravan.
func (pm peekModel) selectedIsCaravan() bool {
	if pm.cursor >= len(pm.items) {
		return false
	}
	return pm.items[pm.cursor].isCaravan
}

// selectedIsWrit returns true if the currently selected peek item is a writ
// backlog item (description detail panel).
func (pm peekModel) selectedIsWrit() bool {
	if pm.cursor >= len(pm.items) {
		return false
	}
	return pm.items[pm.cursor].isWrit
}

// selectedIsCapturing returns true if the right panel for the currently
// selected item is rendering live tmux pane capture content (as opposed to
// a static "no active session" message, forge idle info, or a source feed).
func (pm peekModel) selectedIsCapturing() bool {
	if pm.cursor >= len(pm.items) {
		return false
	}
	item := pm.items[pm.cursor]
	return item.peekable && item.alive && !item.isCaravan
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
			pm.captureScroll = 0
			pm.writScroll = 0
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
			pm.captureScroll = 0
			pm.writScroll = 0
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

	case "pgup":
		// Scroll toward older history — increases the offset from the tail.
		if pm.selectedIsCapturing() {
			return pm.scrollCapture(capturePageStep)
		}
		// Writ description: scroll up toward the top (decreases offset).
		if pm.selectedIsWrit() {
			return pm.scrollWritDetail(-capturePageStep)
		}

	case "pgdown":
		// Scroll toward the live tail — decreases the offset (clamped at 0).
		if pm.selectedIsCapturing() {
			return pm.scrollCapture(-capturePageStep)
		}
		// Writ description: scroll down toward the end (increases offset).
		if pm.selectedIsWrit() {
			return pm.scrollWritDetail(capturePageStep)
		}

	case "ctrl+u":
		if pm.selectedIsCapturing() {
			return pm.scrollCapture(captureHalfStep)
		}
		if pm.selectedIsWrit() {
			return pm.scrollWritDetail(-captureHalfStep)
		}

	case "ctrl+d":
		if pm.selectedIsCapturing() {
			return pm.scrollCapture(-captureHalfStep)
		}
		if pm.selectedIsWrit() {
			return pm.scrollWritDetail(captureHalfStep)
		}
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

// captureScrollbackLines is how many lines of pane history to request when
// the capture view is scrolled away from the live tail. 500 lines comfortably
// covers a full agent turn (tool calls, file diffs, test output) worth of
// diagnostic context. It is only requested while scrolled — capturing and
// ANSI-truncating 500 lines on every 250ms tick would be wasted work during
// live-follow, since only the last screenful is ever rendered there; live
// mode keeps requesting lines=0 (just the visible pane, matching prior
// behavior).
const captureScrollbackLines = 500

// capturePageStep is how many lines pgup/pgdn move the capture scroll offset.
// captureHalfStep is the ctrl+u/ctrl+d half-page step — tmux copy-mode
// convention.
const (
	capturePageStep = 20
	captureHalfStep = 10
)

// captureCmd returns a command that captures the selected item's pane. The
// requested depth depends on scroll state: live-follow (captureScroll == 0)
// captures just the visible pane; scrolled-away captures deep history so
// there's a buffer to scroll into (see captureScrollbackLines).
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
	lines := 0
	if pm.captureScroll > 0 {
		lines = captureScrollbackLines
	}
	return func() tea.Msg {
		content, err := mgr.CaptureEscapes(sessName, lines)
		return captureResultMsg{content: content, err: err}
	}
}

// scrollCapture adjusts the capture scrollback offset by delta lines
// (positive moves toward older history, negative moves back toward the live
// tail), clamping at zero so it never scrolls past the tail. Crossing from
// live-follow into scrollback (offset goes from zero to positive) triggers
// an immediate deep capture so there's history to scroll into right away,
// rather than waiting for the next 250ms tick.
func (pm peekModel) scrollCapture(delta int) (peekModel, tea.Cmd) {
	wasLive := pm.captureScroll == 0
	pm.captureScroll += delta
	if pm.captureScroll < 0 {
		pm.captureScroll = 0
	}
	if wasLive && pm.captureScroll > 0 {
		return pm, pm.captureCmd()
	}
	return pm, nil
}

// scrollWritDetail adjusts the writ-description scroll offset by delta lines
// (positive scrolls down toward the end of the description, negative scrolls
// back up toward the top), clamping at zero. Unlike scrollCapture there is no
// upper bound computed here — that depends on the wrapped line count and
// panel height, both only known at render time, so renderWritDetail clamps
// the effective offset there instead.
func (pm peekModel) scrollWritDetail(delta int) (peekModel, tea.Cmd) {
	pm.writScroll += delta
	if pm.writScroll < 0 {
		pm.writScroll = 0
	}
	return pm, nil
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

	// Caravan peek uses a sidebar + item detail table layout.
	if pm.selectedIsCaravan() {
		return pm.viewCaravan()
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

	// Live tmux capture lines are effectively unique on every render tick
	// (timestamps, spinners, cursor position embedded in the pane content),
	// so padding them through the shared widthCache would only thrash it —
	// bypass the cache and measure width directly for this content.
	rightIsCapture := pm.selectedIsCapturing()

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
		if rightIsCapture {
			b.WriteString(padRightNoCache(right, rightWidth))
		} else {
			b.WriteString(padRight(right, rightWidth))
		}
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
	name := style.TruncateWidth(item.name, maxNameLen)

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

// truncateCaptureLine truncates a captured pane line to at most maxWidth
// visible columns. It is ANSI-aware (via github.com/charmbracelet/x/ansi):
// it never splits an escape sequence and measures width by visible cells,
// not bytes or runes, so escape sequences don't inflate the count. If the
// line carries any escape sequences, a style reset is appended so a color
// or attribute left open by truncation (or by the tail of the captured
// line simply not containing its own reset yet) can't bleed into whatever
// is rendered after it in the dashboard frame.
func truncateCaptureLine(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	hasEscapes := strings.Contains(s, "\x1b")
	truncated := ansi.Truncate(s, maxWidth, "")
	if hasEscapes {
		truncated += ansi.ResetStyle
	}
	return truncated
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

	// Caravan items use a dedicated detail table.
	if item.isCaravan {
		return pm.renderCaravanDetail(item, maxHeight, rightWidth)
	}

	// Writ items use a scrollable description text panel.
	if item.isWrit {
		return pm.renderWritDetail(item, maxHeight, rightWidth)
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
					lines = append(lines, " "+errorStyle.Render(fmt.Sprintf("Last error: %s", style.TruncateRunes(pm.forgeInfo.LastError, rightWidth-14))))
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

	// Split capture into lines. By default we show the tail (most recent
	// output); captureScroll shifts the window toward older history without
	// touching what was captured — see scrollCapture. The offset is clamped
	// here (rather than in scrollCapture) since it depends on the buffer
	// size and available height, both of which can change out from under a
	// pinned scroll position as new capture results arrive.
	capLines := strings.Split(pm.capture, "\n")
	availHeight := maxHeight - 1 // minus header

	maxOffset := len(capLines) - availHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset := pm.captureScroll
	if offset > maxOffset {
		offset = maxOffset
	}

	end := len(capLines) - offset
	if end < 0 {
		end = 0
	}
	start := end - availHeight
	if start < 0 {
		start = 0
	}
	capLines = capLines[start:end]

	if offset > 0 {
		lines[0] = header + "  " + warnStyle.Render(fmt.Sprintf("scrolled: %d above live tail", offset))
	}

	for _, cl := range capLines {
		// Truncate to fit panel width. ANSI-aware and rune-boundary safe:
		// capture-pane is invoked with -e (see CaptureEscapes), so lines
		// routinely carry color/style escape sequences on top of the usual
		// unicode spinners, box-drawing, or emoji. truncateCaptureLine never
		// splits an escape sequence, counts only visible width, and resets
		// styling at the end of the line so color can't bleed into the rest
		// of the dashboard frame.
		cl = truncateCaptureLine(cl, rightWidth-1)
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
	if pm.selectedIsCapturing() {
		return dimStyle.Render("  ↑↓ cycle · pgup/pgdn scroll (ctrl+u/ctrl+d) · enter attach · a attach · esc back · r refresh") + "\n"
	}
	if pm.selectedIsWrit() {
		return dimStyle.Render("  ↑↓ cycle · pgup/pgdn scroll (ctrl+u/ctrl+d) · esc back") + "\n"
	}
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

// buildCaravanPeekItems creates peek items for caravans.
func buildCaravanPeekItems(caravans []status.CaravanInfo) []peekItem {
	var items []peekItem

	// Sort active first, then drydocked — same order as rendering.
	var active, drydocked []status.CaravanInfo
	for _, c := range caravans {
		if c.Status == "drydock" {
			drydocked = append(drydocked, c)
		} else {
			active = append(active, c)
		}
	}

	for _, c := range active {
		items = append(items, peekItem{
			name:      c.Name,
			category:  "Active",
			state:     caravanStateSummary(c),
			alive:     c.Status == "open",
			peekable:  false,
			isCaravan: true,
			caravanID: c.ID,
		})
	}
	for _, c := range drydocked {
		items = append(items, peekItem{
			name:      c.Name,
			category:  "Drydocked",
			state:     caravanStateSummary(c),
			alive:     false,
			peekable:  false,
			isCaravan: true,
			caravanID: c.ID,
		})
	}
	return items
}

// caravanStateSummary builds a short state summary for a caravan peek item.
func caravanStateSummary(c status.CaravanInfo) string {
	var parts []string
	if c.ClosedItems > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d merged", c.ClosedItems, c.TotalItems))
	}
	if c.DispatchedItems > 0 {
		parts = append(parts, fmt.Sprintf("%d in progress", c.DispatchedItems))
	}
	if c.ReadyItems > 0 {
		parts = append(parts, fmt.Sprintf("%d ready", c.ReadyItems))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d items", c.TotalItems)
	}
	return strings.Join(parts, ", ")
}

// viewCaravan renders the caravan peek layout: sidebar + item detail table.
func (pm peekModel) viewCaravan() string {
	// Footer line.
	footerLines := 2

	// Available height for the split pane (no feed panel for caravan peek).
	contentHeight := pm.height - footerLines
	if contentHeight < 6 {
		contentHeight = 6
	}

	var b strings.Builder

	// Render the split pane line by line.
	leftLines := pm.renderItemList(contentHeight)
	rightLines := pm.renderCapture(contentHeight)

	rightWidth := pm.width - pm.listWidth - 3 // 3 for separator + padding
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

		b.WriteString(padRight(left, pm.listWidth))
		b.WriteString(dimStyle.Render("│"))
		b.WriteString(padRight(right, rightWidth))
		b.WriteString("\n")
	}

	// Footer (no feed panel for caravan peek).
	b.WriteString(dimStyle.Render("  ↑↓ cycle · esc back") + "\n")

	return b.String()
}

// renderCaravanDetail renders the right panel for a caravan peek item.
func (pm peekModel) renderCaravanDetail(item peekItem, maxHeight, maxWidth int) []string {
	// Find the CaravanInfo for this item.
	var info *status.CaravanInfo
	for i := range pm.caravanData {
		if pm.caravanData[i].ID == item.caravanID {
			info = &pm.caravanData[i]
			break
		}
	}

	if info == nil {
		lines := []string{" " + focusStyle.Render(item.name)}
		lines = append(lines, "")
		lines = append(lines, " "+dimStyle.Render("No caravan data"))
		for len(lines) < maxHeight {
			lines = append(lines, "")
		}
		return lines
	}

	// Header: caravan name + status + progress summary.
	statusStr := info.Status
	switch info.Status {
	case "open":
		statusStr = okStyle.Render("open")
	case "drydock":
		statusStr = dimStyle.Render("drydock")
	case "closed":
		statusStr = dimStyle.Render("closed")
	}
	header := " " + focusStyle.Render(info.Name) + "  " + statusStr + "  " +
		dimStyle.Render(fmt.Sprintf("%d/%d merged", info.ClosedItems, info.TotalItems))
	lines := []string{header, ""}

	if len(info.Items) == 0 {
		lines = append(lines, " "+dimStyle.Render("No items"))
		for len(lines) < maxHeight {
			lines = append(lines, "")
		}
		return lines
	}

	// Column widths.
	pCol := 2   // "P" column
	writCol := 20 // writ ID
	statusCol := 14 // status
	assigneeCol := 12 // assignee
	// Title gets remaining width.
	titleCol := maxWidth - pCol - writCol - statusCol - assigneeCol - 7 // separators + indent
	if titleCol < 10 {
		titleCol = 10
	}

	// Column headers.
	colHeader := " " + padRight(dimStyle.Render("P"), pCol) + "  " +
		padRight(dimStyle.Render("WRIT"), writCol) + "  " +
		padRight(dimStyle.Render("STATUS"), statusCol) + "  " +
		padRight(dimStyle.Render("ASSIGNEE"), assigneeCol) + "  " +
		dimStyle.Render("TITLE")
	lines = append(lines, colHeader)

	// Item rows.
	for _, d := range info.Items {
		phase := fmt.Sprintf("%d", d.Phase)

		writID := style.TruncateWidth(d.WritID, writCol)

		itemStatus := d.Status
		switch d.Status {
		case "open":
			if d.Ready {
				itemStatus = okStyle.Render("ready")
			} else {
				itemStatus = dimStyle.Render("open")
			}
		case "tethered", "working":
			itemStatus = warnStyle.Render("in progress")
		case "resolve":
			itemStatus = warnStyle.Render("resolve")
		case "done":
			itemStatus = headerStyle.Render("done")
		case "closed":
			itemStatus = dimStyle.Render("closed")
		}

		assignee := "—"
		if d.Assignee != "" {
			// Strip world prefix (e.g., "sol-dev/Nova" → "Nova").
			assignee = d.Assignee
			if idx := strings.LastIndex(assignee, "/"); idx >= 0 {
				assignee = assignee[idx+1:]
			}
		}
		assignee = style.TruncateWidth(assignee, assigneeCol)

		title := style.TruncateWidth(d.Title, titleCol)

		row := " " + padRight(phase, pCol) + "  " +
			padRight(writID, writCol) + "  " +
			padRight(itemStatus, statusCol) + "  " +
			padRight(assignee, assigneeCol) + "  " +
			title
		lines = append(lines, row)
	}

	// Pad to maxHeight.
	for len(lines) < maxHeight {
		lines = append(lines, "")
	}

	return lines
}

// wrapTextLines word-wraps text to at most width visible columns per line,
// preserving the source's existing line breaks (each line is wrapped
// independently; blank lines pass through as empty lines) — unlike
// wordWrap in confirm.go, which is Fields-based and collapses all
// whitespace, including intentional paragraph/list breaks in writ
// descriptions.
func wrapTextLines(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, para := range strings.Split(text, "\n") {
		if strings.TrimSpace(para) == "" {
			out = append(out, "")
			continue
		}
		words := strings.Fields(para)
		line := words[0]
		for _, word := range words[1:] {
			if len(line)+1+len(word) > width {
				out = append(out, line)
				line = word
			} else {
				line += " " + word
			}
		}
		out = append(out, line)
	}
	return out
}

// renderWritDetail renders the right panel for a writ backlog peek item: a
// header (writ ID + title) followed by a scrollable, word-wrapped rendering
// of the writ description. pm.writScroll is the line offset from the top of
// the wrapped text, clamped here against the actual wrapped line count and
// available height (see scrollWritDetail's doc comment for why the clamp
// lives here rather than at scroll time).
func (pm peekModel) renderWritDetail(item peekItem, maxHeight, maxWidth int) []string {
	header := " " + focusStyle.Render(item.writID) + "  " + item.name
	lines := []string{style.TruncateWidth(header, maxWidth), ""}

	if item.description == "" {
		lines = append(lines, " "+dimStyle.Render("(no description)"))
		for len(lines) < maxHeight {
			lines = append(lines, "")
		}
		return lines
	}

	wrapWidth := maxWidth - 1
	if wrapWidth < 1 {
		wrapWidth = 1
	}
	wrapped := wrapTextLines(item.description, wrapWidth)

	availHeight := maxHeight - len(lines)
	if availHeight < 1 {
		availHeight = 1
	}

	maxOffset := len(wrapped) - availHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset := pm.writScroll
	if offset > maxOffset {
		offset = maxOffset
	}

	end := offset + availHeight
	if end > len(wrapped) {
		end = len(wrapped)
	}

	if offset > 0 {
		lines[0] += "  " + warnStyle.Render(fmt.Sprintf("scrolled: line %d/%d", offset+1, len(wrapped)))
	}

	for _, l := range wrapped[offset:end] {
		lines = append(lines, " "+l)
	}

	// Pad to maxHeight.
	for len(lines) < maxHeight {
		lines = append(lines, "")
	}

	return lines
}
