package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	historyWorld string
	historyJSON  bool
)

// historyRow is a display-ready row combining history + token data.
type historyRow struct {
	ID         string          `json:"id"`
	AgentName  string          `json:"agent_name"`
	WritID string          `json:"writ_id,omitempty"`
	Action     string          `json:"action"`
	StartedAt  time.Time       `json:"started_at"`
	EndedAt    *time.Time      `json:"ended_at,omitempty"`
	Duration   string          `json:"duration,omitempty"`
	Summary    string          `json:"summary,omitempty"`
	Tokens     *tokenTotals    `json:"tokens,omitempty"`
}

type tokenTotals struct {
	Input         int64 `json:"input"`
	Output        int64 `json:"output"`
	CacheRead     int64 `json:"cache_read"`
	CacheCreation int64 `json:"cache_creation"`
	Total         int64 `json:"total"`
}

var agentHistoryCmd = &cobra.Command{
	Use:   "history [name]",
	Short: "Show agent work trail",
	Long: `Show the work trail for an agent — writs, cast/resolve times, cycle duration, and token usage.

Without a name argument, shows all agent activity in the world.`,
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

		// Build display rows with token data.
		rows := make([]historyRow, 0, len(entries))
		for _, e := range entries {
			row := historyRow{
				ID:         e.ID,
				AgentName:  e.AgentName,
				WritID: e.WritID,
				Action:     e.Action,
				StartedAt:  e.StartedAt,
				EndedAt:    e.EndedAt,
				Summary:    e.Summary,
			}

			// Compute duration.
			if e.EndedAt != nil {
				d := e.EndedAt.Sub(e.StartedAt)
				row.Duration = status.FormatDuration(d)
			}

			// Fetch token usage.
			ts, err := worldStore.TokensForHistory(e.ID)
			if err == nil && ts != nil {
				row.Tokens = &tokenTotals{
					Input:         ts.InputTokens,
					Output:        ts.OutputTokens,
					CacheRead:     ts.CacheReadTokens,
					CacheCreation: ts.CacheCreationTokens,
					Total:         ts.InputTokens + ts.OutputTokens + ts.CacheReadTokens + ts.CacheCreationTokens,
				}
			}

			rows = append(rows, row)
		}

		if historyJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(rows)
		}

		renderHistory(rows, agentName, world)
		return nil
	},
}

func renderHistory(rows []historyRow, agentName, world string) {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	actionCast := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	actionResolve := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	actionOther := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))

	var b strings.Builder

	// Header.
	if agentName != "" {
		b.WriteString(headerStyle.Render(fmt.Sprintf("Agent History: %s", agentName)))
	} else {
		b.WriteString(headerStyle.Render(fmt.Sprintf("Agent History: %s", world)))
	}
	b.WriteString(fmt.Sprintf("  %s\n\n", dimStyle.Render(fmt.Sprintf("(%d entries)", len(rows)))))

	// Table header.
	hdr := fmt.Sprintf("  %-14s", "AGENT")
	hdr += fmt.Sprintf("%-10s", "ACTION")
	hdr += fmt.Sprintf("%-22s", "WRIT")
	hdr += fmt.Sprintf("%-22s", "STARTED")
	hdr += fmt.Sprintf("%-10s", "DURATION")
	hdr += fmt.Sprintf("%-12s", "TOKENS")
	b.WriteString(dimStyle.Render(hdr))
	b.WriteString("\n")

	for _, row := range rows {
		// Agent name — skip if filtering by one agent.
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
		// Pad after styled string (ANSI codes mess with %-Ns).
		b.WriteString(actionStr)
		b.WriteString(strings.Repeat(" ", maxInt(10-len(row.Action), 1)))

		// Work item.
		writ := row.WritID
		if writ == "" {
			writ = "-"
		} else if len(writ) > 20 {
			writ = writ[:20]
		}
		b.WriteString(fmt.Sprintf("%-22s", writ))

		// Started at.
		b.WriteString(fmt.Sprintf("%-22s", row.StartedAt.Format("2006-01-02 15:04:05")))

		// Duration.
		dur := "-"
		if row.Duration != "" {
			dur = row.Duration
		}
		b.WriteString(fmt.Sprintf("%-10s", dur))

		// Tokens.
		tok := "-"
		if row.Tokens != nil {
			tok = formatTokenCount(row.Tokens.Total)
		}
		b.WriteString(fmt.Sprintf("%-12s", tok))

		b.WriteString("\n")
	}

	fmt.Print(b.String())
}

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
	agentHistoryCmd.Flags().StringVar(&historyWorld, "world", "", "world name")
	agentHistoryCmd.Flags().BoolVar(&historyJSON, "json", false, "output as JSON")
}
