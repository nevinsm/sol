package store

import "fmt"

// AddDependency records that fromID depends on toID.
// Both writs must exist. Returns error on cycle detection.
func (s *WorldStore) AddDependency(fromID, toID string) error {
	if fromID == toID {
		return fmt.Errorf("writ %q cannot depend on itself", fromID)
	}

	// Verify both writs exist.
	var exists int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM writs WHERE id = ?`, fromID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check writ %q: %w", fromID, err)
	}
	if exists == 0 {
		return fmt.Errorf("writ %q not found", fromID)
	}

	err = s.db.QueryRow(`SELECT COUNT(*) FROM writs WHERE id = ?`, toID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check writ %q: %w", toID, err)
	}
	if exists == 0 {
		return fmt.Errorf("writ %q not found", toID)
	}

	// Check for cycles.
	cycle, err := detectCycle(s.GetDependencies, fromID, toID)
	if err != nil {
		return fmt.Errorf("failed to check for cycles: %w", err)
	}
	if cycle {
		return fmt.Errorf("adding dependency %q → %q would create a cycle", fromID, toID)
	}

	_, err = s.db.Exec(
		`INSERT OR IGNORE INTO dependencies (from_id, to_id) VALUES (?, ?)`,
		fromID, toID,
	)
	if err != nil {
		return fmt.Errorf("failed to add dependency %q → %q: %w", fromID, toID, err)
	}
	return nil
}

// RemoveDependency removes a dependency relationship.
func (s *WorldStore) RemoveDependency(fromID, toID string) error {
	_, err := s.db.Exec(
		`DELETE FROM dependencies WHERE from_id = ? AND to_id = ?`,
		fromID, toID,
	)
	if err != nil {
		return fmt.Errorf("failed to remove dependency %q → %q: %w", fromID, toID, err)
	}
	return nil
}

// GetDependencies returns the IDs of writs that itemID depends on.
// (What must complete before this item can start.)
func (s *WorldStore) GetDependencies(itemID string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT to_id FROM dependencies WHERE from_id = ? ORDER BY to_id`,
		itemID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependencies for %q: %w", itemID, err)
	}
	defer rows.Close()

	var deps []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan dependency: %w", err)
		}
		deps = append(deps, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating dependencies for %q: %w", itemID, err)
	}
	return deps, nil
}

// GetDependents returns the IDs of writs that depend on itemID.
// (What is waiting for this item to complete.)
func (s *WorldStore) GetDependents(itemID string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT from_id FROM dependencies WHERE to_id = ? ORDER BY from_id`,
		itemID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependents for %q: %w", itemID, err)
	}
	defer rows.Close()

	var deps []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan dependent: %w", err)
		}
		deps = append(deps, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating dependents for %q: %w", itemID, err)
	}
	return deps, nil
}

// IsReady returns true if all dependencies of itemID are satisfied
// (status is "closed" / merged). An item with no dependencies is
// always ready. Note: "done" (code complete, awaiting merge) is NOT
// sufficient — the prerequisite code must be merged to the target branch.
func (s *WorldStore) IsReady(itemID string) (bool, error) {
	// Count dependencies whose writ is NOT closed (merged).
	var unsatisfied int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM dependencies d
		JOIN writs w ON d.to_id = w.id
		WHERE d.from_id = ? AND w.status != 'closed'`,
		itemID,
	).Scan(&unsatisfied)
	if err != nil {
		return false, fmt.Errorf("failed to check readiness for %q: %w", itemID, err)
	}
	return unsatisfied == 0, nil
}

// HasOpenTransitiveDependents checks whether the given writ has any open
// (non-closed) writs in its transitive dependent graph. This is used by
// writ clean to guard against cleaning output directories that are still
// needed by downstream writs.
func (s *WorldStore) HasOpenTransitiveDependents(writID string) (bool, error) {
	visited := map[string]bool{}
	queue := []string{writID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if visited[current] {
			continue
		}
		visited[current] = true

		dependents, err := s.GetDependents(current)
		if err != nil {
			return false, err
		}
		for _, depID := range dependents {
			// Check if this dependent is open (not closed).
			var status string
			err := s.db.QueryRow(`SELECT status FROM writs WHERE id = ?`, depID).Scan(&status)
			if err != nil {
				return false, fmt.Errorf("failed to check status of writ %q: %w", depID, err)
			}
			if status != "closed" {
				return true, nil
			}
			queue = append(queue, depID)
		}
	}
	return false, nil
}

