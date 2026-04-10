package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/session"
)

func TestFilterSessionsByRole(t *testing.T) {
	sessions := []session.SessionInfo{
		{Name: "sol-w-Toast", Role: "outpost"},
		{Name: "sol-w-Envoy", Role: "envoy"},
		{Name: "sol-w-forge", Role: "forge-merge"},
		{Name: "sol-w-Pixie", Role: "outpost"},
		{Name: "sol-w-sentinel", Role: "sentinel"},
	}

	tests := []struct {
		name      string
		role      string
		wantNames []string
	}{
		{
			name:      "empty role returns all",
			role:      "",
			wantNames: []string{"sol-w-Toast", "sol-w-Envoy", "sol-w-forge", "sol-w-Pixie", "sol-w-sentinel"},
		},
		{
			name:      "outpost filter",
			role:      "outpost",
			wantNames: []string{"sol-w-Toast", "sol-w-Pixie"},
		},
		{
			name:      "envoy filter",
			role:      "envoy",
			wantNames: []string{"sol-w-Envoy"},
		},
		{
			name:      "forge-merge filter",
			role:      "forge-merge",
			wantNames: []string{"sol-w-forge"},
		},
		{
			name:      "sentinel filter",
			role:      "sentinel",
			wantNames: []string{"sol-w-sentinel"},
		},
		{
			name:      "unknown role yields empty result",
			role:      "ghost",
			wantNames: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filterSessionsByRole(sessions, tc.role)
			if len(got) != len(tc.wantNames) {
				t.Fatalf("expected %d sessions, got %d", len(tc.wantNames), len(got))
			}
			for i, want := range tc.wantNames {
				if got[i].Name != want {
					t.Errorf("index %d: want %q, got %q", i, want, got[i].Name)
				}
			}
		})
	}
}

func TestRenderSessionList_EmptyMessage(t *testing.T) {
	var buf bytes.Buffer
	renderSessionList(&buf, nil, time.Now())
	if !strings.Contains(buf.String(), "No sessions found") {
		t.Errorf("expected empty message, got %q", buf.String())
	}
}

func TestRenderSessionList_TimestampFormatting(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	sessions := []session.SessionInfo{
		{
			Name:      "sol-w-Recent",
			Role:      "outpost",
			World:     "sol-dev",
			StartedAt: now.Add(-5 * time.Minute), // relative bucket
			Alive:     true,
		},
		{
			Name:      "sol-w-Old",
			Role:      "envoy",
			World:     "sol-dev",
			StartedAt: now.Add(-72 * time.Hour), // RFC3339 bucket
			Alive:     false,
		},
		{
			Name:      "sol-w-Empty",
			Role:      "",
			World:     "",
			StartedAt: time.Time{}, // empty marker
			Alive:     false,
		},
	}

	var buf bytes.Buffer
	renderSessionList(&buf, sessions, now)
	out := buf.String()

	// Header present.
	if !strings.Contains(out, "STARTED") {
		t.Errorf("missing header: %q", out)
	}

	// Recent session uses "5m ago".
	if !strings.Contains(out, "5m ago") {
		t.Errorf("expected relative timestamp '5m ago' in output:\n%s", out)
	}

	// Old session uses RFC3339 UTC; -72h from 2026-04-10 12:00Z is 2026-04-07 12:00Z.
	if !strings.Contains(out, "2026-04-07T12:00:00Z") {
		t.Errorf("expected RFC3339 UTC timestamp for old session:\n%s", out)
	}

	// Old format must NOT appear.
	if strings.Contains(out, "2026-04-10 12:00:00") {
		t.Errorf("legacy local-datetime format leaked into output:\n%s", out)
	}

	// Empty cells use the canonical "-" marker for role/world/timestamp.
	if !strings.Contains(out, "sol-w-Empty") {
		t.Errorf("missing empty session row:\n%s", out)
	}
	// Find the row and check it uses "-".
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "sol-w-Empty") {
			// Should contain at least one "-" cell.
			if !strings.Contains(line, "-") {
				t.Errorf("expected empty marker in row: %q", line)
			}
		}
	}

	// Footer pluralisation: 3 sessions.
	if !strings.Contains(out, "3 sessions") {
		t.Errorf("expected '3 sessions' footer in output:\n%s", out)
	}
}

func TestRenderSessionList_FooterSingular(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	sessions := []session.SessionInfo{
		{
			Name:      "sol-w-Only",
			Role:      "outpost",
			World:     "sol-dev",
			StartedAt: now.Add(-30 * time.Second),
			Alive:     true,
		},
	}
	var buf bytes.Buffer
	renderSessionList(&buf, sessions, now)
	if !strings.Contains(buf.String(), "1 session\n") {
		t.Errorf("expected singular '1 session' footer:\n%s", buf.String())
	}
}
