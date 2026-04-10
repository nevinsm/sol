// Package migrations provides the CLI API type for applied migration records.
package migrations

import (
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// MigrationApplied is the CLI API representation of a migration that has been applied.
type MigrationApplied struct {
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	AppliedAt time.Time `json:"applied_at"`
	Summary   string    `json:"summary"`
}

// FromStoreMigration converts a store.AppliedMigration to the CLI API MigrationApplied type.
func FromStoreMigration(m store.AppliedMigration) MigrationApplied {
	return MigrationApplied{
		Name:      m.Name,
		Version:   m.Version,
		AppliedAt: m.AppliedAt,
		Summary:   m.Summary,
	}
}
