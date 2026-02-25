package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Agent represents an agent record in the town database.
type Agent struct {
	ID        string
	Name      string
	Rig       string
	Role      string
	State     string
	HookItem  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CreateAgent creates an agent record in the town DB. Returns the agent ID ("rig/name").
func (s *Store) CreateAgent(name, rig, role string) (string, error) {
	id := rig + "/" + name
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`INSERT INTO agents (id, name, rig, role, state, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'idle', ?, ?)`,
		id, name, rig, role, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create agent %q: %w", id, err)
	}
	return id, nil
}

// GetAgent returns an agent by ID ("rig/name").
func (s *Store) GetAgent(id string) (*Agent, error) {
	a := &Agent{}
	var hookItem sql.NullString
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		`SELECT id, name, rig, role, state, hook_item, created_at, updated_at
		 FROM agents WHERE id = ?`, id,
	).Scan(&a.ID, &a.Name, &a.Rig, &a.Role, &a.State, &hookItem, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", id, err)
	}

	a.HookItem = hookItem.String
	a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	a.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return a, nil
}

// UpdateAgentState updates an agent's state and optionally its hook_item.
// Pass empty hookItem to clear it, or a work item ID to set it.
func (s *Store) UpdateAgentState(id, state, hookItem string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var result sql.Result
	var err error

	if hookItem == "" {
		result, err = s.db.Exec(
			`UPDATE agents SET state = ?, hook_item = NULL, updated_at = ? WHERE id = ?`,
			state, now, id,
		)
	} else {
		result, err = s.db.Exec(
			`UPDATE agents SET state = ?, hook_item = ?, updated_at = ? WHERE id = ?`,
			state, hookItem, now, id,
		)
	}
	if err != nil {
		return fmt.Errorf("failed to update agent %q state: %w", id, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent %q not found", id)
	}
	return nil
}

// ListAgents returns agents for a rig, optionally filtered by state.
func (s *Store) ListAgents(rig string, state string) ([]Agent, error) {
	query := `SELECT id, name, rig, role, state, hook_item, created_at, updated_at FROM agents WHERE rig = ?`
	args := []interface{}{rig}
	if state != "" {
		query += ` AND state = ?`
		args = append(args, state)
	}
	query += ` ORDER BY name`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents for rig %q: %w", rig, err)
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		var a Agent
		var hookItem sql.NullString
		var createdAt, updatedAt string
		if err := rows.Scan(&a.ID, &a.Name, &a.Rig, &a.Role, &a.State, &hookItem, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan agent: %w", err)
		}
		a.HookItem = hookItem.String
		a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		a.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		agents = append(agents, a)
	}
	return agents, nil
}

// FindIdleAgent returns the first idle polecat for a rig, or nil if none available.
func (s *Store) FindIdleAgent(rig string) (*Agent, error) {
	a := &Agent{}
	var hookItem sql.NullString
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		`SELECT id, name, rig, role, state, hook_item, created_at, updated_at
		 FROM agents WHERE rig = ? AND role = 'polecat' AND state = 'idle'
		 ORDER BY name LIMIT 1`, rig,
	).Scan(&a.ID, &a.Name, &a.Rig, &a.Role, &a.State, &hookItem, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find idle agent for rig %q: %w", rig, err)
	}

	a.HookItem = hookItem.String
	a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	a.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return a, nil
}
