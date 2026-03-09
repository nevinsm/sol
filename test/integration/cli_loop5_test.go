package integration

import (
	"strings"
	"testing"
)

func TestCLIEscalateHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "escalate", "--help")
	if err != nil {
		t.Fatalf("sol escalate --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Create an escalation") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIEscalationListHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "escalation", "list", "--help")
	if err != nil {
		t.Fatalf("sol escalation list --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "List escalations") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIEscalationAckHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "escalation", "ack", "--help")
	if err != nil {
		t.Fatalf("sol escalation ack --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Acknowledge") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIEscalationResolveHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "escalation", "resolve", "--help")
	if err != nil {
		t.Fatalf("sol escalation resolve --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Resolve") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIHandoffHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "handoff", "--help")
	if err != nil {
		t.Fatalf("sol handoff --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Stop the current agent session") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIConsulRunHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "consul", "run", "--help")
	if err != nil {
		t.Fatalf("sol consul run --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Run the consul patrol loop") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIConsulStatusHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "consul", "status", "--help")
	if err != nil {
		t.Fatalf("sol consul status --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Show consul status") {
		t.Errorf("output missing expected text: %s", out)
	}
}

func TestCLIPrefectRunConsulFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "prefect", "run", "--help")
	if err != nil {
		t.Fatalf("sol prefect run --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "--consul") {
		t.Errorf("prefect run help missing --consul flag: %s", out)
	}
	if !strings.Contains(out, "--source-repo") {
		t.Errorf("prefect run help missing --source-repo flag: %s", out)
	}
}
