package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// fixedEscalations returns a synthetic set of escalations covering:
//   - a recent one (should render relative age)
//   - an old one (>24h, should render RFC3339 timestamp)
//   - one with empty SourceRef (should render as the empty marker)
func fixedEscalations(now time.Time) []store.Escalation {
	return []store.Escalation{
		{
			ID:          "esc-1111111111111111",
			Severity:    "high",
			Source:      "sol-dev/Toast",
			SourceRef:   "mr:mr-abc123",
			Status:      "open",
			Description: "build failed",
			CreatedAt:   now.Add(-30 * time.Minute),
			UpdatedAt:   now.Add(-30 * time.Minute),
		},
		{
			ID:          "esc-2222222222222222",
			Severity:    "low",
			Source:      "sol-dev/Rigel",
			SourceRef:   "",
			Status:      "resolved",
			Description: "old thing",
			CreatedAt:   now.Add(-72 * time.Hour),
			UpdatedAt:   now.Add(-71 * time.Hour),
		},
	}
}

// TestRenderEscalationTableHeader verifies that the table output includes an
// explicit header row — the central pain point this writ addresses.
func TestRenderEscalationTableHeader(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	escs := fixedEscalations(now)

	var buf bytes.Buffer
	if err := renderEscalationTable(&buf, escs, "", true, now); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()

	// Header row must appear as the first line and contain the canonical columns.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 {
		t.Fatal("no output")
	}
	header := lines[0]
	for _, col := range []string{"ID", "SEVERITY", "STATUS", "SOURCE", "REFERENCE", "AGE", "MESSAGE"} {
		if !strings.Contains(header, col) {
			t.Errorf("header row missing column %q: %q", col, header)
		}
	}
}

// TestRenderEscalationTableNoBracketPadding verifies the severity column is
// rendered as a plain column (no "[high    ]" bracket-padding leak).
func TestRenderEscalationTableNoBracketPadding(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	escs := fixedEscalations(now)

	var buf bytes.Buffer
	if err := renderEscalationTable(&buf, escs, "", true, now); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()

	if strings.Contains(out, "[high") || strings.Contains(out, "[low") {
		t.Errorf("severity column still has bracket padding:\n%s", out)
	}
}

// TestRenderEscalationTableTimestampBuckets verifies that recent rows use a
// relative age and rows older than 24h get the full RFC3339 timestamp.
func TestRenderEscalationTableTimestampBuckets(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	escs := fixedEscalations(now)

	var buf bytes.Buffer
	if err := renderEscalationTable(&buf, escs, "", true, now); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()

	// Recent row: "30m ago" bucket.
	if !strings.Contains(out, "30m ago") {
		t.Errorf("expected recent row to render as '30m ago':\n%s", out)
	}
	// Old row (72h ago): absolute RFC3339 timestamp expected.
	oldStamp := now.Add(-72 * time.Hour).UTC().Format(time.RFC3339)
	if !strings.Contains(out, oldStamp) {
		t.Errorf("expected old row to render RFC3339 timestamp %q:\n%s", oldStamp, out)
	}
}

// TestRenderEscalationTableEmptyMarker verifies that missing source_ref cells
// use cliformat.EmptyMarker rather than an em-dash or empty string.
func TestRenderEscalationTableEmptyMarker(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	escs := fixedEscalations(now)

	var buf bytes.Buffer
	if err := renderEscalationTable(&buf, escs, "", true, now); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()

	// The old em-dash ("—") should no longer appear.
	if strings.Contains(out, "—") {
		t.Errorf("output still contains legacy em-dash marker:\n%s", out)
	}
}

// TestEscalationFooter covers the three filter variants.
func TestEscalationFooter(t *testing.T) {
	cases := []struct {
		name   string
		n      int
		filter string
		all    bool
		want   string
	}{
		{"default open", 3, "", false, "3 open"},
		{"all singular", 1, "", true, "1 escalation"},
		{"all plural", 5, "", true, "5 escalations"},
		{"status resolved", 2, "resolved", false, "2 resolved"},
		{"status filter wins over all", 4, "acknowledged", true, "4 acknowledged"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := escalationFooter(tc.n, tc.filter, tc.all)
			if got != tc.want {
				t.Errorf("escalationFooter(%d, %q, %v) = %q, want %q", tc.n, tc.filter, tc.all, got, tc.want)
			}
		})
	}
}

// TestRenderEscalationTableFooter verifies the count footer appears at the end.
func TestRenderEscalationTableFooter(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	escs := fixedEscalations(now)

	var buf bytes.Buffer
	if err := renderEscalationTable(&buf, escs, "", true, now); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), "2 escalations") {
		t.Errorf("expected footer '2 escalations':\n%s", buf.String())
	}
}

// TestEscalationsToJSON verifies the JSON shape is flat and includes timestamps
// in RFC3339 form without requiring consumers to parse rendered cells.
func TestEscalationsToJSON(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	escs := fixedEscalations(now)
	lastNotified := now.Add(-15 * time.Minute)
	escs[0].LastNotifiedAt = &lastNotified

	out := escalationsToJSON(escs)
	if len(out) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(out))
	}

	// Round-trip through encoding/json to make sure the tags are right.
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, needle := range []string{
		`"id":"esc-1111111111111111"`,
		`"severity":"high"`,
		`"status":"open"`,
		`"source":"sol-dev/Toast"`,
		`"source_ref":"mr:mr-abc123"`,
		`"description":"build failed"`,
		`"created_at":"`,
		`"updated_at":"`,
		`"last_notified_at":"`,
	} {
		if !strings.Contains(s, needle) {
			t.Errorf("missing JSON field %s in output: %s", needle, s)
		}
	}

	// Row without LastNotifiedAt should omit the field (omitempty).
	if strings.Count(s, `"last_notified_at"`) != 1 {
		t.Errorf("expected last_notified_at only on row with value set: %s", s)
	}
}
