package cmd

import (
	"strings"
	"testing"
)

// resetHandoffCmdFlags resets handoff package-level flag vars between test runs
// (handoffCmd is a package-level singleton *cobra.Command).
func resetHandoffCmdFlags() {
	handoffWorld = ""
	handoffAgent = ""
	handoffSummary = ""
	handoffSummaryFile = ""
	handoffReason = ""
	handoffJSON = false
}

// TestHandoffRequiresSummaryOrSummaryFile verifies --summary is no longer a
// hard cobra-required flag (MarkFlagRequired), since --summary-file is now a
// valid alternative — but at least one of the two must still be supplied.
// The check happens before any world/agent resolution, so this needs no
// SOL_HOME setup at all.
func TestHandoffRequiresSummaryOrSummaryFile(t *testing.T) {
	resetHandoffCmdFlags()
	t.Cleanup(resetHandoffCmdFlags)

	rootCmd.SetArgs([]string{"handoff"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when neither --summary nor --summary-file is set")
	}
	if !strings.Contains(err.Error(), "--summary") || !strings.Contains(err.Error(), "--summary-file") {
		t.Errorf("expected error to name both --summary and --summary-file, got: %v", err)
	}
}

// TestHandoffSummaryMutuallyExclusiveWithFile verifies passing both
// --summary and --summary-file is rejected (mutual exclusion is checked
// before world/agent resolution, so no SOL_HOME setup is needed).
func TestHandoffSummaryMutuallyExclusiveWithFile(t *testing.T) {
	resetHandoffCmdFlags()
	t.Cleanup(resetHandoffCmdFlags)

	rootCmd.SetArgs([]string{"handoff", "--summary", "inline summary", "--summary-file", "/nonexistent/whatever.txt"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when both --summary and --summary-file are set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutually exclusive error, got: %v", err)
	}
}
