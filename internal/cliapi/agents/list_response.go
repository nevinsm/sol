package agents

// AgentListRow is the CLI API type for a single row in `sol agent list --json`.
//
// This is a display-oriented DTO: fields like LastSeen and ActiveWrit are
// pre-formatted strings (relative timestamps, empty markers) rather than
// typed values. The canonical Agent type uses proper types; this type
// preserves the existing JSON shape for backward compatibility.
type AgentListRow struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	World      string `json:"world"`
	Role       string `json:"role"`
	State      string `json:"state"`
	ActiveWrit string `json:"active_writ"`
	Model      string `json:"model"`
	Account    string `json:"account"`
	LastSeen   string `json:"last_seen"`
}
