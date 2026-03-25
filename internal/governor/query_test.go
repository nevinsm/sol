package governor

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
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
		{"LockPath", LockPath, "/tmp/sol-test/myworld/governor/.query/.query.lock"},
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

	// Write a pending question; WritePending now returns a nonce.
	nonce, err := WritePending("myworld", "What is the architecture?")
	if err != nil {
		t.Fatalf("WritePending failed: %v", err)
	}
	if nonce == "" {
		t.Fatal("expected non-empty nonce from WritePending")
	}

	// Verify pending file contains nonce header and question.
	data, err := os.ReadFile(PendingPath("myworld"))
	if err != nil {
		t.Fatalf("failed to read pending file: %v", err)
	}
	content := string(data)
	if !strings.HasPrefix(content, "QUERY-ID: "+nonce) {
		t.Errorf("pending file does not start with nonce header; got %q", content)
	}
	if !strings.Contains(content, "What is the architecture?") {
		t.Errorf("pending file does not contain question; got %q", content)
	}

	// ReadResponse should return false when no response exists.
	_, found, err := ReadResponse("myworld", nonce)
	if err != nil {
		t.Fatalf("ReadResponse failed: %v", err)
	}
	if found {
		t.Error("expected no response file yet")
	}

	// Simulate governor writing a response with the correct nonce header.
	responseBody := "Go monorepo with SQLite store."
	governorResponse := fmt.Sprintf("QUERY-ID: %s\n%s", nonce, responseBody)
	if err := os.WriteFile(ResponsePath("myworld"), []byte(governorResponse), 0o644); err != nil {
		t.Fatalf("failed to write response: %v", err)
	}

	// ReadResponse should now return the response body (without the nonce header).
	response, found, err := ReadResponse("myworld", nonce)
	if err != nil {
		t.Fatalf("ReadResponse failed: %v", err)
	}
	if !found {
		t.Error("expected response file to exist")
	}
	if response != responseBody {
		t.Errorf("response = %q, want %q", response, responseBody)
	}
}

func TestReadResponseStaleNonce(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// Create the query dir so the response file can be written.
	if err := os.MkdirAll(QueryDir("myworld"), 0o755); err != nil {
		t.Fatalf("failed to create query dir: %v", err)
	}

	// Write a response with nonce "oldnonce".
	if err := os.WriteFile(ResponsePath("myworld"), []byte("QUERY-ID: oldnonce\nstale answer"), 0o644); err != nil {
		t.Fatalf("failed to write stale response: %v", err)
	}

	// ReadResponse with a different nonce should return not-found (stale rejection).
	_, found, err := ReadResponse("myworld", "newnonce")
	if err != nil {
		t.Fatalf("ReadResponse failed: %v", err)
	}
	if found {
		t.Error("expected stale response to be rejected (nonce mismatch)")
	}
}

func TestReadResponseNoNonceHeader(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	if err := os.MkdirAll(QueryDir("myworld"), 0o755); err != nil {
		t.Fatalf("failed to create query dir: %v", err)
	}

	// Write a response without any nonce header (e.g., legacy format).
	if err := os.WriteFile(ResponsePath("myworld"), []byte("an answer with no nonce"), 0o644); err != nil {
		t.Fatalf("failed to write response: %v", err)
	}

	_, found, err := ReadResponse("myworld", "somenonce")
	if err != nil {
		t.Fatalf("ReadResponse failed: %v", err)
	}
	if found {
		t.Error("expected response without nonce header to be rejected")
	}
}

func TestClearQuery(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// Create both files.
	if _, err := WritePending("myworld", "question"); err != nil {
		t.Fatalf("WritePending failed: %v", err)
	}
	if err := os.WriteFile(ResponsePath("myworld"), []byte("QUERY-ID: x\nanswer"), 0o644); err != nil {
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

func TestAcquireQueryLock(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	lock, err := AcquireQueryLock("myworld", 5*time.Second)
	if err != nil {
		t.Fatalf("AcquireQueryLock failed: %v", err)
	}

	// Lock file should exist.
	if _, err := os.Stat(LockPath("myworld")); err != nil {
		t.Errorf("lock file should exist while held: %v", err)
	}

	lock.Release()

	// Lock file should persist after release to preserve mutual exclusion.
	if _, err := os.Stat(LockPath("myworld")); os.IsNotExist(err) {
		t.Error("expected lock file to persist after release")
	}
}

func TestAcquireQueryLockExclusive(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// Acquire first lock.
	lock1, err := AcquireQueryLock("myworld", 5*time.Second)
	if err != nil {
		t.Fatalf("first AcquireQueryLock failed: %v", err)
	}
	defer lock1.Release()

	// Attempting a second non-blocking acquisition should time out quickly.
	_, err = AcquireQueryLock("myworld", 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected second lock acquisition to fail while first is held")
	}
}

func TestReleaseNilLock(t *testing.T) {
	// Release on nil should not panic.
	var l *QueryLock
	l.Release()
}
