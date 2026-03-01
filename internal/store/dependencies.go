package store

import "fmt"

// AddDependency records that fromID depends on toID.
// Both work items must exist. Returns error on cycle detection.
func (s *Store) AddDependency(fromID, toID string) error {
	if fromID == toID {
		return fmt.Errorf("work item %q cannot depend on itself", fromID)
	}

	// Verify both work items exist.
	var exists int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE id = ?`, fromID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check work item %q: %w", fromID, err)
	}
	if exists == 0 {
		return fmt.Errorf("work item %q not found", fromID)
	}

	err = s.db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE id = ?`, toID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check work item %q: %w", toID, err)
	}
	if exists == 0 {
		return fmt.Errorf("work item %q not found", toID)
	}

	// Check for cycles.
	cycle, err := s.wouldCreateCycle(fromID, toID)
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
func (s *Store) RemoveDependency(fromID, toID string) error {
	_, err := s.db.Exec(
		`DELETE FROM dependencies WHERE from_id = ? AND to_id = ?`,
		fromID, toID,
	)
	if err != nil {
		return fmt.Errorf("failed to remove dependency %q → %q: %w", fromID, toID, err)
	}
	return nil
}

// GetDependencies returns the IDs of work items that itemID depends on.
// (What must complete before this item can start.)
func (s *Store) GetDependencies(itemID string) ([]string, error) {
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

// GetDependents returns the IDs of work items that depend on itemID.
// (What is waiting for this item to complete.)
func (s *Store) GetDependents(itemID string) ([]string, error) {
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
// (status is "done" or "closed"). An item with no dependencies is
// always ready.
func (s *Store) IsReady(itemID string) (bool, error) {
	// Count dependencies whose work item is NOT done/closed.
	var unsatisfied int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM dependencies d
		JOIN work_items w ON d.to_id = w.id
		WHERE d.from_id = ? AND w.status NOT IN ('done', 'closed')`,
		itemID,
	).Scan(&unsatisfied)
	if err != nil {
		return false, fmt.Errorf("failed to check readiness for %q: %w", itemID, err)
	}
	return unsatisfied == 0, nil
}

// wouldCreateCycle checks if adding the edge from→to would create a cycle
// by walking the dependency graph from toID to see if fromID is reachable.
//
// Implementation note: this does a BFS with one GetDependencies query per
// node. For large dependency graphs (100+ nodes), consider replacing with
// a recursive CTE:
//
//	WITH RECURSIVE chain(id) AS (
//	    SELECT to_id FROM dependencies WHERE from_id = ?
//	    UNION ALL
//	    SELECT d.to_id FROM dependencies d JOIN chain c ON d.from_id = c.id
//	)
//	SELECT 1 FROM chain WHERE id = ? LIMIT 1
func (s *Store) wouldCreateCycle(fromID, toID string) (bool, error) {
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
		deps, err := s.GetDependencies(current)
		if err != nil {
			return false, err
		}
		queue = append(queue, deps...)
	}
	return false, nil
}
