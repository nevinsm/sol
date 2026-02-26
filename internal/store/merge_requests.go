package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// MergeRequest represents a merge request in the rig database.
type MergeRequest struct {
	ID         string
	WorkItemID string
	Branch     string
	Phase      string // ready, claimed, merged, failed
	ClaimedBy  string // refinery agent ID (empty if unclaimed)
	ClaimedAt  *time.Time
	Attempts   int
	Priority   int
	CreatedAt  time.Time
	UpdatedAt  time.Time
	MergedAt   *time.Time
}

// generateMRID returns a new merge request ID in the format "mr-" + 8 hex chars.
func generateMRID() (string, error) {
	b := make([]byte, 4)
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
	var claimedBy sql.NullString
	var claimedAt, mergedAt sql.NullString
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		`SELECT id, work_item_id, branch, phase, claimed_by, claimed_at,
		        attempts, priority, created_at, updated_at, merged_at
		 FROM merge_requests WHERE id = ?`, id,
	).Scan(&mr.ID, &mr.WorkItemID, &mr.Branch, &mr.Phase, &claimedBy, &claimedAt,
		&mr.Attempts, &mr.Priority, &createdAt, &updatedAt, &mergedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("merge request %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get merge request %q: %w", id, err)
	}

	mr.ClaimedBy = claimedBy.String
	mr.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	mr.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	if claimedAt.Valid {
		t, _ := time.Parse(time.RFC3339, claimedAt.String)
		mr.ClaimedAt = &t
	}
	if mergedAt.Valid {
		t, _ := time.Parse(time.RFC3339, mergedAt.String)
		mr.MergedAt = &t
	}
	return mr, nil
}

// ListMergeRequests returns merge requests filtered by phase.
// If phase is empty, returns all. Ordered by priority ASC, created_at ASC
// (highest priority first, oldest first within same priority).
func (s *Store) ListMergeRequests(phase string) ([]MergeRequest, error) {
	query := `SELECT id, work_item_id, branch, phase, claimed_by, claimed_at,
	                 attempts, priority, created_at, updated_at, merged_at
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
		var claimedBy sql.NullString
		var claimedAt, mergedAt sql.NullString
		var createdAt, updatedAt string

		if err := rows.Scan(&mr.ID, &mr.WorkItemID, &mr.Branch, &mr.Phase, &claimedBy, &claimedAt,
			&mr.Attempts, &mr.Priority, &createdAt, &updatedAt, &mergedAt); err != nil {
			return nil, fmt.Errorf("failed to scan merge request: %w", err)
		}
		mr.ClaimedBy = claimedBy.String
		mr.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		mr.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		if claimedAt.Valid {
			t, _ := time.Parse(time.RFC3339, claimedAt.String)
			mr.ClaimedAt = &t
		}
		if mergedAt.Valid {
			t, _ := time.Parse(time.RFC3339, mergedAt.String)
			mr.MergedAt = &t
		}
		mrs = append(mrs, mr)
	}
	return mrs, nil
}

// ClaimMergeRequest atomically claims the next ready merge request.
// Sets phase=claimed, claimed_by=claimerID, claimed_at=now, attempts++.
// Returns the claimed MR, or nil if no ready MRs exist.
// Uses a single UPDATE ... WHERE to prevent races.
func (s *Store) ClaimMergeRequest(claimerID string) (*MergeRequest, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	mr := &MergeRequest{}
	var claimedBy sql.NullString
	var claimedAt, mergedAt sql.NullString
	var createdAt, updatedAt string

	err := s.db.QueryRow(
		`UPDATE merge_requests
		 SET phase = 'claimed', claimed_by = ?, claimed_at = ?,
		     attempts = attempts + 1, updated_at = ?
		 WHERE id = (
		     SELECT id FROM merge_requests
		     WHERE phase = 'ready'
		     ORDER BY priority ASC, created_at ASC
		     LIMIT 1
		 )
		 RETURNING id, work_item_id, branch, phase, claimed_by, claimed_at,
		           attempts, priority, created_at, updated_at, merged_at`,
		claimerID, now, now,
	).Scan(&mr.ID, &mr.WorkItemID, &mr.Branch, &mr.Phase, &claimedBy, &claimedAt,
		&mr.Attempts, &mr.Priority, &createdAt, &updatedAt, &mergedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to claim merge request: %w", err)
	}

	mr.ClaimedBy = claimedBy.String
	mr.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	mr.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	if claimedAt.Valid {
		t, _ := time.Parse(time.RFC3339, claimedAt.String)
		mr.ClaimedAt = &t
	}
	if mergedAt.Valid {
		t, _ := time.Parse(time.RFC3339, mergedAt.String)
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
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("merge request %q not found", id)
	}
	return nil
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
	n, _ := result.RowsAffected()
	return int(n), nil
}
