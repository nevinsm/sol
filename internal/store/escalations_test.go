package store

import (
	"regexp"
	"testing"
	"time"
)

func TestCreateEscalation(t *testing.T) {
	s := setupTown(t)

	id, err := s.CreateEscalation("high", "myrig/sentinel", "Agent Toast stalled for 30m")
	if err != nil {
		t.Fatal(err)
	}

	// Verify ID format.
	pattern := regexp.MustCompile(`^esc-[0-9a-f]{8}$`)
	if !pattern.MatchString(id) {
		t.Fatalf("ID %q does not match pattern esc-[0-9a-f]{8}", id)
	}

	// Verify with GetEscalation.
	esc, err := s.GetEscalation(id)
	if err != nil {
		t.Fatal(err)
	}
	if esc.Severity != "high" {
		t.Fatalf("expected severity 'high', got %q", esc.Severity)
	}
	if esc.Source != "myrig/sentinel" {
		t.Fatalf("expected source 'myrig/sentinel', got %q", esc.Source)
	}
	if esc.Description != "Agent Toast stalled for 30m" {
		t.Fatalf("expected description 'Agent Toast stalled for 30m', got %q", esc.Description)
	}
	if esc.Status != "open" {
		t.Fatalf("expected status 'open', got %q", esc.Status)
	}
	if esc.Acknowledged {
		t.Fatal("expected acknowledged=false")
	}
	if esc.CreatedAt.IsZero() {
		t.Fatal("expected non-zero created_at")
	}
	if esc.UpdatedAt.IsZero() {
		t.Fatal("expected non-zero updated_at")
	}
}

func TestCreateEscalationInvalidSeverity(t *testing.T) {
	s := setupTown(t)

	_, err := s.CreateEscalation("invalid", "operator", "test")
	if err == nil {
		t.Fatal("expected error for invalid severity")
	}
}

func TestListEscalations(t *testing.T) {
	s := setupTown(t)

	// Create 3 escalations with distinct timestamps by inserting directly.
	now := time.Now().UTC()
	s.db.Exec(`INSERT INTO escalations (id, severity, source, description, status, acknowledged, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'open', 0, ?, ?)`,
		"esc-oldest01", "low", "operator", "Low issue",
		now.Add(-2*time.Hour).Format(time.RFC3339), now.Add(-2*time.Hour).Format(time.RFC3339))
	s.db.Exec(`INSERT INTO escalations (id, severity, source, description, status, acknowledged, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'open', 0, ?, ?)`,
		"esc-middle01", "medium", "operator", "Medium issue",
		now.Add(-1*time.Hour).Format(time.RFC3339), now.Add(-1*time.Hour).Format(time.RFC3339))
	s.db.Exec(`INSERT INTO escalations (id, severity, source, description, status, acknowledged, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'open', 0, ?, ?)`,
		"esc-newest01", "high", "operator", "High issue",
		now.Format(time.RFC3339), now.Format(time.RFC3339))

	// List all.
	escs, err := s.ListEscalations("")
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 3 {
		t.Fatalf("expected 3 escalations, got %d", len(escs))
	}

	// Newest first ordering: last created should be first.
	if escs[0].Description != "High issue" {
		t.Fatalf("expected newest first, got %q", escs[0].Description)
	}
	if escs[2].Description != "Low issue" {
		t.Fatalf("expected oldest last, got %q", escs[2].Description)
	}

	// List by status="open" -> all 3.
	escs, err = s.ListEscalations("open")
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 3 {
		t.Fatalf("expected 3 open escalations, got %d", len(escs))
	}

	// Resolve one, list open -> 2.
	s.ResolveEscalation("esc-oldest01")
	escs, err = s.ListEscalations("open")
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 2 {
		t.Fatalf("expected 2 open escalations after resolve, got %d", len(escs))
	}

	// List resolved -> 1.
	escs, err = s.ListEscalations("resolved")
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 1 {
		t.Fatalf("expected 1 resolved escalation, got %d", len(escs))
	}
}

func TestAckEscalation(t *testing.T) {
	s := setupTown(t)

	id, _ := s.CreateEscalation("medium", "operator", "Test ack")

	err := s.AckEscalation(id)
	if err != nil {
		t.Fatal(err)
	}

	esc, err := s.GetEscalation(id)
	if err != nil {
		t.Fatal(err)
	}
	if !esc.Acknowledged {
		t.Fatal("expected acknowledged=true")
	}
	if esc.Status != "acknowledged" {
		t.Fatalf("expected status 'acknowledged', got %q", esc.Status)
	}
}

func TestResolveEscalation(t *testing.T) {
	s := setupTown(t)

	id, _ := s.CreateEscalation("high", "operator", "Test resolve")

	err := s.ResolveEscalation(id)
	if err != nil {
		t.Fatal(err)
	}

	esc, err := s.GetEscalation(id)
	if err != nil {
		t.Fatal(err)
	}
	if esc.Status != "resolved" {
		t.Fatalf("expected status 'resolved', got %q", esc.Status)
	}
}

func TestCountOpen(t *testing.T) {
	s := setupTown(t)

	// Create 3.
	_, _ = s.CreateEscalation("low", "operator", "Issue 1")
	_, _ = s.CreateEscalation("medium", "operator", "Issue 2")
	id3, _ := s.CreateEscalation("high", "operator", "Issue 3")

	count, err := s.CountOpen()
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected 3 open, got %d", count)
	}

	// Resolve one -> 2.
	s.ResolveEscalation(id3)
	count, err = s.CountOpen()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 open after resolve, got %d", count)
	}
}

func TestAckEscalationNotFound(t *testing.T) {
	s := setupTown(t)

	err := s.AckEscalation("esc-nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent escalation")
	}
}

func TestResolveEscalationNotFound(t *testing.T) {
	s := setupTown(t)

	err := s.ResolveEscalation("esc-nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent escalation")
	}
}
