package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Escalation represents a flagged problem requiring attention.
type Escalation struct {
	ID             string
	Severity       string     // "low", "medium", "high", "critical"
	Source         string     // agent ID or component that created it
	Description    string
	SourceRef      string     // structured reference (e.g., "mr:mr-abc123", "writ:sol-xyz")
	Status         string     // "open", "acknowledged", "resolved"
	Acknowledged   bool
	LastNotifiedAt *time.Time // when notification was last sent; nil means use CreatedAt
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// validSeverities defines the allowed severity levels for escalations.
var validSeverities = map[string]bool{
	"low":      true,
	"medium":   true,
	"high":     true,
	"critical": true,
}

// generateEscalationID returns a new escalation ID in the format "esc-" + 16 hex chars.
func generateEscalationID() (string, error) {
	return generatePrefixedID("esc-")
}

// CreateEscalation creates an escalation record.
// Severity must be one of: "low", "medium", "high", "critical".
// An optional sourceRef provides a structured reference for the escalation
// (e.g., "mr:mr-abc123" or "writ:sol-xyz").
// Returns the escalation ID.
func (s *Store) CreateEscalation(severity, source, description string, sourceRef ...string) (string, error) {
	if !validSeverities[severity] {
		return "", fmt.Errorf("invalid escalation severity %q: must be one of low, medium, high, critical", severity)
	}

	id, err := generateEscalationID()
	if err != nil {
		return "", fmt.Errorf("failed to create escalation: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	var ref *string
	if len(sourceRef) > 0 && sourceRef[0] != "" {
		ref = &sourceRef[0]
	}

	_, err = s.db.Exec(
		`INSERT INTO escalations (id, severity, source, description, source_ref, status, acknowledged, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 'open', 0, ?, ?)`,
		id, severity, source, description, ref, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create escalation: %w", err)
	}
	return id, nil
}

// GetEscalation returns an escalation by ID.
func (s *Store) GetEscalation(id string) (*Escalation, error) {
	esc := &Escalation{}
	var createdAt, updatedAt string
	var acknowledged int
	var sourceRef sql.NullString
	var lastNotifiedAt sql.NullString

	err := s.db.QueryRow(
		`SELECT id, severity, source, description, source_ref, status, acknowledged, last_notified_at, created_at, updated_at
		 FROM escalations WHERE id = ?`, id,
	).Scan(&esc.ID, &esc.Severity, &esc.Source, &esc.Description, &sourceRef, &esc.Status, &acknowledged, &lastNotifiedAt, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("escalation %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get escalation %q: %w", id, err)
	}

	if sourceRef.Valid {
		esc.SourceRef = sourceRef.String
	}
	esc.Acknowledged = acknowledged != 0
	if lastNotifiedAt.Valid {
		t, err := time.Parse(time.RFC3339, lastNotifiedAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse last_notified_at for escalation %q: %w", id, err)
		}
		esc.LastNotifiedAt = &t
	}
	if esc.CreatedAt, err = parseRFC3339(createdAt, "created_at", "escalation "+id); err != nil {
		return nil, err
	}
	if esc.UpdatedAt, err = parseRFC3339(updatedAt, "updated_at", "escalation "+id); err != nil {
		return nil, err
	}
	return esc, nil
}

// severityOrderSQL is a CASE expression that maps severity levels to
// numeric weights for ORDER BY (higher = more severe, sorted first with DESC).
const severityOrderSQL = `CASE severity
	WHEN 'critical' THEN 4
	WHEN 'high' THEN 3
	WHEN 'medium' THEN 2
	WHEN 'low' THEN 1
	ELSE 0 END`

// ListEscalations returns escalations filtered by status.
// If status is empty, returns all escalations.
// Ordered by severity DESC (critical first), then created_at DESC (newest first).
func (s *Store) ListEscalations(status string) ([]Escalation, error) {
	query := `SELECT id, severity, source, description, source_ref, status, acknowledged, last_notified_at, created_at, updated_at
	          FROM escalations`
	var args []interface{}
	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY ` + severityOrderSQL + ` DESC, created_at DESC`

	return s.scanEscalations(query, args...)
}

// ListOpenEscalations returns all non-resolved escalations.
// Ordered by severity DESC (critical first), then created_at DESC (newest first).
func (s *Store) ListOpenEscalations() ([]Escalation, error) {
	query := `SELECT id, severity, source, description, source_ref, status, acknowledged, last_notified_at, created_at, updated_at
	          FROM escalations WHERE status != 'resolved'
	          ORDER BY ` + severityOrderSQL + ` DESC, created_at DESC`
	return s.scanEscalations(query)
}

// ListEscalationsBySourceRef returns non-resolved escalations matching a source_ref.
// Ordered by created_at DESC (newest first).
func (s *Store) ListEscalationsBySourceRef(sourceRef string) ([]Escalation, error) {
	query := `SELECT id, severity, source, description, source_ref, status, acknowledged, last_notified_at, created_at, updated_at
	          FROM escalations WHERE source_ref = ? AND status != 'resolved' ORDER BY created_at DESC`
	return s.scanEscalations(query, sourceRef)
}

// scanEscalations executes a query and scans the results into Escalation structs.
func (s *Store) scanEscalations(query string, args ...interface{}) ([]Escalation, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list escalations: %w", err)
	}
	defer rows.Close()

	var escs []Escalation
	for rows.Next() {
		var esc Escalation
		var createdAt, updatedAt string
		var acknowledged int
		var sourceRef sql.NullString
		var lastNotifiedAt sql.NullString

		if err := rows.Scan(&esc.ID, &esc.Severity, &esc.Source, &esc.Description, &sourceRef, &esc.Status, &acknowledged, &lastNotifiedAt, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan escalation: %w", err)
		}
		if sourceRef.Valid {
			esc.SourceRef = sourceRef.String
		}
		esc.Acknowledged = acknowledged != 0
		if lastNotifiedAt.Valid {
			t, parseErr := time.Parse(time.RFC3339, lastNotifiedAt.String)
			if parseErr != nil {
				return nil, fmt.Errorf("failed to parse last_notified_at for escalation %q: %w", esc.ID, parseErr)
			}
			esc.LastNotifiedAt = &t
		}
		var parseErr error
		if esc.CreatedAt, parseErr = parseRFC3339(createdAt, "created_at", "escalation "+esc.ID); parseErr != nil {
			return nil, parseErr
		}
		if esc.UpdatedAt, parseErr = parseRFC3339(updatedAt, "updated_at", "escalation "+esc.ID); parseErr != nil {
			return nil, parseErr
		}
		escs = append(escs, esc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating escalations: %w", err)
	}
	return escs, nil
}

// AckEscalation marks an escalation as acknowledged.
// Sets acknowledged=true, status="acknowledged", updated_at=now.
func (s *Store) AckEscalation(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(
		`UPDATE escalations SET acknowledged = 1, status = 'acknowledged', updated_at = ? WHERE id = ?`,
		now, id,
	)
	if err != nil {
		return fmt.Errorf("failed to acknowledge escalation %q: %w", id, err)
	}
	return checkRowsAffected(result, "escalation", id)
}

// ResolveEscalation marks an escalation as resolved.
// Sets status="resolved", updated_at=now.
func (s *Store) ResolveEscalation(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(
		`UPDATE escalations SET status = 'resolved', updated_at = ? WHERE id = ?`,
		now, id,
	)
	if err != nil {
		return fmt.Errorf("failed to resolve escalation %q: %w", id, err)
	}
	return checkRowsAffected(result, "escalation", id)
}

// UpdateEscalationLastNotified sets the last_notified_at timestamp to now.
func (s *Store) UpdateEscalationLastNotified(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`UPDATE escalations SET last_notified_at = ? WHERE id = ?`, now, id)
	return err
}

// CountOpen returns the number of open (unresolved) escalations.
func (s *Store) CountOpen() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM escalations WHERE status != 'resolved'`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count open escalations: %w", err)
	}
	return count, nil
}
