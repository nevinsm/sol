// Package broker provides the CLI API types for broker command output.
package broker

import (
	"time"

	ibroker "github.com/nevinsm/sol/internal/broker"
)

// RuntimeEntry is the CLI API representation of a single runtime's liveness state.
type RuntimeEntry struct {
	Runtime   string     `json:"runtime"`
	OK        bool       `json:"ok"`
	LastProbe *time.Time `json:"last_probe_at,omitempty"`
}

// StatusResponse is the CLI API representation of broker status --json output.
type StatusResponse struct {
	Status      string         `json:"status"`
	CheckedAt   time.Time      `json:"checked_at"`
	PatrolCount int            `json:"patrol_count"`
	Stale       bool           `json:"stale"`
	Runtimes    []RuntimeEntry `json:"runtimes,omitempty"`
}

// FromHeartbeat builds a StatusResponse from a broker.Heartbeat.
// staleDuration is the threshold for marking the heartbeat as stale.
func FromHeartbeat(hb *ibroker.Heartbeat, staleDuration time.Duration) StatusResponse {
	resp := StatusResponse{
		Status:      hb.Status,
		CheckedAt:   hb.Timestamp.UTC(),
		PatrolCount: hb.PatrolCount,
		Stale:       hb.IsStale(staleDuration),
	}

	for _, r := range hb.Runtimes {
		entry := RuntimeEntry{
			Runtime: r.Runtime,
			OK:      r.OK,
		}
		if !r.LastProbe.IsZero() {
			t := r.LastProbe.UTC()
			entry.LastProbe = &t
		}
		resp.Runtimes = append(resp.Runtimes, entry)
	}

	return resp
}
