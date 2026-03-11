package dash

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/status"
)

// worldSection identifies a focusable section in the world view.
type worldSection int

const (
	sectionProcesses worldSection = iota
	sectionOutposts
	sectionEnvoys
	sectionMergeQueue
)

// processEntry holds the name and running state for the compact process grid.
type processEntry struct {
	name     string
	running  bool
	required bool // required processes show red ✗ when down; optional show dim ○
}

// worldModel handles the world detail view.
type worldModel struct {
	width  int
	height int

	// Whether any section has focus (receives cursor/scroll input).
	// Outpost and envoy sections are always expanded as full tables.
	// MQ section uses collapse/expand (only expanded when focused).
	hasFocus bool

	// Section focus and per-section cursors.
	focusedSection worldSection
	processCursor  int
	outpostCursor  int
	envoyCursor    int
	mqCursor       int

	// Per-section scroll offsets for independent scrolling.
	outpostScroll int
	envoyScroll   int
	mqScroll      int

	// Section row counts.
	processLen int
	outpostLen int
	envoyLen   int
	mrLen      int // active (non-merged) MRs

	// Inline "no active session" message.
	showNoSession bool

	// Restart feedback message (auto-dismissed after 3 seconds).
	restartFeedback    string
	restartFeedbackErr bool

	// Spinners for active processes.
	processSpinners map[string]spinner.Model

	// Spinners for working agents/envoys.
	agentSpinners map[string]spinner.Model

	// Progress bars for caravans.
	caravanProgress map[string]progress.Model
}

func newWorldModel() worldModel {
	return worldModel{
		processSpinners: make(map[string]spinner.Model),
		agentSpinners:   make(map[string]spinner.Model),
		caravanProgress: make(map[string]progress.Model),
	}
}

func (wm worldModel) init() tea.Cmd {
	return nil
}

// updateData syncs spinners with fresh data and returns a tea.Cmd to
// schedule initial spinner ticks. One tick per spinner type is sufficient
// because all spinners sharing the same spinner.Spinner type use the same
// TickMsg ID — one tick drives them all.
func (wm *worldModel) updateData(data *status.WorldStatus) tea.Cmd {
	if data == nil {
		return nil
	}

	// Sphere process spinners.
	wm.syncProcessSpinner("Prefect", data.Prefect.Running, spinnerForRole("sphere-process"))
	wm.syncProcessSpinner("Chronicle", data.Chronicle.Running, spinnerForRole("sphere-process"))
	wm.syncProcessSpinner("Ledger", data.Ledger.Running, spinnerForRole("sphere-process"))
	wm.syncProcessSpinner("Broker", data.Broker.Running, spinnerForRole("sphere-process"))
	wm.syncProcessSpinner("Senate", data.Senate.Running, spinnerForRole("sphere-process"))

	// World process spinners.
	wm.syncProcessSpinner("Forge", data.Forge.Running, spinnerForRole("world-process"))
	wm.syncProcessSpinner("Sentinel", data.Sentinel.Running, spinnerForRole("world-process"))
	wm.syncProcessSpinner("Governor", data.Governor.Running, spinnerForRole("world-process"))

	// Agent spinners — working agents get spinners.
	active := make(map[string]bool)
	for _, a := range data.Agents {
		if a.State == "working" && a.SessionAlive {
			active[a.Name] = true
			if _, ok := wm.agentSpinners[a.Name]; !ok {
				s := spinner.New()
				s.Spinner = spinnerForRole("outpost")
				wm.agentSpinners[a.Name] = s
			}
		}
	}
	for _, e := range data.Envoys {
		if e.State == "working" && e.SessionAlive {
			active[e.Name] = true
			if _, ok := wm.agentSpinners[e.Name]; !ok {
				s := spinner.New()
				s.Spinner = spinnerForRole("envoy")
				wm.agentSpinners[e.Name] = s
			}
		}
	}
	// Remove spinners for agents no longer working.
	for name := range wm.agentSpinners {
		if !active[name] {
			delete(wm.agentSpinners, name)
		}
	}

	// Caravan progress bars.
	for _, c := range data.Caravans {
		if _, ok := wm.caravanProgress[c.ID]; !ok {
			p := progress.New(progress.WithDefaultGradient())
			wm.caravanProgress[c.ID] = p
		}
	}

	wm.processLen = len(worldProcessList(data))
	wm.outpostLen = len(data.Agents)
	wm.envoyLen = len(data.Envoys)

	// Count active (non-merged) MRs.
	activeMRs := 0
	for _, mr := range data.MergeRequests {
		if mr.Phase != "merged" {
			activeMRs++
		}
	}
	wm.mrLen = activeMRs

	// Schedule initial spinner ticks. One representative tick per spinner
	// type is enough — all spinners with the same ID advance together.
	// s.Tick is a method value (func() tea.Msg) which satisfies tea.Cmd.
	var cmds []tea.Cmd
	for _, s := range wm.processSpinners {
		cmds = append(cmds, s.Tick)
		break
	}
	for _, s := range wm.agentSpinners {
		cmds = append(cmds, s.Tick)
		break
	}
	return tea.Batch(cmds...)
}

func (wm *worldModel) syncProcessSpinner(name string, running bool, style spinner.Spinner) {
	if running {
		if _, ok := wm.processSpinners[name]; !ok {
			s := spinner.New()
			s.Spinner = style
			wm.processSpinners[name] = s
		}
	} else {
		delete(wm.processSpinners, name)
	}
}

// availableSections returns the sections that have rows, in order.
func (wm worldModel) availableSections() []worldSection {
	var sections []worldSection
	if wm.processLen > 0 {
		sections = append(sections, sectionProcesses)
	}
	if wm.outpostLen > 0 {
		sections = append(sections, sectionOutposts)
	}
	if wm.envoyLen > 0 {
		sections = append(sections, sectionEnvoys)
	}
	if wm.mrLen > 0 {
		sections = append(sections, sectionMergeQueue)
	}
	return sections
}

// sectionLen returns the number of rows in the given section.
func (wm worldModel) sectionLen(s worldSection) int {
	switch s {
	case sectionProcesses:
		return wm.processLen
	case sectionOutposts:
		return wm.outpostLen
	case sectionEnvoys:
		return wm.envoyLen
	case sectionMergeQueue:
		return wm.mrLen
	}
	return 0
}

// cursor returns the current cursor for the given section.
func (wm worldModel) cursor(s worldSection) int {
	switch s {
	case sectionProcesses:
		return wm.processCursor
	case sectionOutposts:
		return wm.outpostCursor
	case sectionEnvoys:
		return wm.envoyCursor
	case sectionMergeQueue:
		return wm.mqCursor
	}
	return 0
}

// setCursor sets the cursor for a section.
func (wm *worldModel) setCursor(s worldSection, v int) {
	switch s {
	case sectionProcesses:
		wm.processCursor = v
	case sectionOutposts:
		wm.outpostCursor = v
	case sectionEnvoys:
		wm.envoyCursor = v
	case sectionMergeQueue:
		wm.mqCursor = v
	}
}

// updateAnim is called on each animation tick (~30 FPS).
// Currently a no-op — spinners advance via their own spinner.TickMsg.
// This hook exists for future pulse/fade/highlight effects.
func (wm *worldModel) updateAnim() {}

func (wm worldModel) update(msg tea.KeyMsg, data *status.WorldStatus) (worldModel, tea.Cmd) {
	// Any key dismisses the "no active session" message.
	if wm.showNoSession {
		wm.showNoSession = false
		return wm, nil
	}

	switch msg.String() {
	case "up", "k":
		if !wm.hasFocus {
			return wm, nil
		}
		cur := wm.cursor(wm.focusedSection)
		if cur > 0 {
			wm.setCursor(wm.focusedSection, cur-1)
			wm.adjustScroll()
		}

	case "down", "j":
		if !wm.hasFocus {
			return wm, nil
		}
		cur := wm.cursor(wm.focusedSection)
		max := wm.sectionLen(wm.focusedSection) - 1
		if max < 0 {
			max = 0
		}
		if cur < max {
			wm.setCursor(wm.focusedSection, cur+1)
			wm.adjustScroll()
		}

	case "tab":
		wm.hasFocus = true
		wm.cycleFocus(1)

	case "shift+tab":
		wm.hasFocus = true
		wm.cycleFocus(-1)

	case "esc":
		if wm.hasFocus {
			// First esc: unfocus (no section receives cursor input).
			wm.hasFocus = false
			return wm, nil
		}
		// Second esc: pop back to sphere view.
		return wm, func() tea.Msg { return popMsg{} }

	case "h", "left":
		// Pop back to sphere view.
		return wm, func() tea.Msg { return popMsg{} }

	case "enter", "l", "right":
		if !wm.hasFocus {
			return wm, nil
		}
		// Enter peek mode for the selected item.
		return wm.handlePeek(data)

	case "a":
		if !wm.hasFocus {
			return wm, nil
		}
		// Direct attach — bypass peek mode.
		return wm.handleAttach(data)

	case "R":
		if !wm.hasFocus {
			return wm, nil
		}
		return wm.handleRestart(data)
	}
	return wm, nil
}

// adjustScroll updates the focused section's scroll offset so the cursor stays within the viewport.
func (wm *worldModel) adjustScroll() {
	section := wm.focusedSection
	vpHeight := wm.sectionViewportHeight(section)
	cur := wm.cursor(section)
	scroll := wm.scrollForSection(section)
	if cur < scroll {
		scroll = cur
	}
	if cur >= scroll+vpHeight {
		scroll = cur - vpHeight + 1
	}
	wm.setScrollForSection(section, scroll)
}

// scrollForSection returns the scroll offset for a given section.
func (wm worldModel) scrollForSection(s worldSection) int {
	switch s {
	case sectionOutposts:
		return wm.outpostScroll
	case sectionEnvoys:
		return wm.envoyScroll
	case sectionMergeQueue:
		return wm.mqScroll
	}
	return 0
}

// setScrollForSection sets the scroll offset for a given section.
func (wm *worldModel) setScrollForSection(s worldSection, v int) {
	switch s {
	case sectionOutposts:
		wm.outpostScroll = v
	case sectionEnvoys:
		wm.envoyScroll = v
	case sectionMergeQueue:
		wm.mqScroll = v
	}
}

// agentSectionViewport computes the per-section viewport height for agent
// sections (outposts and envoys). Available vertical space is split equally
// between the two always-expanded agent sections.
func (wm worldModel) agentSectionViewport() int {
	// Fixed lines consumed by other UI elements:
	//   header(2) + sphere procs grid(~2-3) + world procs grid(~1-2) +
	//   outpost header(1) + envoy header(1) + caravans(~1-2) +
	//   MQ summary(1) + summary(1) + footer(2) + column headers(2)
	// Conservative estimate: 16 fixed lines.
	fixedLines := 16
	agentSpace := wm.height - fixedLines
	if agentSpace < 4 {
		agentSpace = 4
	}
	vpHeight := agentSpace / 2
	if vpHeight < 2 {
		vpHeight = 2
	}
	if vpHeight > 20 {
		vpHeight = 20
	}
	return vpHeight
}

// sectionViewportHeight returns the viewport height for a given section.
func (wm worldModel) sectionViewportHeight(s worldSection) int {
	switch s {
	case sectionOutposts, sectionEnvoys:
		return wm.agentSectionViewport()
	case sectionMergeQueue:
		// MQ uses the old approach — same estimate.
		fixedLines := 18
		vpHeight := wm.height - fixedLines
		if vpHeight < 4 {
			vpHeight = 4
		}
		if vpHeight > 20 {
			vpHeight = 20
		}
		return vpHeight
	}
	return 4
}

// cycleFocus moves focus to the next/previous section with rows.
func (wm *worldModel) cycleFocus(dir int) {
	sections := wm.availableSections()
	if len(sections) == 0 {
		return
	}

	// Find current section index.
	idx := -1
	for i, s := range sections {
		if s == wm.focusedSection {
			idx = i
			break
		}
	}

	if idx == -1 {
		// Focus not on a valid section; snap to first.
		wm.focusedSection = sections[0]
		return
	}

	// Cycle.
	next := (idx + dir + len(sections)) % len(sections)
	wm.focusedSection = sections[next]
}

// handlePeek builds peek items from the current world data and emits a peekMsg.
func (wm worldModel) handlePeek(data *status.WorldStatus) (worldModel, tea.Cmd) {
	if data == nil {
		return wm, nil
	}

	items := buildWorldPeekItems(data)
	if len(items) == 0 {
		return wm, nil
	}

	// Determine initial cursor based on focused section and cursor.
	initialCursor := 0
	switch wm.focusedSection {
	case sectionProcesses:
		// Processes appear after outposts and envoys in the item list.
		initialCursor = len(data.Agents) + len(data.Envoys) + wm.processCursor
	case sectionOutposts:
		initialCursor = wm.outpostCursor
	case sectionEnvoys:
		initialCursor = len(data.Agents) + wm.envoyCursor
	case sectionMergeQueue:
		initialCursor = len(data.Agents) + len(data.Envoys) // Forge is first world process
	}

	msg := peekMsg{
		items:         items,
		initialCursor: initialCursor,
		fromView:      viewWorld,
		world:         data.World,
	}
	return wm, func() tea.Msg { return msg }
}

// buildWorldPeekItems creates peek items for all agents, envoys, and world processes.
func buildWorldPeekItems(data *status.WorldStatus) []peekItem {
	var items []peekItem

	// Outposts.
	for _, a := range data.Agents {
		items = append(items, peekItem{
			name:        a.Name,
			sessionName: fmt.Sprintf("sol-%s-%s", data.World, a.Name),
			category:    "Outposts",
			state:       a.State,
			alive:       a.SessionAlive,
			peekable:    a.SessionAlive,
		})
	}

	// Envoys.
	for _, e := range data.Envoys {
		items = append(items, peekItem{
			name:        e.Name,
			sessionName: fmt.Sprintf("sol-%s-%s", data.World, e.Name),
			category:    "Envoys",
			state:       e.State,
			alive:       e.SessionAlive,
			peekable:    e.SessionAlive,
		})
	}

	// World processes.
	type proc struct {
		name        string
		running     bool
		sessionName string
		isForge     bool
		source      string
	}

	// Forge: peek session is the merge agent session, not the forge process session.
	forgeMergeSess := fmt.Sprintf("sol-%s-forge-merge", data.World)
	worldProcs := []proc{
		{"Forge", data.Forge.Running, forgeMergeSess, true, "forge"},
		{"Sentinel", data.Sentinel.Running, "", false, "sentinel"},
	}
	if data.Governor.Running || data.Governor.SessionAlive {
		worldProcs = append(worldProcs, proc{
			name:    "Governor",
			running: data.Governor.Running,
			sessionName: fmt.Sprintf("sol-%s-governor", data.World),
			source:  "governor",
		})
	}
	for _, p := range worldProcs {
		state := "stopped"
		if p.running {
			state = "alive"
		}
		sessName := p.sessionName
		if sessName == "" {
			sessName = fmt.Sprintf("sol-%s-%s", data.World, strings.ToLower(p.name))
		}
		items = append(items, peekItem{
			name:        p.name,
			sessionName: sessName,
			category:    "Processes",
			state:       state,
			alive:       p.running,
			peekable:    true, // services are always peekable (forge shows idle state when no merge)
			isForge:     p.isForge,
			source:      p.source,
		})
	}

	return items
}

// handleAttach checks if the selected row has a live session and returns an attach command.
func (wm worldModel) handleAttach(data *status.WorldStatus) (worldModel, tea.Cmd) {
	if data == nil {
		return wm, nil
	}

	switch wm.focusedSection {
	case sectionProcesses:
		procs := worldProcessList(data)
		if wm.processCursor < len(procs) {
			p := procs[wm.processCursor]
			if !p.running {
				return wm, func() tea.Msg { return noSessionMsg{} }
			}
			sessName := fmt.Sprintf("sol-%s-%s", data.World, strings.ToLower(p.name))
			return wm, func() tea.Msg { return attachMsg{sessionName: sessName} }
		}

	case sectionOutposts:
		if wm.outpostCursor < len(data.Agents) {
			agent := data.Agents[wm.outpostCursor]
			if !agent.SessionAlive {
				return wm, func() tea.Msg { return noSessionMsg{} }
			}
			return wm, func() tea.Msg {
				return attachMsg{sessionName: fmt.Sprintf("sol-%s-%s", data.World, agent.Name)}
			}
		}

	case sectionEnvoys:
		if wm.envoyCursor < len(data.Envoys) {
			envoy := data.Envoys[wm.envoyCursor]
			if !envoy.SessionAlive {
				return wm, func() tea.Msg { return noSessionMsg{} }
			}
			return wm, func() tea.Msg {
				return attachMsg{sessionName: fmt.Sprintf("sol-%s-%s", data.World, envoy.Name)}
			}
		}
	}

	return wm, nil
}

// handleRestart builds a restart request for the currently selected item.
func (wm worldModel) handleRestart(data *status.WorldStatus) (worldModel, tea.Cmd) {
	if data == nil {
		return wm, nil
	}

	var target restartTarget
	target.world = data.World

	switch wm.focusedSection {
	case sectionProcesses:
		procs := worldProcessList(data)
		if wm.processCursor >= len(procs) {
			return wm, nil
		}
		p := procs[wm.processCursor]
		target.name = p.name
		target.role = strings.ToLower(p.name)
		target.sessionName = fmt.Sprintf("sol-%s-%s", data.World, target.role)
		target.confirmTitle = fmt.Sprintf("Restart %s?", p.name)
		switch target.role {
		case "forge":
			target.confirmDetail = "Stop and restart the forge pipeline"
		case "sentinel":
			target.confirmDetail = "Stop and restart the health monitor"
		case "governor":
			target.confirmDetail = "Stop and restart the governor"
		}

	case sectionOutposts:
		if wm.outpostCursor >= len(data.Agents) {
			return wm, nil
		}
		a := data.Agents[wm.outpostCursor]
		target.name = a.Name
		target.role = "outpost"
		target.sessionName = fmt.Sprintf("sol-%s-%s", data.World, a.Name)
		target.confirmTitle = fmt.Sprintf("Restart %s?", a.Name)
		target.confirmDetail = "Kill session and re-cast tethered writ"

	case sectionEnvoys:
		if wm.envoyCursor >= len(data.Envoys) {
			return wm, nil
		}
		e := data.Envoys[wm.envoyCursor]
		target.name = e.Name
		target.role = "envoy"
		target.sessionName = fmt.Sprintf("sol-%s-%s", data.World, e.Name)
		target.confirmTitle = fmt.Sprintf("Restart %s?", e.Name)
		target.confirmDetail = "Kill session and restart (tethered work preserved)"

	default:
		return wm, nil
	}

	return wm, func() tea.Msg { return requestRestartMsg{target: target} }
}

func (wm worldModel) updateSpinner(msg spinner.TickMsg) (worldModel, tea.Cmd) {
	var cmds []tea.Cmd

	for name, s := range wm.processSpinners {
		var cmd tea.Cmd
		s, cmd = s.Update(msg)
		wm.processSpinners[name] = s
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	for name, s := range wm.agentSpinners {
		var cmd tea.Cmd
		s, cmd = s.Update(msg)
		wm.agentSpinners[name] = s
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return wm, tea.Batch(cmds...)
}

func (wm worldModel) view(data *status.WorldStatus, lastRefresh time.Time, healthLevel int, agentHighlights map[string]int, pulseBright bool) string {
	if data == nil {
		return "Gathering world status..."
	}

	var b strings.Builder

	// Header.
	b.WriteString(headerStyle.Render(fmt.Sprintf("World: %s", data.World)))
	b.WriteString("  ")
	b.WriteString(healthBadgeWithEmphasis(data.HealthString(), healthLevel))
	b.WriteString("\n\n")

	// Sphere Processes — compact grid.
	sphereProcs := []processEntry{
		{"Prefect", data.Prefect.Running, true},
		{"Chronicle", data.Chronicle.Running, false},
		{"Ledger", data.Ledger.Running, false},
		{"Broker", data.Broker.Running, true},
		{"Senate", data.Senate.Running, false},
	}
	b.WriteString(headerStyle.Render("Sphere Processes"))
	b.WriteString("\n")
	wm.renderProcessGrid(&b, sphereProcs, pulseBright)
	b.WriteString("\n")

	// World Processes — interactive section.
	wm.renderWorldProcessesSection(&b, data)

	// Outposts.
	if len(data.Agents) > 0 {
		wm.renderOutpostsSection(&b, data, agentHighlights, pulseBright)
	}

	// Envoys.
	if len(data.Envoys) > 0 {
		wm.renderEnvoysSection(&b, data, pulseBright)
	}

	if len(data.Agents) == 0 && len(data.Envoys) == 0 {
		b.WriteString(dimStyle.Render("No agents registered."))
		b.WriteString("\n")
	}

	// Caravans — always expanded (already compact).
	if len(data.Caravans) > 0 {
		b.WriteString(headerStyle.Render("Caravans"))
		b.WriteString("\n")
		wm.renderCaravans(&b, data.Caravans)
		b.WriteString("\n")
	}

	// Merge Queue.
	wm.renderMergeQueueSection(&b, data, pulseBright)

	// Summary.
	b.WriteString(wm.renderSummary(data))

	// Inline "no active session" message.
	if wm.showNoSession {
		b.WriteString(warnStyle.Render("  no active session"))
		b.WriteString("\n\n")
	}

	// Restart feedback message.
	if wm.restartFeedback != "" {
		if wm.restartFeedbackErr {
			b.WriteString(errorStyle.Render("  " + wm.restartFeedback))
		} else {
			b.WriteString(okStyle.Render("  " + wm.restartFeedback))
		}
		b.WriteString("\n\n")
	}

	// Footer.
	b.WriteString(wm.renderFooter(lastRefresh))

	return b.String()
}

// renderOutpostsSection renders the Outposts section as a full table (always expanded).
func (wm worldModel) renderOutpostsSection(b *strings.Builder, data *status.WorldStatus, agentHighlights map[string]int, pulseBright bool) {
	isFocused := wm.hasFocus && wm.focusedSection == sectionOutposts
	sectionHeader := fmt.Sprintf("Outposts (%d)", len(data.Agents))

	vpHeight := wm.agentSectionViewport()
	scrollInfo := scrollIndicator(wm.outpostScroll, vpHeight, len(data.Agents))

	if isFocused {
		header := "  " + focusIndicator + " " + focusStyle.Render(sectionHeader)
		if scrollInfo != "" {
			header += "  " + dimStyle.Render(scrollInfo)
		}
		b.WriteString(header + "\n")
	} else {
		header := "  " + headerStyle.Render(sectionHeader)
		if scrollInfo != "" {
			header += "  " + dimStyle.Render(scrollInfo)
		}
		b.WriteString(header + "\n")
	}
	wm.renderAgentsTable(b, data.Agents, agentHighlights, pulseBright)
	b.WriteString("\n")
}

// renderEnvoysSection renders the Envoys section as a full table (always expanded).
func (wm worldModel) renderEnvoysSection(b *strings.Builder, data *status.WorldStatus, pulseBright bool) {
	isFocused := wm.hasFocus && wm.focusedSection == sectionEnvoys
	sectionHeader := fmt.Sprintf("Envoys (%d)", len(data.Envoys))

	vpHeight := wm.agentSectionViewport()
	scrollInfo := scrollIndicator(wm.envoyScroll, vpHeight, len(data.Envoys))

	if isFocused {
		header := "  " + focusIndicator + " " + focusStyle.Render(sectionHeader)
		if scrollInfo != "" {
			header += "  " + dimStyle.Render(scrollInfo)
		}
		b.WriteString(header + "\n")
	} else {
		header := "  " + headerStyle.Render(sectionHeader)
		if scrollInfo != "" {
			header += "  " + dimStyle.Render(scrollInfo)
		}
		b.WriteString(header + "\n")
	}
	wm.renderEnvoysTable(b, data.Envoys, pulseBright)
	b.WriteString("\n")
}

// renderMergeQueueSection renders the Merge Queue in summary or expanded mode.
func (wm worldModel) renderMergeQueueSection(b *strings.Builder, data *status.WorldStatus, pulseBright bool) {
	isFocused := wm.hasFocus && wm.focusedSection == sectionMergeQueue
	summary := mqSummaryLine(data.MergeQueue)

	if !isFocused {
		// Summary mode: one-line.
		b.WriteString("  " + headerStyle.Render("Merge Queue"))
		b.WriteString("      " + summary)
		b.WriteString("\n")
		return
	}

	// Expanded mode: header + summary line + MR detail rows.
	header := "  " + focusIndicator + " " + focusStyle.Render("Merge Queue")
	b.WriteString(header + "\n")
	wm.renderMergeQueue(b, data.MergeQueue, data.MergeRequests, pulseBright)
	b.WriteString("\n")
}

func (wm worldModel) renderProcess(b *strings.Builder, name string, running bool, detail string) {
	indicator := statusIndicator(running)
	if running {
		if s, ok := wm.processSpinners[name]; ok {
			indicator = s.View()
		}
	}
	line := fmt.Sprintf("  %s %-12s", indicator, name)
	if detail != "" {
		line += dimStyle.Render("  " + detail)
	}
	b.WriteString(line + "\n")
}

func (wm worldModel) renderAgentsTable(b *strings.Builder, agents []status.AgentStatus, agentHighlights map[string]int, pulseBright bool) {
	// Column headers.
	b.WriteString("  " + padRight(dimStyle.Render("NAME"), 14) + " " + padRight(dimStyle.Render("STATE"), 18) + " " + padRight(dimStyle.Render("SESSION"), 10) + " " + dimStyle.Render("WORK") + "\n")

	// Apply viewport windowing with per-section scroll.
	vpHeight := wm.agentSectionViewport()
	start := wm.outpostScroll
	end := start + vpHeight
	if end > len(agents) {
		end = len(agents)
	}
	if start > len(agents) {
		start = len(agents)
	}

	isFocused := wm.hasFocus && wm.focusedSection == sectionOutposts
	for i := start; i < end; i++ {
		a := agents[i]
		line := wm.renderAgentRow(a, pulseBright)
		if isFocused && i == wm.outpostCursor {
			b.WriteString(selectStyle.Render(padRight(line, wm.width)))
		} else if agentHighlights != nil {
			if level, highlighted := agentHighlights[a.Name]; highlighted && level > 0 {
				b.WriteString(highlightAtLevel(level).Render(padRight(line, wm.width)))
			} else {
				b.WriteString(line)
			}
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
}

func (wm worldModel) renderAgentRow(a status.AgentStatus, pulseBright bool) string {
	// Name column — spinner for working agents.
	name := a.Name
	if s, ok := wm.agentSpinners[a.Name]; ok {
		name = s.View() + " " + a.Name
	}

	state := a.State
	switch a.State {
	case "working":
		if a.SessionAlive {
			state = okStyle.Render("working")
		} else {
			state = "working (" + pulseStyle(errorStyle, pulseBright).Render("dead!") + ")"
		}
	case "idle":
		state = dimStyle.Render("idle")
	case "stalled":
		state = pulseStyle(warnStyle, pulseBright).Render("stalled")
	}

	sess := dimStyle.Render("—")
	if a.State == "working" || a.State == "stalled" {
		if a.SessionAlive {
			sess = okStyle.Render("alive")
		} else {
			sess = pulseStyle(errorStyle, pulseBright).Render("dead")
		}
	}

	work := dimStyle.Render("—")
	if a.ActiveWrit != "" {
		work = fmt.Sprintf("%s: %s", a.ActiveWrit, a.WorkTitle)
		// Truncate work column to fit available width.
		// Fixed columns: 2 (indent) + 14 (name) + 1 (sep) + 18 (state) + 1 (sep) + 10 (sess) + 1 (sep) = 47
		maxWork := wm.width - 47
		if maxWork < 20 {
			maxWork = 20
		}
		work = truncateStr(work, maxWork)
	}

	return "  " + padRight(name, 14) + " " + padRight(state, 18) + " " + padRight(sess, 10) + " " + work
}

func (wm worldModel) renderEnvoysTable(b *strings.Builder, envoys []status.EnvoyStatus, pulseBright bool) {
	b.WriteString("  " + padRight(dimStyle.Render("NAME"), 14) + " " + padRight(dimStyle.Render("STATE"), 18) + " " + padRight(dimStyle.Render("SESSION"), 10) + " " + padRight(dimStyle.Render("WORK"), 24) + " " + dimStyle.Render("BRIEF") + "\n")

	// Apply viewport windowing with per-section scroll.
	vpHeight := wm.agentSectionViewport()
	start := wm.envoyScroll
	end := start + vpHeight
	if end > len(envoys) {
		end = len(envoys)
	}
	if start > len(envoys) {
		start = len(envoys)
	}

	isFocused := wm.hasFocus && wm.focusedSection == sectionEnvoys
	for i := start; i < end; i++ {
		e := envoys[i]
		line := wm.renderEnvoyRow(e, pulseBright)
		if isFocused && i == wm.envoyCursor {
			b.WriteString(selectStyle.Render(padRight(line, wm.width)))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
}

func (wm worldModel) renderEnvoyRow(e status.EnvoyStatus, pulseBright bool) string {
	name := e.Name
	if s, ok := wm.agentSpinners[e.Name]; ok {
		name = s.View() + " " + e.Name
	}

	state := e.State
	switch e.State {
	case "working":
		if e.SessionAlive {
			state = okStyle.Render("working")
		} else {
			state = "working (" + pulseStyle(errorStyle, pulseBright).Render("dead!") + ")"
		}
	case "idle":
		state = dimStyle.Render("idle")
	case "stalled":
		state = pulseStyle(warnStyle, pulseBright).Render("stalled")
	}

	sess := dimStyle.Render("—")
	if e.State == "working" || e.State == "stalled" {
		if e.SessionAlive {
			sess = okStyle.Render("alive")
		} else {
			sess = pulseStyle(errorStyle, pulseBright).Render("dead")
		}
	}

	work := dimStyle.Render("—")
	if e.ActiveWrit != "" {
		work = truncateStr(e.WorkTitle, 24)
	}

	brief := dimStyle.Render("—")
	if e.BriefAge != "" {
		brief = e.BriefAge + " ago"
	}

	return "  " + padRight(name, 14) + " " + padRight(state, 18) + " " + padRight(sess, 10) + " " + padRight(work, 24) + " " + brief
}

func (wm worldModel) renderMergeQueue(b *strings.Builder, mq status.MergeQueueInfo, mrs []status.MergeRequestInfo, pulseBright bool) {
	if mq.Total == 0 {
		b.WriteString(dimStyle.Render("  empty"))
		b.WriteString("\n")
		return
	}

	// Summary line.
	var parts []string
	if mq.Ready > 0 {
		parts = append(parts, fmt.Sprintf("%d ready", mq.Ready))
	}
	if mq.Claimed > 0 {
		parts = append(parts, fmt.Sprintf("%d in progress", mq.Claimed))
	}
	if mq.Failed > 0 {
		parts = append(parts, errorStyle.Render(fmt.Sprintf("%d failed", mq.Failed)))
	}
	if mq.Merged > 0 {
		parts = append(parts, okStyle.Render(fmt.Sprintf("%d merged", mq.Merged)))
	}
	b.WriteString(fmt.Sprintf("  %s\n", strings.Join(parts, ", ")))

	// Individual MR rows — only show active (non-merged) MRs.
	var activeMRs []status.MergeRequestInfo
	for _, mr := range mrs {
		if mr.Phase != "merged" {
			activeMRs = append(activeMRs, mr)
		}
	}
	if len(activeMRs) > 0 {
		b.WriteString("  " + padRight(dimStyle.Render("ID"), 20) + " " + padRight(dimStyle.Render("WRIT"), 20) + " " + padRight(dimStyle.Render("STATUS"), 10) + " " + dimStyle.Render("TITLE") + "\n")
		for _, mr := range activeMRs {
			b.WriteString(wm.renderMRRow(mr, pulseBright))
			b.WriteString("\n")
		}
	}
}

func (wm worldModel) renderMRRow(mr status.MergeRequestInfo, pulseBright bool) string {
	phase := mr.Phase
	switch mr.Phase {
	case "ready":
		phase = okStyle.Render("ready")
	case "claimed":
		phase = warnStyle.Render("in progress")
	case "failed":
		phase = pulseStyle(errorStyle, pulseBright).Render("failed")
	case "merged":
		phase = okStyle.Render("merged")
	}

	title := mr.Title
	if len(title) > 40 {
		title = title[:37] + "..."
	}

	return "  " + padRight(mr.ID, 20) + " " + padRight(mr.WritID, 20) + " " + padRight(phase, 10) + " " + title
}

func (wm worldModel) renderCaravans(b *strings.Builder, caravans []status.CaravanInfo) {
	maxProgressWidth := wm.width / 3
	if maxProgressWidth < 20 {
		maxProgressWidth = 20
	}
	if maxProgressWidth > 40 {
		maxProgressWidth = 40
	}

	for _, c := range caravans {
		fraction := float64(0)
		if c.TotalItems > 0 {
			fraction = float64(c.ClosedItems) / float64(c.TotalItems)
		}

		progressStr := ""
		if p, ok := wm.caravanProgress[c.ID]; ok {
			p.Width = maxProgressWidth
			progressStr = p.ViewAs(fraction)
		}

		phaseSummary := caravanPhaseSummary(c)
		if phaseSummary != "" {
			b.WriteString(fmt.Sprintf("  %s  %s  %s  %s\n",
				c.Name, progressStr,
				dimStyle.Render(fmt.Sprintf("%d/%d merged", c.ClosedItems, c.TotalItems)),
				dimStyle.Render(phaseSummary),
			))
		} else {
			b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
				c.Name, progressStr,
				dimStyle.Render(fmt.Sprintf("%d/%d merged", c.ClosedItems, c.TotalItems)),
			))
		}
	}
}

func (wm worldModel) renderSummary(data *status.WorldStatus) string {
	parts := fmt.Sprintf("%d agents", data.Summary.Total)
	if len(data.Envoys) > 0 {
		parts += fmt.Sprintf(", %d envoys", len(data.Envoys))
	}
	parts += fmt.Sprintf(" | %d working, %d idle", data.Summary.Working, data.Summary.Idle)
	if data.Summary.Stalled > 0 {
		parts += warnStyle.Render(fmt.Sprintf(", %d stalled", data.Summary.Stalled))
	}
	if data.Summary.Dead > 0 {
		parts += errorStyle.Render(fmt.Sprintf(", %d dead", data.Summary.Dead))
	}
	return dimStyle.Render(parts) + "\n"
}

func (wm worldModel) renderFooter(lastRefresh time.Time) string {
	help := dimStyle.Render("q quit · ↑↓ select · tab section · enter peek · a attach · R restart · esc back · r refresh")

	age := ""
	if !lastRefresh.IsZero() {
		elapsed := time.Since(lastRefresh)
		age = dimStyle.Render(fmt.Sprintf("refreshed %ds ago", int(math.Round(elapsed.Seconds()))))
	}

	if age != "" {
		return fmt.Sprintf("\n%s    %s\n", help, age)
	}
	return fmt.Sprintf("\n%s\n", help)
}

// outpostSummary builds a compact summary string for the outposts section.
func outpostSummary(agents []status.AgentStatus) string {
	working, idle, stalled, dead := 0, 0, 0, 0
	for _, a := range agents {
		switch a.State {
		case "working":
			working++
			if !a.SessionAlive {
				dead++
			}
		case "idle":
			idle++
		case "stalled":
			stalled++
		}
	}
	var parts []string
	if working > 0 {
		parts = append(parts, fmt.Sprintf("%d working", working))
	}
	if idle > 0 {
		parts = append(parts, fmt.Sprintf("%d idle", idle))
	}
	if stalled > 0 {
		parts = append(parts, warnStyle.Render(fmt.Sprintf("%d stalled", stalled)))
	}
	if dead > 0 {
		parts = append(parts, errorStyle.Render(fmt.Sprintf("%d dead", dead)))
	}
	return strings.Join(parts, ", ")
}

// envoySummary builds a compact summary string for the envoys section.
func envoySummary(envoys []status.EnvoyStatus) string {
	if len(envoys) == 1 {
		e := envoys[0]
		state := e.State
		if e.State == "working" && !e.SessionAlive {
			state = "dead"
		}
		return fmt.Sprintf("%s (%s)", e.Name, state)
	}
	// Multiple envoys — aggregate like outposts.
	working, idle := 0, 0
	for _, e := range envoys {
		switch e.State {
		case "working":
			working++
		case "idle":
			idle++
		}
	}
	var parts []string
	if working > 0 {
		parts = append(parts, fmt.Sprintf("%d working", working))
	}
	if idle > 0 {
		parts = append(parts, fmt.Sprintf("%d idle", idle))
	}
	return strings.Join(parts, ", ")
}

// mqSummaryLine builds the summary string for the merge queue.
func mqSummaryLine(mq status.MergeQueueInfo) string {
	if mq.Total == 0 {
		return dimStyle.Render("empty")
	}
	var parts []string
	if mq.Ready > 0 {
		parts = append(parts, fmt.Sprintf("%d ready", mq.Ready))
	}
	if mq.Claimed > 0 {
		parts = append(parts, fmt.Sprintf("%d in progress", mq.Claimed))
	}
	if mq.Failed > 0 {
		parts = append(parts, errorStyle.Render(fmt.Sprintf("%d failed", mq.Failed)))
	}
	if mq.Merged > 0 {
		parts = append(parts, okStyle.Render(fmt.Sprintf("%d merged", mq.Merged)))
	}
	return strings.Join(parts, ", ")
}

// scrollIndicator returns a scroll position string like "1-8 of 12 ↓".
func scrollIndicator(offset, vpHeight, totalRows int) string {
	if totalRows <= vpHeight {
		return ""
	}
	first := offset + 1
	last := offset + vpHeight
	if last > totalRows {
		last = totalRows
	}
	indicator := fmt.Sprintf("%d-%d of %d", first, last, totalRows)
	if offset > 0 && last < totalRows {
		indicator += " ↕"
	} else if offset > 0 {
		indicator += " ↑"
	} else {
		indicator += " ↓"
	}
	return indicator
}

// worldProcessList returns the list of world processes for the interactive section.
// Always includes forge, sentinel, and governor regardless of running state.
func worldProcessList(data *status.WorldStatus) []processEntry {
	return []processEntry{
		{"Forge", data.Forge.Running, false},
		{"Sentinel", data.Sentinel.Running, false},
		{"Governor", data.Governor.Running, false},
	}
}

// renderWorldProcessesSection renders the World Processes as an interactive section.
func (wm worldModel) renderWorldProcessesSection(b *strings.Builder, data *status.WorldStatus) {
	isFocused := wm.hasFocus && wm.focusedSection == sectionProcesses
	procs := worldProcessList(data)
	sectionHeader := fmt.Sprintf("World Processes (%d)", len(procs))

	if !isFocused {
		// Summary mode: one-line with running count.
		b.WriteString("  " + headerStyle.Render(sectionHeader))
		running := 0
		for _, p := range procs {
			if p.running {
				running++
			}
		}
		b.WriteString(fmt.Sprintf("      %d/%d running", running, len(procs)))
		b.WriteString("\n")
		return
	}

	// Expanded mode: vertical list with cursor.
	header := "  " + focusIndicator + " " + focusStyle.Render(sectionHeader)
	b.WriteString(header + "\n")
	for i, p := range procs {
		indicator := optionalStatusIndicator(p.running)
		if p.required {
			indicator = statusIndicator(p.running)
		}
		if p.running {
			if s, ok := wm.processSpinners[p.name]; ok {
				indicator = s.View()
			}
		}
		line := fmt.Sprintf("  %s %-12s", indicator, p.name)
		if i == wm.processCursor {
			b.WriteString(selectStyle.Render(padRight(line, wm.width)))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

// renderProcessGrid renders processes in a compact 3-column grid.
func (wm worldModel) renderProcessGrid(b *strings.Builder, procs []processEntry, pulseBright bool) {
	cellWidth := (wm.width - 4) / 3
	if cellWidth < 20 {
		cellWidth = 20
	}
	for i, p := range procs {
		indicator := optionalStatusIndicator(p.running)
		if p.required {
			indicator = pulsingStatusIndicator(p.running, pulseBright)
		}
		if p.running {
			if s, ok := wm.processSpinners[p.name]; ok {
				indicator = s.View()
			}
		}
		cell := padRight(indicator+" "+p.name, cellWidth)
		if i%3 == 0 {
			b.WriteString("  ")
		}
		b.WriteString(cell)
		if i%3 == 2 || i == len(procs)-1 {
			b.WriteString("\n")
		}
	}
}
