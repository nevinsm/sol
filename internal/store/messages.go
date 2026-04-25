package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Message represents a message in the sphere database.
type Message struct {
	ID        string
	Sender    string
	Recipient string
	Subject   string
	Body      string
	Priority  int
	Type      string     // "notification" or "protocol"
	ThreadID  string     // empty if not threaded
	Delivery  string     // "pending" or "acked"
	Read      bool
	CreatedAt time.Time
	AckedAt   *time.Time
}

// MessageFilters controls which messages are returned by ListMessages.
type MessageFilters struct {
	Recipient      string // filter by recipient (empty = all)
	Type           string // filter by type: "notification", "protocol" (empty = all)
	Delivery       string // filter by delivery: "pending", "acked" (empty = all)
	ThreadID       string // filter by exact thread_id (empty = all)
	ThreadIDPrefix string // filter by thread_id prefix using LIKE (empty = all)
}

// generateMessageID returns a new message ID in the format "msg-" + 16 hex chars.
func generateMessageID() (string, error) {
	return generatePrefixedID("msg-")
}

// SendMessage creates a new message in the store.
// Returns the generated message ID (msg-XXXXXXXX).
func (s *SphereStore) SendMessage(sender, recipient, subject, body string, priority int, msgType string) (string, error) {
	id, err := generateMessageID()
	if err != nil {
		return "", fmt.Errorf("failed to send message: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = s.db.Exec(
		`INSERT INTO messages (id, sender, recipient, subject, body, priority, type, thread_id, delivery, read, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, '', 'pending', 0, ?)`,
		id, sender, recipient, subject, body, priority, msgType, now,
	)
	if err != nil {
		return "", fmt.Errorf("failed to send message: %w", err)
	}
	return id, nil
}

// SendMessageWithThread creates a new message with an explicit ThreadID.
// Returns the generated message ID (msg-XXXXXXXX).
func (s *SphereStore) SendMessageWithThread(sender, recipient, subject, body string, priority int, msgType, threadID string) (string, error) {
	id, err := generateMessageID()
	if err != nil {
		return "", fmt.Errorf("failed to send message: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = s.db.Exec(
		`INSERT INTO messages (id, sender, recipient, subject, body, priority, type, thread_id, delivery, read, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', 0, ?)`,
		id, sender, recipient, subject, body, priority, msgType, threadID, now,
	)
	if err != nil {
		return "", fmt.Errorf("failed to send message: %w", err)
	}
	return id, nil
}

// HasPendingThreadMessage checks if a pending message with the given threadID exists.
func (s *SphereStore) HasPendingThreadMessage(threadID string) (bool, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM messages WHERE thread_id = ? AND delivery = 'pending'`,
		threadID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check pending thread message %q: %w", threadID, err)
	}
	return count > 0, nil
}

// Inbox returns pending messages for a recipient, ordered by priority ASC
// then created_at ASC (highest priority first, oldest first).
// If recipient is empty, returns all pending messages.
func (s *SphereStore) Inbox(recipient string) ([]Message, error) {
	query := `SELECT id, sender, recipient, subject, body, priority, type, thread_id, delivery, read, created_at, acked_at
	          FROM messages WHERE delivery = 'pending'`
	var args []interface{}
	if recipient != "" {
		query += ` AND recipient = ?`
		args = append(args, recipient)
	}
	query += ` ORDER BY priority ASC, created_at ASC`

	return s.scanMessages(query, args...)
}

// ReadMessage returns a message by ID and marks it as read (read=1).
// Uses UPDATE...RETURNING to atomically mark read and fetch the message.
func (s *SphereStore) ReadMessage(id string) (*Message, error) {
	msg := &Message{}
	var body sql.NullString
	var threadID, ackedAt sql.NullString
	var createdAt string
	var read int

	err := s.db.QueryRow(
		`UPDATE messages SET read = 1 WHERE id = ?
		 RETURNING id, sender, recipient, subject, body, priority, type, thread_id, delivery, read, created_at, acked_at`,
		id,
	).Scan(&msg.ID, &msg.Sender, &msg.Recipient, &msg.Subject, &body, &msg.Priority, &msg.Type, &threadID, &msg.Delivery, &read, &createdAt, &ackedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("message %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read message %q: %w", id, err)
	}

	msg.Body = body.String
	msg.ThreadID = threadID.String
	msg.Read = read != 0
	if msg.CreatedAt, err = parseRFC3339(createdAt, "created_at", "message "+id); err != nil {
		return nil, err
	}
	if msg.AckedAt, err = parseOptionalRFC3339(ackedAt, "acked_at", "message "+id); err != nil {
		return nil, err
	}
	return msg, nil
}

// DismissMessage dismisses a message from the inbox — sets delivery='dismissed'.
// The message remains accessible via ListMessages but no longer appears in
// the Inbox query (which filters on delivery='pending').
func (s *SphereStore) DismissMessage(id string) error {
	result, err := s.db.Exec(
		`UPDATE messages SET delivery = 'dismissed' WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to dismiss message %q: %w", id, err)
	}
	return checkRowsAffected(result, "message", id)
}

// AckMessage acknowledges a message — sets delivery='acked' and acked_at=now.
func (s *SphereStore) AckMessage(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(
		`UPDATE messages SET delivery = 'acked', acked_at = ? WHERE id = ?`,
		now, id,
	)
	if err != nil {
		return fmt.Errorf("failed to ack message %q: %w", id, err)
	}
	return checkRowsAffected(result, "message", id)
}

// CountPending returns the number of pending (unacknowledged) messages for a recipient.
func (s *SphereStore) CountPending(recipient string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM messages WHERE recipient = ? AND delivery = 'pending'`,
		recipient,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count pending messages for %q: %w", recipient, err)
	}
	return count, nil
}

// ListMessages returns messages filtered by optional criteria.
// Supports filtering by recipient, type, delivery status, and thread_id.
func (s *SphereStore) ListMessages(filters MessageFilters) ([]Message, error) {
	query := `SELECT id, sender, recipient, subject, body, priority, type, thread_id, delivery, read, created_at, acked_at
	          FROM messages WHERE 1=1`
	var args []interface{}

	if filters.Recipient != "" {
		query += ` AND recipient = ?`
		args = append(args, filters.Recipient)
	}
	if filters.Type != "" {
		query += ` AND type = ?`
		args = append(args, filters.Type)
	}
	if filters.Delivery != "" {
		query += ` AND delivery = ?`
		args = append(args, filters.Delivery)
	}
	if filters.ThreadID != "" {
		query += ` AND thread_id = ?`
		args = append(args, filters.ThreadID)
	}
	if filters.ThreadIDPrefix != "" {
		query += ` AND thread_id LIKE ?`
		args = append(args, filters.ThreadIDPrefix+"%")
	}
	query += ` ORDER BY priority ASC, created_at ASC`

	return s.scanMessages(query, args...)
}

// CountAcked returns the number of acknowledged messages.
func (s *SphereStore) CountAcked() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE delivery = 'acked'`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count acked messages: %w", err)
	}
	return count, nil
}

// CountAckedBefore returns the number of acknowledged messages with acked_at older than before.
func (s *SphereStore) CountAckedBefore(before time.Time) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM messages WHERE delivery = 'acked' AND acked_at < ?`,
		before.UTC().Format(time.RFC3339),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count acked messages before %s: %w", before.Format(time.RFC3339), err)
	}
	return count, nil
}

// PurgeAckedMessages deletes acknowledged messages with acked_at older than before.
// Returns the number of deleted rows. Never deletes unread/pending messages.
func (s *SphereStore) PurgeAckedMessages(before time.Time) (int64, error) {
	result, err := s.db.Exec(
		`DELETE FROM messages WHERE delivery = 'acked' AND acked_at < ?`,
		before.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to purge acked messages: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get purge count: %w", err)
	}
	return n, nil
}

// PurgeAllAcked deletes all acknowledged messages regardless of age.
// Returns the number of deleted rows. Never deletes unread/pending messages.
func (s *SphereStore) PurgeAllAcked() (int64, error) {
	result, err := s.db.Exec(`DELETE FROM messages WHERE delivery = 'acked'`)
	if err != nil {
		return 0, fmt.Errorf("failed to purge all acked messages: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get purge count: %w", err)
	}
	return n, nil
}

// scanMessages executes a query and scans the results into Message structs.
func (s *SphereStore) scanMessages(query string, args ...interface{}) ([]Message, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var msg Message
		var body sql.NullString
		var threadID, ackedAt sql.NullString
		var createdAt string
		var read int

		if err := rows.Scan(&msg.ID, &msg.Sender, &msg.Recipient, &msg.Subject, &body, &msg.Priority, &msg.Type, &threadID, &msg.Delivery, &read, &createdAt, &ackedAt); err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		msg.Body = body.String
		msg.ThreadID = threadID.String
		msg.Read = read != 0
		var parseErr error
		if msg.CreatedAt, parseErr = parseRFC3339(createdAt, "created_at", "message "+msg.ID); parseErr != nil {
			return nil, parseErr
		}
		if msg.AckedAt, parseErr = parseOptionalRFC3339(ackedAt, "acked_at", "message "+msg.ID); parseErr != nil {
			return nil, parseErr
		}
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating messages: %w", err)
	}
	return msgs, nil
}
