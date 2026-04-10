// Package workflows provides the CLI API type for workflow metadata.
package workflows

// Workflow is the CLI API representation of workflow metadata.
type Workflow struct {
	Name   string `json:"name"`
	Scope  string `json:"scope"`
	Path   string `json:"path"`
	Source string `json:"source"`
}
