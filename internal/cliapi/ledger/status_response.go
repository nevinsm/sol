// Package ledger provides the CLI API types for ledger command output.
package ledger

// StatusResponse is the CLI API representation of ledger status --json output.
type StatusResponse struct {
	Status          string `json:"status"`
	PID             int    `json:"pid,omitempty"`
	Port            int    `json:"port,omitempty"`
	HeartbeatAge    string `json:"heartbeat_age,omitempty"`
	RequestsTotal   *int64 `json:"requests_total,omitempty"`
	TokensProcessed *int64 `json:"tokens_processed,omitempty"`
	WorldsWritten   *int   `json:"worlds_written,omitempty"`
}
