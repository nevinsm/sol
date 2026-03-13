package chronicle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/fileutil"
)

// Heartbeat records the chronicle's liveness state.
type Heartbeat struct {
	Timestamp       time.Time `json:"timestamp"`
	Status          string    `json:"status"`           // "running", "stopping"
	EventsProcessed int64     `json:"events_processed"` // total events processed
	CheckpointOffset int64    `json:"checkpoint_offset"` // current file offset
}

// HeartbeatPath returns the path to the heartbeat file.
// $SOL_HOME/chronicle.heartbeat
func HeartbeatPath() string {
	return filepath.Join(config.Home(), "chronicle.heartbeat")
}

// WriteHeartbeat writes the heartbeat file atomically.
func WriteHeartbeat(hb *Heartbeat) error {
	if err := fileutil.AtomicWriteJSON(HeartbeatPath(), hb, 0o644); err != nil {
		return fmt.Errorf("failed to write heartbeat: %w", err)
	}
	return nil
}

// ReadHeartbeat reads the current heartbeat file.
// Returns nil, nil if no heartbeat file exists.
func ReadHeartbeat() (*Heartbeat, error) {
	data, err := os.ReadFile(HeartbeatPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read heartbeat: %w", err)
	}

	var hb Heartbeat
	if err := json.Unmarshal(data, &hb); err != nil {
		return nil, fmt.Errorf("failed to parse heartbeat: %w", err)
	}
	return &hb, nil
}

// IsStale returns true if the heartbeat is older than maxAge.
func (hb *Heartbeat) IsStale(maxAge time.Duration) bool {
	return time.Since(hb.Timestamp) > maxAge
}
