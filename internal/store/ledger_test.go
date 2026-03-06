package store

import (
	"errors"
	"testing"
	"time"
)

func TestWriteAndGetHistory(t *testing.T) {
	s := setupWorld(t)

	start := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 5, 10, 30, 0, 0, time.UTC)

	id, err := s.WriteHistory("Toast", "sol-item01", "cast", "Started work on feature", start, &end)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	h, err := s.GetHistory(id)
	if err != nil {
		t.Fatal(err)
	}
	if h.AgentName != "Toast" {
		t.Fatalf("expected agent_name 'Toast', got %q", h.AgentName)
	}
	if h.WorkItemID != "sol-item01" {
		t.Fatalf("expected work_item_id 'sol-item01', got %q", h.WorkItemID)
	}
	if h.Action != "cast" {
		t.Fatalf("expected action 'cast', got %q", h.Action)
	}
	if h.Summary != "Started work on feature" {
		t.Fatalf("expected summary 'Started work on feature', got %q", h.Summary)
	}
	if !h.StartedAt.Equal(start) {
		t.Fatalf("expected started_at %v, got %v", start, h.StartedAt)
	}
	if h.EndedAt == nil {
		t.Fatal("expected ended_at to be set")
	}
	if !h.EndedAt.Equal(end) {
		t.Fatalf("expected ended_at %v, got %v", end, *h.EndedAt)
	}
}

func TestWriteHistoryNullableFields(t *testing.T) {
	s := setupWorld(t)

	start := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)

	// No work_item_id, no ended_at, no summary.
	id, err := s.WriteHistory("Jasper", "", "respawn", "", start, nil)
	if err != nil {
		t.Fatal(err)
	}

	h, err := s.GetHistory(id)
	if err != nil {
		t.Fatal(err)
	}
	if h.WorkItemID != "" {
		t.Fatalf("expected empty work_item_id, got %q", h.WorkItemID)
	}
	if h.EndedAt != nil {
		t.Fatalf("expected nil ended_at, got %v", h.EndedAt)
	}
	if h.Summary != "" {
		t.Fatalf("expected empty summary, got %q", h.Summary)
	}
}

func TestGetHistoryNotFound(t *testing.T) {
	s := setupWorld(t)

	_, err := s.GetHistory("ah-nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent history")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestListHistory(t *testing.T) {
	s := setupWorld(t)

	start1 := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	start2 := time.Date(2026, 3, 5, 11, 0, 0, 0, time.UTC)
	start3 := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)

	s.WriteHistory("Toast", "sol-item01", "cast", "", start1, nil)
	s.WriteHistory("Toast", "sol-item01", "resolve", "", start2, nil)
	s.WriteHistory("Jasper", "sol-item02", "cast", "", start3, nil)

	// List all for Toast.
	entries, err := s.ListHistory("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for Toast, got %d", len(entries))
	}
	// Verify ordering by started_at ASC.
	if entries[0].Action != "cast" || entries[1].Action != "resolve" {
		t.Fatalf("expected actions [cast, resolve], got [%s, %s]", entries[0].Action, entries[1].Action)
	}

	// List all for Jasper.
	entries, err = s.ListHistory("Jasper")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry for Jasper, got %d", len(entries))
	}

	// List all (empty filter).
	entries, err = s.ListHistory("")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 total entries, got %d", len(entries))
	}
}

func TestEndHistory(t *testing.T) {
	s := setupWorld(t)

	start := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)

	// Write a cast record with no ended_at.
	id, err := s.WriteHistory("Toast", "sol-item01", "cast", "", start, nil)
	if err != nil {
		t.Fatal(err)
	}

	// EndHistory should find and close it.
	closedID, err := s.EndHistory("sol-item01")
	if err != nil {
		t.Fatal(err)
	}
	if closedID != id {
		t.Fatalf("expected closed ID %q, got %q", id, closedID)
	}

	// Verify ended_at is now set.
	h, err := s.GetHistory(id)
	if err != nil {
		t.Fatal(err)
	}
	if h.EndedAt == nil {
		t.Fatal("expected ended_at to be set after EndHistory")
	}

	// Calling EndHistory again should return empty (no open record).
	closedID, err = s.EndHistory("sol-item01")
	if err != nil {
		t.Fatal(err)
	}
	if closedID != "" {
		t.Fatalf("expected empty ID on second EndHistory, got %q", closedID)
	}
}

func TestEndHistoryNoRecord(t *testing.T) {
	s := setupWorld(t)

	// EndHistory with no matching record should return empty, no error.
	id, err := s.EndHistory("sol-nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if id != "" {
		t.Fatalf("expected empty ID, got %q", id)
	}
}

func TestEndHistoryClosesLatest(t *testing.T) {
	s := setupWorld(t)

	start1 := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	start2 := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)

	// Two cast records for the same work item (re-cast scenario).
	id1, _ := s.WriteHistory("Toast", "sol-item01", "cast", "", start1, nil)
	id2, _ := s.WriteHistory("Jasper", "sol-item01", "cast", "", start2, nil)

	// EndHistory should close the latest one (by started_at DESC).
	closedID, err := s.EndHistory("sol-item01")
	if err != nil {
		t.Fatal(err)
	}
	if closedID != id2 {
		t.Fatalf("expected latest record %q closed, got %q", id2, closedID)
	}

	// First record should still be open.
	h1, _ := s.GetHistory(id1)
	if h1.EndedAt != nil {
		t.Fatal("expected first record to still be open")
	}
}

func TestWriteTokenUsage(t *testing.T) {
	s := setupWorld(t)

	start := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	histID, err := s.WriteHistory("Toast", "sol-item01", "cast", "", start, nil)
	if err != nil {
		t.Fatal(err)
	}

	tuID, err := s.WriteTokenUsage(histID, "claude-sonnet-4-6", 1000, 500, 200, 100)
	if err != nil {
		t.Fatal(err)
	}
	if tuID == "" {
		t.Fatal("expected non-empty token usage ID")
	}
}

func TestAggregateTokens(t *testing.T) {
	s := setupWorld(t)

	start1 := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	start2 := time.Date(2026, 3, 5, 11, 0, 0, 0, time.UTC)
	start3 := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)

	h1, _ := s.WriteHistory("Toast", "sol-item01", "cast", "", start1, nil)
	h2, _ := s.WriteHistory("Toast", "sol-item01", "resolve", "", start2, nil)
	h3, _ := s.WriteHistory("Jasper", "sol-item02", "cast", "", start3, nil)

	// Toast uses sonnet in both sessions.
	s.WriteTokenUsage(h1, "claude-sonnet-4-6", 1000, 500, 200, 100)
	s.WriteTokenUsage(h2, "claude-sonnet-4-6", 2000, 800, 300, 50)
	// Toast also uses opus in one session.
	s.WriteTokenUsage(h1, "claude-opus-4-6", 500, 200, 0, 0)
	// Jasper uses sonnet.
	s.WriteTokenUsage(h3, "claude-sonnet-4-6", 3000, 1000, 500, 200)

	// Aggregate for Toast.
	summaries, err := s.AggregateTokens("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 model summaries for Toast, got %d", len(summaries))
	}

	// Results ordered by model name.
	opus := summaries[0]
	sonnet := summaries[1]

	if opus.Model != "claude-opus-4-6" {
		t.Fatalf("expected first model 'claude-opus-4-6', got %q", opus.Model)
	}
	if opus.InputTokens != 500 || opus.OutputTokens != 200 {
		t.Fatalf("opus tokens: input=%d output=%d, expected 500/200", opus.InputTokens, opus.OutputTokens)
	}

	if sonnet.Model != "claude-sonnet-4-6" {
		t.Fatalf("expected second model 'claude-sonnet-4-6', got %q", sonnet.Model)
	}
	if sonnet.InputTokens != 3000 {
		t.Fatalf("sonnet input_tokens=%d, expected 3000", sonnet.InputTokens)
	}
	if sonnet.OutputTokens != 1300 {
		t.Fatalf("sonnet output_tokens=%d, expected 1300", sonnet.OutputTokens)
	}
	if sonnet.CacheReadTokens != 500 {
		t.Fatalf("sonnet cache_read_tokens=%d, expected 500", sonnet.CacheReadTokens)
	}
	if sonnet.CacheCreationTokens != 150 {
		t.Fatalf("sonnet cache_creation_tokens=%d, expected 150", sonnet.CacheCreationTokens)
	}

	// Aggregate for Jasper.
	summaries, err = s.AggregateTokens("Jasper")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 model summary for Jasper, got %d", len(summaries))
	}
	if summaries[0].InputTokens != 3000 {
		t.Fatalf("Jasper input_tokens=%d, expected 3000", summaries[0].InputTokens)
	}

	// Aggregate for nonexistent agent returns empty.
	summaries, err = s.AggregateTokens("Nobody")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected 0 summaries for Nobody, got %d", len(summaries))
	}
}
