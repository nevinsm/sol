package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Caravan represents a group of related work items tracked together.
type Caravan struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"` // "open", "ready", "closed"
	Owner     string     `json:"owner"`
	CreatedAt time.Time  `json:"created_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
}

// CaravanItem is a work item associated with a caravan.
type CaravanItem struct {
	CaravanID  string `json:"caravan_id"`
	WorkItemID string `json:"work_item_id"`
	World      string `json:"world"`
	Phase      int    `json:"phase"`
}

// CaravanItemStatus represents the status of a work item within a caravan.
type CaravanItemStatus struct {
	WorkItemID     string `json:"work_item_id"`
	World          string `json:"world"`
	Phase          int    `json:"phase"`
	WorkItemStatus string `json:"work_item_status"`
	Ready          bool   `json:"ready"`
	Assignee       string `json:"assignee,omitempty"`
}

// IsDispatched returns true if the item is actively being worked on by an agent.
func (s CaravanItemStatus) IsDispatched() bool {
	return s.WorkItemStatus == "tethered" || s.WorkItemStatus == "working"
}

// generateCaravanID returns a new caravan ID in the format "car-" + 16 hex chars.
func generateCaravanID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate caravan ID: %w", err)
	}
	return "car-" + hex.EncodeToString(b), nil
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
		`INSERT INTO caravans (id, name, status, owner, created_at) VALUES (?, ?, 'open', ?, ?)`,
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
	c.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at for caravan %q: %w", id, err)
	}
	if closedAt.Valid {
		t, err := time.Parse(time.RFC3339, closedAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse closed_at for caravan %q: %w", id, err)
		}
		c.ClosedAt = &t
	}
	return c, nil
}

// ListCaravans returns caravans, optionally filtered by status.
// If status is empty, returns all caravans.
// Ordered by created_at DESC (newest first).
func (s *Store) ListCaravans(status string) ([]Caravan, error) {
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
		c.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse created_at for caravan %q: %w", c.ID, parseErr)
		}
		if closedAt.Valid {
			t, parseErr := time.Parse(time.RFC3339, closedAt.String)
			if parseErr != nil {
				return nil, fmt.Errorf("failed to parse closed_at for caravan %q: %w", c.ID, parseErr)
			}
			c.ClosedAt = &t
		}
		caravans = append(caravans, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating caravans: %w", err)
	}
	return caravans, nil
}

var validCaravanStatuses = map[string]bool{
	"open":   true,
	"ready":  true,
	"closed": true,
}

// UpdateCaravanStatus sets the caravan's status. If status is "closed",
// also sets closed_at.
func (s *Store) UpdateCaravanStatus(id, status string) error {
	if !validCaravanStatuses[status] {
		return fmt.Errorf("invalid caravan status %q", status)
	}
	var result sql.Result
	var err error

	if status == "closed" {
		now := time.Now().UTC().Format(time.RFC3339)
		result, err = s.db.Exec(
			`UPDATE caravans SET status = ?, closed_at = ? WHERE id = ?`,
			status, now, id,
		)
	} else {
		result, err = s.db.Exec(
			`UPDATE caravans SET status = ? WHERE id = ?`,
			status, id,
		)
	}
	if err != nil {
		return fmt.Errorf("failed to update caravan %q status: %w", id, err)
	}
	// RowsAffected error is unlikely with modernc.org/sqlite but check defensively.
	n, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("failed to check rows affected: %w", raErr)
	}
	if n == 0 {
		return fmt.Errorf("caravan %q: %w", id, ErrNotFound)
	}
	return nil
}

// CreateCaravanItem associates a work item with a caravan at the given phase.
func (s *Store) CreateCaravanItem(caravanID, workItemID, world string, phase int) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO caravan_items (caravan_id, work_item_id, world, phase) VALUES (?, ?, ?, ?)`,
		caravanID, workItemID, world, phase,
	)
	if err != nil {
		return fmt.Errorf("failed to add item %q to caravan %q: %w", workItemID, caravanID, err)
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

// RemoveCaravanItem removes a work item from a caravan.
func (s *Store) RemoveCaravanItem(caravanID, workItemID string) error {
	_, err := s.db.Exec(
		`DELETE FROM caravan_items WHERE caravan_id = ? AND work_item_id = ?`,
		caravanID, workItemID,
	)
	if err != nil {
		return fmt.Errorf("failed to remove item %q from caravan %q: %w", workItemID, caravanID, err)
	}
	return nil
}

// ListCaravanItems returns all items in a caravan.
func (s *Store) ListCaravanItems(caravanID string) ([]CaravanItem, error) {
	rows, err := s.db.Query(
		`SELECT caravan_id, work_item_id, world, phase FROM caravan_items WHERE caravan_id = ? ORDER BY phase, work_item_id`,
		caravanID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list caravan items for %q: %w", caravanID, err)
	}
	defer rows.Close()

	var items []CaravanItem
	for rows.Next() {
		var ci CaravanItem
		if err := rows.Scan(&ci.CaravanID, &ci.WorkItemID, &ci.World, &ci.Phase); err != nil {
			return nil, fmt.Errorf("failed to scan caravan item: %w", err)
		}
		items = append(items, ci)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating caravan items for %q: %w", caravanID, err)
	}
	return items, nil
}

// CheckCaravanReadiness returns the status of all items in a caravan.
// This requires opening each world's database to check work item status
// and dependency satisfaction.
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
					WorkItemID: ci.WorkItemID,
					World:      ci.World,
					Phase:      ci.Phase,
				}

				item, err := worldStore.GetWorkItem(ci.WorkItemID)
				if err != nil {
					cis.WorkItemStatus = "unknown"
					out = append(out, cis)
					continue
				}

				cis.WorkItemStatus = item.Status
				cis.Assignee = item.Assignee

				ready, err := worldStore.IsReady(ci.WorkItemID)
				if err != nil {
					return nil, fmt.Errorf("failed to check readiness for %q: %w", ci.WorkItemID, err)
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
				if results[j].WorkItemStatus != "closed" {
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
		if st.WorkItemStatus != "closed" {
			return false, nil
		}
	}

	if err := s.UpdateCaravanStatus(caravanID, "closed"); err != nil {
		return false, err
	}
	return true, nil
}
