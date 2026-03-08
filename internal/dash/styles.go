package dash

import "github.com/charmbracelet/lipgloss"

// Color semantics — mirror internal/status/render.go.
var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))  // bright blue
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))             // green
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))             // yellow
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))              // red
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))              // gray
	selectStyle = lipgloss.NewStyle().Background(lipgloss.Color("236")).Bold(true) // row highlight
	focusStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))  // focused section header (cyan)
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
