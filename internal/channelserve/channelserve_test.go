package channelserve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/nudge"
)

func setupTestHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create .runtime: %v", err)
	}
}

// syncBuffer is a concurrency-safe io.Writer wrapper around bytes.Buffer —
// Serve's poll loop and readLoop goroutine both write through the shared
// writer's mutex, but tests also read the buffer concurrently while Serve
// is still running, so reads need their own lock too.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// lines splits the buffer's current content into non-empty JSON lines.
func (b *syncBuffer) lines() []string {
	var out []string
	for _, l := range strings.Split(b.String(), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// failingWriter always errors, simulating a dead/disconnected client (the
// stdout pipe closed because the claude process exited mid-push).
type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) {
	return 0, errors.New("simulated write failure: client disconnected")
}

// openPipeIn returns an io.Reader that stays open (never EOFs) until the
// test ends, standing in for a live claude process's stdin connection.
// Serve returns as soon as opts.In reaches EOF (see readLoop) — using
// strings.NewReader("") in tests that need Serve to keep running past the
// first poll tick would exit Serve immediately instead of exercising it.
func openPipeIn(t *testing.T) io.Reader {
	t.Helper()
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	return pr
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func TestServeRespondsToInitializeWithChannelCapability(t *testing.T) {
	setupTestHome(t)
	const sess = "sol-dev-Nova"

	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	out := &syncBuffer{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Serve(ctx, Options{Session: sess, In: in, Out: out, Poll: 10 * time.Millisecond}) }()

	waitFor(t, 2*time.Second, func() bool { return len(out.lines()) >= 1 })

	var resp map[string]any
	if err := json.Unmarshal([]byte(out.lines()[0]), &resp); err != nil {
		t.Fatalf("failed to parse response: %v (raw: %s)", err, out.lines()[0])
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %+v", resp)
	}
	caps, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("expected capabilities object, got %+v", result)
	}
	exp, ok := caps["experimental"].(map[string]any)
	if !ok {
		t.Fatalf("expected experimental capabilities, got %+v", caps)
	}
	if _, ok := exp["claude/channel"]; !ok {
		t.Errorf("expected claude/channel experimental capability, got %+v", exp)
	}

	cancel()
	<-done
}

func TestServeMarksChannelAliveImmediately(t *testing.T) {
	setupTestHome(t)
	const sess = "sol-dev-Nova"

	in := openPipeIn(t)
	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Serve(ctx, Options{Session: sess, In: in, Out: out, Poll: time.Minute}) }()

	waitFor(t, 2*time.Second, func() bool { return nudge.ChannelAvailable(sess) })

	cancel()
	<-done
}

func TestServeDeliversPendingQueueMessagesAsChannelNotifications(t *testing.T) {
	setupTestHome(t)
	const sess = "sol-dev-Nova"

	if err := nudge.Enqueue(sess, nudge.Message{Sender: "autarch", Type: "info", Subject: "hello", Body: "from the queue"}); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	in := openPipeIn(t)
	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Serve(ctx, Options{Session: sess, In: in, Out: out, Poll: 10 * time.Millisecond}) }()

	waitFor(t, 2*time.Second, func() bool { return len(out.lines()) >= 1 })

	var notif map[string]any
	if err := json.Unmarshal([]byte(out.lines()[0]), &notif); err != nil {
		t.Fatalf("failed to parse notification: %v (raw: %s)", err, out.lines()[0])
	}
	if notif["method"] != "notifications/claude/channel" {
		t.Fatalf("expected a channel notification, got %+v", notif)
	}
	params, ok := notif["params"].(map[string]any)
	if !ok {
		t.Fatalf("expected params object, got %+v", notif)
	}
	content, _ := params["content"].(string)
	if !strings.Contains(content, "hello") || !strings.Contains(content, "from the queue") {
		t.Errorf("notification content missing expected message text: %q", content)
	}

	// Drain already consumed the message from the durable queue — pushing
	// it via channel must not leave it re-drainable (no duplicate delivery
	// via the doorbell/TurnBoundary path).
	n, err := nudge.Peek(sess)
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if n != 0 {
		t.Errorf("expected queue empty after successful channel push, got %d pending", n)
	}

	cancel()
	<-done
}

func TestServeReenqueuesOnPushFailure(t *testing.T) {
	setupTestHome(t)
	const sess = "sol-dev-Nova"

	if err := nudge.Enqueue(sess, nudge.Message{Sender: "autarch", Type: "info", Subject: "must-not-be-lost"}); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	in := openPipeIn(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, Options{Session: sess, In: in, Out: failingWriter{}, Poll: 10 * time.Millisecond})
	}()

	// The message must reappear in the durable queue after a failed push —
	// this is the "failure fallback" the routing design depends on: since
	// the push failed, the message is back in the queue for the next poll
	// tick (or, once the heartbeat goes stale, nudge.Deliver's doorbell
	// fallback) to pick up instead of being silently lost.
	waitFor(t, 2*time.Second, func() bool {
		n, err := nudge.Peek(sess)
		return err == nil && n >= 1
	})

	cancel()
	<-done
}

func TestServeReplyToolInvokesReplyCallback(t *testing.T) {
	setupTestHome(t)
	const sess = "sol-dev-Nova"

	var mu sync.Mutex
	var gotText string
	reply := func(text string) error {
		mu.Lock()
		defer mu.Unlock()
		gotText = text
		return nil
	}

	pr, pw := io.Pipe()
	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, Options{Session: sess, In: pr, Out: out, Poll: time.Minute, Reply: reply})
	}()

	req := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"reply","arguments":{"text":"ack from model"}}}` + "\n"
	if _, err := pw.Write([]byte(req)); err != nil {
		t.Fatalf("failed to write request: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return gotText != ""
	})

	mu.Lock()
	got := gotText
	mu.Unlock()
	if got != "ack from model" {
		t.Errorf("Reply callback got %q, want %q", got, "ack from model")
	}

	waitFor(t, 2*time.Second, func() bool { return len(out.lines()) >= 1 })
	var resp map[string]any
	if err := json.Unmarshal([]byte(out.lines()[0]), &resp); err != nil {
		t.Fatalf("failed to parse tools/call response: %v", err)
	}
	if _, ok := resp["error"]; ok {
		t.Errorf("expected a successful tools/call response, got %+v", resp)
	}

	pw.Close()
	cancel()
	<-done
}

func TestServeStopsOnStdinEOF(t *testing.T) {
	setupTestHome(t)
	const sess = "sol-dev-Nova"

	in := strings.NewReader("") // immediately EOF
	out := &syncBuffer{}

	done := make(chan error, 1)
	go func() {
		done <- Serve(context.Background(), Options{Session: sess, In: in, Out: out, Poll: time.Minute})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error on stdin EOF: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not stop after stdin EOF")
	}
}
