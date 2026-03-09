package cmd

import (
	"testing"
)

func TestDocsGenerateCommand(t *testing.T) {
	cmd := docsGenerateCmd
	if cmd.Short == "" {
		t.Error("docsGenerateCmd should have a Short description")
	}
	if cmd.Short == "Generate CLI reference documentation (deprecated — use skills)" {
		t.Error("docsGenerateCmd should no longer be marked as deprecated")
	}
}

func TestDocsValidateCommand(t *testing.T) {
	cmd := docsValidateCmd
	if cmd.Short == "" {
		t.Error("docsValidateCmd should have a Short description")
	}
}

func TestDocsGenerateHasFlags(t *testing.T) {
	f := docsGenerateCmd.Flags()
	if f.Lookup("stdout") == nil {
		t.Error("docsGenerateCmd should have --stdout flag")
	}
	if f.Lookup("check") == nil {
		t.Error("docsGenerateCmd should have --check flag")
	}
}
