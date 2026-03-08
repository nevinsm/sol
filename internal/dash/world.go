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

// worldModel handles the world detail view.
type worldModel struct {
	width  int
	height int

	// Row selection for the outposts table.
	cursor   int
	agentLen int

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

	// Process spinners.
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

	wm.agentLen = len(data.Agents)
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

func (wm worldModel) update(msg tea.KeyMsg, _ *status.WorldStatus) (worldModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if wm.cursor > 0 {
			wm.cursor--
		}
	case "down", "j":
		max := wm.agentLen - 1
		if max < 0 {
			max = 0
		}
		if wm.cursor < max {
			wm.cursor++
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

func (wm worldModel) view(data *status.WorldStatus, lastRefresh time.Time) string {
	if data == nil {
		return "Gathering world status..."
	}

	var b strings.Builder

	// Header.
	b.WriteString(headerStyle.Render(fmt.Sprintf("World: %s", data.World)))
	b.WriteString("  ")
	b.WriteString(healthBadge(data.HealthString()))
	b.WriteString("\n\n")

	// Processes.
	b.WriteString(headerStyle.Render("Processes"))
	b.WriteString("\n")
	wm.renderProcess(&b, "Forge", data.Forge.Running)
	wm.renderProcess(&b, "Sentinel", data.Sentinel.Running)
	if data.Governor.Running {
		wm.renderProcess(&b, "Governor", true)
	}
	b.WriteString("\n")

	// Outposts.
	if len(data.Agents) > 0 {
		b.WriteString(headerStyle.Render(fmt.Sprintf("Outposts (%d)", len(data.Agents))))
		b.WriteString("\n")
		wm.renderAgentsTable(&b, data.Agents)
		b.WriteString("\n")
	}

	// Envoys.
	if len(data.Envoys) > 0 {
		b.WriteString(headerStyle.Render(fmt.Sprintf("Envoys (%d)", len(data.Envoys))))
		b.WriteString("\n")
		wm.renderEnvoysTable(&b, data.Envoys)
		b.WriteString("\n")
	}

	if len(data.Agents) == 0 && len(data.Envoys) == 0 {
		b.WriteString(dimStyle.Render("No agents registered."))
		b.WriteString("\n")
	}

	// Merge queue.
	b.WriteString(headerStyle.Render("Merge Queue"))
	b.WriteString("\n")
	wm.renderMergeQueue(&b, data.MergeQueue)
	b.WriteString("\n")

	// Caravans.
	if len(data.Caravans) > 0 {
		b.WriteString(headerStyle.Render("Caravans"))
		b.WriteString("\n")
		wm.renderCaravans(&b, data.Caravans)
		b.WriteString("\n")
	}

	// Footer.
	b.WriteString(wm.renderFooter(lastRefresh))

	return b.String()
}

func (wm worldModel) renderProcess(b *strings.Builder, name string, running bool) {
	indicator := statusIndicator(running)
	if running {
		if s, ok := wm.processSpinners[name]; ok {
			indicator = s.View()
		}
	}
	b.WriteString(fmt.Sprintf("  %s %-12s\n", indicator, name))
}

func (wm worldModel) renderAgentsTable(b *strings.Builder, agents []status.AgentStatus) {
	// Column headers.
	b.WriteString(fmt.Sprintf("  %-14s %-18s %-10s %s\n",
		dimStyle.Render("NAME"),
		dimStyle.Render("STATE"),
		dimStyle.Render("SESSION"),
		dimStyle.Render("WORK"),
	))

	for i, a := range agents {
		line := wm.renderAgentRow(a)
		if i == wm.cursor {
			b.WriteString(selectStyle.Render(line))
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

	return fmt.Sprintf("  %-14s %-18s %-10s %s", name, state, sess, work)
}

func (wm worldModel) renderEnvoysTable(b *strings.Builder, envoys []status.EnvoyStatus) {
	b.WriteString(fmt.Sprintf("  %-14s %-18s %-10s %-24s %s\n",
		dimStyle.Render("NAME"),
		dimStyle.Render("STATE"),
		dimStyle.Render("SESSION"),
		dimStyle.Render("WORK"),
		dimStyle.Render("BRIEF"),
	))

	for _, e := range envoys {
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

		b.WriteString(fmt.Sprintf("  %-14s %-18s %-10s %-24s %s\n",
			name, state, sess, work, brief))
	}
}

func (wm worldModel) renderMergeQueue(b *strings.Builder, mq status.MergeQueueInfo) {
	if mq.Total == 0 {
		b.WriteString(dimStyle.Render("  empty"))
		b.WriteString("\n")
		return
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
	b.WriteString(fmt.Sprintf("  %s\n", strings.Join(parts, ", ")))
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

func (wm worldModel) renderFooter(lastRefresh time.Time) string {
	help := dimStyle.Render("q quit · ↑↓ select · r refresh")

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
