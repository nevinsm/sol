// Package cliformat provides shared helpers for sol's CLI list and status
// views. Historically each cmd/ file implemented its own timestamp rendering
// and empty-cell marker, which resulted in five distinct timestamp formats
// and four distinct empty-value markers across the CLI surface.
//
// This package is the canonical home for those primitives. Any list, table,
// or status view in cmd/ should import these helpers rather than inventing
// its own.
//
// Scope boundary vs internal/statusformat:
//
//   - internal/statusformat renders process-detail one-liners for
//     `sol status` and `sol dash` (prefect/consul/forge/sentinel/...).
//   - internal/cliformat renders primitive cells (timestamps, counts,
//     empty markers) used by every list view.
//
// Kept separate so statusformat's DTO surface does not bleed into generic
// table formatting.
package cliformat

import (
	"fmt"
	"time"
)

// EmptyMarker is the canonical placeholder for a table cell with no value.
// Every list/status view MUST use this constant instead of inventing its
// own marker ("", "-", "n/a", "none").
const EmptyMarker = "-"

// FormatTimestamp renders t as RFC3339 in UTC, e.g. "2026-04-10T00:08:30Z".
// A zero time renders as EmptyMarker.
func FormatTimestamp(t time.Time) string {
	if t.IsZero() {
		return EmptyMarker
	}
	return t.UTC().Format(time.RFC3339)
}

// FormatRelative renders t as a short relative duration from now
// ("2m ago", "3h ago", "5d ago"). now is passed explicitly so tests can
// pin time. A zero time renders as EmptyMarker.
//
// Buckets:
//   - <1 minute  -> "just now"
//   - <1 hour    -> "Nm ago"
//   - <1 day     -> "Nh ago"
//   - otherwise  -> "Nd ago"
//
// Future times (t after now) are rendered the same way using the absolute
// delta with a "in N…" prefix would be nice but is out of scope for the
// current list views — treat future times as "just now" if within a minute,
// otherwise as their absolute magnitude. We clamp to zero so callers that
// pass a slightly-skewed timestamp never see a negative bucket.
func FormatRelative(t time.Time, now time.Time) string {
	if t.IsZero() {
		return EmptyMarker
	}
	d := now.Sub(t)
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	}
}

// FormatTimestampOrRelative is the canonical TABLE format for a time cell.
// It renders a relative string if t is within 24h of now, otherwise the
// full RFC3339 UTC timestamp. A zero time renders as EmptyMarker.
//
// This is the form phase-1 writs should use when migrating per-command
// formatting to the shared surface.
func FormatTimestampOrRelative(t time.Time, now time.Time) string {
	if t.IsZero() {
		return EmptyMarker
	}
	d := now.Sub(t)
	if d < 0 {
		d = -d
	}
	if d < 24*time.Hour {
		return FormatRelative(t, now)
	}
	return FormatTimestamp(t)
}

// FormatCount renders a pluralised count for table footers,
// e.g. "4 worlds" / "1 world". Callers supply both forms so irregular
// plurals ("entries"/"entry") remain correct without runtime inflection.
func FormatCount(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}
