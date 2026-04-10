package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cliwrits "github.com/nevinsm/sol/internal/cliapi/writs"
	"github.com/nevinsm/sol/internal/cliformat"
	"github.com/nevinsm/sol/internal/store"
)

func TestWritCreateBlockedInSleepingWorld(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	world := "sleeptest"

	// Create world directory with sleeping=true config.
	worldDir := filepath.Join(dir, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[world]\nsleeping = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reset package-level flag vars.
	createTitle = "test writ"
	createDescription = "test description"
	createPriority = 2
	createLabels = nil
	createKind = ""
	createMetadata = ""

	rootCmd.SetArgs([]string{"writ", "create", "--world", world, "--title", "test writ"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when creating writ in sleeping world")
	}
	errStr := err.Error()
	if !(strings.Contains(errStr, "sleeping") && strings.Contains(errStr, "writ creation blocked")) {
		t.Errorf("expected sleeping/blocked error, got: %v", err)
	}
	if !strings.Contains(errStr, "sol world wake") {
		t.Errorf("expected wake hint in error, got: %v", err)
	}
}

func TestPriorityLabel(t *testing.T) {
	cases := map[int]string{
		1: "high",
		2: "normal",
		3: "low",
		0: "0",
		7: "7",
	}
	for in, want := range cases {
		if got := priorityLabel(in); got != want {
			t.Errorf("priorityLabel(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestOrEmpty(t *testing.T) {
	if got := orEmpty(""); got != cliformat.EmptyMarker {
		t.Errorf("orEmpty empty = %q, want %q", got, cliformat.EmptyMarker)
	}
	if got := orEmpty("nev"); got != "nev" {
		t.Errorf("orEmpty non-empty = %q, want %q", got, "nev")
	}
}

func TestRenderLabelsCell(t *testing.T) {
	if got := renderLabelsCell(nil); got != cliformat.EmptyMarker {
		t.Errorf("renderLabelsCell(nil) = %q, want EmptyMarker", got)
	}
	if got := renderLabelsCell([]string{}); got != cliformat.EmptyMarker {
		t.Errorf("renderLabelsCell(empty) = %q, want EmptyMarker", got)
	}
	if got := renderLabelsCell([]string{"a", "b"}); got != "a, b" {
		t.Errorf("renderLabelsCell = %q, want %q", got, "a, b")
	}
}

func TestRenderCaravanCell(t *testing.T) {
	// Empty membership.
	if got := renderCaravanCell(writMembership{}); got != cliformat.EmptyMarker {
		t.Errorf("empty membership = %q, want EmptyMarker", got)
	}
	// Single caravan with a name.
	single := writMembership{Caravans: []cliwrits.CaravanRef{{ID: "car-1", Name: "refactor"}}}
	if got := renderCaravanCell(single); got != "refactor" {
		t.Errorf("single named = %q, want %q", got, "refactor")
	}
	// Single caravan with no name falls back to the ID.
	unnamed := writMembership{Caravans: []cliwrits.CaravanRef{{ID: "car-2"}}}
	if got := renderCaravanCell(unnamed); got != "car-2" {
		t.Errorf("single unnamed = %q, want %q", got, "car-2")
	}
	// Multiple caravans render first plus +N suffix.
	multi := writMembership{Caravans: []cliwrits.CaravanRef{
		{ID: "car-1", Name: "refactor"},
		{ID: "car-2", Name: "cleanup"},
		{ID: "car-3"},
	}}
	if got := renderCaravanCell(multi); got != "refactor +2" {
		t.Errorf("multi = %q, want %q", got, "refactor +2")
	}
}

func TestBuildWritListItems(t *testing.T) {
	createdAt := time.Date(2026, 4, 10, 0, 8, 30, 0, time.UTC)
	updatedAt := createdAt.Add(1 * time.Hour)
	closedAt := createdAt.Add(2 * time.Hour)

	items := []store.Writ{
		{
			ID:        "sol-1111111111111111",
			Title:     "first",
			Status:    "open",
			Priority:  1,
			Kind:      "code",
			CreatedBy: "autarch",
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
			Labels:    []string{"alpha"},
		},
		{
			ID:        "sol-2222222222222222",
			Title:     "second",
			Status:    "closed",
			Priority:  3,
			Kind:      "analysis",
			CreatedBy: "autarch",
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
			ClosedAt:  &closedAt,
		},
	}
	memberships := map[string]writMembership{
		"sol-1111111111111111": {Caravans: []cliwrits.CaravanRef{{ID: "car-1", Name: "refactor"}}},
	}

	got := buildWritListItems(items, memberships)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}

	// First writ: caravan populated, integer priority preserved.
	if got[0].Priority != 1 {
		t.Errorf("priority = %d, want 1 (integer preserved in JSON)", got[0].Priority)
	}
	if got[0].Kind != "code" {
		t.Errorf("kind = %q, want %q", got[0].Kind, "code")
	}
	if got[0].CreatedAt != cliformat.FormatTimestamp(createdAt) {
		t.Errorf("created_at = %q, want RFC3339 %q", got[0].CreatedAt, cliformat.FormatTimestamp(createdAt))
	}
	if got[0].Caravan == nil || got[0].Caravan.ID != "car-1" || got[0].Caravan.Name != "refactor" {
		t.Errorf("caravan = %+v, want car-1/refactor", got[0].Caravan)
	}

	// Second writ: no caravan, closed_at populated.
	if got[1].Caravan != nil {
		t.Errorf("caravan = %+v, want nil", got[1].Caravan)
	}
	if got[1].ClosedAt == "" {
		t.Errorf("closed_at empty, want RFC3339")
	}

	// Verify the JSON surface uses the documented snake_case keys.
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, want := range []string{
		`"kind":"code"`,
		`"created_at":"2026-04-10T00:08:30Z"`,
		`"caravan":{"id":"car-1","name":"refactor"}`,
		`"priority":1`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON missing %s\nfull: %s", want, s)
		}
	}
	// Second writ's caravan must be present as null (the field is
	// non-omitempty so downstream consumers can rely on it).
	if !strings.Contains(s, `"caravan":null`) {
		t.Errorf("JSON missing null caravan\nfull: %s", s)
	}
}
