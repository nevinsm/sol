package prefect

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/heartbeat"
)

// Heartbeat records the prefect's liveness state.
type Heartbeat struct {
	Timestamp      time.Time `json:"timestamp"`
	Status         string    `json:"status"`          // "running", "degraded", "stopping"
	HeartbeatCount int       `json:"heartbeat_count"` // total heartbeat cycles
	WorkingAgents  int       `json:"working_agents"`  // agents in working state
	DeadSessions   int       `json:"dead_sessions"`   // dead sessions detected this cycle
}

// HeartbeatPath returns the path to the prefect heartbeat file.
// $SOL_HOME/.runtime/prefect-heartbeat.json
func HeartbeatPath() string {
	return filepath.Join(config.RuntimeDir(), "prefect-heartbeat.json")
}

// WriteHeartbeat writes the heartbeat file atomically.
// Creates the runtime directory if needed.
func WriteHeartbeat(hb *Heartbeat) error {
	dir := config.RuntimeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}
	return heartbeat.Write(HeartbeatPath(), hb)
}

// ReadHeartbeat reads the current prefect heartbeat file.
// Returns nil, nil if no heartbeat file exists.
func ReadHeartbeat() (*Heartbeat, error) {
	var hb Heartbeat
	if err := heartbeat.Read(HeartbeatPath(), &hb); err != nil {
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

// ClearHeartbeat removes the prefect heartbeat file.
func ClearHeartbeat() {
	os.Remove(HeartbeatPath())
}
