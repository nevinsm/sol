package store

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestSendMessage(t *testing.T) {
	s := setupSphere(t)

	id, err := s.SendMessage("haven/Toast", "operator", "Work done", "Finished task sol-abc12345", 2, "notification")
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
	if msg.Recipient != "operator" {
		t.Fatalf("expected recipient 'operator', got %q", msg.Recipient)
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

	// Send 3 messages to "operator" with different priorities.
	s.SendMessage("agent1", "operator", "Low priority", "", 3, "notification")
	s.SendMessage("agent2", "operator", "Urgent", "", 1, "notification")
	s.SendMessage("agent3", "operator", "Normal", "", 2, "notification")

	// Inbox should return all 3, ordered by priority then age.
	msgs, err := s.Inbox("operator")
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
	msgs, err = s.Inbox("operator")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages for operator, got %d", len(msgs))
	}

	// Ack one message -> no longer in inbox.
	s.AckMessage(msgs[0].ID)
	msgs, err = s.Inbox("operator")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after ack, got %d", len(msgs))
	}
}

func TestReadMessage(t *testing.T) {
	s := setupSphere(t)

	id, _ := s.SendMessage("agent1", "operator", "Test", "Body", 2, "notification")

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

	id, _ := s.SendMessage("agent1", "operator", "Test", "", 2, "notification")

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
	msgs, _ := s.Inbox("operator")
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages in inbox after ack, got %d", len(msgs))
	}
}

func TestCountPending(t *testing.T) {
	s := setupSphere(t)

	// No messages -> 0.
	count, err := s.CountPending("operator")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}

	// Send 3 messages -> 3.
	id1, _ := s.SendMessage("agent1", "operator", "Msg 1", "", 2, "notification")
	s.SendMessage("agent2", "operator", "Msg 2", "", 2, "notification")
	id3, _ := s.SendMessage("agent3", "operator", "Msg 3", "", 2, "notification")

	count, err = s.CountPending("operator")
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected 3, got %d", count)
	}

	// Read one -> still 3 (read doesn't affect count, only ack does).
	s.ReadMessage(id1)
	count, err = s.CountPending("operator")
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected 3 after read, got %d", count)
	}

	// Ack one -> 2.
	s.AckMessage(id3)
	count, err = s.CountPending("operator")
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
	s.SendMessage("agent1", "operator", "Notif 1", "", 2, "notification")
	s.SendMessage("agent2", "operator", "Proto 1", "{}", 1, "protocol")
	s.SendMessage("agent3", "other", "Notif 2", "", 2, "notification")
	id4, _ := s.SendMessage("agent4", "operator", "Acked", "", 2, "notification")
	s.AckMessage(id4)

	// Filter by recipient -> only matching.
	msgs, err := s.ListMessages(MessageFilters{Recipient: "operator"})
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

	// Send messages with thread_id. Thread ID is stored as empty string by default,
	// so we need to insert manually with a thread_id to test filtering.
	s.SendMessage("agent1", "operator", "No thread", "", 2, "notification")

	// Insert a message with a thread_id directly.
	s.db.Exec(
		`INSERT INTO messages (id, sender, recipient, subject, body, priority, type, thread_id, delivery, read, created_at)
		 VALUES ('msg-thread01', 'agent2', 'operator', 'Threaded', '', 2, 'notification', 'thread-abc', 'pending', 0, '2025-01-01T00:00:00Z')`)

	// Filter by thread -> only the threaded message.
	msgs, err := s.ListMessages(MessageFilters{ThreadID: "thread-abc"})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 threaded message, got %d", len(msgs))
	}
	if msgs[0].ID != "msg-thread01" {
		t.Fatalf("expected msg-thread01, got %q", msgs[0].ID)
	}
}

func TestSendProtocolMessage(t *testing.T) {
	s := setupSphere(t)

	payload := AgentDonePayload{
		WorkItemID: "sol-abc12345",
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
	if parsed.WorkItemID != "sol-abc12345" {
		t.Fatalf("expected work_item_id 'sol-abc12345', got %q", parsed.WorkItemID)
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
