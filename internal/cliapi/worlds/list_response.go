// Package worlds provides CLI API types for world entities.
package worlds

import "time"

// WorldListItem is the CLI API representation of a single world in the list output.
// It preserves the existing --json shape from world list exactly.
type WorldListItem struct {
	Name       string    `json:"name"`
	State      string    `json:"state"`
	Health     string    `json:"health"`
	Agents     int       `json:"agents"`
	Queue      int       `json:"queue"`
	SourceRepo string    `json:"source_repo"`
	CreatedAt  time.Time `json:"created_at"`
}
