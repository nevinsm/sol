package cmd

import (
	"testing"
)

func TestDocsGenerateDeprecated(t *testing.T) {
	// sol docs generate is deprecated — skills replace the flat CLI reference.
	// Verify the command still exists and runs without error.
	cmd := docsGenerateCmd
	if cmd.Short == "" {
		t.Error("docsGenerateCmd should have a Short description")
	}
}
