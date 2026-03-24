package store

import (
	"database/sql"
	"fmt"
)

// ImportAgent inserts an agent record with a specific ID and timestamps.
// State is always reset to "idle" and active_writ is cleared on import.
func (s *SphereStore) ImportAgent(id, name, world, role, createdAt, updatedAt string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO agents (id, name, world, role, state, active_writ, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'idle', NULL, ?, ?)`,
		id, name, world, role, createdAt, updatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to import agent %q: %w", id, err)
	}
	return nil
}

// ImportMessage inserts a message record with specific ID and timestamps.
func (s *SphereStore) ImportMessage(id, sender, recipient, subject, body string, priority int, msgType, threadID, delivery string, read bool, createdAt, ackedAt string) error {
	readInt := 0
	if read {
		readInt = 1
	}
	var acked sql.NullString
	if ackedAt != "" {
		acked = sql.NullString{String: ackedAt, Valid: true}
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO messages (id, sender, recipient, subject, body, priority, type, thread_id, delivery, read, created_at, acked_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, sender, recipient, subject, body, priority, msgType, threadID, delivery, readInt, createdAt, acked,
	)
	if err != nil {
		return fmt.Errorf("failed to import message %q: %w", id, err)
	}
	return nil
}

// ImportEscalation inserts an escalation record with specific ID and timestamps.
func (s *SphereStore) ImportEscalation(id, severity, source, description, status string, acknowledged bool, createdAt, updatedAt, sourceRef, lastNotifiedAt string) error {
	ackInt := 0
	if acknowledged {
		ackInt = 1
	}
	var sourceRefVal sql.NullString
	if sourceRef != "" {
		sourceRefVal = sql.NullString{String: sourceRef, Valid: true}
	}
	var lastNotifiedAtVal sql.NullString
	if lastNotifiedAt != "" {
		lastNotifiedAtVal = sql.NullString{String: lastNotifiedAt, Valid: true}
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO escalations (id, severity, source, description, source_ref, status, acknowledged, last_notified_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, severity, source, description, sourceRefVal, status, ackInt, lastNotifiedAtVal, createdAt, updatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to import escalation %q: %w", id, err)
	}
	return nil
}

// ImportCaravan inserts a caravan record with specific ID and timestamps.
func (s *SphereStore) ImportCaravan(id, name, status, owner, createdAt, closedAt string) error {
	var closed sql.NullString
	if closedAt != "" {
		closed = sql.NullString{String: closedAt, Valid: true}
	}
	var ownerVal sql.NullString
	if owner != "" {
		ownerVal = sql.NullString{String: owner, Valid: true}
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO caravans (id, name, status, owner, created_at, closed_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, status, ownerVal, createdAt, closed,
	)
	if err != nil {
		return fmt.Errorf("failed to import caravan %q: %w", id, err)
	}
	return nil
}

// ImportCaravanItem inserts a caravan item record.
func (s *SphereStore) ImportCaravanItem(caravanID, writID, world string, phase int) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO caravan_items (caravan_id, writ_id, world, phase)
		 VALUES (?, ?, ?, ?)`,
		caravanID, writID, world, phase,
	)
	if err != nil {
		return fmt.Errorf("failed to import caravan item %q in caravan %q: %w", writID, caravanID, err)
	}
	return nil
}

// ImportCaravanDependency inserts a caravan dependency record.
func (s *SphereStore) ImportCaravanDependency(fromID, toID string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO caravan_dependencies (from_id, to_id)
		 VALUES (?, ?)`,
		fromID, toID,
	)
	if err != nil {
		return fmt.Errorf("failed to import caravan dependency %q → %q: %w", fromID, toID, err)
	}
	return nil
}
