package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/store"
)

// --- Mail flow integration tests ---

func TestMailFlowLifecycle(t *testing.T) {
	gtHome, _ := setupTestEnv(t)
	_ = gtHome

	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// 1. Send message via store.
	id, err := s.SendMessage("haven/Toast", "autarch", "Task complete", "All tests pass", 2, "notification")
	if err != nil {
		t.Fatal(err)
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
		t.Fatalf("expected message %s, got %s", id, msgs[0].ID)
	}

	// 3. ReadMessage marks read.
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

	// 4. AckMessage sets acked.
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

	// 5. PurgeAllAcked cleans up.
	purged, err := s.PurgeAllAcked()
	if err != nil {
		t.Fatal(err)
	}
	if purged != 1 {
		t.Fatalf("expected 1 purged, got %d", purged)
	}

	// 6. Message is gone.
	msgs, err = s.ListMessages(store.MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after purge, got %d", len(msgs))
	}
}

func TestMailCountPendingAccuracy(t *testing.T) {
	gtHome, _ := setupTestEnv(t)
	_ = gtHome

	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Initially 0.
	count, err := s.CountPending("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 pending, got %d", count)
	}

	// Send 3 messages.
	id1, _ := s.SendMessage("agent1", "autarch", "Msg 1", "", 2, "notification")
	s.SendMessage("agent2", "autarch", "Msg 2", "", 2, "notification")
	id3, _ := s.SendMessage("agent3", "autarch", "Msg 3", "", 1, "notification")

	count, err = s.CountPending("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected 3 pending, got %d", count)
	}

	// Read one — still 3 pending (read doesn't affect pending count).
	s.ReadMessage(id1)
	count, err = s.CountPending("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected 3 pending after read, got %d", count)
	}

	// Ack one — now 2.
	s.AckMessage(id3)
	count, err = s.CountPending("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 pending after ack, got %d", count)
	}
}

func TestMailProtocolMessageSendAndFilter(t *testing.T) {
	gtHome, _ := setupTestEnv(t)
	_ = gtHome

	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Send protocol messages of different types.
	donePayload := store.AgentDonePayload{
		WritID:  "sol-test12345678",
		AgentID: "haven/Toast",
		Branch:  "outpost/Toast/sol-test12345678",
		World:   "haven",
	}
	_, err = s.SendProtocolMessage("haven/Toast", "haven/sentinel", store.ProtoAgentDone, donePayload)
	if err != nil {
		t.Fatal(err)
	}

	mergePayload := store.MergeReadyPayload{
		MergeRequestID: "mr-001",
		WritID:         "sol-test12345678",
		Branch:         "outpost/Toast/sol-test12345678",
	}
	_, err = s.SendProtocolMessage("haven/sentinel", "haven/forge", store.ProtoMergeReady, mergePayload)
	if err != nil {
		t.Fatal(err)
	}

	// Filter by AGENT_DONE for sentinel.
	msgs, err := s.PendingProtocol("haven/sentinel", store.ProtoAgentDone)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 AGENT_DONE for sentinel, got %d", len(msgs))
	}

	// Filter by MERGE_READY for forge.
	msgs, err = s.PendingProtocol("haven/forge", store.ProtoMergeReady)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 MERGE_READY for forge, got %d", len(msgs))
	}

	// Wrong recipient — empty result.
	msgs, err = s.PendingProtocol("haven/sentinel", store.ProtoMergeReady)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 MERGE_READY for sentinel, got %d", len(msgs))
	}
}

func TestMailPurgeBeforeDuration(t *testing.T) {
	gtHome, _ := setupTestEnv(t)
	_ = gtHome

	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Send and ack 3 messages.
	id1, _ := s.SendMessage("agent1", "autarch", "Old 1", "", 2, "notification")
	id2, _ := s.SendMessage("agent2", "autarch", "Old 2", "", 2, "notification")
	id3, _ := s.SendMessage("agent3", "autarch", "Recent", "", 2, "notification")
	s.AckMessage(id1)
	s.AckMessage(id2)
	s.AckMessage(id3)

	// Backdate id1 and id2 to simulate old messages.
	oldTime := time.Now().UTC().Add(-10 * 24 * time.Hour).Format(time.RFC3339)
	s.DB().Exec(`UPDATE messages SET acked_at = ? WHERE id = ?`, oldTime, id1)
	s.DB().Exec(`UPDATE messages SET acked_at = ? WHERE id = ?`, oldTime, id2)

	// Purge messages older than 7 days.
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	purged, err := s.PurgeAckedMessages(cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if purged != 2 {
		t.Fatalf("expected 2 purged, got %d", purged)
	}

	// id3 (recently acked) should still exist.
	msgs, err := s.ListMessages(store.MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 remaining, got %d", len(msgs))
	}
	if msgs[0].ID != id3 {
		t.Fatalf("expected %s to remain, got %s", id3, msgs[0].ID)
	}
}

func TestMailPurgeNeverDeletesPending(t *testing.T) {
	gtHome, _ := setupTestEnv(t)
	_ = gtHome

	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Send 2 pending and 1 acked.
	s.SendMessage("agent1", "autarch", "Pending 1", "", 2, "notification")
	s.SendMessage("agent2", "autarch", "Pending 2", "", 2, "notification")
	id3, _ := s.SendMessage("agent3", "autarch", "Acked", "", 2, "notification")
	s.AckMessage(id3)

	// Purge all acked.
	purged, err := s.PurgeAllAcked()
	if err != nil {
		t.Fatal(err)
	}
	if purged != 1 {
		t.Fatalf("expected 1 purged, got %d", purged)
	}

	// Both pending messages should remain.
	pending, err := s.CountPending("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if pending != 2 {
		t.Fatalf("expected 2 pending preserved, got %d", pending)
	}
}

// --- CLI purge integration tests ---

func TestMailPurgeCLIConfirm(t *testing.T) {
	gtHome, _ := setupTestEnv(t)

	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}

	// Send and ack a message.
	id, _ := s.SendMessage("agent1", "autarch", "Test", "", 2, "notification")
	s.AckMessage(id)
	s.Close()

	// Run purge with --all-acked --confirm via CLI.
	out, err := runGT(t, gtHome, "mail", "purge", "--all-acked", "--confirm")
	if err != nil {
		t.Fatalf("mail purge failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Purged 1 message(s)") {
		t.Fatalf("expected purge count in output, got: %s", out)
	}

	// Verify message is gone.
	s, err = store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	msgs, err := s.ListMessages(store.MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after purge, got %d", len(msgs))
	}
}

func TestMailPurgeCLIBeforeFlag(t *testing.T) {
	gtHome, _ := setupTestEnv(t)

	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}

	// Send and ack 2 messages, backdate one.
	id1, _ := s.SendMessage("agent1", "autarch", "Old", "", 2, "notification")
	id2, _ := s.SendMessage("agent2", "autarch", "Recent", "", 2, "notification")
	s.AckMessage(id1)
	s.AckMessage(id2)

	oldTime := time.Now().UTC().Add(-10 * 24 * time.Hour).Format(time.RFC3339)
	s.DB().Exec(`UPDATE messages SET acked_at = ? WHERE id = ?`, oldTime, id1)
	s.Close()

	// Run purge with --before=7d --confirm.
	out, err := runGT(t, gtHome, "mail", "purge", "--before=7d", "--confirm")
	if err != nil {
		t.Fatalf("mail purge failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Purged 1 message(s)") {
		t.Fatalf("expected 1 purged in output, got: %s", out)
	}

	// Verify recent message survives.
	s, err = store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	msgs, err := s.ListMessages(store.MessageFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 remaining, got %d", len(msgs))
	}
	if msgs[0].ID != id2 {
		t.Fatalf("expected %s to survive, got %s", id2, msgs[0].ID)
	}
}

func TestMailPurgeCLIRequiresFlag(t *testing.T) {
	gtHome, _ := setupTestEnv(t)

	// Running purge without --before or --all-acked should fail.
	out, err := runGT(t, gtHome, "mail", "purge", "--confirm")
	if err == nil {
		t.Fatalf("expected error when no purge mode specified, got: %s", out)
	}
	if !strings.Contains(out, "must specify") {
		t.Fatalf("expected helpful error message, got: %s", out)
	}
}

// --- Nudge integration tests ---

func TestNudgeEnqueuePeekDrain(t *testing.T) {
	setupTestEnv(t)
	session := "sol-test-Nova"

	// Enqueue a message.
	msg := nudge.Message{
		Sender:   "sentinel",
		Type:     "HEALTH",
		Subject:  "check passed",
		Priority: "normal",
	}
	if err := nudge.Enqueue(session, msg); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Peek shows count=1.
	count, err := nudge.Peek(session)
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 pending, got %d", count)
	}

	// Drain returns the message.
	messages, err := nudge.Drain(session)
	if err != nil {
		t.Fatalf("Drain failed: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Subject != "check passed" {
		t.Fatalf("expected subject 'check passed', got %q", messages[0].Subject)
	}

	// Peek now shows 0.
	count, err = nudge.Peek(session)
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 pending after drain, got %d", count)
	}
}

func TestNudgeTTLExpiry(t *testing.T) {
	setupTestEnv(t)
	session := "sol-test-Nova"

	// Enqueue a message with a very short TTL that's already expired.
	msg := nudge.Message{
		Sender:    "test",
		Type:      "info",
		Subject:   "short-lived",
		TTL:       "1ms",
		CreatedAt: time.Now().UTC().Add(-1 * time.Second),
	}
	if err := nudge.Enqueue(session, msg); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Drain should return empty (message expired).
	messages, err := nudge.Drain(session)
	if err != nil {
		t.Fatalf("Drain failed: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected 0 messages (expired), got %d", len(messages))
	}
}

func TestNudgeCleanupOrphanedClaimed(t *testing.T) {
	setupTestEnv(t)
	session := "sol-test-Nova"

	// Enqueue a message.
	msg := nudge.Message{
		Sender:  "test",
		Type:    "info",
		Subject: "orphan-test",
	}
	if err := nudge.Enqueue(session, msg); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Simulate an orphaned .claimed file by renaming.
	dir := filepath.Join(os.Getenv("SOL_HOME"), ".runtime", "nudge_queue", session)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	src := filepath.Join(dir, entries[0].Name())
	dst := src + ".claimed"
	if err := os.Rename(src, dst); err != nil {
		t.Fatalf("Rename failed: %v", err)
	}

	// Backdate the .claimed file to make it orphaned (>5 min old).
	oldTime := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(dst, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes failed: %v", err)
	}

	// Peek before cleanup — should be 0 (.claimed files don't count).
	count, err := nudge.Peek(session)
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 before cleanup, got %d", count)
	}

	// Cleanup should requeue the orphaned file.
	if err := nudge.Cleanup(session); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Now it should appear as pending again.
	count, err = nudge.Peek(session)
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 requeued, got %d", count)
	}

	// Drain to verify it's a real message.
	messages, err := nudge.Drain(session)
	if err != nil {
		t.Fatalf("Drain failed: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message after cleanup+drain, got %d", len(messages))
	}
	if messages[0].Subject != "orphan-test" {
		t.Fatalf("expected subject 'orphan-test', got %q", messages[0].Subject)
	}
}

// --- Mail bridge test (without live tmux) ---

func TestMailSendCLINoNotifySuppressesNudge(t *testing.T) {
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "haven")

	session := "sol-haven-Toast"

	// Send a message with --no-notify.
	out, err := runGT(t, gtHome, "mail", "send",
		"--to=Toast", "--subject=Test message", "--body=Hello",
		"--no-notify", "--world=haven")
	if err != nil {
		t.Fatalf("mail send failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Sent:") {
		t.Fatalf("expected 'Sent:' in output, got: %s", out)
	}

	// The nudge queue for this session should be empty (--no-notify).
	count, err := nudge.Peek(session)
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 nudges with --no-notify, got %d", count)
	}
}

func TestMailSendToOperatorNoNudge(t *testing.T) {
	gtHome, _ := setupTestEnv(t)

	// Send to "autarch" — should never generate a nudge.
	out, err := runGT(t, gtHome, "mail", "send",
		"--to=autarch", "--subject=Report", "--body=All done")
	if err != nil {
		t.Fatalf("mail send failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Sent:") {
		t.Fatalf("expected 'Sent:' in output, got: %s", out)
	}

	// Verify message exists in store.
	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	msgs, err := s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for operator, got %d", len(msgs))
	}
}
