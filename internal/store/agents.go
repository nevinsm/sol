package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Agent represents an agent record in the sphere database.
type Agent struct {
	ID        string
	Name      string
	World     string
	Role      string
	State     string
	TetherItem  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CreateAgent creates an agent record in the sphere DB. Returns the agent ID ("world/name").
func (s *Store) CreateAgent(name, world, role string) (string, error) {
	id := world + "/" + name
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`INSERT INTO agents (id, name, world, role, state, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'idle', ?, ?)`,
		id, name, world, role, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create agent %q: %w", id, err)
	}
	return id, nil
}

// GetAgent returns an agent by ID ("world/name").
func (s *Store) GetAgent(id string) (*Agent, error) {
	a := &Agent{}
	var tetherItem sql.NullString
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		`SELECT id, name, world, role, state, tether_item, created_at, updated_at
		 FROM agents WHERE id = ?`, id,
	).Scan(&a.ID, &a.Name, &a.World, &a.Role, &a.State, &tetherItem, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", id, err)
	}

	a.TetherItem = tetherItem.String
	var parseErr error
	a.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt)
	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse created_at for agent %q: %w", id, parseErr)
	}
	a.UpdatedAt, parseErr = time.Parse(time.RFC3339, updatedAt)
	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse updated_at for agent %q: %w", id, parseErr)
	}
	return a, nil
}

// UpdateAgentState updates an agent's state and optionally its tether_item.
// Pass empty tetherItem to clear it, or a work item ID to set it.
func (s *Store) UpdateAgentState(id, state, tetherItem string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var result sql.Result
	var err error

	if tetherItem == "" {
		result, err = s.db.Exec(
			`UPDATE agents SET state = ?, tether_item = NULL, updated_at = ? WHERE id = ?`,
			state, now, id,
		)
	} else {
		result, err = s.db.Exec(
			`UPDATE agents SET state = ?, tether_item = ?, updated_at = ? WHERE id = ?`,
			state, tetherItem, now, id,
		)
	}
	if err != nil {
		return fmt.Errorf("failed to update agent %q state: %w", id, err)
	}
	// RowsAffected error is unlikely with modernc.org/sqlite but check defensively.
	n, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("failed to check rows affected: %w", raErr)
	}
	if n == 0 {
		return fmt.Errorf("agent %q not found", id)
	}
	return nil
}

// ListAgents returns agents, optionally filtered by world and/or state.
// When world is empty, agents across all worlds are returned.
func (s *Store) ListAgents(world string, state string) ([]Agent, error) {
	query := `SELECT id, name, world, role, state, tether_item, created_at, updated_at FROM agents WHERE 1=1`
	var args []interface{}
	if world != "" {
		query += ` AND world = ?`
		args = append(args, world)
	}
	if state != "" {
		query += ` AND state = ?`
		args = append(args, state)
	}
	query += ` ORDER BY name`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents for world %q: %w", world, err)
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		var a Agent
		var tetherItem sql.NullString
		var createdAt, updatedAt string
		if err := rows.Scan(&a.ID, &a.Name, &a.World, &a.Role, &a.State, &tetherItem, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan agent: %w", err)
		}
		a.TetherItem = tetherItem.String
		var parseErr error
		a.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse created_at for agent %q: %w", a.ID, parseErr)
		}
		a.UpdatedAt, parseErr = time.Parse(time.RFC3339, updatedAt)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse updated_at for agent %q: %w", a.ID, parseErr)
		}
		agents = append(agents, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating agents: %w", err)
	}
	return agents, nil
}

// DeleteAgentsForWorld removes all agent records for the given world.
// Used during world deletion to clean up sphere state.
func (s *Store) DeleteAgentsForWorld(world string) error {
	_, err := s.db.Exec(`DELETE FROM agents WHERE world = ?`, world)
	if err != nil {
		return fmt.Errorf("failed to delete agents for world %q: %w", world, err)
	}
	return nil
}

// FindIdleAgent returns the first idle agent for a world, or nil if none available.
func (s *Store) FindIdleAgent(world string) (*Agent, error) {
	a := &Agent{}
	var tetherItem sql.NullString
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		`SELECT id, name, world, role, state, tether_item, created_at, updated_at
		 FROM agents WHERE world = ? AND role = 'agent' AND state = 'idle'
		 ORDER BY name LIMIT 1`, world,
	).Scan(&a.ID, &a.Name, &a.World, &a.Role, &a.State, &tetherItem, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find idle agent for world %q: %w", world, err)
	}

	a.TetherItem = tetherItem.String
	var parseErr error
	a.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt)
	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse created_at for agent %q: %w", a.ID, parseErr)
	}
	a.UpdatedAt, parseErr = time.Parse(time.RFC3339, updatedAt)
	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse updated_at for agent %q: %w", a.ID, parseErr)
	}
	return a, nil
}
