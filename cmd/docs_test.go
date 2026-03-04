package cmd

import (
	"strings"
	"testing"
)

func TestGenerateDocsCoversAllCommands(t *testing.T) {
	root := rootCmd
	allCommands := collectRunnableCommands(root)
	documented := make(map[string]bool)

	for _, sec := range docSections {
		for _, path := range sec.paths {
			if _, ok := allCommands[path]; !ok {
				t.Errorf("documented command path %q not found in command tree", path)
			}
			documented[path] = true
		}
	}

	var undocumented []string
	for path := range allCommands {
		if !documented[path] && !isSkippedCommand(path) {
			undocumented = append(undocumented, path)
		}
	}
	if len(undocumented) > 0 {
		t.Errorf("undocumented commands: %s", strings.Join(undocumented, ", "))
	}
}

func TestGenerateDocsFormat(t *testing.T) {
	root := rootCmd
	allCommands := collectRunnableCommands(root)

	// Verify every documented command produces a valid table row.
	for _, sec := range docSections {
		for _, path := range sec.paths {
			cmd, ok := allCommands[path]
			if !ok {
				continue // covered by TestGenerateDocsCoversAllCommands
			}
			display := formatCommandCell(path, cmd)
			if !strings.HasPrefix(display, "`sol ") {
				t.Errorf("command display for %q should start with `sol : got %q", path, display)
			}
			if cmd.Short == "" {
				t.Errorf("command %q has empty Short description", path)
			}
		}
	}
}
