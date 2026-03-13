package prefect

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/processutil"
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
	return processutil.WritePID(pidPath(), pidSelf())
}

// ReadPID reads the PID from the PID file. Returns 0 if no file exists.
func ReadPID() (int, error) {
	return processutil.ReadPID(pidPath())
}

// ClearPID removes the PID file.
func ClearPID() error {
	return processutil.ClearPID(pidPath())
}

// IsRunning checks if a process with the given PID is alive and not a zombie.
// It delegates to processutil.IsRunning for zombie-aware detection.
func IsRunning(pid int) bool {
	return processutil.IsRunning(pid)
}

// ReadDaemonPID reads the PID from a named daemon's PID file
// at $SOL_HOME/.runtime/{name}.pid. Returns 0 if no file exists
// or the content is invalid.
func ReadDaemonPID(name string) int {
	pid, _ := processutil.ReadPID(filepath.Join(config.RuntimeDir(), name+".pid"))
	return pid
}

// WriteDaemonPID writes a PID to a named daemon's PID file
// at $SOL_HOME/.runtime/{name}.pid.
func WriteDaemonPID(name string, pid int) error {
	return processutil.WritePID(filepath.Join(config.RuntimeDir(), name+".pid"), pid)
}
