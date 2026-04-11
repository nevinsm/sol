// Package consul provides CLI API types for consul command output.
package consul

import (
	"time"

	"github.com/nevinsm/sol/internal/consul"
)

// StatusResponse is the CLI API representation of consul status.
//
// Fields are ordered alphabetically to match the pre-migration JSON output
// (which used map[string]any, serialised with sorted keys by encoding/json).
type StatusResponse struct {
	CaravanFeeds int    `json:"caravan_feeds"`
	Escalations  int    `json:"escalations"`
	PatrolCount  int    `json:"patrol_count"`
	PIDGone      bool   `json:"pid_gone"`
	Stale        bool   `json:"stale"`
	StaleTethers int    `json:"stale_tethers"`
	Status       string    `json:"status"`
	CheckedAt    time.Time `json:"checked_at"`
	Wedged       bool      `json:"wedged"`
}

// FromHeartbeat builds a StatusResponse from a consul Heartbeat and computed health flags.
func FromHeartbeat(hb *consul.Heartbeat, stale, pidGone, wedged bool) StatusResponse {
	return StatusResponse{
		CaravanFeeds: hb.CaravanFeeds,
		Escalations:  hb.Escalations,
		PatrolCount:  hb.PatrolCount,
		PIDGone:      pidGone,
		Stale:        stale,
		StaleTethers: hb.StaleTethers,
		Status:       hb.Status,
		CheckedAt:    hb.Timestamp.UTC(),
		Wedged:       wedged,
	}
}
