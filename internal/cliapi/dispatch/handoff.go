package dispatch

import "time"

// HandoffResult is the CLI API response for the "sol handoff" command.
type HandoffResult struct {
	WritID      string    `json:"writ_id"`
	Agent       string    `json:"agent"`
	OldSession  string    `json:"old_session"`
	NewSession  string    `json:"new_session"`
	HandedOffAt time.Time `json:"handed_off_at"`
}
