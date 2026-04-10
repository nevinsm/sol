package cmd

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/nevinsm/sol/internal/cliapi/agents"
	"github.com/nevinsm/sol/internal/cliformat"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	historyWorld string
	historyJSON  bool
)

// Outcome values rendered by the history view.
//
// The agent_history table has no explicit outcome column, so these are
// inferred from the row's ended_at and (when applicable) the linked writ's
// terminal status. See inferOutcome below.
const (
	historyOutcomeRunning = "running"
	historyOutcomeDone    = "done"
	historyOutcomeUnknown = "unknown"
)

// historyRow is an alias for the CLI API HistoryEntry type, used for
// both --json output and table rendering.
type historyRow = agents.HistoryEntry

// writStatusLookup is the narrow slice of the world store needed to infer
// history outcomes. Defined as an interface so tests can pin outcomes without
// spinning up a full SQLite database.
type writStatusLookup interface {
	GetWrit(id string) (*store.Writ, error)
}

// inferOutcome derives an OUTCOME string for a history entry from existing
// fields. The store layer has no explicit outcome column (W1.6 scope forbids
// changing the schema), so inference uses:
//
//   - ended_at IS NULL      -> "running"  (cycle still active)
//   - no linked writ        -> "done"     (e.g. bare session rows)
//   - linked writ terminal  -> "done"     (resolved / closed cleanly)
//   - writ lookup error     -> "unknown"  (best-effort — don't mask with "done")
//   - writ not terminal     -> "unknown"  (handoff, escalation, crash)
//
// Escalation is tracked in the sphere database, which this command does not
// open. Rows whose writ was escalated back to open will therefore show
// "unknown" rather than "escalated" — an acceptable simplification for a
// per-world view.
func inferOutcome(entry store.HistoryEntry, lookup writStatusLookup) string {
	if entry.EndedAt == nil {
		return historyOutcomeRunning
	}
	if entry.WritID == "" {
		return historyOutcomeDone
	}
	if lookup == nil {
		return historyOutcomeUnknown
	}
	w, err := lookup.GetWrit(entry.WritID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Writ record is gone but the cycle did end — treat as done.
			return historyOutcomeDone
		}
		return historyOutcomeUnknown
	}
	if store.IsTerminalStatus(w.Status) {
		return historyOutcomeDone
	}
	return historyOutcomeUnknown
}

var agentHistoryCmd = &cobra.Command{
	Use:   "history [name]",
	Short: "Show agent work trail",
	Long: `Show the work trail for an agent — writs, cast/resolve times,
cycle duration, and outcome.

Without a name argument, shows all agent activity in the world.

World resolution (see ADR-0039):
  1. --world flag
  2. SOL_WORLD environment variable
  3. Current directory's managed world (auto-detected from git worktree)

If none can be determined the command errors.

The OUTCOME column is inferred from the history row:
  running   — cycle is still active (no ended_at)
  done      — cycle ended and the linked writ is in a terminal state,
              or the row has no linked writ
  unknown   — cycle ended but the writ is not terminal (handoff,
              escalation, or crash); no linked writ could be looked up

For per-writ token / cost details, use 'sol cost --writ=<id>' which is the
canonical source of token accounting.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(historyWorld)
		if err != nil {
			return err
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		var agentName string
		if len(args) > 0 {
			agentName = args[0]
		}

		entries, err := worldStore.ListHistory(agentName)
		if err != nil {
			return err
		}

		if len(entries) == 0 {
			if agentName != "" {
				fmt.Printf("No history found for agent %q in world %q.\n", agentName, world)
			} else {
				fmt.Printf("No agent history found in world %q.\n", world)
			}
			return nil
		}

		// Build display rows.
		rows := make([]historyRow, 0, len(entries))
		for _, e := range entries {
			var duration string
			if e.EndedAt != nil {
				duration = status.FormatDuration(e.EndedAt.Sub(e.StartedAt))
			}

			row := agents.FromStoreHistoryEntry(e, duration, inferOutcome(e, worldStore))
			rows = append(rows, row)
		}

		if historyJSON {
			return printJSON(rows)
		}

		renderHistory(rows, agentName, world, time.Now())
		return nil
	},
}

func renderHistory(rows []historyRow, agentName, world string, now time.Time) {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	actionCast := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	actionResolve := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	actionOther := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))

	outcomeRunningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	outcomeDoneStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	outcomeUnknownStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	var b strings.Builder

	// Header.
	if agentName != "" {
		b.WriteString(headerStyle.Render(fmt.Sprintf("Agent History: %s", agentName)))
	} else {
		b.WriteString(headerStyle.Render(fmt.Sprintf("Agent History: %s", world)))
	}
	b.WriteString(fmt.Sprintf("  %s\n\n", dimStyle.Render(cliformat.FormatCount(len(rows), "entry", "entries"))))

	// Table header.
	hdr := fmt.Sprintf("  %-14s", "AGENT")
	hdr += fmt.Sprintf("%-10s", "ACTION")
	hdr += fmt.Sprintf("%-22s", "WRIT")
	hdr += fmt.Sprintf("%-22s", "STARTED")
	hdr += fmt.Sprintf("%-10s", "DURATION")
	hdr += fmt.Sprintf("%-10s", "OUTCOME")
	b.WriteString(dimStyle.Render(hdr))
	b.WriteString("\n")

	for _, row := range rows {
		// Agent name — capped to fit column.
		agent := row.AgentName
		if len(agent) > 12 {
			agent = agent[:12]
		}
		b.WriteString(fmt.Sprintf("  %-14s", agent))

		// Action — color-coded.
		actionStr := row.Action
		switch row.Action {
		case "cast":
			actionStr = actionCast.Render(actionStr)
		case "resolve":
			actionStr = actionResolve.Render(actionStr)
		default:
			actionStr = actionOther.Render(actionStr)
		}
		// Pad after styled string (ANSI codes break %-Ns).
		b.WriteString(actionStr)
		b.WriteString(strings.Repeat(" ", maxInt(10-len(row.Action), 1)))

		// Writ.
		writ := row.WritID
		if writ == "" {
			writ = cliformat.EmptyMarker
		} else if len(writ) > 20 {
			writ = writ[:20]
		}
		b.WriteString(fmt.Sprintf("%-22s", writ))

		// Started at — canonical relative-or-RFC3339 format.
		b.WriteString(fmt.Sprintf("%-22s", cliformat.FormatTimestampOrRelative(row.StartedAt, now)))

		// Duration — "running" when the cycle has no ended_at.
		var dur string
		switch {
		case row.EndedAt == nil:
			dur = "running"
		case row.Duration != "":
			dur = row.Duration
		default:
			dur = cliformat.EmptyMarker
		}
		b.WriteString(fmt.Sprintf("%-10s", dur))

		// Outcome — color-coded, padded after the styled string.
		var outcomeStr string
		switch row.Outcome {
		case historyOutcomeRunning:
			outcomeStr = outcomeRunningStyle.Render(row.Outcome)
		case historyOutcomeDone:
			outcomeStr = outcomeDoneStyle.Render(row.Outcome)
		default:
			outcomeStr = outcomeUnknownStyle.Render(row.Outcome)
		}
		b.WriteString(outcomeStr)
		b.WriteString(strings.Repeat(" ", maxInt(10-len(row.Outcome), 1)))

		b.WriteString("\n")
	}

	fmt.Print(b.String())
}

// formatTokenCount formats a token count with SI suffix for compact display.
// Retained in this file (despite the TOKENS column being dropped from
// history output in W1.6) because agent_stats.go still imports it.
func formatTokenCount(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func init() {
	agentCmd.AddCommand(agentHistoryCmd)
	agentHistoryCmd.Flags().StringVar(&historyWorld, "world", "", "world name (auto-detected from $SOL_WORLD or cwd if unset; see ADR-0039)")
	agentHistoryCmd.Flags().BoolVar(&historyJSON, "json", false, "output as JSON")
}
