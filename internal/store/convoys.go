package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
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
}

// CaravanItemStatus represents the status of a work item within a caravan.
type CaravanItemStatus struct {
	CaravanItem
	WorkItemStatus string `json:"work_item_status"` // status from the world's work_items table
	Ready          bool   `json:"ready"`             // true if all dependencies are satisfied
}

// generateCaravanID returns a new caravan ID in the format "car-" + 8 hex chars.
func generateCaravanID() (string, error) {
	b := make([]byte, 4)
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
		`INSERT INTO convoys (id, name, status, owner, created_at) VALUES (?, ?, 'open', ?, ?)`,
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
		`SELECT id, name, status, owner, created_at, closed_at FROM convoys WHERE id = ?`, id,
	).Scan(&c.ID, &c.Name, &c.Status, &owner, &createdAt, &closedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("caravan %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get caravan %q: %w", id, err)
	}

	c.Owner = owner.String
	c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	if closedAt.Valid {
		t, _ := time.Parse(time.RFC3339, closedAt.String)
		c.ClosedAt = &t
	}
	return c, nil
}

// ListCaravans returns caravans, optionally filtered by status.
// If status is empty, returns all caravans.
// Ordered by created_at DESC (newest first).
func (s *Store) ListCaravans(status string) ([]Caravan, error) {
	query := `SELECT id, name, status, owner, created_at, closed_at FROM convoys`
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
		c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		if closedAt.Valid {
			t, _ := time.Parse(time.RFC3339, closedAt.String)
			c.ClosedAt = &t
		}
		caravans = append(caravans, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating caravans: %w", err)
	}
	return caravans, nil
}

// UpdateCaravanStatus sets the caravan's status. If status is "closed",
// also sets closed_at.
func (s *Store) UpdateCaravanStatus(id, status string) error {
	var result sql.Result
	var err error

	if status == "closed" {
		now := time.Now().UTC().Format(time.RFC3339)
		result, err = s.db.Exec(
			`UPDATE convoys SET status = ?, closed_at = ? WHERE id = ?`,
			status, now, id,
		)
	} else {
		result, err = s.db.Exec(
			`UPDATE convoys SET status = ? WHERE id = ?`,
			status, id,
		)
	}
	if err != nil {
		return fmt.Errorf("failed to update caravan %q status: %w", id, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("caravan %q not found", id)
	}
	return nil
}

// AddCaravanItem associates a work item with a caravan.
func (s *Store) AddCaravanItem(caravanID, workItemID, world string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO convoy_items (convoy_id, work_item_id, rig) VALUES (?, ?, ?)`,
		caravanID, workItemID, world,
	)
	if err != nil {
		return fmt.Errorf("failed to add item %q to caravan %q: %w", workItemID, caravanID, err)
	}
	return nil
}

// RemoveCaravanItem removes a work item from a caravan.
func (s *Store) RemoveCaravanItem(caravanID, workItemID string) error {
	_, err := s.db.Exec(
		`DELETE FROM convoy_items WHERE convoy_id = ? AND work_item_id = ?`,
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
		`SELECT convoy_id, work_item_id, rig FROM convoy_items WHERE convoy_id = ? ORDER BY work_item_id`,
		caravanID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list caravan items for %q: %w", caravanID, err)
	}
	defer rows.Close()

	var items []CaravanItem
	for rows.Next() {
		var ci CaravanItem
		if err := rows.Scan(&ci.CaravanID, &ci.WorkItemID, &ci.World); err != nil {
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
// The worldOpener function opens a world store by name — the caller provides
// this so the caravan checker doesn't need to know about store paths.
func (s *Store) CheckCaravanReadiness(caravanID string,
	worldOpener func(world string) (*Store, error)) ([]CaravanItemStatus, error) {

	items, err := s.ListCaravanItems(caravanID)
	if err != nil {
		return nil, err
	}

	// Group items by world (rig column in DB).
	byWorld := map[string][]CaravanItem{}
	for _, item := range items {
		byWorld[item.World] = append(byWorld[item.World], item)
	}

	var results []CaravanItemStatus

	for world, worldItems := range byWorld {
		worldStore, err := worldOpener(world)
		if err != nil {
			return nil, fmt.Errorf("failed to open world %q: %w", world, err)
		}

		for _, ci := range worldItems {
			cis := CaravanItemStatus{CaravanItem: ci}

			item, err := worldStore.GetWorkItem(ci.WorkItemID)
			if err != nil {
				// Work item might not exist in world yet.
				cis.WorkItemStatus = "unknown"
				results = append(results, cis)
				continue
			}

			cis.WorkItemStatus = item.Status

			ready, err := worldStore.IsReady(ci.WorkItemID)
			if err != nil {
				worldStore.Close()
				return nil, fmt.Errorf("failed to check readiness for %q: %w", ci.WorkItemID, err)
			}
			cis.Ready = ready

			results = append(results, cis)
		}

		worldStore.Close()
	}

	return results, nil
}

// TryCloseCaravan checks if all items in a caravan are done/closed.
// If so, sets the caravan status to "closed".
// Returns true if the caravan was closed.
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
		if st.WorkItemStatus != "done" && st.WorkItemStatus != "closed" {
			return false, nil
		}
	}

	if err := s.UpdateCaravanStatus(caravanID, "closed"); err != nil {
		return false, err
	}
	return true, nil
}
