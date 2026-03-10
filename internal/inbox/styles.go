package inbox

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Color semantics — mirror internal/dash/styles.go and internal/status/render.go.
// Defined as standalone constants to avoid coupling.
var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))  // bright blue
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))             // green
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))             // yellow
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))              // red
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))              // gray
	selectStyle = lipgloss.NewStyle().Background(lipgloss.Color("236")).Bold(true) // row highlight

	// Flash highlight for action confirmation (progressive decay).
	flashStyle = lipgloss.NewStyle().Background(lipgloss.Color("22")) // dark green bg
)

// highlightMaxLevel is the initial intensity level for action flash highlights.
const highlightMaxLevel = 5

// highlightColors maps highlight intensity levels to progressively dimmer background colors.
var highlightColors = [6]string{
	"",    // level 0: no highlight
	"235", // level 1: barely visible
	"236", // level 2
	"237", // level 3
	"22",  // level 4: dark green
	"28",  // level 5: brightest green
}

// highlightAtLevel returns a style with the background color for the given highlight level.
func highlightAtLevel(level int) lipgloss.Style {
	if level <= 0 || level > 5 {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(highlightColors[level]))
}

// focusIndicator marks the currently selected row.
const focusIndicator = "▸"

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
