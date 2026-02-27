package integration

import (
	"strings"
	"testing"
)

func TestCLIEscalateHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "escalate", "--help")
	if err != nil {
		t.Fatalf("gt escalate --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Create an escalation") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIEscalationListHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "escalation", "list", "--help")
	if err != nil {
		t.Fatalf("gt escalation list --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "List escalations") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIEscalationAckHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "escalation", "ack", "--help")
	if err != nil {
		t.Fatalf("gt escalation ack --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Acknowledge") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIEscalationResolveHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "escalation", "resolve", "--help")
	if err != nil {
		t.Fatalf("gt escalation resolve --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Resolve") {
		t.Errorf("output missing expected text: %s", out)
	}
}
