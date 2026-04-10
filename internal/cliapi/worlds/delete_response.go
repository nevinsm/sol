package worlds

// DeleteResponse is the CLI API response for world delete --json.
type DeleteResponse struct {
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}
