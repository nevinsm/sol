package sessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/session"
)

func TestFromSessionInfoSlice(t *testing.T) {
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	created := time.Date(2025, 6, 15, 11, 55, 0, 0, time.UTC)

	infos := []session.SessionInfo{
		{
			Name:      "sol-dev-Nova",
			PID:       1234,
			Role:      "outpost",
			World:     "sol-dev",
			WorkDir:   "/home/user/sol/worktrees/nova",
			StartedAt: now,
			CreatedAt: created,
			Alive:     true,
		},
		{
			Name:      "sol-dev-forge-merge",
			PID:       5678,
			Role:      "forge-merge",
			World:     "sol-dev",
			WorkDir:   "/home/user/sol/repo",
			StartedAt: now.Add(-time.Hour),
			CreatedAt: created.Add(-time.Hour),
			Alive:     false,
		},
	}

	items := FromSessionInfoSlice(infos)

	if len(items) != 2 {
		t.Fatalf("len = %d, want 2", len(items))
	}

	// Verify first item fields.
	if items[0].Name != "sol-dev-Nova" {
		t.Errorf("items[0].Name = %q, want %q", items[0].Name, "sol-dev-Nova")
	}
	if items[0].PID != 1234 {
		t.Errorf("items[0].PID = %d, want 1234", items[0].PID)
	}
	if items[0].Role != "outpost" {
		t.Errorf("items[0].Role = %q, want %q", items[0].Role, "outpost")
	}
	if items[0].World != "sol-dev" {
		t.Errorf("items[0].World = %q, want %q", items[0].World, "sol-dev")
	}
	if items[0].WorkDir != "/home/user/sol/worktrees/nova" {
		t.Errorf("items[0].WorkDir = %q, want %q", items[0].WorkDir, "/home/user/sol/worktrees/nova")
	}
	if !items[0].StartedAt.Equal(now) {
		t.Errorf("items[0].StartedAt = %v, want %v", items[0].StartedAt, now)
	}
	if !items[0].CreatedAt.Equal(created) {
		t.Errorf("items[0].CreatedAt = %v, want %v", items[0].CreatedAt, created)
	}
	if !items[0].Alive {
		t.Error("items[0].Alive = false, want true")
	}

	// Verify second item.
	if items[1].Alive {
		t.Error("items[1].Alive = true, want false")
	}
	if items[1].PID != 5678 {
		t.Errorf("items[1].PID = %d, want 5678", items[1].PID)
	}
}

func TestFromSessionInfoSliceEmpty(t *testing.T) {
	items := FromSessionInfoSlice([]session.SessionInfo{})

	if items == nil {
		t.Fatal("expected non-nil slice for empty input")
	}
	if len(items) != 0 {
		t.Errorf("len = %d, want 0", len(items))
	}

	// Verify JSON renders as [] not null.
	data, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("JSON = %s, want []", data)
	}
}

func TestListItemJSONShape(t *testing.T) {
	// Verify the JSON output of ListItem matches session.SessionInfo's shape.
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	created := time.Date(2025, 6, 15, 11, 55, 0, 0, time.UTC)

	si := session.SessionInfo{
		Name:      "sol-test-Agent1",
		PID:       42,
		Role:      "outpost",
		World:     "test",
		WorkDir:   "/tmp/work",
		StartedAt: now,
		CreatedAt: created,
		Alive:     true,
	}

	items := FromSessionInfoSlice([]session.SessionInfo{si})

	// Marshal both and compare.
	siJSON, err := json.Marshal(si)
	if err != nil {
		t.Fatalf("marshal SessionInfo: %v", err)
	}
	liJSON, err := json.Marshal(items[0])
	if err != nil {
		t.Fatalf("marshal ListItem: %v", err)
	}

	if string(siJSON) != string(liJSON) {
		t.Errorf("JSON mismatch:\n  SessionInfo: %s\n  ListItem:    %s", siJSON, liJSON)
	}
}
