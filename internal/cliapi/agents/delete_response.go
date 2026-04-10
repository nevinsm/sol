package agents

// DeleteResponse is the CLI API response for agent deletion.
type DeleteResponse struct {
	Name    string `json:"name"`
	World   string `json:"world"`
	Deleted bool   `json:"deleted"`
}
