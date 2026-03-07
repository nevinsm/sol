package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
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
	Kind        string
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ClosedAt    *time.Time
	CloseReason string
	Labels      []string
	Metadata    map[string]any
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
	ParentID                      string         // optional
	Kind                          string         // optional, defaults to "code"
	Metadata                      map[string]any // optional
}

// CreateWritWithOpts creates a new writ with full options including parent_id, kind, and metadata.
func (s *Store) CreateWritWithOpts(opts CreateWritOpts) (string, error) {
	id, err := generateID()
	if err != nil {
		return "", err
	}
	if opts.Priority == 0 {
		opts.Priority = 2
	}
	kind := opts.Kind
	if kind == "" {
		kind = "code"
	}

	// Validate and serialize metadata if provided.
	var metadataJSON sql.NullString
	if opts.Metadata != nil {
		b, err := json.Marshal(opts.Metadata)
		if err != nil {
			return "", fmt.Errorf("failed to marshal metadata: %w", err)
		}
		metadataJSON = sql.NullString{String: string(b), Valid: true}
	}

	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return "", fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var parentID sql.NullString
	if opts.ParentID != "" {
		parentID = sql.NullString{String: opts.ParentID, Valid: true}
	}
	_, err = tx.Exec(
		`INSERT INTO writs (id, title, description, status, priority, parent_id, kind, metadata, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, 'open', ?, ?, ?, ?, ?, ?, ?)`,
		id, opts.Title, opts.Description, opts.Priority, parentID, kind, metadataJSON, opts.CreatedBy, now, now,
	)
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
	var desc, assignee, parentID, closeReason, metadataRaw sql.NullString
	var closedAt sql.NullString
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		`SELECT id, title, description, status, priority, assignee, parent_id, kind, metadata, created_by, created_at, updated_at, closed_at, close_reason
		 FROM writs WHERE id = ?`, id,
	).Scan(&w.ID, &w.Title, &desc, &w.Status, &w.Priority, &assignee, &parentID, &w.Kind, &metadataRaw, &w.CreatedBy, &createdAt, &updatedAt, &closedAt, &closeReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("writ %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get writ %q: %w", id, err)
	}

	w.Description = desc.String
	w.Assignee = assignee.String
	w.ParentID = parentID.String
	w.CloseReason = closeReason.String
	if metadataRaw.Valid {
		if err := json.Unmarshal([]byte(metadataRaw.String), &w.Metadata); err != nil {
			return nil, fmt.Errorf("failed to parse metadata for writ %q: %w", id, err)
		}
	}
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
	query := `SELECT DISTINCT w.id, w.title, w.description, w.status, w.priority, w.assignee, w.parent_id, w.kind, w.metadata, w.created_by, w.created_at, w.updated_at, w.closed_at, w.close_reason
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
		var desc, assignee, parentID, closeReason, metadataRaw sql.NullString
		var closedAt sql.NullString
		var createdAt, updatedAt string

		if err := rows.Scan(&w.ID, &w.Title, &desc, &w.Status, &w.Priority, &assignee, &parentID, &w.Kind, &metadataRaw, &w.CreatedBy, &createdAt, &updatedAt, &closedAt, &closeReason); err != nil {
			return nil, fmt.Errorf("failed to scan writ: %w", err)
		}
		w.Description = desc.String
		w.Assignee = assignee.String
		w.ParentID = parentID.String
		w.CloseReason = closeReason.String
		if metadataRaw.Valid {
			if err := json.Unmarshal([]byte(metadataRaw.String), &w.Metadata); err != nil {
				return nil, fmt.Errorf("failed to parse metadata for writ %q: %w", w.ID, err)
			}
		}
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
// An optional close reason can be provided as the second argument.
func (s *Store) CloseWrit(id string, closeReason ...string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var result sql.Result
	var err error
	if len(closeReason) > 0 && closeReason[0] != "" {
		result, err = s.db.Exec(
			`UPDATE writs SET status = 'closed', closed_at = ?, close_reason = ?, updated_at = ? WHERE id = ?`,
			now, closeReason[0], now, id,
		)
	} else {
		result, err = s.db.Exec(
			`UPDATE writs SET status = 'closed', closed_at = ?, updated_at = ? WHERE id = ?`,
			now, now, id,
		)
	}
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

// GetWritMetadata returns the metadata for a writ.
func (s *Store) GetWritMetadata(id string) (map[string]any, error) {
	var raw sql.NullString
	err := s.db.QueryRow(`SELECT metadata FROM writs WHERE id = ?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("writ %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata for writ %q: %w", id, err)
	}
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(raw.String), &meta); err != nil {
		return nil, fmt.Errorf("failed to parse metadata for writ %q: %w", id, err)
	}
	return meta, nil
}

// SetWritMetadata merges the given metadata into the writ's existing metadata.
// Keys set to nil are deleted from the metadata.
func (s *Store) SetWritMetadata(id string, metadata map[string]any) error {
	// Validate the provided metadata is well-formed by marshaling it.
	if _, err := json.Marshal(metadata); err != nil {
		return fmt.Errorf("invalid metadata: %w", err)
	}

	// Read existing metadata.
	existing, err := s.GetWritMetadata(id)
	if err != nil {
		return err
	}
	if existing == nil {
		existing = make(map[string]any)
	}

	// Merge: set keys, delete keys with nil values.
	for k, v := range metadata {
		if v == nil {
			delete(existing, k)
		} else {
			existing[k] = v
		}
	}

	// Serialize merged metadata.
	var metadataJSON sql.NullString
	if len(existing) > 0 {
		b, err := json.Marshal(existing)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
		metadataJSON = sql.NullString{String: string(b), Valid: true}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(
		`UPDATE writs SET metadata = ?, updated_at = ? WHERE id = ?`,
		metadataJSON, now, id,
	)
	if err != nil {
		return fmt.Errorf("failed to set metadata for writ %q: %w", id, err)
	}
	n, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("failed to check rows affected: %w", raErr)
	}
	if n == 0 {
		return fmt.Errorf("writ %q: %w", id, ErrNotFound)
	}
	return nil
}

// ReadyWrits returns writs that are ready for dispatch: status is "open"
// and all dependencies (direct) are closed. A writ whose direct dependency
// is not closed is implicitly transitively blocked — e.g. if A blocks B and
// B blocks C, then B is not closed, so C's direct dep check fails.
//
// Writs are returned sorted by priority (highest first = lowest number),
// then creation date (oldest first).
//
// Caravan-level checks (caravan deps, phase gating) are NOT applied here —
// callers should use IsWritBlockedByCaravan on the sphere store for that.
func (s *Store) ReadyWrits() ([]Writ, error) {
	query := `SELECT DISTINCT w.id, w.title, w.description, w.status, w.priority, w.assignee, w.parent_id, w.kind, w.metadata, w.created_by, w.created_at, w.updated_at, w.closed_at, w.close_reason
	           FROM writs w
	           WHERE w.status = 'open'
	           AND NOT EXISTS (
	               SELECT 1 FROM dependencies d
	               JOIN writs dep ON d.to_id = dep.id
	               WHERE d.from_id = w.id AND dep.status != 'closed'
	           )
	           ORDER BY w.priority ASC, w.created_at ASC`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query ready writs: %w", err)
	}
	defer rows.Close()

	var items []Writ
	for rows.Next() {
		var w Writ
		var desc, assignee, parentID, closeReason, metadataRaw sql.NullString
		var closedAt sql.NullString
		var createdAt, updatedAt string

		if err := rows.Scan(&w.ID, &w.Title, &desc, &w.Status, &w.Priority, &assignee, &parentID, &w.Kind, &metadataRaw, &w.CreatedBy, &createdAt, &updatedAt, &closedAt, &closeReason); err != nil {
			return nil, fmt.Errorf("failed to scan ready writ: %w", err)
		}
		w.Description = desc.String
		w.Assignee = assignee.String
		w.ParentID = parentID.String
		w.CloseReason = closeReason.String
		if metadataRaw.Valid {
			if err := json.Unmarshal([]byte(metadataRaw.String), &w.Metadata); err != nil {
				return nil, fmt.Errorf("failed to parse metadata for writ %q: %w", w.ID, err)
			}
		}
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
		return nil, fmt.Errorf("failed iterating ready writs: %w", err)
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
