package cmd

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	agentStatsWorld string
	agentStatsJSON  bool
)

// AgentStatsReport holds computed performance metrics for an agent.
type AgentStatsReport struct {
	Name             string              `json:"name"`
	TotalCasts       int                 `json:"total_casts"`
	CompletedCasts   int                 `json:"completed_casts"`
	CycleTimeMedianS *float64           `json:"cycle_time_median_s,omitempty"`
	CycleTimeP90S    *float64           `json:"cycle_time_p90_s,omitempty"`
	FirstPassRate    *float64           `json:"first_pass_rate,omitempty"`
	FirstPassMRs     int                `json:"first_pass_mrs"`
	MergedMRs        int                `json:"merged_mrs"`
	FailedMRs        int                `json:"failed_mrs"`
	ReworkCount      int                `json:"rework_count"`
	Tokens           []store.TokenSummary `json:"tokens"`
	TotalTokens      int64              `json:"total_tokens"`
	EstimatedCost    *float64           `json:"estimated_cost,omitempty"`
	UnpricedModels   int                `json:"unpriced_models,omitempty"`
	PricedModels     int                `json:"priced_models,omitempty"`
	PricingAvailable bool               `json:"pricing_available"`
}

var agentStatsCmd = &cobra.Command{
	Use:          "stats [name]",
	Short:        "Show agent performance metrics",
	Long:         "Shows performance summary for a single agent, or a leaderboard across all agents when no name is given.",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(agentStatsWorld)
		if err != nil {
			return err
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		pricing, pricingErr := config.LoadPricing()
		if pricingErr != nil {
			// Non-fatal: proceed without pricing.
			pricing = config.PricingConfig{}
		}

		if len(args) == 1 {
			// Single agent mode.
			name := args[0]
			report, err := computeAgentStats(worldStore, name, pricing)
			if err != nil {
				return err
			}

			if agentStatsJSON {
				return printJSON(report)
			}

			renderAgentStats(report)
			return nil
		}

		// Leaderboard mode.
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		agents, err := sphereStore.ListAgents(world, "")
		if err != nil {
			return err
		}

		var reports []AgentStatsReport
		for _, agent := range agents {
			report, err := computeAgentStats(worldStore, agent.Name, pricing)
			if err != nil {
				return err
			}
			if report.TotalCasts > 0 {
				reports = append(reports, *report)
			}
		}

		if agentStatsJSON {
			return printJSON(reports)
		}

		if len(reports) == 0 {
			fmt.Println("No agent stats available.")
			return nil
		}

		renderLeaderboard(world, reports)
		return nil
	},
}

func computeAgentStats(worldStore *store.WorldStore, agentName string, pricing config.PricingConfig) (*AgentStatsReport, error) {
	report := &AgentStatsReport{Name: agentName}

	// 1. Get history entries for cycle time and rework.
	history, err := worldStore.ListHistory(agentName)
	if err != nil {
		return nil, err
	}

	var cycleTimes []time.Duration
	writCasts := make(map[string]int)

	for _, h := range history {
		if h.Action != "cast" {
			continue
		}
		report.TotalCasts++
		if h.WritID != "" {
			writCasts[h.WritID]++
		}
		if h.EndedAt != nil {
			report.CompletedCasts++
			cycleTimes = append(cycleTimes, h.EndedAt.Sub(h.StartedAt))
		}
	}

	// Rework: writs that required more than one cast.
	for _, count := range writCasts {
		if count > 1 {
			report.ReworkCount += count - 1
		}
	}

	// Cycle time percentiles.
	if len(cycleTimes) > 0 {
		sort.Slice(cycleTimes, func(i, j int) bool { return cycleTimes[i] < cycleTimes[j] })
		median := durationPercentile(cycleTimes, 50)
		p90 := durationPercentile(cycleTimes, 90)
		medianS := median.Seconds()
		p90S := p90.Seconds()
		report.CycleTimeMedianS = &medianS
		report.CycleTimeP90S = &p90S
	}

	// 2. Merge stats.
	mergeStats, err := worldStore.MergeStatsForAgent(agentName)
	if err != nil {
		return nil, err
	}
	report.MergedMRs = mergeStats.MergedMRs
	report.FailedMRs = mergeStats.FailedMRs
	report.FirstPassMRs = mergeStats.FirstPassMRs
	if mergeStats.MergedMRs > 0 {
		rate := float64(mergeStats.FirstPassMRs) / float64(mergeStats.MergedMRs) * 100
		report.FirstPassRate = &rate
	}

	// 3. Token totals.
	tokens, err := worldStore.AggregateTokens(agentName)
	if err != nil {
		return nil, err
	}
	report.Tokens = tokens
	for _, t := range tokens {
		report.TotalTokens += t.InputTokens + t.OutputTokens + t.CacheReadTokens + t.CacheCreationTokens
	}

	// 4. Cost estimation.
	report.PricingAvailable = len(pricing) > 0
	if report.PricingAvailable && len(tokens) > 0 {
		// Convert store.TokenSummary to config.TokenSummary for ComputeCost.
		configSummaries := make([]config.TokenSummary, len(tokens))
		for i, t := range tokens {
			configSummaries[i] = config.TokenSummary{
				Model:               t.Model,
				InputTokens:         t.InputTokens,
				OutputTokens:        t.OutputTokens,
				CacheReadTokens:     t.CacheReadTokens,
				CacheCreationTokens: t.CacheCreationTokens,
			}
		}
		cost, unpriced := pricing.ComputeCost(configSummaries)
		report.EstimatedCost = &cost
		report.UnpricedModels = len(unpriced)
		report.PricedModels = len(tokens) - len(unpriced)
	}

	return report, nil
}

// durationPercentile returns the p-th percentile from a sorted slice of durations.
func durationPercentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := p / 100 * float64(len(sorted)-1)
	lower := int(math.Floor(idx))
	upper := int(math.Ceil(idx))
	if lower == upper {
		return sorted[lower]
	}
	frac := idx - float64(lower)
	return time.Duration(float64(sorted[lower])*(1-frac) + float64(sorted[upper])*frac)
}

func renderAgentStats(r *AgentStatsReport) {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))

	var b strings.Builder

	b.WriteString(headerStyle.Render(fmt.Sprintf("Agent Stats: %s", r.Name)))
	b.WriteString("\n\n")

	// Casts.
	b.WriteString(fmt.Sprintf("  %-18s %d total, %d completed\n", "Casts:", r.TotalCasts, r.CompletedCasts))

	// Cycle time.
	if r.CycleTimeMedianS != nil {
		medianDur := time.Duration(*r.CycleTimeMedianS * float64(time.Second))
		p90Dur := time.Duration(*r.CycleTimeP90S * float64(time.Second))
		b.WriteString(fmt.Sprintf("  %-18s median %s, p90 %s\n", "Cycle Time:",
			status.FormatDuration(medianDur), status.FormatDuration(p90Dur)))
	} else {
		b.WriteString(fmt.Sprintf("  %-18s %s\n", "Cycle Time:", dimStyle.Render("-")))
	}

	// Merge rate.
	if r.FirstPassRate != nil {
		rateStr := fmt.Sprintf("%d/%d first-pass (%.0f%%)", r.FirstPassMRs, r.MergedMRs, *r.FirstPassRate)
		if *r.FirstPassRate >= 80 {
			rateStr = okStyle.Render(rateStr)
		}
		b.WriteString(fmt.Sprintf("  %-18s %s\n", "Merge Rate:", rateStr))
	} else {
		b.WriteString(fmt.Sprintf("  %-18s %s\n", "Merge Rate:", dimStyle.Render("-")))
	}
	if r.FailedMRs > 0 {
		b.WriteString(fmt.Sprintf("  %-18s %d\n", "Failed MRs:", r.FailedMRs))
	}

	// Rework.
	b.WriteString(fmt.Sprintf("  %-18s %d\n", "Rework:", r.ReworkCount))

	b.WriteString("\n")

	// Token usage table.
	b.WriteString(headerStyle.Render("Token Usage"))
	b.WriteString("\n")

	if len(r.Tokens) == 0 {
		b.WriteString(dimStyle.Render("  (no token data)"))
		b.WriteString("\n")
	} else {
		tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "  MODEL\tINPUT\tOUTPUT\tCACHE READ\tCACHE WRITE\n")
		var totalInput, totalOutput, totalCacheRead, totalCacheWrite int64
		for _, t := range r.Tokens {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n",
				t.Model,
				formatTokenInt(t.InputTokens),
				formatTokenInt(t.OutputTokens),
				formatTokenInt(t.CacheReadTokens),
				formatTokenInt(t.CacheCreationTokens))
			totalInput += t.InputTokens
			totalOutput += t.OutputTokens
			totalCacheRead += t.CacheReadTokens
			totalCacheWrite += t.CacheCreationTokens
		}
		if len(r.Tokens) > 1 {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n",
				dimStyle.Render("Total"),
				formatTokenInt(totalInput),
				formatTokenInt(totalOutput),
				formatTokenInt(totalCacheRead),
				formatTokenInt(totalCacheWrite))
		}
		tw.Flush()
	}

	// Cost estimation line.
	b.WriteString("\n")
	if !r.PricingAvailable {
		b.WriteString(fmt.Sprintf("  %-18s %s\n", "Estimated cost:", dimStyle.Render("(no pricing configured)")))
	} else if r.EstimatedCost != nil {
		costStr := fmt.Sprintf("$%.2f", *r.EstimatedCost)
		if r.UnpricedModels > 0 {
			costStr += fmt.Sprintf(" (%d unpriced model", r.UnpricedModels)
			if r.UnpricedModels > 1 {
				costStr += "s"
			}
			costStr += ")"
		} else {
			costStr += fmt.Sprintf(" (%d model", r.PricedModels)
			if r.PricedModels > 1 {
				costStr += "s"
			}
			costStr += ", pricing from sol.toml)"
		}
		b.WriteString(fmt.Sprintf("  %-18s %s\n", "Estimated cost:", costStr))
	}

	fmt.Print(b.String())
}

func renderLeaderboard(world string, reports []AgentStatsReport) {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))

	// Sort by total casts descending.
	sort.Slice(reports, func(i, j int) bool {
		return reports[i].TotalCasts > reports[j].TotalCasts
	})

	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("Agent Leaderboard (%s)", world)))
	b.WriteString("\n\n")

	tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  NAME\tCASTS\tMEDIAN\tP90\t1ST-PASS\tREWORK\tTOKENS\n")

	for _, r := range reports {
		median := dimStyle.Render("-")
		p90 := dimStyle.Render("-")
		if r.CycleTimeMedianS != nil {
			median = status.FormatDuration(time.Duration(*r.CycleTimeMedianS * float64(time.Second)))
			p90 = status.FormatDuration(time.Duration(*r.CycleTimeP90S * float64(time.Second)))
		}

		firstPass := dimStyle.Render("-")
		if r.FirstPassRate != nil {
			fp := fmt.Sprintf("%.0f%%", *r.FirstPassRate)
			if *r.FirstPassRate >= 80 {
				fp = okStyle.Render(fp)
			}
			firstPass = fp
		}

		fmt.Fprintf(tw, "  %s\t%d\t%s\t%s\t%s\t%d\t%s\n",
			r.Name,
			r.TotalCasts,
			median,
			p90,
			firstPass,
			r.ReworkCount,
			formatTokenCount(r.TotalTokens))
	}
	tw.Flush()

	fmt.Print(b.String())
}

// formatTokenInt formats a token count with comma separators.
func formatTokenInt(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// formatTokenCount formats a token count with SI suffix for compact display.
func init() {
	agentCmd.AddCommand(agentStatsCmd)
	agentStatsCmd.Flags().StringVar(&agentStatsWorld, "world", "", "world name")
	agentStatsCmd.Flags().BoolVar(&agentStatsJSON, "json", false, "output as JSON")
}
