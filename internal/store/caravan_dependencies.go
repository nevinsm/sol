package store

import "fmt"

// AddCaravanDependency records that fromID depends on toID (fromID is blocked
// until toID is closed). Both caravans must exist. Returns error on cycle detection.
func (s *Store) AddCaravanDependency(fromID, toID string) error {
	if fromID == toID {
		return fmt.Errorf("caravan %q cannot depend on itself", fromID)
	}

	// Verify both caravans exist.
	if _, err := s.GetCaravan(fromID); err != nil {
		return fmt.Errorf("caravan %q not found", fromID)
	}
	if _, err := s.GetCaravan(toID); err != nil {
		return fmt.Errorf("caravan %q not found", toID)
	}

	// Check for cycles.
	cycle, err := s.wouldCreateCaravanCycle(fromID, toID)
	if err != nil {
		return fmt.Errorf("failed to check for cycles: %w", err)
	}
	if cycle {
		return fmt.Errorf("adding dependency %q → %q would create a cycle", fromID, toID)
	}

	_, err = s.db.Exec(
		`INSERT OR IGNORE INTO caravan_dependencies (from_id, to_id) VALUES (?, ?)`,
		fromID, toID,
	)
	if err != nil {
		return fmt.Errorf("failed to add caravan dependency %q → %q: %w", fromID, toID, err)
	}
	return nil
}

// RemoveCaravanDependency removes a caravan dependency relationship.
func (s *Store) RemoveCaravanDependency(fromID, toID string) error {
	_, err := s.db.Exec(
		`DELETE FROM caravan_dependencies WHERE from_id = ? AND to_id = ?`,
		fromID, toID,
	)
	if err != nil {
		return fmt.Errorf("failed to remove caravan dependency %q → %q: %w", fromID, toID, err)
	}
	return nil
}

// GetCaravanDependencies returns the IDs of caravans that caravanID depends on.
// (What must be closed before this caravan's items can proceed.)
func (s *Store) GetCaravanDependencies(caravanID string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT to_id FROM caravan_dependencies WHERE from_id = ? ORDER BY to_id`,
		caravanID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get caravan dependencies for %q: %w", caravanID, err)
	}
	defer rows.Close()

	var deps []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan caravan dependency: %w", err)
		}
		deps = append(deps, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating caravan dependencies for %q: %w", caravanID, err)
	}
	return deps, nil
}

// GetCaravanDependents returns the IDs of caravans that depend on caravanID.
// (What is waiting for this caravan to close.)
func (s *Store) GetCaravanDependents(caravanID string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT from_id FROM caravan_dependencies WHERE to_id = ? ORDER BY from_id`,
		caravanID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get caravan dependents for %q: %w", caravanID, err)
	}
	defer rows.Close()

	var deps []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan caravan dependent: %w", err)
		}
		deps = append(deps, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating caravan dependents for %q: %w", caravanID, err)
	}
	return deps, nil
}

// AreCaravanDependenciesSatisfied returns true if all caravans that caravanID
// depends on are closed. A caravan with no dependencies is always satisfied.
func (s *Store) AreCaravanDependenciesSatisfied(caravanID string) (bool, error) {
	var unsatisfied int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM caravan_dependencies cd
		JOIN caravans c ON cd.to_id = c.id
		WHERE cd.from_id = ? AND c.status != 'closed'`,
		caravanID,
	).Scan(&unsatisfied)
	if err != nil {
		return false, fmt.Errorf("failed to check caravan dependencies for %q: %w", caravanID, err)
	}
	return unsatisfied == 0, nil
}

// UnsatisfiedCaravanDependencies returns the IDs of caravans that caravanID
// depends on that are not yet closed.
func (s *Store) UnsatisfiedCaravanDependencies(caravanID string) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT cd.to_id FROM caravan_dependencies cd
		JOIN caravans c ON cd.to_id = c.id
		WHERE cd.from_id = ? AND c.status != 'closed'
		ORDER BY cd.to_id`,
		caravanID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get unsatisfied caravan deps for %q: %w", caravanID, err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan unsatisfied caravan dep: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// IsWritBlockedByCaravanDeps checks whether a writ belongs to any
// caravan that has unsatisfied caravan-level dependencies. Returns true if
// blocked, along with the blocking caravan IDs.
func (s *Store) IsWritBlockedByCaravanDeps(writID string) (bool, []string, error) {
	// Find all caravans containing this writ.
	rows, err := s.db.Query(
		`SELECT DISTINCT caravan_id FROM caravan_items WHERE writ_id = ?`,
		writID,
	)
	if err != nil {
		return false, nil, fmt.Errorf("failed to find caravans for writ %q: %w", writID, err)
	}
	defer rows.Close()

	var caravanIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return false, nil, fmt.Errorf("failed to scan caravan ID: %w", err)
		}
		caravanIDs = append(caravanIDs, id)
	}
	if err := rows.Err(); err != nil {
		return false, nil, err
	}

	// Check each caravan for unsatisfied dependencies.
	var blockers []string
	for _, carID := range caravanIDs {
		satisfied, err := s.AreCaravanDependenciesSatisfied(carID)
		if err != nil {
			return false, nil, err
		}
		if !satisfied {
			unsatisfied, err := s.UnsatisfiedCaravanDependencies(carID)
			if err != nil {
				return false, nil, err
			}
			blockers = append(blockers, unsatisfied...)
		}
	}
	return len(blockers) > 0, blockers, nil
}

// DeleteCaravanDependencies removes all dependencies where the given caravan
// is either the dependent or the prerequisite.
func (s *Store) DeleteCaravanDependencies(caravanID string) error {
	_, err := s.db.Exec(
		`DELETE FROM caravan_dependencies WHERE from_id = ? OR to_id = ?`,
		caravanID, caravanID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete caravan dependencies for %q: %w", caravanID, err)
	}
	return nil
}

// wouldCreateCaravanCycle checks if adding the edge from→to would create a
// cycle by walking the dependency graph from toID to see if fromID is reachable.
func (s *Store) wouldCreateCaravanCycle(fromID, toID string) (bool, error) {
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
		deps, err := s.GetCaravanDependencies(current)
		if err != nil {
			return false, err
		}
		queue = append(queue, deps...)
	}
	return false, nil
}
