package caravans

// LaunchResponse is the CLI API response for caravan launch --json output.
type LaunchResponse struct {
	CaravanID  string       `json:"caravan_id"`
	World      string       `json:"world"`
	Dispatched []LaunchItem `json:"dispatched"`
	Blocked    int          `json:"blocked"`
	AutoClosed bool         `json:"auto_closed"`
}

// LaunchItem describes a single dispatched item from a caravan launch.
type LaunchItem struct {
	WritID      string `json:"writ_id"`
	AgentName   string `json:"agent_name"`
	SessionName string `json:"session_name"`
}
