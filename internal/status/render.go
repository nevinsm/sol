package status

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Section headers.
	headerStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")) // bright blue

	// Status indicators.
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // gray

	// Health badges.
	healthyBadge   = okStyle.Render("● healthy")
	unhealthyBadge = errorStyle.Render("● unhealthy")
	degradedBadge  = warnStyle.Render("● degraded")
	unknownBadge   = dimStyle.Render("● unknown")
)

func healthBadge(health string) string {
	switch health {
	case "healthy":
		return healthyBadge
	case "unhealthy":
		return unhealthyBadge
	case "degraded":
		return degradedBadge
	default:
		return unknownBadge
	}
}

func statusIndicator(running bool) string {
	if running {
		return okStyle.Render("✓")
	}
	return errorStyle.Render("✗")
}

// RenderSphere renders a SphereStatus as styled terminal output.
func RenderSphere(s *SphereStatus) string {
	var b strings.Builder

	// Header.
	b.WriteString(headerStyle.Render("Sol Sphere"))
	b.WriteString("  ")
	b.WriteString(healthBadge(s.Health))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(s.SOLHome))
	b.WriteString("\n\n")

	// Sphere processes.
	b.WriteString(headerStyle.Render("Processes"))
	b.WriteString("\n")
	renderProcess(&b, "Prefect", s.Prefect.Running,
		formatPrefectDetail(s.Prefect))
	renderProcess(&b, "Consul", s.Consul.Running,
		formatConsulDetail(s.Consul))
	renderProcess(&b, "Chronicle", s.Chronicle.Running,
		formatChronicleDetail(s.Chronicle))
	b.WriteString("\n")

	// Worlds table.
	if len(s.Worlds) == 0 {
		b.WriteString(dimStyle.Render("No worlds initialized."))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("Run: sol init --name=<world>"))
		b.WriteString("\n")
	} else {
		b.WriteString(headerStyle.Render("Worlds"))
		b.WriteString("\n")
		renderWorldsTable(&b, s.Worlds)
		b.WriteString("\n")
	}

	// Caravans (if any).
	if len(s.Caravans) > 0 {
		b.WriteString(headerStyle.Render("Caravans"))
		b.WriteString("\n")
		renderCaravansTable(&b, s.Caravans)
		b.WriteString("\n")
	}

	return b.String()
}

func renderProcess(b *strings.Builder, name string, running bool, detail string) {
	b.WriteString(fmt.Sprintf("  %s %-12s", statusIndicator(running), name))
	if detail != "" {
		b.WriteString(dimStyle.Render("  " + detail))
	}
	b.WriteString("\n")
}

func formatPrefectDetail(p PrefectInfo) string {
	if p.Running {
		return fmt.Sprintf("pid %d", p.PID)
	}
	return ""
}

func formatConsulDetail(c ConsulInfo) string {
	if !c.Running {
		return ""
	}
	parts := fmt.Sprintf("%d patrols", c.PatrolCount)
	if c.HeartbeatAge != "" {
		parts += fmt.Sprintf(", last %s ago", c.HeartbeatAge)
	}
	if c.Stale {
		parts += warnStyle.Render(" (stale)")
	}
	return parts
}

func formatChronicleDetail(c ChronicleInfo) string {
	if c.Running {
		return c.SessionName
	}
	return ""
}

func renderWorldsTable(b *strings.Builder, worlds []WorldSummary) {
	// Use tabwriter for alignment.
	tw := tabwriter.NewWriter(b, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  WORLD\tAGENTS\tFORGE\tSENTINEL\tMR QUEUE\tHEALTH\n")

	for _, w := range worlds {
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
				mrQueue += errorStyle.Render(fmt.Sprintf(", %d failed", w.MRFailed))
			}
		}

		health := healthBadge(w.Health)

		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\n",
			w.Name, agents, forge, sentinel, mrQueue, health)
	}
	tw.Flush()
}

func renderCaravansTable(b *strings.Builder, caravans []CaravanInfo) {
	for _, c := range caravans {
		blocked := c.TotalItems - c.DoneItems - c.ReadyItems
		progress := fmt.Sprintf("%d/%d done", c.DoneItems, c.TotalItems)
		if c.ReadyItems > 0 {
			progress += fmt.Sprintf(", %d ready", c.ReadyItems)
		}
		if blocked > 0 {
			progress += fmt.Sprintf(", %d blocked", blocked)
		}
		b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
			c.ID, c.Name, dimStyle.Render(progress)))
	}
}

// RenderWorld renders a WorldStatus as styled terminal output.
func RenderWorld(ws *WorldStatus) string {
	var b strings.Builder

	// Header.
	b.WriteString(headerStyle.Render(fmt.Sprintf("World: %s", ws.World)))
	b.WriteString("  ")
	b.WriteString(healthBadge(ws.HealthString()))
	b.WriteString("\n\n")

	// Processes.
	b.WriteString(headerStyle.Render("Processes"))
	b.WriteString("\n")
	renderProcess(&b, "Prefect", ws.Prefect.Running,
		formatPrefectDetail(ws.Prefect))
	renderProcess(&b, "Forge", ws.Forge.Running,
		formatForgeDetail(ws.Forge))
	renderProcess(&b, "Sentinel", ws.Sentinel.Running,
		formatSentinelDetail(ws.Sentinel))
	renderProcess(&b, "Chronicle", ws.Chronicle.Running,
		formatChronicleDetail(ws.Chronicle))
	b.WriteString("\n")

	// Agents.
	if len(ws.Agents) == 0 {
		b.WriteString(dimStyle.Render("No agents registered."))
		b.WriteString("\n")
	} else {
		b.WriteString(headerStyle.Render("Agents"))
		b.WriteString("\n")
		renderAgentsTable(&b, ws)
		b.WriteString("\n")
	}

	// Caravans.
	if len(ws.Caravans) > 0 {
		b.WriteString(headerStyle.Render("Caravans"))
		b.WriteString("\n")
		renderCaravansTable(&b, ws.Caravans)
		b.WriteString("\n")
	}

	// Merge queue.
	b.WriteString(headerStyle.Render("Merge Queue"))
	b.WriteString("\n")
	renderMergeQueue(&b, ws.MergeQueue)
	b.WriteString("\n")

	// Summary.
	renderSummary(&b, ws.Summary)

	return b.String()
}

func formatForgeDetail(f ForgeInfo) string {
	if f.Running {
		return f.SessionName
	}
	return ""
}

func formatSentinelDetail(s SentinelInfo) string {
	if s.Running {
		return s.SessionName
	}
	return ""
}

func renderAgentsTable(b *strings.Builder, ws *WorldStatus) {
	tw := tabwriter.NewWriter(b, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  AGENT\tSTATE\tSESSION\tWORK\n")

	for _, a := range ws.Agents {
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

		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", a.Name, state, sess, work)
	}
	tw.Flush()
}

func renderMergeQueue(b *strings.Builder, mq MergeQueueInfo) {
	if mq.Total == 0 {
		b.WriteString(dimStyle.Render("  empty"))
		b.WriteString("\n")
		return
	}
	parts := []string{}
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

func renderSummary(b *strings.Builder, s Summary) {
	parts := fmt.Sprintf("%d agents: %d working, %d idle",
		s.Total, s.Working, s.Idle)
	if s.Stalled > 0 {
		parts += warnStyle.Render(fmt.Sprintf(", %d stalled", s.Stalled))
	}
	if s.Dead > 0 {
		parts += errorStyle.Render(fmt.Sprintf(", %d dead", s.Dead))
	}
	b.WriteString(dimStyle.Render(parts))
	b.WriteString("\n")
}
