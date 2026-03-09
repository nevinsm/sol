package docgen

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestValidateMatchesGenerated(t *testing.T) {
	root := newTestRoot()
	generated := Generate(root)

	result := Validate(root, generated)
	if !result.Match {
		t.Errorf("Validate should match when content is identical, got diff:\n%s", result.Diff)
	}
}

func TestValidateDetectsMismatch(t *testing.T) {
	root := newTestRoot()

	result := Validate(root, "# Old content\n\nNothing here.\n")
	if result.Match {
		t.Error("Validate should detect mismatch with different content")
	}
	if result.Diff == "" {
		t.Error("Validate should produce a diff description")
	}
}

func TestValidateDetectsMissingCommands(t *testing.T) {
	root := newTestRoot()
	// Remove a command from the existing content to simulate drift.
	generated := Generate(root)
	// Remove all lines mentioning "version" to simulate a missing command.
	lines := strings.Split(generated, "\n")
	var filtered []string
	for _, line := range lines {
		if !strings.Contains(line, "version") {
			filtered = append(filtered, line)
		}
	}
	modified := strings.Join(filtered, "\n")

	result := Validate(root, modified)
	if result.Match {
		t.Error("Validate should detect missing command")
	}
	if !strings.Contains(result.Diff, "Missing commands") || !strings.Contains(result.Diff, "testcli version") {
		t.Errorf("Validate diff should mention missing 'testcli version', got:\n%s", result.Diff)
	}
}

func TestValidateDetectsExtraCommands(t *testing.T) {
	root := newTestRoot()
	generated := Generate(root)
	// Add a fake command reference to simulate an extra command in docs.
	modified := generated + "\n### `testcli phantom`\n\nA ghost command.\n"

	result := Validate(root, modified)
	if result.Match {
		t.Error("Validate should detect extra command")
	}
	if !strings.Contains(result.Diff, "Extra commands") || !strings.Contains(result.Diff, "testcli phantom") {
		t.Errorf("Validate diff should mention extra 'testcli phantom', got:\n%s", result.Diff)
	}
}

func TestValidateReportsFirstDivergence(t *testing.T) {
	root := newTestRoot()
	generated := Generate(root)
	// Modify one line.
	modified := strings.Replace(generated, "Auto-generated", "Hand-written", 1)

	result := Validate(root, modified)
	if result.Match {
		t.Error("Validate should detect content change")
	}
	if !strings.Contains(result.Diff, "First difference at line") {
		t.Errorf("Validate diff should report first divergence, got:\n%s", result.Diff)
	}
}

func TestExtractHeadingCmd(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"### `sol agent list`", "sol agent list"},
		{"## `sol cost`", "sol cost"},
		{"no command here", ""},
	}

	for _, tc := range tests {
		got := extractHeadingCmd(tc.input)
		if got != tc.want {
			t.Errorf("extractHeadingCmd(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestExtractTableCmd(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"| `sol cost --json` | desc |", "sol cost"},
		{"| `sol agent list` | desc |", "sol agent list"},
		{"no command here", ""},
	}

	for _, tc := range tests {
		got := extractTableCmd(tc.input)
		if got != tc.want {
			t.Errorf("extractTableCmd(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestValidateEmptyExisting(t *testing.T) {
	root := &cobra.Command{Use: "test", Short: "test"}
	result := Validate(root, "")
	if result.Match {
		t.Error("Validate should not match empty existing content against generated output")
	}
}
