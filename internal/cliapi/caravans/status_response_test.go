package caravans

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

func TestFromStoreCaravanSummary(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	c := store.Caravan{
		ID:        "car-0000000000000001",
		Name:      "test",
		Status:    "open",
		Owner:     "autarch",
		CreatedAt: now,
	}

	s := FromStoreCaravanSummary(c)

	if s.ID != c.ID {
		t.Errorf("ID = %q, want %q", s.ID, c.ID)
	}
	if s.Name != c.Name {
		t.Errorf("Name = %q, want %q", s.Name, c.Name)
	}
	if s.Status != c.Status {
		t.Errorf("Status = %q, want %q", s.Status, c.Status)
	}
	if s.Owner != c.Owner {
		t.Errorf("Owner = %q, want %q", s.Owner, c.Owner)
	}
	if !s.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", s.CreatedAt, now)
	}
	if s.ClosedAt != nil {
		t.Errorf("ClosedAt should be nil, got %v", s.ClosedAt)
	}
}

func TestFromStoreCaravanSummaryWithClosedAt(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	closedAt := now.Add(time.Hour)
	c := store.Caravan{
		ID:        "car-0000000000000001",
		Name:      "done",
		Status:    "closed",
		Owner:     "autarch",
		CreatedAt: now,
		ClosedAt:  &closedAt,
	}

	s := FromStoreCaravanSummary(c)

	if s.ClosedAt == nil {
		t.Fatal("ClosedAt should not be nil")
	}
	if !s.ClosedAt.Equal(closedAt) {
		t.Errorf("ClosedAt = %v, want %v", *s.ClosedAt, closedAt)
	}
}

func TestCaravanSummaryJSONShape(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	s := CaravanSummary{
		ID:        "car-0000000000000001",
		Name:      "test",
		Status:    "open",
		Owner:     "autarch",
		CreatedAt: now,
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	for _, key := range []string{"id", "name", "status", "owner", "created_at"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing key %q", key)
		}
	}
	if _, ok := raw["closed_at"]; ok {
		t.Error("closed_at should be omitted when nil")
	}
}
