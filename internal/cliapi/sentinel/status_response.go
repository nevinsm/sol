// Package sentinel provides the CLI API types for sentinel command output.
package sentinel

// StatusResponse is the CLI API representation of sentinel status --json output.
type StatusResponse struct {
	World         string `json:"world"`
	Running       bool   `json:"running"`
	PID           int    `json:"pid,omitempty"`
	PatrolCount   int    `json:"patrol_count,omitempty"`
	AgentsChecked int    `json:"agents_checked,omitempty"`
	StalledCount  int    `json:"stalled_count,omitempty"`
	ReapedCount   int    `json:"reaped_count,omitempty"`
	HeartbeatAge  string `json:"heartbeat_age,omitempty"`
	Status        string `json:"status,omitempty"`
}
