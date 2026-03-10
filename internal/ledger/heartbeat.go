package ledger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/config"
)

// Heartbeat records the ledger's liveness state.
type Heartbeat struct {
	Timestamp time.Time `json:"timestamp"`
	Status    string    `json:"status"` // "running", "stopping"
}

// HeartbeatPath returns the path to the ledger heartbeat file.
func HeartbeatPath() string {
	return filepath.Join(config.RuntimeDir(), "ledger-heartbeat.json")
}

// WriteHeartbeat writes the heartbeat file atomically.
func WriteHeartbeat(hb *Heartbeat) error {
	dir := config.RuntimeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}

	data, err := json.MarshalIndent(hb, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat: %w", err)
	}

	path := HeartbeatPath()
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

// ReadHeartbeat reads the current ledger heartbeat file.
// Returns nil, nil if no heartbeat file exists.
func ReadHeartbeat() (*Heartbeat, error) {
	data, err := os.ReadFile(HeartbeatPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read ledger heartbeat: %w", err)
	}

	var hb Heartbeat
	if err := json.Unmarshal(data, &hb); err != nil {
		return nil, fmt.Errorf("failed to parse ledger heartbeat: %w", err)
	}
	return &hb, nil
}

// IsStale returns true if the heartbeat is older than maxAge.
func (hb *Heartbeat) IsStale(maxAge time.Duration) bool {
	return time.Since(hb.Timestamp) > maxAge
}

// RemoveHeartbeat removes the heartbeat file on clean shutdown.
func RemoveHeartbeat() {
	_ = os.Remove(HeartbeatPath())
}
