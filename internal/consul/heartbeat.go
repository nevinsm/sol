package consul

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/fileutil"
)

// Heartbeat records the consul's liveness state.
type Heartbeat struct {
	Timestamp        time.Time `json:"timestamp"`
	PatrolCount      int       `json:"patrol_count"`
	Status           string    `json:"status"`            // "running", "stopping"
	StaleTethers     int       `json:"stale_tethers"`     // recovered this patrol
	CaravanFeeds     int       `json:"caravan_feeds"`     // dispatched this patrol
	Escalations      int       `json:"escalations"`       // open escalation count
	OrphanedSessions int       `json:"orphaned_sessions"` // stopped this patrol
	EscRenotified    int       `json:"esc_renotified"`    // re-notified this patrol
	EscalationAlert  bool      `json:"escalation_alert"`  // buildup alert fired
}

// HeartbeatPath returns the path to the heartbeat file.
// $SOL_HOME/consul/heartbeat.json
func HeartbeatPath(solHome string) string {
	return filepath.Join(solHome, "consul", "heartbeat.json")
}

// WriteHeartbeat writes the heartbeat file atomically.
// Creates the consul directory if needed.
func WriteHeartbeat(solHome string, hb *Heartbeat) error {
	dir := filepath.Join(solHome, "consul")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create consul directory: %w", err)
	}
	if err := fileutil.AtomicWriteJSON(HeartbeatPath(solHome), hb, 0o644); err != nil {
		return fmt.Errorf("failed to write heartbeat: %w", err)
	}
	return nil
}

// ReadHeartbeat reads the current heartbeat file.
// Returns nil, nil if no heartbeat file exists.
func ReadHeartbeat(solHome string) (*Heartbeat, error) {
	data, err := os.ReadFile(HeartbeatPath(solHome))
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
