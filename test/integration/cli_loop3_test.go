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
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "mail", "check", "--help")
	if err != nil {
		t.Fatalf("sol mail check --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "unread") {
		t.Errorf("output missing expected text: %s", out)
	}
}

// TestCLIMailCheckBehavior is the behavioral counterpart to
// TestCLIMailCheckHelp (IT-M6): it exercises `sol mail check` end-to-end
// across both the empty-inbox and pending-mail scenarios.
//
// Per cmd/mail.go:
//   - When no unread messages exist, the command prints "No unread messages."
//     and exits 1.
//   - When unread messages exist, it prints "%d unread messages" and exits 0.
//
// The test also verifies that the mail content can be retrieved via
// `sol mail inbox` (the agent-side read path) while the count is non-zero,
// satisfying the writ's "asserts the mail content is presented" requirement,
// and confirms the count drops back to zero after the message is read+acked.
func TestCLIMailCheckBehavior(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	const (
		world    = "ember"
		agent    = "Toast"
		identity = world + "/" + agent
		subject  = "Reactor temperature climbing"
		body     = "Coolant pump 3 is reporting drift."
	)

	// === Phase 1: empty inbox ===
	// Before any mail is sent, `mail check` should print the empty message
	// and exit with code 1.
	out, err := runGT(t, gtHome, "mail", "check", "--identity="+identity)
	if err == nil {
		t.Errorf("expected mail check on empty inbox to exit with non-zero status, got nil err; output: %s", out)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError on empty inbox, got %T: %v", err, err)
	}
	if got := exitErr.ExitCode(); got != 1 {
		t.Errorf("empty inbox exit code = %d, want 1", got)
	}
	if !strings.Contains(out, "No unread messages") {
		t.Errorf("empty inbox output missing 'No unread messages': %q", out)
	}

	// === Phase 2: send a mail to the agent ===
	// --no-notify keeps the test free of nudge bridge concerns; check only
	// inspects the persistent message store.
	out, err = runGT(t, gtHome, "mail", "send",
		"--to="+agent, "--world="+world,
		"--subject="+subject, "--body="+body,
		"--no-notify")
	if err != nil {
		t.Fatalf("mail send failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Sent:") {
		t.Fatalf("mail send output missing 'Sent:': %q", out)
	}

	// === Phase 3: pending mail — check reports unread count, exit 0 ===
	out, err = runGT(t, gtHome, "mail", "check", "--identity="+identity)
	if err != nil {
		t.Fatalf("mail check on non-empty inbox failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "1 unread") {
		t.Errorf("pending inbox output missing '1 unread': %q", out)
	}
	// Defensive: ensure the empty-message path was not hit.
	if strings.Contains(out, "No unread messages") {
		t.Errorf("pending inbox should not report empty: %q", out)
	}

	// === Phase 4: agent reads its mail — content is presented ===
	// `sol mail inbox` is the read-side counterpart agents use to surface
	// pending mail. Its tabular output presents subject (and message ID for
	// ack/read flows). This satisfies the "asserts the mail content is
	// presented" arm of IT-M6.
	out, err = runGT(t, gtHome, "mail", "inbox", "--identity="+identity)
	if err != nil {
		t.Fatalf("mail inbox failed: %v: %s", err, out)
	}
	if !strings.Contains(out, subject) {
		t.Errorf("mail inbox output missing subject %q: %s", subject, out)
	}

	// Capture the message ID from the inbox listing so we can read+ack it.
	// Inbox columns: ID  FROM  PRIORITY  SUBJECT  AGE
	var msgID string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "msg-") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				msgID = fields[0]
				break
			}
		}
	}
	if msgID == "" {
		t.Fatalf("could not extract message ID from inbox output: %s", out)
	}

	// `mail read` presents the full message body (the writ's "mail content
	// is presented" requirement, end-to-end).
	out, err = runGT(t, gtHome, "mail", "read", msgID, "--identity="+identity)
	if err != nil {
		t.Fatalf("mail read failed: %v: %s", err, out)
	}
	if !strings.Contains(out, subject) {
		t.Errorf("mail read output missing subject %q: %s", subject, out)
	}
	if !strings.Contains(out, body) {
		t.Errorf("mail read output missing body %q: %s", body, out)
	}

	// === Phase 5: ack the message and confirm check returns to empty path ===
	// Note: `mail check` invokes CountPending which filters on
	// delivery='pending'. `mail read` flips read=1 but does NOT change
	// delivery, so the count still includes read-but-not-acked messages.
	// `mail ack` is what flips delivery to 'acked' and drops the count.
	out, err = runGT(t, gtHome, "mail", "ack", msgID, "--identity="+identity)
	if err != nil {
		t.Fatalf("mail ack failed: %v: %s", err, out)
	}

	out, err = runGT(t, gtHome, "mail", "check", "--identity="+identity)
	if err == nil {
		t.Errorf("expected mail check after ack to exit non-zero, got nil err; output: %s", out)
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if got := exitErr.ExitCode(); got != 1 {
			t.Errorf("post-ack empty-inbox exit code = %d, want 1", got)
		}
	} else {
		t.Errorf("expected *exec.ExitError after ack, got %T: %v", err, err)
	}
	if !strings.Contains(out, "No unread messages") {
		t.Errorf("post-ack inbox output missing 'No unread messages': %q", out)
	}
}

func TestCLIChronicleRunHelp(t *testing.T) {
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)
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
	skipUnlessIntegration(t)

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
	ok := pollUntil(defaultPollTimeout, defaultPollInterval, func() bool {
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
