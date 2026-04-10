// Package workflows provides the CLI API types for workflow command output.
package workflows

import "github.com/nevinsm/sol/internal/workflow"

// ShowResponse is the CLI API representation of workflow show --json output.
type ShowResponse struct {
	Name        string                  `json:"name"`
	Type        string                  `json:"type"`
	Description string                  `json:"description,omitempty"`
	Manifest    bool                    `json:"manifest"`
	Tier        workflow.Tier           `json:"tier"`
	Path        string                  `json:"path"`
	Valid       bool                    `json:"valid"`
	Error       string                  `json:"error,omitempty"`
	Variables   map[string]ShowVariable `json:"variables,omitempty"`
	Steps       []ShowStep              `json:"steps,omitempty"`
}

// ShowVariable describes a workflow variable in show --json output.
type ShowVariable struct {
	Required bool   `json:"required"`
	Default  string `json:"default,omitempty"`
}

// ShowStep describes a workflow step in show --json output.
type ShowStep struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Instructions string   `json:"instructions"`
	Needs        []string `json:"needs,omitempty"`
}
