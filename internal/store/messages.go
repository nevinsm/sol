package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
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
	Via       string // SOL_VIA origin channel (ADR-0043 decision 1); empty if unset
	// ArchivedAt is set at THREAD granularity — archiving stamps every
	// message sharing this ThreadID, not just one message. nil means the
	// thread is not archived. See ArchiveThread/UnarchiveThread.
	ArchivedAt *time.Time
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

// SendMessageWithThreadIfAbsent attempts to insert a new pending message
// with the given non-empty threadID. If a pending message with the same
// threadID already exists, the insert is silently skipped — the second
// return value is false and id is empty.
//
// Dedup is enforced atomically by the partial UNIQUE index
// idx_messages_pending_thread_unique (sphere schema v16) on
// messages(thread_id) WHERE delivery='pending' AND thread_id != ''. This
// is the authoritative source of truth for thread-based dedup; callers
// must not rely on a separate SELECT-then-INSERT pattern, which races
// under multi-process deployments.
//
// Returns an error if threadID is empty (use SendMessageWithThread or
// SendMessage for non-threaded messages).
func (s *SphereStore) SendMessageWithThreadIfAbsent(sender, recipient, subject, body string, priority int, msgType, threadID string) (string, bool, error) {
	if threadID == "" {
		return "", false, fmt.Errorf("SendMessageWithThreadIfAbsent: threadID must be non-empty")
	}
	id, err := generateMessageID()
	if err != nil {
		return "", false, fmt.Errorf("failed to send message: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	// INSERT OR IGNORE relies on the partial UNIQUE index on thread_id
	// (delivery='pending', thread_id != ''). When a pending message with
	// this thread_id already exists, the insert is skipped silently and
	// RowsAffected() returns 0.
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO messages (id, sender, recipient, subject, body, priority, type, thread_id, delivery, read, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', 0, ?)`,
		id, sender, recipient, subject, body, priority, msgType, threadID, now,
	)
	if err != nil {
		return "", false, fmt.Errorf("failed to send message: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", false, fmt.Errorf("failed to inspect insert result: %w", err)
	}
	if n == 0 {
		// Dedup hit — a pending message with this thread_id already exists.
		return "", false, nil
	}
	return id, true, nil
}

// SendMessageWithOrigin creates a new message recording the SOL_VIA origin
// channel (ADR-0043 decision 1) and an explicit or auto-assigned ThreadID
// (ADR-0043 decision 3). If threadID is empty, the newly generated message
// ID is used as its own ThreadID — every message sent through this path
// gets a thread, and a fresh message with no stated thread is the simplest
// root of one, requiring no separate ID scheme. Returns the generated
// message ID.
func (s *SphereStore) SendMessageWithOrigin(sender, recipient, subject, body string, priority int, msgType, via, threadID string) (string, error) {
	id, err := generateMessageID()
	if err != nil {
		return "", fmt.Errorf("failed to send message: %w", err)
	}
	if threadID == "" {
		threadID = id
	}
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = s.db.Exec(
		`INSERT INTO messages (id, sender, recipient, subject, body, priority, type, thread_id, delivery, read, created_at, via)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', 0, ?, ?)`,
		id, sender, recipient, subject, body, priority, msgType, threadID, now, via,
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

// Inbox returns pending, non-archived messages for a recipient, ordered by
// priority ASC then created_at ASC (highest priority first, oldest first).
// If recipient is empty, returns all pending non-archived messages.
// Archived threads are excluded by default (see docs on ArchiveThread) —
// use InboxAll to include them ("mail inbox --all").
func (s *SphereStore) Inbox(recipient string) ([]Message, error) {
	query := `SELECT id, sender, recipient, subject, body, priority, type, thread_id, delivery, read, created_at, acked_at, via, archived_at
	          FROM messages WHERE delivery = 'pending' AND archived_at IS NULL`
	var args []interface{}
	if recipient != "" {
		query += ` AND recipient = ?`
		args = append(args, recipient)
	}
	query += ` ORDER BY priority ASC, created_at ASC`

	return s.scanMessages(query, args...)
}

// InboxAll returns pending messages for a recipient like Inbox, but
// includes messages belonging to archived threads ("mail inbox --all").
// If recipient is empty, returns all pending messages.
func (s *SphereStore) InboxAll(recipient string) ([]Message, error) {
	query := `SELECT id, sender, recipient, subject, body, priority, type, thread_id, delivery, read, created_at, acked_at, via, archived_at
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
	var threadID, ackedAt, archivedAt sql.NullString
	var createdAt string
	var read int

	err := s.db.QueryRow(
		`UPDATE messages SET read = 1 WHERE id = ?
		 RETURNING id, sender, recipient, subject, body, priority, type, thread_id, delivery, read, created_at, acked_at, via, archived_at`,
		id,
	).Scan(&msg.ID, &msg.Sender, &msg.Recipient, &msg.Subject, &body, &msg.Priority, &msg.Type, &threadID, &msg.Delivery, &read, &createdAt, &ackedAt, &msg.Via, &archivedAt)
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
	if msg.ArchivedAt, err = parseOptionalRFC3339(archivedAt, "archived_at", "message "+id); err != nil {
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

// CountPending returns the number of pending (unacknowledged), non-archived
// messages for a recipient. Archived+unread messages must not count here or
// trigger anything — archiving a thread with unread messages is allowed by
// design (it is often the point: sweeping up dead-weight trickle), so
// counting them would defeat the purpose of archiving.
func (s *SphereStore) CountPending(recipient string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM messages WHERE recipient = ? AND delivery = 'pending' AND archived_at IS NULL`,
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
	query := `SELECT id, sender, recipient, subject, body, priority, type, thread_id, delivery, read, created_at, acked_at, via, archived_at
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
		// Escape LIKE wildcards so the prefix is treated as a literal string.
		// SQLite LIKE special chars: % (any sequence), _ (any char), \ (escape char).
		prefix := filters.ThreadIDPrefix
		prefix = strings.ReplaceAll(prefix, `\`, `\\`)
		prefix = strings.ReplaceAll(prefix, `%`, `\%`)
		prefix = strings.ReplaceAll(prefix, `_`, `\_`)
		query += ` AND thread_id LIKE ? ESCAPE '\'`
		args = append(args, prefix+"%")
	}
	query += ` ORDER BY priority ASC, created_at ASC`

	return s.scanMessages(query, args...)
}

// Thread returns every message with the given thread_id, ordered
// chronologically (created_at ASC) regardless of read or delivery status.
// Unlike Inbox/ListMessages, callers reconstructing a conversation (`sol
// mail thread`) want the full history, not just pending/unread messages,
// and reading it must not mutate read state. Returns an empty slice (not
// an error) when no messages match — access control and "not found"
// semantics are the caller's responsibility.
// Thread always returns archived content — a pure read of thread history
// should not hide it, only listings (Inbox) filter archived threads out.
func (s *SphereStore) Thread(threadID string) ([]Message, error) {
	query := `SELECT id, sender, recipient, subject, body, priority, type, thread_id, delivery, read, created_at, acked_at, via, archived_at
	          FROM messages WHERE thread_id = ? ORDER BY created_at ASC`
	return s.scanMessages(query, threadID)
}

// ArchiveThread stamps archived_at = now on every message sharing threadID
// (including messages already archived — re-stamping is harmless and keeps
// this a single statement). Returns the number of messages in the thread;
// 0 means no message has this thread_id, which callers (e.g. cmd/mail.go's
// `mail archive`) treat as "thread not found".
//
// A single UPDATE statement is used deliberately rather than a per-message
// loop: SQLite applies one DML statement's row changes atomically (all or
// nothing), so a mid-statement failure — a constraint violation, a disk
// error partway through — leaves every message in the thread unarchived
// rather than archiving some and not others. This satisfies
// docs/conventions/state-mutation.md #4 ("prefer SQLite transactions for
// pure-DB multi-step mutations") without needing an explicit BEGIN/COMMIT,
// since the statement itself is the transaction.
func (s *SphereStore) ArchiveThread(threadID string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(`UPDATE messages SET archived_at = ? WHERE thread_id = ?`, now, threadID)
	if err != nil {
		return 0, fmt.Errorf("failed to archive thread %q: %w", threadID, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get archive count for thread %q: %w", threadID, err)
	}
	return n, nil
}

// UnarchiveThread clears archived_at on every message sharing threadID.
// See ArchiveThread for the single-statement atomicity rationale.
func (s *SphereStore) UnarchiveThread(threadID string) (int64, error) {
	result, err := s.db.Exec(`UPDATE messages SET archived_at = NULL WHERE thread_id = ?`, threadID)
	if err != nil {
		return 0, fmt.Errorf("failed to unarchive thread %q: %w", threadID, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get unarchive count for thread %q: %w", threadID, err)
	}
	return n, nil
}

// PurgeFilter narrows the messages CountPurgeCandidates and PurgeMessages
// select for deletion. The zero value matches nothing — at least one of
// RequireAcked or RequireArchived must be set; it is the caller's job
// (cmd/mail.go's `mail purge`) to require an explicit selector rather than
// defaulting to "everything".
//
// When both RequireAcked and RequireArchived are set, the two dimensions
// intersect (AND): only messages that are both acknowledged and archived
// (and pass any *Before cutoffs) match. This is how `mail purge` composes
// the new --archived/--older-than filters with the pre-existing
// --all-acked/--before selectors.
type PurgeFilter struct {
	// RequireAcked restricts the match to delivery='acked' messages — the
	// pre-existing purge invariant (never touches pending/unread mail)
	// when this dimension is used.
	RequireAcked bool
	// AckedBefore, if non-nil, additionally requires acked_at < this time.
	// Only meaningful when RequireAcked is true.
	AckedBefore *time.Time
	// RequireArchived restricts the match to messages whose thread has
	// been archived (archived_at IS NOT NULL). Unlike RequireAcked, this
	// does NOT imply the message was acknowledged: archiving a thread is
	// itself a "done with this" signal — see the mail skill's promotion
	// norm (distill anything durable, then archive) — independent of
	// per-message ack/read state. Used alone, RequireArchived can match
	// unacked/unread messages; combine with RequireAcked to narrow further.
	RequireArchived bool
	// ArchivedBefore, if non-nil, additionally requires archived_at < this
	// time. Only meaningful when RequireArchived is true.
	ArchivedBefore *time.Time
}

// whereClause builds the SQL WHERE fragment and bind args for f. Returns an
// empty clause when neither RequireAcked nor RequireArchived is set —
// CountPurgeCandidates and PurgeMessages treat that as invalid input rather
// than silently matching every message.
func (f PurgeFilter) whereClause() (string, []interface{}) {
	var conds []string
	var args []interface{}
	if f.RequireAcked {
		conds = append(conds, "delivery = 'acked'")
		if f.AckedBefore != nil {
			conds = append(conds, "acked_at < ?")
			args = append(args, f.AckedBefore.UTC().Format(time.RFC3339))
		}
	}
	if f.RequireArchived {
		conds = append(conds, "archived_at IS NOT NULL")
		if f.ArchivedBefore != nil {
			conds = append(conds, "archived_at < ?")
			args = append(args, f.ArchivedBefore.UTC().Format(time.RFC3339))
		}
	}
	return strings.Join(conds, " AND "), args
}

// CountPurgeCandidates returns how many messages match f without deleting
// them — used by `mail purge`'s dry-run preview (the existing confirmation
// convention: without --confirm, preview and exit 1).
func (s *SphereStore) CountPurgeCandidates(f PurgeFilter) (int, error) {
	where, args := f.whereClause()
	if where == "" {
		return 0, fmt.Errorf("PurgeFilter: at least one of RequireAcked or RequireArchived must be set")
	}
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE `+where, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count purge candidates: %w", err)
	}
	return count, nil
}

// PurgeMessages deletes every message matching f and returns the number of
// deleted rows. A single DELETE statement is used deliberately — see
// ArchiveThread's doc comment for the same atomicity rationale
// (docs/conventions/state-mutation.md #4): a mid-statement failure leaves
// no rows deleted rather than deleting some and not others.
func (s *SphereStore) PurgeMessages(f PurgeFilter) (int64, error) {
	where, args := f.whereClause()
	if where == "" {
		return 0, fmt.Errorf("PurgeFilter: at least one of RequireAcked or RequireArchived must be set")
	}
	result, err := s.db.Exec(`DELETE FROM messages WHERE `+where, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to purge messages: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get purge count: %w", err)
	}
	return n, nil
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
		var threadID, ackedAt, archivedAt sql.NullString
		var createdAt string
		var read int

		if err := rows.Scan(&msg.ID, &msg.Sender, &msg.Recipient, &msg.Subject, &body, &msg.Priority, &msg.Type, &threadID, &msg.Delivery, &read, &createdAt, &ackedAt, &msg.Via, &archivedAt); err != nil {
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
		if msg.ArchivedAt, parseErr = parseOptionalRFC3339(archivedAt, "archived_at", "message "+msg.ID); parseErr != nil {
			return nil, parseErr
		}
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating messages: %w", err)
	}
	return msgs, nil
}
