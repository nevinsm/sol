// Package chronicle provides the CLI API types for chronicle command output.
package chronicle

// StatusResponse is the CLI API representation of chronicle status --json output.
//
// Optional fields use pointer types so that they are omitted from JSON when nil,
// matching the pre-migration map[string]any behavior where keys were only added
// conditionally.
type StatusResponse struct {
	Status           string `json:"status"`
	PID              *int   `json:"pid,omitempty"`
	CheckpointOffset *int64 `json:"checkpoint_offset,omitempty"`
	EventsProcessed  *int64 `json:"events_processed,omitempty"`
	HeartbeatAge     string `json:"heartbeat_age,omitempty"`
}
