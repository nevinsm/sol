package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// Escalation represents a flagged problem requiring attention.
type Escalation struct {
	ID           string
	Severity     string // "low", "medium", "high", "critical"
	Source       string // agent ID or component that created it
	Description  string
	Status       string // "open", "acknowledged", "resolved"
	Acknowledged bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// validSeverities defines the allowed severity levels for escalations.
var validSeverities = map[string]bool{
	"low":      true,
	"medium":   true,
	"high":     true,
	"critical": true,
}

// generateEscalationID returns a new escalation ID in the format "esc-" + 8 hex chars.
func generateEscalationID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate escalation ID: %w", err)
	}
	return "esc-" + hex.EncodeToString(b), nil
}

// CreateEscalation creates an escalation record.
// Severity must be one of: "low", "medium", "high", "critical".
// Returns the escalation ID.
func (s *Store) CreateEscalation(severity, source, description string) (string, error) {
	if !validSeverities[severity] {
		return "", fmt.Errorf("invalid escalation severity %q: must be one of low, medium, high, critical", severity)
	}

	id, err := generateEscalationID()
	if err != nil {
		return "", fmt.Errorf("failed to create escalation: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = s.db.Exec(
		`INSERT INTO escalations (id, severity, source, description, status, acknowledged, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'open', 0, ?, ?)`,
		id, severity, source, description, now, now,
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

	err := s.db.QueryRow(
		`SELECT id, severity, source, description, status, acknowledged, created_at, updated_at
		 FROM escalations WHERE id = ?`, id,
	).Scan(&esc.ID, &esc.Severity, &esc.Source, &esc.Description, &esc.Status, &acknowledged, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("escalation %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get escalation %q: %w", id, err)
	}

	esc.Acknowledged = acknowledged != 0
	esc.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at for escalation %q: %w", id, err)
	}
	esc.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse updated_at for escalation %q: %w", id, err)
	}
	return esc, nil
}

// ListEscalations returns escalations filtered by status.
// If status is empty, returns all escalations.
// Ordered by created_at DESC (newest first).
func (s *Store) ListEscalations(status string) ([]Escalation, error) {
	query := `SELECT id, severity, source, description, status, acknowledged, created_at, updated_at
	          FROM escalations`
	var args []interface{}
	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC`

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

		if err := rows.Scan(&esc.ID, &esc.Severity, &esc.Source, &esc.Description, &esc.Status, &acknowledged, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan escalation: %w", err)
		}
		esc.Acknowledged = acknowledged != 0
		var parseErr error
		esc.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse created_at for escalation %q: %w", esc.ID, parseErr)
		}
		esc.UpdatedAt, parseErr = time.Parse(time.RFC3339, updatedAt)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse updated_at for escalation %q: %w", esc.ID, parseErr)
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
	// RowsAffected error is unlikely with modernc.org/sqlite but check defensively.
	n, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("failed to check rows affected: %w", raErr)
	}
	if n == 0 {
		return fmt.Errorf("escalation %q not found", id)
	}
	return nil
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
	// RowsAffected error is unlikely with modernc.org/sqlite but check defensively.
	n, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("failed to check rows affected: %w", raErr)
	}
	if n == 0 {
		return fmt.Errorf("escalation %q not found", id)
	}
	return nil
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
