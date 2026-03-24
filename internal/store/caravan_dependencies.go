package store

import (
	"context"
	"errors"
	"fmt"
)

// AddCaravanDependency records that fromID depends on toID (fromID is blocked
// until toID is closed). Both caravans must exist. Returns error on cycle detection.
// Uses an IMMEDIATE transaction to prevent concurrent cycle creation.
func (s *SphereStore) AddCaravanDependency(fromID, toID string) error {
	if fromID == toID {
		return fmt.Errorf("caravan %q cannot depend on itself", fromID)
	}

	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close()

	// BEGIN IMMEDIATE acquires the write lock upfront, preventing concurrent
	// writers from passing cycle detection before our INSERT is visible.
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			conn.ExecContext(ctx, "ROLLBACK") //nolint:errcheck
		}
	}()

	// Verify both caravans exist.
	var exists int
	err = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM caravans WHERE id = ?`, fromID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check caravan %q: %w", fromID, err)
	}
	if exists == 0 {
		return fmt.Errorf("caravan %q not found", fromID)
	}

	err = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM caravans WHERE id = ?`, toID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check caravan %q: %w", toID, err)
	}
	if exists == 0 {
		return fmt.Errorf("caravan %q not found", toID)
	}

	// Check for cycles using transaction-scoped queries.
	getDepsInTx := func(caravanID string) ([]string, error) {
		rows, err := conn.QueryContext(ctx,
			`SELECT to_id FROM caravan_dependencies WHERE from_id = ? ORDER BY to_id`, caravanID)
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
		return deps, rows.Err()
	}

	cycle, err := detectCycle(getDepsInTx, fromID, toID)
	if err != nil {
		return fmt.Errorf("failed to check for cycles: %w", err)
	}
	if cycle {
		return fmt.Errorf("adding dependency %q → %q would create a cycle", fromID, toID)
	}

	_, err = conn.ExecContext(ctx,
		`INSERT OR IGNORE INTO caravan_dependencies (from_id, to_id) VALUES (?, ?)`,
		fromID, toID,
	)
	if err != nil {
		return fmt.Errorf("failed to add caravan dependency %q → %q: %w", fromID, toID, err)
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("failed to commit caravan dependency: %w", err)
	}
	committed = true
	return nil
}

// RemoveCaravanDependency removes a caravan dependency relationship.
func (s *SphereStore) RemoveCaravanDependency(fromID, toID string) error {
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
func (s *SphereStore) GetCaravanDependencies(caravanID string) ([]string, error) {
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
func (s *SphereStore) GetCaravanDependents(caravanID string) ([]string, error) {
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
func (s *SphereStore) AreCaravanDependenciesSatisfied(caravanID string) (bool, error) {
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
func (s *SphereStore) UnsatisfiedCaravanDependencies(caravanID string) ([]string, error) {
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating unsatisfied caravan deps for %q: %w", caravanID, err)
	}
	return ids, nil
}

// IsWritBlockedByCaravanDeps checks whether a writ belongs to any
// caravan that has unsatisfied caravan-level dependencies. Returns true if
// blocked, along with the blocking caravan IDs.
func (s *SphereStore) IsWritBlockedByCaravanDeps(writID string) (bool, []string, error) {
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
func (s *SphereStore) DeleteCaravanDependencies(caravanID string) error {
	_, err := s.db.Exec(
		`DELETE FROM caravan_dependencies WHERE from_id = ? OR to_id = ?`,
		caravanID, caravanID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete caravan dependencies for %q: %w", caravanID, err)
	}
	return nil
}

// IsWritBlockedByCaravan checks whether a writ is blocked by any caravan
// constraint. This includes caravan-level dependency blocking (the writ's
// caravan depends on another caravan that isn't closed) AND phase gating
// (the writ is in a phase > 0 and items in lower phases aren't all closed).
//
// The worldOpener function opens a world store by name — the caller provides
// this so the caravan checker doesn't need to know about store paths.
func (s *SphereStore) IsWritBlockedByCaravan(writID, world string,
	worldOpener func(world string) (*WorldStore, error)) (bool, error) {

	// Check caravan-level dependency blocking.
	blocked, _, err := s.IsWritBlockedByCaravanDeps(writID)
	if err != nil {
		return false, err
	}
	if blocked {
		return true, nil
	}

	// Check phase gating: find all caravans containing this writ.
	rows, err := s.db.Query(
		`SELECT caravan_id, phase FROM caravan_items WHERE writ_id = ?`,
		writID,
	)
	if err != nil {
		return false, fmt.Errorf("failed to find caravans for writ %q: %w", writID, err)
	}
	defer rows.Close()

	type caravanPhase struct {
		caravanID string
		phase     int
	}
	var memberships []caravanPhase
	for rows.Next() {
		var cp caravanPhase
		if err := rows.Scan(&cp.caravanID, &cp.phase); err != nil {
			return false, fmt.Errorf("failed to scan caravan membership: %w", err)
		}
		memberships = append(memberships, cp)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}

	// For each caravan where the writ is in phase > 0, check if all items
	// in lower phases are closed.
	for _, cp := range memberships {
		if cp.phase == 0 {
			continue // Phase 0 uses only within-world dependency check.
		}

		// Get all items in lower phases of this caravan.
		lowerRows, err := s.db.Query(
			`SELECT writ_id, world FROM caravan_items WHERE caravan_id = ? AND phase < ?`,
			cp.caravanID, cp.phase,
		)
		if err != nil {
			return false, fmt.Errorf("failed to query lower phase items: %w", err)
		}

		type lowerItem struct {
			writID string
			world  string
		}
		var lowerItems []lowerItem
		for lowerRows.Next() {
			var li lowerItem
			if err := lowerRows.Scan(&li.writID, &li.world); err != nil {
				lowerRows.Close()
				return false, fmt.Errorf("failed to scan lower phase item: %w", err)
			}
			lowerItems = append(lowerItems, li)
		}
		if err := lowerRows.Err(); err != nil {
			lowerRows.Close()
			return false, fmt.Errorf("failed iterating lower phase items: %w", err)
		}
		lowerRows.Close()

		// Check if any lower-phase item is not closed.
		// Group by world for efficiency.
		byWorld := map[string][]string{}
		for _, li := range lowerItems {
			byWorld[li.world] = append(byWorld[li.world], li.writID)
		}

		for w, writIDs := range byWorld {
			ws, err := worldOpener(w)
			if err != nil {
				return false, fmt.Errorf("failed to open world %q: %w", w, err)
			}

			blocked := false
			for _, wid := range writIDs {
				item, err := ws.GetWrit(wid)
				if err != nil {
					if errors.Is(err, ErrNotFound) {
						// Writ not found — treat as blocking (conservative).
						ws.Close()
						return true, nil
					}
					// Actual database error — propagate.
					ws.Close()
					return false, fmt.Errorf("failed to get writ %q in world %q: %w", wid, w, err)
				}
				if item.Status != "closed" {
					blocked = true
					break
				}
			}
			ws.Close()
			if blocked {
				return true, nil
			}
		}
	}

	return false, nil
}

