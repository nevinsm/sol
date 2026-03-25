package forge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateResult(t *testing.T) {
	tests := []struct {
		name    string
		result  ForgeResult
		wantErr string
	}{
		{
			name:   "valid merged result",
			result: ForgeResult{Result: "merged", Summary: "Clean merge, all gates passed"},
		},
		{
			name:   "valid failed result",
			result: ForgeResult{Result: "failed", Summary: "Gate failure in tests"},
		},
		{
			name:   "valid conflict result",
			result: ForgeResult{Result: "conflict", Summary: "Incompatible changes in auth module"},
		},
		{
			name:   "valid with files and gate output",
			result: ForgeResult{Result: "merged", Summary: "OK", FilesChanged: []string{"a.go", "b.go"}, GateOutput: "PASS"},
		},
		{
			name:    "missing result field",
			result:  ForgeResult{Summary: "something happened"},
			wantErr: `missing required field "result"`,
		},
		{
			name:    "invalid result value",
			result:  ForgeResult{Result: "success", Summary: "it worked"},
			wantErr: `invalid result "success"`,
		},
		{
			name:    "missing summary field",
			result:  ForgeResult{Result: "merged"},
			wantErr: `missing required field "summary"`,
		},
		{
			name:   "valid no-op merged result",
			result: ForgeResult{Result: "merged", Summary: "No-op: work already present", NoOp: true},
		},
		{
			name:    "no_op invalid with failed result",
			result:  ForgeResult{Result: "failed", Summary: "something", NoOp: true},
			wantErr: `no_op is only valid with result "merged"`,
		},
		{
			name:    "no_op invalid with conflict result",
			result:  ForgeResult{Result: "conflict", Summary: "something", NoOp: true},
			wantErr: `no_op is only valid with result "merged"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResult(&tt.result)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := err.Error(); !contains(got, tt.wantErr) {
				t.Errorf("error %q does not contain %q", got, tt.wantErr)
			}
		})
	}
}

func TestReadResult(t *testing.T) {
	t.Run("reads valid result file", func(t *testing.T) {
		dir := t.TempDir()
		result := ForgeResult{
			Result:       "merged",
			Summary:      "Clean merge",
			FilesChanged: []string{"main.go", "go.mod"},
			GateOutput:   "ok\t./...",
		}
		writeResultFile(t, dir, result)

		got, err := ReadResult(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Result != "merged" {
			t.Errorf("result = %q, want merged", got.Result)
		}
		if got.Summary != "Clean merge" {
			t.Errorf("summary = %q, want Clean merge", got.Summary)
		}
		if len(got.FilesChanged) != 2 {
			t.Errorf("files_changed len = %d, want 2", len(got.FilesChanged))
		}
		if got.GateOutput != "ok\t./..." {
			t.Errorf("gate_output = %q, want ok\\t./...", got.GateOutput)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		dir := t.TempDir()
		_, err := ReadResult(dir)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !contains(err.Error(), "not found") {
			t.Errorf("error %q should mention not found", err.Error())
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, resultFileName)
		if err := os.WriteFile(path, []byte("{bad json}"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := ReadResult(dir)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !contains(err.Error(), "parse") {
			t.Errorf("error %q should mention parse", err.Error())
		}
	})

	t.Run("rejects invalid result value", func(t *testing.T) {
		dir := t.TempDir()
		writeResultFile(t, dir, ForgeResult{Result: "bad", Summary: "x"})

		_, err := ReadResult(dir)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !contains(err.Error(), "invalid result") {
			t.Errorf("error %q should mention invalid result", err.Error())
		}
	})

	t.Run("reads valid no-op result file", func(t *testing.T) {
		dir := t.TempDir()
		result := ForgeResult{
			Result:       "merged",
			Summary:      "No-op: work already present on target branch",
			FilesChanged: []string{},
			NoOp:         true,
		}
		writeResultFile(t, dir, result)

		got, err := ReadResult(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.NoOp {
			t.Error("expected NoOp to be true")
		}
		if got.Result != "merged" {
			t.Errorf("result = %q, want merged", got.Result)
		}
	})

	t.Run("rejects no_op with non-merged result", func(t *testing.T) {
		dir := t.TempDir()
		// Write raw JSON to bypass Go-level validation
		path := filepath.Join(dir, resultFileName)
		data := []byte(`{"result":"failed","summary":"something","files_changed":[],"no_op":true}`)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := ReadResult(dir)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !contains(err.Error(), "no_op is only valid") {
			t.Errorf("error %q should mention no_op validity", err.Error())
		}
	})

	t.Run("optional gate output can be empty", func(t *testing.T) {
		dir := t.TempDir()
		writeResultFile(t, dir, ForgeResult{Result: "conflict", Summary: "Cannot reconcile"})

		got, err := ReadResult(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.GateOutput != "" {
			t.Errorf("gate_output = %q, want empty", got.GateOutput)
		}
	})
}

func TestCleanForgeResult(t *testing.T) {
	t.Run("removes existing file", func(t *testing.T) {
		dir := t.TempDir()
		writeResultFile(t, dir, ForgeResult{Result: "merged", Summary: "ok"})

		if err := CleanForgeResult(dir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		path := filepath.Join(dir, resultFileName)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("result file should have been removed")
		}
	})

	t.Run("no error if file does not exist", func(t *testing.T) {
		dir := t.TempDir()
		if err := CleanForgeResult(dir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// writeResultFile is a test helper that writes a ForgeResult to .forge-result.json.
func writeResultFile(t *testing.T, dir string, result ForgeResult) {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal result: %v", err)
	}
	path := filepath.Join(dir, resultFileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed to write result file: %v", err)
	}
}

// contains is a simple substring check for error messages.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
