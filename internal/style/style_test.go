package style

import (
	"testing"
	"unicode/utf8"
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
