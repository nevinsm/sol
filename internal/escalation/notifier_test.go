package escalation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
)

func testEscalation() store.Escalation {
	return store.Escalation{
		ID:          "esc-test0001",
		Severity:    "high",
		Source:      "haven/sentinel",
		Description: "Agent Toast stalled for 30 minutes",
		Status:      "open",
		CreatedAt:   time.Date(2026, 2, 27, 10, 30, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 2, 27, 10, 30, 0, 0, time.UTC),
	}
}

func setupSphereStore(t *testing.T) *store.SphereStore {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestLogNotifier(t *testing.T) {
	dir := t.TempDir()
	logger := events.NewLogger(dir)

	n := NewLogNotifier(logger)

	if n.Name() != "log" {
		t.Fatalf("expected name 'log', got %q", n.Name())
	}

	esc := testEscalation()
	err := n.Notify(context.Background(), esc)
	if err != nil {
		t.Fatal(err)
	}

	// Verify event was written to feed.
	data, err := os.ReadFile(filepath.Join(dir, ".events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "escalation_created") {
		t.Fatalf("expected escalation_created event in feed, got: %s", data)
	}
	if !strings.Contains(string(data), "esc-test0001") {
		t.Fatalf("expected escalation ID in feed, got: %s", data)
	}
}

func TestLogNotifierRenotify(t *testing.T) {
	dir := t.TempDir()
	logger := events.NewLogger(dir)

	n := NewLogNotifier(logger)

	// Set LastNotifiedAt to simulate a re-notification.
	esc := testEscalation()
	ts := time.Date(2026, 2, 27, 12, 0, 0, 0, time.UTC)
	esc.LastNotifiedAt = &ts

	err := n.Notify(context.Background(), esc)
	if err != nil {
		t.Fatal(err)
	}

	// Re-notification should emit consul_esc_renotified, not escalation_created.
	data, err := os.ReadFile(filepath.Join(dir, ".events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "escalation_created") {
		t.Fatalf("re-notification must not emit escalation_created, got: %s", data)
	}
	if !strings.Contains(string(data), "consul_esc_renotified") {
		t.Fatalf("expected consul_esc_renotified event in feed, got: %s", data)
	}
	if !strings.Contains(string(data), "esc-test0001") {
		t.Fatalf("expected escalation ID in feed, got: %s", data)
	}
}

func TestLogNotifierNilLogger(t *testing.T) {
	n := NewLogNotifier(nil)

	esc := testEscalation()
	err := n.Notify(context.Background(), esc)
	if err != nil {
		t.Fatalf("expected nil error for nil logger, got: %v", err)
	}
}

func TestMailNotifier(t *testing.T) {
	s := setupSphereStore(t)

	n := NewMailNotifier(s)

	if n.Name() != "mail" {
		t.Fatalf("expected name 'mail', got %q", n.Name())
	}

	esc := testEscalation()
	err := n.Notify(context.Background(), esc)
	if err != nil {
		t.Fatal(err)
	}

	// Verify message sent to "autarch".
	msgs, err := s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message in inbox, got %d", len(msgs))
	}

	msg := msgs[0]
	if !strings.Contains(msg.Subject, "ESCALATION-high") {
		t.Fatalf("expected subject containing 'ESCALATION-high', got %q", msg.Subject)
	}
	if msg.Recipient != "autarch" {
		t.Fatalf("expected recipient 'autarch', got %q", msg.Recipient)
	}
	// High severity -> priority 1.
	if msg.Priority != 1 {
		t.Fatalf("expected priority 1 for high severity, got %d", msg.Priority)
	}
	// Verify ThreadID is set.
	expectedThreadID := EscalationThreadID(esc.ID)
	if msg.ThreadID != expectedThreadID {
		t.Fatalf("expected thread_id %q, got %q", expectedThreadID, msg.ThreadID)
	}

	// Test low severity -> priority 3.
	escLow := testEscalation()
	escLow.ID = "esc-test0002" // distinct ID to avoid dedup
	escLow.Severity = "low"
	err = n.Notify(context.Background(), escLow)
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ = s.Inbox("autarch")
	// Find the low-priority message.
	for _, m := range msgs {
		if strings.Contains(m.Subject, "ESCALATION-low") {
			if m.Priority != 3 {
				t.Fatalf("expected priority 3 for low severity, got %d", m.Priority)
			}
		}
	}
}

func TestMailNotifierSkipsDuplicate(t *testing.T) {
	s := setupSphereStore(t)

	n := NewMailNotifier(s)
	esc := testEscalation()

	// First notification should succeed.
	if err := n.Notify(context.Background(), esc); err != nil {
		t.Fatal(err)
	}

	// Second notification with the same escalation should be skipped
	// (pending message with same ThreadID exists).
	if err := n.Notify(context.Background(), esc); err != nil {
		t.Fatal(err)
	}

	// Only 1 message should exist.
	msgs, err := s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (dedup), got %d", len(msgs))
	}
}

// TestMailNotifierConcurrentDedupe simulates two senders racing on the
// same escalation. The DB-level partial UNIQUE index on
// messages(thread_id) for pending messages (sphere schema v16) must
// guarantee that exactly one mail row is inserted, even if the
// in-process mutex is bypassed (as it would be across two MailNotifier
// instances backed by the same sphere DB — the multi-consul scenario).
func TestMailNotifierConcurrentDedupe(t *testing.T) {
	s := setupSphereStore(t)

	// Two independent MailNotifier instances backed by the same store —
	// each has its own mutex, so within-process serialization does not
	// help. This is exactly the multi-process invariant we need to
	// defend.
	n1 := NewMailNotifier(s)
	n2 := NewMailNotifier(s)
	esc := testEscalation()

	const senders = 8
	var wg sync.WaitGroup
	errs := make(chan error, senders)
	wg.Add(senders)
	start := make(chan struct{})
	for i := range senders {
		notifier := n1
		if i%2 == 1 {
			notifier = n2
		}
		go func(n *MailNotifier) {
			defer wg.Done()
			<-start
			if err := n.Notify(context.Background(), esc); err != nil {
				errs <- err
			}
		}(notifier)
	}
	close(start) // release all goroutines simultaneously
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Notify returned error: %v", err)
	}

	// Exactly one pending mail message must exist for this escalation —
	// the partial UNIQUE index makes the dedupe atomic across senders.
	msgs, err := s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	threadID := EscalationThreadID(esc.ID)
	matched := 0
	for _, m := range msgs {
		if m.ThreadID == threadID {
			matched++
		}
	}
	if matched != 1 {
		t.Fatalf("expected exactly 1 pending message for thread %q after concurrent Notify, got %d (total msgs=%d)", threadID, matched, len(msgs))
	}
}

func TestMailNotifierSendsAfterAck(t *testing.T) {
	s := setupSphereStore(t)

	n := NewMailNotifier(s)
	esc := testEscalation()

	// First notification.
	if err := n.Notify(context.Background(), esc); err != nil {
		t.Fatal(err)
	}

	// Ack the message.
	msgs, _ := s.Inbox("autarch")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if err := s.AckMessage(msgs[0].ID); err != nil {
		t.Fatal(err)
	}

	// Second notification should succeed since the first was acked.
	if err := n.Notify(context.Background(), esc); err != nil {
		t.Fatal(err)
	}

	// Should have 1 pending message (the new one).
	msgs, err := s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 pending message after ack + re-send, got %d", len(msgs))
	}
}

func TestEscalationThreadID(t *testing.T) {
	got := EscalationThreadID("sol-abc123")
	want := "esc:sol-abc123"
	if got != want {
		t.Fatalf("EscalationThreadID = %q, want %q", got, want)
	}
}

func TestWebhookNotifier(t *testing.T) {
	var receivedBody []byte
	var receivedContentType string
	var receivedUserAgent string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		receivedUserAgent = r.Header.Get("User-Agent")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL)

	if n.Name() != "webhook" {
		t.Fatalf("expected name 'webhook', got %q", n.Name())
	}

	esc := testEscalation()
	err := n.Notify(context.Background(), esc)
	if err != nil {
		t.Fatal(err)
	}

	// Verify Content-Type and User-Agent headers.
	if receivedContentType != "application/json" {
		t.Fatalf("expected Content-Type 'application/json', got %q", receivedContentType)
	}
	if receivedUserAgent != "sol-escalation/1.0" {
		t.Fatalf("expected User-Agent 'sol-escalation/1.0', got %q", receivedUserAgent)
	}

	// Verify JSON body.
	var payload map[string]string
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("failed to parse webhook body: %v", err)
	}
	if payload["id"] != "esc-test0001" {
		t.Fatalf("expected id 'esc-test0001', got %q", payload["id"])
	}
	if payload["severity"] != "high" {
		t.Fatalf("expected severity 'high', got %q", payload["severity"])
	}
	if payload["source"] != "haven/sentinel" {
		t.Fatalf("expected source 'haven/sentinel', got %q", payload["source"])
	}
}

func TestWebhookNotifierServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL)
	esc := testEscalation()
	err := n.Notify(context.Background(), esc)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected error containing '500', got %q", err.Error())
	}
}

func TestWebhookNotifierTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL)
	n.Timeout = 50 * time.Millisecond

	esc := testEscalation()
	err := n.Notify(context.Background(), esc)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestRouterDefaultRouting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer sphereStore.Close()

	logger := events.NewLogger(dir)

	// Create a webhook server that counts requests.
	webhookCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	router := DefaultRouter(logger, sphereStore, srv.URL)

	// Route low -> only log fires (no mail, no webhook).
	escLow := testEscalation()
	escLow.ID = "esc-low-0001"
	escLow.Severity = "low"
	router.Route(context.Background(), escLow)

	msgs, _ := sphereStore.Inbox("autarch")
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages for low severity, got %d", len(msgs))
	}
	if webhookCalls != 0 {
		t.Fatalf("expected 0 webhook calls for low severity, got %d", webhookCalls)
	}

	// Route medium -> log + mail fire.
	escMed := testEscalation()
	escMed.ID = "esc-med-0001"
	escMed.Severity = "medium"
	router.Route(context.Background(), escMed)

	msgs, _ = sphereStore.Inbox("autarch")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for medium severity, got %d", len(msgs))
	}
	if webhookCalls != 0 {
		t.Fatalf("expected 0 webhook calls for medium severity, got %d", webhookCalls)
	}

	// Route high -> log + mail + webhook fire.
	escHigh := testEscalation()
	escHigh.ID = "esc-high-0001"
	escHigh.Severity = "high"
	router.Route(context.Background(), escHigh)

	msgs, _ = sphereStore.Inbox("autarch")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after high severity, got %d", len(msgs))
	}
	if webhookCalls != 1 {
		t.Fatalf("expected 1 webhook call for high severity, got %d", webhookCalls)
	}

	// Route critical -> log + mail + webhook fire.
	escCrit := testEscalation()
	escCrit.ID = "esc-crit-0001"
	escCrit.Severity = "critical"
	router.Route(context.Background(), escCrit)

	msgs, _ = sphereStore.Inbox("autarch")
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages after critical severity, got %d", len(msgs))
	}
	if webhookCalls != 2 {
		t.Fatalf("expected 2 webhook calls after critical severity, got %d", webhookCalls)
	}
}

func TestRouterNoWebhook(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer sphereStore.Close()

	logger := events.NewLogger(dir)

	// DefaultRouter with empty webhookURL.
	router := DefaultRouter(logger, sphereStore, "")

	// Route high -> log + mail fire (no webhook).
	esc := testEscalation()
	esc.Severity = "high"
	router.Route(context.Background(), esc)

	msgs, _ := sphereStore.Inbox("autarch")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for high severity without webhook, got %d", len(msgs))
	}
}

// failingNotifier is a test notifier that always returns an error.
type failingNotifier struct{}

func (f *failingNotifier) Notify(_ context.Context, _ store.Escalation) error {
	return context.DeadlineExceeded
}
func (f *failingNotifier) Name() string { return "failing" }

// recordingNotifier records whether Notify was called.
type recordingNotifier struct {
	called bool
}

func (r *recordingNotifier) Notify(_ context.Context, _ store.Escalation) error {
	r.called = true
	return nil
}
func (r *recordingNotifier) Name() string { return "recording" }

// ctxAwareNotifier records the context error at the time Notify is called.
type ctxAwareNotifier struct {
	ctxErr error
}

func (c *ctxAwareNotifier) Notify(ctx context.Context, _ store.Escalation) error {
	c.ctxErr = ctx.Err()
	return nil
}
func (c *ctxAwareNotifier) Name() string { return "ctx-aware" }

func TestRouterBestEffort(t *testing.T) {
	r := NewRouter()

	failing := &failingNotifier{}
	recording := &recordingNotifier{}

	r.AddRule("high", failing, recording)

	esc := testEscalation()
	esc.Severity = "high"
	err := r.Route(context.Background(), esc)

	// First error returned.
	if err == nil {
		t.Fatal("expected error from failing notifier")
	}

	// But recording notifier still fired.
	if !recording.called {
		t.Fatal("expected recording notifier to still fire despite failure")
	}
}

// --- Router edge cases ---

func TestRouterUnknownSeverityReturnsError(t *testing.T) {
	r := NewRouter()
	recording := &recordingNotifier{}
	r.AddRule("high", recording)

	esc := testEscalation()
	esc.Severity = "unknown-severity"
	err := r.Route(context.Background(), esc)

	if err == nil {
		t.Fatal("expected error for unknown severity, got nil")
	}
	if !strings.Contains(err.Error(), "unknown escalation severity") {
		t.Fatalf("expected error about unknown severity, got: %v", err)
	}
	if recording.called {
		t.Fatal("expected no notifiers to fire for unknown severity")
	}
}

func TestRouterEmptyRouterReturnsError(t *testing.T) {
	r := NewRouter()

	esc := testEscalation()
	esc.Severity = "high"
	err := r.Route(context.Background(), esc)

	if err == nil {
		t.Fatal("expected error from empty router, got nil")
	}
	if !strings.Contains(err.Error(), "unknown escalation severity") {
		t.Fatalf("expected error about unknown severity, got: %v", err)
	}
}

func TestRouterContextCancellationPassedToNotifier(t *testing.T) {
	r := NewRouter()
	n := &ctxAwareNotifier{}
	r.AddRule("high", n)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	esc := testEscalation()
	esc.Severity = "high"
	r.Route(ctx, esc) //nolint:errcheck

	if n.ctxErr != context.Canceled {
		t.Fatalf("expected notifier to receive cancelled context, got %v", n.ctxErr)
	}
}

func TestRouterAddRuleAccumulates(t *testing.T) {
	r := NewRouter()

	recA := &recordingNotifier{}
	recB := &recordingNotifier{}

	r.AddRule("high", recA)
	r.AddRule("high", recB)

	esc := testEscalation()
	esc.Severity = "high"
	if err := r.Route(context.Background(), esc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !recA.called {
		t.Error("expected first notifier (recA) to fire")
	}
	if !recB.called {
		t.Error("expected second notifier (recB) to fire")
	}
}

// --- Mail notifier edge cases ---

func TestMailNotifierLongDescriptionTruncatesSubject(t *testing.T) {
	s := setupSphereStore(t)
	n := NewMailNotifier(s)

	esc := testEscalation()
	// Build a description that is exactly 100 runes.
	esc.Description = strings.Repeat("x", 100)
	esc.ID = "esc-long-001"

	if err := n.Notify(context.Background(), esc); err != nil {
		t.Fatal(err)
	}

	msgs, err := s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	// The subject should contain only 80 runes from the description.
	subject := msgs[0].Subject
	// Strip the "[ESCALATION-high] " prefix (18 chars) and check remaining length.
	prefix := "[ESCALATION-high] "
	if !strings.HasPrefix(subject, prefix) {
		t.Fatalf("expected subject to start with %q, got %q", prefix, subject)
	}
	descPart := subject[len(prefix):]
	if len([]rune(descPart)) != 80 {
		t.Fatalf("expected truncated description of 80 runes, got %d", len([]rune(descPart)))
	}
}

func TestMailNotifierEmptyDescription(t *testing.T) {
	s := setupSphereStore(t)
	n := NewMailNotifier(s)

	esc := testEscalation()
	esc.Description = ""
	esc.ID = "esc-empty-001"

	// Should not panic and should succeed.
	if err := n.Notify(context.Background(), esc); err != nil {
		t.Fatalf("unexpected error with empty description: %v", err)
	}

	msgs, err := s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for empty description, got %d", len(msgs))
	}
}

func TestMailNotifierSeverityPriorities(t *testing.T) {
	s := setupSphereStore(t)
	n := NewMailNotifier(s)

	cases := []struct {
		id       string
		severity string
		want     int
	}{
		{"esc-crit-p", "critical", 1},
		{"esc-high-p", "high", 1},
		{"esc-med-p", "medium", 2},
		{"esc-low-p", "low", 3},
	}

	for _, tc := range cases {
		esc := testEscalation()
		esc.ID = tc.id
		esc.Severity = tc.severity
		if err := n.Notify(context.Background(), esc); err != nil {
			t.Fatalf("severity %q: unexpected error: %v", tc.severity, err)
		}
	}

	msgs, err := s.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != len(cases) {
		t.Fatalf("expected %d messages, got %d", len(cases), len(msgs))
	}

	// Build a map from subject keyword to priority for easy lookup.
	priorityByEscID := make(map[string]int, len(msgs))
	for _, m := range msgs {
		// Extract escalation ID from thread ID ("esc:<id>").
		if len(m.ThreadID) > 4 {
			priorityByEscID[m.ThreadID[4:]] = m.Priority
		}
	}

	for _, tc := range cases {
		got, ok := priorityByEscID[tc.id]
		if !ok {
			t.Errorf("severity %q (id=%s): message not found", tc.severity, tc.id)
			continue
		}
		if got != tc.want {
			t.Errorf("severity %q: expected priority %d, got %d", tc.severity, tc.want, got)
		}
	}
}

// --- Webhook notifier edge cases ---

func TestWebhookNotifierCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	esc := testEscalation()
	err := n.Notify(ctx, esc)
	if err == nil {
		t.Fatal("expected error for already-cancelled context")
	}
}

func TestWebhookNotifierUnreachableURL(t *testing.T) {
	// Start a server then immediately close it so the URL is unreachable.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	url := srv.URL
	srv.Close() // closed before we notify

	n := NewWebhookNotifier(url)

	esc := testEscalation()
	err := n.Notify(context.Background(), esc)
	if err == nil {
		t.Fatal("expected error for unreachable webhook URL")
	}
}
