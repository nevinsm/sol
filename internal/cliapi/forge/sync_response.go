package forge

// ForgeSyncResponse is the CLI API representation of `forge sync --json` output.
type ForgeSyncResponse struct {
	World      string `json:"world"`
	Fetched    bool   `json:"fetched"`
	HeadCommit string `json:"head_commit"`
}
