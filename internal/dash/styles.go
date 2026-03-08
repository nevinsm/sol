package dash

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/nevinsm/sol/internal/status"
)

// Color semantics — mirror internal/status/render.go.
var (
	headerStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))  // bright blue
	okStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))             // green
	warnStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))             // yellow
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))              // red
	dimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))              // gray
	selectStyle    = lipgloss.NewStyle().Background(lipgloss.Color("236")).Bold(true) // row highlight
	focusStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))  // focused section header (cyan)
	highlightStyle = lipgloss.NewStyle().Background(lipgloss.Color("237"))            // state-change highlight
)

// Health badge strings — same semantics as render.go.
var (
	healthyBadge   = okStyle.Render("● healthy")
	unhealthyBadge = errorStyle.Render("● unhealthy")
	degradedBadge  = warnStyle.Render("● degraded")
	sleepingBadge  = dimStyle.Render("○ sleeping")
	unknownBadge   = dimStyle.Render("● unknown")
)

func healthBadge(health string) string {
	return healthBadgeWithEmphasis(health, false)
}

func healthBadgeWithEmphasis(health string, emphasis bool) string {
	var badge string
	switch health {
	case "healthy":
		badge = healthyBadge
	case "unhealthy":
		badge = unhealthyBadge
	case "degraded":
		badge = degradedBadge
	case "sleeping":
		badge = sleepingBadge
	default:
		badge = unknownBadge
	}
	if emphasis {
		return lipgloss.NewStyle().Bold(true).Render(badge)
	}
	return badge
}

// Static indicators for inactive items.
const (
	checkMark = "✓"
	crossMark = "✗"
)

// padRight pads s with spaces to reach the given visible width.
// Unlike fmt.Sprintf("%-Ns"), this measures visible width excluding
// ANSI escape codes, so styled strings align correctly.
func padRight(s string, width int) string {
	visible := lipgloss.Width(s)
	if visible >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visible)
}

// truncateStr truncates a plain (unstyled) string to max visible runes,
// appending "..." if truncated.
func truncateStr(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func statusIndicator(running bool) string {
	if running {
		return okStyle.Render(checkMark)
	}
	return errorStyle.Render(crossMark)
}

// Process detail formatters — mirror internal/status/render.go.

func formatPrefectDetail(p status.PrefectInfo) string {
	if p.Running && p.PID > 0 {
		return fmt.Sprintf("pid %d", p.PID)
	}
	return ""
}

func formatConsulDetail(c status.ConsulInfo) string {
	if !c.Running {
		return ""
	}
	parts := fmt.Sprintf("%d patrols", c.PatrolCount)
	if c.HeartbeatAge != "" {
		parts += fmt.Sprintf(", last %s ago", c.HeartbeatAge)
	}
	return parts
}

func formatChronicleDetail(c status.ChronicleInfo) string {
	if !c.Running {
		return ""
	}
	if c.SessionName != "" {
		return c.SessionName
	}
	if c.PID > 0 {
		return fmt.Sprintf("pid %d", c.PID)
	}
	return ""
}

func formatLedgerDetail(l status.LedgerInfo) string {
	if !l.Running {
		return ""
	}
	if l.SessionName != "" {
		return l.SessionName
	}
	if l.PID > 0 {
		return fmt.Sprintf("pid %d", l.PID)
	}
	return ""
}

func formatBrokerDetail(b status.BrokerInfo) string {
	if !b.Running {
		return ""
	}
	return fmt.Sprintf("%d accounts", b.Accounts)
}

func formatSenateDetail(s status.SenateInfo) string {
	if s.Running {
		return s.SessionName
	}
	return ""
}

func formatForgeDetail(f status.ForgeInfo) string {
	if f.Running {
		return f.SessionName
	}
	return ""
}

func formatSentinelDetail(s status.SentinelInfo) string {
	if s.Running {
		return s.SessionName
	}
	return ""
}

func formatGovernorDetail(g status.GovernorInfo) string {
	if g.BriefAge != "" {
		return "brief: " + g.BriefAge + " ago"
	}
	return ""
}
