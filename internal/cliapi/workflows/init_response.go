package workflows

// InitResponse is the CLI API representation of workflow init --json output.
type InitResponse struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
	Path  string `json:"path"`
}
