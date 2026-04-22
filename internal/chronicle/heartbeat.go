package chronicle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/heartbeat"
)

// Heartbeat records the chronicle's liveness state.
type Heartbeat struct {
	Timestamp        time.Time `json:"timestamp"`
	Status           string    `json:"status"`            // "running", "stopping"
	EventsProcessed  int64     `json:"events_processed"`  // total events processed
	CheckpointOffset int64     `json:"checkpoint_offset"` // current file offset
}

// HeartbeatPath returns the path to the heartbeat file.
// $SOL_HOME/.runtime/chronicle-heartbeat.json
func HeartbeatPath() string {
	return filepath.Join(config.RuntimeDir(), "chronicle-heartbeat.json")
}

// WriteHeartbeat writes the heartbeat file atomically.
// Creates the parent directory if needed.
func WriteHeartbeat(hb *Heartbeat) error {
	path := HeartbeatPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}
	return heartbeat.Write(path, hb)
}

// ReadHeartbeat reads the current heartbeat file.
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
