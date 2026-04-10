// Package schema provides the CLI API types for schema management commands.
package schema

// StatusEntry is the CLI API representation of a single database's schema info.
// Used by `sol schema status --json`.
type StatusEntry struct {
	Database string `json:"database"`
	Type     string `json:"type"`
	Version  int    `json:"version"`
	Target   int    `json:"target"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}
