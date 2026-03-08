package dash

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// confirmDismissMsg signals the confirm overlay was dismissed without action.
type confirmDismissMsg struct{}

// confirmModel renders a centered confirmation box with y/n handling.
type confirmModel struct {
	title  string  // e.g., "Restart Nova?"
	detail string  // e.g., "This will kill the session and re-cast the tethered writ."
	onYes  tea.Cmd // command to execute on confirmation
	active bool    // whether the overlay is showing
}

// show activates the confirmation overlay with the given parameters.
func (c *confirmModel) show(title, detail string, onYes tea.Cmd) {
	c.title = title
	c.detail = detail
	c.onYes = onYes
	c.active = true
}

// dismiss hides the confirmation overlay.
func (c *confirmModel) dismiss() {
	c.active = false
	c.title = ""
	c.detail = ""
	c.onYes = nil
}

// update handles key input while the confirm overlay is active.
// Returns true if the overlay consumed the message.
func (c *confirmModel) update(msg tea.KeyMsg) (bool, tea.Cmd) {
	if !c.active {
		return false, nil
	}

	switch msg.String() {
	case "y", "enter":
		cmd := c.onYes
		c.dismiss()
		return true, cmd
	case "n", "esc", "q":
		c.dismiss()
		return true, nil
	default:
		// Overlay is active — consume all keys, do nothing.
		return true, nil
	}
}

// view renders the confirmation overlay centered in the terminal.
func (c confirmModel) view(width, height int) string {
	if !c.active {
		return ""
	}

	// Build content lines.
	var content strings.Builder
	content.WriteString(lipgloss.NewStyle().Bold(true).Render(c.title))
	if c.detail != "" {
		// Word-wrap detail text to fit within the box.
		maxDetailWidth := 35
		wrapped := wordWrap(c.detail, maxDetailWidth)
		content.WriteString("\n\n")
		content.WriteString(wrapped)
	}
	content.WriteString("\n\n")
	content.WriteString(dimStyle.Render("y confirm · n cancel"))

	// Style the box — same aesthetic as help overlay.
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(1, 3).
		Foreground(lipgloss.Color("7")).
		Background(lipgloss.Color("235"))

	box := boxStyle.Render(content.String())

	// Center horizontally and vertically.
	boxLines := strings.Split(box, "\n")
	boxHeight := len(boxLines)
	boxWidth := lipgloss.Width(box)

	topPad := (height - boxHeight) / 2
	if topPad < 0 {
		topPad = 0
	}

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

// wordWrap wraps text to the given width, breaking on spaces.
func wordWrap(text string, width int) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}

	var lines []string
	currentLine := words[0]

	for _, word := range words[1:] {
		if len(currentLine)+1+len(word) > width {
			lines = append(lines, currentLine)
			currentLine = word
		} else {
			currentLine += " " + word
		}
	}
	lines = append(lines, currentLine)

	return strings.Join(lines, "\n")
}
