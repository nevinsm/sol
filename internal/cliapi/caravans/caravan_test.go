package caravans

import (
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

func TestFromStoreCaravan(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	c := store.Caravan{
		ID:        "car-0000000000000001",
		Name:      "test-caravan",
		Status:    "open",
		Owner:     "autarch",
		CreatedAt: now,
	}

	items := []store.CaravanItemStatus{
		{WritID: "sol-0001", World: "alpha", Phase: 0, WritStatus: "merged", Ready: false},
		{WritID: "sol-0002", World: "alpha", Phase: 0, WritStatus: "tethered", Ready: false, Assignee: "Nova"},
		{WritID: "sol-0003", World: "beta", Phase: 1, WritStatus: "open", Ready: true},
		{WritID: "sol-0004", World: "beta", Phase: 1, WritStatus: "open", Ready: false},
	}

	result := FromStoreCaravan(c, items)

	if result.ID != c.ID {
		t.Errorf("ID = %q, want %q", result.ID, c.ID)
	}
	if result.Name != "test-caravan" {
		t.Errorf("Name = %q, want %q", result.Name, "test-caravan")
	}
	if result.ItemsTotal != 4 {
		t.Errorf("ItemsTotal = %d, want 4", result.ItemsTotal)
	}
	if result.ItemsMerged != 1 {
		t.Errorf("ItemsMerged = %d, want 1", result.ItemsMerged)
	}
	if result.ItemsInProg != 1 {
		t.Errorf("ItemsInProg = %d, want 1", result.ItemsInProg)
	}
	if result.ItemsReady != 1 {
		t.Errorf("ItemsReady = %d, want 1", result.ItemsReady)
	}
	if result.ItemsBlocked != 1 {
		t.Errorf("ItemsBlocked = %d, want 1", result.ItemsBlocked)
	}
	if len(result.Worlds) != 2 {
		t.Errorf("Worlds len = %d, want 2", len(result.Worlds))
	}
	if len(result.PhaseProgress) != 2 {
		t.Errorf("PhaseProgress len = %d, want 2", len(result.PhaseProgress))
	}
}

func TestFromStoreCaravanEmpty(t *testing.T) {
	c := store.Caravan{
		ID:        "car-0000000000000002",
		Name:      "empty",
		Status:    "drydock",
		Owner:     "autarch",
		CreatedAt: time.Now().UTC(),
	}

	result := FromStoreCaravan(c, nil)

	if result.ItemsTotal != 0 {
		t.Errorf("ItemsTotal = %d, want 0", result.ItemsTotal)
	}
	if result.Worlds == nil {
		t.Fatal("Worlds should be empty slice, not nil")
	}
	if result.PhaseProgress == nil {
		t.Fatal("PhaseProgress should be empty slice, not nil")
	}
}

func TestFromStoreCaravanItem(t *testing.T) {
	item := store.CaravanItemStatus{
		WritID:     "sol-0001",
		World:      "alpha",
		Phase:      1,
		WritStatus: "open",
		Ready:      true,
		Assignee:   "Toast",
	}

	result := FromStoreCaravanItem(item)

	if result.WritID != "sol-0001" {
		t.Errorf("WritID = %q, want %q", result.WritID, "sol-0001")
	}
	if result.Status != "open" {
		t.Errorf("Status = %q, want %q", result.Status, "open")
	}
	if !result.Ready {
		t.Error("Ready = false, want true")
	}
	if result.Assignee != "Toast" {
		t.Errorf("Assignee = %q, want %q", result.Assignee, "Toast")
	}
}
