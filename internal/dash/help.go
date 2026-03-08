package dash

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const helpContent = `Sol Dash — Keyboard Shortcuts

Navigation
  ↑/↓ or j/k    Move selection
  enter or l     Drill in / Peek process / Attach
  esc or h       Back to sphere view
  tab            Cycle sections

Actions
  a              Attach directly to session
  R              Restart selected process/agent/service
  r              Force refresh
  q              Quit
  ?              Toggle this help`

// helpOverlay renders the help content centered in the terminal.
func helpOverlay(width, height int) string {
	// Style the help box.
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(1, 3).
		Foreground(lipgloss.Color("7")).
		Background(lipgloss.Color("235"))

	box := boxStyle.Render(helpContent)

	// Center horizontally and vertically.
	boxLines := strings.Split(box, "\n")
	boxHeight := len(boxLines)
	boxWidth := lipgloss.Width(box)

	// Vertical centering.
	topPad := (height - boxHeight) / 2
	if topPad < 0 {
		topPad = 0
	}

	// Horizontal centering.
	leftPad := (width - boxWidth) / 2
	if leftPad < 0 {
		leftPad = 0
	}

	var b strings.Builder
	for i := 0; i < topPad; i++ {
		b.WriteString("\n")
	}
	padding := strings.Repeat(" ", leftPad)
	for _, line := range boxLines {
		b.WriteString(padding)
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}
