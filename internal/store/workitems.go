package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// WorkItem represents a tracked work item in a world database.
type WorkItem struct {
	ID          string
	Title       string
	Description string
	Status      string
	Priority    int
	Assignee    string
	ParentID    string
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ClosedAt    *time.Time
	Labels      []string
}

// ListFilters controls which work items are returned by ListWorkItems.
type ListFilters struct {
	Status   string // empty = all
	Assignee string // empty = all
	Label    string // empty = all
	Priority int    // 0 = all
}

// WorkItemUpdates specifies which fields to update on a work item.
type WorkItemUpdates struct {
	Status   string // empty = no change
	Assignee string // empty = no change, "-" = clear
	Priority int    // 0 = no change
}

// generateID returns a new work item ID in the format "sol-" + 8 hex chars.
func generateID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate work item ID: %w", err)
	}
	return "sol-" + hex.EncodeToString(b), nil
}

// CreateWorkItem creates a new work item and returns its generated ID.
func (s *Store) CreateWorkItem(title, description, createdBy string, priority int, labels []string) (string, error) {
	id, err := generateID()
	if err != nil {
		return "", err
	}
	if priority == 0 {
		priority = 2
	}
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return "", fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		`INSERT INTO work_items (id, title, description, status, priority, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, 'open', ?, ?, ?, ?)`,
		id, title, description, priority, createdBy, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("failed to insert work item: %w", err)
	}

	for _, label := range labels {
		_, err = tx.Exec(`INSERT INTO labels (work_item_id, label) VALUES (?, ?)`, id, label)
		if err != nil {
			return "", fmt.Errorf("failed to insert label %q: %w", label, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit work item: %w", err)
	}
	return id, nil
}

// CreateWorkItemOpts holds options for creating a work item with full control.
type CreateWorkItemOpts struct {
	Title, Description, CreatedBy string
	Priority                      int
	Labels                        []string
	ParentID                      string // optional
}

// CreateWorkItemWithOpts creates a new work item with full options including parent_id.
func (s *Store) CreateWorkItemWithOpts(opts CreateWorkItemOpts) (string, error) {
	id, err := generateID()
	if err != nil {
		return "", err
	}
	if opts.Priority == 0 {
		opts.Priority = 2
	}
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return "", fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if opts.ParentID != "" {
		_, err = tx.Exec(
			`INSERT INTO work_items (id, title, description, status, priority, parent_id, created_by, created_at, updated_at)
			 VALUES (?, ?, ?, 'open', ?, ?, ?, ?, ?)`,
			id, opts.Title, opts.Description, opts.Priority, opts.ParentID, opts.CreatedBy, now, now,
		)
	} else {
		_, err = tx.Exec(
			`INSERT INTO work_items (id, title, description, status, priority, created_by, created_at, updated_at)
			 VALUES (?, ?, ?, 'open', ?, ?, ?, ?)`,
			id, opts.Title, opts.Description, opts.Priority, opts.CreatedBy, now, now,
		)
	}
	if err != nil {
		return "", fmt.Errorf("failed to insert work item: %w", err)
	}

	for _, label := range opts.Labels {
		_, err = tx.Exec(`INSERT INTO labels (work_item_id, label) VALUES (?, ?)`, id, label)
		if err != nil {
			return "", fmt.Errorf("failed to insert label %q: %w", label, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit work item: %w", err)
	}
	return id, nil
}

// HasLabel returns true if the work item has the given label.
func (w *WorkItem) HasLabel(label string) bool {
	for _, l := range w.Labels {
		if l == label {
			return true
		}
	}
	return false
}

// GetWorkItem returns a work item by ID, including its labels.
func (s *Store) GetWorkItem(id string) (*WorkItem, error) {
	w := &WorkItem{}
	var desc, assignee, parentID sql.NullString
	var closedAt sql.NullString
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		`SELECT id, title, description, status, priority, assignee, parent_id, created_by, created_at, updated_at, closed_at
		 FROM work_items WHERE id = ?`, id,
	).Scan(&w.ID, &w.Title, &desc, &w.Status, &w.Priority, &assignee, &parentID, &w.CreatedBy, &createdAt, &updatedAt, &closedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("work item %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get work item %q: %w", id, err)
	}

	w.Description = desc.String
	w.Assignee = assignee.String
	w.ParentID = parentID.String
	w.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	w.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	if closedAt.Valid {
		t, _ := time.Parse(time.RFC3339, closedAt.String)
		w.ClosedAt = &t
	}

	// Fetch labels.
	rows, err := s.db.Query(`SELECT label FROM labels WHERE work_item_id = ? ORDER BY label`, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get labels for work item %q: %w", id, err)
	}
	defer rows.Close()
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			return nil, fmt.Errorf("failed to scan label: %w", err)
		}
		w.Labels = append(w.Labels, label)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating labels for work item %q: %w", id, err)
	}
	return w, nil
}

// ListWorkItems returns work items matching the filters.
func (s *Store) ListWorkItems(filters ListFilters) ([]WorkItem, error) {
	query := `SELECT DISTINCT w.id, w.title, w.description, w.status, w.priority, w.assignee, w.parent_id, w.created_by, w.created_at, w.updated_at, w.closed_at
	           FROM work_items w`
	var conditions []string
	var args []interface{}

	if filters.Label != "" {
		query += ` JOIN labels l ON w.id = l.work_item_id`
		conditions = append(conditions, "l.label = ?")
		args = append(args, filters.Label)
	}
	if filters.Status != "" {
		conditions = append(conditions, "w.status = ?")
		args = append(args, filters.Status)
	}
	if filters.Assignee != "" {
		conditions = append(conditions, "w.assignee = ?")
		args = append(args, filters.Assignee)
	}
	if filters.Priority != 0 {
		conditions = append(conditions, "w.priority = ?")
		args = append(args, filters.Priority)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY w.priority ASC, w.created_at ASC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list work items: %w", err)
	}
	defer rows.Close()

	var items []WorkItem
	for rows.Next() {
		var w WorkItem
		var desc, assignee, parentID sql.NullString
		var closedAt sql.NullString
		var createdAt, updatedAt string

		if err := rows.Scan(&w.ID, &w.Title, &desc, &w.Status, &w.Priority, &assignee, &parentID, &w.CreatedBy, &createdAt, &updatedAt, &closedAt); err != nil {
			return nil, fmt.Errorf("failed to scan work item: %w", err)
		}
		w.Description = desc.String
		w.Assignee = assignee.String
		w.ParentID = parentID.String
		w.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		w.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		if closedAt.Valid {
			t, _ := time.Parse(time.RFC3339, closedAt.String)
			w.ClosedAt = &t
		}
		items = append(items, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating work items: %w", err)
	}

	// Fetch labels for each item.
	for i := range items {
		labelRows, err := s.db.Query(`SELECT label FROM labels WHERE work_item_id = ? ORDER BY label`, items[i].ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get labels: %w", err)
		}
		for labelRows.Next() {
			var label string
			if err := labelRows.Scan(&label); err != nil {
				labelRows.Close()
				return nil, fmt.Errorf("failed to scan label: %w", err)
			}
			items[i].Labels = append(items[i].Labels, label)
		}
		labelRows.Close()
	}
	return items, nil
}

// UpdateWorkItem updates fields on a work item. Only non-zero fields are applied.
func (s *Store) UpdateWorkItem(id string, updates WorkItemUpdates) error {
	var sets []string
	var args []interface{}

	if updates.Status != "" {
		sets = append(sets, "status = ?")
		args = append(args, updates.Status)
	}
	if updates.Assignee == "-" {
		sets = append(sets, "assignee = NULL")
	} else if updates.Assignee != "" {
		sets = append(sets, "assignee = ?")
		args = append(args, updates.Assignee)
	}
	if updates.Priority != 0 {
		sets = append(sets, "priority = ?")
		args = append(args, updates.Priority)
	}

	if len(sets) == 0 {
		return fmt.Errorf("no updates specified for work item %q", id)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	sets = append(sets, "updated_at = ?")
	args = append(args, now)
	args = append(args, id)

	result, err := s.db.Exec(
		fmt.Sprintf("UPDATE work_items SET %s WHERE id = ?", strings.Join(sets, ", ")),
		args...,
	)
	if err != nil {
		return fmt.Errorf("failed to update work item %q: %w", id, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("work item %q not found", id)
	}
	return nil
}

// CloseWorkItem sets status to "closed" and records closed_at.
func (s *Store) CloseWorkItem(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(
		`UPDATE work_items SET status = 'closed', closed_at = ?, updated_at = ? WHERE id = ?`,
		now, now, id,
	)
	if err != nil {
		return fmt.Errorf("failed to close work item %q: %w", id, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("work item %q not found", id)
	}
	return nil
}

// AddLabel adds a label to a work item. No-op if already present.
func (s *Store) AddLabel(itemID, label string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO labels (work_item_id, label) VALUES (?, ?)`,
		itemID, label,
	)
	if err != nil {
		return fmt.Errorf("failed to add label %q to work item %q: %w", label, itemID, err)
	}
	return nil
}

// RemoveLabel removes a label from a work item.
func (s *Store) RemoveLabel(itemID, label string) error {
	_, err := s.db.Exec(
		`DELETE FROM labels WHERE work_item_id = ? AND label = ?`,
		itemID, label,
	)
	if err != nil {
		return fmt.Errorf("failed to remove label %q from work item %q: %w", label, itemID, err)
	}
	return nil
}
