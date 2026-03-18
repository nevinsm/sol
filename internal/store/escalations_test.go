package store

import (
	"regexp"
	"testing"
	"time"
)

func TestCreateEscalation(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id, err := s.CreateEscalation("high", "haven/sentinel", "Agent Toast stalled for 30m")
	if err != nil {
		t.Fatal(err)
	}

	// Verify ID format.
	pattern := regexp.MustCompile(`^esc-[0-9a-f]{16}$`)
	if !pattern.MatchString(id) {
		t.Fatalf("ID %q does not match pattern esc-[0-9a-f]{16}", id)
	}

	// Verify with GetEscalation.
	esc, err := s.GetEscalation(id)
	if err != nil {
		t.Fatal(err)
	}
	if esc.Severity != "high" {
		t.Fatalf("expected severity 'high', got %q", esc.Severity)
	}
	if esc.Source != "haven/sentinel" {
		t.Fatalf("expected source 'haven/sentinel', got %q", esc.Source)
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
	t.Parallel()
	s := setupSphere(t)

	_, err := s.CreateEscalation("invalid", "autarch", "test")
	if err == nil {
		t.Fatal("expected error for invalid severity")
	}
}

func TestListEscalations(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Create 3 escalations with distinct timestamps by inserting directly.
	now := time.Now().UTC()
	s.db.Exec(`INSERT INTO escalations (id, severity, source, description, status, acknowledged, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'open', 0, ?, ?)`,
		"esc-oldest01", "low", "autarch", "Low issue",
		now.Add(-2*time.Hour).Format(time.RFC3339), now.Add(-2*time.Hour).Format(time.RFC3339))
	s.db.Exec(`INSERT INTO escalations (id, severity, source, description, status, acknowledged, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'open', 0, ?, ?)`,
		"esc-middle01", "medium", "autarch", "Medium issue",
		now.Add(-1*time.Hour).Format(time.RFC3339), now.Add(-1*time.Hour).Format(time.RFC3339))
	s.db.Exec(`INSERT INTO escalations (id, severity, source, description, status, acknowledged, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'open', 0, ?, ?)`,
		"esc-newest01", "high", "autarch", "High issue",
		now.Format(time.RFC3339), now.Format(time.RFC3339))

	// List all.
	escs, err := s.ListEscalations("")
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 3 {
		t.Fatalf("expected 3 escalations, got %d", len(escs))
	}

	// Severity-first ordering: high > medium > low (then newest within same severity).
	if escs[0].Description != "High issue" {
		t.Fatalf("expected high severity first, got %q", escs[0].Description)
	}
	if escs[1].Description != "Medium issue" {
		t.Fatalf("expected medium severity second, got %q", escs[1].Description)
	}
	if escs[2].Description != "Low issue" {
		t.Fatalf("expected low severity last, got %q", escs[2].Description)
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

func TestListEscalationsSortsBySeverityThenCreatedAt(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	now := time.Now().UTC()

	// Insert escalations with mixed severities and timestamps.
	// Two critical (older first), one high, one medium, one low.
	s.db.Exec(`INSERT INTO escalations (id, severity, source, description, status, acknowledged, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'open', 0, ?, ?)`,
		"esc-low00000001", "low", "autarch", "Low issue",
		now.Add(-5*time.Hour).Format(time.RFC3339), now.Format(time.RFC3339))
	s.db.Exec(`INSERT INTO escalations (id, severity, source, description, status, acknowledged, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'open', 0, ?, ?)`,
		"esc-crit0000001", "critical", "autarch", "Critical older",
		now.Add(-2*time.Hour).Format(time.RFC3339), now.Format(time.RFC3339))
	s.db.Exec(`INSERT INTO escalations (id, severity, source, description, status, acknowledged, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'open', 0, ?, ?)`,
		"esc-med00000001", "medium", "autarch", "Medium issue",
		now.Add(-3*time.Hour).Format(time.RFC3339), now.Format(time.RFC3339))
	s.db.Exec(`INSERT INTO escalations (id, severity, source, description, status, acknowledged, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'open', 0, ?, ?)`,
		"esc-crit0000002", "critical", "autarch", "Critical newer",
		now.Add(-1*time.Hour).Format(time.RFC3339), now.Format(time.RFC3339))
	s.db.Exec(`INSERT INTO escalations (id, severity, source, description, status, acknowledged, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'open', 0, ?, ?)`,
		"esc-high0000001", "high", "autarch", "High issue",
		now.Add(-4*time.Hour).Format(time.RFC3339), now.Format(time.RFC3339))

	escs, err := s.ListEscalations("")
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 5 {
		t.Fatalf("expected 5 escalations, got %d", len(escs))
	}

	// Expected order: critical newer, critical older, high, medium, low.
	expected := []string{"Critical newer", "Critical older", "High issue", "Medium issue", "Low issue"}
	for i, desc := range expected {
		if escs[i].Description != desc {
			t.Fatalf("position %d: expected %q, got %q", i, desc, escs[i].Description)
		}
	}
}

func TestListOpenEscalationsSortsBySeverity(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	now := time.Now().UTC()
	// Insert with different severities — all open.
	s.db.Exec(`INSERT INTO escalations (id, severity, source, description, status, acknowledged, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'open', 0, ?, ?)`,
		"esc-low00000010", "low", "autarch", "Low issue",
		now.Add(-1*time.Hour).Format(time.RFC3339), now.Format(time.RFC3339))
	s.db.Exec(`INSERT INTO escalations (id, severity, source, description, status, acknowledged, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'open', 0, ?, ?)`,
		"esc-high0000010", "high", "autarch", "High issue",
		now.Format(time.RFC3339), now.Format(time.RFC3339))

	escs, err := s.ListOpenEscalations()
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 2 {
		t.Fatalf("expected 2, got %d", len(escs))
	}
	// High should come first despite being newer (severity wins).
	if escs[0].Severity != "high" {
		t.Fatalf("expected high first, got %q", escs[0].Severity)
	}
	if escs[1].Severity != "low" {
		t.Fatalf("expected low second, got %q", escs[1].Severity)
	}
}

func TestListOpenEscalations(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Create 3 escalations.
	id1, _ := s.CreateEscalation("low", "autarch", "Issue 1")
	_, _ = s.CreateEscalation("medium", "autarch", "Issue 2")
	id3, _ := s.CreateEscalation("high", "autarch", "Issue 3")

	// All 3 are open -> ListOpenEscalations returns 3.
	escs, err := s.ListOpenEscalations()
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 3 {
		t.Fatalf("expected 3 open escalations, got %d", len(escs))
	}

	// Resolve one -> ListOpenEscalations returns 2.
	s.ResolveEscalation(id1)
	escs, err = s.ListOpenEscalations()
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 2 {
		t.Fatalf("expected 2 open escalations after resolve, got %d", len(escs))
	}

	// Acknowledge one -> still returned (not resolved).
	s.AckEscalation(id3)
	escs, err = s.ListOpenEscalations()
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 2 {
		t.Fatalf("expected 2 open escalations after ack, got %d", len(escs))
	}

	// Resolve all remaining -> ListOpenEscalations returns 0.
	for _, e := range escs {
		s.ResolveEscalation(e.ID)
	}
	escs, err = s.ListOpenEscalations()
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 0 {
		t.Fatalf("expected 0 open escalations after resolving all, got %d", len(escs))
	}

	// ListEscalations("") still returns all 3.
	all, err := s.ListEscalations("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 total escalations, got %d", len(all))
	}
}

func TestAckEscalation(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id, _ := s.CreateEscalation("medium", "autarch", "Test ack")

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
	t.Parallel()
	s := setupSphere(t)

	id, _ := s.CreateEscalation("high", "autarch", "Test resolve")

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
	t.Parallel()
	s := setupSphere(t)

	// Create 3.
	_, _ = s.CreateEscalation("low", "autarch", "Issue 1")
	_, _ = s.CreateEscalation("medium", "autarch", "Issue 2")
	id3, _ := s.CreateEscalation("high", "autarch", "Issue 3")

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
	t.Parallel()
	s := setupSphere(t)

	err := s.AckEscalation("esc-nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent escalation")
	}
}

func TestResolveEscalationNotFound(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	err := s.ResolveEscalation("esc-nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent escalation")
	}
}

func TestCreateEscalationWithSourceRef(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id, err := s.CreateEscalation("high", "ember/forge", "Merge failed for MR mr-abc123", "mr:mr-abc123")
	if err != nil {
		t.Fatal(err)
	}

	esc, err := s.GetEscalation(id)
	if err != nil {
		t.Fatal(err)
	}
	if esc.SourceRef != "mr:mr-abc123" {
		t.Fatalf("expected source_ref %q, got %q", "mr:mr-abc123", esc.SourceRef)
	}
}

func TestCreateEscalationWithoutSourceRef(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Existing callers that don't pass source_ref should still work.
	id, err := s.CreateEscalation("low", "autarch", "Something happened")
	if err != nil {
		t.Fatal(err)
	}

	esc, err := s.GetEscalation(id)
	if err != nil {
		t.Fatal(err)
	}
	if esc.SourceRef != "" {
		t.Fatalf("expected empty source_ref, got %q", esc.SourceRef)
	}
}

func TestUpdateEscalationLastNotified(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id, err := s.CreateEscalation("high", "autarch", "Test last_notified_at")
	if err != nil {
		t.Fatal(err)
	}

	// Initially nil.
	esc, err := s.GetEscalation(id)
	if err != nil {
		t.Fatal(err)
	}
	if esc.LastNotifiedAt != nil {
		t.Fatalf("expected nil LastNotifiedAt, got %v", esc.LastNotifiedAt)
	}

	// Set last_notified_at.
	if err := s.UpdateEscalationLastNotified(id); err != nil {
		t.Fatal(err)
	}

	esc, err = s.GetEscalation(id)
	if err != nil {
		t.Fatal(err)
	}
	if esc.LastNotifiedAt == nil {
		t.Fatal("expected non-nil LastNotifiedAt after update")
	}
	if time.Since(*esc.LastNotifiedAt) > 5*time.Second {
		t.Fatalf("LastNotifiedAt too old: %v", esc.LastNotifiedAt)
	}

	// Verify it's also returned by ListOpenEscalations.
	escs, err := s.ListOpenEscalations()
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 1 {
		t.Fatalf("expected 1 escalation, got %d", len(escs))
	}
	if escs[0].LastNotifiedAt == nil {
		t.Fatal("expected non-nil LastNotifiedAt in ListOpenEscalations result")
	}
}

func TestUpdateEscalationLastNotifiedNotFound(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	err := s.UpdateEscalationLastNotified("esc-nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent escalation")
	}
}

func TestUpdateEscalationLastNotifiedUpdatesUpdatedAt(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id, err := s.CreateEscalation("high", "autarch", "Test updated_at")
	if err != nil {
		t.Fatal(err)
	}

	// Backdate updated_at to ensure the update produces a different timestamp.
	oldTime := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	if _, err := s.db.Exec(`UPDATE escalations SET updated_at = ? WHERE id = ?`, oldTime, id); err != nil {
		t.Fatal(err)
	}

	esc, err := s.GetEscalation(id)
	if err != nil {
		t.Fatal(err)
	}
	initialUpdatedAt := esc.UpdatedAt

	// Update last_notified_at — should also update updated_at.
	if err := s.UpdateEscalationLastNotified(id); err != nil {
		t.Fatal(err)
	}

	esc, err = s.GetEscalation(id)
	if err != nil {
		t.Fatal(err)
	}
	if !esc.UpdatedAt.After(initialUpdatedAt) {
		t.Fatalf("expected updated_at to advance: initial=%v, after=%v",
			initialUpdatedAt, esc.UpdatedAt)
	}
}

func TestListEscalationsBySourceRef(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Create escalations with different source_refs.
	id1, _ := s.CreateEscalation("high", "ember/forge", "Failed MR 1", "mr:mr-abc123")
	_, _ = s.CreateEscalation("high", "ember/forge", "Failed MR 2", "mr:mr-def456")
	id3, _ := s.CreateEscalation("low", "ember/forge", "Unclosed writ", "mr:mr-abc123")

	// List by source_ref = "mr:mr-abc123" → should return 2.
	escs, err := s.ListEscalationsBySourceRef("mr:mr-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 2 {
		t.Fatalf("expected 2 escalations for mr:mr-abc123, got %d", len(escs))
	}

	// Resolve one and list again → should return 1.
	s.ResolveEscalation(id1)
	escs, err = s.ListEscalationsBySourceRef("mr:mr-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 1 {
		t.Fatalf("expected 1 escalation after resolve, got %d", len(escs))
	}
	if escs[0].ID != id3 {
		t.Fatalf("expected remaining escalation ID %q, got %q", id3, escs[0].ID)
	}

	// List by non-existent source_ref → should return 0.
	escs, err = s.ListEscalationsBySourceRef("mr:mr-nonexist")
	if err != nil {
		t.Fatal(err)
	}
	if len(escs) != 0 {
		t.Fatalf("expected 0 escalations for nonexistent ref, got %d", len(escs))
	}
}
