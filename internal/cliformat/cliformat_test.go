package cliformat

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

func TestFormatTimestamp(t *testing.T) {
	tests := []struct {
		name string
		in   time.Time
		want string
	}{
		{"zero", time.Time{}, EmptyMarker},
		{
			"utc",
			mustParse(t, "2026-04-10T00:08:30Z"),
			"2026-04-10T00:08:30Z",
		},
		{
			"nonUTC_converted",
			time.Date(2026, 4, 10, 2, 8, 30, 0, time.FixedZone("X", 2*3600)),
			"2026-04-10T00:08:30Z",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatTimestamp(tc.in)
			if got != tc.want {
				t.Fatalf("FormatTimestamp(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFormatRelative(t *testing.T) {
	now := mustParse(t, "2026-04-10T12:00:00Z")
	tests := []struct {
		name string
		in   time.Time
		want string
	}{
		{"zero", time.Time{}, EmptyMarker},
		{"just_now_30s", now.Add(-30 * time.Second), "just now"},
		{"minutes", now.Add(-2 * time.Minute), "2m ago"},
		{"minutes_edge", now.Add(-59 * time.Minute), "59m ago"},
		{"hours", now.Add(-3 * time.Hour), "3h ago"},
		{"hours_edge", now.Add(-23 * time.Hour), "23h ago"},
		{"days", now.Add(-5 * 24 * time.Hour), "5d ago"},
		{"distant_past", now.Add(-365 * 24 * time.Hour), "365d ago"},
		// Future times clamp to positive bucket.
		{"future_minute", now.Add(5 * time.Minute), "5m ago"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatRelative(tc.in, now)
			if got != tc.want {
				t.Fatalf("FormatRelative(%v, now) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFormatTimestampOrRelative(t *testing.T) {
	now := mustParse(t, "2026-04-10T12:00:00Z")
	tests := []struct {
		name string
		in   time.Time
		want string
	}{
		{"zero", time.Time{}, EmptyMarker},
		{"recent_minutes", now.Add(-10 * time.Minute), "10m ago"},
		{"recent_hours", now.Add(-6 * time.Hour), "6h ago"},
		{"boundary_24h_uses_timestamp", now.Add(-24 * time.Hour), "2026-04-09T12:00:00Z"},
		{"distant_past", mustParse(t, "2025-01-01T00:00:00Z"), "2025-01-01T00:00:00Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatTimestampOrRelative(tc.in, now)
			if got != tc.want {
				t.Fatalf("FormatTimestampOrRelative(%v, now) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFormatCount(t *testing.T) {
	tests := []struct {
		name             string
		n                int
		singular, plural string
		want             string
	}{
		{"zero_plural", 0, "world", "worlds", "0 worlds"},
		{"one_singular", 1, "world", "worlds", "1 world"},
		{"many_plural", 4, "world", "worlds", "4 worlds"},
		{"irregular", 1, "entry", "entries", "1 entry"},
		{"irregular_many", 3, "entry", "entries", "3 entries"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatCount(tc.n, tc.singular, tc.plural)
			if got != tc.want {
				t.Fatalf("FormatCount(%d,%q,%q) = %q, want %q", tc.n, tc.singular, tc.plural, got, tc.want)
			}
		})
	}
}

func TestEmptyMarkerConstant(t *testing.T) {
	if EmptyMarker != "-" {
		t.Fatalf("EmptyMarker = %q, want %q", EmptyMarker, "-")
	}
}
