package governor

import (
	"os"
	"testing"
)

func TestQueryDirPaths(t *testing.T) {
	t.Setenv("SOL_HOME", "/tmp/sol-test")

	tests := []struct {
		name string
		fn   func(string) string
		want string
	}{
		{"QueryDir", QueryDir, "/tmp/sol-test/myworld/governor/.query"},
		{"PendingPath", PendingPath, "/tmp/sol-test/myworld/governor/.query/pending.md"},
		{"ResponsePath", ResponsePath, "/tmp/sol-test/myworld/governor/.query/response.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn("myworld")
			if got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestWritePendingAndReadResponse(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// Write a pending question.
	if err := WritePending("myworld", "What is the architecture?"); err != nil {
		t.Fatalf("WritePending failed: %v", err)
	}

	// Verify pending file exists with correct content.
	data, err := os.ReadFile(PendingPath("myworld"))
	if err != nil {
		t.Fatalf("failed to read pending file: %v", err)
	}
	if string(data) != "What is the architecture?" {
		t.Errorf("pending content = %q, want %q", string(data), "What is the architecture?")
	}

	// ReadResponse should return false when no response exists.
	_, found, err := ReadResponse("myworld")
	if err != nil {
		t.Fatalf("ReadResponse failed: %v", err)
	}
	if found {
		t.Error("expected no response file yet")
	}

	// Simulate governor writing a response.
	if err := os.WriteFile(ResponsePath("myworld"), []byte("Go monorepo with SQLite store."), 0o644); err != nil {
		t.Fatalf("failed to write response: %v", err)
	}

	// ReadResponse should now return the response.
	response, found, err := ReadResponse("myworld")
	if err != nil {
		t.Fatalf("ReadResponse failed: %v", err)
	}
	if !found {
		t.Error("expected response file to exist")
	}
	if response != "Go monorepo with SQLite store." {
		t.Errorf("response = %q, want %q", response, "Go monorepo with SQLite store.")
	}
}

func TestClearQuery(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// Create both files.
	if err := WritePending("myworld", "question"); err != nil {
		t.Fatalf("WritePending failed: %v", err)
	}
	if err := os.WriteFile(ResponsePath("myworld"), []byte("answer"), 0o644); err != nil {
		t.Fatalf("failed to write response: %v", err)
	}

	// Clear should remove both.
	ClearQuery("myworld")

	if _, err := os.Stat(PendingPath("myworld")); !os.IsNotExist(err) {
		t.Error("pending file should be removed after ClearQuery")
	}
	if _, err := os.Stat(ResponsePath("myworld")); !os.IsNotExist(err) {
		t.Error("response file should be removed after ClearQuery")
	}
}

func TestClearQueryNoFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// ClearQuery should not panic when files don't exist.
	ClearQuery("myworld")
}
