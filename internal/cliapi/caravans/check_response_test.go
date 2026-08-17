package caravans

import (
	"encoding/json"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

func TestNewCheckResponse(t *testing.T) {
	c := &store.Caravan{
		ID:     "car-0000000000000001",
		Name:   "test-caravan",
		Status: "open",
	}
	statuses := []store.CaravanItemStatus{
		{WritID: "sol-0001", World: "alpha", Phase: 0, WritStatus: "closed", Ready: false},
		{WritID: "sol-0002", World: "alpha", Phase: 0, WritStatus: "tethered", Ready: false, Assignee: "Nova"},
		{WritID: "sol-0003", World: "beta", Phase: 1, WritStatus: "open", Ready: true},
	}
	blockedBy := []string{"car-0000000000000099"}

	resp := NewCheckResponse(c, statuses, blockedBy)

	if resp.ID != c.ID {
		t.Errorf("ID = %q, want %q", resp.ID, c.ID)
	}
	if resp.Name != "test-caravan" {
		t.Errorf("Name = %q, want %q", resp.Name, "test-caravan")
	}
	if resp.Status != "open" {
		t.Errorf("Status = %q, want %q", resp.Status, "open")
	}
	if len(resp.BlockedBy) != 1 || resp.BlockedBy[0] != "car-0000000000000099" {
		t.Errorf("BlockedBy = %v, want [car-0000000000000099]", resp.BlockedBy)
	}
	if len(resp.Items) != 3 {
		t.Fatalf("Items len = %d, want 3", len(resp.Items))
	}
	if resp.Items[1].Assignee != "Nova" {
		t.Errorf("Items[1].Assignee = %q, want %q", resp.Items[1].Assignee, "Nova")
	}
}

func TestNewCheckResponseEmpty(t *testing.T) {
	c := &store.Caravan{
		ID:     "car-0000000000000002",
		Name:   "empty",
		Status: "drydock",
	}

	resp := NewCheckResponse(c, nil, nil)

	if resp.Items == nil {
		t.Fatal("Items should be empty slice, not nil")
	}
	if len(resp.Items) != 0 {
		t.Errorf("Items len = %d, want 0", len(resp.Items))
	}
	if resp.BlockedBy != nil {
		t.Errorf("BlockedBy should be nil for omitempty, got %v", resp.BlockedBy)
	}
}

func TestCheckResponseJSONShape(t *testing.T) {
	c := &store.Caravan{
		ID:     "car-0000000000000001",
		Name:   "test",
		Status: "open",
	}
	statuses := []store.CaravanItemStatus{
		{WritID: "sol-0001", World: "alpha", Phase: 0, WritStatus: "open", Ready: true},
	}

	resp := NewCheckResponse(c, statuses, nil)
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify expected top-level keys.
	for _, key := range []string{"id", "name", "status", "items"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing key %q in JSON output", key)
		}
	}
	// blocked_by_caravans should be omitted when nil.
	if _, ok := raw["blocked_by_caravans"]; ok {
		t.Error("blocked_by_caravans should be omitted when nil")
	}

	// Verify item uses writ_status, not status.
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw["items"], &items); err != nil {
		t.Fatalf("Unmarshal items failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if _, ok := items[0]["writ_status"]; !ok {
		t.Error("item should have writ_status key")
	}
	if _, ok := items[0]["status"]; ok {
		t.Error("item should NOT have status key (should be writ_status)")
	}
}

func TestCheckResponseWithVitals(t *testing.T) {
	c := &store.Caravan{
		ID:     "car-0000000000000003",
		Name:   "vitals-caravan",
		Status: "open",
	}
	statuses := []store.CaravanItemStatus{
		{WritID: "sol-0001", World: "alpha", Phase: 0, WritStatus: "closed"},
		{WritID: "sol-0002", World: "alpha", Phase: 0, WritStatus: "open"},
		{WritID: "sol-0003", World: "beta", Phase: 1, WritStatus: "open"},
	}

	resp := NewCheckResponse(c, statuses, nil)

	// sol-0001 has vitals; sol-0002 has a nil map entry (lookup failed or no
	// history — same treatment); sol-0003 has no entry at all.
	vitals := map[string]*store.WritVitals{
		"sol-0001": {
			WritID:       "sol-0001",
			SessionCount: 3,
			InputTokens:  1000,
			OutputTokens: 500,
			CacheTokens:  200,
		},
		"sol-0002": nil,
	}

	resp = resp.WithVitals(vitals)

	if resp.Items[0].TotalTokens != 1700 {
		t.Errorf("Items[0].TotalTokens = %d, want 1700", resp.Items[0].TotalTokens)
	}
	if resp.Items[0].SessionCount != 3 {
		t.Errorf("Items[0].SessionCount = %d, want 3", resp.Items[0].SessionCount)
	}
	if resp.Items[1].TotalTokens != 0 || resp.Items[1].SessionCount != 0 {
		t.Errorf("Items[1] should stay zero for a nil vitals entry, got %+v", resp.Items[1])
	}
	if resp.Items[2].TotalTokens != 0 || resp.Items[2].SessionCount != 0 {
		t.Errorf("Items[2] should stay zero with no vitals entry, got %+v", resp.Items[2])
	}
	if resp.TotalTokens != 1700 {
		t.Errorf("TotalTokens = %d, want 1700", resp.TotalTokens)
	}
	if resp.TotalSessions != 3 {
		t.Errorf("TotalSessions = %d, want 3", resp.TotalSessions)
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if _, ok := raw["total_tokens"]; !ok {
		t.Error("expected total_tokens key when caravan has vitals")
	}
}

func TestCheckResponseWithVitalsEmptyOmitted(t *testing.T) {
	c := &store.Caravan{ID: "car-0000000000000004", Name: "no-vitals", Status: "open"}
	statuses := []store.CaravanItemStatus{
		{WritID: "sol-0001", World: "alpha", Phase: 0, WritStatus: "open"},
	}

	resp := NewCheckResponse(c, statuses, nil).WithVitals(nil)

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if _, ok := raw["total_tokens"]; ok {
		t.Error("total_tokens should be omitted when no item has vitals")
	}
	if _, ok := raw["total_sessions"]; ok {
		t.Error("total_sessions should be omitted when no item has vitals")
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw["items"], &items); err != nil {
		t.Fatalf("Unmarshal items failed: %v", err)
	}
	if _, ok := items[0]["total_tokens"]; ok {
		t.Error("item total_tokens should be omitted with no vitals")
	}
}
