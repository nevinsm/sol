package supervisor

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func setupTestPIDDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	// Ensure runtime dir exists.
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestWriteAndReadPID(t *testing.T) {
	setupTestPIDDir(t)

	// Write PID.
	if err := WritePID(); err != nil {
		t.Fatalf("WritePID() error: %v", err)
	}

	// Read PID — should be our PID.
	pid, err := ReadPID()
	if err != nil {
		t.Fatalf("ReadPID() error: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("ReadPID() = %d, want %d", pid, os.Getpid())
	}

	// Clear PID.
	if err := ClearPID(); err != nil {
		t.Fatalf("ClearPID() error: %v", err)
	}

	// Read again — should be 0.
	pid, err = ReadPID()
	if err != nil {
		t.Fatalf("ReadPID() after clear error: %v", err)
	}
	if pid != 0 {
		t.Errorf("ReadPID() after clear = %d, want 0", pid)
	}
}

func TestWritePIDAlreadyRunning(t *testing.T) {
	setupTestPIDDir(t)

	// Write our own PID to simulate already running.
	if err := WritePID(); err != nil {
		t.Fatalf("first WritePID() error: %v", err)
	}

	// Second write should fail because our PID is alive.
	err := WritePID()
	if err == nil {
		t.Fatal("second WritePID() should have failed")
	}
	want := "supervisor already running"
	if got := err.Error(); !contains(got, want) {
		t.Errorf("error = %q, want to contain %q", got, want)
	}
}

func TestWritePIDStalePID(t *testing.T) {
	setupTestPIDDir(t)

	// Write a dead PID (99999 is very unlikely to be alive).
	path := pidPath()
	if err := os.WriteFile(path, []byte("99999"), 0o644); err != nil {
		t.Fatal(err)
	}

	// WritePID should succeed by overwriting the stale PID.
	if err := WritePID(); err != nil {
		t.Fatalf("WritePID() with stale PID error: %v", err)
	}

	pid, err := ReadPID()
	if err != nil {
		t.Fatalf("ReadPID() error: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("ReadPID() = %d, want %d (our PID)", pid, os.Getpid())
	}
}

func TestIsRunning(t *testing.T) {
	// Our own PID should be running.
	if !IsRunning(os.Getpid()) {
		t.Error("IsRunning(os.Getpid()) = false, want true")
	}
	// Dead PID should not be running.
	if IsRunning(99999) {
		t.Error("IsRunning(99999) = true, want false")
	}
	// Invalid PID.
	if IsRunning(0) {
		t.Error("IsRunning(0) = true, want false")
	}
	if IsRunning(-1) {
		t.Error("IsRunning(-1) = true, want false")
	}
}

func TestReadPIDInvalidContent(t *testing.T) {
	setupTestPIDDir(t)

	path := pidPath()
	if err := os.WriteFile(path, []byte("not-a-number"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ReadPID()
	if err == nil {
		t.Fatal("ReadPID() with invalid content should error")
	}
}

func TestClearPIDNoFile(t *testing.T) {
	setupTestPIDDir(t)

	// Clearing when no file exists should be a no-op.
	if err := ClearPID(); err != nil {
		t.Fatalf("ClearPID() with no file error: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsHelper(s, substr)
}

func containsHelper(s, substr string) bool {
	return strconv.IntSize > 0 && // always true, but prevents inlining
		len(s) >= len(substr) &&
		(s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
