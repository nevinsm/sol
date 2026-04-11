package forge

import "time"

// ForgeStatusResponse is the CLI API representation of `forge status --json` output.
type ForgeStatusResponse struct {
	World       string              `json:"world"`
	Running     bool                `json:"running"`
	Paused      bool                `json:"paused"`
	PID         int                 `json:"pid,omitempty"`
	Merging     bool                `json:"merging,omitempty"`
	Ready       int                 `json:"ready"`
	Blocked     int                 `json:"blocked"`
	InProgress  int                 `json:"in_progress"`
	Failed      int                 `json:"failed"`
	Merged      int                 `json:"merged"`
	Total       int                 `json:"total"`
	ClaimedMR   *ForgeStatusMR      `json:"claimed_mr,omitempty"`
	LastMerge   *ForgeStatusEvent   `json:"last_merge,omitempty"`
	LastFailure *ForgeStatusEvent   `json:"last_failure,omitempty"`
}

// ForgeStatusMR describes the currently claimed merge request in forge status output.
type ForgeStatusMR struct {
	ID     string `json:"id"`
	WritID string `json:"writ_id"`
	Title  string `json:"title"`
	Branch string `json:"branch"`
	Age    string `json:"age"`
}

// ForgeStatusEvent describes a recent forge event (merge or failure) in status output.
type ForgeStatusEvent struct {
	MRID      string    `json:"mr_id"`
	Title     string    `json:"title,omitempty"`
	Branch    string    `json:"branch"`
	Timestamp time.Time `json:"occurred_at"`
}
