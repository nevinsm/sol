package writs

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

func TestFromStoreWrit(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	closed := now.Add(time.Hour)

	sw := store.Writ{
		ID:          "sol-a1b2c3d4e5f6a7b8",
		Title:       "Test writ",
		Description: "A test writ description",
		Status:      "open",
		Kind:        "code",
		Priority:    2,
		Assignee:    "Nova",
		Labels:      []string{"cli", "api"},
		CreatedAt:   now,
		UpdatedAt:   now,
		ClosedAt:    &closed,
	}

	w := FromStoreWrit(sw, "sol-dev", "car-abc123")

	if w.ID != sw.ID {
		t.Errorf("ID = %q, want %q", w.ID, sw.ID)
	}
	if w.Title != sw.Title {
		t.Errorf("Title = %q, want %q", w.Title, sw.Title)
	}
	if w.Status != "open" {
		t.Errorf("Status = %q, want %q", w.Status, "open")
	}
	if w.Kind != "code" {
		t.Errorf("Kind = %q, want %q", w.Kind, "code")
	}
	if w.World != "sol-dev" {
		t.Errorf("World = %q, want %q", w.World, "sol-dev")
	}
	if w.Caravan != "car-abc123" {
		t.Errorf("Caravan = %q, want %q", w.Caravan, "car-abc123")
	}
	if w.ClosedAt == nil || !w.ClosedAt.Equal(closed) {
		t.Errorf("ClosedAt = %v, want %v", w.ClosedAt, closed)
	}
	if len(w.Labels) != 2 {
		t.Errorf("Labels len = %d, want 2", len(w.Labels))
	}
}

func TestFromStoreWritNilLabels(t *testing.T) {
	sw := store.Writ{
		ID:        "sol-0000000000000001",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	w := FromStoreWrit(sw, "test", "")

	if w.Labels == nil {
		t.Fatal("Labels should be empty slice, not nil")
	}
	if len(w.Labels) != 0 {
		t.Errorf("Labels len = %d, want 0", len(w.Labels))
	}

	// Verify JSON output includes empty array, not null.
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	labels, ok := m["labels"].([]any)
	if !ok {
		t.Fatal("labels should be a JSON array")
	}
	if len(labels) != 0 {
		t.Errorf("labels len = %d, want 0", len(labels))
	}
}
