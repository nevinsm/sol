package cmd

import (
	"errors"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// fakeWritLookup implements writStatusLookup for outcome inference tests
// without touching the real world store.
type fakeWritLookup struct {
	writs map[string]*store.Writ
	err   error
}

func (f *fakeWritLookup) GetWrit(id string) (*store.Writ, error) {
	if f.err != nil {
		return nil, f.err
	}
	w, ok := f.writs[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return w, nil
}

func TestInferOutcome(t *testing.T) {
	start := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	end := start.Add(15 * time.Minute)

	tests := []struct {
		name   string
		entry  store.HistoryEntry
		lookup writStatusLookup
		want   string
	}{
		{
			name: "running when ended_at is nil",
			entry: store.HistoryEntry{
				ID:        "ah-1",
				AgentName: "Toast",
				WritID:    "sol-aaaaaaaaaaaaaaaa",
				Action:    "cast",
				StartedAt: start,
				EndedAt:   nil,
			},
			lookup: &fakeWritLookup{},
			want:   historyOutcomeRunning,
		},
		{
			name: "running ignores writ status entirely",
			entry: store.HistoryEntry{
				ID:        "ah-2",
				AgentName: "Toast",
				WritID:    "sol-aaaaaaaaaaaaaaaa",
				Action:    "cast",
				StartedAt: start,
				EndedAt:   nil,
			},
			lookup: &fakeWritLookup{
				writs: map[string]*store.Writ{
					"sol-aaaaaaaaaaaaaaaa": {ID: "sol-aaaaaaaaaaaaaaaa", Status: "closed"},
				},
			},
			want: historyOutcomeRunning,
		},
		{
			name: "done when ended and no linked writ",
			entry: store.HistoryEntry{
				ID:        "ah-3",
				AgentName: "Toast",
				Action:    "session",
				StartedAt: start,
				EndedAt:   &end,
			},
			lookup: &fakeWritLookup{},
			want:   historyOutcomeDone,
		},
		{
			name: "done when ended and writ is done (terminal)",
			entry: store.HistoryEntry{
				ID:        "ah-4",
				AgentName: "Toast",
				WritID:    "sol-bbbbbbbbbbbbbbbb",
				Action:    "cast",
				StartedAt: start,
				EndedAt:   &end,
			},
			lookup: &fakeWritLookup{
				writs: map[string]*store.Writ{
					"sol-bbbbbbbbbbbbbbbb": {ID: "sol-bbbbbbbbbbbbbbbb", Status: "done"},
				},
			},
			want: historyOutcomeDone,
		},
		{
			name: "done when ended and writ is closed (terminal)",
			entry: store.HistoryEntry{
				ID:        "ah-5",
				AgentName: "Toast",
				WritID:    "sol-cccccccccccccccc",
				Action:    "cast",
				StartedAt: start,
				EndedAt:   &end,
			},
			lookup: &fakeWritLookup{
				writs: map[string]*store.Writ{
					"sol-cccccccccccccccc": {ID: "sol-cccccccccccccccc", Status: "closed"},
				},
			},
			want: historyOutcomeDone,
		},
		{
			name: "done when ended and writ record was deleted",
			entry: store.HistoryEntry{
				ID:        "ah-6",
				AgentName: "Toast",
				WritID:    "sol-dddddddddddddddd",
				Action:    "cast",
				StartedAt: start,
				EndedAt:   &end,
			},
			// lookup returns ErrNotFound for any id not in map.
			lookup: &fakeWritLookup{},
			want:   historyOutcomeDone,
		},
		{
			name: "unknown when ended and writ still open (handoff / escalation)",
			entry: store.HistoryEntry{
				ID:        "ah-7",
				AgentName: "Toast",
				WritID:    "sol-eeeeeeeeeeeeeeee",
				Action:    "cast",
				StartedAt: start,
				EndedAt:   &end,
			},
			lookup: &fakeWritLookup{
				writs: map[string]*store.Writ{
					"sol-eeeeeeeeeeeeeeee": {ID: "sol-eeeeeeeeeeeeeeee", Status: "open"},
				},
			},
			want: historyOutcomeUnknown,
		},
		{
			name: "unknown when ended and writ is tethered (not terminal)",
			entry: store.HistoryEntry{
				ID:        "ah-8",
				AgentName: "Toast",
				WritID:    "sol-ffffffffffffffff",
				Action:    "cast",
				StartedAt: start,
				EndedAt:   &end,
			},
			lookup: &fakeWritLookup{
				writs: map[string]*store.Writ{
					"sol-ffffffffffffffff": {ID: "sol-ffffffffffffffff", Status: "tethered"},
				},
			},
			want: historyOutcomeUnknown,
		},
		{
			name: "unknown when writ lookup errors (non-NotFound)",
			entry: store.HistoryEntry{
				ID:        "ah-9",
				AgentName: "Toast",
				WritID:    "sol-0000000000000000",
				Action:    "cast",
				StartedAt: start,
				EndedAt:   &end,
			},
			lookup: &fakeWritLookup{err: errors.New("database locked")},
			want:   historyOutcomeUnknown,
		},
		{
			name: "unknown when lookup is nil and a writ is linked",
			entry: store.HistoryEntry{
				ID:        "ah-10",
				AgentName: "Toast",
				WritID:    "sol-1111111111111111",
				Action:    "cast",
				StartedAt: start,
				EndedAt:   &end,
			},
			lookup: nil,
			want:   historyOutcomeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferOutcome(tt.entry, tt.lookup)
			if got != tt.want {
				t.Errorf("inferOutcome() = %q, want %q", got, tt.want)
			}
		})
	}
}
