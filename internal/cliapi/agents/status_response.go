package agents

// EnvoyStatus is the CLI API representation of an envoy's runtime status.
type EnvoyStatus struct {
	World       string `json:"world"`
	Name        string `json:"name"`
	Running     bool   `json:"running"`
	SessionName string `json:"session_name"`
	State       string `json:"state,omitempty"`
	ActiveWrit  string `json:"active_writ_id,omitempty"`
}
