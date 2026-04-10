package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/cliformat"
)

// TestFormatQuotaTime verifies the timestamp helper used by
// `sol quota status` renders nil as the empty marker, recent times as
// relative ("Nm ago"), and old times as full RFC3339 UTC — replacing the
// bare "15:04:05" format that previously made LAST USED unreadable without
// surrounding context.
func TestFormatQuotaTime(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		t    *time.Time
		want string
	}{
		{
			name: "nil renders as empty marker",
			t:    nil,
			want: cliformat.EmptyMarker,
		},
		{
			name: "recent renders as relative",
			t:    timePtr(now.Add(-5 * time.Minute)),
			want: "5m ago",
		},
		{
			name: "very recent renders as just now",
			t:    timePtr(now.Add(-10 * time.Second)),
			want: "just now",
		},
		{
			name: "hours ago renders as Nh ago",
			t:    timePtr(now.Add(-3 * time.Hour)),
			want: "3h ago",
		},
		{
			name: "old renders as full RFC3339 UTC with date",
			t:    timePtr(now.Add(-72 * time.Hour)),
			want: "2026-04-07T12:00:00Z",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatQuotaTime(tc.t, now)
			if got != tc.want {
				t.Errorf("formatQuotaTime() = %q, want %q", got, tc.want)
			}
			// Old timestamps must contain a full date — this is the
			// operator-facing regression the writ exists to fix.
			if tc.t != nil && now.Sub(*tc.t) >= 24*time.Hour {
				if !strings.Contains(got, "2026-") {
					t.Errorf("old timestamp %q should contain a full date", got)
				}
			}
		})
	}
}

// TestQuotaStatusLongIsSphereWide guards the documented contract that
// `sol quota status` is sphere-wide and does not honour a --world flag.
// If a future writ adds --world to status, this test should be revisited
// deliberately rather than silently.
func TestQuotaStatusLongIsSphereWide(t *testing.T) {
	if !strings.Contains(quotaStatusCmd.Long, "sphere-wide") {
		t.Errorf("quota status Long help must document sphere-wide scope; got:\n%s", quotaStatusCmd.Long)
	}
	if quotaStatusCmd.Flags().Lookup("world") != nil {
		t.Errorf("quota status must not accept --world flag")
	}
}

func timePtr(t time.Time) *time.Time { return &t }
