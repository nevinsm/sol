// Package forge provides the CLI API types for forge/merge-request entities.
package forge

import (
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// MergeRequest is the CLI API representation of a merge request.
type MergeRequest struct {
	ID        string     `json:"id"`
	WritID    string     `json:"writ_id"`
	Branch    string     `json:"branch"`
	Status    string     `json:"status"`
	Phase     string     `json:"phase"`
	BlockedBy string     `json:"blocked_by,omitempty"`
	Attempts  int        `json:"attempts"`
	QueuedAt  time.Time  `json:"queued_at"`
	MergedAt  *time.Time `json:"merged_at,omitempty"`
}

// ForgeStatus is the CLI API representation of the forge's runtime state.
type ForgeStatus struct {
	Running        bool       `json:"running"`
	Paused         bool       `json:"paused"`
	QueueDepth     int        `json:"queue_depth"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at,omitempty"`
	CurrentMR      string     `json:"current_mr,omitempty"`
}

// ForgeHeartbeat holds the information needed to build a ForgeStatus.
type ForgeHeartbeat struct {
	Timestamp  time.Time
	Status     string
	QueueDepth int
	CurrentMR  string
}

// FromStoreMR converts a store.MergeRequest to the CLI API MergeRequest type.
func FromStoreMR(mr store.MergeRequest) MergeRequest {
	return MergeRequest{
		ID:        mr.ID,
		WritID:    mr.WritID,
		Branch:    mr.Branch,
		Status:    mr.Phase, // store uses "Phase" for what the API calls status
		Phase:     mr.Phase,
		BlockedBy: mr.BlockedBy,
		Attempts:  mr.Attempts,
		QueuedAt:  mr.CreatedAt,
		MergedAt:  mr.MergedAt,
	}
}

// FromForgeStatus builds a ForgeStatus from runtime forge state.
func FromForgeStatus(running, paused bool, hb *ForgeHeartbeat) ForgeStatus {
	fs := ForgeStatus{
		Running: running,
		Paused:  paused,
	}
	if hb != nil {
		ts := hb.Timestamp
		fs.LastHeartbeatAt = &ts
		fs.QueueDepth = hb.QueueDepth
		fs.CurrentMR = hb.CurrentMR
	}
	return fs
}
