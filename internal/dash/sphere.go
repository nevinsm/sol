package dash

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/daemon"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/statusformat"
)

// sphereSection identifies a focusable section in the sphere view.
type sphereSection int

const (
	sphereSectionProcesses sphereSection = iota
	sphereSectionWorlds
	sphereSectionCaravans
)

// processItem holds a sphere process entry for the focused list.
type processItem struct {
	name        string
	running     bool
	required    bool   // required processes show red ✗ when down; optional show dim ○
	sessionName string // empty for PID-only processes
	detail      string // formatted detail string
	peekable    bool   // has a tmux session
	source      string // event source filter for feed-based peek (e.g., "prefect", "consul")
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
	showNoSession    bool
	noSessionMessage string // descriptive message to show instead of default "no active session"

	// Spinners for active processes — one per named process.
	processSpinners map[string]spinner.Model

	// Spinners for worlds that have working agents.
	worldSpinners map[string]spinner.Model

	// Progress bars for caravans.
	caravanProgress map[string]progress.Model

	// Caravan section cursor and count.
	caravanCursor int
	caravanLen    int
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

// updateData syncs spinner and progress state with fresh data and returns a
// tea.Cmd that kicks off ticking for any spinner newly created this call.
// Each spinner.Model has its own unique ID (see the bubbles spinner
// package), so a tick only ever drives the one spinner it targets; each
// spinner must therefore start its own self-perpetuating tick chain exactly
// once, at creation — see syncProcessSpinner and the worldSpinners block
// below. updateData used to instead re-inject one representative tick per
// spinner map on every call (every 3s, per dataMsg), regardless of whether
// anything was newly created; that was redundant once a spinner's chain was
// already running (dedup'd harmlessly by the tag check in spinner.Update)
// and has been removed — see sol-7d76ff96a749ddb1.
func (sm *sphereModel) updateData(data *status.SphereStatus) tea.Cmd {
	if data == nil {
		return nil
	}

	var cmds []tea.Cmd

	// Sync process spinners.
	cmds = append(cmds,
		sm.syncProcessSpinner("Prefect", data.Prefect.Running),
		sm.syncProcessSpinner("Consul", data.Consul.Running),
		sm.syncProcessSpinner("Chronicle", data.Chronicle.Running),
		sm.syncProcessSpinner("Ledger", data.Ledger.Running),
		sm.syncProcessSpinner("Broker", data.Broker.Running),
	)
	// Build process items for focused list navigation.
	sm.processItems = []processItem{
		{name: "Prefect", running: data.Prefect.Running, required: true, detail: statusformat.FormatPrefectDetail(statusformat.PrefectDetail(data.Prefect)), peekable: false, source: "prefect"},
		{name: "Consul", running: data.Consul.Running, required: true, detail: statusformat.FormatConsulDetail(statusformat.ConsulDetail(data.Consul)), peekable: false, source: "consul"},
		{name: "Chronicle", running: data.Chronicle.Running, required: false, detail: statusformat.FormatChronicleDetail(statusformat.ChronicleDetail(data.Chronicle)), peekable: false, source: "chronicle"},
		{name: "Ledger", running: data.Ledger.Running, required: false, detail: statusformat.FormatLedgerDetail(statusformat.LedgerDetail(data.Ledger)), peekable: false, source: "ledger"},
		{name: "Broker", running: data.Broker.Running, required: true, detail: statusformat.FormatBrokerDetail(statusformat.BrokerDetail(data.Broker)), peekable: false, source: "broker"},
	}

	// Clamp cursor.
	if sm.processCursor >= len(sm.processItems) {
		sm.processCursor = len(sm.processItems) - 1
	}
	if sm.processCursor < 0 {
		sm.processCursor = 0
	}

	// Sync world spinners.
	currentWorlds := make(map[string]bool, len(data.Worlds))
	for _, w := range data.Worlds {
		currentWorlds[w.Name] = true
		if w.Working > 0 {
			if _, ok := sm.worldSpinners[w.Name]; !ok {
				s := spinner.New()
				s.Spinner = spinnerForRole("world-process")
				sm.worldSpinners[w.Name] = s
				cmds = append(cmds, s.Tick)
			}
		} else {
			delete(sm.worldSpinners, w.Name)
		}
	}
	// Prune spinners for worlds no longer in the current list.
	for name := range sm.worldSpinners {
		if !currentWorlds[name] {
			delete(sm.worldSpinners, name)
		}
	}

	// Sync caravan progress.
	currentCaravans := make(map[string]bool, len(data.Caravans))
	for _, c := range data.Caravans {
		currentCaravans[c.ID] = true
		if _, ok := sm.caravanProgress[c.ID]; !ok {
			p := progress.New(progress.WithDefaultGradient())
			sm.caravanProgress[c.ID] = p
		}
	}
	// Prune progress bars for caravans no longer in the current list.
	for id := range sm.caravanProgress {
		if !currentCaravans[id] {
			delete(sm.caravanProgress, id)
		}
	}

	sm.worldRows = len(data.Worlds)
	sm.caravanLen = len(data.Caravans)

	return tea.Batch(cmds...)
}

// syncProcessSpinner creates or removes the named process spinner to match
// running, and returns the tea.Cmd that starts its tick chain when a new
// spinner is created (nil otherwise — see updateData).
func (sm *sphereModel) syncProcessSpinner(name string, running bool) tea.Cmd {
	if running {
		if _, ok := sm.processSpinners[name]; !ok {
			s := spinner.New()
			s.Spinner = spinner.Dot
			sm.processSpinners[name] = s
			return s.Tick
		}
	} else {
		delete(sm.processSpinners, name)
	}
	return nil
}

// updateAnim is called on each animation tick (~30 FPS).
// Returns true if any animation is actively affecting visible output
// (running spinners or world spinners present).
func (sm *sphereModel) updateAnim() bool {
	return len(sm.processSpinners) > 0 || len(sm.worldSpinners) > 0
}

func (sm sphereModel) update(msg tea.KeyMsg, data *status.SphereStatus) (sphereModel, tea.Cmd) {
	// Any key dismisses the "no active session" message.
	if sm.showNoSession {
		sm.showNoSession = false
		sm.noSessionMessage = ""
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
		} else if sm.hasFocus && sm.focusedSection == sphereSectionCaravans {
			if sm.caravanCursor > 0 {
				sm.caravanCursor--
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
		} else if sm.hasFocus && sm.focusedSection == sphereSectionCaravans {
			max := sm.caravanLen - 1
			if max < 0 {
				max = 0
			}
			if sm.caravanCursor < max {
				sm.caravanCursor++
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
		if sm.hasFocus && sm.focusedSection == sphereSectionCaravans {
			return sm.handleCaravanAction(data)
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
	if sm.caravanLen > 0 {
		sections = append(sections, sphereSectionCaravans)
	}

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
			source:      pi.source,
		})
	}
	return items
}

// handleProcessAttach handles 'a' on a process item. Every sphere process
// (Prefect, Consul, Chronicle, Ledger, Broker) is a PID-file daemon — none
// of them run inside a tmux session, so a direct attach is never possible.
// When the process is running, show a descriptive message naming its log
// file instead of the generic "no active session" text.
func (sm sphereModel) handleProcessAttach() (sphereModel, tea.Cmd) {
	if sm.processCursor >= len(sm.processItems) {
		return sm, nil
	}
	item := sm.processItems[sm.processCursor]
	if !item.running {
		return sm, func() tea.Msg { return noSessionMsg{} }
	}
	info, ok := sphereProcessMap[item.name]
	if !ok {
		return sm, func() tea.Msg { return noSessionMsg{} }
	}
	lc, ok := daemon.SphereLifecycle(info.cliName)
	if !ok {
		return sm, func() tea.Msg { return noSessionMsg{} }
	}
	daemonMsg := fmt.Sprintf("%s runs as a daemon; view logs at %s", item.name, lc.LogPath())
	return sm, func() tea.Msg { return noSessionMsg{message: daemonMsg} }
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

	// Caravans — focusable section with Active/Drydocked subheadings.
	if len(data.Caravans) > 0 {
		sm.renderCaravansSection(&b, data.Caravans)
	}

	// Token summary (aggregated across all worlds).
	statusformat.FormatTokenSection(&b, toTokenDetail(data.Tokens))

	// Inbox (escalations + mail) — absent when zero, matching sol status.
	if line := statusformat.FormatInboxLine(data.MailCount, toEscalationSummaryDetail(data.Escalations)); line != "" {
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Inline "no active session" message.
	if sm.showNoSession {
		sessionMsg := sm.noSessionMessage
		if sessionMsg == "" {
			sessionMsg = "no active session"
		}
		b.WriteString(warnStyle.Render("  " + sessionMsg))
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
	b.WriteString("  " + padRight(dimStyle.Render("WORLD"), 16) + " " + padRight(dimStyle.Render("AGENTS"), 20) + " " + padRight(dimStyle.Render("HEALTH"), 14) + " " + padRight(dimStyle.Render("FORGE"), 7) + " " + padRight(dimStyle.Render("SENTINEL"), 10) + " " + dimStyle.Render("MR QUEUE") + "\n")

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
		return "  " + padRight(w.Name, 16) + " " + padRight(agents, 20) + " " + padRight(sleepingBadge, 14) + " " + padRight(dimStyle.Render("—"), 7) + " " + padRight(dimStyle.Render("—"), 10) + " " + dimStyle.Render("—")
	}

	// Agents column with optional spinner.
	agents := statusformat.FormatMaxActive(w.MaxActive, w.Agents)
	if w.Working > 0 || w.Stalled > 0 || w.Dead > 0 {
		agents = fmt.Sprintf("%s (%d work", statusformat.FormatMaxActive(w.MaxActive, w.Agents), w.Working)
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

	return "  " + padRight(w.Name, 16) + " " + padRight(agents, 20) + " " + padRight(health, 14) + " " + padRight(forge, 7) + " " + padRight(sentinel, 10) + " " + mrQueue
}

// renderCaravansSection renders the Caravans section with Active/Drydocked subheadings.
func (sm sphereModel) renderCaravansSection(b *strings.Builder, caravans []status.CaravanInfo) {
	isFocused := sm.hasFocus && sm.focusedSection == sphereSectionCaravans

	if isFocused {
		b.WriteString("  " + focusIndicator + " " + focusStyle.Render("Caravans"))
	} else {
		b.WriteString(headerStyle.Render("Caravans"))
	}
	b.WriteString("\n")

	opts := statusformat.CaravanRenderOpts{
		MaxProgressWidth: sm.width / 3,
		ShowCursor:       isFocused,
		Cursor:           sm.caravanCursor,
		ProgressFn: func(id string, fraction float64, maxWidth int) string {
			if p, ok := sm.caravanProgress[id]; ok {
				p.Width = maxWidth
				return p.ViewAs(fraction)
			}
			return ""
		},
		SelectFn: func(line string) string {
			return selectStyle.Render(padRight(line, sm.width))
		},
	}
	statusformat.RenderCaravanRows(b, toCaravanDetails(caravans), opts)
	b.WriteString("\n")
}

// handleCaravanAction handles enter on a caravan item — opens peek mode.
func (sm sphereModel) handleCaravanAction(data *status.SphereStatus) (sphereModel, tea.Cmd) {
	if data == nil || len(data.Caravans) == 0 {
		return sm, nil
	}

	items := buildCaravanPeekItems(data.Caravans)
	if len(items) == 0 {
		return sm, nil
	}

	msg := peekMsg{
		items:         items,
		initialCursor: sm.caravanCursor,
		fromView:      viewSphere,
	}
	return sm, func() tea.Msg { return msg }
}

func (sm sphereModel) renderFooter(lastRefresh time.Time) string {
	help := dimStyle.Render("q quit · ↑↓ select · tab section · enter drill in · R restart · i inbox · r refresh")

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
// This is a dash-level helper shared between sphere.go, world.go, and their
// tests. The rendering logic itself lives in statusformat.RenderCaravanRows.
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

// toCaravanDetails converts a status.CaravanInfo slice into the DTO form
// expected by statusformat.RenderCaravanRows. Shared by sphere.go and world.go.
func toCaravanDetails(caravans []status.CaravanInfo) []statusformat.CaravanDetail {
	result := make([]statusformat.CaravanDetail, len(caravans))
	for i, c := range caravans {
		phases := make([]statusformat.PhaseProgressDetail, len(c.Phases))
		for j, p := range c.Phases {
			phases[j] = statusformat.PhaseProgressDetail{
				Phase:  p.Phase,
				Total:  p.Total,
				Closed: p.Closed,
			}
		}
		result[i] = statusformat.CaravanDetail{
			ID:          c.ID,
			Name:        c.Name,
			Status:      c.Status,
			TotalItems:  c.TotalItems,
			ClosedItems: c.ClosedItems,
			Phases:      phases,
		}
	}
	return result
}

// toEscalationSummaryDetail converts a status.EscalationSummary into the DTO
// form expected by statusformat.FormatInboxLine. Shared by sphere.go and
// world.go.
func toEscalationSummaryDetail(e *status.EscalationSummary) *statusformat.EscalationSummaryDetail {
	if e == nil {
		return nil
	}
	return &statusformat.EscalationSummaryDetail{
		Total:      e.Total,
		BySeverity: e.BySeverity,
	}
}

// toTokenDetail converts a status.TokenInfo into the DTO form expected by
// statusformat.FormatTokenSection. Shared by sphere.go and world.go.
func toTokenDetail(t status.TokenInfo) statusformat.TokenDetail {
	rbd := make([]statusformat.RuntimeTokenDetail, len(t.RuntimeBreakdown))
	for i, r := range t.RuntimeBreakdown {
		rbd[i] = statusformat.RuntimeTokenDetail{
			Runtime:      r.Runtime,
			InputTokens:  r.InputTokens,
			OutputTokens: r.OutputTokens,
			CostUSD:      r.CostUSD,
		}
	}
	return statusformat.TokenDetail{
		InputTokens:      t.InputTokens,
		OutputTokens:     t.OutputTokens,
		CacheTokens:      t.CacheTokens,
		AgentCount:       t.AgentCount,
		CostUSD:          t.CostUSD,
		RuntimeBreakdown: rbd,
	}
}
