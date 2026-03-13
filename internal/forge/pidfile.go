package forge

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/processutil"
)

// PIDPath returns the path to the forge PID file for a world.
func PIDPath(world string) string {
	return filepath.Join(config.Home(), world, "forge", "forge.pid")
}

// WritePID writes a PID to the forge PID file.
func WritePID(world string, pid int) error {
	return processutil.WritePID(PIDPath(world), pid)
}

// ReadPID reads the PID from the forge PID file. Returns 0 if no file exists
// or the content is invalid.
func ReadPID(world string) int {
	pid, _ := processutil.ReadPID(PIDPath(world))
	return pid
}

// ClearPID removes the forge PID file.
func ClearPID(world string) {
	_ = processutil.ClearPID(PIDPath(world))
}

// IsRunning checks if a process with the given PID is alive and not a zombie.
// It delegates to processutil.IsRunning for zombie-aware detection.
func IsRunning(pid int) bool {
	return processutil.IsRunning(pid)
}

// StopProcess sends SIGTERM to the forge process and waits for it to exit.
// Falls back to SIGKILL after the given timeout. Cleans up the PID file.
func StopProcess(world string, timeout time.Duration) error {
	pid := ReadPID(world)
	if pid <= 0 {
		return fmt.Errorf("no forge PID file for world %q", world)
	}
	if !IsRunning(pid) {
		ClearPID(world)
		return nil
	}
	if err := processutil.GracefulKill(pid, timeout); err != nil {
		return fmt.Errorf("failed to stop forge (pid %d): %w", pid, err)
	}
	ClearPID(world)
	return nil
}
