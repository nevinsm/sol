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

func setupSphereStore(t *testing.T) *store.Store {
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
