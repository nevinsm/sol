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

		// Acquire exclusive advisory lock for this process's lifetime.
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("failed to open PID file %q: %w", path, err)
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
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID file content in %q: %w", path, err)
	}
	return pid, nil
}

// ClearPID releases any held flock on the PID file at path and removes it.
// It is safe to call when no lock is held or when the file does not exist.
func ClearPID(path string) error {
	if v, ok := pidFiles.LoadAndDelete(path); ok {
		v.(*os.File).Close()
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove PID file %q: %w", path, err)
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
