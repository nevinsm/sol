package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/nevinsm/sol/internal/config"
)

// Agent represents an agent record in the sphere database.
type Agent struct {
	ID         string
	Name       string
	World      string
	Role       string
	State      string
	ActiveWrit string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// CreateAgent creates an agent record in the sphere DB. Returns the agent ID ("world/name").
func (s *SphereStore) CreateAgent(name, world, role string) (string, error) {
	if err := config.ValidateAgentName(name); err != nil {
		return "", fmt.Errorf("invalid agent: %w", err)
	}
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

// EnsureAgent creates an agent if it doesn't already exist.
// Returns nil if the agent already exists or was successfully created.
func (s *SphereStore) EnsureAgent(name, world, role string) error {
	id := world + "/" + name
	agent, err := s.GetAgent(id)
	if err == nil && agent != nil {
		return nil // already registered
	}
	if err != nil {
		// GetAgent failed — log context but try CreateAgent anyway.
		// CreateAgent will fail cleanly on unique constraint if agent exists.
		fmt.Fprintf(os.Stderr, "store: GetAgent %q failed, attempting create: %v\n", id, err)
	}
	getErr := err
	_, createErr := s.CreateAgent(name, world, role)
	if createErr != nil {
		if getErr != nil {
			return fmt.Errorf("failed to ensure agent %q: get: %w; create: %v", id, getErr, createErr)
		}
		return fmt.Errorf("failed to ensure agent %q: %w", id, createErr)
	}
	// If GetAgent failed with a real error (not just "not found"), surface it
	// even though CreateAgent succeeded — this may indicate database corruption.
	if getErr != nil && !errors.Is(getErr, ErrNotFound) {
		return fmt.Errorf("failed to ensure agent %q: GetAgent failed (create succeeded): %w", id, getErr)
	}
	return nil
}

// GetAgent returns an agent by ID ("world/name").
func (s *SphereStore) GetAgent(id string) (*Agent, error) {
	a := &Agent{}
	var activeWrit sql.NullString
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		`SELECT id, name, world, role, state, active_writ, created_at, updated_at
		 FROM agents WHERE id = ?`, id,
	).Scan(&a.ID, &a.Name, &a.World, &a.Role, &a.State, &activeWrit, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("agent %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", id, err)
	}

	a.ActiveWrit = activeWrit.String
	var parseErr error
	if a.CreatedAt, parseErr = parseRFC3339(createdAt, "created_at", "agent "+id); parseErr != nil {
		return nil, parseErr
	}
	if a.UpdatedAt, parseErr = parseRFC3339(updatedAt, "updated_at", "agent "+id); parseErr != nil {
		return nil, parseErr
	}
	return a, nil
}

var validAgentStates = map[string]bool{
	"idle":    true,
	"working": true,
	"stalled": true,
}

// UpdateAgentState updates an agent's state and optionally its active_writ.
// Pass empty activeWrit to clear it, or a writ ID to set it.
func (s *SphereStore) UpdateAgentState(id, state, activeWrit string) error {
	if !validAgentStates[state] {
		return fmt.Errorf("invalid agent state %q", state)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var result sql.Result
	var err error

	if activeWrit == "" {
		result, err = s.db.Exec(
			`UPDATE agents SET state = ?, active_writ = NULL, updated_at = ? WHERE id = ?`,
			state, now, id,
		)
	} else {
		result, err = s.db.Exec(
			`UPDATE agents SET state = ?, active_writ = ?, updated_at = ? WHERE id = ?`,
			state, activeWrit, now, id,
		)
	}
	if err != nil {
		return fmt.Errorf("failed to update agent %q state: %w", id, err)
	}
	return checkRowsAffected(result, "agent", id)
}

// CompareAndSetAgentState updates an agent's state and active_writ only if the
// agent's current state matches expectedState. Returns (false, nil) if the agent
// exists but its state has already changed (no row updated). Returns (true, nil)
// on successful update.
func (s *SphereStore) CompareAndSetAgentState(id, expectedState, newState, activeWrit string) (bool, error) {
	if !validAgentStates[newState] {
		return false, fmt.Errorf("invalid agent state %q", newState)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var result sql.Result
	var err error

	if activeWrit == "" {
		result, err = s.db.Exec(
			`UPDATE agents SET state = ?, active_writ = NULL, updated_at = ? WHERE id = ? AND state = ?`,
			newState, now, id, expectedState,
		)
	} else {
		result, err = s.db.Exec(
			`UPDATE agents SET state = ?, active_writ = ?, updated_at = ? WHERE id = ? AND state = ?`,
			newState, activeWrit, now, id, expectedState,
		)
	}
	if err != nil {
		return false, fmt.Errorf("failed to compare-and-set agent %q state: %w", id, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to get rows affected for agent %q: %w", id, err)
	}
	return n > 0, nil
}

// ListAgents returns agents, optionally filtered by world and/or state.
// When world is empty, agents across all worlds are returned.
func (s *SphereStore) ListAgents(world string, state string) ([]Agent, error) {
	query := `SELECT id, name, world, role, state, active_writ, created_at, updated_at FROM agents WHERE 1=1`
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
		var activeWrit sql.NullString
		var createdAt, updatedAt string
		if err := rows.Scan(&a.ID, &a.Name, &a.World, &a.Role, &a.State, &activeWrit, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan agent: %w", err)
		}
		a.ActiveWrit = activeWrit.String
		var parseErr error
		if a.CreatedAt, parseErr = parseRFC3339(createdAt, "created_at", "agent "+a.ID); parseErr != nil {
			return nil, parseErr
		}
		if a.UpdatedAt, parseErr = parseRFC3339(updatedAt, "updated_at", "agent "+a.ID); parseErr != nil {
			return nil, parseErr
		}
		agents = append(agents, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating agents: %w", err)
	}
	return agents, nil
}

// DeleteAgent removes a single agent record by ID ("world/name").
// Returns ErrNotFound if the agent does not exist.
func (s *SphereStore) DeleteAgent(id string) error {
	result, err := s.db.Exec(`DELETE FROM agents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete agent %q: %w", id, err)
	}
	return checkRowsAffected(result, "agent", id)
}

// DeleteAgentsForWorld removes all agent records for the given world.
// Used during world deletion to clean up sphere state.
func (s *SphereStore) DeleteAgentsForWorld(world string) error {
	_, err := s.db.Exec(`DELETE FROM agents WHERE world = ?`, world)
	if err != nil {
		return fmt.Errorf("failed to delete agents for world %q: %w", world, err)
	}
	return nil
}

// FindIdleAgent returns the first idle agent for a world, or nil if none available.
func (s *SphereStore) FindIdleAgent(world string) (*Agent, error) {
	a := &Agent{}
	var activeWrit sql.NullString
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		`SELECT id, name, world, role, state, active_writ, created_at, updated_at
		 FROM agents WHERE world = ? AND role = 'outpost' AND state = 'idle'
		 ORDER BY name LIMIT 1`, world,
	).Scan(&a.ID, &a.Name, &a.World, &a.Role, &a.State, &activeWrit, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find idle agent for world %q: %w", world, err)
	}

	a.ActiveWrit = activeWrit.String
	var parseErr error
	if a.CreatedAt, parseErr = parseRFC3339(createdAt, "created_at", "agent "+a.ID); parseErr != nil {
		return nil, parseErr
	}
	if a.UpdatedAt, parseErr = parseRFC3339(updatedAt, "updated_at", "agent "+a.ID); parseErr != nil {
		return nil, parseErr
	}
	return a, nil
}
