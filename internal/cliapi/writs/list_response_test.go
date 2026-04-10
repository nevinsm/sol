package writs

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

func TestWritListItemFromStore(t *testing.T) {
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	closed := now.Add(2 * time.Hour)

	sw := store.Writ{
		ID:          "sol-a1b2c3d4e5f6a7b8",
		Title:       "Test writ",
		Description: "A description",
		Status:      "closed",
		Kind:        "code",
		Priority:    1,
		Assignee:    "Nova",
		ParentID:    "sol-0000000000000001",
		CreatedBy:   "autarch",
		CreatedAt:   now,
		UpdatedAt:   now.Add(time.Hour),
		ClosedAt:    &closed,
		CloseReason: "completed",
		Labels:      []string{"cli", "api"},
		Metadata:    map[string]any{"key": "value"},
	}

	caravan := &CaravanRef{ID: "car-abc123", Name: "test-caravan"}
	item := WritListItemFromStore(sw, caravan)

	if item.ID != sw.ID {
		t.Errorf("ID = %q, want %q", item.ID, sw.ID)
	}
	if item.Title != sw.Title {
		t.Errorf("Title = %q, want %q", item.Title, sw.Title)
	}
	if item.Description != sw.Description {
		t.Errorf("Description = %q, want %q", item.Description, sw.Description)
	}
	if item.Status != "closed" {
		t.Errorf("Status = %q, want %q", item.Status, "closed")
	}
	if item.Kind != "code" {
		t.Errorf("Kind = %q, want %q", item.Kind, "code")
	}
	if item.Priority != 1 {
		t.Errorf("Priority = %d, want 1", item.Priority)
	}
	if item.Assignee != "Nova" {
		t.Errorf("Assignee = %q, want %q", item.Assignee, "Nova")
	}
	if item.ParentID != sw.ParentID {
		t.Errorf("ParentID = %q, want %q", item.ParentID, sw.ParentID)
	}
	if item.CreatedBy != "autarch" {
		t.Errorf("CreatedBy = %q, want %q", item.CreatedBy, "autarch")
	}
	if item.CloseReason != "completed" {
		t.Errorf("CloseReason = %q, want %q", item.CloseReason, "completed")
	}
	if item.ClosedAt == "" {
		t.Error("ClosedAt should not be empty for closed writs")
	}
	if item.Caravan == nil {
		t.Fatal("Caravan should not be nil")
	}
	if item.Caravan.ID != "car-abc123" {
		t.Errorf("Caravan.ID = %q, want %q", item.Caravan.ID, "car-abc123")
	}
	if item.Caravan.Name != "test-caravan" {
		t.Errorf("Caravan.Name = %q, want %q", item.Caravan.Name, "test-caravan")
	}
	if len(item.Labels) != 2 {
		t.Errorf("Labels len = %d, want 2", len(item.Labels))
	}
}

func TestWritListItemFromStoreNilCaravan(t *testing.T) {
	sw := store.Writ{
		ID:        "sol-0000000000000001",
		Title:     "No caravan",
		Status:    "open",
		Kind:      "code",
		Priority:  2,
		CreatedBy: "autarch",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	item := WritListItemFromStore(sw, nil)

	if item.Caravan != nil {
		t.Errorf("Caravan should be nil, got %+v", item.Caravan)
	}
}

func TestWritListItemFromStoreNoClosedAt(t *testing.T) {
	sw := store.Writ{
		ID:        "sol-0000000000000002",
		Title:     "Open writ",
		Status:    "open",
		Kind:      "code",
		Priority:  2,
		CreatedBy: "autarch",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	item := WritListItemFromStore(sw, nil)

	if item.ClosedAt != "" {
		t.Errorf("ClosedAt should be empty, got %q", item.ClosedAt)
	}

	// Verify closed_at is omitted from JSON.
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if _, ok := m["closed_at"]; ok {
		t.Error("closed_at should be omitted from JSON when empty")
	}
}

func TestCaravanRefJSON(t *testing.T) {
	ref := CaravanRef{ID: "car-abc123", Name: "test-caravan"}
	data, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if m["id"] != "car-abc123" {
		t.Errorf("id = %v, want %q", m["id"], "car-abc123")
	}
	if m["name"] != "test-caravan" {
		t.Errorf("name = %v, want %q", m["name"], "test-caravan")
	}
}
