package integration

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/events"
)

func TestCLIFeedHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "feed", "--help")
	if err != nil {
		t.Fatalf("sol feed --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "event activity feed") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLILogEventHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "log-event", "--help")
	if err != nil {
		t.Fatalf("sol log-event --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Log a custom event") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIMailSendHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "mail", "send", "--help")
	if err != nil {
		t.Fatalf("sol mail send --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Send a message") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIMailInboxHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "mail", "inbox", "--help")
	if err != nil {
		t.Fatalf("sol mail inbox --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "pending messages") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIMailReadHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "mail", "read", "--help")
	if err != nil {
		t.Fatalf("sol mail read --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Read a message") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIMailAckHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "mail", "ack", "--help")
	if err != nil {
		t.Fatalf("sol mail ack --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Acknowledge") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIMailCheckHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "mail", "check", "--help")
	if err != nil {
		t.Fatalf("sol mail check --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "unread") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIChronicleRunHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "chronicle", "run", "--help")
	if err != nil {
		t.Fatalf("sol chronicle run --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Run the chronicle") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIChronicleStartHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "chronicle", "start", "--help")
	if err != nil {
		t.Fatalf("sol chronicle start --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "background process") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIChronicleStopHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "chronicle", "stop", "--help")
	if err != nil {
		t.Fatalf("sol chronicle stop --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Stop the chronicle") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLISentinelRunHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "sentinel", "run", "--help")
	if err != nil {
		t.Fatalf("sol sentinel run --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "patrol loop") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLISentinelStartHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "sentinel", "start", "--help")
	if err != nil {
		t.Fatalf("sol sentinel start --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "background process") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLISentinelStopHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "sentinel", "stop", "--help")
	if err != nil {
		t.Fatalf("sol sentinel stop --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Stop the sentinel") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLISentinelLogHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "sentinel", "log", "--help")
	if err != nil {
		t.Fatalf("sol sentinel log --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Show or tail the sentinel log") {
		t.Errorf("output missing expected text: %s", out)
	}
}

// TestCLIFeedFollow verifies that "sol feed --follow" streams events written
// after the command starts. This covers the CLI integration path for Follow mode.
func TestCLIFeedFollow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Create events file so the command can open it immediately.
	feedPath := filepath.Join(solHome, ".events.jsonl")
	if err := os.WriteFile(feedPath, nil, 0o644); err != nil {
		t.Fatalf("create feed file: %v", err)
	}

	bin := gtBin(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "feed", "--follow", "--raw")
	cmd.Env = append(os.Environ(), "SOL_HOME="+solHome)

	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start sol feed --follow: %v", err)
	}
	defer func() {
		cancel()
		cmd.Wait() //nolint:errcheck
	}()

	// Drain stdout into a mutex-protected buffer to avoid data races.
	var mu sync.Mutex
	var buf bytes.Buffer
	go func() {
		io.Copy(io.Writer(&lockedWriter{mu: &mu, buf: &buf}), pipe) //nolint:errcheck
	}()

	// Canary: repeatedly emit a uniquely identifiable event until the follower
	// picks it up. This replaces a fixed sleep(300ms) that was flaky on loaded
	// CI machines. We re-emit each poll iteration because the process may not
	// have finished seeking to end-of-file when the first canary is written.
	// The marker is used as the actor field so it appears in formatted output.
	logger := events.NewLogger(solHome)
	canaryMarker := "feed-canary-" + t.Name()
	if !pollUntil(10*time.Second, 100*time.Millisecond, func() bool {
		logger.Emit(events.EventPatrol, "sol", canaryMarker, "both", nil)
		mu.Lock()
		defer mu.Unlock()
		return strings.Contains(buf.String(), canaryMarker)
	}) {
		mu.Lock()
		stdout := buf.String()
		mu.Unlock()
		t.Fatalf("sol feed --follow did not emit canary event within 10s; got: %q", stdout)
	}

	// Now emit the real test event.
	logger.Emit(events.EventCast, "sol", "test-actor", "both", map[string]string{"item": "z"})

	// Poll for the event to appear in output (sol feed prints event type on each line).
	ok := pollUntil(15*time.Second, 200*time.Millisecond, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Contains(buf.String(), events.EventCast)
	})
	if !ok {
		mu.Lock()
		got := buf.String()
		mu.Unlock()
		t.Errorf("sol feed --follow did not output cast event within 5s; got: %q", got)
	}
}

// lockedWriter wraps a bytes.Buffer with a mutex for safe concurrent access.
type lockedWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (lw *lockedWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.buf.Write(p)
}
