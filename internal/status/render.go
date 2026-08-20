package status

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/nevinsm/sol/internal/broker"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/statusformat"
	"github.com/nevinsm/sol/internal/style"
)

var (
	// Health badges.
	healthyBadge   = style.OK.Render("● healthy")
	unhealthyBadge = style.Error.Render("● unhealthy")
	degradedBadge  = style.Warn.Render("● degraded")
	sleepingBadge  = style.Dim.Render("○ sleeping")
	unknownBadge   = style.Dim.Render("● unknown")
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
		return style.OK.Render("✓")
	}
	return style.Error.Render("✗")
}

// optionalStatusIndicator returns a dim ○ for non-running optional processes
// instead of the alarming red ✗ used for required processes.
func optionalStatusIndicator(running bool) string {
	if running {
		return style.OK.Render("✓")
	}
	return style.Dim.Render("○")
}

// RenderSphere renders a SphereStatus as styled terminal output.
func RenderSphere(s *SphereStatus) string {
	var b strings.Builder

	// Header.
	b.WriteString(style.Header.Render("Sol Sphere"))
	b.WriteString("  ")
	b.WriteString(healthBadge(s.Health))
	b.WriteString("\n")
	b.WriteString(style.Dim.Render(s.SOLHome))
	b.WriteString("\n\n")

	// Sphere processes.
	b.WriteString(style.Header.Render("Processes"))
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
	renderBrokerRuntimes(&b, s.Broker.Runtimes)
	b.WriteString("\n")

	// Worlds table.
	if len(s.Worlds) == 0 {
		b.WriteString(style.Dim.Render("No worlds initialized."))
		b.WriteString("\n")
		b.WriteString(style.Dim.Render("Run: sol init --name=<world>"))
		b.WriteString("\n")
	} else {
		b.WriteString(style.Header.Render("Worlds"))
		b.WriteString("\n")
		renderWorldsTable(&b, s.Worlds)
		b.WriteString("\n")
	}

	// Token summary (aggregated across all worlds).
	renderTokens(&b, s.Tokens)

	// Caravans (if any).
	if len(s.Caravans) > 0 {
		b.WriteString(style.Header.Render("Caravans"))
		b.WriteString("\n")
		renderCaravansTable(&b, s.Caravans)
		b.WriteString("\n")
	}

	// Unified inbox count (escalations + mail) — delegates to statusformat
	// so sol status and sol dash render identical wording (including the
	// severity/mail breakdown).
	if line := statusformat.FormatInboxLine(s.MailCount, (*statusformat.EscalationSummaryDetail)(s.Escalations)); line != "" {
		b.WriteString(line)
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
		b.WriteString(style.Dim.Render("  " + detail))
	}
	b.WriteString("\n")
}

// Process detail formatters delegate to internal/statusformat so the
// dashboard and the CLI status renderer share one source of truth.

func formatPrefectDetail(p PrefectInfo) string {
	return statusformat.FormatPrefectDetail(statusformat.PrefectDetail(p))
}

func formatConsulDetail(c ConsulInfo) string {
	return statusformat.FormatConsulDetail(statusformat.ConsulDetail(c))
}

func formatBrokerDetail(b BrokerInfo) string {
	return statusformat.FormatBrokerDetail(statusformat.BrokerDetail(b))
}

// renderBrokerRuntimes writes per-runtime liveness lines below the broker process line.
func renderBrokerRuntimes(b *strings.Builder, runtimes []broker.RuntimeLiveness) {
	if len(runtimes) == 0 {
		return
	}
	for _, r := range runtimes {
		var line string
		if r.OK {
			line = style.OK.Render("ok")
		} else {
			line = style.Error.Render("unreachable")
		}
		b.WriteString(fmt.Sprintf("    %-16s  %s\n", r.Runtime, line))
	}
}

func formatChronicleDetail(c ChronicleInfo) string {
	return statusformat.FormatChronicleDetail(statusformat.ChronicleDetail(c))
}

func formatLedgerDetail(l LedgerInfo) string {
	return statusformat.FormatLedgerDetail(statusformat.LedgerDetail(l))
}

func renderWorldsTable(b *strings.Builder, worlds []WorldSummary) {
	// Use tabwriter for alignment.
	tw := tabwriter.NewWriter(b, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  WORLD\tAGENTS\tENVOYS\tFORGE\tSENTINEL\tMR QUEUE\tHEALTH\n")

	for _, w := range worlds {
		if w.Sleeping {
			// Show active agent/envoy counts for sleeping worlds (soft sleep wind-down).
			agents := style.Dim.Render("—")
			if w.Agents > 0 {
				agents = fmt.Sprintf("%d", w.Agents)
			}
			envoys := style.Dim.Render("—")
			if w.Envoys > 0 {
				envoys = fmt.Sprintf("%d", w.Envoys)
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				w.Name,
				agents, envoys,
				style.Dim.Render("—"), style.Dim.Render("—"), style.Dim.Render("—"),
				sleepingBadge)
			continue
		}

		agentCount := fmt.Sprintf("%d", w.Agents)
		if w.MaxActive > 0 {
			agentCount = fmt.Sprintf("%d/%d", w.Agents, w.MaxActive)
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

		forge := style.Dim.Render("—")
		if w.Forge {
			forge = style.OK.Render("✓")
		}

		sentinel := style.Dim.Render("—")
		if w.Sentinel {
			sentinel = style.OK.Render("✓")
		}

		mrQueue := style.Dim.Render("—")
		if w.MRReady > 0 || w.MRFailed > 0 {
			mrQueue = fmt.Sprintf("%d ready", w.MRReady)
			if w.MRFailed > 0 {
				mrQueue += style.Error.Render(fmt.Sprintf(", %d failed", w.MRFailed))
			}
		}

		health := healthBadge(w.Health)

		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			w.Name, agents, envoys, forge, sentinel, mrQueue, health)
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
			// Blocked = items not accounted for by any per-phase bucket.
			// Mirrors the non-phase branch so operators see the same residual
			// in either display mode (ORCH-L2).
			blocked := c.TotalItems - c.ClosedItems - c.DoneItems - c.ReadyItems - c.DispatchedItems
			progress := fmt.Sprintf("%d items  %s", c.TotalItems, strings.Join(parts, ", "))
			if blocked > 0 {
				progress += fmt.Sprintf(", %d blocked", blocked)
			}
			b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
				c.ID, c.Name, style.Dim.Render(progress)))
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
				c.ID, c.Name, style.Dim.Render(progress)))
		}
	}
}

// RenderWorld renders a WorldStatus as styled terminal output.
func RenderWorld(ws *WorldStatus) string {
	var b strings.Builder

	// Header.
	b.WriteString(style.Header.Render(fmt.Sprintf("World: %s", ws.World)))
	b.WriteString("  ")
	b.WriteString(healthBadge(ws.HealthString()))
	b.WriteString("\n\n")

	// Processes.
	b.WriteString(style.Header.Render("Processes"))
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
	renderBrokerRuntimes(&b, ws.Broker.Runtimes)
	b.WriteString("\n")

	// Outposts (role=outpost only).
	if len(ws.Agents) > 0 {
		b.WriteString(style.Header.Render(fmt.Sprintf("Outposts (%d)", len(ws.Agents))))
		b.WriteString("\n")
		renderAgentsTable(&b, ws.Agents)
		b.WriteString("\n")
	}

	// Envoys.
	if len(ws.Envoys) > 0 {
		b.WriteString(style.Header.Render(fmt.Sprintf("Envoys (%d)", len(ws.Envoys))))
		b.WriteString("\n")
		renderEnvoysTable(&b, ws.Envoys)
		b.WriteString("\n")
	}

	// Show "no agents" if neither outposts nor envoys exist.
	if len(ws.Agents) == 0 && len(ws.Envoys) == 0 {
		b.WriteString(style.Dim.Render("No agents registered."))
		b.WriteString("\n")
	}

	// Caravans.
	if len(ws.Caravans) > 0 {
		b.WriteString(style.Header.Render("Caravans"))
		b.WriteString("\n")
		renderCaravansTable(&b, ws.Caravans)
		b.WriteString("\n")
	}

	// Merge queue.
	b.WriteString(style.Header.Render("Merge Queue"))
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
	return statusformat.FormatForgeDetail(statusformat.ForgeDetail(f))
}

func formatSentinelDetail(s SentinelInfo) string {
	return statusformat.FormatSentinelDetail(statusformat.SentinelDetail(s))
}

// stateStyle maps an agent/envoy state to its styled display string.
// This is the single source of truth for state→color mapping:
//   working (session alive) → green, working (session dead) → red,
//   idle → gray, stalled → yellow.
func stateStyle(state string, sessionAlive bool) string {
	switch state {
	case "working":
		if sessionAlive {
			return style.OK.Render("working")
		}
		return style.Error.Render("working (dead!)")
	case "idle":
		return style.Dim.Render("idle")
	case "stalled":
		return style.Warn.Render("stalled")
	default:
		return state
	}
}

// sessionDisplay renders session liveness for an agent or envoy. Semantics
// live in statusformat.SessionLabel so status and dash never drift — see
// that function's doc for the envoy-only idle-session distinction.
func sessionDisplay(state string, sessionAlive bool, isEnvoy bool) string {
	label, severity := statusformat.SessionLabel(state, sessionAlive, isEnvoy)
	switch severity {
	case statusformat.SessionOK:
		return style.OK.Render(label)
	case statusformat.SessionError:
		return style.Error.Render(label)
	default:
		return style.Dim.Render(label)
	}
}

// nudgeDisplay renders a nudge count, or a dim dash if zero.
func nudgeDisplay(count int) string {
	if count > 0 {
		return fmt.Sprintf("%d", count)
	}
	return style.Dim.Render("—")
}

func renderAgentsTable(b *strings.Builder, agents []AgentStatus) {
	tw := tabwriter.NewWriter(b, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  NAME\tSTATE\tSESSION\tWORK\tNUDGE\n")

	for _, a := range agents {
		work := style.Dim.Render("—")
		if a.ActiveWrit != "" {
			work = fmt.Sprintf("%s: %s", a.ActiveWrit, a.WorkTitle)
		}

		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n",
			a.Name,
			stateStyle(a.State, a.SessionAlive),
			sessionDisplay(a.State, a.SessionAlive, false),
			work,
			nudgeDisplay(a.NudgeCount))
	}
	tw.Flush()
}

func renderEnvoysTable(b *strings.Builder, envoys []EnvoyStatus) {
	tw := tabwriter.NewWriter(b, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  NAME\tSTATE\tSESSION\tWORK\tNUDGE\n")

	for _, e := range envoys {
		work := style.Dim.Render("—")
		if e.ActiveWrit != "" {
			work = e.WorkTitle
			// Show background tether count for multi-tether envoys.
			bgCount := e.TetheredCount - 1 // exclude active writ
			if bgCount > 0 {
				work += style.Dim.Render(fmt.Sprintf(" [+%d tethered]", bgCount))
			}
		}

		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n",
			e.Name,
			stateStyle(e.State, e.SessionAlive),
			sessionDisplay(e.State, e.SessionAlive, true),
			work,
			nudgeDisplay(e.NudgeCount))
	}
	tw.Flush()
}

// formatCompactTokens delegates to statusformat.FormatCompactTokens.
func formatCompactTokens(n int64) string {
	return statusformat.FormatCompactTokens(n)
}

func renderTokens(b *strings.Builder, t TokenInfo) {
	if t.InputTokens == 0 && t.OutputTokens == 0 && t.CacheTokens == 0 {
		return
	}

	b.WriteString(style.Header.Render("Tokens (24h)"))
	b.WriteString("\n")

	line := fmt.Sprintf("  %s in / %s out",
		formatCompactTokens(t.InputTokens),
		formatCompactTokens(t.OutputTokens))

	if t.CostUSD > 0 {
		line += fmt.Sprintf(", %s", formatCost(t.CostUSD))
	}

	if t.AgentCount > 0 {
		line += fmt.Sprintf("  %s  %d agents", style.Dim.Render("•"), t.AgentCount)
	}

	b.WriteString(line)
	b.WriteString("\n")

	// Per-runtime breakdown (only shown when multiple runtimes present).
	if len(t.RuntimeBreakdown) > 0 {
		for _, rt := range t.RuntimeBreakdown {
			rtLine := fmt.Sprintf("    %s: %s in / %s out",
				rt.Runtime,
				formatCompactTokens(rt.InputTokens),
				formatCompactTokens(rt.OutputTokens))
			if rt.CostUSD > 0 {
				rtLine += fmt.Sprintf(", %s", formatCost(rt.CostUSD))
			}
			b.WriteString(style.Dim.Render(rtLine))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
}

// formatCost delegates to statusformat.FormatCost.
func formatCost(cost float64) string {
	return statusformat.FormatCost(cost)
}

func renderMergeQueue(b *strings.Builder, mq MergeQueueInfo) {
	if mq.Total == 0 {
		b.WriteString(style.Dim.Render("  empty"))
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
		parts = append(parts, style.Error.Render(fmt.Sprintf("%d failed", mq.Failed)))
	}
	if mq.Merged > 0 {
		parts = append(parts, style.OK.Render(fmt.Sprintf("%d merged", mq.Merged)))
	}
	b.WriteString(fmt.Sprintf("  %s\n", strings.Join(parts, ", ")))
}

func renderWorldSummary(b *strings.Builder, ws *WorldStatus) {
	parts := fmt.Sprintf("%d agents", ws.Summary.Total)
	if ws.MaxActive > 0 {
		parts += fmt.Sprintf(" (max_active: %d)", ws.MaxActive)
	}
	if len(ws.Envoys) > 0 {
		parts += fmt.Sprintf(", %d envoys", len(ws.Envoys))
	}
	parts += fmt.Sprintf(" | %d working, %d idle", ws.Summary.Working, ws.Summary.Idle)
	if ws.Summary.Stalled > 0 {
		parts += style.Warn.Render(fmt.Sprintf(", %d stalled", ws.Summary.Stalled))
	}
	if ws.Summary.Dead > 0 {
		parts += style.Error.Render(fmt.Sprintf(", %d dead", ws.Summary.Dead))
	}
	b.WriteString(style.Dim.Render(parts))
	b.WriteString("\n")
}

// RenderCombined renders sphere processes and world detail as a single view.
// Used when sol status auto-detects a world from the current directory.
// mailCount and escalations are used to compute a unified inbox count.
func RenderCombined(consul ConsulInfo, ws *WorldStatus, mailCount int, escalations ...*EscalationSummary) string {
	var b strings.Builder

	// Header — world-focused.
	b.WriteString(style.Header.Render(fmt.Sprintf("World: %s", ws.World)))
	b.WriteString("  ")
	b.WriteString(healthBadge(ws.HealthString()))
	b.WriteString("\n")
	b.WriteString(style.Dim.Render(config.WorldDir(ws.World)))
	b.WriteString("\n\n")

	// Sphere-level processes.
	b.WriteString(style.Header.Render("Sphere Processes"))
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
	renderBrokerRuntimes(&b, ws.Broker.Runtimes)
	b.WriteString("\n")

	// World processes (Forge, Sentinel — not Prefect/Chronicle).
	b.WriteString(style.Header.Render("World Processes"))
	b.WriteString("\n")
	renderProcess(&b, "Forge", ws.Forge.Running, false,
		formatForgeDetail(ws.Forge))
	renderProcess(&b, "Sentinel", ws.Sentinel.Running, false,
		formatSentinelDetail(ws.Sentinel))
	b.WriteString("\n")

	// Outposts (role=outpost only).
	if len(ws.Agents) > 0 {
		b.WriteString(style.Header.Render(fmt.Sprintf("Outposts (%d)", len(ws.Agents))))
		b.WriteString("\n")
		renderAgentsTable(&b, ws.Agents)
		b.WriteString("\n")
	}

	// Envoys.
	if len(ws.Envoys) > 0 {
		b.WriteString(style.Header.Render(fmt.Sprintf("Envoys (%d)", len(ws.Envoys))))
		b.WriteString("\n")
		renderEnvoysTable(&b, ws.Envoys)
		b.WriteString("\n")
	}

	// Show "no agents" if neither outposts nor envoys exist.
	if len(ws.Agents) == 0 && len(ws.Envoys) == 0 {
		b.WriteString(style.Dim.Render("No agents registered."))
		b.WriteString("\n")
	}

	// Caravans.
	if len(ws.Caravans) > 0 {
		b.WriteString(style.Header.Render("Caravans"))
		b.WriteString("\n")
		renderCaravansTable(&b, ws.Caravans)
		b.WriteString("\n")
	}

	// Merge queue.
	b.WriteString(style.Header.Render("Merge Queue"))
	b.WriteString("\n")
	renderMergeQueue(&b, ws.MergeQueue)
	b.WriteString("\n")

	// Token summary.
	renderTokens(&b, ws.Tokens)

	// Unified inbox count (escalations + mail) — delegates to statusformat
	// so sol status and sol dash render identical wording.
	var escSummary *EscalationSummary
	if len(escalations) > 0 {
		escSummary = escalations[0]
	}
	if line := statusformat.FormatInboxLine(mailCount, (*statusformat.EscalationSummaryDetail)(escSummary)); line != "" {
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Summary.
	renderWorldSummary(&b, ws)

	return b.String()
}

// RenderWorldConfig renders the config section for sol world status.
func RenderWorldConfig(world string, cfg config.WorldConfig) string {
	var b strings.Builder

	b.WriteString(style.Header.Render("Config"))
	b.WriteString("\n")

	sourceDisplay := cfg.World.SourceRepo
	if sourceDisplay == "" {
		sourceDisplay = style.Dim.Render("(none)")
	}
	b.WriteString(fmt.Sprintf("  Source repo:    %s\n", sourceDisplay))

	if cfg.Agents.MaxActive == 0 {
		b.WriteString(fmt.Sprintf("  Max active:     %s\n", style.Dim.Render("unlimited")))
	} else {
		b.WriteString(fmt.Sprintf("  Max active:     %d\n", cfg.Agents.MaxActive))
	}
	if cfg.Agents.Model != "" {
		b.WriteString(fmt.Sprintf("  Model:          %s\n", cfg.Agents.Model))
	}

	// Show per-runtime, per-role model overrides if any are configured.
	for rt, rm := range cfg.Agents.Models {
		if rm.Outpost != "" || rm.Envoy != "" || rm.Forge != "" {
			b.WriteString(fmt.Sprintf("  Model overrides [%s]:\n", rt))
			if rm.Outpost != "" {
				b.WriteString(fmt.Sprintf("    outpost:      %s\n", rm.Outpost))
			}
			if rm.Envoy != "" {
				b.WriteString(fmt.Sprintf("    envoy:        %s\n", rm.Envoy))
			}
			if rm.Forge != "" {
				b.WriteString(fmt.Sprintf("    forge:        %s\n", rm.Forge))
			}
		}
	}

	if cfg.Agents.ChannelsEnabled {
		b.WriteString(fmt.Sprintf("  Channels:       %s (ADR-0044 — requires managed-settings.json, see sol doctor)\n", style.OK.Render("enabled")))
	}

	b.WriteString(fmt.Sprintf("  Quality gates:  %d\n", len(cfg.Forge.QualityGates)))

	namePool := style.Dim.Render("(default)")
	if cfg.Agents.NamePoolPath != "" {
		namePool = cfg.Agents.NamePoolPath
	}
	b.WriteString(fmt.Sprintf("  Name pool:      %s\n", namePool))
	b.WriteString("\n")

	return b.String()
}
