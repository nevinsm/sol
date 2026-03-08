package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// parseRFC3339 parses an RFC3339 timestamp string, returning a descriptive
// error that includes the field name and entity ID for debugging.
func parseRFC3339(s, fieldName, entityID string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse %s for %s: %w", fieldName, entityID, err)
	}
	return t, nil
}

// parseOptionalRFC3339 parses a nullable RFC3339 timestamp. Returns nil if
// the sql.NullString is not valid, otherwise parses the value.
func parseOptionalRFC3339(ns sql.NullString, fieldName, entityID string) (*time.Time, error) {
	if !ns.Valid {
		return nil, nil
	}
	t, err := parseRFC3339(ns.String, fieldName, entityID)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// checkRowsAffected checks that a SQL result affected at least one row.
// Returns ErrNotFound (wrapped with entity context) if zero rows were affected.
func checkRowsAffected(result sql.Result, entityType, entityID string) error {
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%s %q: %w", entityType, entityID, ErrNotFound)
	}
	return nil
}

// generatePrefixedID generates a random ID with the given prefix.
// Format: prefix + 16 hex chars (8 random bytes).
func generatePrefixedID(prefix string) (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate %sID: %w", prefix, err)
	}
	return prefix + hex.EncodeToString(b), nil
}

// detectCycle checks if adding an edge from→to would create a cycle in a
// directed graph. It performs a BFS from toID, using getNeighbors to fetch
// outgoing edges, and returns true if fromID is reachable from toID.
func detectCycle(getNeighbors func(string) ([]string, error), fromID, toID string) (bool, error) {
	visited := map[string]bool{}
	queue := []string{toID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == fromID {
			return true, nil
		}
		if visited[current] {
			continue
		}
		visited[current] = true
		deps, err := getNeighbors(current)
		if err != nil {
			return false, err
		}
		queue = append(queue, deps...)
	}
	return false, nil
}
