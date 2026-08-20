package store

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestSendMessage(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id, err := s.SendMessage("haven/Toast", "autarch", "Work done", "Finished task sol-abc12345", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}

	// Verify ID format.
	pattern := regexp.MustCompile(`^msg-[0-9a-f]{16}$`)
	if !pattern.MatchString(id) {
		t.Fatalf("ID %q does not match pattern msg-[0-9a-f]{16}", id)
	}

	// Read it back and verify all fields.
	msg, err := s.ReadMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Sender != "haven/Toast" {
		t.Fatalf("expected sender 'haven/Toast', got %q", msg.Sender)
	}
	if msg.Recipient != "autarch" {
		t.Fatalf("expected recipient 'autarch', got %q", msg.Recipient)
	}
	if msg.Subject != "Work done" {
		t.Fatalf("expected subject 'Work done', got %q", msg.Subject)
	}
	if msg.Body != "Finished task sol-abc12345" {
		t.Fatalf("expected body 'Finished task sol-abc12345', got %q", msg.Body)
	}
	if msg.Priority != 2 {
		t.Fatalf("expected priority 2, got %d", msg.Priority)
	}
	if msg.Type != "notification" {
		t.Fatalf("expected type 'notification', got %q", msg.Type)
	}
	if msg.Delivery != "pending" {
		t.Fatalf("expected delivery 'pending', got %q", msg.Delivery)
	}
	if msg.AckedAt != nil {
		t.Fatalf("expected nil acked_at, got %v", msg.AckedAt)
	}
}

func TestInbox(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Send 3 messages to "autarch" with different priorities.
	s.SendMessage("agent1", "autarch", "Low priority", "", 3, "notification")
	s.SendMessage("agent2", "autarch", "Urgent", "", 1, "notification")
	s.SendMessage("agent3", "autarch", "Normal", "", 2, "notification")

	// Inbox should return all 3, ordered by priority then age.
	msgs, err := s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Priority != 1 {
		t.Fatalf("expected first message priority 1, got %d", msgs[0].Priority)
	}
	if msgs[1].Priority != 2 {
		t.Fatalf("expected second message priority 2, got %d", msgs[1].Priority)
	}
	if msgs[2].Priority != 3 {
		t.Fatalf("expected third message priority 3, got %d", msgs[2].Priority)
	}

	// Send a message to "other" -> not in operator's inbox.
	s.SendMessage("agent4", "other", "For other", "", 2, "notification")
	msgs, err = s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages for operator, got %d", len(msgs))
	}

	// Ack one message -> no longer in inbox.
	s.AckMessage(msgs[0].ID)
	msgs, err = s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after ack, got %d", len(msgs))
	}
}

func TestReadMessage(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id, _ := s.SendMessage("agent1", "autarch", "Test", "Body", 2, "notification")

	// ReadMessage -> returns full message, marks as read.
	msg, err := s.ReadMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if !msg.Read {
		t.Fatal("expected message to be marked as read")
	}
	if msg.Subject != "Test" {
		t.Fatalf("expected subject 'Test', got %q", msg.Subject)
	}

	// ReadMessage again -> still returns (idempotent read).
	msg2, err := s.ReadMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if !msg2.Read {
		t.Fatal("expected message still marked as read")
	}
}

// TestGetMessageDoesNotMarkRead verifies GetMessage is a pure peek — unlike
// ReadMessage, it must never set read=1 as a side effect.
func TestGetMessageDoesNotMarkRead(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id, _ := s.SendMessage("agent1", "autarch", "Test", "Body", 2, "notification")

	msg, err := s.GetMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Read {
		t.Fatal("expected GetMessage to leave read=0")
	}
	if msg.Subject != "Test" {
		t.Fatalf("expected subject 'Test', got %q", msg.Subject)
	}

	// Confirm via a second peek that nothing changed.
	msg2, err := s.GetMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if msg2.Read {
		t.Fatal("expected read to remain 0 after a second GetMessage call")
	}

	// CountPending is unaffected — GetMessage must not touch delivery either.
	count, err := s.CountPending("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 pending after GetMessage, got %d", count)
	}
}

// TestGetMessageNotFound verifies GetMessage surfaces the same "not found"
// error shape as ReadMessage for a bogus ID.
func TestGetMessageNotFound(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	_, err := s.GetMessage("msg-nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent message")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected error containing 'not found', got %q", err.Error())
	}
}

func TestAckMessage(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id, _ := s.SendMessage("agent1", "autarch", "Test", "", 2, "notification")

	// AckMessage -> delivery='acked', acked_at set.
	err := s.AckMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := s.ReadMessage(id)
	if msg.Delivery != "acked" {
		t.Fatalf("expected delivery 'acked', got %q", msg.Delivery)
	}
	if msg.AckedAt == nil {
		t.Fatal("expected acked_at to be set")
	}

	// AckMessage again -> no error (idempotent).
	err = s.AckMessage(id)
	if err != nil {
		t.Fatal(err)
	}

	// Message no longer appears in Inbox.
	msgs, _ := s.Inbox("autarch")
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages in inbox after ack, got %d", len(msgs))
	}
}

func TestDismissMessage(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id, _ := s.SendMessage("agent1", "autarch", "Test", "", 2, "notification")

	// DismissMessage -> delivery='dismissed'.
	err := s.DismissMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := s.ReadMessage(id)
	if msg.Delivery != "dismissed" {
		t.Fatalf("expected delivery 'dismissed', got %q", msg.Delivery)
	}

	// Message no longer appears in Inbox (which filters delivery='pending').
	msgs, _ := s.Inbox("autarch")
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages in inbox after dismiss, got %d", len(msgs))
	}

	// Message still accessible via ListMessages (no delivery filter).
	all, err := s.ListMessages(MessageFilters{Recipient: "autarch"})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 message in ListMessages after dismiss, got %d", len(all))
	}
	if all[0].Delivery != "dismissed" {
		t.Fatalf("expected delivery 'dismissed' in ListMessages, got %q", all[0].Delivery)
	}
}

func TestCountPending(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// No messages -> 0.
	count, err := s.CountPending("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}

	// Send 3 messages -> 3.
	id1, _ := s.SendMessage("agent1", "autarch", "Msg 1", "", 2, "notification")
	s.SendMessage("agent2", "autarch", "Msg 2", "", 2, "notification")
	id3, _ := s.SendMessage("agent3", "autarch", "Msg 3", "", 2, "notification")

	count, err = s.CountPending("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected 3, got %d", count)
	}

	// Read one -> still 3 (read doesn't affect count, only ack does).
	s.ReadMessage(id1)
	count, err = s.CountPending("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected 3 after read, got %d", count)
	}

	// Ack one -> 2.
	s.AckMessage(id3)
	count, err = s.CountPending("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 after ack, got %d", count)
	}
}

func TestListMessages(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Send messages of different types and to different recipients.
	s.SendMessage("agent1", "autarch", "Notif 1", "", 2, "notification")
	s.SendMessage("agent2", "autarch", "Proto 1", "{}", 1, "protocol")
	s.SendMessage("agent3", "other", "Notif 2", "", 2, "notification")
	id4, _ := s.SendMessage("agent4", "autarch", "Acked", "", 2, "notification")
	s.AckMessage(id4)

	// Filter by recipient -> only matching.
	msgs, err := s.ListMessages(MessageFilters{Recipient: "autarch"})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages for operator, got %d", len(msgs))
	}

	// Filter by type -> only matching.
	msgs, err = s.ListMessages(MessageFilters{Type: "protocol"})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 protocol message, got %d", len(msgs))
	}

	// Filter by delivery -> only matching.
	msgs, err = s.ListMessages(MessageFilters{Delivery: "acked"})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 acked message, got %d", len(msgs))
	}

	// No filters -> all messages.
	msgs, err = s.ListMessages(MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 {
		t.Fatalf("expected 4 total messages, got %d", len(msgs))
	}
}

func TestListMessagesThreadFilter(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Send a message without thread_id and one with.
	s.SendMessage("agent1", "autarch", "No thread", "", 2, "notification")
	s.SendMessageWithThread("agent2", "autarch", "Threaded", "", 2, "notification", "thread-abc")

	// Filter by exact thread -> only the threaded message.
	msgs, err := s.ListMessages(MessageFilters{ThreadID: "thread-abc"})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 threaded message, got %d", len(msgs))
	}
	if msgs[0].ThreadID != "thread-abc" {
		t.Fatalf("expected thread_id 'thread-abc', got %q", msgs[0].ThreadID)
	}
}

func TestSendMessageWithThread(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id, err := s.SendMessageWithThread("agent1", "autarch", "Test", "Body", 2, "notification", "esc:sol-abc123")
	if err != nil {
		t.Fatal(err)
	}

	// Verify the message has the correct ThreadID.
	msg, err := s.ReadMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if msg.ThreadID != "esc:sol-abc123" {
		t.Fatalf("expected thread_id 'esc:sol-abc123', got %q", msg.ThreadID)
	}
	if msg.Sender != "agent1" {
		t.Fatalf("expected sender 'agent1', got %q", msg.Sender)
	}
}

func TestHasPendingThreadMessage(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// No messages -> false.
	exists, err := s.HasPendingThreadMessage("esc:test")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("expected false for nonexistent thread")
	}

	// Send a message with thread -> true.
	id, _ := s.SendMessageWithThread("agent1", "autarch", "Test", "", 2, "notification", "esc:test")
	exists, err = s.HasPendingThreadMessage("esc:test")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("expected true for pending thread message")
	}

	// Ack the message -> false.
	s.AckMessage(id)
	exists, err = s.HasPendingThreadMessage("esc:test")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("expected false after acking the message")
	}
}

func TestSendMessageWithThreadIfAbsent(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// First call inserts a new pending message with this thread_id.
	id1, inserted, err := s.SendMessageWithThreadIfAbsent("agent1", "autarch", "Test", "", 2, "notification", "esc:thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if !inserted {
		t.Fatal("expected first call to insert (inserted=true), got false")
	}
	if id1 == "" {
		t.Fatal("expected first call to return a non-empty id")
	}

	// Second call with the same thread_id must not insert and must not
	// error — the partial UNIQUE index makes this dedupe atomic.
	id2, inserted2, err := s.SendMessageWithThreadIfAbsent("agent2", "autarch", "Test", "", 2, "notification", "esc:thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if inserted2 {
		t.Fatal("expected second call to be deduped (inserted=false), got true")
	}
	if id2 != "" {
		t.Fatalf("expected empty id on dedup, got %q", id2)
	}

	// Only one pending message with this thread_id should exist.
	msgs, err := s.ListMessages(MessageFilters{ThreadID: "esc:thread-1", Delivery: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected exactly 1 pending message, got %d", len(msgs))
	}

	// After the original message is acked, a new insert is allowed —
	// the partial index only constrains pending messages.
	if err := s.AckMessage(id1); err != nil {
		t.Fatal(err)
	}
	id3, inserted3, err := s.SendMessageWithThreadIfAbsent("agent3", "autarch", "Test again", "", 2, "notification", "esc:thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if !inserted3 {
		t.Fatal("expected post-ack insert to succeed (inserted=true), got false")
	}
	if id3 == "" {
		t.Fatal("expected post-ack insert to return a non-empty id")
	}
}

func TestSendMessageWithThreadIfAbsentRejectsEmptyThreadID(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	_, inserted, err := s.SendMessageWithThreadIfAbsent("agent", "autarch", "S", "", 2, "notification", "")
	if err == nil {
		t.Fatal("expected error for empty thread_id, got nil")
	}
	if inserted {
		t.Fatal("expected inserted=false on error")
	}
}

// TestSendMessageWithThreadAllowsMultiplePending is the store-level repro
// for the bug this writ fixes: sending a second message into a thread
// before the first is acked used to fail with "UNIQUE constraint failed:
// messages.thread_id" because idx_messages_pending_thread_unique (sphere
// schema v16) bound every threaded pending message, not just
// escalation-notification dedup sends. Rescoping dedup onto the dedup_key
// column (sphere schema v19, which SendMessageWithThread leaves NULL)
// means ordinary threaded conversation messages coexist freely while
// pending.
func TestSendMessageWithThreadAllowsMultiplePending(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id1, err := s.SendMessageWithThread("sol-dev/Nova", "autarch", "First", "body1", 2, "notification", "thread-multi-1")
	if err != nil {
		t.Fatal(err)
	}
	// Neither message is acked — both stay pending.
	id2, err := s.SendMessageWithThread("autarch", "sol-dev/Nova", "Second", "body2", 2, "notification", "thread-multi-1")
	if err != nil {
		t.Fatalf("second send into the same pending thread must succeed: %v", err)
	}
	if id1 == id2 {
		t.Fatalf("expected distinct message ids, got %q twice", id1)
	}

	msgs, err := s.ListMessages(MessageFilters{ThreadID: "thread-multi-1", Delivery: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 pending messages in the thread, got %d", len(msgs))
	}
}

// TestSendMessageWithOriginAllowsMultiplePending covers the same repro via
// SendMessageWithOrigin — the path `sol mail send --thread=<id>` actually
// uses (cmd/mail.go's mailSendCmd).
func TestSendMessageWithOriginAllowsMultiplePending(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id1, err := s.SendMessageWithOrigin("sol-dev/Nova", "autarch", "First", "body1", 2, "notification", "", "thread-multi-origin")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.SendMessageWithOrigin("autarch", "sol-dev/Nova", "Second", "body2", 2, "notification", "", "thread-multi-origin")
	if err != nil {
		t.Fatalf("second send into the same pending thread must succeed: %v", err)
	}
	if id1 == id2 {
		t.Fatalf("expected distinct message ids, got %q twice", id1)
	}

	msgs, err := s.ListMessages(MessageFilters{ThreadID: "thread-multi-origin", Delivery: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 pending messages in the thread, got %d", len(msgs))
	}
}

func TestListMessagesThreadIDPrefix(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Send messages with various thread IDs.
	s.SendMessageWithThread("agent1", "autarch", "Esc 1", "", 2, "notification", "esc:sol-aaa")
	s.SendMessageWithThread("agent2", "autarch", "Esc 2", "", 2, "notification", "esc:sol-bbb")
	s.SendMessage("agent3", "autarch", "No thread", "", 2, "notification")
	s.SendMessageWithThread("agent4", "autarch", "Other thread", "", 2, "notification", "other:xyz")

	// Filter by prefix "esc:" -> should return both escalation messages.
	msgs, err := s.ListMessages(MessageFilters{ThreadIDPrefix: "esc:"})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages with prefix 'esc:', got %d", len(msgs))
	}

	// Filter by prefix "other:" -> 1 message.
	msgs, err = s.ListMessages(MessageFilters{ThreadIDPrefix: "other:"})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message with prefix 'other:', got %d", len(msgs))
	}

	// Empty prefix -> all messages (no prefix filter).
	msgs, err = s.ListMessages(MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 {
		t.Fatalf("expected 4 total messages, got %d", len(msgs))
	}
}

func TestSendProtocolMessage(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	payload := AgentDonePayload{
		WritID: "sol-abc12345",
		AgentID:    "haven/Toast",
		Branch:     "outpost/Toast/sol-abc12345",
		World:      "haven",
	}

	id, err := s.SendProtocolMessage("haven/Toast", "haven/sentinel", ProtoAgentDone, payload)
	if err != nil {
		t.Fatal(err)
	}

	// Verify: type='protocol', subject='AGENT_DONE', body is valid JSON.
	msg, err := s.ReadMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Type != "protocol" {
		t.Fatalf("expected type 'protocol', got %q", msg.Type)
	}
	if msg.Subject != "AGENT_DONE" {
		t.Fatalf("expected subject 'AGENT_DONE', got %q", msg.Subject)
	}
	if msg.Priority != 1 {
		t.Fatalf("expected priority 1, got %d", msg.Priority)
	}

	// Parse body back into AgentDonePayload, verify fields.
	var parsed AgentDonePayload
	if err := json.Unmarshal([]byte(msg.Body), &parsed); err != nil {
		t.Fatalf("failed to unmarshal body: %v", err)
	}
	if parsed.WritID != "sol-abc12345" {
		t.Fatalf("expected writ_id 'sol-abc12345', got %q", parsed.WritID)
	}
	if parsed.AgentID != "haven/Toast" {
		t.Fatalf("expected agent_id 'haven/Toast', got %q", parsed.AgentID)
	}

	// PendingProtocol(recipient, "AGENT_DONE") -> returns message.
	msgs, err := s.PendingProtocol("haven/sentinel", ProtoAgentDone)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 pending AGENT_DONE, got %d", len(msgs))
	}

	// PendingProtocol(recipient, "MERGE_READY") -> empty (wrong type).
	msgs, err = s.PendingProtocol("haven/sentinel", ProtoMergeReady)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 pending MERGE_READY, got %d", len(msgs))
	}
}

func TestMessageNotFound(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// ReadMessage with bogus ID -> error containing "not found".
	_, err := s.ReadMessage("msg-nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent message")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected error containing 'not found', got %q", err.Error())
	}

	// AckMessage with bogus ID -> error containing "not found".
	err = s.AckMessage("msg-nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent message")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected error containing 'not found', got %q", err.Error())
	}
}

func TestPurgeAckedMessages(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Send 3 messages and ack them all.
	id1, _ := s.SendMessage("agent1", "autarch", "Msg 1", "", 2, "notification")
	id2, _ := s.SendMessage("agent2", "autarch", "Msg 2", "", 2, "notification")
	id3, _ := s.SendMessage("agent3", "autarch", "Msg 3", "", 2, "notification")

	s.AckMessage(id1)
	s.AckMessage(id2)
	s.AckMessage(id3)

	// Backdate acked_at for id1 and id2 to simulate old messages.
	oldTime := time.Now().UTC().Add(-10 * 24 * time.Hour).Format(time.RFC3339)
	if _, err := s.db.Exec(`UPDATE messages SET acked_at = ? WHERE id = ?`, oldTime, id1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE messages SET acked_at = ? WHERE id = ?`, oldTime, id2); err != nil {
		t.Fatal(err)
	}

	// Purge messages acked more than 7 days ago.
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	count, err := s.PurgeAckedMessages(cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 purged, got %d", count)
	}

	// id3 should still exist (acked recently).
	msgs, err := s.ListMessages(MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 remaining message, got %d", len(msgs))
	}
	if msgs[0].ID != id3 {
		t.Fatalf("expected %s to remain, got %s", id3, msgs[0].ID)
	}
}

func TestPurgeAckedMessagesNeverDeletesPending(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Send messages: 2 pending, 1 acked.
	s.SendMessage("agent1", "autarch", "Pending 1", "", 2, "notification")
	s.SendMessage("agent2", "autarch", "Pending 2", "", 2, "notification")
	id3, _ := s.SendMessage("agent3", "autarch", "Acked", "", 2, "notification")
	s.AckMessage(id3)

	// Backdate the acked message.
	oldTime := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	if _, err := s.db.Exec(`UPDATE messages SET acked_at = ? WHERE id = ?`, oldTime, id3); err != nil {
		t.Fatal(err)
	}

	// Purge old acked messages.
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	count, err := s.PurgeAckedMessages(cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 purged, got %d", count)
	}

	// Both pending messages should still exist.
	pending, err := s.CountPending("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if pending != 2 {
		t.Fatalf("expected 2 pending messages preserved, got %d", pending)
	}
}

func TestPurgeAllAcked(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Send 3 messages, ack 2, leave 1 pending.
	id1, _ := s.SendMessage("agent1", "autarch", "Acked 1", "", 2, "notification")
	id2, _ := s.SendMessage("agent2", "autarch", "Acked 2", "", 2, "notification")
	s.SendMessage("agent3", "autarch", "Pending", "", 2, "notification")

	s.AckMessage(id1)
	s.AckMessage(id2)

	count, err := s.PurgeAllAcked()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 purged, got %d", count)
	}

	// Only the pending message should remain.
	msgs, err := s.ListMessages(MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 remaining message, got %d", len(msgs))
	}
	if msgs[0].Subject != "Pending" {
		t.Fatalf("expected pending message to remain, got %q", msgs[0].Subject)
	}
}

func TestPurgeAllAckedEmpty(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// No messages at all.
	count, err := s.PurgeAllAcked()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 purged, got %d", count)
	}
}

func TestMailLifecycle(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// 1. Send message.
	id, err := s.SendMessage("agent1", "autarch", "Task complete", "Details here", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`^msg-[0-9a-f]{16}$`)
	if !pattern.MatchString(id) {
		t.Fatalf("ID %q does not match expected pattern", id)
	}

	// 2. Verify in Inbox.
	msgs, err := s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message in inbox, got %d", len(msgs))
	}
	if msgs[0].ID != id {
		t.Fatalf("expected message %s in inbox, got %s", id, msgs[0].ID)
	}

	// 3. CountPending accurate.
	count, err := s.CountPending("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}

	// 4. ReadMessage marks read.
	msg, err := s.ReadMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if !msg.Read {
		t.Fatal("expected message to be marked as read")
	}
	if msg.Delivery != "pending" {
		t.Fatalf("expected delivery 'pending' after read, got %q", msg.Delivery)
	}

	// 5. CountPending unchanged by read.
	count, err = s.CountPending("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected count 1 after read, got %d", count)
	}

	// 6. AckMessage sets acked.
	if err := s.AckMessage(id); err != nil {
		t.Fatal(err)
	}
	msg, err = s.ReadMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Delivery != "acked" {
		t.Fatalf("expected delivery 'acked', got %q", msg.Delivery)
	}
	if msg.AckedAt == nil {
		t.Fatal("expected acked_at to be set")
	}

	// 7. CountPending drops after ack.
	count, err = s.CountPending("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected count 0 after ack, got %d", count)
	}

	// 8. PurgeAllAcked cleans up.
	purged, err := s.PurgeAllAcked()
	if err != nil {
		t.Fatal(err)
	}
	if purged != 1 {
		t.Fatalf("expected 1 purged, got %d", purged)
	}

	// 9. Message is gone.
	msgs, err = s.ListMessages(MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after purge, got %d", len(msgs))
	}
}

func TestProtocolMessageSendAndFilter(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Send protocol messages of different types.
	donePayload := AgentDonePayload{
		WritID:  "sol-test12345678",
		AgentID: "haven/Toast",
		Branch:  "outpost/Toast/sol-test12345678",
		World:   "haven",
	}
	id1, err := s.SendProtocolMessage("haven/Toast", "haven/sentinel", ProtoAgentDone, donePayload)
	if err != nil {
		t.Fatal(err)
	}

	mergePayload := MergeReadyPayload{
		MergeRequestID: "mr-001",
		WritID:         "sol-test12345678",
		Branch:         "outpost/Toast/sol-test12345678",
	}
	_, err = s.SendProtocolMessage("haven/sentinel", "haven/forge", ProtoMergeReady, mergePayload)
	if err != nil {
		t.Fatal(err)
	}

	// Filter by AGENT_DONE.
	msgs, err := s.PendingProtocol("haven/sentinel", ProtoAgentDone)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 AGENT_DONE, got %d", len(msgs))
	}
	if msgs[0].ID != id1 {
		t.Fatalf("expected id %s, got %s", id1, msgs[0].ID)
	}

	// Verify body is valid JSON with correct fields.
	var parsed AgentDonePayload
	if err := json.Unmarshal([]byte(msgs[0].Body), &parsed); err != nil {
		t.Fatalf("failed to unmarshal body: %v", err)
	}
	if parsed.WritID != "sol-test12345678" {
		t.Fatalf("expected writ_id 'sol-test12345678', got %q", parsed.WritID)
	}

	// Filter by MERGE_READY for sentinel -> empty (wrong recipient).
	msgs, err = s.PendingProtocol("haven/sentinel", ProtoMergeReady)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 MERGE_READY for sentinel, got %d", len(msgs))
	}

	// Filter by MERGE_READY for forge -> 1 message.
	msgs, err = s.PendingProtocol("haven/forge", ProtoMergeReady)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 MERGE_READY for forge, got %d", len(msgs))
	}
}

// TestSendMessageWithOriginRecordsVia verifies the via origin channel
// (ADR-0043 decision 1) round-trips through ReadMessage.
func TestSendMessageWithOriginRecordsVia(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id, err := s.SendMessageWithOrigin("autarch", "haven/Toast", "Hello", "body", 2, "notification", "notify-bridge", "")
	if err != nil {
		t.Fatal(err)
	}

	msg, err := s.ReadMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Via != "notify-bridge" {
		t.Fatalf("expected via 'notify-bridge', got %q", msg.Via)
	}
}

// TestSendMessageWithOriginEmptyVia verifies an empty via is stored as ''
// (not NULL) and surfaces as an empty string, matching the "sol's own
// internal callers never set via" convention.
func TestSendMessageWithOriginEmptyVia(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id, err := s.SendMessageWithOrigin("autarch", "haven/Toast", "Hello", "body", 2, "notification", "", "")
	if err != nil {
		t.Fatal(err)
	}

	msg, err := s.ReadMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Via != "" {
		t.Fatalf("expected empty via, got %q", msg.Via)
	}
}

// TestSendMessageWithOriginAutoThread verifies that an empty threadID
// (ADR-0043 decision 3, "omitted --thread") makes the message its own
// thread root: ThreadID equals the generated message ID.
func TestSendMessageWithOriginAutoThread(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id, err := s.SendMessageWithOrigin("autarch", "haven/Toast", "Hello", "body", 2, "notification", "", "")
	if err != nil {
		t.Fatal(err)
	}

	msg, err := s.ReadMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if msg.ThreadID != id {
		t.Fatalf("expected auto-assigned thread_id to equal message id %q, got %q", id, msg.ThreadID)
	}
}

// TestSendMessageWithOriginExplicitThread verifies an explicit threadID is
// stored as given, not overridden by the auto-assignment rule.
func TestSendMessageWithOriginExplicitThread(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id, err := s.SendMessageWithOrigin("autarch", "haven/Toast", "Hello", "body", 2, "notification", "", "thread-explicit-1")
	if err != nil {
		t.Fatal(err)
	}

	msg, err := s.ReadMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if msg.ThreadID != "thread-explicit-1" {
		t.Fatalf("expected explicit thread_id 'thread-explicit-1', got %q", msg.ThreadID)
	}
}

// TestThreadReturnsAllMessagesChronologically verifies Thread returns every
// message with the given thread_id in created_at order, regardless of read
// or delivery status — the point is reconstructing the whole conversation.
func TestThreadReturnsAllMessagesChronologically(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Multiple pending messages per thread_id are allowed (sphere schema
	// v19 scoped dedup to dedup_key, which SendMessageWithThread leaves
	// NULL) — this test still acks each message before the next is sent,
	// mirroring how a live conversation actually progresses turn by turn,
	// but that's a modeling choice here, not a constraint the store
	// enforces.
	id1, err := s.SendMessageWithThread("sol-dev/Nova", "autarch", "First", "body1", 2, "notification", "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadMessage(id1); err != nil {
		t.Fatal(err)
	}
	if err := s.AckMessage(id1); err != nil {
		t.Fatal(err)
	}

	id2, err := s.SendMessageWithThread("autarch", "sol-dev/Nova", "Second", "body2", 2, "notification", "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AckMessage(id2); err != nil {
		t.Fatal(err)
	}

	id3, err := s.SendMessageWithThread("sol-dev/Nova", "autarch", "Third", "body3", 2, "notification", "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	// id3 stays pending — Thread must return all three regardless of the
	// read/delivery mix (id1 read+acked, id2 acked-only, id3 pending).

	// Unrelated message in a different thread must not appear.
	if _, err := s.SendMessageWithThread("agent-x", "autarch", "Other", "", 2, "notification", "thread-2"); err != nil {
		t.Fatal(err)
	}

	msgs, err := s.Thread("thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].ID != id1 || msgs[1].ID != id2 || msgs[2].ID != id3 {
		t.Fatalf("expected chronological order [%s %s %s], got [%s %s %s]",
			id1, id2, id3, msgs[0].ID, msgs[1].ID, msgs[2].ID)
	}
}

// TestThreadUnknownReturnsEmpty verifies Thread returns an empty slice (no
// error) for a thread_id with no messages — "not found" is the caller's
// responsibility (mail thread's access-check layer), not the store's.
func TestThreadUnknownReturnsEmpty(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	msgs, err := s.Thread("thread-does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(msgs))
	}
}

// TestInboxSurfacesViaAndThread verifies Inbox (used by `mail inbox --json`)
// scans the via and thread_id columns rather than dropping them.
func TestInboxSurfacesViaAndThread(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	_, err := s.SendMessageWithOrigin("automation-bot", "autarch", "Ping", "", 2, "notification", "automation-bot", "thread-inbox-1")
	if err != nil {
		t.Fatal(err)
	}

	msgs, err := s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Via != "automation-bot" {
		t.Fatalf("expected via 'automation-bot', got %q", msgs[0].Via)
	}
	if msgs[0].ThreadID != "thread-inbox-1" {
		t.Fatalf("expected thread_id 'thread-inbox-1', got %q", msgs[0].ThreadID)
	}
}

// --- Thread archive/unarchive + purge filters ---

// TestArchiveThreadStampsAllMessagesInThread verifies ArchiveThread sets
// archived_at on every message sharing the thread_id, regardless of
// individual read/delivery state, and returns the count.
func TestArchiveThreadStampsAllMessagesInThread(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Mirror a real back-and-forth: ack each pending message before the
	// next is sent. Not required by the store (multiple pending messages
	// per thread_id are allowed since sphere schema v19), just a realistic
	// conversation shape for this test.
	id1, err := s.SendMessageWithThread("sol-dev/Nova", "autarch", "First", "b1", 2, "notification", "thread-arc-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AckMessage(id1); err != nil {
		t.Fatal(err)
	}
	id2, err := s.SendMessageWithThread("autarch", "sol-dev/Nova", "Second", "b2", 2, "notification", "thread-arc-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AckMessage(id2); err != nil {
		t.Fatal(err)
	}
	// id3 stays pending and unread — archiving must still stamp it.
	if _, err := s.SendMessageWithThread("sol-dev/Nova", "autarch", "Third", "b3", 2, "notification", "thread-arc-1"); err != nil {
		t.Fatal(err)
	}

	n, err := s.ArchiveThread("thread-arc-1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("expected 3 messages archived, got %d", n)
	}

	msgs, err := s.Thread("thread-arc-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages in thread, got %d", len(msgs))
	}
	for _, m := range msgs {
		if m.ArchivedAt == nil {
			t.Errorf("expected message %s (delivery=%s) to be archived", m.ID, m.Delivery)
		}
	}
}

// TestArchiveThreadExcludesFromInboxAndUnreadCount verifies design point 3
// and 4: archiving hides the thread from Inbox and its unread messages no
// longer count toward CountPending, but InboxAll ("mail inbox --all")
// still surfaces it.
func TestArchiveThreadExcludesFromInboxAndUnreadCount(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id, err := s.SendMessageWithThread("agent1", "autarch", "Test", "body", 2, "notification", "thread-arc-2")
	if err != nil {
		t.Fatal(err)
	}

	// Sanity: before archiving, the pending+unread message is visible and counted.
	msgs, err := s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message in inbox before archive, got %d", len(msgs))
	}
	count, err := s.CountPending("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 pending before archive, got %d", count)
	}

	if _, err := s.ArchiveThread("thread-arc-2"); err != nil {
		t.Fatal(err)
	}

	msgs, err = s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected archived thread excluded from Inbox, got %d messages", len(msgs))
	}
	count, err = s.CountPending("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected archived+unread message to not count as pending, got %d", count)
	}

	all, err := s.InboxAll("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != id {
		t.Fatalf("expected InboxAll to include the archived thread's message %s, got %+v", id, all)
	}
	if all[0].ArchivedAt == nil {
		t.Error("expected ArchivedAt set on the message returned by InboxAll")
	}
}

// TestUnarchiveThreadRestoresInboxListing verifies --unarchive reverses
// ArchiveThread's effect on Inbox visibility.
func TestUnarchiveThreadRestoresInboxListing(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	if _, err := s.SendMessageWithThread("agent1", "autarch", "Test", "body", 2, "notification", "thread-arc-3"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveThread("thread-arc-3"); err != nil {
		t.Fatal(err)
	}

	msgs, err := s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected excluded after archive, got %d", len(msgs))
	}

	n, err := s.UnarchiveThread("thread-arc-3")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 message unarchived, got %d", n)
	}

	msgs, err = s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message back in inbox after unarchive, got %d", len(msgs))
	}
	if msgs[0].ArchivedAt != nil {
		t.Fatalf("expected ArchivedAt nil after unarchive, got %v", msgs[0].ArchivedAt)
	}
}

// TestThreadAlwaysReturnsArchivedContent verifies Thread (the backing
// query for "mail thread") never filters on archived_at — a pure read of
// thread history must not hide it, only listings (Inbox) do that.
func TestThreadAlwaysReturnsArchivedContent(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id, err := s.SendMessageWithThread("agent1", "autarch", "Test", "body", 2, "notification", "thread-arc-4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveThread("thread-arc-4"); err != nil {
		t.Fatal(err)
	}

	msgs, err := s.Thread("thread-arc-4")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].ID != id {
		t.Fatalf("expected Thread to still return the archived message, got %+v", msgs)
	}
	if msgs[0].ArchivedAt == nil {
		t.Error("expected ArchivedAt set on the message returned by Thread")
	}
}

// TestArchiveThreadUnknownReturnsZero verifies archiving a thread_id with
// no messages is a no-op that reports 0 rather than erroring — the caller
// (cmd/mail.go's `mail archive`) treats 0 as "thread not found".
func TestArchiveThreadUnknownReturnsZero(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	n, err := s.ArchiveThread("thread-does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 for unknown thread, got %d", n)
	}
}

// TestArchiveThreadRollsBackOnFailure verifies the state-mutation
// convention (docs/conventions/state-mutation.md #4): a failure partway
// through archiving a multi-message thread must not leave some messages
// archived and others not. ArchiveThread relies on SQLite applying a
// single UPDATE statement's row changes atomically — this test forces a
// mid-statement failure with a trigger and confirms no row was archived.
func TestArchiveThreadRollsBackOnFailure(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	id1, err := s.SendMessageWithThread("sol-dev/Nova", "autarch", "One", "", 2, "notification", "thread-fail-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AckMessage(id1); err != nil {
		t.Fatal(err)
	}
	id2, err := s.SendMessageWithThread("autarch", "sol-dev/Nova", "Two", "", 2, "notification", "thread-fail-1")
	if err != nil {
		t.Fatal(err)
	}

	// Install a trigger that aborts the UPDATE the moment it touches id2's
	// archived_at column. id2 is a store-generated ID under test control,
	// not user input, so splicing it into the trigger body with
	// fmt.Sprintf is safe here (mirrors columnExists' use of
	// fmt.Sprintf for schema-internal identifiers elsewhere in this
	// package).
	triggerSQL := fmt.Sprintf(`
		CREATE TRIGGER fail_archive_id2
		BEFORE UPDATE OF archived_at ON messages
		WHEN NEW.id = %q
		BEGIN
			SELECT RAISE(ABORT, 'simulated failure');
		END;
	`, id2)
	if _, err := s.db.Exec(triggerSQL); err != nil {
		t.Fatalf("failed to install test trigger: %v", err)
	}

	if _, err := s.ArchiveThread("thread-fail-1"); err == nil {
		t.Fatal("expected ArchiveThread to return an error when the trigger aborts")
	}

	// Neither message should be archived: SQLite aborts and rolls back the
	// whole statement, including any row changes already applied before
	// the trigger fired (row processing order within one UPDATE is
	// unspecified, so id1 could be processed either before or after id2).
	msgs, err := s.Thread("thread-fail-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages in thread, got %d", len(msgs))
	}
	for _, m := range msgs {
		if m.ArchivedAt != nil {
			t.Errorf("expected message %s to remain unarchived after simulated failure, got archived_at=%v", m.ID, m.ArchivedAt)
		}
	}
}

// TestCountPurgeCandidatesRequiresSelector and
// TestPurgeMessagesRequiresSelector verify the zero-value PurgeFilter is
// rejected rather than silently matching every message.
func TestCountPurgeCandidatesRequiresSelector(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	if _, err := s.CountPurgeCandidates(PurgeFilter{}); err == nil {
		t.Fatal("expected error for empty PurgeFilter")
	}
}

func TestPurgeMessagesRequiresSelector(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	if _, err := s.PurgeMessages(PurgeFilter{}); err == nil {
		t.Fatal("expected error for empty PurgeFilter")
	}
}

// TestPurgeMessagesArchivedDeletesRegardlessOfAckState verifies
// RequireArchived alone matches archived messages independent of ack/read
// state, while never touching messages that are neither archived nor
// acked.
func TestPurgeMessagesArchivedDeletesRegardlessOfAckState(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Archived + unacked + unread — purgeable via --archived alone.
	archivedID, err := s.SendMessageWithThread("agent1", "autarch", "Unacked archived", "", 2, "notification", "thread-purge-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveThread("thread-purge-1"); err != nil {
		t.Fatal(err)
	}

	// Acked, not archived — must NOT be deleted by RequireArchived alone.
	ackedID, err := s.SendMessage("agent2", "autarch", "Acked only", "", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AckMessage(ackedID); err != nil {
		t.Fatal(err)
	}

	// Pending, not archived, not acked — must never be deleted.
	untouchedID, err := s.SendMessage("agent3", "autarch", "Untouched", "", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}

	n, err := s.PurgeMessages(PurgeFilter{RequireArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 purged, got %d", n)
	}

	remaining, err := s.ListMessages(MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	present := map[string]bool{}
	for _, m := range remaining {
		present[m.ID] = true
	}
	if present[archivedID] {
		t.Errorf("expected archived message %s to be purged", archivedID)
	}
	if !present[ackedID] {
		t.Errorf("expected acked-only message %s to survive an --archived-only purge", ackedID)
	}
	if !present[untouchedID] {
		t.Errorf("expected untouched message %s to survive purge", untouchedID)
	}
}

// TestPurgeMessagesArchivedBeforeOnlyMatchesOlderCutoff verifies
// ArchivedBefore (--older-than) narrows RequireArchived to threads
// archived more than the cutoff ago, leaving recently-archived threads
// alone.
func TestPurgeMessagesArchivedBeforeOnlyMatchesOlderCutoff(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	oldID, err := s.SendMessageWithThread("agent1", "autarch", "Old", "", 2, "notification", "thread-purge-old")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveThread("thread-purge-old"); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	if _, err := s.db.Exec(`UPDATE messages SET archived_at = ? WHERE id = ?`, oldTime, oldID); err != nil {
		t.Fatal(err)
	}

	recentID, err := s.SendMessageWithThread("agent2", "autarch", "Recent", "", 2, "notification", "thread-purge-recent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveThread("thread-purge-recent"); err != nil {
		t.Fatal(err)
	}

	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	n, err := s.PurgeMessages(PurgeFilter{RequireArchived: true, ArchivedBefore: &cutoff})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 purged (only the old one), got %d", n)
	}

	remaining, err := s.ListMessages(MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != recentID {
		t.Fatalf("expected only %s to remain, got %+v", recentID, remaining)
	}
}

// TestPurgeMessagesComposesArchivedAndAckedByIntersection verifies
// combining RequireAcked and RequireArchived narrows to messages matching
// BOTH — the "composable with the existing acked semantics" requirement.
func TestPurgeMessagesComposesArchivedAndAckedByIntersection(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Archived but not acked — excluded when both dimensions are required.
	archivedOnlyID, err := s.SendMessageWithThread("agent1", "autarch", "Archived only", "", 2, "notification", "thread-both-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveThread("thread-both-1"); err != nil {
		t.Fatal(err)
	}

	// Acked but not archived — excluded when both dimensions are required.
	ackedOnlyID, err := s.SendMessage("agent2", "autarch", "Acked only", "", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AckMessage(ackedOnlyID); err != nil {
		t.Fatal(err)
	}

	// Both archived and acked — the only message matching the intersection.
	bothID, err := s.SendMessageWithThread("agent3", "autarch", "Both", "", 2, "notification", "thread-both-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AckMessage(bothID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveThread("thread-both-2"); err != nil {
		t.Fatal(err)
	}

	n, err := s.PurgeMessages(PurgeFilter{RequireAcked: true, RequireArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 purged (acked AND archived), got %d", n)
	}

	remaining, err := s.ListMessages(MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	present := map[string]bool{}
	for _, m := range remaining {
		present[m.ID] = true
	}
	if !present[archivedOnlyID] {
		t.Error("expected archived-only message to survive an intersection purge")
	}
	if !present[ackedOnlyID] {
		t.Error("expected acked-only message to survive an intersection purge")
	}
	if present[bothID] {
		t.Error("expected the acked+archived message to be purged")
	}
}

// TestPurgeMessagesDismissedDeletesRegardlessOfAckState verifies
// RequireDismissed alone matches dismissed messages independent of
// ack/read state, while never touching messages that are neither dismissed
// nor matching another requested selector.
func TestPurgeMessagesDismissedDeletesRegardlessOfAckState(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Dismissed, unacked, unread — purgeable via --dismissed alone.
	dismissedID, err := s.SendMessage("agent1", "autarch", "Dismissed", "", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DismissMessage(dismissedID); err != nil {
		t.Fatal(err)
	}

	// Acked, not dismissed — must NOT be deleted by RequireDismissed alone.
	ackedID, err := s.SendMessage("agent2", "autarch", "Acked only", "", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AckMessage(ackedID); err != nil {
		t.Fatal(err)
	}

	// Archived, not dismissed — must NOT be deleted by RequireDismissed alone.
	archivedID, err := s.SendMessageWithThread("agent3", "autarch", "Archived only", "", 2, "notification", "thread-purge-dismissed-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveThread("thread-purge-dismissed-1"); err != nil {
		t.Fatal(err)
	}

	// Pending, untouched — must never be deleted.
	untouchedID, err := s.SendMessage("agent4", "autarch", "Untouched", "", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}

	n, err := s.PurgeMessages(PurgeFilter{RequireDismissed: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 purged, got %d", n)
	}

	remaining, err := s.ListMessages(MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	present := map[string]bool{}
	for _, m := range remaining {
		present[m.ID] = true
	}
	if present[dismissedID] {
		t.Errorf("expected dismissed message %s to be purged", dismissedID)
	}
	if !present[ackedID] {
		t.Errorf("expected acked-only message %s to survive a --dismissed-only purge", ackedID)
	}
	if !present[archivedID] {
		t.Errorf("expected archived-only message %s to survive a --dismissed-only purge", archivedID)
	}
	if !present[untouchedID] {
		t.Errorf("expected untouched message %s to survive purge", untouchedID)
	}
}

// TestPurgeMessagesDismissedComposesWithArchivedByIntersection verifies
// combining RequireDismissed and RequireArchived narrows to messages
// matching BOTH, mirroring the RequireAcked+RequireArchived intersection
// test above.
func TestPurgeMessagesDismissedComposesWithArchivedByIntersection(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	// Dismissed but not archived — excluded when both dimensions required.
	dismissedOnlyID, err := s.SendMessage("agent1", "autarch", "Dismissed only", "", 2, "notification")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DismissMessage(dismissedOnlyID); err != nil {
		t.Fatal(err)
	}

	// Archived but not dismissed — excluded when both dimensions required.
	archivedOnlyID, err := s.SendMessageWithThread("agent2", "autarch", "Archived only", "", 2, "notification", "thread-purge-dismissed-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveThread("thread-purge-dismissed-2"); err != nil {
		t.Fatal(err)
	}

	n, err := s.PurgeMessages(PurgeFilter{RequireDismissed: true, RequireArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 purged (no message is both dismissed and archived), got %d", n)
	}

	remaining, err := s.ListMessages(MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	present := map[string]bool{}
	for _, m := range remaining {
		present[m.ID] = true
	}
	if !present[dismissedOnlyID] {
		t.Error("expected dismissed-only message to survive an empty intersection purge")
	}
	if !present[archivedOnlyID] {
		t.Error("expected archived-only message to survive an empty intersection purge")
	}
	if len(remaining) != 2 {
		t.Fatalf("expected both messages to survive an empty intersection purge, got %d", len(remaining))
	}
}

// TestCountPurgeCandidatesMatchesPurgeCount verifies the dry-run preview
// count and the actual delete count agree for the same filter.
func TestCountPurgeCandidatesMatchesPurgeCount(t *testing.T) {
	t.Parallel()
	s := setupSphere(t)

	if _, err := s.SendMessageWithThread("agent1", "autarch", "Test", "", 2, "notification", "thread-count-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveThread("thread-count-1"); err != nil {
		t.Fatal(err)
	}

	n, err := s.CountPurgeCandidates(PurgeFilter{RequireArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected count 1, got %d", n)
	}

	purged, err := s.PurgeMessages(PurgeFilter{RequireArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	if int64(n) != purged {
		t.Fatalf("count %d != purged %d", n, purged)
	}
}
