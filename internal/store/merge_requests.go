package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// MergeRequest represents a merge request in the world database.
type MergeRequest struct {
	ID         string
	WorkItemID string
	Branch     string
	Phase      string // ready, claimed, merged, failed
	ClaimedBy  string // forge agent ID (empty if unclaimed)
	ClaimedAt  *time.Time
	Attempts   int
	Priority   int
	BlockedBy  string // work item ID blocking this MR (empty = not blocked)
	CreatedAt  time.Time
	UpdatedAt  time.Time
	MergedAt   *time.Time
}

// generateMRID returns a new merge request ID in the format "mr-" + 16 hex chars.
func generateMRID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate merge request ID: %w", err)
	}
	return "mr-" + hex.EncodeToString(b), nil
}

// CreateMergeRequest creates a new merge request with phase=ready.
// Returns the generated MR ID (mr-XXXXXXXX).
func (s *Store) CreateMergeRequest(workItemID, branch string, priority int) (string, error) {
	id, err := generateMRID()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = s.db.Exec(
		`INSERT INTO merge_requests (id, work_item_id, branch, phase, priority, created_at, updated_at)
		 VALUES (?, ?, ?, 'ready', ?, ?, ?)`,
		id, workItemID, branch, priority, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create merge request: %w", err)
	}
	return id, nil
}

// GetMergeRequest returns a merge request by ID.
func (s *Store) GetMergeRequest(id string) (*MergeRequest, error) {
	mr := &MergeRequest{}
	var claimedBy, blockedBy sql.NullString
	var claimedAt, mergedAt sql.NullString
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		`SELECT id, work_item_id, branch, phase, claimed_by, claimed_at,
		        attempts, priority, blocked_by, created_at, updated_at, merged_at
		 FROM merge_requests WHERE id = ?`, id,
	).Scan(&mr.ID, &mr.WorkItemID, &mr.Branch, &mr.Phase, &claimedBy, &claimedAt,
		&mr.Attempts, &mr.Priority, &blockedBy, &createdAt, &updatedAt, &mergedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("merge request %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get merge request %q: %w", id, err)
	}

	mr.ClaimedBy = claimedBy.String
	mr.BlockedBy = blockedBy.String
	mr.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at for merge request %q: %w", id, err)
	}
	mr.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse updated_at for merge request %q: %w", id, err)
	}
	if claimedAt.Valid {
		t, err := time.Parse(time.RFC3339, claimedAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse claimed_at for merge request %q: %w", id, err)
		}
		mr.ClaimedAt = &t
	}
	if mergedAt.Valid {
		t, err := time.Parse(time.RFC3339, mergedAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse merged_at for merge request %q: %w", id, err)
		}
		mr.MergedAt = &t
	}
	return mr, nil
}

// ListMergeRequests returns merge requests filtered by phase.
// If phase is empty, returns all. Ordered by priority ASC, created_at ASC
// (highest priority first, oldest first within same priority).
func (s *Store) ListMergeRequests(phase string) ([]MergeRequest, error) {
	query := `SELECT id, work_item_id, branch, phase, claimed_by, claimed_at,
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
		var mr MergeRequest
		var claimedBy, blockedBy sql.NullString
		var claimedAt, mergedAt sql.NullString
		var createdAt, updatedAt string

		if err := rows.Scan(&mr.ID, &mr.WorkItemID, &mr.Branch, &mr.Phase, &claimedBy, &claimedAt,
			&mr.Attempts, &mr.Priority, &blockedBy, &createdAt, &updatedAt, &mergedAt); err != nil {
			return nil, fmt.Errorf("failed to scan merge request: %w", err)
		}
		mr.ClaimedBy = claimedBy.String
		mr.BlockedBy = blockedBy.String
		var parseErr error
		mr.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse created_at for merge request %q: %w", mr.ID, parseErr)
		}
		mr.UpdatedAt, parseErr = time.Parse(time.RFC3339, updatedAt)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse updated_at for merge request %q: %w", mr.ID, parseErr)
		}
		if claimedAt.Valid {
			t, parseErr := time.Parse(time.RFC3339, claimedAt.String)
			if parseErr != nil {
				return nil, fmt.Errorf("failed to parse claimed_at for merge request %q: %w", mr.ID, parseErr)
			}
			mr.ClaimedAt = &t
		}
		if mergedAt.Valid {
			t, parseErr := time.Parse(time.RFC3339, mergedAt.String)
			if parseErr != nil {
				return nil, fmt.Errorf("failed to parse merged_at for merge request %q: %w", mr.ID, parseErr)
			}
			mr.MergedAt = &t
		}
		mrs = append(mrs, mr)
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
func (s *Store) ClaimMergeRequest(claimerID string) (*MergeRequest, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	mr := &MergeRequest{}
	var claimedBy, blockedBy sql.NullString
	var claimedAt, mergedAt sql.NullString
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		`UPDATE merge_requests
		 SET phase = 'claimed', claimed_by = ?, claimed_at = ?,
		     attempts = attempts + 1, updated_at = ?
		 WHERE id = (
		     SELECT id FROM merge_requests
		     WHERE phase = 'ready' AND blocked_by IS NULL
		     ORDER BY priority ASC, created_at ASC
		     LIMIT 1
		 )
		 RETURNING id, work_item_id, branch, phase, claimed_by, claimed_at,
		           attempts, priority, blocked_by, created_at, updated_at, merged_at`,
		claimerID, now, now,
	).Scan(&mr.ID, &mr.WorkItemID, &mr.Branch, &mr.Phase, &claimedBy, &claimedAt,
		&mr.Attempts, &mr.Priority, &blockedBy, &createdAt, &updatedAt, &mergedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to claim merge request: %w", err)
	}

	mr.ClaimedBy = claimedBy.String
	mr.BlockedBy = blockedBy.String
	mr.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at for merge request %q: %w", mr.ID, err)
	}
	mr.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse updated_at for merge request %q: %w", mr.ID, err)
	}
	if claimedAt.Valid {
		t, err := time.Parse(time.RFC3339, claimedAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse claimed_at for merge request %q: %w", mr.ID, err)
		}
		mr.ClaimedAt = &t
	}
	if mergedAt.Valid {
		t, err := time.Parse(time.RFC3339, mergedAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse merged_at for merge request %q: %w", mr.ID, err)
		}
		mr.MergedAt = &t
	}
	return mr, nil
}

// UpdateMergeRequestPhase updates the phase of a merge request.
// Also sets updated_at=now. If phase=merged, also sets merged_at=now.
// If phase=ready, clears claimed_by and claimed_at (release).
func (s *Store) UpdateMergeRequestPhase(id, phase string) error {
	validPhases := map[string]bool{"ready": true, "claimed": true, "merged": true, "failed": true}
	if !validPhases[phase] {
		return fmt.Errorf("invalid merge request phase %q", phase)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	var result sql.Result
	var err error

	switch phase {
	case "merged":
		result, err = s.db.Exec(
			`UPDATE merge_requests SET phase = ?, merged_at = ?, updated_at = ? WHERE id = ?`,
			phase, now, now, id,
		)
	case "ready":
		result, err = s.db.Exec(
			`UPDATE merge_requests SET phase = ?, claimed_by = NULL, claimed_at = NULL, updated_at = ? WHERE id = ?`,
			phase, now, id,
		)
	default:
		result, err = s.db.Exec(
			`UPDATE merge_requests SET phase = ?, updated_at = ? WHERE id = ?`,
			phase, now, id,
		)
	}

	if err != nil {
		return fmt.Errorf("failed to update merge request %q: %w", id, err)
	}
	// RowsAffected error is unlikely with modernc.org/sqlite but check defensively.
	n, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("failed to check rows affected: %w", raErr)
	}
	if n == 0 {
		return fmt.Errorf("merge request %q: %w", id, ErrNotFound)
	}
	return nil
}

// BlockMergeRequest sets blocked_by on a merge request and ensures phase=ready.
// A blocked MR is skipped during claiming.
func (s *Store) BlockMergeRequest(mrID, blockerWorkItemID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(
		`UPDATE merge_requests SET blocked_by = ?, phase = 'ready',
		        claimed_by = NULL, claimed_at = NULL, updated_at = ?
		 WHERE id = ?`,
		blockerWorkItemID, now, mrID,
	)
	if err != nil {
		return fmt.Errorf("failed to block merge request %q: %w", mrID, err)
	}
	// RowsAffected error is unlikely with modernc.org/sqlite but check defensively.
	n, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("failed to check rows affected: %w", raErr)
	}
	if n == 0 {
		return fmt.Errorf("merge request %q: %w", mrID, ErrNotFound)
	}
	return nil
}

// UnblockMergeRequest clears blocked_by and ensures phase=ready.
func (s *Store) UnblockMergeRequest(mrID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(
		`UPDATE merge_requests SET blocked_by = NULL, phase = 'ready', updated_at = ?
		 WHERE id = ?`,
		now, mrID,
	)
	if err != nil {
		return fmt.Errorf("failed to unblock merge request %q: %w", mrID, err)
	}
	// RowsAffected error is unlikely with modernc.org/sqlite but check defensively.
	n, raErr := result.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("failed to check rows affected: %w", raErr)
	}
	if n == 0 {
		return fmt.Errorf("merge request %q: %w", mrID, ErrNotFound)
	}
	return nil
}

// FindMergeRequestByBlocker finds the MR blocked by a given work item ID.
// Returns nil if no MR is blocked by the given work item.
func (s *Store) FindMergeRequestByBlocker(blockerID string) (*MergeRequest, error) {
	mr := &MergeRequest{}
	var claimedBy, blockedBy sql.NullString
	var claimedAt, mergedAt sql.NullString
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		`SELECT id, work_item_id, branch, phase, claimed_by, claimed_at,
		        attempts, priority, blocked_by, created_at, updated_at, merged_at
		 FROM merge_requests WHERE blocked_by = ?`, blockerID,
	).Scan(&mr.ID, &mr.WorkItemID, &mr.Branch, &mr.Phase, &claimedBy, &claimedAt,
		&mr.Attempts, &mr.Priority, &blockedBy, &createdAt, &updatedAt, &mergedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find merge request blocked by %q: %w", blockerID, err)
	}

	mr.ClaimedBy = claimedBy.String
	mr.BlockedBy = blockedBy.String
	mr.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at for merge request %q: %w", mr.ID, err)
	}
	mr.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse updated_at for merge request %q: %w", mr.ID, err)
	}
	if claimedAt.Valid {
		t, err := time.Parse(time.RFC3339, claimedAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse claimed_at for merge request %q: %w", mr.ID, err)
		}
		mr.ClaimedAt = &t
	}
	if mergedAt.Valid {
		t, err := time.Parse(time.RFC3339, mergedAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse merged_at for merge request %q: %w", mr.ID, err)
		}
		mr.MergedAt = &t
	}
	return mr, nil
}

// ReleaseStaleClaims releases merge requests that have been claimed for
// longer than the given TTL. Sets them back to phase=ready, clears
// claimed_by and claimed_at. Returns the number of released MRs.
func (s *Store) ReleaseStaleClaims(ttl time.Duration) (int, error) {
	threshold := time.Now().UTC().Add(-ttl).Format(time.RFC3339)
	now := time.Now().UTC().Format(time.RFC3339)

	result, err := s.db.Exec(
		`UPDATE merge_requests
		 SET phase = 'ready', claimed_by = NULL, claimed_at = NULL, updated_at = ?
		 WHERE phase = 'claimed' AND claimed_at < ?`,
		now, threshold,
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
