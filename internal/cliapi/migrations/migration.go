// Package migrations provides the CLI API types for migration command output.
package migrations

import (
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/migrate"
	"github.com/nevinsm/sol/internal/store"
)

// MigrationApplied is the CLI API representation of a migration that has been applied.
type MigrationApplied struct {
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	AppliedAt time.Time `json:"applied_at"`
	Summary   string    `json:"summary"`
}

// MigrationStatus is the CLI API representation of a migration's current status,
// as returned by migrate list --json.
type MigrationStatus struct {
	Name      string     `json:"name"`
	Version   string     `json:"version"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Reason    string     `json:"reason,omitempty"`
	AppliedAt *time.Time `json:"applied_at,omitempty"`
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

// FromStoreMigrations converts a slice of store.AppliedMigration to CLI API types.
func FromStoreMigrations(rows []store.AppliedMigration) []MigrationApplied {
	out := make([]MigrationApplied, len(rows))
	for i, r := range rows {
		out[i] = FromStoreMigration(r)
	}
	return out
}

// FromMigrateStatus converts a migrate.Status to the CLI API MigrationStatus type.
func FromMigrateStatus(s migrate.Status) MigrationStatus {
	ms := MigrationStatus{
		Name:    s.Migration.Name,
		Version: s.Migration.Version,
		Title:   s.Migration.Title,
	}
	switch {
	case s.Applied:
		ms.Status = "applied"
		ms.AppliedAt = &s.AppliedAt
	case strings.HasPrefix(s.Reason, "detect error:"):
		ms.Status = "error"
		ms.Reason = s.Reason
	case s.Needed:
		ms.Status = "pending"
		ms.Reason = s.Reason
	default:
		ms.Status = "not-needed"
		ms.Reason = s.Reason
	}
	return ms
}

// FromMigrateStatuses converts a slice of migrate.Status to CLI API types.
func FromMigrateStatuses(statuses []migrate.Status) []MigrationStatus {
	out := make([]MigrationStatus, len(statuses))
	for i, s := range statuses {
		out[i] = FromMigrateStatus(s)
	}
	return out
}
