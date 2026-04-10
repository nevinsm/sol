package cmd

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestDurationPercentile(t *testing.T) {
	tests := []struct {
		name   string
		values []time.Duration
		p      float64
		want   time.Duration
	}{
		{
			name:   "empty",
			values: nil,
			p:      50,
			want:   0,
		},
		{
			name:   "single value",
			values: []time.Duration{10 * time.Minute},
			p:      50,
			want:   10 * time.Minute,
		},
		{
			name:   "single value p90",
			values: []time.Duration{10 * time.Minute},
			p:      90,
			want:   10 * time.Minute,
		},
		{
			name:   "two values median",
			values: []time.Duration{10 * time.Minute, 20 * time.Minute},
			p:      50,
			want:   15 * time.Minute,
		},
		{
			name:   "three values median",
			values: []time.Duration{10 * time.Minute, 20 * time.Minute, 30 * time.Minute},
			p:      50,
			want:   20 * time.Minute,
		},
		{
			name: "ten values p90",
			values: []time.Duration{
				1 * time.Minute, 2 * time.Minute, 3 * time.Minute,
				4 * time.Minute, 5 * time.Minute, 6 * time.Minute,
				7 * time.Minute, 8 * time.Minute, 9 * time.Minute,
				10 * time.Minute,
			},
			p:    90,
			want: 9*time.Minute + 6*time.Second, // 9.1 minutes interpolated
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := durationPercentile(tt.values, tt.p)
			// Allow 1 second tolerance for floating-point interpolation.
			diff := got - tt.want
			if diff < 0 {
				diff = -diff
			}
			if diff > time.Second {
				t.Fatalf("percentile(%v, %.0f) = %v, want %v", tt.values, tt.p, got, tt.want)
			}
		})
	}
}

func TestFormatTokenInt(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
		{10000000, "10,000,000"},
	}

	for _, tt := range tests {
		got := formatTokenInt(tt.input)
		if got != tt.want {
			t.Errorf("formatTokenInt(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{1000000, "1.0M"},
		{2500000, "2.5M"},
	}

	for _, tt := range tests {
		got := formatTokenCount(tt.input)
		if got != tt.want {
			t.Errorf("formatTokenCount(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRenderLeaderboardEmpty(t *testing.T) {
	// W1.13: empty result must render an empty table (header row only)
	// with a "0 agents" footer, NOT a "No agent stats available." sentence.
	got := renderLeaderboard("sol-dev", nil)

	if strings.Contains(got, "No agent stats available") {
		t.Fatalf("empty leaderboard should not print the legacy sentence; got:\n%s", got)
	}
	if !strings.Contains(got, "Agent Leaderboard (sol-dev)") {
		t.Errorf("expected world header in empty leaderboard; got:\n%s", got)
	}
	// Column header row must still be rendered.
	for _, col := range []string{"NAME", "CASTS", "MEDIAN", "P90", "1ST-PASS", "REWORK", "TOKENS"} {
		if !strings.Contains(got, col) {
			t.Errorf("expected column %q in empty leaderboard; got:\n%s", col, got)
		}
	}
	// Footer count.
	if !strings.Contains(got, "0 agents") {
		t.Errorf("expected '0 agents' footer in empty leaderboard; got:\n%s", got)
	}
}

func TestRenderLeaderboardPopulatedUsesEmptyMarker(t *testing.T) {
	// A report with no cycle-time or merge stats should use the canonical
	// cliformat.EmptyMarker ("-") in its empty cells, and the footer should
	// pluralise correctly.
	reports := []AgentStatsReport{
		{Name: "Vega", TotalCasts: 3, ReworkCount: 1},
	}
	got := renderLeaderboard("sol-dev", reports)

	if !strings.Contains(got, "Vega") {
		t.Errorf("expected agent name in leaderboard; got:\n%s", got)
	}
	// Empty cells must use the canonical marker.
	if !strings.Contains(got, "-") {
		t.Errorf("expected EmptyMarker '-' in empty cells; got:\n%s", got)
	}
	// Footer uses singular form.
	if !strings.Contains(got, "1 agent\n") {
		t.Errorf("expected '1 agent' singular footer; got:\n%s", got)
	}
}

func TestDurationPercentileP90Interpolation(t *testing.T) {
	// 10 values: p90 should interpolate between index 8.1
	// sorted: [1,2,3,4,5,6,7,8,9,10] minutes
	// idx = 0.9 * 9 = 8.1
	// result = sorted[8]*0.9 + sorted[9]*0.1 = 9*0.9 + 10*0.1 = 8.1 + 1.0 = 9.1 minutes
	sorted := make([]time.Duration, 10)
	for i := range sorted {
		sorted[i] = time.Duration(i+1) * time.Minute
	}
	got := durationPercentile(sorted, 90)
	wantMinutes := 9.1
	gotMinutes := got.Minutes()
	if math.Abs(gotMinutes-wantMinutes) > 0.01 {
		t.Fatalf("p90 = %.2f minutes, want %.2f", gotMinutes, wantMinutes)
	}
}
