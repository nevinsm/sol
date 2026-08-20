package style

import (
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

func TestFormatTokenInt(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
		{-1, "-1"},
		{-999, "-999"},
		{-1234, "-1,234"},
		{-1234567, "-1,234,567"},
		{-100, "-100"},
	}

	for _, tt := range tests {
		got := FormatTokenInt(tt.input)
		if got != tt.want {
			t.Errorf("FormatTokenInt(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTruncateBytesNoSplit(t *testing.T) {
	// Plain emoji string — every rune is 4 bytes.
	s := "🚀🚀🚀🚀🚀"
	for budget := 0; budget <= len(s)+4; budget++ {
		got := TruncateBytes(s, budget)
		if !utf8.ValidString(got) {
			t.Fatalf("TruncateBytes(%q, %d) produced invalid UTF-8: %q", s, budget, got)
		}
		if len(got) > budget && budget > 0 {
			t.Fatalf("TruncateBytes(%q, %d) exceeded budget: %q (len=%d)", s, budget, got, len(got))
		}
	}
}

func TestTruncateBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxBytes int
		want     string
	}{
		{"shorter than budget", "hello", 10, "hello"},
		{"exact budget", "hello", 5, "hello"},
		{"needs truncation", "hello world", 8, "hello..."},
		{"tiny budget no room for ellipsis", "hello", 2, "he"},
		{"zero budget", "hello", 0, "hello"},
		{"empty string", "", 5, ""},
		{"unicode safe", "こんにちは世界", 10, "こん..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateBytes(tt.input, tt.maxBytes)
			if got != tt.want {
				t.Errorf("TruncateBytes(%q, %d) = %q, want %q", tt.input, tt.maxBytes, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("TruncateBytes(%q, %d) produced invalid UTF-8: %q", tt.input, tt.maxBytes, got)
			}
		})
	}
}

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{"shorter than max", "hello", 10, "hello"},
		{"exact max", "hello", 5, "hello"},
		{"needs truncation", "hello world", 8, "hello..."},
		{"very short max", "hello", 3, "hel"},
		{"max 0", "hello", 0, ""},
		{"empty string", "", 5, ""},
		{"unicode safe", "こんにちは世界", 5, "こん..."},
		{"hello world at 3", "hello world", 3, "hel"},
		{"hello world at 2", "hello world", 2, "he"},
		{"abcdefghij at 7", "abcdefghij", 7, "abcd..."},
		{"short input untouched", "ab", 5, "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateRunes(tt.input, tt.max)
			if got != tt.want {
				t.Errorf("TruncateRunes(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("TruncateRunes(%q, %d) produced invalid UTF-8: %q", tt.input, tt.max, got)
			}
		})
	}
}

func TestTruncateRunesNoSplit(t *testing.T) {
	// Plain emoji string — every rune is 4 bytes.
	s := "🚀🚀🚀🚀🚀"
	for max := 0; max <= 8; max++ {
		got := TruncateRunes(s, max)
		if !utf8.ValidString(got) {
			t.Fatalf("TruncateRunes(%q, %d) produced invalid UTF-8: %q", s, max, got)
		}
	}
}

func TestTruncateWidthASCII(t *testing.T) {
	// For ASCII-only input, every rune is one cell wide, so behavior must
	// match TruncateRunes exactly.
	tests := []struct {
		name     string
		input    string
		maxCells int
		want     string
	}{
		{"shorter than max", "hello", 10, "hello"},
		{"exact max", "hello", 5, "hello"},
		{"needs truncation", "hello world", 8, "hello..."},
		{"very short max", "hello", 3, "hel"},
		{"max 0", "hello", 0, ""},
		{"empty string", "", 5, ""},
		{"hello world at 3", "hello world", 3, "hel"},
		{"hello world at 2", "hello world", 2, "he"},
		{"abcdefghij at 7", "abcdefghij", 7, "abcd..."},
		{"short input untouched", "ab", 5, "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateWidth(tt.input, tt.maxCells)
			if got != tt.want {
				t.Errorf("TruncateWidth(%q, %d) = %q, want %q", tt.input, tt.maxCells, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("TruncateWidth(%q, %d) produced invalid UTF-8: %q", tt.input, tt.maxCells, got)
			}
		})
	}
}

func TestTruncateWidthDoubleWidth(t *testing.T) {
	// Every rune in this CJK string occupies 2 cells (14 cells total for 7
	// runes), unlike TruncateRunes which would count 7 "characters".
	s := "こんにちは世界"
	if got, want := lipgloss.Width(s), 14; got != want {
		t.Fatalf("test fixture assumption wrong: lipgloss.Width(%q) = %d, want %d", s, got, want)
	}

	tests := []struct {
		name     string
		maxCells int
		want     string
	}{
		{"exact width", 14, "こんにちは世界"},
		{"wider than needed", 20, "こんにちは世界"},
		{"truncate with room for ellipsis", 7, "こん..."},
		{"truncate to one char plus ellipsis", 5, "こ..."},
		{"no room for ellipsis, one char fits", 3, "こ"},
		{"no room for ellipsis, exact fit", 2, "こ"},
		{"no room for even one double-width char", 1, ""},
		{"zero budget", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateWidth(s, tt.maxCells)
			if got != tt.want {
				t.Errorf("TruncateWidth(%q, %d) = %q, want %q", s, tt.maxCells, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("TruncateWidth(%q, %d) produced invalid UTF-8: %q", s, tt.maxCells, got)
			}
			if w := lipgloss.Width(got); tt.maxCells > 0 && w > tt.maxCells {
				t.Errorf("TruncateWidth(%q, %d) = %q exceeded budget: width=%d", s, tt.maxCells, got, w)
			}
		})
	}
}

func TestTruncateWidthEmoji(t *testing.T) {
	// Most emoji render double-width in terminals.
	s := "🚀🚀🚀🚀🚀"
	for maxCells := 0; maxCells <= lipgloss.Width(s)+4; maxCells++ {
		got := TruncateWidth(s, maxCells)
		if !utf8.ValidString(got) {
			t.Fatalf("TruncateWidth(%q, %d) produced invalid UTF-8: %q", s, maxCells, got)
		}
		if w := lipgloss.Width(got); maxCells > 0 && w > maxCells {
			t.Fatalf("TruncateWidth(%q, %d) = %q exceeded budget: width=%d", s, maxCells, got, w)
		}
	}
}

func TestTruncateWidthNeverExceedsBudget(t *testing.T) {
	// Fuzz-lite: across a mix of ASCII, CJK, and emoji inputs and a wide
	// range of budgets, the visible width of the result — measured the same
	// way padRight measures (lipgloss.Width) — must never exceed maxCells.
	inputs := []string{
		"hello world",
		"こんにちは世界",
		"🚀🚀🚀🚀🚀",
		"mixed 混合 🎉 text",
		"",
	}
	for _, s := range inputs {
		total := lipgloss.Width(s)
		for maxCells := 0; maxCells <= total+4; maxCells++ {
			got := TruncateWidth(s, maxCells)
			if !utf8.ValidString(got) {
				t.Fatalf("TruncateWidth(%q, %d) produced invalid UTF-8: %q", s, maxCells, got)
			}
			if w := lipgloss.Width(got); maxCells > 0 && w > maxCells {
				t.Fatalf("TruncateWidth(%q, %d) = %q exceeded budget: width=%d (maxCells=%d)", s, maxCells, got, w, maxCells)
			}
		}
	}
}
