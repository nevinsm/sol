package sentinel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/config"
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
func WriteHeartbeat(world string, hb *Heartbeat) error {
	data, err := json.MarshalIndent(hb, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat: %w", err)
	}

	path := HeartbeatPath(world)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("failed to write heartbeat temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to rename heartbeat file: %w", err)
	}
	return nil
}

// ReadHeartbeat reads the current sentinel heartbeat file.
// Returns nil, nil if no heartbeat file exists.
func ReadHeartbeat(world string) (*Heartbeat, error) {
	data, err := os.ReadFile(HeartbeatPath(world))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read sentinel heartbeat: %w", err)
	}

	var hb Heartbeat
	if err := json.Unmarshal(data, &hb); err != nil {
		return nil, fmt.Errorf("failed to parse sentinel heartbeat: %w", err)
	}
	return &hb, nil
}

// IsStale returns true if the heartbeat is older than maxAge.
func (hb *Heartbeat) IsStale(maxAge time.Duration) bool {
	return time.Since(hb.Timestamp) > maxAge
}

// WritePID writes the sentinel PID to the PID file.
func WritePID(world string, pid int) error {
	path := PIDPath(world)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create PID directory: %w", err)
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("%d", pid)), 0o644)
}

// ReadPID reads the sentinel PID from its PID file. Returns 0 if not found.
func ReadPID(world string) int {
	data, err := os.ReadFile(PIDPath(world))
	if err != nil {
		return 0
	}
	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil {
		return 0
	}
	return pid
}

// ClearPID removes the sentinel PID file.
func ClearPID(world string) {
	os.Remove(PIDPath(world))
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
