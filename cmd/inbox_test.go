package cmd

import "testing"

// TestRequireTTY covers the decision behind requireTTYForInboxTUI: the
// inbox TUI needs both stdin and stdout to be a terminal, and refuses with
// a clear error (suggesting --json) otherwise, rather than letting
// bubbletea fail deep inside with a raw "could not open a new TTY" error.
func TestRequireTTY(t *testing.T) {
	tests := []struct {
		name      string
		stdinTTY  bool
		stdoutTTY bool
		wantErr   bool
	}{
		{"both TTY", true, true, false},
		{"stdin not TTY", false, true, true},
		{"stdout not TTY", true, false, true},
		{"neither TTY", false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := requireTTY(tt.stdinTTY, tt.stdoutTTY)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
