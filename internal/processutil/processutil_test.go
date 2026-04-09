package processutil

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMain lets this test binary double as a fake `sol` binary. When invoked
// via a symlink whose basename is "sol" and with argv ["sol", "consul", "run"],
// it blocks until SIGTERM/SIGINT (or a safety timeout), so /proc/{pid}/cmdline
// matches the filter used by FindSolSubcommandPIDs. Normal test runs (where
// argv[0] is the default test binary name) fall through to m.Run().
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "sol" &&
		len(os.Args) >= 3 && os.Args[1] == "consul" && os.Args[2] == "run" {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		select {
		case <-sig:
		case <-time.After(30 * time.Second):
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// ----- WritePID -----

func TestWritePIDNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")

	// Write a foreign PID (parent process) — no locking involved.
	foreignPID := os.Getppid()
	if err := WritePID(path, foreignPID); err != nil {
		t.Fatalf("WritePID() error: %v", err)
	}

	pid, err := ReadPID(path)
	if err != nil {
		t.Fatalf("ReadPID() error: %v", err)
	}
	if pid != foreignPID {
		t.Fatalf("ReadPID() = %d, want %d", pid, foreignPID)
	}
}

func TestWritePIDCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "sub", "daemon.pid")

	if err := WritePID(path, os.Getppid()); err != nil {
		t.Fatalf("WritePID() should create parent dirs: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("PID file should exist: %v", err)
	}
}

func TestWritePIDReentry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "self.pid")
	t.Cleanup(func() { _ = ClearPID(path) })

	myPID := os.Getpid()

	// First call acquires the lock.
	if err := WritePID(path, myPID); err != nil {
		t.Fatalf("WritePID() first call error: %v", err)
	}

	// Second call with the same path and PID should succeed via the re-entry path
	// (uses the already-held file descriptor rather than trying to flock again).
	if err := WritePID(path, myPID); err != nil {
		t.Fatalf("WritePID() re-entry error: %v", err)
	}

	pid, err := ReadPID(path)
	if err != nil {
		t.Fatalf("ReadPID() error: %v", err)
	}
	if pid != myPID {
		t.Fatalf("ReadPID() = %d, want %d", pid, myPID)
	}
}

func TestWritePIDErrorOnBadPath(t *testing.T) {
	// A path whose parent directory cannot be created (SOL_HOME is a file).
	tmpFile, err := os.CreateTemp("", "not-a-dir")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })

	// Attempt to write PID into a path whose ancestor is a file.
	path := filepath.Join(tmpFile.Name(), "subdir", "daemon.pid")
	if err := WritePID(path, os.Getppid()); err == nil {
		t.Fatal("WritePID() expected error when parent cannot be created, got nil")
	}
}

// ----- ReadPID -----

func TestReadPIDValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")

	want := 12345
	if err := os.WriteFile(path, []byte(strconv.Itoa(want)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadPID(path)
	if err != nil {
		t.Fatalf("ReadPID() error: %v", err)
	}
	if got != want {
		t.Fatalf("ReadPID() = %d, want %d", got, want)
	}
}

func TestReadPIDMissingFile(t *testing.T) {
	pid, err := ReadPID("/nonexistent/path/daemon.pid")
	if err != nil {
		t.Fatalf("ReadPID() on missing file should return nil error, got: %v", err)
	}
	if pid != 0 {
		t.Fatalf("ReadPID() on missing file should return 0, got %d", pid)
	}
}

func TestReadPIDInvalidContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.pid")

	if err := os.WriteFile(path, []byte("not-a-number\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pid, err := ReadPID(path)
	if err == nil {
		t.Fatal("ReadPID() with invalid content expected error, got nil")
	}
	if pid != 0 {
		t.Fatalf("ReadPID() with invalid content should return 0, got %d", pid)
	}
}

// ----- ClearPID -----

func TestClearPIDHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "self.pid")

	// WritePID with our own PID to acquire the lock.
	if err := WritePID(path, os.Getpid()); err != nil {
		t.Fatalf("WritePID() error: %v", err)
	}

	// ClearPID should release the lock and truncate the file (not delete it).
	if err := ClearPID(path); err != nil {
		t.Fatalf("ClearPID() error: %v", err)
	}

	// File should still exist but be empty (truncated).
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("PID file should still exist after ClearPID(), got: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("PID file should be truncated (size 0) after ClearPID(), got size %d", info.Size())
	}
}

func TestClearPIDThenWritePIDSameInode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "self.pid")

	// WritePID acquires flock on the file.
	if err := WritePID(path, os.Getpid()); err != nil {
		t.Fatalf("WritePID() error: %v", err)
	}

	// Get the inode of the file before ClearPID.
	info1, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}

	// ClearPID truncates and releases flock.
	if err := ClearPID(path); err != nil {
		t.Fatalf("ClearPID() error: %v", err)
	}

	// WritePID again should reuse the same file (same inode).
	if err := WritePID(path, os.Getpid()); err != nil {
		t.Fatalf("WritePID() after ClearPID() error: %v", err)
	}
	t.Cleanup(func() { _ = ClearPID(path) })

	info2, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() after second WritePID error: %v", err)
	}

	// Verify same inode was reused (file was not deleted and recreated).
	// On Linux, Sys() returns *syscall.Stat_t with an Ino field.
	if s1, ok := info1.Sys().(*syscall.Stat_t); ok {
		if s2, ok := info2.Sys().(*syscall.Stat_t); ok {
			if s1.Ino != s2.Ino {
				t.Fatalf("inode changed: %d → %d (expected same inode after ClearPID + WritePID)", s1.Ino, s2.Ino)
			}
		}
	}

	// Verify PID was written correctly.
	pid, err := ReadPID(path)
	if err != nil {
		t.Fatalf("ReadPID() error: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("ReadPID() = %d, want %d", pid, os.Getpid())
	}
}

func TestClearPIDNonExistentFile(t *testing.T) {
	// Should not return an error for a file that doesn't exist.
	if err := ClearPID("/nonexistent/path/daemon.pid"); err != nil {
		t.Fatalf("ClearPID() on non-existent file should succeed, got: %v", err)
	}
}

// ----- GracefulKill -----

func TestGracefulKillExitsCleanly(t *testing.T) {
	// Start a process that responds to SIGTERM (the default for sleep).
	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start test process: %v", err)
	}

	pid := cmd.Process.Pid
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	if err := GracefulKill(pid, 2*time.Second); err != nil {
		t.Fatalf("GracefulKill() error: %v", err)
	}

	// Wait for the process to be reaped.
	select {
	case <-done:
		// Process exited — success.
	case <-time.After(3 * time.Second):
		t.Fatal("process did not exit after GracefulKill()")
	}
}

func TestGracefulKillRequiresSIGKILL(t *testing.T) {
	// Start a process that ignores SIGTERM; GracefulKill must escalate to SIGKILL.
	cmd := exec.Command("sh", "-c", "trap '' TERM; while true; do sleep 0.05; done")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start SIGTERM-ignoring process: %v", err)
	}

	pid := cmd.Process.Pid
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// Use a short grace period so the test doesn't take long.
	if err := GracefulKill(pid, 300*time.Millisecond); err != nil {
		t.Fatalf("GracefulKill() error: %v", err)
	}

	select {
	case <-done:
		// Process was killed — success.
	case <-time.After(3 * time.Second):
		t.Fatal("process did not exit after SIGKILL escalation")
	}
}

func TestGracefulKillAlreadyGone(t *testing.T) {
	// Start and immediately wait for a process that exits on its own.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to run test process: %v", err)
	}
	pid := cmd.Process.Pid

	// Process is already gone — should return nil without error.
	if err := GracefulKill(pid, time.Second); err != nil {
		t.Fatalf("GracefulKill() on already-exited process error: %v", err)
	}
}

func TestGracefulKillInvalidPID(t *testing.T) {
	err := GracefulKill(-1, time.Second)
	if err == nil {
		t.Fatal("GracefulKill() with invalid PID expected error, got nil")
	}
}

// ----- ClearPIDIfMatches -----

func TestClearPIDIfMatchesEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")
	// File does not exist — safe to clear (no-op).
	if err := ClearPIDIfMatches(path, 12345); err != nil {
		t.Fatalf("ClearPIDIfMatches() on missing file error: %v", err)
	}

	// Empty file — safe to clear.
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ClearPIDIfMatches(path, 12345); err != nil {
		t.Fatalf("ClearPIDIfMatches() on empty file error: %v", err)
	}
}

func TestClearPIDIfMatchesDeadPID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")
	// Start and wait for a short-lived process; its PID is safe to clear.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	deadPID := cmd.Process.Pid
	if err := os.WriteFile(path, []byte(strconv.Itoa(deadPID)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ClearPIDIfMatches(path, 99999); err != nil {
		t.Fatalf("ClearPIDIfMatches() error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("file should be truncated, got size %d", info.Size())
	}
}

func TestClearPIDIfMatchesExpectedPID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")
	// File contains exactly expectedPid — even if it's somehow live, clearing
	// is allowed because the caller is claiming ownership.
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ClearPIDIfMatches(path, os.Getpid()); err != nil {
		t.Fatalf("ClearPIDIfMatches() error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("expected truncated file, size=%d", info.Size())
	}
}

func TestClearPIDIfMatchesForeignLivePID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")
	// Start a real sleep subprocess — a foreign, live pid.
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	foreignPID := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	content := []byte(strconv.Itoa(foreignPID) + "\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	// Caller claims some other pid; the file should be left alone because it
	// records a different live process.
	if err := ClearPIDIfMatches(path, 99999); err != nil {
		t.Fatalf("ClearPIDIfMatches() error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), strconv.Itoa(foreignPID)) {
		t.Fatalf("file should still contain foreign pid %d, got: %q", foreignPID, string(got))
	}
}

// ----- FindSolSubcommandPIDs -----

// TestFindSolSubcommandPIDsMatchesRenamedSleep verifies the /proc scan picks
// up processes whose argv[0] basename is "sol" and whose following args match
// the filter, by using a shell script saved as "sol" that execs sleep.
func TestFindSolSubcommandPIDsMatchesRenamedSleep(t *testing.T) {
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("/proc not available")
	}

	dir := t.TempDir()
	solLink := filepath.Join(dir, "sol")
	// Symlink the test binary (which has a TestMain that blocks when argv
	// matches [sol, consul, run]) as "sol".
	if err := os.Symlink(os.Args[0], solLink); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	cmd := &exec.Cmd{
		Path: solLink,
		Args: []string{"sol", "consul", "run"},
		Env:  os.Environ(),
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fake sol: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// Give the kernel a moment to populate /proc/{pid}/cmdline.
	var pids []int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		pids, err = FindSolSubcommandPIDs("consul", "run")
		if err != nil {
			t.Fatalf("FindSolSubcommandPIDs: %v", err)
		}
		for _, p := range pids {
			if p == cmd.Process.Pid {
				return // success
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("FindSolSubcommandPIDs did not return fake pid %d (returned %v)", cmd.Process.Pid, pids)
}

func TestFindSolSubcommandPIDsNoMatch(t *testing.T) {
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("/proc not available")
	}
	// No fake sol binary alive; result should not include any spurious pid.
	pids, err := FindSolSubcommandPIDs("consul", "run")
	if err != nil {
		t.Fatalf("FindSolSubcommandPIDs: %v", err)
	}
	self := os.Getpid()
	for _, p := range pids {
		if p == self {
			t.Fatalf("scan should exclude self (pid %d)", self)
		}
	}
}

// ----- Cross-process flock contention -----

// TestFlockHelper is a helper "test" that acts as a child process for
// TestCrossProcessFlockContention. It is never run directly by go test
// (it skips unless the sentinel env var is set).
func TestFlockHelper(t *testing.T) {
	pidPath := os.Getenv("TEST_FLOCK_HELPER_PATH")
	if pidPath == "" {
		t.Skip("not invoked as flock helper process")
	}

	// Attempt to acquire flock via WritePID with our own PID.
	err := WritePID(pidPath, os.Getpid())
	if err != nil {
		// Print the error so the parent can inspect it, then exit non-zero.
		fmt.Fprintf(os.Stderr, "FLOCK_ERR: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "FLOCK_OK pid=%d\n", os.Getpid())
	// Do NOT call ClearPID — let the parent verify the file contents.
	// The flock is released automatically when this process exits.
	os.Exit(0)
}

// TestCrossProcessFlockContention verifies that two processes racing for the
// same pidfile flock get the correct behavior:
//  1. Process A acquires the flock via WritePID.
//  2. Process B (child) attempts WritePID on the same path — must fail.
//  3. Process A calls ClearPID (releases flock, preserves inode).
//  4. Process B (new child) retries WritePID — must succeed on the same inode.
func TestCrossProcessFlockContention(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "contention.pid")

	// Step 1: Parent acquires flock.
	if err := WritePID(path, os.Getpid()); err != nil {
		t.Fatalf("parent WritePID() error: %v", err)
	}
	t.Cleanup(func() { _ = ClearPID(path) })

	// Record original inode.
	info1, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	ino1 := info1.Sys().(*syscall.Stat_t).Ino

	// Step 2: Child attempts WritePID — should fail with EWOULDBLOCK.
	cmd := exec.Command(os.Args[0], "-test.run=^TestFlockHelper$")
	cmd.Env = append(os.Environ(), "TEST_FLOCK_HELPER_PATH="+path)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("child should fail while parent holds flock, output: %s", out)
	}
	if !strings.Contains(string(out), "another instance may be running") {
		t.Fatalf("child error should mention flock contention, got: %s", out)
	}

	// Step 3: Parent releases flock (ClearPID truncates but preserves inode).
	if err := ClearPID(path); err != nil {
		t.Fatalf("ClearPID() error: %v", err)
	}

	// Step 4: New child retries — should succeed now that flock is released.
	cmd2 := exec.Command(os.Args[0], "-test.run=^TestFlockHelper$")
	cmd2.Env = append(os.Environ(), "TEST_FLOCK_HELPER_PATH="+path)
	out2, err2 := cmd2.CombinedOutput()
	if err2 != nil {
		t.Fatalf("child should succeed after ClearPID, error: %v, output: %s", err2, out2)
	}
	if !strings.Contains(string(out2), "FLOCK_OK") {
		t.Fatalf("child should report FLOCK_OK, got: %s", out2)
	}

	// Step 5: Verify same inode was used (file was never deleted/recreated).
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() after child WritePID error: %v", err)
	}
	ino2 := info2.Sys().(*syscall.Stat_t).Ino
	if ino1 != ino2 {
		t.Fatalf("inode changed: %d → %d (expected same inode after ClearPID + child WritePID)", ino1, ino2)
	}

	// Step 6: Verify the child actually wrote its PID.
	childPID, err := ReadPID(path)
	if err != nil {
		t.Fatalf("ReadPID() after child WritePID error: %v", err)
	}
	if childPID == 0 {
		t.Fatal("ReadPID() returned 0 — child did not write its PID")
	}
	if childPID == os.Getpid() {
		t.Fatal("ReadPID() returned parent PID — expected child PID")
	}
}
