package accounts

// DeleteResponse is the CLI API response for `sol account delete --json`.
type DeleteResponse struct {
	Handle  string `json:"handle"`
	Deleted bool   `json:"deleted"`
}
