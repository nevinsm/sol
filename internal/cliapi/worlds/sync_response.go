package worlds

// SyncResponse is the CLI API response for world sync --json.
type SyncResponse struct {
	Name       string `json:"name"`
	Fetched    bool   `json:"fetched"`
	HeadCommit string `json:"head_commit"`
}
