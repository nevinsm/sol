// Package prefect provides the CLI API types for prefect command output.
package prefect

// StatusResponse is the CLI API representation of prefect status --json output.
type StatusResponse struct {
	Status        string `json:"status"`
	PID           int    `json:"pid,omitempty"`
	UptimeSeconds int    `json:"uptime_seconds,omitempty"`
}
