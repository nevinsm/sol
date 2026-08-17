package store

import (
	"testing"
	"time"
)

func TestWritVitalsNoHistory(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	v, err := s.WritVitals("sol-doesnotexist0000")
	if err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Fatalf("expected nil vitals for writ with no history, got %+v", v)
	}
}

func TestWritVitalsMultiSession(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	writID := "sol-vitals0000000001"
	start := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)

	// Initial cast to Toast, with ledger-recorded token usage.
	h1, err := s.WriteHistory("Toast", writID, "cast", "", start, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteTokenUsage(h1, "claude-sonnet", 1000, 500, 200, 50, 0, nil, nil, "claude-code", ""); err != nil {
		t.Fatal(err)
	}

	// A respawn (prefect recovering a dead session), no token usage recorded
	// (ledger gap — e.g. telemetry never arrived).
	respawnStart := start.Add(1 * time.Hour)
	if _, err := s.WriteHistory("Toast", writID, "respawn", "", respawnStart, nil); err != nil {
		t.Fatal(err)
	}

	// A handoff to a different agent, with more token usage, and this one
	// closes out (has ended_at).
	handoffStart := start.Add(2 * time.Hour)
	handoffEnd := start.Add(3 * time.Hour)
	h3, err := s.WriteHistory("Jasper", writID, "handoff", "", handoffStart, &handoffEnd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteTokenUsage(h3, "claude-sonnet", 2000, 1000, 300, 100, 0, nil, nil, "claude-code", ""); err != nil {
		t.Fatal(err)
	}

	// Unrelated history for a different writ must not leak in.
	if _, err := s.WriteHistory("Ember", "sol-unrelated00000001", "cast", "", start, nil); err != nil {
		t.Fatal(err)
	}

	v, err := s.WritVitals(writID)
	if err != nil {
		t.Fatal(err)
	}
	if v == nil {
		t.Fatal("expected non-nil vitals")
	}

	if v.SessionCount != 3 {
		t.Errorf("SessionCount = %d, want 3", v.SessionCount)
	}
	if v.HandoffCount != 1 {
		t.Errorf("HandoffCount = %d, want 1", v.HandoffCount)
	}
	if v.RespawnCount != 1 {
		t.Errorf("RespawnCount = %d, want 1", v.RespawnCount)
	}

	wantInput := int64(1000 + 2000)
	wantOutput := int64(500 + 1000)
	wantCache := int64(200 + 50 + 300 + 100)
	if v.InputTokens != wantInput {
		t.Errorf("InputTokens = %d, want %d", v.InputTokens, wantInput)
	}
	if v.OutputTokens != wantOutput {
		t.Errorf("OutputTokens = %d, want %d", v.OutputTokens, wantOutput)
	}
	if v.CacheTokens != wantCache {
		t.Errorf("CacheTokens = %d, want %d", v.CacheTokens, wantCache)
	}

	if len(v.AgentNames) != 2 || v.AgentNames[0] != "Jasper" || v.AgentNames[1] != "Toast" {
		t.Errorf("AgentNames = %v, want [Jasper Toast]", v.AgentNames)
	}

	if v.StartedAt == nil || !v.StartedAt.Equal(start) {
		t.Errorf("StartedAt = %v, want %v", v.StartedAt, start)
	}
	// Only h3 has an ended_at; the respawn and initial cast rows are still
	// open, but that must not stop EndedAt from reflecting the one session
	// that did close.
	if v.EndedAt == nil || !v.EndedAt.Equal(handoffEnd) {
		t.Errorf("EndedAt = %v, want %v", v.EndedAt, handoffEnd)
	}
}

func TestWritVitalsAllSessionsOpen(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	writID := "sol-vitalsopen0000001"
	start := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)

	if _, err := s.WriteHistory("Toast", writID, "cast", "", start, nil); err != nil {
		t.Fatal(err)
	}

	v, err := s.WritVitals(writID)
	if err != nil {
		t.Fatal(err)
	}
	if v == nil {
		t.Fatal("expected non-nil vitals")
	}
	if v.SessionCount != 1 {
		t.Errorf("SessionCount = %d, want 1", v.SessionCount)
	}
	if v.EndedAt != nil {
		t.Errorf("EndedAt = %v, want nil (no session has closed yet)", v.EndedAt)
	}
	if v.InputTokens != 0 || v.OutputTokens != 0 || v.CacheTokens != 0 {
		t.Errorf("expected zero token totals with no token_usage rows, got in=%d out=%d cache=%d",
			v.InputTokens, v.OutputTokens, v.CacheTokens)
	}
}
