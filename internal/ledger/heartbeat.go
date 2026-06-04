package ledger

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/heartbeat"
)

// Heartbeat holds the ledger's periodic health state.
type Heartbeat struct {
	Timestamp       time.Time `json:"timestamp"`
	Status          string    `json:"status"` // "running", "stopping"
	RequestsTotal   int64     `json:"requests_total"`
	TokensProcessed int64     `json:"tokens_processed"` // aggregate total across all categories
	// Per-category token breakdown. These fields are available for operators
	// and tooling that needs cost attribution (cache_read ≈ 10× cheaper than
	// input). Current display surfaces (sol status, dash) render only the
	// aggregate TokensProcessed; the per-category fields are available here
	// for future renderers or direct JSON inspection of the heartbeat file.
	TokensInput         int64 `json:"tokens_input"`
	TokensOutput        int64 `json:"tokens_output"`
	TokensCacheRead     int64 `json:"tokens_cache_read"`
	TokensCacheCreation int64 `json:"tokens_cache_creation"`
	TokensReasoning     int64 `json:"tokens_reasoning"`
	WorldsWritten       int   `json:"worlds_written"`
}

// IsStale returns true if the heartbeat is older than maxAge.
func (h *Heartbeat) IsStale(maxAge time.Duration) bool {
	return heartbeat.IsStale(h.Timestamp, maxAge)
}

// HeartbeatPath returns the path to the ledger heartbeat file.
func HeartbeatPath() string {
	return filepath.Join(config.RuntimeDir(), "ledger-heartbeat.json")
}

// WriteHeartbeat writes the heartbeat to disk atomically.
// Uses compact JSON (not indented) since this file is machine-read only.
func WriteHeartbeat(hb Heartbeat) error {
	path := HeartbeatPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return heartbeat.WriteCompact(path, hb)
}

// ReadHeartbeat reads the ledger heartbeat from disk.
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

// RemoveHeartbeat removes the heartbeat file.
func RemoveHeartbeat() {
	_ = os.Remove(HeartbeatPath())
}
