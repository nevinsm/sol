package style

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Base styles — shared color semantics used across sol's terminal UI.
var (
	// Header is bold bright-blue, used for section headings.
	Header = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))

	// OK is green, used for success indicators.
	OK = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))

	// Warn is yellow, used for warning indicators.
	Warn = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))

	// Error is red, used for error indicators.
	Error = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))

	// Dim is gray, used for secondary/muted text.
	Dim = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// FormatTokenInt formats a token count with comma separators.
func FormatTokenInt(n int64) string {
	s := fmt.Sprintf("%d", n)
	prefix := ""
	if s[0] == '-' {
		prefix = "-"
		s = s[1:]
	}
	if len(s) <= 3 {
		return prefix + s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return prefix + string(result)
}
