package prefect

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/nevinsm/sol/internal/config"
)

// pidPath returns the path to the prefect PID file.
func pidPath() string {
	return filepath.Join(config.RuntimeDir(), "prefect.pid")
}

// pidSelf returns the current process PID.
func pidSelf() int {
	return os.Getpid()
}

// WritePID writes the current process PID to the PID file.
// Returns an error if a prefect is already running.
func WritePID() error {
	existing, err := ReadPID()
	if err != nil {
		return fmt.Errorf("failed to read existing PID: %w", err)
	}
	if existing != 0 && IsRunning(existing) {
		return fmt.Errorf("prefect already running (pid %d)", existing)
	}

	// Ensure runtime directory exists.
	if err := os.MkdirAll(filepath.Dir(pidPath()), 0o755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}

	return os.WriteFile(pidPath(), []byte(strconv.Itoa(pidSelf())), 0o644)
}

// ReadPID reads the PID from the PID file. Returns 0 if no file exists.
func ReadPID() (int, error) {
	data, err := os.ReadFile(pidPath())
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read PID file: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID file content: %w", err)
	}
	return pid, nil
}

// ClearPID removes the PID file.
func ClearPID() error {
	err := os.Remove(pidPath())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear PID file: %w", err)
	}
	return nil
}

// IsRunning checks if a process with the given PID is alive.
func IsRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// ReadDaemonPID reads the PID from a named daemon's PID file
// at $SOL_HOME/.runtime/{name}.pid. Returns 0 if no file exists
// or the content is invalid.
func ReadDaemonPID(name string) int {
	path := filepath.Join(config.RuntimeDir(), name+".pid")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// WriteDaemonPID writes a PID to a named daemon's PID file
// at $SOL_HOME/.runtime/{name}.pid.
func WriteDaemonPID(name string, pid int) error {
	dir := config.RuntimeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, name+".pid"), []byte(strconv.Itoa(pid)), 0o644)
}
