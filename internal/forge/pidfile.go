package forge

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/config"
)

// PIDPath returns the path to the forge PID file for a world.
func PIDPath(world string) string {
	return filepath.Join(config.Home(), world, "forge", "forge.pid")
}

// WritePID writes a PID to the forge PID file.
func WritePID(world string, pid int) error {
	dir := filepath.Join(config.Home(), world, "forge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create forge directory: %w", err)
	}
	return os.WriteFile(PIDPath(world), []byte(strconv.Itoa(pid)), 0o644)
}

// ReadPID reads the PID from the forge PID file. Returns 0 if no file exists
// or the content is invalid.
func ReadPID(world string) int {
	data, err := os.ReadFile(PIDPath(world))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// ClearPID removes the forge PID file.
func ClearPID(world string) {
	os.Remove(PIDPath(world))
}

// IsRunning checks if a process with the given PID is alive.
func IsRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
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

	// Send SIGTERM.
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		ClearPID(world)
		return fmt.Errorf("failed to send SIGTERM to forge (pid %d): %w", pid, err)
	}

	// Wait for graceful shutdown.
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !IsRunning(pid) {
			ClearPID(world)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Fallback to SIGKILL.
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		if !IsRunning(pid) {
			ClearPID(world)
			return nil
		}
		return fmt.Errorf("failed to send SIGKILL to forge (pid %d): %w", pid, err)
	}

	// Brief wait for SIGKILL to take effect.
	time.Sleep(200 * time.Millisecond)
	ClearPID(world)
	return nil
}
