package docgen

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// noop is a no-op RunE for test commands. Cobra's IsAvailableCommand()
// requires either Run/RunE or available subcommands.
var noop = func(cmd *cobra.Command, args []string) error { return nil }

func newTestRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "testcli",
		Short: "Test CLI",
	}

	root.AddGroup(
		&cobra.Group{ID: "core", Title: "Core:"},
		&cobra.Group{ID: "util", Title: "Utilities:"},
	)

	// Simple command with flags.
	statusCmd := &cobra.Command{
		Use:     "status",
		Short:   "Show status",
		GroupID: "core",
		RunE:    noop,
	}
	statusCmd.Flags().Bool("json", false, "output as JSON")
	statusCmd.Flags().String("world", "", "world name")
	root.AddCommand(statusCmd)

	// Command with subcommands.
	agentCmd := &cobra.Command{
		Use:     "agent",
		Short:   "Manage agents",
		GroupID: "core",
		Aliases: []string{"ag"},
	}
	agentListCmd := &cobra.Command{
		Use:   "list",
		Short: "List agents",
		RunE:  noop,
	}
	agentListCmd.Flags().String("world", "", "world name")
	agentCreateCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create an agent",
		RunE:  noop,
	}
	agentCreateCmd.Flags().String("role", "outpost", "agent role")
	agentCmd.AddCommand(agentListCmd, agentCreateCmd)
	root.AddCommand(agentCmd)

	// Utility command.
	versionCmd := &cobra.Command{
		Use:     "version",
		Short:   "Show version",
		GroupID: "util",
		RunE:    noop,
	}
	root.AddCommand(versionCmd)

	// Hidden command — should not appear.
	debugCmd := &cobra.Command{
		Use:    "debug",
		Short:  "Debug internals",
		Hidden: true,
		RunE:   noop,
	}
	root.AddCommand(debugCmd)

	return root
}

func TestGenerateDeterministic(t *testing.T) {
	root := newTestRoot()
	out1 := Generate(root)
	out2 := Generate(root)
	if out1 != out2 {
		t.Error("Generate should produce identical output on repeated calls")
	}
}

func TestGenerateContainsHeader(t *testing.T) {
	root := newTestRoot()
	out := Generate(root)
	if !strings.Contains(out, "# CLI Reference") {
		t.Error("output should contain CLI Reference header")
	}
	if !strings.Contains(out, "Auto-generated") {
		t.Error("output should contain auto-generated notice")
	}
}

func TestGenerateGroupsCommands(t *testing.T) {
	root := newTestRoot()
	out := Generate(root)

	if !strings.Contains(out, "## Core:") {
		t.Error("output should contain Core group heading")
	}
	if !strings.Contains(out, "## Utilities:") {
		t.Error("output should contain Utilities group heading")
	}
}

func TestGenerateIncludesCommands(t *testing.T) {
	root := newTestRoot()
	out := Generate(root)

	for _, want := range []string{
		"`testcli status`",
		"`testcli agent`",
		"`testcli version`",
		"`testcli agent list`",
		"`testcli agent create`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %s", want)
		}
	}
}

func TestGenerateExcludesHiddenFromMainSections(t *testing.T) {
	root := newTestRoot()
	out := Generate(root)

	// Split at the Plumbing Commands section.
	parts := strings.SplitN(out, "## Plumbing Commands", 2)
	mainSection := parts[0]

	// Hidden commands should not appear in the main sections.
	if strings.Contains(mainSection, "debug") {
		t.Error("main sections should not contain hidden commands")
	}

	// But the plumbing section should list them.
	if len(parts) < 2 {
		t.Fatal("expected a Plumbing Commands section")
	}
	if !strings.Contains(parts[1], "debug") {
		t.Error("plumbing section should list hidden commands")
	}
}

func TestGenerateIncludesFlags(t *testing.T) {
	root := newTestRoot()
	out := Generate(root)

	for _, want := range []string{
		"`--json`",
		"`--world`",
		"`--role`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain flag %s", want)
		}
	}
}

func TestGenerateIncludesAliases(t *testing.T) {
	root := newTestRoot()
	out := Generate(root)
	if !strings.Contains(out, "`ag`") {
		t.Error("output should contain alias `ag`")
	}
}

func TestGenerateIncludesUsage(t *testing.T) {
	root := newTestRoot()
	out := Generate(root)
	if !strings.Contains(out, "<name>") {
		t.Error("output should contain usage args like <name>")
	}
}

func TestGenerateFlagDefaults(t *testing.T) {
	root := newTestRoot()
	out := Generate(root)
	if !strings.Contains(out, "outpost") {
		t.Error("output should contain default value 'outpost' for --role flag")
	}
}
