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
	sleepingBadge  = dimStyle.Render("○ sleeping")
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
	case "sleeping":
		return sleepingBadge
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

// optionalStatusIndicator returns a dim ○ for non-running optional processes
// instead of the alarming red ✗ used for required processes.
func optionalStatusIndicator(running bool) string {
	if running {
		return okStyle.Render("✓")
	}
	return dimStyle.Render("○")
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
	renderProcess(&b, "Prefect", s.Prefect.Running, true,
		formatPrefectDetail(s.Prefect))
	renderProcess(&b, "Consul", s.Consul.Running, true,
		formatConsulDetail(s.Consul))
	renderProcess(&b, "Chronicle", s.Chronicle.Running, false,
		formatChronicleDetail(s.Chronicle))
	renderProcess(&b, "Ledger", s.Ledger.Running, false,
		formatLedgerDetail(s.Ledger))
	renderProcess(&b, "Broker", s.Broker.Running, true,
		formatBrokerDetail(s.Broker))
	renderProcess(&b, "Senate", s.Senate.Running, false,
		formatSenateDetail(s.Senate))
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

	// Token summary (aggregated across all worlds).
	renderTokens(&b, s.Tokens)

	// Caravans (if any).
	if len(s.Caravans) > 0 {
		b.WriteString(headerStyle.Render("Caravans"))
		b.WriteString("\n")
		renderCaravansTable(&b, s.Caravans)
		b.WriteString("\n")
	}

	// Unified inbox count (escalations + mail).
	inboxCount := s.MailCount
	if s.Escalations != nil {
		inboxCount += s.Escalations.Total
	}
	if inboxCount > 0 {
		label := "items need attention"
		if inboxCount == 1 {
			label = "item needs attention"
		}
		b.WriteString(fmt.Sprintf("Inbox: %d %s\n", inboxCount, label))
		b.WriteString("\n")
	}

	return b.String()
}

func renderProcess(b *strings.Builder, name string, running bool, required bool, detail string) {
	indicator := optionalStatusIndicator(running)
	if required {
		indicator = statusIndicator(running)
	}
	b.WriteString(fmt.Sprintf("  %s %-12s", indicator, name))
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

func formatBrokerDetail(b BrokerInfo) string {
	if !b.Running {
		return ""
	}
	parts := fmt.Sprintf("%d accounts, %d dirs, %d patrols", b.Accounts, b.AgentDirs, b.PatrolCount)
	if b.HeartbeatAge != "" {
		parts += fmt.Sprintf(", last %s ago", b.HeartbeatAge)
	}
	if b.Stale {
		parts += warnStyle.Render(" (stale)")
	}
	// Show provider health when not healthy.
	switch b.ProviderHealth {
	case "degraded":
		parts += warnStyle.Render(" [provider: degraded]")
	case "down":
		parts += errorStyle.Render(" [provider: down]")
	}
	return parts
}

func formatChronicleDetail(c ChronicleInfo) string {
	if !c.Running {
		return ""
	}
	var parts string
	if c.PID > 0 {
		parts = fmt.Sprintf("pid %d", c.PID)
	}
	if c.HeartbeatAge != "" {
		if parts != "" {
			parts += " "
		}
		parts += dimStyle.Render(fmt.Sprintf("hb %s", c.HeartbeatAge))
	}
	if c.EventsProcessed > 0 {
		if parts != "" {
			parts += " "
		}
		parts += dimStyle.Render(fmt.Sprintf("ev %d", c.EventsProcessed))
	}
	if c.Stale {
		parts += warnStyle.Render(" (stale)")
	}
	return parts
}

func formatLedgerDetail(l LedgerInfo) string {
	if !l.Running {
		return ""
	}
	detail := ""
	if l.PID > 0 {
		detail = fmt.Sprintf("pid %d", l.PID)
	}
	if l.HeartbeatAge != "" {
		if detail != "" {
			detail += "  "
		}
		detail += fmt.Sprintf("hb %s", l.HeartbeatAge)
	}
	if l.Stale {
		detail += warnStyle.Render(" (stale)")
	}
	if detail == "" {
		return "running"
	}
	return detail
}

func formatSenateDetail(s SenateInfo) string {
	if s.Running {
		return s.SessionName
	}
	return ""
}

func renderWorldsTable(b *strings.Builder, worlds []WorldSummary) {
	// Use tabwriter for alignment.
	tw := tabwriter.NewWriter(b, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  WORLD\tAGENTS\tENVOYS\tGOV\tFORGE\tSENTINEL\tMR QUEUE\tHEALTH\n")

	for _, w := range worlds {
		if w.Sleeping {
			// Show active agent/envoy counts for sleeping worlds (soft sleep wind-down).
			agents := dimStyle.Render("—")
			if w.Agents > 0 {
				agents = fmt.Sprintf("%d", w.Agents)
			}
			envoys := dimStyle.Render("—")
			if w.Envoys > 0 {
				envoys = fmt.Sprintf("%d", w.Envoys)
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				w.Name,
				agents, envoys, dimStyle.Render("—"),
				dimStyle.Render("—"), dimStyle.Render("—"), dimStyle.Render("—"),
				sleepingBadge)
			continue
		}

		agentCount := fmt.Sprintf("%d", w.Agents)
		if w.Capacity > 0 {
			agentCount = fmt.Sprintf("%d/%d", w.Agents, w.Capacity)
		}
		agents := agentCount
		if w.Working > 0 || w.Stalled > 0 || w.Dead > 0 {
			agents = fmt.Sprintf("%s (%d work", agentCount, w.Working)
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
				if p.Dispatched > 0 {
					part += fmt.Sprintf(", %d in progress", p.Dispatched)
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
			blocked := c.TotalItems - c.ClosedItems - c.DoneItems - c.ReadyItems - c.DispatchedItems
			progress := fmt.Sprintf("%d/%d merged", c.ClosedItems, c.TotalItems)
			if c.DoneItems > 0 {
				progress += fmt.Sprintf(", %d awaiting merge", c.DoneItems)
			}
			if c.DispatchedItems > 0 {
				progress += fmt.Sprintf(", %d in progress", c.DispatchedItems)
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
	renderProcess(&b, "Prefect", ws.Prefect.Running, true,
		formatPrefectDetail(ws.Prefect))
	renderProcess(&b, "Forge", ws.Forge.Running, false,
		formatForgeDetail(ws.Forge))
	renderProcess(&b, "Sentinel", ws.Sentinel.Running, false,
		formatSentinelDetail(ws.Sentinel))
	renderProcess(&b, "Chronicle", ws.Chronicle.Running, false,
		formatChronicleDetail(ws.Chronicle))
	renderProcess(&b, "Ledger", ws.Ledger.Running, false,
		formatLedgerDetail(ws.Ledger))
	renderProcess(&b, "Broker", ws.Broker.Running, true,
		formatBrokerDetail(ws.Broker))
	renderProcess(&b, "Governor", ws.Governor.Running, false,
		formatGovernorDetail(ws.Governor))
	b.WriteString("\n")

	// Outposts (role=outpost only).
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

	// Token summary.
	renderTokens(&b, ws.Tokens)

	// Summary.
	renderWorldSummary(&b, ws)

	return b.String()
}

func formatForgeDetail(f ForgeInfo) string {
	if !f.Running {
		return ""
	}
	if f.Paused {
		return warnStyle.Render("paused") + fmt.Sprintf(" (pid %d)", f.PID)
	}
	if f.PatrolCount > 0 || f.MergesTotal > 0 {
		parts := fmt.Sprintf("pid %d, %d patrols, %d merged", f.PID, f.PatrolCount, f.MergesTotal)
		if f.HeartbeatAge != "" {
			parts += fmt.Sprintf(", last %s ago", f.HeartbeatAge)
		}
		if f.QueueDepth > 0 {
			parts += fmt.Sprintf(", %d queued", f.QueueDepth)
		}
		if f.Stale {
			parts += warnStyle.Render(" (stale)")
		}
		if f.Merging {
			parts += okStyle.Render(" [merging]")
		}
		return parts
	}
	if f.PID > 0 {
		detail := fmt.Sprintf("pid %d", f.PID)
		if f.Merging {
			detail += okStyle.Render(" [merging]")
		}
		return detail
	}
	return ""
}

func formatSentinelDetail(s SentinelInfo) string {
	if !s.Running {
		return ""
	}
	if s.PatrolCount > 0 {
		parts := fmt.Sprintf("%d patrols, %d checked", s.PatrolCount, s.AgentsChecked)
		if s.HeartbeatAge != "" {
			parts += fmt.Sprintf(", last %s ago", s.HeartbeatAge)
		}
		if s.Stale {
			parts += warnStyle.Render(" (stale)")
		}
		return parts
	}
	if s.PID > 0 {
		return fmt.Sprintf("pid %d", s.PID)
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

// stateStyle maps an agent/envoy state to its styled display string.
// This is the single source of truth for state→color mapping:
//   working (session alive) → green, working (session dead) → red,
//   idle → gray, stalled → yellow.
func stateStyle(state string, sessionAlive bool) string {
	switch state {
	case "working":
		if sessionAlive {
			return okStyle.Render("working")
		}
		return errorStyle.Render("working (dead!)")
	case "idle":
		return dimStyle.Render("idle")
	case "stalled":
		return warnStyle.Render("stalled")
	default:
		return state
	}
}

// sessionDisplay renders session liveness for an agent or envoy.
// Only working/stalled agents have a meaningful session indicator.
func sessionDisplay(state string, sessionAlive bool) string {
	if state == "working" || state == "stalled" {
		if sessionAlive {
			return okStyle.Render("alive")
		}
		return errorStyle.Render("dead")
	}
	return dimStyle.Render("—")
}

// nudgeDisplay renders a nudge count, or a dim dash if zero.
func nudgeDisplay(count int) string {
	if count > 0 {
		return fmt.Sprintf("%d", count)
	}
	return dimStyle.Render("—")
}

func renderAgentsTable(b *strings.Builder, agents []AgentStatus) {
	tw := tabwriter.NewWriter(b, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  NAME\tSTATE\tSESSION\tWORK\tNUDGE\n")

	for _, a := range agents {
		work := dimStyle.Render("—")
		if a.ActiveWrit != "" {
			work = fmt.Sprintf("%s: %s", a.ActiveWrit, a.WorkTitle)
		}

		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n",
			a.Name,
			stateStyle(a.State, a.SessionAlive),
			sessionDisplay(a.State, a.SessionAlive),
			work,
			nudgeDisplay(a.NudgeCount))
	}
	tw.Flush()
}

func renderEnvoysTable(b *strings.Builder, envoys []EnvoyStatus) {
	tw := tabwriter.NewWriter(b, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  NAME\tSTATE\tSESSION\tWORK\tBRIEF\tNUDGE\n")

	for _, e := range envoys {
		work := dimStyle.Render("—")
		if e.ActiveWrit != "" {
			work = e.WorkTitle
			// Show background tether count for multi-tether envoys.
			bgCount := e.TetheredCount - 1 // exclude active writ
			if bgCount > 0 {
				work += dimStyle.Render(fmt.Sprintf(" [+%d tethered]", bgCount))
			}
		}

		brief := dimStyle.Render("—")
		if e.BriefAge != "" {
			brief = e.BriefAge + " ago"
		}

		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\n",
			e.Name,
			stateStyle(e.State, e.SessionAlive),
			sessionDisplay(e.State, e.SessionAlive),
			work,
			brief,
			nudgeDisplay(e.NudgeCount))
	}
	tw.Flush()
}

// formatCompactTokens formats a token count as a compact human-readable string.
// < 1,000: show as-is (e.g., "842")
// 1,000–999,999: "1.2K", "340K"
// 1,000,000+: "1.2M", "14.3M"
func formatCompactTokens(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		v := float64(n) / 1000
		if v < 9.95 {
			return fmt.Sprintf("%.1fK", v)
		}
		return fmt.Sprintf("%.0fK", v)
	}
	v := float64(n) / 1_000_000
	if v < 9.95 {
		return fmt.Sprintf("%.1fM", v)
	}
	return fmt.Sprintf("%.0fM", v)
}

func renderTokens(b *strings.Builder, t TokenInfo) {
	if t.InputTokens == 0 && t.OutputTokens == 0 && t.CacheTokens == 0 {
		return
	}

	b.WriteString(headerStyle.Render("Tokens (24h)"))
	b.WriteString("\n")

	line := fmt.Sprintf("  %s in / %s out",
		formatCompactTokens(t.InputTokens),
		formatCompactTokens(t.OutputTokens))

	if t.AgentCount > 0 {
		line += fmt.Sprintf("  %s  %d agents", dimStyle.Render("•"), t.AgentCount)
	}

	b.WriteString(line)
	b.WriteString("\n\n")
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
	if ws.Capacity > 0 {
		parts += fmt.Sprintf(" (capacity: %d)", ws.Capacity)
	}
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

// RenderCombined renders sphere processes and world detail as a single view.
// Used when sol status auto-detects a world from the current directory.
// mailCount and escalations are used to compute a unified inbox count.
func RenderCombined(consul ConsulInfo, ws *WorldStatus, mailCount int, escalations ...*EscalationSummary) string {
	var b strings.Builder

	// Header — world-focused.
	b.WriteString(headerStyle.Render(fmt.Sprintf("World: %s", ws.World)))
	b.WriteString("  ")
	b.WriteString(healthBadge(ws.HealthString()))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(config.WorldDir(ws.World)))
	b.WriteString("\n\n")

	// Sphere-level processes.
	b.WriteString(headerStyle.Render("Sphere Processes"))
	b.WriteString("\n")
	renderProcess(&b, "Prefect", ws.Prefect.Running, true,
		formatPrefectDetail(ws.Prefect))
	renderProcess(&b, "Consul", consul.Running, true,
		formatConsulDetail(consul))
	renderProcess(&b, "Chronicle", ws.Chronicle.Running, false,
		formatChronicleDetail(ws.Chronicle))
	renderProcess(&b, "Ledger", ws.Ledger.Running, false,
		formatLedgerDetail(ws.Ledger))
	renderProcess(&b, "Broker", ws.Broker.Running, true,
		formatBrokerDetail(ws.Broker))
	renderProcess(&b, "Senate", ws.Senate.Running, false,
		formatSenateDetail(ws.Senate))
	b.WriteString("\n")

	// World processes (Forge, Sentinel, Governor — not Prefect/Chronicle).
	b.WriteString(headerStyle.Render("World Processes"))
	b.WriteString("\n")
	renderProcess(&b, "Forge", ws.Forge.Running, false,
		formatForgeDetail(ws.Forge))
	renderProcess(&b, "Sentinel", ws.Sentinel.Running, false,
		formatSentinelDetail(ws.Sentinel))
	renderProcess(&b, "Governor", ws.Governor.Running, false,
		formatGovernorDetail(ws.Governor))
	b.WriteString("\n")

	// Outposts (role=outpost only).
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

	// Token summary.
	renderTokens(&b, ws.Tokens)

	// Unified inbox count (escalations + mail).
	inboxCount := mailCount
	if len(escalations) > 0 && escalations[0] != nil {
		inboxCount += escalations[0].Total
	}
	if inboxCount > 0 {
		label := "items need attention"
		if inboxCount == 1 {
			label = "item needs attention"
		}
		b.WriteString(fmt.Sprintf("Inbox: %d %s\n", inboxCount, label))
		b.WriteString("\n")
	}

	// Summary.
	renderWorldSummary(&b, ws)

	return b.String()
}

// renderEscalationLine returns a formatted escalation summary line.
// Format: "Escalations: 3 open (1 critical, 2 high)"
func renderEscalationLine(esc *EscalationSummary) string {
	line := fmt.Sprintf("Escalations: %d open", esc.Total)

	// Build severity breakdown in decreasing severity order.
	severityOrder := []string{"critical", "high", "medium", "low"}
	var parts []string
	for _, sev := range severityOrder {
		if count, ok := esc.BySeverity[sev]; ok && count > 0 {
			part := fmt.Sprintf("%d %s", count, sev)
			switch sev {
			case "critical":
				part = errorStyle.Render(part)
			case "high", "medium":
				part = warnStyle.Render(part)
			}
			parts = append(parts, part)
		}
	}

	if len(parts) > 0 {
		line += " (" + strings.Join(parts, ", ") + ")"
	}

	return line + "\n"
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
