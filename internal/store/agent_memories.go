package store

import (
	"fmt"
	"time"
)

// AgentMemory represents a persistent key-value memory for an agent.
type AgentMemory struct {
	ID        string
	AgentName string
	Key       string
	Value     string
	CreatedAt time.Time
}

// SetAgentMemory creates or updates a memory for the given agent.
// Uses UPSERT semantics: if the (agent_name, key) pair exists, the value is replaced.
func (s *Store) SetAgentMemory(agentName, key, value string) error {
	id, err := generateMemoryID()
	if err != nil {
		return fmt.Errorf("failed to generate memory ID: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = s.db.Exec(
		`INSERT INTO agent_memories (id, agent_name, key, value, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(agent_name, key) DO UPDATE SET value = excluded.value`,
		id, agentName, key, value, now,
	)
	if err != nil {
		return fmt.Errorf("failed to set agent memory %q/%q: %w", agentName, key, err)
	}
	return nil
}

// ListAgentMemories returns all memories for the given agent, ordered by creation time.
func (s *Store) ListAgentMemories(agentName string) ([]AgentMemory, error) {
	rows, err := s.db.Query(
		`SELECT id, agent_name, key, value, created_at
		 FROM agent_memories
		 WHERE agent_name = ?
		 ORDER BY created_at ASC`,
		agentName,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list agent memories for %q: %w", agentName, err)
	}
	defer rows.Close()

	var memories []AgentMemory
	for rows.Next() {
		var m AgentMemory
		var createdAt string
		if err := rows.Scan(&m.ID, &m.AgentName, &m.Key, &m.Value, &createdAt); err != nil {
			return nil, fmt.Errorf("failed to scan agent memory: %w", err)
		}
		var parseErr error
		if m.CreatedAt, parseErr = parseRFC3339(createdAt, "created_at", "memory "+m.Key); parseErr != nil {
			return nil, parseErr
		}
		memories = append(memories, m)
	}
	return memories, rows.Err()
}

// DeleteAgentMemory deletes a single memory by agent name and key.
func (s *Store) DeleteAgentMemory(agentName, key string) error {
	result, err := s.db.Exec(
		`DELETE FROM agent_memories WHERE agent_name = ? AND key = ?`,
		agentName, key,
	)
	if err != nil {
		return fmt.Errorf("failed to delete agent memory %q/%q: %w", agentName, key, err)
	}
	return checkRowsAffected(result, "memory "+key+" for agent", agentName)
}

// CountAgentMemories returns the number of memories for the given agent.
func (s *Store) CountAgentMemories(agentName string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM agent_memories WHERE agent_name = ?`,
		agentName,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count agent memories for %q: %w", agentName, err)
	}
	return count, nil
}

// DeleteAllAgentMemories deletes all memories for the given agent.
func (s *Store) DeleteAllAgentMemories(agentName string) (int64, error) {
	result, err := s.db.Exec(
		`DELETE FROM agent_memories WHERE agent_name = ?`,
		agentName,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to delete all memories for agent %q: %w", agentName, err)
	}
	return result.RowsAffected()
}

func generateMemoryID() (string, error) {
	return generatePrefixedID("mem-")
}
