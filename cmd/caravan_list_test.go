package cmd

import (
	"testing"

	"github.com/nevinsm/sol/internal/cliformat"
	"github.com/nevinsm/sol/internal/store"
)

func TestFormatCaravanWorlds(t *testing.T) {
	tests := []struct {
		name  string
		items []store.CaravanItem
		want  string
	}{
		{
			name:  "empty caravan renders empty marker",
			items: nil,
			want:  cliformat.EmptyMarker,
		},
		{
			name: "single world",
			items: []store.CaravanItem{
				{WritID: "w1", World: "bettr"},
				{WritID: "w2", World: "bettr"},
			},
			want: "bettr",
		},
		{
			name: "two worlds sorted",
			items: []store.CaravanItem{
				{WritID: "w1", World: "diaa"},
				{WritID: "w2", World: "bettr"},
			},
			want: "bettr,diaa",
		},
		{
			name: "three worlds no truncation",
			items: []store.CaravanItem{
				{WritID: "w1", World: "diaa"},
				{WritID: "w2", World: "bettr"},
				{WritID: "w3", World: "sol-dev"},
			},
			want: "bettr,diaa,sol-dev",
		},
		{
			name: "more than three worlds truncated with +N",
			items: []store.CaravanItem{
				{WritID: "w1", World: "diaa"},
				{WritID: "w2", World: "bettr"},
				{WritID: "w3", World: "sol-dev"},
				{WritID: "w4", World: "alpha"},
				{WritID: "w5", World: "zeta"},
			},
			// sorted: alpha, bettr, diaa, sol-dev, zeta -> first 3 + "+2"
			want: "alpha,bettr,diaa+2",
		},
		{
			name: "duplicate worlds collapsed",
			items: []store.CaravanItem{
				{WritID: "w1", World: "bettr"},
				{WritID: "w2", World: "bettr"},
				{WritID: "w3", World: "diaa"},
				{WritID: "w4", World: "diaa"},
			},
			want: "bettr,diaa",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatCaravanWorlds(tc.items)
			if got != tc.want {
				t.Fatalf("formatCaravanWorlds: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestComputeAndFormatCaravanProgress(t *testing.T) {
	items := []store.CaravanItem{
		{WritID: "a", World: "w1", Phase: 0},
		{WritID: "b", World: "w1", Phase: 0},
		{WritID: "c", World: "w1", Phase: 0},
		{WritID: "d", World: "w1", Phase: 1},
		{WritID: "e", World: "w1", Phase: 1},
		{WritID: "f", World: "w1", Phase: 2},
	}
	statuses := []store.CaravanItemStatus{
		{WritID: "a", Phase: 0, WritStatus: "closed"},
		{WritID: "b", Phase: 0, WritStatus: "closed"},
		{WritID: "c", Phase: 0, WritStatus: "closed"},
		{WritID: "d", Phase: 1, WritStatus: "tethered"},
		{WritID: "e", Phase: 1, WritStatus: "open", Ready: true},
		{WritID: "f", Phase: 2, WritStatus: "open", Ready: false},
	}
	progress := computeCaravanPhaseProgress(items, statuses)

	if got := progress[0]; got == nil || got.Total != 3 || got.Merged != 3 {
		t.Fatalf("phase 0: got %+v, want total=3 merged=3", got)
	}
	if got := progress[1]; got == nil || got.Total != 2 || got.InProgress != 1 || got.Ready != 1 {
		t.Fatalf("phase 1: got %+v, want total=2 in_progress=1 ready=1", got)
	}
	if got := progress[2]; got == nil || got.Total != 1 || got.Blocked != 1 {
		t.Fatalf("phase 2: got %+v, want total=1 blocked=1", got)
	}

	want := "p0:3/3 p1:0/2 p2:0/1"
	if got := formatCaravanProgress(progress); got != want {
		t.Fatalf("formatCaravanProgress: got %q, want %q", got, want)
	}
}

func TestFormatCaravanProgressSinglePhaseDropsPrefix(t *testing.T) {
	items := []store.CaravanItem{
		{WritID: "a", Phase: 0},
		{WritID: "b", Phase: 0},
	}
	statuses := []store.CaravanItemStatus{
		{WritID: "a", Phase: 0, WritStatus: "closed"},
		{WritID: "b", Phase: 0, WritStatus: "open", Ready: true},
	}
	progress := computeCaravanPhaseProgress(items, statuses)
	if got, want := formatCaravanProgress(progress), "1/2"; got != want {
		t.Fatalf("single-phase progress: got %q, want %q", got, want)
	}
}

func TestFormatCaravanProgressEmpty(t *testing.T) {
	progress := computeCaravanPhaseProgress(nil, nil)
	if got, want := formatCaravanProgress(progress), "0/0"; got != want {
		t.Fatalf("empty progress: got %q, want %q", got, want)
	}
}

func TestComputeCaravanPhaseProgressMissingStatusCountsAsBlocked(t *testing.T) {
	items := []store.CaravanItem{
		{WritID: "a", Phase: 0},
		{WritID: "b", Phase: 0},
	}
	// Only one status returned (e.g. readiness check failed for second).
	statuses := []store.CaravanItemStatus{
		{WritID: "a", Phase: 0, WritStatus: "closed"},
	}
	progress := computeCaravanPhaseProgress(items, statuses)
	ps := progress[0]
	if ps == nil || ps.Total != 2 || ps.Merged != 1 || ps.Blocked != 1 {
		t.Fatalf("missing status: got %+v, want total=2 merged=1 blocked=1", ps)
	}
}
