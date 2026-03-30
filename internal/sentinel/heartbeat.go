package sentinel

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/heartbeat"
	"github.com/nevinsm/sol/internal/processutil"
)

// Heartbeat records the sentinel's liveness state.
type Heartbeat struct {
	Timestamp          time.Time `json:"timestamp"`
	Status             string    `json:"status"`               // "running", "assessing", "stopping"
	PatrolCount        int       `json:"patrol_count"`
	AgentsChecked      int       `json:"agents_checked"`
	StalledCount       int       `json:"stalled_count"`
	ReapedCount        int       `json:"reaped_count"`
	LastPatrolDuration string    `json:"last_patrol_duration"` // human-readable duration
}

// HeartbeatPath returns the path to the sentinel heartbeat file.
// $SOL_HOME/{world}/sentinel.heartbeat
func HeartbeatPath(world string) string {
	return filepath.Join(config.Home(), world, "sentinel.heartbeat")
}

// PIDPath returns the path to the sentinel PID file.
// $SOL_HOME/{world}/sentinel.pid
func PIDPath(world string) string {
	return filepath.Join(config.Home(), world, "sentinel.pid")
}

// LogPath returns the path to the sentinel log file.
// $SOL_HOME/{world}/sentinel.log
func LogPath(world string) string {
	return filepath.Join(config.Home(), world, "sentinel.log")
}

// WriteHeartbeat writes the heartbeat file atomically.
// Creates the world directory if it does not exist.
func WriteHeartbeat(world string, hb *Heartbeat) error {
	path := HeartbeatPath(world)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create world directory: %w", err)
	}
	return heartbeat.Write(path, hb)
}

// ReadHeartbeat reads the current sentinel heartbeat file.
// Returns nil, nil if no heartbeat file exists.
func ReadHeartbeat(world string) (*Heartbeat, error) {
	var hb Heartbeat
	if err := heartbeat.Read(HeartbeatPath(world), &hb); err != nil {
		if errors.Is(err, heartbeat.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &hb, nil
}

// IsStale returns true if the heartbeat is older than maxAge.
func (hb *Heartbeat) IsStale(maxAge time.Duration) bool {
	return heartbeat.IsStale(hb.Timestamp, maxAge)
}

// WritePID writes the sentinel PID to the PID file.
func WritePID(world string, pid int) error {
	return processutil.WritePID(PIDPath(world), pid)
}

// ReadPID reads the sentinel PID from its PID file. Returns 0 if not found.
func ReadPID(world string) int {
	pid, _ := processutil.ReadPID(PIDPath(world))
	return pid
}

// ClearPID removes the sentinel PID file.
func ClearPID(world string) {
	_ = processutil.ClearPID(PIDPath(world))
}

// ClearHeartbeat removes the sentinel heartbeat file.
func ClearHeartbeat(world string) {
	os.Remove(HeartbeatPath(world))
}

// IsRunning checks if a sentinel process is alive and not a zombie.
// It delegates to processutil.IsRunning for zombie-aware detection.
func IsRunning(pid int) bool {
	return processutil.IsRunning(pid)
}

// StopProcess sends SIGTERM to the sentinel process and waits for it to exit.
// Falls back to SIGKILL after the given timeout. Cleans up the PID file.
func StopProcess(world string, timeout time.Duration) error {
	pid := ReadPID(world)
	if pid <= 0 {
		return fmt.Errorf("no sentinel PID file for world %q", world)
	}
	if !IsRunning(pid) {
		ClearPID(world)
		return nil
	}
	if err := processutil.GracefulKill(pid, timeout); err != nil {
		return fmt.Errorf("failed to stop sentinel (pid %d): %w", pid, err)
	}
	ClearPID(world)
	return nil
}
