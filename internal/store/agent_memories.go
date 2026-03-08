package store

import (
	"crypto/rand"
	"encoding/hex"
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
		 ON CONFLICT(agent_name, key) DO UPDATE SET value = excluded.value, created_at = excluded.created_at`,
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
		m.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse created_at for memory %q: %w", m.Key, parseErr)
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
	n, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("failed to check rows affected: %w", raErr)
	}
	if n == 0 {
		return fmt.Errorf("memory %q for agent %q: %w", key, agentName, ErrNotFound)
	}
	return nil
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
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "mem-" + hex.EncodeToString(b), nil
}
