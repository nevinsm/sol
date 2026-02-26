package integration

import (
	"strings"
	"testing"
)

func TestCLIFeedHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "feed", "--help")
	if err != nil {
		t.Fatalf("gt feed --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "event activity feed") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLILogEventHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "log-event", "--help")
	if err != nil {
		t.Fatalf("gt log-event --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Log a custom event") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIMailSendHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "mail", "send", "--help")
	if err != nil {
		t.Fatalf("gt mail send --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Send a message") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIMailInboxHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "mail", "inbox", "--help")
	if err != nil {
		t.Fatalf("gt mail inbox --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "pending messages") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIMailReadHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "mail", "read", "--help")
	if err != nil {
		t.Fatalf("gt mail read --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Read a message") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIMailAckHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "mail", "ack", "--help")
	if err != nil {
		t.Fatalf("gt mail ack --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Acknowledge") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIMailCheckHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "mail", "check", "--help")
	if err != nil {
		t.Fatalf("gt mail check --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "unread") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLICuratorRunHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "curator", "run", "--help")
	if err != nil {
		t.Fatalf("gt curator run --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Run the curator") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLICuratorStartHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "curator", "start", "--help")
	if err != nil {
		t.Fatalf("gt curator start --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "background tmux session") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLICuratorStopHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "curator", "stop", "--help")
	if err != nil {
		t.Fatalf("gt curator stop --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Stop the curator") {
		t.Errorf("output missing expected text: %s", out)
	}
}
