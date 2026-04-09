package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// AppliedMigration is a single row from the migrations_applied table. It
// records the identity of a migration that has been successfully applied
// to this sphere, along with a short summary and any structured details
// the migration returned.
type AppliedMigration struct {
	Name      string         `json:"name"`
	Version   string         `json:"version"`
	AppliedAt time.Time      `json:"applied_at"`
	Summary   string         `json:"summary"`
	Details   map[string]any `json:"details"`
}

// RecordMigrationApplied inserts a row into migrations_applied. Called by
// the migrate package after a successful migration run. The details map is
// JSON-encoded for storage; nil is stored as an empty object.
func (s *SphereStore) RecordMigrationApplied(name, version, summary string, details map[string]any) error {
	if name == "" {
		return errors.New("RecordMigrationApplied: name must not be empty")
	}
	if details == nil {
		details = map[string]any{}
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("failed to encode migration details for %q: %w", name, err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(
		`INSERT INTO migrations_applied (name, version, applied_at, summary, details)
		 VALUES (?, ?, ?, ?, ?)`,
		name, version, now, summary, string(detailsJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to record applied migration %q: %w", name, err)
	}
	return nil
}

// ListAppliedMigrations returns all rows from migrations_applied, newest
// first. Callers that need a map keyed by Name should build one.
func (s *SphereStore) ListAppliedMigrations() ([]AppliedMigration, error) {
	rows, err := s.db.Query(
		`SELECT name, version, applied_at, summary, details
		 FROM migrations_applied
		 ORDER BY applied_at DESC, name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list applied migrations: %w", err)
	}
	defer rows.Close()

	var out []AppliedMigration
	for rows.Next() {
		var (
			name, version, appliedAt, summary, detailsJSON string
		)
		if err := rows.Scan(&name, &version, &appliedAt, &summary, &detailsJSON); err != nil {
			return nil, fmt.Errorf("failed to scan migrations_applied row: %w", err)
		}
		t, parseErr := parseRFC3339(appliedAt, "applied_at", "migration "+name)
		if parseErr != nil {
			return nil, parseErr
		}
		details := map[string]any{}
		if detailsJSON != "" {
			if err := json.Unmarshal([]byte(detailsJSON), &details); err != nil {
				return nil, fmt.Errorf("failed to decode details for migration %q: %w", name, err)
			}
		}
		out = append(out, AppliedMigration{
			Name:      name,
			Version:   version,
			AppliedAt: t,
			Summary:   summary,
			Details:   details,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating migrations_applied: %w", err)
	}
	return out, nil
}

// IsMigrationApplied reports whether a migration with the given name has
// been recorded in the migrations_applied table.
func (s *SphereStore) IsMigrationApplied(name string) (bool, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM migrations_applied WHERE name = ?`, name,
	).Scan(&count)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("failed to check migration %q applied: %w", name, err)
	}
	return count > 0, nil
}
