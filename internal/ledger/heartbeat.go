package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/fileutil"
)

// Heartbeat holds the ledger's periodic health state.
type Heartbeat struct {
	Timestamp       time.Time `json:"timestamp"`
	Status          string    `json:"status"` // "running", "stopping"
	RequestsTotal   int64     `json:"requests_total"`
	TokensProcessed int64     `json:"tokens_processed"`
	WorldsWritten   int       `json:"worlds_written"`
}

// IsStale returns true if the heartbeat is older than maxAge.
func (h *Heartbeat) IsStale(maxAge time.Duration) bool {
	return time.Since(h.Timestamp) > maxAge
}

// HeartbeatPath returns the path to the ledger heartbeat file.
func HeartbeatPath() string {
	return filepath.Join(config.RuntimeDir(), "ledger-heartbeat.json")
}

// WriteHeartbeat writes the heartbeat to disk atomically.
func WriteHeartbeat(hb Heartbeat) error {
	path := HeartbeatPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(hb)
	if err != nil {
		return err
	}
	return fileutil.AtomicWrite(path, data, 0o644)
}

// ReadHeartbeat reads the ledger heartbeat from disk.
// Returns nil, nil if no heartbeat file exists.
func ReadHeartbeat() (*Heartbeat, error) {
	data, err := os.ReadFile(HeartbeatPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var hb Heartbeat
	if err := json.Unmarshal(data, &hb); err != nil {
		return nil, err
	}
	return &hb, nil
}

// RemoveHeartbeat removes the heartbeat file.
func RemoveHeartbeat() {
	_ = os.Remove(HeartbeatPath())
}
