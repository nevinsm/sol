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

func TestCLIHandoffHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "handoff", "--help")
	if err != nil {
		t.Fatalf("gt handoff --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Hand off") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIDeaconRunHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "deacon", "run", "--help")
	if err != nil {
		t.Fatalf("gt deacon run --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Run the deacon patrol loop") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIDeaconStatusHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "deacon", "status", "--help")
	if err != nil {
		t.Fatalf("gt deacon status --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Show deacon status") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLISupervisorRunDeaconFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "supervisor", "run", "--help")
	if err != nil {
		t.Fatalf("gt supervisor run --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "--deacon") {
		t.Errorf("supervisor run help missing --deacon flag: %s", out)
	}
	if !strings.Contains(out, "--source-repo") {
		t.Errorf("supervisor run help missing --source-repo flag: %s", out)
	}
}
