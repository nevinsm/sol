package trace

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("12"))
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// RenderFull renders the complete trace output.
func RenderFull(td *TraceData) string {
	var b strings.Builder

	renderHeader(&b, td)
	b.WriteString("\n")
	renderTimeline(&b, td)
	b.WriteString("\n")
	renderCost(&b, td)
	b.WriteString("\n")
	renderEscalations(&b, td)

	if len(td.Degradations) > 0 {
		b.WriteString("\n")
		for _, d := range td.Degradations {
			b.WriteString(dimStyle.Render(d))
			b.WriteString("\n")
		}
	}

	return b.String()
}

// RenderTimeline renders only the timeline section.
func RenderTimeline(td *TraceData) string {
	var b strings.Builder
	renderHeader(&b, td)
	b.WriteString("\n")
	renderTimeline(&b, td)

	if len(td.Degradations) > 0 {
		b.WriteString("\n")
		for _, d := range td.Degradations {
			b.WriteString(dimStyle.Render(d))
			b.WriteString("\n")
		}
	}

	return b.String()
}

// RenderCost renders only the cost section.
func RenderCost(td *TraceData) string {
	var b strings.Builder
	renderHeader(&b, td)
	b.WriteString("\n")
	renderCost(&b, td)

	if len(td.Degradations) > 0 {
		b.WriteString("\n")
		for _, d := range td.Degradations {
			b.WriteString(dimStyle.Render(d))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func renderHeader(b *strings.Builder, td *TraceData) {
	w := td.Writ

	b.WriteString(fmt.Sprintf("Writ: %s\n", headerStyle.Render(w.ID)))
	b.WriteString(fmt.Sprintf("Title: %s\n", w.Title))

	kind := w.Kind
	if kind == "" {
		kind = "code"
	}
	b.WriteString(fmt.Sprintf("Kind: %s    Status: %s    Priority: %d\n", kind, w.Status, w.Priority))

	created := w.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")
	b.WriteString(fmt.Sprintf("Created: %s", created))
	if w.ClosedAt != nil {
		b.WriteString(fmt.Sprintf("    Closed: %s", w.ClosedAt.UTC().Format("2006-01-02T15:04:05Z")))
	}
	b.WriteString(fmt.Sprintf("    World: %s", td.World))
	b.WriteString("\n")

	if len(td.Labels) > 0 {
		b.WriteString(fmt.Sprintf("Labels: %s\n", strings.Join(td.Labels, ", ")))
	}

	// Caravan info.
	if len(td.CaravanItems) > 0 {
		var parts []string
		for _, ci := range td.CaravanItems {
			name := ci.CaravanID
			if c, ok := td.Caravans[ci.CaravanID]; ok {
				name = c.Name
			}
			parts = append(parts, fmt.Sprintf("%s (phase %d)", name, ci.Phase))
		}
		b.WriteString(fmt.Sprintf("Caravan: %s\n", strings.Join(parts, ", ")))
	}

	// Dependencies.
	if len(td.Dependencies) > 0 || len(td.Dependents) > 0 {
		var depParts []string
		if len(td.Dependencies) > 0 {
			depParts = append(depParts, fmt.Sprintf("depends on %s", strings.Join(td.Dependencies, ", ")))
		}
		if len(td.Dependents) > 0 {
			depParts = append(depParts, fmt.Sprintf("blocks %s", strings.Join(td.Dependents, ", ")))
		}
		b.WriteString(fmt.Sprintf("Dependencies: %s\n", strings.Join(depParts, "; ")))
	}

	// Tethers.
	if len(td.Tethers) > 0 {
		var parts []string
		for _, t := range td.Tethers {
			parts = append(parts, fmt.Sprintf("%s (%s)", t.Agent, t.Role))
		}
		b.WriteString(fmt.Sprintf("Tethered: %s\n", strings.Join(parts, ", ")))
	}
}

func renderTimeline(b *strings.Builder, td *TraceData) {
	b.WriteString(headerStyle.Render("── Timeline "))
	b.WriteString(headerStyle.Render(strings.Repeat("─", 58)))
	b.WriteString("\n")

	if len(td.Timeline) == 0 {
		b.WriteString(dimStyle.Render("  (no events)"))
		b.WriteString("\n")
		return
	}

	for _, e := range td.Timeline {
		ts := e.Timestamp.UTC().Format("15:04:05Z")
		b.WriteString(fmt.Sprintf("  %s  %-14s %s\n", ts, e.Action, e.Detail))
	}
}

func renderCost(b *strings.Builder, td *TraceData) {
	b.WriteString(headerStyle.Render("── Cost "))
	b.WriteString(headerStyle.Render(strings.Repeat("─", 62)))
	b.WriteString("\n")

	if td.Cost == nil || len(td.Cost.Models) == 0 {
		b.WriteString(dimStyle.Render("  (no token data)"))
		b.WriteString("\n")
		return
	}

	hasPricing := false
	hasReasoning := false
	for _, m := range td.Cost.Models {
		if m.Cost > 0 {
			hasPricing = true
		}
		if m.ReasoningTokens > 0 {
			hasReasoning = true
		}
	}

	tw := tabwriter.NewWriter(b, 0, 4, 2, ' ', 0)
	switch {
	case hasReasoning && hasPricing:
		fmt.Fprintf(tw, "  Model\tInput\tOutput\tReasoning\tCache Read\tCache Write\tCost\n")
	case hasReasoning:
		fmt.Fprintf(tw, "  Model\tInput\tOutput\tReasoning\tCache Read\tCache Write\n")
	case hasPricing:
		fmt.Fprintf(tw, "  Model\tInput\tOutput\tCache Read\tCache Write\tCost\n")
	default:
		fmt.Fprintf(tw, "  Model\tInput\tOutput\tCache Read\tCache Write\n")
	}

	var totalInput, totalOutput, totalCacheRead, totalCacheWrite, totalReasoning int64
	for _, m := range td.Cost.Models {
		totalInput += m.InputTokens
		totalOutput += m.OutputTokens
		totalCacheRead += m.CacheReadTokens
		totalCacheWrite += m.CacheCreationTokens
		totalReasoning += m.ReasoningTokens

		switch {
		case hasReasoning && hasPricing:
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t$%.2f\n",
				m.Model,
				formatTokenInt(m.InputTokens),
				formatTokenInt(m.OutputTokens),
				formatTokenInt(m.ReasoningTokens),
				formatTokenInt(m.CacheReadTokens),
				formatTokenInt(m.CacheCreationTokens),
				m.Cost)
		case hasReasoning:
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\n",
				m.Model,
				formatTokenInt(m.InputTokens),
				formatTokenInt(m.OutputTokens),
				formatTokenInt(m.ReasoningTokens),
				formatTokenInt(m.CacheReadTokens),
				formatTokenInt(m.CacheCreationTokens))
		case hasPricing:
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t$%.2f\n",
				m.Model,
				formatTokenInt(m.InputTokens),
				formatTokenInt(m.OutputTokens),
				formatTokenInt(m.CacheReadTokens),
				formatTokenInt(m.CacheCreationTokens),
				m.Cost)
		default:
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n",
				m.Model,
				formatTokenInt(m.InputTokens),
				formatTokenInt(m.OutputTokens),
				formatTokenInt(m.CacheReadTokens),
				formatTokenInt(m.CacheCreationTokens))
		}
	}

	if len(td.Cost.Models) > 1 && hasPricing {
		if hasReasoning {
			fmt.Fprintf(tw, "  \t\t\t\t\t\tTotal: $%.2f\n", td.Cost.Total)
		} else {
			fmt.Fprintf(tw, "  \t\t\t\t\tTotal: $%.2f\n", td.Cost.Total)
		}
	}
	tw.Flush()

	if td.Cost.CycleTime != "" {
		b.WriteString(fmt.Sprintf("  Cycle time: %s (cast → merge)\n", td.Cost.CycleTime))
	}
}

func renderEscalations(b *strings.Builder, td *TraceData) {
	b.WriteString(headerStyle.Render("── Escalations "))
	b.WriteString(headerStyle.Render(strings.Repeat("─", 55)))
	b.WriteString("\n")

	if len(td.Escalations) == 0 {
		b.WriteString(dimStyle.Render("  (none)"))
		b.WriteString("\n")
		return
	}

	for _, esc := range td.Escalations {
		ts := esc.CreatedAt.UTC().Format("15:04:05Z")
		b.WriteString(fmt.Sprintf("  %s  [%s] %s  %s\n",
			ts,
			esc.Severity,
			esc.Description,
			dimStyle.Render(fmt.Sprintf("(%s)", esc.Status))))
	}
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
