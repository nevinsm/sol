package store

import (
	"errors"
	"testing"
	"time"
)

func TestWriteAndGetHistory(t *testing.T) {
	t.Parallel()
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
	if h.WritID != "sol-item01" {
		t.Fatalf("expected writ_id 'sol-item01', got %q", h.WritID)
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
	t.Parallel()
	s := setupWorld(t)

	start := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)

	// No writ_id, no ended_at, no summary.
	id, err := s.WriteHistory("Jasper", "", "respawn", "", start, nil)
	if err != nil {
		t.Fatal(err)
	}

	h, err := s.GetHistory(id)
	if err != nil {
		t.Fatal(err)
	}
	if h.WritID != "" {
		t.Fatalf("expected empty writ_id, got %q", h.WritID)
	}
	if h.EndedAt != nil {
		t.Fatalf("expected nil ended_at, got %v", h.EndedAt)
	}
	if h.Summary != "" {
		t.Fatalf("expected empty summary, got %q", h.Summary)
	}
}

func TestGetHistoryNotFound(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	s := setupWorld(t)

	start1 := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	start2 := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)

	// Two cast records for the same writ (re-cast scenario).
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
	t.Parallel()
	s := setupWorld(t)

	start := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	histID, err := s.WriteHistory("Toast", "sol-item01", "cast", "", start, nil)
	if err != nil {
		t.Fatal(err)
	}

	tuID, err := s.WriteTokenUsage(histID, "claude-sonnet-4-6", 1000, 500, 200, 100, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if tuID == "" {
		t.Fatal("expected non-empty token usage ID")
	}
}

func TestTokensForHistory(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	start := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	histID, _ := s.WriteHistory("Toast", "sol-item01", "cast", "", start, nil)

	// No tokens yet — should return nil.
	ts, err := s.TokensForHistory(histID)
	if err != nil {
		t.Fatal(err)
	}
	if ts != nil {
		t.Fatal("expected nil for history with no tokens")
	}

	// Add some token usage.
	s.WriteTokenUsage(histID, "claude-sonnet-4-6", 1000, 500, 200, 100, nil, nil, "")
	s.WriteTokenUsage(histID, "claude-opus-4-6", 2000, 800, 300, 50, nil, nil, "")

	ts, err = s.TokensForHistory(histID)
	if err != nil {
		t.Fatal(err)
	}
	if ts == nil {
		t.Fatal("expected non-nil token summary")
	}
	if ts.InputTokens != 3000 {
		t.Fatalf("expected input 3000, got %d", ts.InputTokens)
	}
	if ts.OutputTokens != 1300 {
		t.Fatalf("expected output 1300, got %d", ts.OutputTokens)
	}
	if ts.CacheReadTokens != 500 {
		t.Fatalf("expected cache_read 500, got %d", ts.CacheReadTokens)
	}
	if ts.CacheCreationTokens != 150 {
		t.Fatalf("expected cache_creation 150, got %d", ts.CacheCreationTokens)
	}
}

func TestAggregateTokens(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	start1 := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	start2 := time.Date(2026, 3, 5, 11, 0, 0, 0, time.UTC)
	start3 := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)

	h1, _ := s.WriteHistory("Toast", "sol-item01", "cast", "", start1, nil)
	h2, _ := s.WriteHistory("Toast", "sol-item01", "resolve", "", start2, nil)
	h3, _ := s.WriteHistory("Jasper", "sol-item02", "cast", "", start3, nil)

	// Toast uses sonnet in both sessions.
	s.WriteTokenUsage(h1, "claude-sonnet-4-6", 1000, 500, 200, 100, nil, nil, "")
	s.WriteTokenUsage(h2, "claude-sonnet-4-6", 2000, 800, 300, 50, nil, nil, "")
	// Toast also uses opus in one session.
	s.WriteTokenUsage(h1, "claude-opus-4-6", 500, 200, 0, 0, nil, nil, "")
	// Jasper uses sonnet.
	s.WriteTokenUsage(h3, "claude-sonnet-4-6", 3000, 1000, 500, 200, nil, nil, "")

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

func TestTokensForWrit(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	start1 := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	start2 := time.Date(2026, 3, 5, 11, 0, 0, 0, time.UTC)
	start3 := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)

	h1, _ := s.WriteHistory("Toast", "sol-item01", "cast", "", start1, nil)
	h2, _ := s.WriteHistory("Jasper", "sol-item01", "cast", "", start2, nil)
	h3, _ := s.WriteHistory("Toast", "sol-item02", "cast", "", start3, nil)

	// Both agents use sonnet on sol-item01.
	s.WriteTokenUsage(h1, "claude-sonnet-4-6", 1000, 500, 200, 100, nil, nil, "")
	s.WriteTokenUsage(h2, "claude-sonnet-4-6", 2000, 800, 300, 50, nil, nil, "")
	// Toast also uses opus on sol-item01.
	s.WriteTokenUsage(h1, "claude-opus-4-6", 500, 200, 0, 0, nil, nil, "")
	// Toast uses sonnet on sol-item02 (should not appear).
	s.WriteTokenUsage(h3, "claude-sonnet-4-6", 9000, 4000, 1000, 500, nil, nil, "")

	summaries, err := s.TokensForWrit("sol-item01")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 model summaries, got %d", len(summaries))
	}

	// Ordered by model name.
	opus := summaries[0]
	sonnet := summaries[1]

	if opus.Model != "claude-opus-4-6" {
		t.Fatalf("expected opus model, got %q", opus.Model)
	}
	if opus.InputTokens != 500 || opus.OutputTokens != 200 {
		t.Fatalf("opus: input=%d output=%d, expected 500/200", opus.InputTokens, opus.OutputTokens)
	}

	if sonnet.Model != "claude-sonnet-4-6" {
		t.Fatalf("expected sonnet model, got %q", sonnet.Model)
	}
	if sonnet.InputTokens != 3000 {
		t.Fatalf("sonnet input=%d, expected 3000", sonnet.InputTokens)
	}
	if sonnet.OutputTokens != 1300 {
		t.Fatalf("sonnet output=%d, expected 1300", sonnet.OutputTokens)
	}

	// Nonexistent writ returns empty.
	summaries, err = s.TokensForWrit("sol-nonexist")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected 0 summaries for nonexistent writ, got %d", len(summaries))
	}
}

func TestTokensForWorld(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	start1 := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	start2 := time.Date(2026, 3, 5, 11, 0, 0, 0, time.UTC)

	h1, _ := s.WriteHistory("Toast", "sol-item01", "cast", "", start1, nil)
	h2, _ := s.WriteHistory("Jasper", "sol-item02", "cast", "", start2, nil)

	s.WriteTokenUsage(h1, "claude-sonnet-4-6", 1000, 500, 200, 100, nil, nil, "")
	s.WriteTokenUsage(h1, "claude-opus-4-6", 500, 200, 0, 0, nil, nil, "")
	s.WriteTokenUsage(h2, "claude-sonnet-4-6", 3000, 1000, 500, 200, nil, nil, "")

	summaries, err := s.TokensForWorld()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 model summaries, got %d", len(summaries))
	}

	opus := summaries[0]
	sonnet := summaries[1]

	if opus.Model != "claude-opus-4-6" {
		t.Fatalf("expected opus, got %q", opus.Model)
	}
	if opus.InputTokens != 500 {
		t.Fatalf("opus input=%d, expected 500", opus.InputTokens)
	}

	if sonnet.Model != "claude-sonnet-4-6" {
		t.Fatalf("expected sonnet, got %q", sonnet.Model)
	}
	// 1000 + 3000 = 4000
	if sonnet.InputTokens != 4000 {
		t.Fatalf("sonnet input=%d, expected 4000", sonnet.InputTokens)
	}
	if sonnet.OutputTokens != 1500 {
		t.Fatalf("sonnet output=%d, expected 1500", sonnet.OutputTokens)
	}
}

func TestTokensForWorldEmpty(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	summaries, err := s.TokensForWorld()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected 0 summaries for empty world, got %d", len(summaries))
	}
}

func TestTokensByWritForAgent(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	start1 := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	start2 := time.Date(2026, 3, 5, 11, 0, 0, 0, time.UTC)
	start3 := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)

	h1, _ := s.WriteHistory("Toast", "sol-item01", "cast", "", start1, nil)
	h2, _ := s.WriteHistory("Toast", "sol-item02", "cast", "", start2, nil)
	h3, _ := s.WriteHistory("Jasper", "sol-item01", "cast", "", start3, nil)

	s.WriteTokenUsage(h1, "claude-sonnet-4-6", 1000, 500, 200, 100, nil, nil, "")
	s.WriteTokenUsage(h1, "claude-opus-4-6", 500, 200, 0, 0, nil, nil, "")
	s.WriteTokenUsage(h2, "claude-sonnet-4-6", 2000, 800, 300, 50, nil, nil, "")
	// Jasper's tokens should not appear in Toast's results.
	s.WriteTokenUsage(h3, "claude-sonnet-4-6", 9000, 4000, 1000, 500, nil, nil, "")

	result, err := s.TokensByWritForAgent("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 writs, got %d", len(result))
	}

	// Check sol-item01 (should have opus + sonnet).
	item01 := result["sol-item01"]
	if len(item01) != 2 {
		t.Fatalf("expected 2 models for sol-item01, got %d", len(item01))
	}
	if item01[0].Model != "claude-opus-4-6" {
		t.Fatalf("expected opus first, got %q", item01[0].Model)
	}
	if item01[1].Model != "claude-sonnet-4-6" {
		t.Fatalf("expected sonnet second, got %q", item01[1].Model)
	}
	if item01[1].InputTokens != 1000 {
		t.Fatalf("sol-item01 sonnet input=%d, expected 1000", item01[1].InputTokens)
	}

	// Check sol-item02 (should have only sonnet).
	item02 := result["sol-item02"]
	if len(item02) != 1 {
		t.Fatalf("expected 1 model for sol-item02, got %d", len(item02))
	}
	if item02[0].InputTokens != 2000 {
		t.Fatalf("sol-item02 input=%d, expected 2000", item02[0].InputTokens)
	}

	// Nonexistent agent returns empty map.
	result, err = s.TokensByWritForAgent("Nobody")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty map for Nobody, got %d entries", len(result))
	}
}

func TestTokensByWritForAgentNoWrit(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	// History entry without a writ_id.
	start := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	h, _ := s.WriteHistory("Toast", "", "respawn", "", start, nil)
	s.WriteTokenUsage(h, "claude-sonnet-4-6", 500, 200, 0, 0, nil, nil, "")

	result, err := s.TokensByWritForAgent("Toast")
	if err != nil {
		t.Fatal(err)
	}
	// Entries without a writ_id should still appear, keyed by empty string.
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	noWrit := result[""]
	if len(noWrit) != 1 {
		t.Fatalf("expected 1 model for no-writ entry, got %d", len(noWrit))
	}
	if noWrit[0].InputTokens != 500 {
		t.Fatalf("no-writ input=%d, expected 500", noWrit[0].InputTokens)
	}
}

func TestTokensSince(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	start1 := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	start2 := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	start3 := time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC)

	h1, _ := s.WriteHistory("Toast", "sol-item01", "cast", "", start1, nil)
	h2, _ := s.WriteHistory("Toast", "sol-item01", "resolve", "", start2, nil)
	h3, _ := s.WriteHistory("Jasper", "sol-item02", "cast", "", start3, nil)

	s.WriteTokenUsage(h1, "claude-sonnet-4-6", 1000, 500, 200, 100, nil, nil, "")
	s.WriteTokenUsage(h2, "claude-sonnet-4-6", 2000, 800, 300, 50, nil, nil, "")
	s.WriteTokenUsage(h3, "claude-sonnet-4-6", 3000, 1000, 500, 200, nil, nil, "")

	// Since 12:00 — should include h2 and h3 but not h1.
	since := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	summaries, err := s.TokensSince(since)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 model summary, got %d", len(summaries))
	}
	if summaries[0].InputTokens != 5000 {
		t.Fatalf("input=%d, expected 5000 (2000+3000)", summaries[0].InputTokens)
	}
	if summaries[0].OutputTokens != 1800 {
		t.Fatalf("output=%d, expected 1800 (800+1000)", summaries[0].OutputTokens)
	}

	// Since future time — should return empty.
	future := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	summaries, err = s.TokensSince(future)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected 0 summaries for future since, got %d", len(summaries))
	}

	// Since before all records — should return everything.
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	summaries, err = s.TokensSince(past)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 model summary, got %d", len(summaries))
	}
	if summaries[0].InputTokens != 6000 {
		t.Fatalf("input=%d, expected 6000 (1000+2000+3000)", summaries[0].InputTokens)
	}
}

func TestTokensByAgentForWorld(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	start1 := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	start2 := time.Date(2026, 3, 5, 11, 0, 0, 0, time.UTC)
	start3 := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)

	h1, _ := s.WriteHistory("Toast", "sol-item01", "cast", "", start1, nil)
	h2, _ := s.WriteHistory("Toast", "sol-item02", "cast", "", start2, nil)
	h3, _ := s.WriteHistory("Jasper", "sol-item03", "cast", "", start3, nil)

	s.WriteTokenUsage(h1, "claude-sonnet-4-6", 1000, 500, 200, 100, nil, nil, "")
	s.WriteTokenUsage(h2, "claude-sonnet-4-6", 2000, 800, 300, 50, nil, nil, "")
	s.WriteTokenUsage(h3, "claude-sonnet-4-6", 3000, 1000, 500, 200, nil, nil, "")

	summaries, err := s.TokensByAgentForWorld()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(summaries))
	}

	// Results ordered by agent name.
	jasper := summaries[0]
	toast := summaries[1]

	if jasper.AgentName != "Jasper" {
		t.Fatalf("expected Jasper first, got %q", jasper.AgentName)
	}
	if jasper.WritCount != 1 {
		t.Fatalf("expected 1 writ for Jasper, got %d", jasper.WritCount)
	}
	if jasper.InputTokens != 3000 {
		t.Fatalf("Jasper input=%d, expected 3000", jasper.InputTokens)
	}

	if toast.AgentName != "Toast" {
		t.Fatalf("expected Toast second, got %q", toast.AgentName)
	}
	if toast.WritCount != 2 {
		t.Fatalf("expected 2 writs for Toast, got %d", toast.WritCount)
	}
	if toast.InputTokens != 3000 {
		t.Fatalf("Toast input=%d, expected 3000", toast.InputTokens)
	}
}

func TestTokensByAgentForWorldEmpty(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	summaries, err := s.TokensByAgentForWorld()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected 0 summaries, got %d", len(summaries))
	}
}

func TestTokensByAgentSince(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	start1 := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	start2 := time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC)

	h1, _ := s.WriteHistory("Toast", "sol-item01", "cast", "", start1, nil)
	h2, _ := s.WriteHistory("Jasper", "sol-item02", "cast", "", start2, nil)

	s.WriteTokenUsage(h1, "claude-sonnet-4-6", 1000, 500, 200, 100, nil, nil, "")
	s.WriteTokenUsage(h2, "claude-sonnet-4-6", 3000, 1000, 500, 200, nil, nil, "")

	// Since 12:00 — should only include Jasper.
	since := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	summaries, err := s.TokensByAgentSince(since)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(summaries))
	}
	if summaries[0].AgentName != "Jasper" {
		t.Fatalf("expected Jasper, got %q", summaries[0].AgentName)
	}
	if summaries[0].InputTokens != 3000 {
		t.Fatalf("input=%d, expected 3000", summaries[0].InputTokens)
	}
}

func TestTokensByWritForAgentSince(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	start1 := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	start2 := time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC)

	h1, _ := s.WriteHistory("Toast", "sol-item01", "cast", "", start1, nil)
	h2, _ := s.WriteHistory("Toast", "sol-item02", "cast", "", start2, nil)

	s.WriteTokenUsage(h1, "claude-sonnet-4-6", 1000, 500, 200, 100, nil, nil, "")
	s.WriteTokenUsage(h2, "claude-sonnet-4-6", 3000, 1000, 500, 200, nil, nil, "")

	// Since 12:00 — should only include sol-item02.
	since := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	result, err := s.TokensByWritForAgentSince("Toast", since)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 writ, got %d", len(result))
	}
	if _, ok := result["sol-item02"]; !ok {
		t.Fatal("expected sol-item02 in result")
	}
	if result["sol-item02"][0].InputTokens != 3000 {
		t.Fatalf("input=%d, expected 3000", result["sol-item02"][0].InputTokens)
	}
}

func TestWorldTokenMeta(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	start1 := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	start2 := time.Date(2026, 3, 5, 11, 0, 0, 0, time.UTC)
	start3 := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)

	h1, _ := s.WriteHistory("Toast", "sol-item01", "cast", "", start1, nil)
	h2, _ := s.WriteHistory("Toast", "sol-item02", "cast", "", start2, nil)
	h3, _ := s.WriteHistory("Jasper", "sol-item01", "cast", "", start3, nil)

	s.WriteTokenUsage(h1, "claude-sonnet-4-6", 1000, 500, 200, 100, nil, nil, "")
	s.WriteTokenUsage(h2, "claude-sonnet-4-6", 2000, 800, 300, 50, nil, nil, "")
	s.WriteTokenUsage(h3, "claude-sonnet-4-6", 3000, 1000, 500, 200, nil, nil, "")

	agents, writs, err := s.WorldTokenMeta()
	if err != nil {
		t.Fatal(err)
	}
	if agents != 2 {
		t.Fatalf("expected 2 agents, got %d", agents)
	}
	if writs != 2 {
		t.Fatalf("expected 2 writs, got %d", writs)
	}
}

func TestWorldTokenMetaEmpty(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	agents, writs, err := s.WorldTokenMeta()
	if err != nil {
		t.Fatal(err)
	}
	if agents != 0 || writs != 0 {
		t.Fatalf("expected 0/0, got %d/%d", agents, writs)
	}
}

func TestWorldTokenMetaSince(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	start1 := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	start2 := time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC)

	h1, _ := s.WriteHistory("Toast", "sol-item01", "cast", "", start1, nil)
	h2, _ := s.WriteHistory("Jasper", "sol-item02", "cast", "", start2, nil)

	s.WriteTokenUsage(h1, "claude-sonnet-4-6", 1000, 500, 200, 100, nil, nil, "")
	s.WriteTokenUsage(h2, "claude-sonnet-4-6", 3000, 1000, 500, 200, nil, nil, "")

	since := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	agents, writs, err := s.WorldTokenMetaSince(since)
	if err != nil {
		t.Fatal(err)
	}
	if agents != 1 {
		t.Fatalf("expected 1 agent, got %d", agents)
	}
	if writs != 1 {
		t.Fatalf("expected 1 writ, got %d", writs)
	}
}

func TestTokensForWritSince(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	start1 := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	start2 := time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC)

	h1, _ := s.WriteHistory("Toast", "sol-item01", "cast", "", start1, nil)
	h2, _ := s.WriteHistory("Jasper", "sol-item01", "cast", "", start2, nil)

	s.WriteTokenUsage(h1, "claude-sonnet-4-6", 1000, 500, 200, 100, nil, nil, "")
	s.WriteTokenUsage(h2, "claude-sonnet-4-6", 3000, 1000, 500, 200, nil, nil, "")

	// Since 12:00 — should only include h2.
	since := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	summaries, err := s.TokensForWritSince("sol-item01", since)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 model summary, got %d", len(summaries))
	}
	if summaries[0].InputTokens != 3000 {
		t.Fatalf("input=%d, expected 3000", summaries[0].InputTokens)
	}
}

func TestMergeStatsForAgent(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	// Create writs (FK dependency for merge_requests).
	item1, _ := s.CreateWrit("Item 1", "", "autarch", 2, nil)
	item2, _ := s.CreateWrit("Item 2", "", "autarch", 2, nil)
	item3, _ := s.CreateWrit("Item 3", "", "autarch", 2, nil)

	// Write cast history linking agents to writs.
	start := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	s.WriteHistory("Toast", item1, "cast", "", start, nil)
	s.WriteHistory("Toast", item2, "cast", "", start, nil)
	s.WriteHistory("Toast", item3, "cast", "", start, nil)
	s.WriteHistory("Jasper", item3, "cast", "", start, nil) // Jasper also worked on item3.

	// Create merge requests with various outcomes.
	mr1, _ := s.CreateMergeRequest(item1, "branch1", 2)
	mr2, _ := s.CreateMergeRequest(item2, "branch2", 2)
	mr3, _ := s.CreateMergeRequest(item3, "branch3", 2)

	// MR1: first-pass merge (claim once, merge).
	s.ClaimMergeRequest("forge")
	s.UpdateMergeRequestPhase(mr1, "merged")

	// MR2: failed then re-merged (claim, fail, re-ready, claim again, merge).
	// After first claim (mr1), claim again for mr2.
	s.ClaimMergeRequest("forge")
	s.UpdateMergeRequestPhase(mr2, "failed")

	// MR3: first-pass merge.
	s.ClaimMergeRequest("forge")
	s.UpdateMergeRequestPhase(mr3, "merged")

	// Check Toast's stats.
	stats, err := s.MergeStatsForAgent("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalMRs != 3 {
		t.Fatalf("expected 3 total MRs, got %d", stats.TotalMRs)
	}
	if stats.MergedMRs != 2 {
		t.Fatalf("expected 2 merged MRs, got %d", stats.MergedMRs)
	}
	if stats.FailedMRs != 1 {
		t.Fatalf("expected 1 failed MR, got %d", stats.FailedMRs)
	}
	if stats.FirstPassMRs != 2 {
		t.Fatalf("expected 2 first-pass MRs, got %d", stats.FirstPassMRs)
	}

	// Check Jasper's stats (only worked on item3).
	stats, err = s.MergeStatsForAgent("Jasper")
	if err != nil {
		t.Fatal(err)
	}
	if stats.MergedMRs != 1 {
		t.Fatalf("expected 1 merged MR for Jasper, got %d", stats.MergedMRs)
	}
	if stats.FirstPassMRs != 1 {
		t.Fatalf("expected 1 first-pass MR for Jasper, got %d", stats.FirstPassMRs)
	}

	// Nonexistent agent returns zeros.
	stats, err = s.MergeStatsForAgent("Nobody")
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalMRs != 0 {
		t.Fatalf("expected 0 total MRs for Nobody, got %d", stats.TotalMRs)
	}
}
