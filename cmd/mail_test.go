package cmd

import (
	"testing"
	"time"
)

func TestParseHumanDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		wantErr  bool
	}{
		// Standard Go durations.
		{"24h", 24 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"1h30m", 90 * time.Minute, false},

		// Day-based durations.
		{"7d", 7 * 24 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},

		// Days + standard suffix.
		{"7d12h", 7*24*time.Hour + 12*time.Hour, false},
		{"1d6h30m", 24*time.Hour + 6*time.Hour + 30*time.Minute, false},

		// Invalid inputs.
		{"", 0, true},
		{"abc", 0, true},
		{"d", 0, true},
		{"7x", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseHumanDuration(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Fatalf("parseHumanDuration(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
