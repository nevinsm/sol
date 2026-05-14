package integration

import (
	"strings"
	"testing"
)

// TestCLIEscalateHelp is intentionally help-only.
// escalate requires an active agent session; behavioral coverage is in loop5_test.go.
func TestCLIEscalateHelp(t *testing.T) {
	skipUnlessIntegration(t)
	// t.TempDir() is sufficient — --help creates no tmux sessions.
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "escalate", "--help")
	if err != nil {
		t.Fatalf("sol escalate --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Create an escalation") {
		t.Errorf("output missing expected text: %s", out)
	}
}

// TestCLIEscalationListHelp is intentionally help-only.
// escalation list/ack/resolve are autarch-side commands; behavioral coverage is in loop5_test.go.
func TestCLIEscalationListHelp(t *testing.T) {
	skipUnlessIntegration(t)
	// t.TempDir() is sufficient — --help creates no tmux sessions.
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "escalation", "list", "--help")
	if err != nil {
		t.Fatalf("sol escalation list --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "List escalations") {
		t.Errorf("output missing expected text: %s", out)
	}
}

// TestCLIEscalationAckHelp is intentionally help-only.
// Behavioral coverage for escalation ack is in loop5_test.go.
func TestCLIEscalationAckHelp(t *testing.T) {
	skipUnlessIntegration(t)
	// t.TempDir() is sufficient — --help creates no tmux sessions.
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "escalation", "ack", "--help")
	if err != nil {
		t.Fatalf("sol escalation ack --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Acknowledge") {
		t.Errorf("output missing expected text: %s", out)
	}
}

// TestCLIEscalationResolveHelp is intentionally help-only.
// Behavioral coverage for escalation resolve is in loop5_test.go.
func TestCLIEscalationResolveHelp(t *testing.T) {
	skipUnlessIntegration(t)
	// t.TempDir() is sufficient — --help creates no tmux sessions.
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "escalation", "resolve", "--help")
	if err != nil {
		t.Fatalf("sol escalation resolve --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Resolve") {
		t.Errorf("output missing expected text: %s", out)
	}
}

// TestCLIHandoffHelp is intentionally help-only.
// handoff requires a live agent session; behavioral coverage is in loop5_test.go.
func TestCLIHandoffHelp(t *testing.T) {
	skipUnlessIntegration(t)
	// t.TempDir() is sufficient — --help creates no tmux sessions.
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "handoff", "--help")
	if err != nil {
		t.Fatalf("sol handoff --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Stop the current agent session") {
		t.Errorf("output missing expected text: %s", out)
	}
}

// TestCLIConsulRunHelp is intentionally help-only.
// consul run is a daemon command; behavioral coverage is in loop5_test.go.
func TestCLIConsulRunHelp(t *testing.T) {
	skipUnlessIntegration(t)
	// t.TempDir() is sufficient — --help creates no tmux sessions.
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "consul", "run", "--help")
	if err != nil {
		t.Fatalf("sol consul run --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Run the consul patrol loop") {
		t.Errorf("output missing expected text: %s", out)
	}
}

// TestCLIConsulStatusHelp is intentionally help-only.
// consul status requires a running consul daemon; behavioral coverage is in loop5_test.go.
func TestCLIConsulStatusHelp(t *testing.T) {
	skipUnlessIntegration(t)
	// t.TempDir() is sufficient — --help creates no tmux sessions.
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "consul", "status", "--help")
	if err != nil {
		t.Fatalf("sol consul status --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Show consul status") {
		t.Errorf("output missing expected text: %s", out)
	}
}

// TestCLIPrefectRunConsulFlag is intentionally help-only.
// It verifies that the --consul and --source-repo flags are wired up in the
// cobra command definition; behavioral coverage is in loop1_test.go.
func TestCLIPrefectRunConsulFlag(t *testing.T) {
	skipUnlessIntegration(t)
	// t.TempDir() is sufficient — --help creates no tmux sessions.
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
