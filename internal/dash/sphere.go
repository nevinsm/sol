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

// sphereSection identifies a focusable section in the sphere view.
type sphereSection int

const (
	sphereSectionProcesses sphereSection = iota
	sphereSectionWorlds
)

// processItem holds a sphere process entry for the focused list.
type processItem struct {
	name        string
	running     bool
	required    bool   // required processes show red ✗ when down; optional show dim ○
	sessionName string // empty for PID-only processes
	detail      string // formatted detail string
	peekable    bool   // has a tmux session
}

// sphereModel handles the sphere overview.
type sphereModel struct {
	width  int
	height int

	// Section focus — mirrors the world view pattern.
	hasFocus       bool
	focusedSection sphereSection

	// Process list cursor (used when processes section is focused).
	processCursor int
	processItems  []processItem

	// Row selection for the worlds table.
	cursor    int
	worldRows int

	// Inline "no active session" message.
	showNoSession bool

	// Spinners for active processes — one per named process.
	processSpinners map[string]spinner.Model

	// Spinners for worlds that have working agents.
	worldSpinners map[string]spinner.Model

	// Progress bars for caravans.
	caravanProgress map[string]progress.Model
}

func newSphereModel() sphereModel {
	return sphereModel{
		focusedSection:  sphereSectionWorlds,
		processSpinners: make(map[string]spinner.Model),
		worldSpinners:   make(map[string]spinner.Model),
		caravanProgress: make(map[string]progress.Model),
	}
}

func (sm sphereModel) init() tea.Cmd {
	return nil
}

// updateData syncs spinner and progress state with fresh data and returns
// a tea.Cmd to schedule initial spinner ticks. One tick per spinner type
// is sufficient because all spinners sharing the same spinner.Spinner type
// use the same TickMsg ID — one tick drives them all.
func (sm *sphereModel) updateData(data *status.SphereStatus) tea.Cmd {
	if data == nil {
		return nil
	}

	// Sync process spinners.
	sm.syncProcessSpinner("Prefect", data.Prefect.Running)
	sm.syncProcessSpinner("Consul", data.Consul.Running)
	sm.syncProcessSpinner("Chronicle", data.Chronicle.Running)
	sm.syncProcessSpinner("Ledger", data.Ledger.Running)
	sm.syncProcessSpinner("Broker", data.Broker.Running)
	sm.syncProcessSpinner("Senate", data.Senate.Running)

	// Build process items for focused list navigation.
	sm.processItems = []processItem{
		{name: "Prefect", running: data.Prefect.Running, required: true, detail: formatPrefectDetail(data.Prefect), peekable: false},
		{name: "Consul", running: data.Consul.Running, required: true, detail: formatConsulDetail(data.Consul), peekable: false},
		{name: "Chronicle", running: data.Chronicle.Running, required: false, sessionName: data.Chronicle.SessionName, detail: formatChronicleDetail(data.Chronicle), peekable: data.Chronicle.SessionName != ""},
		{name: "Ledger", running: data.Ledger.Running, required: false, sessionName: data.Ledger.SessionName, detail: formatLedgerDetail(data.Ledger), peekable: data.Ledger.SessionName != ""},
		{name: "Broker", running: data.Broker.Running, required: true, detail: formatBrokerDetail(data.Broker), peekable: false},
		{name: "Senate", running: data.Senate.Running, required: false, sessionName: data.Senate.SessionName, detail: formatSenateDetail(data.Senate), peekable: data.Senate.SessionName != ""},
	}

	// Clamp cursor.
	if sm.processCursor >= len(sm.processItems) {
		sm.processCursor = len(sm.processItems) - 1
	}
	if sm.processCursor < 0 {
		sm.processCursor = 0
	}

	// Sync world spinners.
	for _, w := range data.Worlds {
		if w.Working > 0 {
			if _, ok := sm.worldSpinners[w.Name]; !ok {
				s := spinner.New()
				s.Spinner = spinnerForRole("world-process")
				sm.worldSpinners[w.Name] = s
			}
		} else {
			delete(sm.worldSpinners, w.Name)
		}
	}

	// Sync caravan progress.
	for _, c := range data.Caravans {
		if _, ok := sm.caravanProgress[c.ID]; !ok {
			p := progress.New(progress.WithDefaultGradient())
			sm.caravanProgress[c.ID] = p
		}
	}

	sm.worldRows = len(data.Worlds)

	// Schedule initial spinner ticks. One representative tick per spinner
	// type is enough — all spinners with the same ID advance together.
	// s.Tick is a method value (func() tea.Msg) which satisfies tea.Cmd.
	var cmds []tea.Cmd
	for _, s := range sm.processSpinners {
		cmds = append(cmds, s.Tick)
		break
	}
	for _, s := range sm.worldSpinners {
		cmds = append(cmds, s.Tick)
		break
	}
	return tea.Batch(cmds...)
}

func (sm *sphereModel) syncProcessSpinner(name string, running bool) {
	if running {
		if _, ok := sm.processSpinners[name]; !ok {
			s := spinner.New()
			s.Spinner = spinner.Dot
			sm.processSpinners[name] = s
		}
	} else {
		delete(sm.processSpinners, name)
	}
}

// updateAnim is called on each animation tick (~30 FPS).
// Currently a no-op — spinners advance via their own spinner.TickMsg.
// This hook exists for future pulse/fade effects.
func (sm *sphereModel) updateAnim() {}

func (sm sphereModel) update(msg tea.KeyMsg, data *status.SphereStatus) (sphereModel, tea.Cmd) {
	// Any key dismisses the "no active session" message.
	if sm.showNoSession {
		sm.showNoSession = false
		return sm, nil
	}

	switch msg.String() {
	case "tab":
		sm.hasFocus = true
		sm.cycleFocus(1)

	case "shift+tab":
		sm.hasFocus = true
		sm.cycleFocus(-1)

	case "esc":
		if sm.hasFocus {
			sm.hasFocus = false
			return sm, nil
		}

	case "up", "k":
		if sm.hasFocus && sm.focusedSection == sphereSectionProcesses {
			if sm.processCursor > 0 {
				sm.processCursor--
			}
		} else {
			// Worlds cursor (default).
			if sm.cursor > 0 {
				sm.cursor--
			}
		}

	case "down", "j":
		if sm.hasFocus && sm.focusedSection == sphereSectionProcesses {
			max := len(sm.processItems) - 1
			if max < 0 {
				max = 0
			}
			if sm.processCursor < max {
				sm.processCursor++
			}
		} else {
			// Worlds cursor (default).
			max := sm.worldRows - 1
			if max < 0 {
				max = 0
			}
			if sm.cursor < max {
				sm.cursor++
			}
		}

	case "enter", "l", "right":
		if sm.hasFocus && sm.focusedSection == sphereSectionProcesses {
			return sm.handleProcessAction()
		}
		// Drill into the selected world.
		if data != nil && sm.cursor < len(data.Worlds) {
			worldName := data.Worlds[sm.cursor].Name
			return sm, func() tea.Msg { return drillMsg{world: worldName} }
		}

	case "a":
		if sm.hasFocus && sm.focusedSection == sphereSectionProcesses {
			return sm.handleProcessAttach()
		}

	case "R":
		if sm.hasFocus && sm.focusedSection == sphereSectionProcesses {
			return sm.handleProcessRestart()
		}
	}
	return sm, nil
}

// cycleFocus moves focus to the next/previous section.
func (sm *sphereModel) cycleFocus(dir int) {
	sections := []sphereSection{sphereSectionProcesses, sphereSectionWorlds}

	idx := 0
	for i, s := range sections {
		if s == sm.focusedSection {
			idx = i
			break
		}
	}

	next := (idx + dir + len(sections)) % len(sections)
	sm.focusedSection = sections[next]
}

// handleProcessAction handles enter/l on a process item — peeks into the session.
func (sm sphereModel) handleProcessAction() (sphereModel, tea.Cmd) {
	if sm.processCursor >= len(sm.processItems) {
		return sm, nil
	}

	items := buildSpherePeekItems(sm)
	if len(items) == 0 {
		return sm, nil
	}

	msg := peekMsg{
		items:         items,
		initialCursor: sm.processCursor,
		fromView:      viewSphere,
	}
	return sm, func() tea.Msg { return msg }
}

// buildSpherePeekItems creates peek items from the sphere process list.
func buildSpherePeekItems(sm sphereModel) []peekItem {
	var items []peekItem
	for _, pi := range sm.processItems {
		items = append(items, peekItem{
			name:        pi.name,
			sessionName: pi.sessionName,
			category:    "Processes",
			state:       pi.detail,
			alive:       pi.running,
			peekable:    pi.peekable,
		})
	}
	return items
}

// handleProcessAttach handles 'a' on a process item — direct attach.
func (sm sphereModel) handleProcessAttach() (sphereModel, tea.Cmd) {
	if sm.processCursor >= len(sm.processItems) {
		return sm, nil
	}
	item := sm.processItems[sm.processCursor]
	if item.sessionName == "" {
		sm.showNoSession = true
		return sm, nil
	}
	return sm, func() tea.Msg {
		return attachMsg{sessionName: item.sessionName}
	}
}

// handleProcessRestart handles 'R' on a process item — restart signal.
func (sm sphereModel) handleProcessRestart() (sphereModel, tea.Cmd) {
	if sm.processCursor >= len(sm.processItems) {
		return sm, nil
	}
	item := sm.processItems[sm.processCursor]
	return sm, func() tea.Msg {
		return restartProcessMsg{processName: item.name}
	}
}

func (sm sphereModel) updateSpinner(msg spinner.TickMsg) (sphereModel, tea.Cmd) {
	var cmds []tea.Cmd

	for name, s := range sm.processSpinners {
		var cmd tea.Cmd
		s, cmd = s.Update(msg)
		sm.processSpinners[name] = s
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	for name, s := range sm.worldSpinners {
		var cmd tea.Cmd
		s, cmd = s.Update(msg)
		sm.worldSpinners[name] = s
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return sm, tea.Batch(cmds...)
}

func (sm sphereModel) view(data *status.SphereStatus, lastRefresh time.Time, healthLevel int, pulseBright bool) string {
	if data == nil {
		return "Gathering sphere status..."
	}

	var b strings.Builder

	// Header.
	b.WriteString(headerStyle.Render("Sol Sphere"))
	b.WriteString("  ")
	b.WriteString(healthBadgeWithEmphasis(data.Health, healthLevel))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(data.SOLHome))
	b.WriteString("\n\n")

	// Processes section.
	processFocused := sm.hasFocus && sm.focusedSection == sphereSectionProcesses
	if processFocused {
		// Focused: vertical list with cursor.
		b.WriteString("  " + focusIndicator + " " + focusStyle.Render("Processes"))
		b.WriteString("\n")
		sm.renderProcessList(&b)
	} else {
		// Unfocused: compact 3-column grid (existing behavior).
		procs := []processEntry{
			{"Prefect", data.Prefect.Running, true},
			{"Consul", data.Consul.Running, true},
			{"Chronicle", data.Chronicle.Running, false},
			{"Ledger", data.Ledger.Running, false},
			{"Broker", data.Broker.Running, true},
			{"Senate", data.Senate.Running, false},
		}
		b.WriteString(headerStyle.Render("Processes"))
		b.WriteString("\n")
		sm.renderProcessGrid(&b, procs, pulseBright)
	}
	b.WriteString("\n")

	// Worlds table.
	worldsFocused := sm.hasFocus && sm.focusedSection == sphereSectionWorlds
	if len(data.Worlds) == 0 {
		b.WriteString(dimStyle.Render("No worlds initialized."))
		b.WriteString("\n")
	} else {
		if worldsFocused {
			b.WriteString("  " + focusIndicator + " " + focusStyle.Render("Worlds"))
		} else {
			b.WriteString(headerStyle.Render("Worlds"))
		}
		b.WriteString("\n")
		sm.renderWorldsTable(&b, data.Worlds, !sm.hasFocus || worldsFocused)
		b.WriteString("\n")
	}

	// Caravans.
	if len(data.Caravans) > 0 {
		b.WriteString(headerStyle.Render("Caravans"))
		b.WriteString("\n")
		sm.renderCaravans(&b, data.Caravans)
		b.WriteString("\n")
	}

	// Inline "no active session" message.
	if sm.showNoSession {
		b.WriteString(warnStyle.Render("  no active session"))
		b.WriteString("\n\n")
	}

	// Footer.
	b.WriteString(sm.renderFooter(lastRefresh))

	return b.String()
}

// renderProcessList renders processes as a vertical list with cursor selection (focused mode).
func (sm sphereModel) renderProcessList(b *strings.Builder) {
	for i, item := range sm.processItems {
		indicator := optionalStatusIndicator(item.running)
		if item.required {
			indicator = statusIndicator(item.running)
		}
		if item.running {
			if s, ok := sm.processSpinners[item.name]; ok {
				indicator = s.View()
			}
		}

		line := fmt.Sprintf("    %s %-12s", indicator, item.name)
		if item.detail != "" {
			line += dimStyle.Render("  " + item.detail)
		}

		if i == sm.processCursor {
			b.WriteString(selectStyle.Render(padRight(line, sm.width)))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
}

func (sm sphereModel) renderProcess(b *strings.Builder, name string, running bool, detail string) {
	indicator := statusIndicator(running)
	if running {
		if s, ok := sm.processSpinners[name]; ok {
			indicator = s.View()
		}
	}
	line := fmt.Sprintf("  %s %-12s", indicator, name)
	if detail != "" {
		line += dimStyle.Render("  " + detail)
	}
	b.WriteString(line + "\n")
}

// renderProcessGrid renders processes in a compact 3-column grid.
func (sm sphereModel) renderProcessGrid(b *strings.Builder, procs []processEntry, pulseBright bool) {
	cellWidth := (sm.width - 4) / 3
	if cellWidth < 20 {
		cellWidth = 20
	}
	for i, p := range procs {
		indicator := optionalStatusIndicator(p.running)
		if p.required {
			indicator = pulsingStatusIndicator(p.running, pulseBright)
		}
		if p.running {
			if s, ok := sm.processSpinners[p.name]; ok {
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

func (sm sphereModel) renderWorldsTable(b *strings.Builder, worlds []status.WorldSummary, showCursor bool) {
	// Column headers.
	b.WriteString("  " + padRight(dimStyle.Render("WORLD"), 16) + " " + padRight(dimStyle.Render("AGENTS"), 20) + " " + padRight(dimStyle.Render("HEALTH"), 14) + " " + padRight(dimStyle.Render("GOV"), 5) + " " + padRight(dimStyle.Render("FORGE"), 7) + " " + padRight(dimStyle.Render("SENTINEL"), 10) + " " + dimStyle.Render("MR QUEUE") + "\n")

	for i, w := range worlds {
		line := sm.renderWorldRow(w)
		if showCursor && i == sm.cursor {
			b.WriteString(selectStyle.Render(padRight(line, sm.width)))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
}

func (sm sphereModel) renderWorldRow(w status.WorldSummary) string {
	if w.Sleeping {
		// Show active agent/envoy counts for sleeping worlds (soft sleep wind-down).
		agents := dimStyle.Render("—")
		if w.Agents > 0 {
			agents = fmt.Sprintf("%d", w.Agents)
		}
		return "  " + padRight(w.Name, 16) + " " + padRight(agents, 20) + " " + padRight(sleepingBadge, 14) + " " + padRight(dimStyle.Render("—"), 5) + " " + padRight(dimStyle.Render("—"), 7) + " " + padRight(dimStyle.Render("—"), 10) + " " + dimStyle.Render("—")
	}

	// Agents column with optional spinner.
	agents := fmt.Sprintf("%d", w.Agents)
	if w.Working > 0 || w.Stalled > 0 || w.Dead > 0 {
		agents = fmt.Sprintf("%d (%d work", w.Agents, w.Working)
		if w.Stalled > 0 {
			agents += fmt.Sprintf(", %d stall", w.Stalled)
		}
		if w.Dead > 0 {
			agents += fmt.Sprintf(", %d dead", w.Dead)
		}
		agents += ")"
	}
	if s, ok := sm.worldSpinners[w.Name]; ok {
		agents = s.View() + " " + agents
	}

	gov := dimStyle.Render("—")
	if w.Governor {
		gov = okStyle.Render("●")
	}

	forge := dimStyle.Render("—")
	if w.Forge {
		forge = okStyle.Render("✓")
	}

	sentinel := dimStyle.Render("—")
	if w.Sentinel {
		sentinel = okStyle.Render("✓")
	}

	mrQueue := dimStyle.Render("—")
	if w.MRReady > 0 || w.MRFailed > 0 {
		mrQueue = fmt.Sprintf("%d ready", w.MRReady)
		if w.MRFailed > 0 {
			mrQueue += errorStyle.Render(fmt.Sprintf(", %d fail", w.MRFailed))
		}
	}

	health := healthBadge(w.Health)

	return "  " + padRight(w.Name, 16) + " " + padRight(agents, 20) + " " + padRight(health, 14) + " " + padRight(gov, 5) + " " + padRight(forge, 7) + " " + padRight(sentinel, 10) + " " + mrQueue
}

func (sm sphereModel) renderCaravans(b *strings.Builder, caravans []status.CaravanInfo) {
	maxProgressWidth := sm.width / 3
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
		if p, ok := sm.caravanProgress[c.ID]; ok {
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

func (sm sphereModel) renderFooter(lastRefresh time.Time) string {
	help := dimStyle.Render("q quit · ↑↓ select · tab section · enter drill in · a attach · R restart · r refresh")

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

// caravanPhaseSummary builds a compact phase description for a caravan.
func caravanPhaseSummary(c status.CaravanInfo) string {
	if len(c.Phases) == 0 {
		return ""
	}
	var parts []string
	for _, p := range c.Phases {
		parts = append(parts, fmt.Sprintf("p%d: %d/%d", p.Phase, p.Closed, p.Total))
	}
	return strings.Join(parts, " ")
}
