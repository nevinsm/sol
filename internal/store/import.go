package store

import (
	"database/sql"
	"fmt"
)

// ImportAgent inserts an agent record with a specific ID and timestamps.
// State is always reset to "idle" and tether_item is cleared on import.
func (s *Store) ImportAgent(id, name, world, role, createdAt, updatedAt string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO agents (id, name, world, role, state, tether_item, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'idle', NULL, ?, ?)`,
		id, name, world, role, createdAt, updatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to import agent %q: %w", id, err)
	}
	return nil
}

// ImportMessage inserts a message record with specific ID and timestamps.
func (s *Store) ImportMessage(id, sender, recipient, subject, body string, priority int, msgType, threadID, delivery string, read bool, createdAt, ackedAt string) error {
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
func (s *Store) ImportEscalation(id, severity, source, description, status string, acknowledged bool, createdAt, updatedAt string) error {
	ackInt := 0
	if acknowledged {
		ackInt = 1
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO escalations (id, severity, source, description, status, acknowledged, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, severity, source, description, status, ackInt, createdAt, updatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to import escalation %q: %w", id, err)
	}
	return nil
}

// ImportCaravan inserts a caravan record with specific ID and timestamps.
func (s *Store) ImportCaravan(id, name, status, owner, createdAt, closedAt string) error {
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
func (s *Store) ImportCaravanItem(caravanID, workItemID, world string, phase int) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO caravan_items (caravan_id, work_item_id, world, phase)
		 VALUES (?, ?, ?, ?)`,
		caravanID, workItemID, world, phase,
	)
	if err != nil {
		return fmt.Errorf("failed to import caravan item %q in caravan %q: %w", workItemID, caravanID, err)
	}
	return nil
}
