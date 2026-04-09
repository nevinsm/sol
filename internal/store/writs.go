package store

import (
	"database/sql"
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
	Status   string   // empty = all; if Statuses is also set, Statuses takes precedence
	Statuses []string // filter to any of these statuses; empty = all
	Assignee string   // empty = all
	Label    string   // empty = all
	Priority int      // 0 = all
	ParentID string   // empty = all
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
	return generatePrefixedID("sol-")
}

// CreateWrit creates a new writ and returns its generated ID.
func (s *WorldStore) CreateWrit(title, description, createdBy string, priority int, labels []string) (string, error) {
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
func (s *WorldStore) CreateWritWithOpts(opts CreateWritOpts) (string, error) {
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
func (s *WorldStore) GetWrit(id string) (*Writ, error) {
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
	if w.CreatedAt, err = parseRFC3339(createdAt, "created_at", "writ "+id); err != nil {
		return nil, err
	}
	if w.UpdatedAt, err = parseRFC3339(updatedAt, "updated_at", "writ "+id); err != nil {
		return nil, err
	}
	if w.ClosedAt, err = parseOptionalRFC3339(closedAt, "closed_at", "writ "+id); err != nil {
		return nil, err
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
func (s *WorldStore) ListWrits(filters ListFilters) ([]Writ, error) {
	query := `SELECT DISTINCT w.id, w.title, w.description, w.status, w.priority, w.assignee, w.parent_id, w.kind, w.metadata, w.created_by, w.created_at, w.updated_at, w.closed_at, w.close_reason
	           FROM writs w`
	var conditions []string
	var args []interface{}

	if filters.Label != "" {
		query += ` JOIN labels l ON w.id = l.writ_id`
		conditions = append(conditions, "l.label = ?")
		args = append(args, filters.Label)
	}
	if len(filters.Statuses) > 0 {
		placeholders := strings.Repeat("?,", len(filters.Statuses))
		placeholders = placeholders[:len(placeholders)-1] // trim trailing comma
		conditions = append(conditions, "w.status IN ("+placeholders+")")
		for _, s := range filters.Statuses {
			args = append(args, s)
		}
	} else if filters.Status != "" {
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
		if w.CreatedAt, parseErr = parseRFC3339(createdAt, "created_at", "writ "+w.ID); parseErr != nil {
			return nil, parseErr
		}
		if w.UpdatedAt, parseErr = parseRFC3339(updatedAt, "updated_at", "writ "+w.ID); parseErr != nil {
			return nil, parseErr
		}
		if w.ClosedAt, parseErr = parseOptionalRFC3339(closedAt, "closed_at", "writ "+w.ID); parseErr != nil {
			return nil, parseErr
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
func (s *WorldStore) ListChildWrits(parentID string) ([]Writ, error) {
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

// terminalWritStatuses is the set of writ statuses that represent finished
// work. Once a writ enters one of these states it should not be silently
// reverted to "open" by recovery tooling — the writ's status and assignee
// fields are part of its historical record.
//
// "done" means the agent has finished the work and the writ is awaiting
// forge merge. "closed" means the writ is terminated (merged, superseded,
// or manually closed via sol writ close).
var terminalWritStatuses = map[string]bool{
	"done":   true,
	"closed": true,
}

// IsTerminalStatus reports whether the given writ status represents a
// terminal (finished) state. Callers performing recovery or cleanup
// operations should use this to avoid clobbering completed work.
func IsTerminalStatus(status string) bool {
	return terminalWritStatuses[status]
}

// validWritTransitions maps each source status to its allowed target statuses.
// Self-transitions are included (idempotent).
var validWritTransitions = map[string]map[string]bool{
	"open":     {"open": true, "tethered": true, "working": true, "done": true, "closed": true},
	"tethered": {"tethered": true, "working": true, "done": true, "open": true, "closed": true},
	"working":  {"working": true, "done": true, "resolve": true, "open": true, "tethered": true, "closed": true},
	"resolve":  {"resolve": true, "done": true, "open": true, "closed": true},
	"done":     {"done": true, "closed": true, "open": true, "tethered": true},
	"closed":   {"closed": true, "open": true},
}

// allowedSourcesForTarget returns the set of statuses that can transition to the given target.
func allowedSourcesForTarget(target string) []string {
	var sources []string
	for from, targets := range validWritTransitions {
		if targets[target] {
			sources = append(sources, from)
		}
	}
	return sources
}

// UpdateWrit updates fields on a writ. Only non-zero fields are applied.
// When Status is being changed, the transition is validated against
// validWritTransitions and enforced atomically in the SQL WHERE clause.
// Returns ErrInvalidTransition if the status transition is not allowed.
func (s *WorldStore) UpdateWrit(id string, updates WritUpdates) error {
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

	// When a status change is requested, encode the allowed source statuses
	// in the WHERE clause to make the transition validation atomic.
	var whereClause string
	if updates.Status != "" {
		sources := allowedSourcesForTarget(updates.Status)
		placeholders := make([]string, len(sources))
		for i, src := range sources {
			placeholders[i] = "?"
			args = append(args, src)
		}
		whereClause = fmt.Sprintf("UPDATE writs SET %s WHERE id = ? AND status IN (%s)",
			strings.Join(sets, ", "), strings.Join(placeholders, ", "))
	} else {
		whereClause = fmt.Sprintf("UPDATE writs SET %s WHERE id = ?", strings.Join(sets, ", "))
	}

	result, err := s.db.Exec(whereClause, args...)
	if err != nil {
		return fmt.Errorf("failed to update writ %q: %w", id, err)
	}

	n, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("failed to check rows affected: %w", raErr)
	}
	if n == 0 {
		if updates.Status != "" {
			// Distinguish not-found from invalid-transition.
			var exists int
			if err := s.db.QueryRow(`SELECT 1 FROM writs WHERE id = ?`, id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("writ %q: %w", id, ErrNotFound)
			}
			// Row exists but its status was not in the allowed source set.
			var currentStatus string
			if diagErr := s.db.QueryRow(`SELECT status FROM writs WHERE id = ?`, id).Scan(&currentStatus); diagErr != nil {
				currentStatus = "(unknown)"
			}
			return fmt.Errorf("writ %q: cannot transition from %q to %q: %w",
				id, currentStatus, updates.Status, ErrInvalidTransition)
		}
		return fmt.Errorf("writ %q: %w", id, ErrNotFound)
	}
	return nil
}

// CloseWrit sets status to "closed" and records closed_at.
// An optional close reason can be provided as the second argument.
// Also supersedes any failed MRs for the writ, returning their IDs.
// Both mutations are wrapped in a single transaction so a crash between them
// cannot leave a closed writ with orphaned failed MRs.
func (s *WorldStore) CloseWrit(id string, closeReason ...string) ([]string, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Only close writs that are in a non-closed state. This prevents
	// double-close races (e.g., concurrent MarkMerged calls).
	var result sql.Result
	if len(closeReason) > 0 && closeReason[0] != "" {
		result, err = tx.Exec(
			`UPDATE writs SET status = 'closed', closed_at = ?, close_reason = ?, updated_at = ?
			 WHERE id = ? AND status IN ('open', 'tethered', 'working', 'resolve', 'done')`,
			now, closeReason[0], now, id,
		)
	} else {
		result, err = tx.Exec(
			`UPDATE writs SET status = 'closed', closed_at = ?, updated_at = ?
			 WHERE id = ? AND status IN ('open', 'tethered', 'working', 'resolve', 'done')`,
			now, now, id,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to close writ %q: %w", id, err)
	}
	n, raErr := result.RowsAffected()
	if raErr != nil {
		return nil, fmt.Errorf("failed to check rows affected: %w", raErr)
	}
	if n == 0 {
		// Distinguish not-found from already-closed.
		var exists int
		if scanErr := tx.QueryRow(`SELECT 1 FROM writs WHERE id = ?`, id).Scan(&exists); errors.Is(scanErr, sql.ErrNoRows) {
			return nil, fmt.Errorf("writ %q: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("writ %q: cannot close from current status: %w", id, ErrInvalidTransition)
	}

	// Supersede any failed MRs for this writ within the same transaction.
	superseded, err := supersedeFailedMRsInTx(tx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to supersede failed MRs for writ %q: %w", id, err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit close writ transaction: %w", err)
	}

	return superseded, nil
}

// GetWritMetadata returns the metadata for a writ.
func (s *WorldStore) GetWritMetadata(id string) (map[string]any, error) {
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
// Uses a single atomic json_patch() statement to avoid read-modify-write races.
func (s *WorldStore) SetWritMetadata(id string, metadata map[string]any) error {
	// Marshal the patch. json_patch treats JSON null values as deletions,
	// which matches our "nil value = delete key" contract.
	patch, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("invalid metadata: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(
		`UPDATE writs SET metadata = json_patch(COALESCE(metadata, '{}'), ?), updated_at = ? WHERE id = ?`,
		string(patch), now, id,
	)
	if err != nil {
		return fmt.Errorf("failed to set metadata for writ %q: %w", id, err)
	}
	return checkRowsAffected(result, "writ", id)
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
func (s *WorldStore) ReadyWrits() ([]Writ, error) {
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
		if w.CreatedAt, parseErr = parseRFC3339(createdAt, "created_at", "writ "+w.ID); parseErr != nil {
			return nil, parseErr
		}
		if w.UpdatedAt, parseErr = parseRFC3339(updatedAt, "updated_at", "writ "+w.ID); parseErr != nil {
			return nil, parseErr
		}
		if w.ClosedAt, parseErr = parseOptionalRFC3339(closedAt, "closed_at", "writ "+w.ID); parseErr != nil {
			return nil, parseErr
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
func (s *WorldStore) AddLabel(itemID, label string) error {
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
func (s *WorldStore) RemoveLabel(itemID, label string) error {
	_, err := s.db.Exec(
		`DELETE FROM labels WHERE writ_id = ? AND label = ?`,
		itemID, label,
	)
	if err != nil {
		return fmt.Errorf("failed to remove label %q from writ %q: %w", label, itemID, err)
	}
	return nil
}
