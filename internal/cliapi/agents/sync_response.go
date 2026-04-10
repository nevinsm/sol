package agents

// SyncResponse is the CLI API response for envoy sync.
type SyncResponse struct {
	Name   string `json:"name"`
	World  string `json:"world"`
	Synced bool   `json:"synced"`
}
