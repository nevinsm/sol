package deacon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Heartbeat records the deacon's liveness state.
type Heartbeat struct {
	Timestamp   time.Time `json:"timestamp"`
	PatrolCount int       `json:"patrol_count"`
	Status      string    `json:"status"` // "running", "stopping"
	StaleHooks  int       `json:"stale_hooks"`  // recovered this patrol
	ConvoyFeeds int       `json:"convoy_feeds"` // dispatched this patrol
	Escalations int       `json:"escalations"`  // open escalation count
}

// HeartbeatPath returns the path to the heartbeat file.
// $GT_HOME/deacon/heartbeat.json
func HeartbeatPath(gtHome string) string {
	return filepath.Join(gtHome, "deacon", "heartbeat.json")
}

// WriteHeartbeat writes the heartbeat file atomically.
// Creates the deacon directory if needed.
func WriteHeartbeat(gtHome string, hb *Heartbeat) error {
	dir := filepath.Join(gtHome, "deacon")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create deacon directory: %w", err)
	}

	data, err := json.MarshalIndent(hb, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat: %w", err)
	}

	// Write to temp file, then rename for atomicity.
	tmp := HeartbeatPath(gtHome) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("failed to write heartbeat temp file: %w", err)
	}
	if err := os.Rename(tmp, HeartbeatPath(gtHome)); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to rename heartbeat file: %w", err)
	}
	return nil
}

// ReadHeartbeat reads the current heartbeat file.
// Returns nil, nil if no heartbeat file exists.
func ReadHeartbeat(gtHome string) (*Heartbeat, error) {
	data, err := os.ReadFile(HeartbeatPath(gtHome))
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
