package caravans

// DeleteResponse is the CLI API response for caravan delete --json output.
type DeleteResponse struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}
