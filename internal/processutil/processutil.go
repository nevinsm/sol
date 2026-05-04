// Package processutil provides process lifecycle helpers shared across sol components.
package processutil

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// IsRunning reports whether a process with the given PID is alive and not a zombie.
//
// On Linux it reads /proc/{pid}/stat to detect zombie (defunct) processes, which
// appear alive to syscall.Kill(pid, 0). Falls back to the signal-0 probe on
// systems where /proc is unavailable (e.g. macOS).
func IsRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	// Attempt /proc-based check first (Linux).
	if alive, ok := isRunningProc(pid); ok {
		return alive
	}

	// Fallback: signal-0 probe (works on all Unix; cannot distinguish zombies).
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// StartDaemon launches solBin with the given args as a detached background process.
//
// stdout and stderr are both redirected to logPath (the directory is created if
// it does not exist). env is the full environment for the child process — callers
// typically pass append(os.Environ(), "SOL_HOME=<path>").
//
// The child is reaped in a background goroutine so it never becomes a zombie.
// Callers must NOT call proc.Process.Release() after StartDaemon — that would
// prevent Go's runtime from reaping the child, leaving a defunct process that
// IsRunning() would incorrectly report as alive.
//
// Returns the PID of the launched process.
func StartDaemon(logPath string, env []string, solBin string, args ...string) (int, error) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return 0, fmt.Errorf("create log directory: %w", err)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open log file: %w", err)
	}

	proc := exec.Command(solBin, args...)
	proc.Stdout = logFile
	proc.Stderr = logFile
	proc.Env = env
	proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := proc.Start(); err != nil {
		logFile.Close()
		return 0, fmt.Errorf("start process: %w", err)
	}

	pid := proc.Process.Pid
	logFile.Close()

	// Reap the child in the background so it never becomes a zombie when it
	// exits. We must not call Release() — that prevents Go's runtime from
	// waiting on the child, leaving a defunct process that IsRunning() would
	// incorrectly report as alive.
	go func() { _ = proc.Wait() }()

	return pid, nil
}

// pidFiles holds open file handles for active PID file locks.
// Key: absolute path, Value: *os.File with LOCK_EX held.
var pidFiles sync.Map

// hookBeforeCreateForTest, when non-nil, is called inside WritePID's create
// branch — between detecting that the PID file does not exist and attempting
// the atomic O_CREATE|O_EXCL open. Tests use it to deterministically simulate
// another caller winning the create race during this exact window. It is
// unset in production builds and adds no overhead beyond a nil-check.
var hookBeforeCreateForTest func()

// WritePID writes pid to the PID file at path, creating parent directories as
// needed.
//
// When pid equals the current process's PID, an exclusive advisory flock is
// acquired on the file and held for the process lifetime (until ClearPID is
// called). This prevents a recycled PID from being treated as the original
// process. If the flock cannot be acquired (another process holds it),
// WritePID returns an error.
//
// When pid belongs to a different process (e.g. a parent recording a child's
// PID), the file is written without locking. The child's own WritePID call
// will acquire the lock.
func WritePID(path string, pid int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for PID file %q: %w", path, err)
	}

	if pid == os.Getpid() {
		// If we already hold the lock for this path (e.g. called twice), just
		// update the file content via the existing fd.
		if v, ok := pidFiles.Load(path); ok {
			f := v.(*os.File)
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return fmt.Errorf("failed to seek PID file %q: %w", path, err)
			}
			if err := f.Truncate(0); err != nil {
				return fmt.Errorf("failed to truncate PID file %q: %w", path, err)
			}
			if _, err := fmt.Fprintf(f, "%d\n", pid); err != nil {
				return fmt.Errorf("failed to write PID file %q: %w", path, err)
			}
			return nil
		}

		// Try to open the existing file and flock it first. This ensures we
		// contend on the same inode as any previous holder (e.g. an orphan
		// process), rather than creating a new file on a new inode where
		// flock would vacuously succeed.
		f, err := os.OpenFile(path, os.O_RDWR, 0o644)
		if err == nil {
			// File exists — try non-blocking flock on the EXISTING inode.
			if flockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); flockErr != nil {
				f.Close()
				return fmt.Errorf("failed to lock PID file %q (another instance may be running): %w", path, flockErr)
			}
			// Got the lock on existing inode. Truncate and write our PID.
			if err := f.Truncate(0); err != nil {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				f.Close()
				return fmt.Errorf("failed to truncate PID file %q: %w", path, err)
			}
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				f.Close()
				return fmt.Errorf("failed to seek PID file %q: %w", path, err)
			}
			if _, err := fmt.Fprintf(f, "%d\n", pid); err != nil {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				f.Close()
				return fmt.Errorf("failed to write PID file %q: %w", path, err)
			}
			if old, loaded := pidFiles.Swap(path, f); loaded {
				old.(*os.File).Close()
			}
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to open PID file %q: %w", path, err)
		}

		// Test-only choreography hook. Lets a test simulate another caller
		// winning the create race in the window between the existing-file
		// open returning ENOENT and our atomic O_EXCL create below.
		if hookBeforeCreateForTest != nil {
			hookBeforeCreateForTest()
		}

		// No existing file — try to atomically create one. O_EXCL ensures
		// only one creator wins; concurrent first-time callers that lose
		// observe EEXIST and fall back to the existing-file branch by
		// re-entering WritePID. Crucially we do NOT pass O_TRUNC: a losing
		// caller must never truncate content that the winning caller has
		// already written under its flock.
		f, err = os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			// Lost the create race. Re-enter; the recursive call takes the
			// existing-file branch (the file now exists) and contends on the
			// winner's flock there. At most one fall-through: once the file
			// exists it stays existing for the lifetime of this call (the
			// package preserves the inode — ClearPID truncates, never unlinks).
			return WritePID(path, pid)
		}
		if err != nil {
			return fmt.Errorf("failed to create PID file %q: %w", path, err)
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			f.Close()
			return fmt.Errorf("failed to lock PID file %q (another instance may be running): %w", path, err)
		}
		if _, err := fmt.Fprintf(f, "%d\n", pid); err != nil {
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
			return fmt.Errorf("failed to write PID file %q: %w", path, err)
		}
		if old, loaded := pidFiles.Swap(path, f); loaded {
			old.(*os.File).Close()
		}
		return nil
	}

	// Writing another process's PID — informational write without locking.
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", pid)), 0o644)
}

// ReadPID reads the PID from the file at path. Returns 0, nil if the file does
// not exist. Returns 0, error if the content is not a valid integer.
func ReadPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read PID file %q: %w", path, err)
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		// Empty/truncated file (e.g. after ClearPID) — treat as no PID.
		return 0, nil
	}
	pid, err := strconv.Atoi(content)
	if err != nil {
		return 0, fmt.Errorf("invalid PID file content in %q: %w", path, err)
	}
	return pid, nil
}

// ClearPIDIfMatches truncates the PID file at path only when it is safe to do
// so from the perspective of a parent process that has no claim on the file.
// Safe to clear means one of:
//   - the file does not exist
//   - the file is empty (no PID recorded)
//   - the file contains an invalid/unreadable PID
//   - the file contains a PID that is not alive
//   - the file contains exactly expectedPid (our own spawned child)
//
// If the file contains a live PID that is NOT expectedPid, the file is left
// untouched — another process owns it, and truncating would destroy its only
// record of its own PID. Returns nil in the left-alone case.
//
// This is the defensive variant of ClearPID; use it at sites where a spawned
// child may have legitimately exited after detecting another running instance.
func ClearPIDIfMatches(path string, expectedPid int) error {
	pid, err := ReadPID(path)
	if err != nil {
		// Unreadable content — safe to clear.
		return ClearPID(path)
	}
	if pid == 0 || pid == expectedPid || !IsRunning(pid) {
		return ClearPID(path)
	}
	// A different live process owns this file. Leave it alone.
	return nil
}

// ClearPID releases any held flock on the PID file at path by truncating it.
// The file is NOT deleted — this preserves the inode so future flock attempts
// contend on the same inode, preventing orphan processes from accumulating.
// It is safe to call when no lock is held or when the file does not exist.
func ClearPID(path string) error {
	if v, ok := pidFiles.LoadAndDelete(path); ok {
		f := v.(*os.File)
		_ = f.Truncate(0)
		f.Close() // releases flock
		return nil
	}
	// No held file handle — truncate via path (e.g. clearing a child's PID file).
	if err := os.Truncate(path, 0); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to truncate PID file %q: %w", path, err)
	}
	return nil
}

// GracefulKill sends SIGTERM to pid and waits up to timeout for the process to
// exit. If the process is still running after the timeout, SIGKILL is sent.
// Returns nil if the process exits cleanly or is already gone.
func GracefulKill(pid int, timeout time.Duration) error {
	if pid <= 0 {
		return fmt.Errorf("invalid PID: %d", pid)
	}
	if !IsRunning(pid) {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		if !IsRunning(pid) {
			return nil // already gone
		}
		return fmt.Errorf("failed to send SIGTERM to pid %d: %w", pid, err)
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !IsRunning(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Grace period expired — force kill.
	if !IsRunning(pid) {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		if !IsRunning(pid) {
			return nil
		}
		return fmt.Errorf("failed to send SIGKILL to pid %d: %w", pid, err)
	}

	// Wait for process to actually die after SIGKILL.
	for i := 0; i < 10; i++ {
		if !IsRunning(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("process %d did not exit after SIGKILL within 1s", pid)
}

// FindSolSubcommandPIDs scans /proc for processes owned by the current user
// whose argv begins with `<sol binary> subcmd[0] subcmd[1] ...`. The args[0]
// entry is matched by basename (must equal "sol"). The calling process is
// excluded.
//
// Matching is prefix-based: the provided subcmd tokens must appear at
// positions 1..len(subcmd) of the target's argv, in order. Trailing argv
// entries after the matched prefix are allowed, which means flag-bearing
// invocations like `sol forge run --world=sol-dev` are matched by passing
// []string{"forge", "run"}. This allows per-world daemons to be located
// without the caller having to reconstruct the exact flag arguments.
//
// This is a narrow recovery helper for the daemon-pidfile bug: when a daemon's
// pidfile has been truncated but the daemon itself is still running, this
// lookup locates the orphan process so the caller can SIGTERM it before
// retrying start. Returns nil, nil on systems without /proc.
func FindSolSubcommandPIDs(subcmd ...string) ([]int, error) {
	if len(subcmd) == 0 {
		return nil, fmt.Errorf("FindSolSubcommandPIDs: at least one subcommand arg required")
	}
	if _, err := os.Stat("/proc"); err != nil {
		return nil, nil
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("read /proc: %w", err)
	}
	self := os.Getpid()
	uid := os.Getuid()

	var matches []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, convErr := strconv.Atoi(e.Name())
		if convErr != nil || pid <= 0 || pid == self {
			continue
		}
		// Ownership check: skip processes owned by other users.
		info, statErr := os.Stat("/proc/" + e.Name())
		if statErr != nil {
			continue
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(st.Uid) != uid {
			continue
		}
		// Read argv.
		data, readErr := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if readErr != nil || len(data) == 0 {
			continue
		}
		// cmdline is NUL-separated; often has a trailing NUL.
		raw := strings.TrimRight(string(data), "\x00")
		args := strings.Split(raw, "\x00")
		if len(args) < 1+len(subcmd) {
			continue
		}
		if filepath.Base(args[0]) != "sol" {
			continue
		}
		match := true
		for i, want := range subcmd {
			if args[1+i] != want {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		matches = append(matches, pid)
	}
	return matches, nil
}

// isRunningProc reads /proc/{pid}/stat and returns (alive, true) when /proc is
// available. Returns (false, false) when /proc is not present so the caller can
// fall back to a different method.
func isRunningProc(pid int) (alive bool, ok bool) {
	path := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(path)
	if err != nil {
		// /proc not available or process already gone.
		if os.IsNotExist(err) {
			// The file not existing could mean no /proc at all, or process gone.
			// If /proc itself doesn't exist we can't tell — signal the caller to
			// fall back.
			if _, statErr := os.Stat("/proc"); os.IsNotExist(statErr) {
				return false, false // /proc not available — use fallback
			}
			return false, true // process is gone
		}
		return false, false // unreadable — use fallback
	}

	// /proc/{pid}/stat format: "pid (comm) state ..."
	// The state field is the third token. Locate it after the closing ')' of the
	// comm field because the comm itself can contain spaces and parentheses.
	s := string(data)
	lastParen := strings.LastIndex(s, ")")
	if lastParen < 0 || lastParen+2 >= len(s) {
		return false, false // unexpected format — use fallback
	}
	// After the closing paren there is a space, then the single-character state.
	state := s[lastParen+2]
	if state == 'Z' {
		return false, true // zombie — not usefully alive
	}
	return true, true
}
