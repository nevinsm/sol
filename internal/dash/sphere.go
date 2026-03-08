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

// sphereModel handles the sphere overview.
type sphereModel struct {
	width  int
	height int

	// Row selection for the worlds table.
	cursor    int
	worldRows int

	// Spinners for active processes — one per named process.
	processSpinners map[string]spinner.Model

	// Spinners for worlds that have working agents.
	worldSpinners map[string]spinner.Model

	// Progress bars for caravans.
	caravanProgress map[string]progress.Model
}

func newSphereModel() sphereModel {
	return sphereModel{
		processSpinners: make(map[string]spinner.Model),
		worldSpinners:   make(map[string]spinner.Model),
		caravanProgress: make(map[string]progress.Model),
	}
}

func (sm sphereModel) init() tea.Cmd {
	return nil
}

// updateData syncs spinner and progress state with fresh data.
func (sm *sphereModel) updateData(data *status.SphereStatus) {
	if data == nil {
		return
	}

	// Sync process spinners.
	sm.syncProcessSpinner("Prefect", data.Prefect.Running)
	sm.syncProcessSpinner("Consul", data.Consul.Running)
	sm.syncProcessSpinner("Chronicle", data.Chronicle.Running)
	sm.syncProcessSpinner("Ledger", data.Ledger.Running)
	sm.syncProcessSpinner("Broker", data.Broker.Running)
	sm.syncProcessSpinner("Senate", data.Senate.Running)

	// Sync world spinners.
	for _, w := range data.Worlds {
		if w.Working > 0 {
			if _, ok := sm.worldSpinners[w.Name]; !ok {
				s := spinner.New()
				s.Spinner = spinner.Dot
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

func (sm sphereModel) update(msg tea.KeyMsg, data *status.SphereStatus) (sphereModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if sm.cursor > 0 {
			sm.cursor--
		}
	case "down", "j":
		max := sm.worldRows - 1
		if max < 0 {
			max = 0
		}
		if sm.cursor < max {
			sm.cursor++
		}
	}
	return sm, nil
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

func (sm sphereModel) view(data *status.SphereStatus, lastRefresh time.Time) string {
	if data == nil {
		return "Gathering sphere status..."
	}

	var b strings.Builder

	// Header.
	b.WriteString(headerStyle.Render("Sol Sphere"))
	b.WriteString("  ")
	b.WriteString(healthBadge(data.Health))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(data.SOLHome))
	b.WriteString("\n\n")

	// Processes.
	b.WriteString(headerStyle.Render("Processes"))
	b.WriteString("\n")
	sm.renderProcess(&b, "Prefect", data.Prefect.Running)
	sm.renderProcess(&b, "Consul", data.Consul.Running)
	sm.renderProcess(&b, "Chronicle", data.Chronicle.Running)
	sm.renderProcess(&b, "Ledger", data.Ledger.Running)
	sm.renderProcess(&b, "Broker", data.Broker.Running)
	sm.renderProcess(&b, "Senate", data.Senate.Running)
	b.WriteString("\n")

	// Worlds table.
	if len(data.Worlds) == 0 {
		b.WriteString(dimStyle.Render("No worlds initialized."))
		b.WriteString("\n")
	} else {
		b.WriteString(headerStyle.Render("Worlds"))
		b.WriteString("\n")
		sm.renderWorldsTable(&b, data.Worlds)
		b.WriteString("\n")
	}

	// Caravans.
	if len(data.Caravans) > 0 {
		b.WriteString(headerStyle.Render("Caravans"))
		b.WriteString("\n")
		sm.renderCaravans(&b, data.Caravans)
		b.WriteString("\n")
	}

	// Footer.
	b.WriteString(sm.renderFooter(lastRefresh))

	return b.String()
}

func (sm sphereModel) renderProcess(b *strings.Builder, name string, running bool) {
	indicator := statusIndicator(running)
	if running {
		if s, ok := sm.processSpinners[name]; ok {
			indicator = s.View()
		}
	}
	b.WriteString(fmt.Sprintf("  %s %-12s\n", indicator, name))
}

func (sm sphereModel) renderWorldsTable(b *strings.Builder, worlds []status.WorldSummary) {
	// Column headers.
	b.WriteString(fmt.Sprintf("  %-16s %-20s %-14s %-5s %-7s %-10s %-10s\n",
		dimStyle.Render("WORLD"),
		dimStyle.Render("AGENTS"),
		dimStyle.Render("HEALTH"),
		dimStyle.Render("GOV"),
		dimStyle.Render("FORGE"),
		dimStyle.Render("SENTINEL"),
		dimStyle.Render("MR QUEUE"),
	))

	for i, w := range worlds {
		line := sm.renderWorldRow(w)
		if i == sm.cursor {
			b.WriteString(selectStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
}

func (sm sphereModel) renderWorldRow(w status.WorldSummary) string {
	if w.Sleeping {
		return fmt.Sprintf("  %-16s %-20s %-14s %-5s %-7s %-10s %-10s",
			w.Name,
			dimStyle.Render("—"), sleepingBadge,
			dimStyle.Render("—"), dimStyle.Render("—"),
			dimStyle.Render("—"), dimStyle.Render("—"),
		)
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

	return fmt.Sprintf("  %-16s %-20s %-14s %-5s %-7s %-10s %-10s",
		w.Name, agents, health, gov, forge, sentinel, mrQueue)
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
		b.WriteString(fmt.Sprintf("  %s  %s  %s  %s\n",
			c.Name, progressStr,
			dimStyle.Render(fmt.Sprintf("%d/%d merged", c.ClosedItems, c.TotalItems)),
			dimStyle.Render(phaseSummary),
		))
	}
}

func (sm sphereModel) renderFooter(lastRefresh time.Time) string {
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
