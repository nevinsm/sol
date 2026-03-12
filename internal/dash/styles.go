package dash

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/nevinsm/sol/internal/status"
)

// spinnerForRole returns the spinner style for a given process/agent role.
// Each role category gets a visually distinct animation so the dashboard
// is easy to scan at a glance.
func spinnerForRole(role string) spinner.Spinner {
	switch role {
	case "world-process":
		return spinner.Line
	case "outpost":
		return spinner.MiniDot
	case "envoy":
		return spinner.Ellipsis
	default: // sphere-process, world summary, etc.
		return spinner.Dot
	}
}

// Color semantics — mirror internal/status/render.go.
var (
	headerStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))  // bright blue
	okStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))             // green
	warnStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))             // yellow
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))              // red
	dimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))              // gray
	selectStyle    = lipgloss.NewStyle().Background(lipgloss.Color("236")).Bold(true) // row highlight
	focusStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))  // focused section header (cyan)
)

// highlightMaxLevel is the initial intensity level for state-change highlights.
const highlightMaxLevel = 5

// highlightColors maps highlight intensity levels to progressively dimmer background colors.
// Level 5 is the brightest, level 1 is barely visible, level 0 means no highlight.
var highlightColors = [6]string{
	"",    // level 0: no highlight
	"235", // level 1: barely visible
	"236", // level 2
	"237", // level 3: matches original highlightStyle
	"238", // level 4
	"239", // level 5: brightest
}

// highlightAtLevel returns a style with the background color for the given highlight level.
// Returns an empty style for level 0 (no highlight).
func highlightAtLevel(level int) lipgloss.Style {
	if level <= 0 || level > 5 {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(highlightColors[level]))
}

// Health badge strings — same semantics as render.go.
var (
	healthyBadge   = okStyle.Render("● healthy")
	unhealthyBadge = errorStyle.Render("● unhealthy")
	degradedBadge  = warnStyle.Render("● degraded")
	sleepingBadge  = dimStyle.Render("○ sleeping")
	unknownBadge   = dimStyle.Render("● unknown")
)

func healthBadge(health string) string {
	return healthBadgeWithEmphasis(health, 0)
}

func healthBadgeWithEmphasis(health string, level int) string {
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
	if level > 0 {
		style := highlightAtLevel(level).Bold(true)
		return style.Render(badge)
	}
	return badge
}

// Static indicators for inactive items.
const (
	checkMark      = "✓"
	crossMark      = "✗"
	focusIndicator = "▸"
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

// optionalStatusIndicator returns a dim ○ for non-running optional processes
// instead of the alarming red ✗ used for required processes.
func optionalStatusIndicator(running bool) string {
	if running {
		return okStyle.Render(checkMark)
	}
	return dimStyle.Render("○")
}

// pulsingStatusIndicator returns a status indicator that pulses when not running.
func pulsingStatusIndicator(running bool, pulseBright bool) string {
	if running {
		return okStyle.Render(checkMark)
	}
	return pulseStyle(errorStyle, pulseBright).Render(crossMark)
}

// pulseStyle returns the base style with bold set according to the bright flag.
// Used for pulsing critical indicators: bold toggles on/off at ~1s cycle.
func pulseStyle(base lipgloss.Style, bright bool) lipgloss.Style {
	return base.Bold(bright)
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
	var parts string
	if c.PID > 0 {
		parts = fmt.Sprintf("pid %d", c.PID)
	}
	if c.HeartbeatAge != "" {
		if parts != "" {
			parts += " "
		}
		parts += fmt.Sprintf("hb %s", c.HeartbeatAge)
	}
	if c.Stale {
		parts += " (stale)"
	}
	return parts
}

func formatLedgerDetail(l status.LedgerInfo) string {
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
	if detail == "" {
		return "running"
	}
	return detail
}

func formatBrokerDetail(b status.BrokerInfo) string {
	if !b.Running {
		return ""
	}
	return fmt.Sprintf("%d patrols", b.PatrolCount)
}

func formatSenateDetail(s status.SenateInfo) string {
	if s.Running {
		return s.SessionName
	}
	return ""
}

func formatForgeDetail(f status.ForgeInfo) string {
	if f.Running && f.PID > 0 {
		detail := fmt.Sprintf("pid %d", f.PID)
		if f.Merging {
			detail += " [merging]"
		}
		return detail
	}
	return ""
}

func formatSentinelDetail(s status.SentinelInfo) string {
	if !s.Running {
		return ""
	}
	if s.PatrolCount > 0 {
		parts := fmt.Sprintf("%d patrols, %d checked", s.PatrolCount, s.AgentsChecked)
		if s.HeartbeatAge != "" {
			parts += fmt.Sprintf(", last %s ago", s.HeartbeatAge)
		}
		return parts
	}
	if s.PID > 0 {
		return fmt.Sprintf("pid %d", s.PID)
	}
	return ""
}

func formatGovernorDetail(g status.GovernorInfo) string {
	if g.BriefAge != "" {
		return "brief: " + g.BriefAge + " ago"
	}
	return ""
}

// feedHighlightAtLevel returns a style for feed entries at the given fade level.
// Level 4 is brightest (new!), level 0 is the normal dimStyle.
func feedHighlightAtLevel(level int) lipgloss.Style {
	switch level {
	case 4:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("15"))  // bright white
	case 3:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("7"))   // normal white
	case 2:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("250")) // light gray
	case 1:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("245")) // medium gray
	default:
		return dimStyle // level 0 — normal dim gray
	}
}
