package store

import (
	"database/sql"
	"fmt"
	"time"
)

// World represents a registered world in the sphere database.
type World struct {
	Name       string
	SourceRepo string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// RegisterWorld creates a world record in the sphere DB.
// Uses INSERT OR IGNORE — idempotent, safe for re-init of existing worlds.
// If the world already exists, this is a no-op (does not update fields).
func (s *Store) RegisterWorld(name, sourceRepo string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO worlds (name, source_repo, created_at, updated_at)
		 VALUES (?, ?, ?, ?)`,
		name, sourceRepo, now, now,
	)
	if err != nil {
		return fmt.Errorf("failed to register world %q: %w", name, err)
	}
	return nil
}

// GetWorld returns a world by name. Returns nil, nil if not found.
func (s *Store) GetWorld(name string) (*World, error) {
	w := &World{}
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		`SELECT name, source_repo, created_at, updated_at
		 FROM worlds WHERE name = ?`, name,
	).Scan(&w.Name, &w.SourceRepo, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get world %q: %w", name, err)
	}

	w.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	w.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return w, nil
}

// ListWorlds returns all registered worlds, ordered by name.
func (s *Store) ListWorlds() ([]World, error) {
	rows, err := s.db.Query(
		`SELECT name, source_repo, created_at, updated_at
		 FROM worlds ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list worlds: %w", err)
	}
	defer rows.Close()

	var worlds []World
	for rows.Next() {
		var w World
		var createdAt, updatedAt string
		if err := rows.Scan(&w.Name, &w.SourceRepo, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan world: %w", err)
		}
		w.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		w.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		worlds = append(worlds, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating worlds: %w", err)
	}
	return worlds, nil
}

// UpdateWorldRepo updates the source_repo for a world.
// Also updates updated_at. Returns an error if the world does not exist.
func (s *Store) UpdateWorldRepo(name, sourceRepo string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(
		`UPDATE worlds SET source_repo = ?, updated_at = ? WHERE name = ?`,
		sourceRepo, now, name,
	)
	if err != nil {
		return fmt.Errorf("failed to update world %q repo: %w", name, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check update result for world %q: %w", name, err)
	}
	if n == 0 {
		return fmt.Errorf("world %q not found", name)
	}
	return nil
}

// RemoveWorld deletes a world record from the sphere DB.
// Does NOT delete the world database file or directory — that's the
// CLI's responsibility.
func (s *Store) RemoveWorld(name string) error {
	_, err := s.db.Exec(`DELETE FROM worlds WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("failed to remove world %q: %w", name, err)
	}
	return nil
}
