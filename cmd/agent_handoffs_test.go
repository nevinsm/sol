package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// resetHandoffFlags restores the package-level flag state between tests so
// invocations don't bleed into each other.
func resetHandoffFlags() {
	handoffsWorld = ""
	handoffsAgent = ""
	handoffsLast = 20
	handoffsJSON = false
}

// runHandoffsArgs invokes the command's argument validator without executing
// RunE, so the test doesn't need SOL_HOME or an events store.
func runHandoffsArgs(t *testing.T, args []string) error {
	t.Helper()
	return agentHandoffsCmd.Args(agentHandoffsCmd, args)
}

func TestAgentHandoffsArgs_AcceptsZeroOrOnePositional(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"no positional", nil, false},
		{"single positional", []string{"Polaris"}, false},
		{"two positional rejected", []string{"Polaris", "Castor"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runHandoffsArgs(t, tc.args)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for args=%v, got nil", tc.args)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for args=%v: %v", tc.args, err)
			}
		})
	}
}

func TestAgentHandoffsCmd_AgentFlagHidden(t *testing.T) {
	f := agentHandoffsCmd.Flags().Lookup("agent")
	if f == nil {
		t.Fatalf("--agent flag should still exist as a deprecated alias")
	}
	if !f.Hidden {
		t.Errorf("--agent flag should be hidden (deprecated)")
	}
}

func TestAgentHandoffsCmd_UseStringAdvertisesPositional(t *testing.T) {
	if !strings.Contains(agentHandoffsCmd.Use, "[name]") {
		t.Errorf("Use = %q, want to contain \"[name]\" for optional positional", agentHandoffsCmd.Use)
	}
}

func TestAgentHandoffsCmd_LongDocumentsWorldAutoDetect(t *testing.T) {
	long := agentHandoffsCmd.Long
	if !strings.Contains(long, "auto-detect") {
		t.Errorf("Long help should document --world auto-detection, got:\n%s", long)
	}
}

// TestAgentHandoffsCmd_DeprecationNoticeOnFlag exercises the deprecation
// notice logic by invoking the command with --agent set but no positional.
// We stop short of reading events (which would require SOL_HOME fixtures) by
// resolving world against an empty config, which returns an error — but the
// deprecation notice is emitted before the read, so we can still observe it
// on stderr by calling the command directly and tolerating the error.
func TestAgentHandoffsCmd_DeprecationNoticeOnFlag(t *testing.T) {
	defer resetHandoffFlags()
	resetHandoffFlags()

	// Run against a throwaway parent cobra.Command to capture stderr.
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	// Clone the command shell so test state stays local; reuse the same
	// RunE/Args/flags.
	c := &cobra.Command{
		Use:          agentHandoffsCmd.Use,
		Args:         agentHandoffsCmd.Args,
		RunE:         agentHandoffsCmd.RunE,
		SilenceUsage: true,
	}
	c.Flags().AddFlagSet(agentHandoffsCmd.Flags())
	c.SetOut(&stdout)
	c.SetErr(&stderr)

	// Simulate --agent=Polaris with no positional; set a bogus world so
	// ResolveWorld has something to work with. The deprecation notice must
	// be printed before any world resolution work that could fail.
	handoffsAgent = "Polaris"
	handoffsWorld = "__test_nonexistent_world__"
	_ = c.RunE(c, nil)

	if !strings.Contains(stderr.String(), "--agent is deprecated") {
		t.Errorf("expected deprecation notice on stderr, got: %q", stderr.String())
	}
}
