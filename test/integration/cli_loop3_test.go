package integration

import (
	"strings"
	"testing"
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
