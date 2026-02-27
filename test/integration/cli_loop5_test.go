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

func TestCLIConsulRunHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "consul", "run", "--help")
	if err != nil {
		t.Fatalf("gt consul run --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Run the consul patrol loop") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIConsulStatusHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "consul", "status", "--help")
	if err != nil {
		t.Fatalf("gt consul status --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Show consul status") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIPrefectRunConsulFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "prefect", "run", "--help")
	if err != nil {
		t.Fatalf("gt prefect run --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "--consul") {
		t.Errorf("prefect run help missing --consul flag: %s", out)
	}
	if !strings.Contains(out, "--source-repo") {
		t.Errorf("prefect run help missing --source-repo flag: %s", out)
	}
}
