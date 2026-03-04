package status

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
	"github.com/nevinsm/sol/internal/config"
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
	fmt.Fprintf(tw, "  WORLD\tAGENTS\tENVOYS\tGOV\tFORGE\tSENTINEL\tMR QUEUE\tHEALTH\n")

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

		envoys := fmt.Sprintf("%d", w.Envoys)

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
				mrQueue += errorStyle.Render(fmt.Sprintf(", %d failed", w.MRFailed))
			}
		}

		health := healthBadge(w.Health)

		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			w.Name, agents, envoys, gov, forge, sentinel, mrQueue, health)
	}
	tw.Flush()
}

func renderCaravansTable(b *strings.Builder, caravans []CaravanInfo) {
	for _, c := range caravans {
		if len(c.Phases) > 0 {
			// Phase-aware display.
			var parts []string
			for _, p := range c.Phases {
				part := fmt.Sprintf("phase %d: %d/%d merged", p.Phase, p.Closed, p.Total)
				if p.Done > 0 {
					part += fmt.Sprintf(", %d awaiting merge", p.Done)
				}
				if p.Ready > 0 {
					part += fmt.Sprintf(", %d ready", p.Ready)
				}
				parts = append(parts, part)
			}
			progress := fmt.Sprintf("%d items  %s", c.TotalItems, strings.Join(parts, ", "))
			b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
				c.ID, c.Name, dimStyle.Render(progress)))
		} else {
			blocked := c.TotalItems - c.ClosedItems - c.DoneItems - c.ReadyItems
			progress := fmt.Sprintf("%d/%d merged", c.ClosedItems, c.TotalItems)
			if c.DoneItems > 0 {
				progress += fmt.Sprintf(", %d awaiting merge", c.DoneItems)
			}
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
	if ws.Governor.Running {
		renderProcess(&b, "Governor", true,
			formatGovernorDetail(ws.Governor))
	}
	b.WriteString("\n")

	// Outposts (role=agent only).
	if len(ws.Agents) > 0 {
		b.WriteString(headerStyle.Render(fmt.Sprintf("Outposts (%d)", len(ws.Agents))))
		b.WriteString("\n")
		renderAgentsTable(&b, ws.Agents)
		b.WriteString("\n")
	}

	// Envoys.
	if len(ws.Envoys) > 0 {
		b.WriteString(headerStyle.Render(fmt.Sprintf("Envoys (%d)", len(ws.Envoys))))
		b.WriteString("\n")
		renderEnvoysTable(&b, ws.Envoys)
		b.WriteString("\n")
	}

	// Show "no agents" if neither outposts nor envoys exist.
	if len(ws.Agents) == 0 && len(ws.Envoys) == 0 {
		b.WriteString(dimStyle.Render("No agents registered."))
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
	renderWorldSummary(&b, ws)

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

func formatGovernorDetail(g GovernorInfo) string {
	detail := ""
	if g.BriefAge != "" {
		detail = "brief: " + g.BriefAge + " ago"
	}
	return detail
}

func renderAgentsTable(b *strings.Builder, agents []AgentStatus) {
	tw := tabwriter.NewWriter(b, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  NAME\tSTATE\tSESSION\tNUDGE\tWORK\n")

	for _, a := range agents {
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

		nudge := dimStyle.Render("—")
		if a.NudgeCount > 0 {
			nudge = warnStyle.Render(fmt.Sprintf("%d pending", a.NudgeCount))
		}

		work := dimStyle.Render("—")
		if a.TetherItem != "" {
			work = fmt.Sprintf("%s: %s", a.TetherItem, a.WorkTitle)
		}

		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", a.Name, state, sess, nudge, work)
	}
	tw.Flush()
}

func renderEnvoysTable(b *strings.Builder, envoys []EnvoyStatus) {
	tw := tabwriter.NewWriter(b, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  NAME\tSTATE\tSESSION\tNUDGE\tWORK\tBRIEF\n")

	for _, e := range envoys {
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

		nudge := dimStyle.Render("—")
		if e.NudgeCount > 0 {
			nudge = warnStyle.Render(fmt.Sprintf("%d pending", e.NudgeCount))
		}

		work := dimStyle.Render("—")
		if e.TetherItem != "" {
			work = e.WorkTitle
		}

		brief := dimStyle.Render("—")
		if e.BriefAge != "" {
			brief = e.BriefAge + " ago"
		}

		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\n", e.Name, state, sess, nudge, work, brief)
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

func renderWorldSummary(b *strings.Builder, ws *WorldStatus) {
	parts := fmt.Sprintf("%d agents", ws.Summary.Total)
	if len(ws.Envoys) > 0 {
		parts += fmt.Sprintf(", %d envoys", len(ws.Envoys))
	}
	parts += fmt.Sprintf(" | %d working, %d idle", ws.Summary.Working, ws.Summary.Idle)
	if ws.Summary.Stalled > 0 {
		parts += warnStyle.Render(fmt.Sprintf(", %d stalled", ws.Summary.Stalled))
	}
	if ws.Summary.Dead > 0 {
		parts += errorStyle.Render(fmt.Sprintf(", %d dead", ws.Summary.Dead))
	}
	b.WriteString(dimStyle.Render(parts))
	b.WriteString("\n")
}

// RenderWorldConfig renders the config section for sol world status.
func RenderWorldConfig(world string, cfg config.WorldConfig) string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("Config"))
	b.WriteString("\n")

	sourceDisplay := cfg.World.SourceRepo
	if sourceDisplay == "" {
		sourceDisplay = dimStyle.Render("(none)")
	}
	b.WriteString(fmt.Sprintf("  Source repo:    %s\n", sourceDisplay))

	if cfg.Agents.Capacity == 0 {
		b.WriteString(fmt.Sprintf("  Agent capacity: %s\n", dimStyle.Render("unlimited")))
	} else {
		b.WriteString(fmt.Sprintf("  Agent capacity: %d\n", cfg.Agents.Capacity))
	}
	b.WriteString(fmt.Sprintf("  Model tier:     %s\n", cfg.Agents.ModelTier))
	b.WriteString(fmt.Sprintf("  Quality gates:  %d\n", len(cfg.Forge.QualityGates)))

	namePool := dimStyle.Render("(default)")
	if cfg.Agents.NamePoolPath != "" {
		namePool = cfg.Agents.NamePoolPath
	}
	b.WriteString(fmt.Sprintf("  Name pool:      %s\n", namePool))
	b.WriteString("\n")

	return b.String()
}
