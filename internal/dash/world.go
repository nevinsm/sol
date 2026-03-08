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
	sectionOutposts worldSection = iota
	sectionEnvoys
	sectionMergeQueue
)

// worldModel handles the world detail view.
type worldModel struct {
	width  int
	height int

	// Section focus and per-section cursors.
	focusedSection worldSection
	outpostCursor  int
	envoyCursor    int
	mrCursor       int

	// Section row counts.
	outpostLen int
	envoyLen   int
	mrLen      int

	// Inline "no active session" message.
	showNoSession bool

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

// updateData syncs spinners with fresh data.
func (wm *worldModel) updateData(data *status.WorldStatus) {
	if data == nil {
		return
	}

	// Sphere process spinners.
	wm.syncProcessSpinner("Prefect", data.Prefect.Running)
	wm.syncProcessSpinner("Chronicle", data.Chronicle.Running)
	wm.syncProcessSpinner("Ledger", data.Ledger.Running)
	wm.syncProcessSpinner("Broker", data.Broker.Running)
	wm.syncProcessSpinner("Senate", data.Senate.Running)

	// World process spinners.
	wm.syncProcessSpinner("Forge", data.Forge.Running)
	wm.syncProcessSpinner("Sentinel", data.Sentinel.Running)
	wm.syncProcessSpinner("Governor", data.Governor.Running)

	// Agent spinners — working agents get spinners.
	active := make(map[string]bool)
	for _, a := range data.Agents {
		if a.State == "working" && a.SessionAlive {
			active[a.Name] = true
			if _, ok := wm.agentSpinners[a.Name]; !ok {
				s := spinner.New()
				s.Spinner = spinner.Dot
				wm.agentSpinners[a.Name] = s
			}
		}
	}
	for _, e := range data.Envoys {
		if e.State == "working" && e.SessionAlive {
			active[e.Name] = true
			if _, ok := wm.agentSpinners[e.Name]; !ok {
				s := spinner.New()
				s.Spinner = spinner.Dot
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

	wm.outpostLen = len(data.Agents)
	wm.envoyLen = len(data.Envoys)
	wm.mrLen = len(data.MergeRequests)
}

func (wm *worldModel) syncProcessSpinner(name string, running bool) {
	if running {
		if _, ok := wm.processSpinners[name]; !ok {
			s := spinner.New()
			s.Spinner = spinner.Dot
			wm.processSpinners[name] = s
		}
	} else {
		delete(wm.processSpinners, name)
	}
}

// availableSections returns the sections that have rows, in order.
func (wm worldModel) availableSections() []worldSection {
	var sections []worldSection
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
	case sectionOutposts:
		return wm.outpostCursor
	case sectionEnvoys:
		return wm.envoyCursor
	case sectionMergeQueue:
		return wm.mrCursor
	}
	return 0
}

// setCursor sets the cursor for a section.
func (wm *worldModel) setCursor(s worldSection, v int) {
	switch s {
	case sectionOutposts:
		wm.outpostCursor = v
	case sectionEnvoys:
		wm.envoyCursor = v
	case sectionMergeQueue:
		wm.mrCursor = v
	}
}

func (wm worldModel) update(msg tea.KeyMsg, data *status.WorldStatus) (worldModel, tea.Cmd) {
	// Any key dismisses the "no active session" message.
	if wm.showNoSession {
		wm.showNoSession = false
		return wm, nil
	}

	switch msg.String() {
	case "up", "k":
		cur := wm.cursor(wm.focusedSection)
		if cur > 0 {
			wm.setCursor(wm.focusedSection, cur-1)
		}

	case "down", "j":
		cur := wm.cursor(wm.focusedSection)
		max := wm.sectionLen(wm.focusedSection) - 1
		if max < 0 {
			max = 0
		}
		if cur < max {
			wm.setCursor(wm.focusedSection, cur+1)
		}

	case "tab":
		wm.cycleFocus(1)

	case "shift+tab":
		wm.cycleFocus(-1)

	case "esc", "h", "left":
		// Pop back to sphere view.
		return wm, func() tea.Msg { return popMsg{} }

	case "enter", "l", "right":
		// Attach to agent/envoy session.
		return wm.handleAttach(data)
	}
	return wm, nil
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

// handleAttach checks if the selected row has a live session and returns an attach command.
func (wm worldModel) handleAttach(data *status.WorldStatus) (worldModel, tea.Cmd) {
	if data == nil {
		return wm, nil
	}

	switch wm.focusedSection {
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

func (wm worldModel) view(data *status.WorldStatus, lastRefresh time.Time, healthEmphasis bool, agentHighlights map[string]int) string {
	if data == nil {
		return "Gathering world status..."
	}

	var b strings.Builder

	// Header.
	b.WriteString(headerStyle.Render(fmt.Sprintf("World: %s", data.World)))
	b.WriteString("  ")
	b.WriteString(healthBadgeWithEmphasis(data.HealthString(), healthEmphasis))
	b.WriteString("\n\n")

	// Sphere Processes.
	b.WriteString(headerStyle.Render("Sphere Processes"))
	b.WriteString("\n")
	wm.renderProcess(&b, "Prefect", data.Prefect.Running, formatPrefectDetail(data.Prefect))
	wm.renderProcess(&b, "Chronicle", data.Chronicle.Running, formatChronicleDetail(data.Chronicle))
	wm.renderProcess(&b, "Ledger", data.Ledger.Running, formatLedgerDetail(data.Ledger))
	wm.renderProcess(&b, "Broker", data.Broker.Running, formatBrokerDetail(data.Broker))
	wm.renderProcess(&b, "Senate", data.Senate.Running, formatSenateDetail(data.Senate))
	b.WriteString("\n")

	// World Processes.
	b.WriteString(headerStyle.Render("World Processes"))
	b.WriteString("\n")
	wm.renderProcess(&b, "Forge", data.Forge.Running, formatForgeDetail(data.Forge))
	wm.renderProcess(&b, "Sentinel", data.Sentinel.Running, formatSentinelDetail(data.Sentinel))
	if data.Governor.Running {
		wm.renderProcess(&b, "Governor", true, formatGovernorDetail(data.Governor))
	}
	b.WriteString("\n")

	// Outposts.
	if len(data.Agents) > 0 {
		sectionHeader := fmt.Sprintf("Outposts (%d)", len(data.Agents))
		if wm.focusedSection == sectionOutposts {
			b.WriteString(focusStyle.Render(sectionHeader))
		} else {
			b.WriteString(headerStyle.Render(sectionHeader))
		}
		b.WriteString("\n")
		wm.renderAgentsTable(&b, data.Agents, agentHighlights)
		b.WriteString("\n")
	}

	// Envoys.
	if len(data.Envoys) > 0 {
		sectionHeader := fmt.Sprintf("Envoys (%d)", len(data.Envoys))
		if wm.focusedSection == sectionEnvoys {
			b.WriteString(focusStyle.Render(sectionHeader))
		} else {
			b.WriteString(headerStyle.Render(sectionHeader))
		}
		b.WriteString("\n")
		wm.renderEnvoysTable(&b, data.Envoys)
		b.WriteString("\n")
	}

	if len(data.Agents) == 0 && len(data.Envoys) == 0 {
		b.WriteString(dimStyle.Render("No agents registered."))
		b.WriteString("\n")
	}

	// Caravans.
	if len(data.Caravans) > 0 {
		b.WriteString(headerStyle.Render("Caravans"))
		b.WriteString("\n")
		wm.renderCaravans(&b, data.Caravans)
		b.WriteString("\n")
	}

	// Merge queue.
	mqHeader := "Merge Queue"
	if wm.focusedSection == sectionMergeQueue {
		b.WriteString(focusStyle.Render(mqHeader))
	} else {
		b.WriteString(headerStyle.Render(mqHeader))
	}
	b.WriteString("\n")
	wm.renderMergeQueue(&b, data.MergeQueue, data.MergeRequests)
	b.WriteString("\n")

	// Summary.
	b.WriteString(wm.renderSummary(data))

	// Inline "no active session" message.
	if wm.showNoSession {
		b.WriteString(warnStyle.Render("  no active session"))
		b.WriteString("\n\n")
	}

	// Footer.
	b.WriteString(wm.renderFooter(lastRefresh))

	return b.String()
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

func (wm worldModel) renderAgentsTable(b *strings.Builder, agents []status.AgentStatus, agentHighlights map[string]int) {
	// Column headers.
	b.WriteString("  " + padRight(dimStyle.Render("NAME"), 14) + " " + padRight(dimStyle.Render("STATE"), 18) + " " + padRight(dimStyle.Render("SESSION"), 10) + " " + dimStyle.Render("WORK") + "\n")

	for i, a := range agents {
		line := wm.renderAgentRow(a)
		if wm.focusedSection == sectionOutposts && i == wm.outpostCursor {
			b.WriteString(selectStyle.Render(line))
		} else if agentHighlights != nil {
			if _, highlighted := agentHighlights[a.Name]; highlighted {
				b.WriteString(highlightStyle.Render(line))
			} else {
				b.WriteString(line)
			}
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
}

func (wm worldModel) renderAgentRow(a status.AgentStatus) string {
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
			state = errorStyle.Render("working (dead!)")
		}
	case "idle":
		state = dimStyle.Render("idle")
	case "stalled":
		state = warnStyle.Render("stalled")
	}

	sess := dimStyle.Render("—")
	if a.State == "working" || a.State == "stalled" {
		if a.SessionAlive {
			sess = okStyle.Render("alive")
		} else {
			sess = errorStyle.Render("dead")
		}
	}

	work := dimStyle.Render("—")
	if a.TetherItem != "" {
		work = fmt.Sprintf("%s: %s", a.TetherItem, a.WorkTitle)
	}

	return "  " + padRight(name, 14) + " " + padRight(state, 18) + " " + padRight(sess, 10) + " " + work
}

func (wm worldModel) renderEnvoysTable(b *strings.Builder, envoys []status.EnvoyStatus) {
	b.WriteString("  " + padRight(dimStyle.Render("NAME"), 14) + " " + padRight(dimStyle.Render("STATE"), 18) + " " + padRight(dimStyle.Render("SESSION"), 10) + " " + padRight(dimStyle.Render("WORK"), 24) + " " + dimStyle.Render("BRIEF") + "\n")

	for i, e := range envoys {
		line := wm.renderEnvoyRow(e)
		if wm.focusedSection == sectionEnvoys && i == wm.envoyCursor {
			b.WriteString(selectStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
}

func (wm worldModel) renderEnvoyRow(e status.EnvoyStatus) string {
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
			state = errorStyle.Render("working (dead!)")
		}
	case "idle":
		state = dimStyle.Render("idle")
	case "stalled":
		state = warnStyle.Render("stalled")
	}

	sess := dimStyle.Render("—")
	if e.State == "working" || e.State == "stalled" {
		if e.SessionAlive {
			sess = okStyle.Render("alive")
		} else {
			sess = errorStyle.Render("dead")
		}
	}

	work := dimStyle.Render("—")
	if e.TetherItem != "" {
		work = e.WorkTitle
	}

	brief := dimStyle.Render("—")
	if e.BriefAge != "" {
		brief = e.BriefAge + " ago"
	}

	return "  " + padRight(name, 14) + " " + padRight(state, 18) + " " + padRight(sess, 10) + " " + padRight(work, 24) + " " + brief
}

func (wm worldModel) renderMergeQueue(b *strings.Builder, mq status.MergeQueueInfo, mrs []status.MergeRequestInfo) {
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

	// Individual MR rows.
	if len(mrs) > 0 {
		b.WriteString("  " + padRight(dimStyle.Render("ID"), 20) + " " + padRight(dimStyle.Render("WRIT"), 20) + " " + padRight(dimStyle.Render("STATUS"), 10) + " " + dimStyle.Render("TITLE") + "\n")
		for i, mr := range mrs {
			line := wm.renderMRRow(mr)
			if wm.focusedSection == sectionMergeQueue && i == wm.mrCursor {
				b.WriteString(selectStyle.Render(line))
			} else {
				b.WriteString(line)
			}
			b.WriteString("\n")
		}
	}
}

func (wm worldModel) renderMRRow(mr status.MergeRequestInfo) string {
	phase := mr.Phase
	switch mr.Phase {
	case "ready":
		phase = okStyle.Render("ready")
	case "claimed":
		phase = warnStyle.Render("in progress")
	case "failed":
		phase = errorStyle.Render("failed")
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
		b.WriteString(fmt.Sprintf("  %s  %s  %s  %s\n",
			c.Name, progressStr,
			dimStyle.Render(fmt.Sprintf("%d/%d merged", c.ClosedItems, c.TotalItems)),
			dimStyle.Render(phaseSummary),
		))
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
	help := dimStyle.Render("q quit · ↑↓ select · tab section · enter attach · esc back · r refresh")

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
