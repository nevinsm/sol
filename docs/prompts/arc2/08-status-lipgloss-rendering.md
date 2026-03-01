# Prompt 08: Arc 2 — Status Lipgloss Rendering

You are building the styled terminal rendering for `sol status` output
using charmbracelet/lipgloss. This prompt creates `render.go` with
functions for both sphere and world rendering.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 2 prompt 07 is complete (sphere status gathering,
`SphereStatus`, `GatherSphere()`).

Read the existing code first. Understand:
- `internal/status/status.go` — `WorldStatus`, `SphereStatus`,
  `WorldSummary`, all type definitions
- `cmd/status.go` — `printWorldStatus()` — the current plain text
  renderer (to be replaced)
- `go.mod` — verify `charmbracelet/lipgloss` is a direct dependency

Familiarize yourself with charmbracelet/lipgloss:
- lipgloss provides styled terminal rendering via style definitions
- Key concepts: `lipgloss.NewStyle()`, `.Foreground()`, `.Bold()`,
  `.Render()`, `.Width()`, `.Padding()`
- Styles are composable and chainable
- lipgloss automatically degrades in non-color terminals

---

## Task 1: Style Definitions

**Create** `internal/status/render.go`.

Define a set of styles used across all rendering:

```go
package status

import "github.com/charmbracelet/lipgloss"

var (
    // Section headers.
    headerStyle = lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color("12")) // bright blue

    // Status indicators.
    okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
    warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
    errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red
    dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // gray

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
```

---

## Task 2: RenderSphere

Add the sphere-level renderer:

```go
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
```

---

## Task 3: RenderWorld

Add the per-world renderer (replaces `printWorldStatus` in cmd/status.go):

```go
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
```

---

## Task 4: Tests

**Create** `internal/status/render_test.go`.

Rendering tests verify structure, not exact styling (ANSI codes make
exact string matching fragile). Use `strings.Contains` checks:

```go
func TestRenderSphereBasic(t *testing.T)
    // Build a SphereStatus with 2 worlds, prefect running.
    // Render → verify output contains "Sol Sphere", both world names,
    // "Prefect", "Consul", "Chronicle".

func TestRenderSphereNoWorlds(t *testing.T)
    // Empty SphereStatus.
    // Render → verify output contains "No worlds initialized."

func TestRenderSphereCaravans(t *testing.T)
    // SphereStatus with caravans.
    // Render → verify output contains "Caravans" header and caravan IDs.

func TestRenderWorldBasic(t *testing.T)
    // Build a WorldStatus with agents, forge running, merge queue.
    // Render → verify output contains world name, "Processes",
    // "Agents", "Merge Queue" sections.

func TestRenderWorldNoAgents(t *testing.T)
    // WorldStatus with no agents.
    // Render → verify output contains "No agents registered."

func TestRenderWorldAgentStates(t *testing.T)
    // WorldStatus with agents in different states.
    // Render → verify output contains agent names and state text.

func TestHealthBadge(t *testing.T)
    // Verify healthBadge returns non-empty strings for each health level.
    // "healthy" → contains "healthy"
    // "unhealthy" → contains "unhealthy"
    // "degraded" → contains "degraded"

func TestStatusIndicator(t *testing.T)
    // statusIndicator(true) → contains "✓"
    // statusIndicator(false) → contains "✗"
```

---

## Task 5: Verify

1. `make build` — compiles cleanly
2. `make test` — all existing and new tests pass
3. Verify rendering manually:
   ```bash
   go test ./internal/status/ -v -run Render
   ```

---

## Guidelines

- **`--json` bypasses all rendering.** The render functions are only
  called in human-readable output paths. JSON output uses
  `encoding/json` directly — no lipgloss involvement.
- Rendering is **separated from data gathering.** `Gather()` and
  `GatherSphere()` produce data structs. `RenderWorld()` and
  `RenderSphere()` produce styled strings. This separation makes
  both testable independently.
- Use **ANSI color numbers** (0-15), not named colors or hex codes.
  This ensures broad terminal compatibility.
- **Don't test exact ANSI sequences.** Lipgloss output depends on
  terminal capabilities. Test for content presence, not styling.
- The worlds table uses `text/tabwriter` for alignment (inside the
  lipgloss-styled output). This provides clean columns without
  requiring a full table component.
- `dimStyle` (gray) is used for inactive/empty state to reduce visual
  noise. Active/error states use bright colors for attention.
- All existing tests must continue to pass.
- Commit after tests pass with message:
  `feat(status): add lipgloss-styled rendering for sphere and world status`
