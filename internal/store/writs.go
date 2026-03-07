package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Writ represents a tracked writ in a world database.
type Writ struct {
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

// ListFilters controls which writs are returned by ListWrits.
type ListFilters struct {
	Status   string // empty = all
	Assignee string // empty = all
	Label    string // empty = all
	Priority int    // 0 = all
	ParentID string // empty = all
}

// WritUpdates specifies which fields to update on a writ.
type WritUpdates struct {
	Status      string // empty = no change
	Assignee    string // empty = no change, "-" = clear
	Priority    int    // 0 = no change
	Title       string // empty = no change
	Description string // empty = no change
}

// generateID returns a new writ ID in the format "sol-" + 16 hex chars.
func generateID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate writ ID: %w", err)
	}
	return "sol-" + hex.EncodeToString(b), nil
}

// CreateWrit creates a new writ and returns its generated ID.
func (s *Store) CreateWrit(title, description, createdBy string, priority int, labels []string) (string, error) {
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
		`INSERT INTO writs (id, title, description, status, priority, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, 'open', ?, ?, ?, ?)`,
		id, title, description, priority, createdBy, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("failed to insert writ: %w", err)
	}

	for _, label := range labels {
		_, err = tx.Exec(`INSERT INTO labels (writ_id, label) VALUES (?, ?)`, id, label)
		if err != nil {
			return "", fmt.Errorf("failed to insert label %q: %w", label, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit writ: %w", err)
	}
	return id, nil
}

// CreateWritOpts holds options for creating a writ with full control.
type CreateWritOpts struct {
	Title, Description, CreatedBy string
	Priority                      int
	Labels                        []string
	ParentID                      string // optional
}

// CreateWritWithOpts creates a new writ with full options including parent_id.
func (s *Store) CreateWritWithOpts(opts CreateWritOpts) (string, error) {
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
			`INSERT INTO writs (id, title, description, status, priority, parent_id, created_by, created_at, updated_at)
			 VALUES (?, ?, ?, 'open', ?, ?, ?, ?, ?)`,
			id, opts.Title, opts.Description, opts.Priority, opts.ParentID, opts.CreatedBy, now, now,
		)
	} else {
		_, err = tx.Exec(
			`INSERT INTO writs (id, title, description, status, priority, created_by, created_at, updated_at)
			 VALUES (?, ?, ?, 'open', ?, ?, ?, ?)`,
			id, opts.Title, opts.Description, opts.Priority, opts.CreatedBy, now, now,
		)
	}
	if err != nil {
		return "", fmt.Errorf("failed to insert writ: %w", err)
	}

	for _, label := range opts.Labels {
		_, err = tx.Exec(`INSERT INTO labels (writ_id, label) VALUES (?, ?)`, id, label)
		if err != nil {
			return "", fmt.Errorf("failed to insert label %q: %w", label, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit writ: %w", err)
	}
	return id, nil
}

// HasLabel returns true if the writ has the given label.
func (w *Writ) HasLabel(label string) bool {
	for _, l := range w.Labels {
		if l == label {
			return true
		}
	}
	return false
}

// GetWrit returns a writ by ID, including its labels.
func (s *Store) GetWrit(id string) (*Writ, error) {
	w := &Writ{}
	var desc, assignee, parentID sql.NullString
	var closedAt sql.NullString
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		`SELECT id, title, description, status, priority, assignee, parent_id, created_by, created_at, updated_at, closed_at
		 FROM writs WHERE id = ?`, id,
	).Scan(&w.ID, &w.Title, &desc, &w.Status, &w.Priority, &assignee, &parentID, &w.CreatedBy, &createdAt, &updatedAt, &closedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("writ %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get writ %q: %w", id, err)
	}

	w.Description = desc.String
	w.Assignee = assignee.String
	w.ParentID = parentID.String
	w.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at for writ %q: %w", id, err)
	}
	w.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse updated_at for writ %q: %w", id, err)
	}
	if closedAt.Valid {
		t, err := time.Parse(time.RFC3339, closedAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse closed_at for writ %q: %w", id, err)
		}
		w.ClosedAt = &t
	}

	// Fetch labels.
	rows, err := s.db.Query(`SELECT label FROM labels WHERE writ_id = ? ORDER BY label`, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get labels for writ %q: %w", id, err)
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
		return nil, fmt.Errorf("failed iterating labels for writ %q: %w", id, err)
	}
	return w, nil
}

// ListWrits returns writs matching the filters.
func (s *Store) ListWrits(filters ListFilters) ([]Writ, error) {
	query := `SELECT DISTINCT w.id, w.title, w.description, w.status, w.priority, w.assignee, w.parent_id, w.created_by, w.created_at, w.updated_at, w.closed_at
	           FROM writs w`
	var conditions []string
	var args []interface{}

	if filters.Label != "" {
		query += ` JOIN labels l ON w.id = l.writ_id`
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
	if filters.ParentID != "" {
		conditions = append(conditions, "w.parent_id = ?")
		args = append(args, filters.ParentID)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY w.priority ASC, w.created_at ASC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list writs: %w", err)
	}
	defer rows.Close()

	var items []Writ
	for rows.Next() {
		var w Writ
		var desc, assignee, parentID sql.NullString
		var closedAt sql.NullString
		var createdAt, updatedAt string

		if err := rows.Scan(&w.ID, &w.Title, &desc, &w.Status, &w.Priority, &assignee, &parentID, &w.CreatedBy, &createdAt, &updatedAt, &closedAt); err != nil {
			return nil, fmt.Errorf("failed to scan writ: %w", err)
		}
		w.Description = desc.String
		w.Assignee = assignee.String
		w.ParentID = parentID.String
		var parseErr error
		w.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse created_at for writ %q: %w", w.ID, parseErr)
		}
		w.UpdatedAt, parseErr = time.Parse(time.RFC3339, updatedAt)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse updated_at for writ %q: %w", w.ID, parseErr)
		}
		if closedAt.Valid {
			t, parseErr := time.Parse(time.RFC3339, closedAt.String)
			if parseErr != nil {
				return nil, fmt.Errorf("failed to parse closed_at for writ %q: %w", w.ID, parseErr)
			}
			w.ClosedAt = &t
		}
		items = append(items, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating writs: %w", err)
	}

	// Batch-fetch all labels for returned items.
	if len(items) > 0 {
		ids := make([]interface{}, len(items))
		placeholders := make([]string, len(items))
		for i, item := range items {
			ids[i] = item.ID
			placeholders[i] = "?"
		}

		labelQuery := fmt.Sprintf(
			`SELECT writ_id, label FROM labels WHERE writ_id IN (%s) ORDER BY writ_id, label`,
			strings.Join(placeholders, ","),
		)
		labelRows, err := s.db.Query(labelQuery, ids...)
		if err != nil {
			return nil, fmt.Errorf("failed to query labels: %w", err)
		}
		defer labelRows.Close()

		labelMap := make(map[string][]string)
		for labelRows.Next() {
			var itemID, label string
			if err := labelRows.Scan(&itemID, &label); err != nil {
				return nil, fmt.Errorf("failed to scan label: %w", err)
			}
			labelMap[itemID] = append(labelMap[itemID], label)
		}
		if err := labelRows.Err(); err != nil {
			return nil, fmt.Errorf("failed to iterate labels: %w", err)
		}

		for i := range items {
			items[i].Labels = labelMap[items[i].ID]
		}
	}
	return items, nil
}

// ListChildWrits returns all writs with the given parent_id.
func (s *Store) ListChildWrits(parentID string) ([]Writ, error) {
	return s.ListWrits(ListFilters{ParentID: parentID})
}

// validWritStatuses is the set of allowed writ status values.
var validWritStatuses = map[string]bool{
	"open":     true,
	"tethered": true,
	"working":  true,
	"resolve":  true,
	"done":     true,
	"closed":   true,
}

// UpdateWrit updates fields on a writ. Only non-zero fields are applied.
func (s *Store) UpdateWrit(id string, updates WritUpdates) error {
	var sets []string
	var args []interface{}

	if updates.Status != "" {
		if !validWritStatuses[updates.Status] {
			return fmt.Errorf("invalid writ status %q", updates.Status)
		}
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
	if updates.Title != "" {
		sets = append(sets, "title = ?")
		args = append(args, updates.Title)
	}
	if updates.Description != "" {
		sets = append(sets, "description = ?")
		args = append(args, updates.Description)
	}

	if len(sets) == 0 {
		return fmt.Errorf("no updates specified for writ %q", id)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	sets = append(sets, "updated_at = ?")
	args = append(args, now)
	args = append(args, id)

	result, err := s.db.Exec(
		fmt.Sprintf("UPDATE writs SET %s WHERE id = ?", strings.Join(sets, ", ")),
		args...,
	)
	if err != nil {
		return fmt.Errorf("failed to update writ %q: %w", id, err)
	}
	// RowsAffected error is unlikely with modernc.org/sqlite but check defensively.
	n, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("failed to check rows affected: %w", raErr)
	}
	if n == 0 {
		return fmt.Errorf("writ %q: %w", id, ErrNotFound)
	}
	return nil
}

// CloseWrit sets status to "closed" and records closed_at.
func (s *Store) CloseWrit(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(
		`UPDATE writs SET status = 'closed', closed_at = ?, updated_at = ? WHERE id = ?`,
		now, now, id,
	)
	if err != nil {
		return fmt.Errorf("failed to close writ %q: %w", id, err)
	}
	// RowsAffected error is unlikely with modernc.org/sqlite but check defensively.
	n, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("failed to check rows affected: %w", raErr)
	}
	if n == 0 {
		return fmt.Errorf("writ %q: %w", id, ErrNotFound)
	}
	return nil
}

// AddLabel adds a label to a writ. No-op if already present.
func (s *Store) AddLabel(itemID, label string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO labels (writ_id, label) VALUES (?, ?)`,
		itemID, label,
	)
	if err != nil {
		return fmt.Errorf("failed to add label %q to writ %q: %w", label, itemID, err)
	}
	return nil
}

// RemoveLabel removes a label from a writ.
func (s *Store) RemoveLabel(itemID, label string) error {
	_, err := s.db.Exec(
		`DELETE FROM labels WHERE writ_id = ? AND label = ?`,
		itemID, label,
	)
	if err != nil {
		return fmt.Errorf("failed to remove label %q from writ %q: %w", label, itemID, err)
	}
	return nil
}
