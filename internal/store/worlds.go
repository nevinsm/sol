package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// World represents a registered world in the sphere database.
type World struct {
	Name       string    `json:"name"`
	SourceRepo string    `json:"source_repo"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
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

// GetWorld returns a world by name. Returns an error if not found.
func (s *Store) GetWorld(name string) (*World, error) {
	w := &World{}
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		`SELECT name, source_repo, created_at, updated_at
		 FROM worlds WHERE name = ?`, name,
	).Scan(&w.Name, &w.SourceRepo, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("world %q: %w", name, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get world %q: %w", name, err)
	}

	var parseErr error
	if w.CreatedAt, parseErr = parseRFC3339(createdAt, "created_at", "world "+name); parseErr != nil {
		return nil, parseErr
	}
	if w.UpdatedAt, parseErr = parseRFC3339(updatedAt, "updated_at", "world "+name); parseErr != nil {
		return nil, parseErr
	}
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
		var parseErr error
		if w.CreatedAt, parseErr = parseRFC3339(createdAt, "created_at", "world "+w.Name); parseErr != nil {
			return nil, parseErr
		}
		if w.UpdatedAt, parseErr = parseRFC3339(updatedAt, "updated_at", "world "+w.Name); parseErr != nil {
			return nil, parseErr
		}
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
	return checkRowsAffected(result, "world", name)
}

// DeleteWorldData removes all sphere-level data for a world in a single
// transaction: messages, escalations, caravan items, agents, and the world record.
func (s *Store) DeleteWorldData(world string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Clean up messages where sender or recipient is an agent in this world.
	// Agent IDs are formatted as "{world}/{name}".
	// Use exact prefix matching (not LIKE) to avoid matching worlds with
	// similar names (e.g. deleting "dev" must not affect "dev-staging").
	worldPrefix := world + "/"
	if _, err := tx.Exec(
		`DELETE FROM messages WHERE
			(length(sender) > ? AND substr(sender, 1, ?) = ?)
			OR (length(recipient) > ? AND substr(recipient, 1, ?) = ?)`,
		len(worldPrefix), len(worldPrefix), worldPrefix,
		len(worldPrefix), len(worldPrefix), worldPrefix,
	); err != nil {
		return fmt.Errorf("failed to delete messages for world %q: %w", world, err)
	}

	// Clean up escalations sourced from this world.
	if _, err := tx.Exec(
		`DELETE FROM escalations WHERE length(source) > ? AND substr(source, 1, ?) = ?`,
		len(worldPrefix), len(worldPrefix), worldPrefix,
	); err != nil {
		return fmt.Errorf("failed to delete escalations for world %q: %w", world, err)
	}

	if _, err := tx.Exec(`DELETE FROM caravan_items WHERE world = ?`, world); err != nil {
		return fmt.Errorf("failed to delete caravan items for world %q: %w", world, err)
	}
	if _, err := tx.Exec(`DELETE FROM agents WHERE world = ?`, world); err != nil {
		return fmt.Errorf("failed to delete agents for world %q: %w", world, err)
	}
	if _, err := tx.Exec(`DELETE FROM worlds WHERE name = ?`, world); err != nil {
		return fmt.Errorf("failed to remove world %q: %w", world, err)
	}

	return tx.Commit()
}
