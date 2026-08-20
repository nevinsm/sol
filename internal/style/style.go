package style

import (
	"fmt"
	"unicode/utf8"

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

// TruncateBytes truncates s so that its byte length is at most maxBytes,
// cutting only at rune boundaries. If truncation occurs and there is room,
// "..." is appended as a visual indicator. Never returns invalid UTF-8.
//
// Use this for byte-budget sizing (log lines, message bodies, anything
// bounded by a byte cap rather than a display column count).
func TruncateBytes(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	const ellipsis = "..."
	if maxBytes <= len(ellipsis) {
		// No room for ellipsis — just take whole runes up to the budget.
		var n int
		for i := range s {
			if i > maxBytes {
				break
			}
			n = i
		}
		return s[:n]
	}
	budget := maxBytes - len(ellipsis)
	var end int
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		if i+size > budget {
			break
		}
		i += size
		end = i
	}
	return s[:end] + ellipsis
}

// TruncateRunes truncates s to at most max runes, appending "..." if
// truncation occurs. Never splits a multi-byte UTF-8 sequence.
//
// Use this for rune/char-count sizing (table columns, list rows — anything
// bounded by a visible character count rather than a byte budget).
func TruncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}
