package consul

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/heartbeat"
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
	return heartbeat.Write(HeartbeatPath(solHome), hb)
}

// ReadHeartbeat reads the current heartbeat file.
// Returns nil, nil if no heartbeat file exists.
func ReadHeartbeat(solHome string) (*Heartbeat, error) {
	var hb Heartbeat
	if err := heartbeat.Read(HeartbeatPath(solHome), &hb); err != nil {
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
