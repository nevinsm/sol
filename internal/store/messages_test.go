package store

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestSendMessage(t *testing.T) {
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

func TestAckMessage(t *testing.T) {
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

func TestCountPending(t *testing.T) {
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

func TestListMessagesThreadIDPrefix(t *testing.T) {
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
	s.db.Exec(`UPDATE messages SET acked_at = ? WHERE id = ?`, oldTime, id1)
	s.db.Exec(`UPDATE messages SET acked_at = ? WHERE id = ?`, oldTime, id2)

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
	s := setupSphere(t)

	// Send messages: 2 pending, 1 acked.
	s.SendMessage("agent1", "autarch", "Pending 1", "", 2, "notification")
	s.SendMessage("agent2", "autarch", "Pending 2", "", 2, "notification")
	id3, _ := s.SendMessage("agent3", "autarch", "Acked", "", 2, "notification")
	s.AckMessage(id3)

	// Backdate the acked message.
	oldTime := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	s.db.Exec(`UPDATE messages SET acked_at = ? WHERE id = ?`, oldTime, id3)

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
