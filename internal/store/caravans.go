package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Caravan represents a group of related writs tracked together.
type Caravan struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Status    CaravanStatus `json:"status"` // "drydock", "open", "ready", "closed"
	Owner     string        `json:"owner"`
	CreatedAt time.Time     `json:"created_at"`
	ClosedAt  *time.Time    `json:"closed_at,omitempty"`
}

// CaravanItem is a writ associated with a caravan.
type CaravanItem struct {
	CaravanID  string `json:"caravan_id"`
	WritID string `json:"writ_id"`
	World      string `json:"world"`
	Phase      int    `json:"phase"`
}

// CaravanItemStatus represents the status of a writ within a caravan.
type CaravanItemStatus struct {
	WritID     string     `json:"writ_id"`
	World      string     `json:"world"`
	Phase      int        `json:"phase"`
	WritStatus WritStatus `json:"writ_status"`
	Ready      bool       `json:"ready"`
	Assignee   string     `json:"assignee,omitempty"`
}

// IsDispatched returns true if the item is actively being worked on by an agent.
func (s CaravanItemStatus) IsDispatched() bool {
	return s.WritStatus == WritTethered || s.WritStatus == WritWorking
}

// generateCaravanID returns a new caravan ID in the format "car-" + 16 hex chars.
func generateCaravanID() (string, error) {
	return generatePrefixedID("car-")
}

// CreateCaravan creates a caravan with the given name and owner.
// Returns the caravan ID.
func (s *Store) CreateCaravan(name, owner string) (string, error) {
	id, err := generateCaravanID()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = s.db.Exec(
		`INSERT INTO caravans (id, name, status, owner, created_at) VALUES (?, ?, 'drydock', ?, ?)`,
		id, name, owner, now,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create caravan %q: %w", name, err)
	}
	return id, nil
}

// GetCaravan returns a caravan by ID.
func (s *Store) GetCaravan(id string) (*Caravan, error) {
	c := &Caravan{}
	var owner sql.NullString
	var closedAt sql.NullString
	var createdAt string

	err := s.db.QueryRow(
		`SELECT id, name, status, owner, created_at, closed_at FROM caravans WHERE id = ?`, id,
	).Scan(&c.ID, &c.Name, &c.Status, &owner, &createdAt, &closedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("caravan %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get caravan %q: %w", id, err)
	}

	c.Owner = owner.String
	if c.CreatedAt, err = parseRFC3339(createdAt, "created_at", "caravan "+id); err != nil {
		return nil, err
	}
	if c.ClosedAt, err = parseOptionalRFC3339(closedAt, "closed_at", "caravan "+id); err != nil {
		return nil, err
	}
	return c, nil
}

// ListCaravans returns caravans, optionally filtered by status.
// If status is empty, returns all caravans.
// Ordered by created_at DESC (newest first).
func (s *Store) ListCaravans(status CaravanStatus) ([]Caravan, error) {
	query := `SELECT id, name, status, owner, created_at, closed_at FROM caravans`
	var args []interface{}

	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list caravans: %w", err)
	}
	defer rows.Close()

	var caravans []Caravan
	for rows.Next() {
		var c Caravan
		var owner sql.NullString
		var closedAt sql.NullString
		var createdAt string

		if err := rows.Scan(&c.ID, &c.Name, &c.Status, &owner, &createdAt, &closedAt); err != nil {
			return nil, fmt.Errorf("failed to scan caravan: %w", err)
		}
		c.Owner = owner.String
		var parseErr error
		if c.CreatedAt, parseErr = parseRFC3339(createdAt, "created_at", "caravan "+c.ID); parseErr != nil {
			return nil, parseErr
		}
		if c.ClosedAt, parseErr = parseOptionalRFC3339(closedAt, "closed_at", "caravan "+c.ID); parseErr != nil {
			return nil, parseErr
		}
		caravans = append(caravans, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating caravans: %w", err)
	}
	return caravans, nil
}

var validCaravanStatuses = map[CaravanStatus]bool{
	CaravanDrydock: true,
	CaravanOpen:    true,
	CaravanReady:   true,
	CaravanClosed:  true,
}

// UpdateCaravanStatus sets the caravan's status. If status is "closed",
// also sets closed_at.
func (s *Store) UpdateCaravanStatus(id string, status CaravanStatus) error {
	if !validCaravanStatuses[status] {
		return fmt.Errorf("invalid caravan status %q", status)
	}
	var result sql.Result
	var err error

	if status == CaravanClosed {
		now := time.Now().UTC().Format(time.RFC3339)
		result, err = s.db.Exec(
			`UPDATE caravans SET status = ?, closed_at = ? WHERE id = ?`,
			status, now, id,
		)
	} else {
		result, err = s.db.Exec(
			`UPDATE caravans SET status = ?, closed_at = NULL WHERE id = ?`,
			status, id,
		)
	}
	if err != nil {
		return fmt.Errorf("failed to update caravan %q status: %w", id, err)
	}
	return checkRowsAffected(result, "caravan", id)
}

// CreateCaravanItem associates a writ with a caravan at the given phase.
func (s *Store) CreateCaravanItem(caravanID, writID, world string, phase int) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO caravan_items (caravan_id, writ_id, world, phase) VALUES (?, ?, ?, ?)`,
		caravanID, writID, world, phase,
	)
	if err != nil {
		return fmt.Errorf("failed to add item %q to caravan %q: %w", writID, caravanID, err)
	}
	return nil
}

// DeleteCaravanItemsForWorld removes all caravan items for a given world.
func (s *Store) DeleteCaravanItemsForWorld(world string) error {
	_, err := s.db.Exec(`DELETE FROM caravan_items WHERE world = ?`, world)
	if err != nil {
		return fmt.Errorf("failed to delete caravan items for world %q: %w", world, err)
	}
	return nil
}

// RemoveCaravanItem removes a writ from a caravan.
func (s *Store) RemoveCaravanItem(caravanID, writID string) error {
	_, err := s.db.Exec(
		`DELETE FROM caravan_items WHERE caravan_id = ? AND writ_id = ?`,
		caravanID, writID,
	)
	if err != nil {
		return fmt.Errorf("failed to remove item %q from caravan %q: %w", writID, caravanID, err)
	}
	return nil
}

// UpdateCaravanItemPhase sets the phase of a single item in a caravan.
func (s *Store) UpdateCaravanItemPhase(caravanID, writID string, phase int) error {
	result, err := s.db.Exec(
		`UPDATE caravan_items SET phase = ? WHERE caravan_id = ? AND writ_id = ?`,
		phase, caravanID, writID,
	)
	if err != nil {
		return fmt.Errorf("failed to update phase for item %q in caravan %q: %w", writID, caravanID, err)
	}
	return checkRowsAffected(result, "item "+writID+" in caravan", caravanID)
}

// UpdateAllCaravanItemPhases sets the phase of all items in a caravan.
func (s *Store) UpdateAllCaravanItemPhases(caravanID string, phase int) (int64, error) {
	result, err := s.db.Exec(
		`UPDATE caravan_items SET phase = ? WHERE caravan_id = ?`,
		phase, caravanID,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to update phases for caravan %q: %w", caravanID, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to check rows affected: %w", err)
	}
	return n, nil
}

// ListCaravanItems returns all items in a caravan.
func (s *Store) ListCaravanItems(caravanID string) ([]CaravanItem, error) {
	rows, err := s.db.Query(
		`SELECT caravan_id, writ_id, world, phase FROM caravan_items WHERE caravan_id = ? ORDER BY phase, writ_id`,
		caravanID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list caravan items for %q: %w", caravanID, err)
	}
	defer rows.Close()

	var items []CaravanItem
	for rows.Next() {
		var ci CaravanItem
		if err := rows.Scan(&ci.CaravanID, &ci.WritID, &ci.World, &ci.Phase); err != nil {
			return nil, fmt.Errorf("failed to scan caravan item: %w", err)
		}
		items = append(items, ci)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating caravan items for %q: %w", caravanID, err)
	}
	return items, nil
}

// GetCaravanItemsForWrit returns all caravan items for a given writ ID.
func (s *Store) GetCaravanItemsForWrit(writID string) ([]CaravanItem, error) {
	rows, err := s.db.Query(
		`SELECT caravan_id, writ_id, world, phase FROM caravan_items WHERE writ_id = ? ORDER BY caravan_id, phase`,
		writID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get caravan items for writ %q: %w", writID, err)
	}
	defer rows.Close()

	var items []CaravanItem
	for rows.Next() {
		var ci CaravanItem
		if err := rows.Scan(&ci.CaravanID, &ci.WritID, &ci.World, &ci.Phase); err != nil {
			return nil, fmt.Errorf("failed to scan caravan item: %w", err)
		}
		items = append(items, ci)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating caravan items for writ %q: %w", writID, err)
	}
	return items, nil
}

// DeleteCaravan permanently removes a caravan and its associated items and
// dependencies. Only drydocked or closed caravans can be deleted.
func (s *Store) DeleteCaravan(id string) error {
	// Delete caravan items.
	if _, err := s.db.Exec(`DELETE FROM caravan_items WHERE caravan_id = ?`, id); err != nil {
		return fmt.Errorf("failed to delete caravan items for %q: %w", id, err)
	}

	// Delete caravan dependencies (both directions).
	if _, err := s.db.Exec(
		`DELETE FROM caravan_dependencies WHERE from_id = ? OR to_id = ?`, id, id,
	); err != nil {
		return fmt.Errorf("failed to delete caravan dependencies for %q: %w", id, err)
	}

	// Delete the caravan itself.
	result, err := s.db.Exec(`DELETE FROM caravans WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete caravan %q: %w", id, err)
	}
	return checkRowsAffected(result, "caravan", id)
}

// CheckCaravanReadiness returns the status of all items in a caravan.
// This requires opening each world's database to check writ status
// and dependency satisfaction.
//
// Caravan-level dependencies: if this caravan depends on other caravans that
// are not yet closed, ALL items are marked not ready.
//
// Phase ordering: items in phase N are only ready if all items in phases < N
// are done or closed. Phase 0 items use only the within-world dependency check.
//
// The worldOpener function opens a world store by name — the caller provides
// this so the caravan checker doesn't need to know about store paths.
func (s *Store) CheckCaravanReadiness(caravanID string,
	worldOpener func(world string) (*Store, error)) ([]CaravanItemStatus, error) {

	items, err := s.ListCaravanItems(caravanID)
	if err != nil {
		return nil, err
	}

	// Check caravan-level dependencies first.
	caravanDepsOK, err := s.AreCaravanDependenciesSatisfied(caravanID)
	if err != nil {
		return nil, fmt.Errorf("failed to check caravan dependencies for %q: %w", caravanID, err)
	}

	// Group items by world.
	byWorld := map[string][]CaravanItem{}
	for _, item := range items {
		byWorld[item.World] = append(byWorld[item.World], item)
	}

	var results []CaravanItemStatus

	for world, worldItems := range byWorld {
		worldResults, err := func() ([]CaravanItemStatus, error) {
			worldStore, err := worldOpener(world)
			if err != nil {
				return nil, fmt.Errorf("failed to open world %q: %w", world, err)
			}
			defer worldStore.Close()

			var out []CaravanItemStatus
			for _, ci := range worldItems {
				cis := CaravanItemStatus{
					WritID: ci.WritID,
					World:      ci.World,
					Phase:      ci.Phase,
				}

				item, err := worldStore.GetWrit(ci.WritID)
				if err != nil {
					cis.WritStatus = "unknown" // not a valid WritStatus constant, but kept for error reporting
					out = append(out, cis)
					continue
				}

				cis.WritStatus = item.Status
				cis.Assignee = item.Assignee

				ready, err := worldStore.IsReady(ci.WritID)
				if err != nil {
					return nil, fmt.Errorf("failed to check readiness for %q: %w", ci.WritID, err)
				}
				cis.Ready = ready

				out = append(out, cis)
			}
			return out, nil
		}()
		if err != nil {
			return nil, err
		}
		results = append(results, worldResults...)
	}

	// If caravan-level dependencies are not satisfied, mark ALL items not ready.
	if !caravanDepsOK {
		for i := range results {
			results[i].Ready = false
		}
		return results, nil
	}

	// Apply phase gating: items in phase N are only ready if all items in
	// phases < N are closed (merged). This is per-caravan, not per-world.
	// Note: "done" (code complete, awaiting merge) is NOT sufficient —
	// prerequisite code must be merged to the target branch.
	for i := range results {
		if results[i].Phase == 0 {
			continue // Phase 0 uses only within-world dependency check.
		}
		if !results[i].Ready {
			continue // Already not ready from dependency check.
		}
		// Check if all items in lower phases are closed (merged).
		for j := range results {
			if results[j].Phase < results[i].Phase {
				if results[j].WritStatus != WritClosed {
					results[i].Ready = false
					break
				}
			}
		}
	}

	return results, nil
}

// TryCloseCaravan checks if all items in a caravan are closed (merged).
// If so, sets the caravan status to "closed".
// Returns true if the caravan was closed.
// Note: "done" (code complete, awaiting merge) is NOT sufficient — all items
// must be "closed" (fully merged) for the caravan to close.
func (s *Store) TryCloseCaravan(caravanID string,
	worldOpener func(world string) (*Store, error)) (bool, error) {

	statuses, err := s.CheckCaravanReadiness(caravanID, worldOpener)
	if err != nil {
		return false, err
	}

	if len(statuses) == 0 {
		return false, nil
	}

	for _, st := range statuses {
		if st.WritStatus != WritClosed {
			return false, nil
		}
	}

	if err := s.UpdateCaravanStatus(caravanID, CaravanClosed); err != nil {
		return false, err
	}
	return true, nil
}
