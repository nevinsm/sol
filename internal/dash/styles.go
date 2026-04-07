package dash

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
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

// highlightStyles contains pre-computed styles for each highlight level.
// Index 0 is an empty style (no highlight), 1-5 are progressively brighter backgrounds.
// Pre-computed to avoid lipgloss.NewStyle() allocations on every render frame.
var highlightStyles [6]lipgloss.Style

func init() {
	highlightStyles[0] = lipgloss.NewStyle()
	for i := 1; i <= 5; i++ {
		highlightStyles[i] = lipgloss.NewStyle().Background(lipgloss.Color(highlightColors[i]))
	}
}

// highlightAtLevel returns a style with the background color for the given highlight level.
// Returns an empty style for level 0 (no highlight).
func highlightAtLevel(level int) lipgloss.Style {
	if level <= 0 || level > 5 {
		return highlightStyles[0]
	}
	return highlightStyles[level]
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

// widthCache caches lipgloss.Width results to avoid repeated ANSI parsing.
// Safe without synchronization because bubbletea runs View() in a single goroutine.
var widthCache = make(map[string]int)

// widthCacheMaxEntries bounds widthCache memory growth in long-running sessions.
// When exceeded, the cache is cleared entirely (simpler than LRU; the hot
// strings will be re-cached on the next render pass).
const widthCacheMaxEntries = 500

// cachedWidth returns the visible width of s, caching the result to avoid
// repeated ANSI escape code parsing on the render hot path.
func cachedWidth(s string) int {
	if w, ok := widthCache[s]; ok {
		return w
	}
	if len(widthCache) >= widthCacheMaxEntries {
		widthCache = make(map[string]int)
	}
	w := lipgloss.Width(s)
	widthCache[s] = w
	return w
}

// padRight pads s with spaces to reach the given visible width.
// Unlike fmt.Sprintf("%-Ns"), this measures visible width excluding
// ANSI escape codes, so styled strings align correctly.
func padRight(s string, width int) string {
	visible := cachedWidth(s)
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

// Process detail formatters live in internal/statusformat — see
// internal/dash/sphere.go for callsites. Keeping a single source of truth
// for the formatter field set prevents the dashboard from drifting from
// `sol status` (the bug class CF-M26 / pattern P5 was about exactly that).

// formatCompactTokens formats a token count as a compact human-readable string.
// Mirrors status.formatCompactTokens for use in dashboard views.
func formatCompactTokens(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		v := float64(n) / 1000
		if v < 9.95 {
			return fmt.Sprintf("%.1fK", v)
		}
		return fmt.Sprintf("%.0fK", v)
	}
	v := float64(n) / 1_000_000
	if v < 9.95 {
		return fmt.Sprintf("%.1fM", v)
	}
	return fmt.Sprintf("%.0fM", v)
}

// formatCost formats a USD cost value for display.
// Mirrors status.formatCost for use in dashboard views.
func formatCost(cost float64) string {
	if cost < 0.01 {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}

// feedHighlightStyles contains pre-computed styles for feed entry fade levels.
// Index 4 is brightest (new!), index 0 is the normal dimStyle.
// Pre-computed to avoid lipgloss.NewStyle() allocations on every render frame.
var feedHighlightStyles [5]lipgloss.Style

func init() {
	feedHighlightStyles[0] = dimStyle
	feedHighlightStyles[1] = lipgloss.NewStyle().Foreground(lipgloss.Color("245")) // medium gray
	feedHighlightStyles[2] = lipgloss.NewStyle().Foreground(lipgloss.Color("250")) // light gray
	feedHighlightStyles[3] = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))   // normal white
	feedHighlightStyles[4] = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))  // bright white
}

// feedHighlightAtLevel returns a style for feed entries at the given fade level.
// Level 4 is brightest (new!), level 0 is the normal dimStyle.
func feedHighlightAtLevel(level int) lipgloss.Style {
	if level < 0 || level > 4 {
		return feedHighlightStyles[0]
	}
	return feedHighlightStyles[level]
}
