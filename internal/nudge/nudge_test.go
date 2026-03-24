package nudge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create .runtime: %v", err)
	}
	return dir
}

func TestEnqueueCreatesFile(t *testing.T) {
	setupTestDir(t)

	msg := Message{
		Sender:   "sentinel",
		Type:     "info",
		Subject:  "health check",
		Body:     "All systems nominal",
		Priority: "normal",
	}

	if err := Enqueue("sol-dev-Nova", msg); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Verify file was created.
	count, err := Peek("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 pending message, got %d", count)
	}
}

func TestEnqueueSetsCreatedAt(t *testing.T) {
	setupTestDir(t)

	msg := Message{
		Sender:  "test",
		Type:    "info",
		Subject: "auto-timestamp",
	}

	before := time.Now().UTC()
	if err := Enqueue("sol-dev-Nova", msg); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}
	after := time.Now().UTC()

	messages, err := Drain("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Drain failed: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].CreatedAt.Before(before) || messages[0].CreatedAt.After(after) {
		t.Fatalf("CreatedAt %v not in expected range [%v, %v]", messages[0].CreatedAt, before, after)
	}
}

func TestEnqueuePreservesExplicitCreatedAt(t *testing.T) {
	setupTestDir(t)

	ts := time.Now().UTC().Add(-1 * time.Minute)
	msg := Message{
		Sender:    "test",
		Type:      "info",
		Subject:   "explicit-ts",
		CreatedAt: ts,
	}

	if err := Enqueue("sol-dev-Nova", msg); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	messages, err := Drain("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Drain failed: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if !messages[0].CreatedAt.Equal(ts) {
		t.Fatalf("expected CreatedAt %v, got %v", ts, messages[0].CreatedAt)
	}
}

func TestEnqueueCollisionAvoidance(t *testing.T) {
	setupTestDir(t)

	ts := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		msg := Message{
			Sender:    "test",
			Type:      "info",
			Subject:   "collision test",
			CreatedAt: ts, // same timestamp
		}
		if err := Enqueue("sol-dev-Nova", msg); err != nil {
			t.Fatalf("Enqueue #%d failed: %v", i, err)
		}
	}

	count, err := Peek("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 pending messages, got %d", count)
	}
}

func TestDrainReturnsMessagesInFIFOOrder(t *testing.T) {
	setupTestDir(t)

	subjects := []string{"first", "second", "third"}
	base := time.Now().UTC().Add(-1 * time.Minute)

	for i, subj := range subjects {
		msg := Message{
			Sender:    "test",
			Type:      "info",
			Subject:   subj,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}
		if err := Enqueue("sol-dev-Nova", msg); err != nil {
			t.Fatalf("Enqueue %q failed: %v", subj, err)
		}
	}

	messages, err := Drain("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Drain failed: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}
	for i, subj := range subjects {
		if messages[i].Subject != subj {
			t.Errorf("message %d: expected subject %q, got %q", i, subj, messages[i].Subject)
		}
	}
}

func TestDrainClearsQueue(t *testing.T) {
	setupTestDir(t)

	msg := Message{Sender: "test", Type: "info", Subject: "drain-me"}
	if err := Enqueue("sol-dev-Nova", msg); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	messages, err := Drain("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Drain failed: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	// Queue should be empty now.
	count, err := Peek("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 pending messages after drain, got %d", count)
	}
}

func TestDrainEmptyQueue(t *testing.T) {
	setupTestDir(t)

	messages, err := Drain("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Drain failed: %v", err)
	}
	if messages != nil {
		t.Fatalf("expected nil messages for empty queue, got %v", messages)
	}
}

func TestDrainDiscardsExpiredMessages(t *testing.T) {
	setupTestDir(t)

	// Enqueue a message that's already expired (created 2 hours ago, normal TTL = 30min).
	expired := Message{
		Sender:    "test",
		Type:      "info",
		Subject:   "old-news",
		Priority:  "normal",
		CreatedAt: time.Now().UTC().Add(-2 * time.Hour),
	}
	if err := Enqueue("sol-dev-Nova", expired); err != nil {
		t.Fatalf("Enqueue expired failed: %v", err)
	}

	// Enqueue a fresh message.
	fresh := Message{
		Sender:  "test",
		Type:    "info",
		Subject: "fresh",
	}
	if err := Enqueue("sol-dev-Nova", fresh); err != nil {
		t.Fatalf("Enqueue fresh failed: %v", err)
	}

	messages, err := Drain("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Drain failed: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message (expired discarded), got %d", len(messages))
	}
	if messages[0].Subject != "fresh" {
		t.Errorf("expected subject %q, got %q", "fresh", messages[0].Subject)
	}
}

func TestDrainUrgentTTL(t *testing.T) {
	setupTestDir(t)

	// Urgent message created 1 hour ago — should still be valid (TTL = 2h).
	msg := Message{
		Sender:    "test",
		Type:      "info",
		Subject:   "urgent-still-valid",
		Priority:  "urgent",
		CreatedAt: time.Now().UTC().Add(-1 * time.Hour),
	}
	if err := Enqueue("sol-dev-Nova", msg); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	messages, err := Drain("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Drain failed: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
}

func TestCustomTTL(t *testing.T) {
	setupTestDir(t)

	// Message with custom short TTL that's already expired.
	msg := Message{
		Sender:    "test",
		Type:      "info",
		Subject:   "short-lived",
		TTL:       "1s",
		CreatedAt: time.Now().UTC().Add(-5 * time.Second),
	}
	if err := Enqueue("sol-dev-Nova", msg); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	messages, err := Drain("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Drain failed: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected 0 messages (custom TTL expired), got %d", len(messages))
	}
}

func TestPeekNonexistentSession(t *testing.T) {
	setupTestDir(t)

	count, err := Peek("nonexistent-session")
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}
}

func TestPeekDoesNotClaimMessages(t *testing.T) {
	setupTestDir(t)

	msg := Message{Sender: "test", Type: "info", Subject: "peek-test"}
	if err := Enqueue("sol-dev-Nova", msg); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Peek multiple times.
	for i := 0; i < 3; i++ {
		count, err := Peek("sol-dev-Nova")
		if err != nil {
			t.Fatalf("Peek #%d failed: %v", i, err)
		}
		if count != 1 {
			t.Fatalf("Peek #%d: expected 1, got %d", i, count)
		}
	}

	// Messages should still be drainable.
	messages, err := Drain("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Drain failed: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message after peeks, got %d", len(messages))
	}
}

func TestCleanupRequeuesOrphanedClaimed(t *testing.T) {
	setupTestDir(t)

	msg := Message{Sender: "test", Type: "info", Subject: "orphan-test"}
	if err := Enqueue("sol-dev-Nova", msg); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Simulate an orphaned .claimed file by manually renaming.
	dir := filepath.Join(os.Getenv("SOL_HOME"), ".runtime", "nudge_queue", "sol-dev-Nova")
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

	// Backdate the .claimed file to make it appear orphaned.
	oldTime := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(dst, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes failed: %v", err)
	}

	if err := Cleanup("sol-dev-Nova"); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Should be requeued as a pending message.
	count, err := Peek("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 requeued message, got %d", count)
	}
}

func TestCleanupDeletesExpired(t *testing.T) {
	setupTestDir(t)

	msg := Message{
		Sender:    "test",
		Type:      "info",
		Subject:   "expired",
		Priority:  "normal",
		CreatedAt: time.Now().UTC().Add(-1 * time.Hour),
	}
	if err := Enqueue("sol-dev-Nova", msg); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	if err := Cleanup("sol-dev-Nova"); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	count, err := Peek("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 after cleanup of expired, got %d", count)
	}
}

func TestCleanupNonexistentSession(t *testing.T) {
	setupTestDir(t)

	if err := Cleanup("nonexistent"); err != nil {
		t.Fatalf("Cleanup on nonexistent session should not error: %v", err)
	}
}

func TestCleanupPreservesValidMessages(t *testing.T) {
	setupTestDir(t)

	msg := Message{
		Sender:  "test",
		Type:    "info",
		Subject: "still-valid",
	}
	if err := Enqueue("sol-dev-Nova", msg); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	if err := Cleanup("sol-dev-Nova"); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	count, err := Peek("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 valid message preserved, got %d", count)
	}
}

func TestCleanupDoesNotRequeueRecentClaimed(t *testing.T) {
	setupTestDir(t)

	msg := Message{Sender: "test", Type: "info", Subject: "recent-claim"}
	if err := Enqueue("sol-dev-Nova", msg); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Simulate a .claimed file that was just claimed (recent mod time).
	dir := filepath.Join(os.Getenv("SOL_HOME"), ".runtime", "nudge_queue", "sol-dev-Nova")
	entries, _ := os.ReadDir(dir)
	src := filepath.Join(dir, entries[0].Name())
	dst := src + ".claimed"
	os.Rename(src, dst)

	if err := Cleanup("sol-dev-Nova"); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Should still be claimed, not requeued.
	count, err := Peek("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 (still claimed), got %d", count)
	}
}

func TestMessageJSON(t *testing.T) {
	msg := Message{
		Sender:    "sentinel",
		Type:      "health",
		Subject:   "check",
		Body:      "OK",
		Priority:  "normal",
		CreatedAt: time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
		TTL:       "30m",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Sender != msg.Sender {
		t.Errorf("Sender: expected %q, got %q", msg.Sender, decoded.Sender)
	}
	if decoded.Subject != msg.Subject {
		t.Errorf("Subject: expected %q, got %q", msg.Subject, decoded.Subject)
	}
	if !decoded.CreatedAt.Equal(msg.CreatedAt) {
		t.Errorf("CreatedAt: expected %v, got %v", msg.CreatedAt, decoded.CreatedAt)
	}
}

func TestListReturnsMessagesWithoutClaiming(t *testing.T) {
	setupTestDir(t)

	subjects := []string{"alpha", "beta", "gamma"}
	base := time.Now().UTC().Add(-1 * time.Minute)

	for i, subj := range subjects {
		msg := Message{
			Sender:    "test",
			Type:      "info",
			Subject:   subj,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}
		if err := Enqueue("sol-dev-Nova", msg); err != nil {
			t.Fatalf("Enqueue %q failed: %v", subj, err)
		}
	}

	// List should return all messages in FIFO order.
	messages, err := List("sol-dev-Nova")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}
	for i, subj := range subjects {
		if messages[i].Subject != subj {
			t.Errorf("message %d: expected subject %q, got %q", i, subj, messages[i].Subject)
		}
	}

	// List should NOT claim messages — count should remain 3.
	count, err := Peek("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 pending after List, got %d", count)
	}
}

func TestListSkipsExpiredMessages(t *testing.T) {
	setupTestDir(t)

	// Enqueue an expired message (normal TTL = 30m).
	expired := Message{
		Sender:    "test",
		Type:      "info",
		Subject:   "old",
		CreatedAt: time.Now().UTC().Add(-1 * time.Hour),
	}
	if err := Enqueue("sol-dev-Nova", expired); err != nil {
		t.Fatalf("Enqueue expired failed: %v", err)
	}

	// Enqueue a fresh message.
	fresh := Message{
		Sender:    "test",
		Type:      "info",
		Subject:   "new",
		CreatedAt: time.Now().UTC(),
	}
	if err := Enqueue("sol-dev-Nova", fresh); err != nil {
		t.Fatalf("Enqueue fresh failed: %v", err)
	}

	messages, err := List("sol-dev-Nova")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message (expired filtered), got %d", len(messages))
	}
	if messages[0].Subject != "new" {
		t.Errorf("expected subject %q, got %q", "new", messages[0].Subject)
	}
}

func TestListEmptyQueue(t *testing.T) {
	setupTestDir(t)

	messages, err := List("sol-dev-Nova")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(messages))
	}
}

func TestDeliverNonexistentSessionReturnsNil(t *testing.T) {
	setupTestDir(t)

	// Deliver on a session that doesn't exist should return nil (best-effort).
	msg := Message{Sender: "test", Type: "info", Subject: "no-session"}
	if err := Deliver("nonexistent-session", msg); err != nil {
		t.Fatalf("Deliver on nonexistent session should return nil, got: %v", err)
	}
}

func TestFormatNotification(t *testing.T) {
	tests := []struct {
		name     string
		msg      Message
		expected string
	}{
		{
			name:     "with subject",
			msg:      Message{Sender: "sentinel", Type: "HEALTH", Subject: "check passed"},
			expected: "[HEALTH] sentinel: check passed",
		},
		{
			name:     "without subject",
			msg:      Message{Sender: "forge", Type: "MR_READY"},
			expected: "[MR_READY] forge",
		},
		{
			name:     "with subject and body",
			msg:      Message{Sender: "sentinel", Type: "HEALTH", Subject: "check failed", Body: "Disk usage at 95%\nMemory nominal"},
			expected: "[HEALTH] sentinel: check failed\nDisk usage at 95%\nMemory nominal",
		},
		{
			name:     "with body no subject",
			msg:      Message{Sender: "consul", Type: "ALERT", Body: "orphaned tether detected"},
			expected: "[ALERT] consul\norphaned tether detected",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatNotification(tt.msg)
			if got != tt.expected {
				t.Errorf("formatNotification() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestMultipleSessionsIndependent(t *testing.T) {
	setupTestDir(t)

	msg1 := Message{Sender: "test", Type: "info", Subject: "for-nova"}
	msg2 := Message{Sender: "test", Type: "info", Subject: "for-lyra"}

	if err := Enqueue("sol-dev-Nova", msg1); err != nil {
		t.Fatalf("Enqueue Nova failed: %v", err)
	}
	if err := Enqueue("sol-dev-Lyra", msg2); err != nil {
		t.Fatalf("Enqueue Lyra failed: %v", err)
	}

	// Drain Nova — should only get Nova's message.
	novaMessages, err := Drain("sol-dev-Nova")
	if err != nil {
		t.Fatalf("Drain Nova failed: %v", err)
	}
	if len(novaMessages) != 1 || novaMessages[0].Subject != "for-nova" {
		t.Fatalf("unexpected Nova messages: %v", novaMessages)
	}

	// Lyra's queue should be untouched.
	lyraCount, err := Peek("sol-dev-Lyra")
	if err != nil {
		t.Fatalf("Peek Lyra failed: %v", err)
	}
	if lyraCount != 1 {
		t.Fatalf("expected 1 Lyra message, got %d", lyraCount)
	}
}
