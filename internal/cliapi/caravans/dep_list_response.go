package caravans

// DepInfo is a summary of a dependency relationship target.
type DepInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// DepListResponse is the CLI API representation of caravan dep list --json output.
type DepListResponse struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	DependsOn  []DepInfo `json:"depends_on"`
	DependedBy []DepInfo `json:"depended_by"`
}
