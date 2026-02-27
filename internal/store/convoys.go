package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// Convoy represents a group of related work items tracked together.
type Convoy struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"` // "open", "ready", "closed"
	Owner     string     `json:"owner"`
	CreatedAt time.Time  `json:"created_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
}

// ConvoyItem is a work item associated with a convoy.
type ConvoyItem struct {
	ConvoyID   string `json:"convoy_id"`
	WorkItemID string `json:"work_item_id"`
	Rig        string `json:"rig"`
}

// ConvoyItemStatus represents the status of a work item within a convoy.
type ConvoyItemStatus struct {
	ConvoyItem
	WorkItemStatus string `json:"work_item_status"` // status from the rig's work_items table
	Ready          bool   `json:"ready"`             // true if all dependencies are satisfied
}

// generateConvoyID returns a new convoy ID in the format "convoy-" + 8 hex chars.
func generateConvoyID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate convoy ID: %w", err)
	}
	return "convoy-" + hex.EncodeToString(b), nil
}

// CreateConvoy creates a convoy with the given name and owner.
// Returns the convoy ID.
func (s *Store) CreateConvoy(name, owner string) (string, error) {
	id, err := generateConvoyID()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = s.db.Exec(
		`INSERT INTO convoys (id, name, status, owner, created_at) VALUES (?, ?, 'open', ?, ?)`,
		id, name, owner, now,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create convoy %q: %w", name, err)
	}
	return id, nil
}

// GetConvoy returns a convoy by ID.
func (s *Store) GetConvoy(id string) (*Convoy, error) {
	c := &Convoy{}
	var owner sql.NullString
	var closedAt sql.NullString
	var createdAt string

	err := s.db.QueryRow(
		`SELECT id, name, status, owner, created_at, closed_at FROM convoys WHERE id = ?`, id,
	).Scan(&c.ID, &c.Name, &c.Status, &owner, &createdAt, &closedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("convoy %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get convoy %q: %w", id, err)
	}

	c.Owner = owner.String
	c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	if closedAt.Valid {
		t, _ := time.Parse(time.RFC3339, closedAt.String)
		c.ClosedAt = &t
	}
	return c, nil
}

// ListConvoys returns convoys, optionally filtered by status.
// If status is empty, returns all convoys.
// Ordered by created_at DESC (newest first).
func (s *Store) ListConvoys(status string) ([]Convoy, error) {
	query := `SELECT id, name, status, owner, created_at, closed_at FROM convoys`
	var args []interface{}

	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list convoys: %w", err)
	}
	defer rows.Close()

	var convoys []Convoy
	for rows.Next() {
		var c Convoy
		var owner sql.NullString
		var closedAt sql.NullString
		var createdAt string

		if err := rows.Scan(&c.ID, &c.Name, &c.Status, &owner, &createdAt, &closedAt); err != nil {
			return nil, fmt.Errorf("failed to scan convoy: %w", err)
		}
		c.Owner = owner.String
		c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		if closedAt.Valid {
			t, _ := time.Parse(time.RFC3339, closedAt.String)
			c.ClosedAt = &t
		}
		convoys = append(convoys, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating convoys: %w", err)
	}
	return convoys, nil
}

// UpdateConvoyStatus sets the convoy's status. If status is "closed",
// also sets closed_at.
func (s *Store) UpdateConvoyStatus(id, status string) error {
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
		return fmt.Errorf("failed to update convoy %q status: %w", id, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("convoy %q not found", id)
	}
	return nil
}

// AddConvoyItem associates a work item with a convoy.
func (s *Store) AddConvoyItem(convoyID, workItemID, rig string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO convoy_items (convoy_id, work_item_id, rig) VALUES (?, ?, ?)`,
		convoyID, workItemID, rig,
	)
	if err != nil {
		return fmt.Errorf("failed to add item %q to convoy %q: %w", workItemID, convoyID, err)
	}
	return nil
}

// RemoveConvoyItem removes a work item from a convoy.
func (s *Store) RemoveConvoyItem(convoyID, workItemID string) error {
	_, err := s.db.Exec(
		`DELETE FROM convoy_items WHERE convoy_id = ? AND work_item_id = ?`,
		convoyID, workItemID,
	)
	if err != nil {
		return fmt.Errorf("failed to remove item %q from convoy %q: %w", workItemID, convoyID, err)
	}
	return nil
}

// ListConvoyItems returns all items in a convoy.
func (s *Store) ListConvoyItems(convoyID string) ([]ConvoyItem, error) {
	rows, err := s.db.Query(
		`SELECT convoy_id, work_item_id, rig FROM convoy_items WHERE convoy_id = ? ORDER BY work_item_id`,
		convoyID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list convoy items for %q: %w", convoyID, err)
	}
	defer rows.Close()

	var items []ConvoyItem
	for rows.Next() {
		var ci ConvoyItem
		if err := rows.Scan(&ci.ConvoyID, &ci.WorkItemID, &ci.Rig); err != nil {
			return nil, fmt.Errorf("failed to scan convoy item: %w", err)
		}
		items = append(items, ci)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating convoy items for %q: %w", convoyID, err)
	}
	return items, nil
}

// CheckConvoyReadiness returns the status of all items in a convoy.
// This requires opening each rig's database to check work item status
// and dependency satisfaction.
//
// The rigOpener function opens a rig store by name — the caller provides
// this so the convoy checker doesn't need to know about store paths.
func (s *Store) CheckConvoyReadiness(convoyID string,
	rigOpener func(rig string) (*Store, error)) ([]ConvoyItemStatus, error) {

	items, err := s.ListConvoyItems(convoyID)
	if err != nil {
		return nil, err
	}

	// Group items by rig.
	byRig := map[string][]ConvoyItem{}
	for _, item := range items {
		byRig[item.Rig] = append(byRig[item.Rig], item)
	}

	var results []ConvoyItemStatus

	for rig, rigItems := range byRig {
		rigStore, err := rigOpener(rig)
		if err != nil {
			return nil, fmt.Errorf("failed to open rig %q: %w", rig, err)
		}

		for _, ci := range rigItems {
			cis := ConvoyItemStatus{ConvoyItem: ci}

			item, err := rigStore.GetWorkItem(ci.WorkItemID)
			if err != nil {
				// Work item might not exist in rig yet.
				cis.WorkItemStatus = "unknown"
				results = append(results, cis)
				continue
			}

			cis.WorkItemStatus = item.Status

			ready, err := rigStore.IsReady(ci.WorkItemID)
			if err != nil {
				rigStore.Close()
				return nil, fmt.Errorf("failed to check readiness for %q: %w", ci.WorkItemID, err)
			}
			cis.Ready = ready

			results = append(results, cis)
		}

		rigStore.Close()
	}

	return results, nil
}

// TryCloseConvoy checks if all items in a convoy are done/closed.
// If so, sets the convoy status to "closed".
// Returns true if the convoy was closed.
func (s *Store) TryCloseConvoy(convoyID string,
	rigOpener func(rig string) (*Store, error)) (bool, error) {

	statuses, err := s.CheckConvoyReadiness(convoyID, rigOpener)
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

	if err := s.UpdateConvoyStatus(convoyID, "closed"); err != nil {
		return false, err
	}
	return true, nil
}
