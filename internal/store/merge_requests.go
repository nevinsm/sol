package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CaravanBlockedSentinel is the blocked_by value used when an MR is blocked
// by caravan-level dependencies. Using a sentinel lets the claim SQL
// (blocked_by IS NULL) naturally exclude caravan-blocked MRs.
const CaravanBlockedSentinel = "caravan-blocked"

// validMRTransition returns true if transitioning from → to is allowed.
// Terminal states (merged, superseded) reject all outgoing transitions.
// Same-phase transitions are always allowed (idempotent no-op).
func validMRTransition(from, to string) bool {
	if from == to {
		return true // idempotent
	}
	switch from {
	case "merged", "superseded":
		return false // terminal states
	case "ready":
		// ready → claimed is handled by ClaimMergeRequest (separate SQL).
		// ready → blocked is handled by BlockMergeRequest (separate method).
		// ready → merged or ready → failed skip the claimed step.
		return to == "claimed"
	case "claimed":
		return to == "ready" || to == "merged" || to == "failed"
	case "failed":
		// failed → ready must go through ResetMergeRequestForRetry.
		// failed → claimed must go through ready first.
		return to == "superseded"
	default:
		return false
	}
}

// MergeRequest represents a merge request in the world database.
type MergeRequest struct {
	ID         string
	WritID string
	Branch     string
	Phase      string // ready, claimed, merged, failed, superseded
	ClaimedBy  string // forge agent ID (empty if unclaimed)
	ClaimedAt  *time.Time
	Attempts   int
	Priority   int
	BlockedBy  string // writ ID blocking this MR (empty = not blocked)
	CreatedAt  time.Time
	UpdatedAt  time.Time
	MergedAt   *time.Time
}

// generateMRID returns a new merge request ID in the format "mr-" + 16 hex chars.
func generateMRID() (string, error) {
	return generatePrefixedID("mr-")
}

// scanner is an interface satisfied by both *sql.Row and *sql.Rows,
// allowing scanMergeRequest to work with QueryRow and Query results.
type scanner interface {
	Scan(dest ...any) error
}

// scanMergeRequest scans a single MergeRequest row from the given scanner,
// parsing nullable fields and timestamps. Returns the raw scan error (if any)
// so callers can check for sql.ErrNoRows.
func scanMergeRequest(s scanner) (*MergeRequest, error) {
	mr := &MergeRequest{}
	var claimedBy, blockedBy sql.NullString
	var claimedAt, mergedAt sql.NullString
	var createdAt, updatedAt string

	if err := s.Scan(&mr.ID, &mr.WritID, &mr.Branch, &mr.Phase, &claimedBy, &claimedAt,
		&mr.Attempts, &mr.Priority, &blockedBy, &createdAt, &updatedAt, &mergedAt); err != nil {
		return nil, err
	}

	mr.ClaimedBy = claimedBy.String
	mr.BlockedBy = blockedBy.String

	var err error
	if mr.CreatedAt, err = parseRFC3339(createdAt, "created_at", "merge request "+mr.ID); err != nil {
		return nil, err
	}
	if mr.UpdatedAt, err = parseRFC3339(updatedAt, "updated_at", "merge request "+mr.ID); err != nil {
		return nil, err
	}
	if mr.ClaimedAt, err = parseOptionalRFC3339(claimedAt, "claimed_at", "merge request "+mr.ID); err != nil {
		return nil, err
	}
	if mr.MergedAt, err = parseOptionalRFC3339(mergedAt, "merged_at", "merge request "+mr.ID); err != nil {
		return nil, err
	}
	return mr, nil
}

// CreateMergeRequest creates a new merge request with phase=ready.
// Returns the generated MR ID (mr-XXXXXXXX).
func (s *WorldStore) CreateMergeRequest(writID, branch string, priority int) (string, error) {
	id, err := generateMRID()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = s.db.Exec(
		`INSERT INTO merge_requests (id, writ_id, branch, phase, priority, created_at, updated_at)
		 VALUES (?, ?, ?, 'ready', ?, ?, ?)`,
		id, writID, branch, priority, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create merge request: %w", err)
	}
	return id, nil
}

// GetMergeRequest returns a merge request by ID.
func (s *WorldStore) GetMergeRequest(id string) (*MergeRequest, error) {
	mr, err := scanMergeRequest(s.db.QueryRow(
		`SELECT id, writ_id, branch, phase, claimed_by, claimed_at,
		        attempts, priority, blocked_by, created_at, updated_at, merged_at
		 FROM merge_requests WHERE id = ?`, id,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("merge request %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get merge request %q: %w", id, err)
	}
	return mr, nil
}

// ListMergeRequests returns merge requests filtered by phase.
// If phase is empty, returns all. Ordered by priority ASC, created_at ASC
// (highest priority first, oldest first within same priority).
func (s *WorldStore) ListMergeRequests(phase string) ([]MergeRequest, error) {
	query := `SELECT id, writ_id, branch, phase, claimed_by, claimed_at,
	                 attempts, priority, blocked_by, created_at, updated_at, merged_at
	          FROM merge_requests`
	var args []interface{}
	if phase != "" {
		query += " WHERE phase = ?"
		args = append(args, phase)
	}
	query += " ORDER BY priority ASC, created_at ASC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list merge requests: %w", err)
	}
	defer rows.Close()

	var mrs []MergeRequest
	for rows.Next() {
		mr, err := scanMergeRequest(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan merge request: %w", err)
		}
		mrs = append(mrs, *mr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating merge requests: %w", err)
	}
	return mrs, nil
}

// ListMergeRequestsByWrit returns merge requests for a given writ,
// optionally filtered by phase. If phase is empty, returns all phases.
func (s *WorldStore) ListMergeRequestsByWrit(writID, phase string) ([]MergeRequest, error) {
	query := `SELECT id, writ_id, branch, phase, claimed_by, claimed_at,
	                 attempts, priority, blocked_by, created_at, updated_at, merged_at
	          FROM merge_requests WHERE writ_id = ?`
	args := []interface{}{writID}
	if phase != "" {
		query += " AND phase = ?"
		args = append(args, phase)
	}
	query += " ORDER BY created_at ASC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list merge requests for writ %q: %w", writID, err)
	}
	defer rows.Close()

	var mrs []MergeRequest
	for rows.Next() {
		mr, err := scanMergeRequest(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan merge request: %w", err)
		}
		mrs = append(mrs, *mr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating merge requests: %w", err)
	}
	return mrs, nil
}

// ClaimMergeRequest atomically claims the next ready merge request.
// Sets phase=claimed, claimed_by=claimerID, claimed_at=now, attempts++.
// Returns the claimed MR, or nil if no ready MRs exist.
// Blocked MRs (blocked_by IS NOT NULL) are never claimed.
// Uses a single UPDATE ... WHERE to prevent races.
func (s *WorldStore) ClaimMergeRequest(claimerID string) (*MergeRequest, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	mr, err := scanMergeRequest(s.db.QueryRow(
		`UPDATE merge_requests
		 SET phase = 'claimed', claimed_by = ?, claimed_at = ?,
		     attempts = attempts + 1, updated_at = ?
		 WHERE id = (
		     SELECT id FROM merge_requests
		     WHERE phase = 'ready' AND blocked_by IS NULL
		     ORDER BY priority ASC, created_at ASC
		     LIMIT 1
		 )
		 RETURNING id, writ_id, branch, phase, claimed_by, claimed_at,
		           attempts, priority, blocked_by, created_at, updated_at, merged_at`,
		claimerID, now, now,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to claim merge request: %w", err)
	}
	return mr, nil
}

// UpdateMergeRequestPhase updates the phase of a merge request.
// Also sets updated_at=now. If phase=merged, also sets merged_at=now.
// If phase=ready, clears claimed_by and claimed_at (release).
// Returns ErrInvalidTransition if the transition is not allowed.
//
// Uses a single atomic UPDATE with the transition validation encoded in the
// WHERE clause, avoiding a TOCTOU race between reading and writing the phase.
func (s *WorldStore) UpdateMergeRequestPhase(id, phase string) error {
	validPhases := map[string]bool{"ready": true, "claimed": true, "merged": true, "failed": true, "superseded": true}
	if !validPhases[phase] {
		return fmt.Errorf("invalid merge request phase %q", phase)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Encode the allowed source phases for this transition in the WHERE clause,
	// making the phase validation and update a single atomic operation.
	// Same-phase (idempotent) is always included per validMRTransition.
	//
	// valid source phases per target:
	//   ready      ← claimed, ready
	//   claimed    ← ready, claimed
	//   merged     ← claimed, merged
	//   failed     ← claimed, failed
	//   superseded ← failed, superseded
	var from1, from2 string
	switch phase {
	case "ready":
		from1, from2 = "claimed", "ready"
	case "claimed":
		from1, from2 = "ready", "claimed"
	case "merged":
		from1, from2 = "claimed", "merged"
	case "failed":
		from1, from2 = "claimed", "failed"
	case "superseded":
		from1, from2 = "failed", "superseded"
	}

	var result sql.Result
	var err error

	switch phase {
	case "merged":
		// Preserve merged_at if already set (idempotent case).
		result, err = s.db.Exec(
			`UPDATE merge_requests
			 SET phase = ?, merged_at = COALESCE(merged_at, ?), updated_at = ?
			 WHERE id = ? AND phase IN (?, ?)`,
			phase, now, now, id, from1, from2,
		)
	case "ready":
		result, err = s.db.Exec(
			`UPDATE merge_requests
			 SET phase = ?, claimed_by = NULL, claimed_at = NULL, updated_at = ?
			 WHERE id = ? AND phase IN (?, ?)`,
			phase, now, id, from1, from2,
		)
	default:
		result, err = s.db.Exec(
			`UPDATE merge_requests SET phase = ?, updated_at = ?
			 WHERE id = ? AND phase IN (?, ?)`,
			phase, now, id, from1, from2,
		)
	}

	if err != nil {
		return fmt.Errorf("failed to update merge request %q: %w", id, err)
	}

	n, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("failed to check rows affected: %w", raErr)
	}
	if n == 0 {
		// Distinguish not-found from invalid-transition.
		var exists int
		if err := s.db.QueryRow(`SELECT 1 FROM merge_requests WHERE id = ?`, id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("merge request %q: %w", id, ErrNotFound)
		}
		// Row exists but its phase was not in the allowed source set.
		var currentPhase string
		_ = s.db.QueryRow(`SELECT phase FROM merge_requests WHERE id = ?`, id).Scan(&currentPhase)
		return fmt.Errorf("merge request %q: cannot transition from %q to %q: %w",
			id, currentPhase, phase, ErrInvalidTransition)
	}
	return nil
}

// BlockMergeRequest sets blocked_by on a merge request and ensures phase=ready.
// A blocked MR is skipped during claiming.
func (s *WorldStore) BlockMergeRequest(mrID, blockerWritID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(
		`UPDATE merge_requests SET blocked_by = ?, phase = 'ready',
		        claimed_by = NULL, claimed_at = NULL, updated_at = ?
		 WHERE id = ?`,
		blockerWritID, now, mrID,
	)
	if err != nil {
		return fmt.Errorf("failed to block merge request %q: %w", mrID, err)
	}
	return checkRowsAffected(result, "merge request", mrID)
}

// UnblockMergeRequest clears blocked_by and ensures phase=ready.
func (s *WorldStore) UnblockMergeRequest(mrID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(
		`UPDATE merge_requests SET blocked_by = NULL, phase = 'ready', updated_at = ?
		 WHERE id = ?`,
		now, mrID,
	)
	if err != nil {
		return fmt.Errorf("failed to unblock merge request %q: %w", mrID, err)
	}
	return checkRowsAffected(result, "merge request", mrID)
}

// FindMergeRequestByBlocker finds the MR blocked by a given writ ID.
// Returns nil if no MR is blocked by the given writ.
func (s *WorldStore) FindMergeRequestByBlocker(blockerID string) (*MergeRequest, error) {
	mr, err := scanMergeRequest(s.db.QueryRow(
		`SELECT id, writ_id, branch, phase, claimed_by, claimed_at,
		        attempts, priority, blocked_by, created_at, updated_at, merged_at
		 FROM merge_requests WHERE blocked_by = ?`, blockerID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find merge request blocked by %q: %w", blockerID, err)
	}
	return mr, nil
}

// ListBlockedMergeRequests returns all merge requests that have a non-empty
// blocked_by field, ordered by creation time.
func (s *WorldStore) ListBlockedMergeRequests() ([]MergeRequest, error) {
	rows, err := s.db.Query(
		`SELECT id, writ_id, branch, phase, claimed_by, claimed_at,
		        attempts, priority, blocked_by, created_at, updated_at, merged_at
		 FROM merge_requests
		 WHERE blocked_by IS NOT NULL AND blocked_by != ''
		 ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("failed to list blocked merge requests: %w", err)
	}
	defer rows.Close()

	var mrs []MergeRequest
	for rows.Next() {
		mr, err := scanMergeRequest(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan blocked merge request: %w", err)
		}
		mrs = append(mrs, *mr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating blocked merge requests: %w", err)
	}
	return mrs, nil
}

// ReleaseStaleClaims releases merge requests that have been claimed for
// longer than the given TTL. Sets them back to phase=ready, clears
// claimed_by and claimed_at. Returns the number of released MRs.
func (s *WorldStore) ReleaseStaleClaims(ttl time.Duration) (int, error) {
	now := time.Now().UTC()
	threshold := now.Add(-ttl).Format(time.RFC3339)
	nowStr := now.Format(time.RFC3339)

	result, err := s.db.Exec(
		`UPDATE merge_requests
		 SET phase = 'ready', claimed_by = NULL, claimed_at = NULL, updated_at = ?
		 WHERE phase = 'claimed' AND claimed_at < ?`,
		nowStr, threshold,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to release stale claims: %w", err)
	}
	// RowsAffected error is unlikely with modernc.org/sqlite but check defensively.
	n, raErr := result.RowsAffected()
	if raErr != nil {
		return 0, fmt.Errorf("failed to check rows affected: %w", raErr)
	}
	return int(n), nil
}

// ResetMergeRequestForRetry resets a merge request for retry after conflict
// resolution: sets phase to ready, resets attempts to 0, and clears
// blocked_by, claimed_by, and claimed_at.
func (s *WorldStore) ResetMergeRequestForRetry(mrID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(
		`UPDATE merge_requests
		 SET phase = 'ready', attempts = 0, blocked_by = NULL,
		     claimed_by = NULL, claimed_at = NULL, updated_at = ?
		 WHERE id = ?`,
		now, mrID,
	)
	if err != nil {
		return fmt.Errorf("failed to reset merge request %q for retry: %w", mrID, err)
	}
	return checkRowsAffected(result, "merge request", mrID)
}

// supersedeFailedMRsInTx marks all failed MRs for a writ as superseded within
// an existing transaction, returning the IDs of superseded MRs.
func supersedeFailedMRsInTx(tx *sql.Tx, writID string) ([]string, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	rows, err := tx.Query(
		`SELECT id FROM merge_requests WHERE writ_id = ? AND phase = 'failed'`,
		writID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list failed MRs for writ %q: %w", writID, err)
	}

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan MR ID: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating MR IDs: %w", err)
	}

	if len(ids) == 0 {
		return nil, nil
	}

	for _, id := range ids {
		_, err := tx.Exec(
			`UPDATE merge_requests SET phase = 'superseded', updated_at = ? WHERE id = ?`,
			now, id,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to supersede MR %q: %w", id, err)
		}
	}

	return ids, nil
}

// SupersedeFailedMRsForWrit atomically marks all failed MRs for a writ as
// superseded, returning the IDs of superseded MRs. Uses a transaction to
// ensure the IDs returned match the rows actually updated.
func (s *WorldStore) SupersedeFailedMRsForWrit(writID string) ([]string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	ids, err := supersedeFailedMRsInTx(tx, writID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit supersede transaction: %w", err)
	}

	return ids, nil
}
