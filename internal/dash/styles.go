package dash

import "github.com/charmbracelet/lipgloss"

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

func statusIndicator(running bool) string {
	if running {
		return okStyle.Render(checkMark)
	}
	return errorStyle.Render(crossMark)
}
