package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// HistoryEntry represents an agent lifecycle event in the world database.
type HistoryEntry struct {
	ID          string
	AgentName   string
	WritID  string
	Action      string
	StartedAt   time.Time
	EndedAt     *time.Time
	Summary     string
}

// TokenUsage represents per-model token consumption within a history entry.
type TokenUsage struct {
	ID                  string
	HistoryID           string
	Model               string
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

// TokenSummary holds aggregated token counts for a single model.
type TokenSummary struct {
	Model               string
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

func generateHistoryID() (string, error) {
	return generatePrefixedID("ah-")
}

func generateTokenUsageID() (string, error) {
	return generatePrefixedID("tu-")
}

// WriteHistory inserts an agent_history record and returns its generated ID.
func (s *Store) WriteHistory(agentName, writID, action, summary string, startedAt time.Time, endedAt *time.Time) (string, error) {
	id, err := generateHistoryID()
	if err != nil {
		return "", err
	}

	startStr := startedAt.UTC().Format(time.RFC3339)
	var endStr sql.NullString
	if endedAt != nil {
		endStr = sql.NullString{String: endedAt.UTC().Format(time.RFC3339), Valid: true}
	}

	var writ sql.NullString
	if writID != "" {
		writ = sql.NullString{String: writID, Valid: true}
	}

	var sum sql.NullString
	if summary != "" {
		sum = sql.NullString{String: summary, Valid: true}
	}

	_, err = s.db.Exec(
		`INSERT INTO agent_history (id, agent_name, writ_id, action, started_at, ended_at, summary)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, agentName, writ, action, startStr, endStr, sum,
	)
	if err != nil {
		return "", fmt.Errorf("failed to insert agent history: %w", err)
	}
	return id, nil
}

// GetHistory returns a single agent_history entry by ID.
func (s *Store) GetHistory(id string) (*HistoryEntry, error) {
	h := &HistoryEntry{}
	var writID, summary, endedAt sql.NullString
	var startedAt string

	err := s.db.QueryRow(
		`SELECT id, agent_name, writ_id, action, started_at, ended_at, summary
		 FROM agent_history WHERE id = ?`, id,
	).Scan(&h.ID, &h.AgentName, &writID, &h.Action, &startedAt, &endedAt, &summary)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("agent history %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent history %q: %w", id, err)
	}

	h.WritID = writID.String
	h.Summary = summary.String

	if h.StartedAt, err = parseRFC3339(startedAt, "started_at", "history "+id); err != nil {
		return nil, err
	}
	if h.EndedAt, err = parseOptionalRFC3339(endedAt, "ended_at", "history "+id); err != nil {
		return nil, err
	}
	return h, nil
}

// ListHistory returns agent_history entries filtered by agent name.
// If agentName is empty, all entries are returned.
func (s *Store) ListHistory(agentName string) ([]HistoryEntry, error) {
	query := `SELECT id, agent_name, writ_id, action, started_at, ended_at, summary
	           FROM agent_history`
	var args []interface{}
	if agentName != "" {
		query += ` WHERE agent_name = ?`
		args = append(args, agentName)
	}
	query += ` ORDER BY started_at ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list agent history: %w", err)
	}
	defer rows.Close()

	var entries []HistoryEntry
	for rows.Next() {
		var h HistoryEntry
		var writID, summary, endedAt sql.NullString
		var startedAt string

		if err := rows.Scan(&h.ID, &h.AgentName, &writID, &h.Action, &startedAt, &endedAt, &summary); err != nil {
			return nil, fmt.Errorf("failed to scan agent history: %w", err)
		}

		h.WritID = writID.String
		h.Summary = summary.String

		var parseErr error
		if h.StartedAt, parseErr = parseRFC3339(startedAt, "started_at", "history "+h.ID); parseErr != nil {
			return nil, parseErr
		}
		if h.EndedAt, parseErr = parseOptionalRFC3339(endedAt, "ended_at", "history "+h.ID); parseErr != nil {
			return nil, parseErr
		}
		entries = append(entries, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating agent history: %w", err)
	}
	return entries, nil
}

// EndHistory updates the ended_at timestamp on the most recent open cast record
// for the given writ. Returns the history ID that was updated, or empty
// string if no open record was found (best-effort — no error for missing records).
func (s *Store) EndHistory(writID string) (string, error) {
	var id string
	err := s.db.QueryRow(
		`SELECT id FROM agent_history
		 WHERE writ_id = ? AND action = 'cast' AND ended_at IS NULL
		 ORDER BY started_at DESC LIMIT 1`, writID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to find open history for writ %q: %w", writID, err)
	}

	endStr := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(`UPDATE agent_history SET ended_at = ? WHERE id = ?`, endStr, id)
	if err != nil {
		return "", fmt.Errorf("failed to update ended_at for history %q: %w", id, err)
	}
	return id, nil
}

// WriteTokenUsage inserts a token_usage record and returns its generated ID.
func (s *Store) WriteTokenUsage(historyID, model string, input, output, cacheRead, cacheCreation int64) (string, error) {
	id, err := generateTokenUsageID()
	if err != nil {
		return "", err
	}

	_, err = s.db.Exec(
		`INSERT INTO token_usage (id, history_id, model, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, historyID, model, input, output, cacheRead, cacheCreation,
	)
	if err != nil {
		return "", fmt.Errorf("failed to insert token usage: %w", err)
	}
	return id, nil
}

// TokensForHistory returns aggregated token totals for a single history entry.
func (s *Store) TokensForHistory(historyID string) (*TokenSummary, error) {
	var ts TokenSummary
	err := s.db.QueryRow(
		`SELECT COALESCE(SUM(input_tokens),0),
		        COALESCE(SUM(output_tokens),0),
		        COALESCE(SUM(cache_read_tokens),0),
		        COALESCE(SUM(cache_creation_tokens),0)
		 FROM token_usage WHERE history_id = ?`, historyID,
	).Scan(&ts.InputTokens, &ts.OutputTokens, &ts.CacheReadTokens, &ts.CacheCreationTokens)
	if err != nil {
		return nil, fmt.Errorf("failed to get tokens for history %q: %w", historyID, err)
	}
	total := ts.InputTokens + ts.OutputTokens + ts.CacheReadTokens + ts.CacheCreationTokens
	if total == 0 {
		return nil, nil
	}
	return &ts, nil
}

// AggregateTokens sums token usage across all history entries for an agent,
// grouped by model. Returns per-model totals.
func (s *Store) AggregateTokens(agentName string) ([]TokenSummary, error) {
	rows, err := s.db.Query(
		`SELECT tu.model,
		        SUM(tu.input_tokens),
		        SUM(tu.output_tokens),
		        SUM(tu.cache_read_tokens),
		        SUM(tu.cache_creation_tokens)
		 FROM token_usage tu
		 JOIN agent_history ah ON tu.history_id = ah.id
		 WHERE ah.agent_name = ?
		 GROUP BY tu.model
		 ORDER BY tu.model`,
		agentName,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate tokens for agent %q: %w", agentName, err)
	}
	defer rows.Close()

	var summaries []TokenSummary
	for rows.Next() {
		var ts TokenSummary
		if err := rows.Scan(&ts.Model, &ts.InputTokens, &ts.OutputTokens, &ts.CacheReadTokens, &ts.CacheCreationTokens); err != nil {
			return nil, fmt.Errorf("failed to scan token summary: %w", err)
		}
		summaries = append(summaries, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating token summaries: %w", err)
	}
	return summaries, nil
}

// TokensForWrit sums token usage across all history entries for a writ,
// grouped by model. Returns per-model totals.
func (s *Store) TokensForWrit(writID string) ([]TokenSummary, error) {
	rows, err := s.db.Query(
		`SELECT tu.model,
		        SUM(tu.input_tokens),
		        SUM(tu.output_tokens),
		        SUM(tu.cache_read_tokens),
		        SUM(tu.cache_creation_tokens)
		 FROM token_usage tu
		 JOIN agent_history ah ON tu.history_id = ah.id
		 WHERE ah.writ_id = ?
		 GROUP BY tu.model
		 ORDER BY tu.model`,
		writID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate tokens for writ %q: %w", writID, err)
	}
	defer rows.Close()

	var summaries []TokenSummary
	for rows.Next() {
		var ts TokenSummary
		if err := rows.Scan(&ts.Model, &ts.InputTokens, &ts.OutputTokens, &ts.CacheReadTokens, &ts.CacheCreationTokens); err != nil {
			return nil, fmt.Errorf("failed to scan token summary: %w", err)
		}
		summaries = append(summaries, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating token summaries: %w", err)
	}
	return summaries, nil
}

// TokensForWorld sums all token usage in the world database, grouped by model.
// Returns per-model totals across all agents and writs.
func (s *Store) TokensForWorld() ([]TokenSummary, error) {
	rows, err := s.db.Query(
		`SELECT tu.model,
		        SUM(tu.input_tokens),
		        SUM(tu.output_tokens),
		        SUM(tu.cache_read_tokens),
		        SUM(tu.cache_creation_tokens)
		 FROM token_usage tu
		 GROUP BY tu.model
		 ORDER BY tu.model`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate world tokens: %w", err)
	}
	defer rows.Close()

	var summaries []TokenSummary
	for rows.Next() {
		var ts TokenSummary
		if err := rows.Scan(&ts.Model, &ts.InputTokens, &ts.OutputTokens, &ts.CacheReadTokens, &ts.CacheCreationTokens); err != nil {
			return nil, fmt.Errorf("failed to scan token summary: %w", err)
		}
		summaries = append(summaries, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating token summaries: %w", err)
	}
	return summaries, nil
}

// TokensByWritForAgent returns token usage for a specific agent, broken down
// by writ ID and model. The returned map is keyed by writ ID, with each value
// being a slice of per-model TokenSummary entries.
func (s *Store) TokensByWritForAgent(agentName string) (map[string][]TokenSummary, error) {
	rows, err := s.db.Query(
		`SELECT ah.writ_id,
		        tu.model,
		        SUM(tu.input_tokens),
		        SUM(tu.output_tokens),
		        SUM(tu.cache_read_tokens),
		        SUM(tu.cache_creation_tokens)
		 FROM token_usage tu
		 JOIN agent_history ah ON tu.history_id = ah.id
		 WHERE ah.agent_name = ?
		 GROUP BY ah.writ_id, tu.model
		 ORDER BY ah.writ_id, tu.model`,
		agentName,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate tokens by writ for agent %q: %w", agentName, err)
	}
	defer rows.Close()

	result := make(map[string][]TokenSummary)
	for rows.Next() {
		var writID sql.NullString
		var ts TokenSummary
		if err := rows.Scan(&writID, &ts.Model, &ts.InputTokens, &ts.OutputTokens, &ts.CacheReadTokens, &ts.CacheCreationTokens); err != nil {
			return nil, fmt.Errorf("failed to scan token summary: %w", err)
		}
		key := writID.String
		result[key] = append(result[key], ts)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating token summaries: %w", err)
	}
	return result, nil
}

// TokensSince sums token usage for history entries that started at or after
// the given time, grouped by model. Returns per-model totals.
func (s *Store) TokensSince(since time.Time) ([]TokenSummary, error) {
	sinceStr := since.UTC().Format(time.RFC3339)
	rows, err := s.db.Query(
		`SELECT tu.model,
		        SUM(tu.input_tokens),
		        SUM(tu.output_tokens),
		        SUM(tu.cache_read_tokens),
		        SUM(tu.cache_creation_tokens)
		 FROM token_usage tu
		 JOIN agent_history ah ON tu.history_id = ah.id
		 WHERE ah.started_at >= ?
		 GROUP BY tu.model
		 ORDER BY tu.model`,
		sinceStr,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate tokens since %s: %w", sinceStr, err)
	}
	defer rows.Close()

	var summaries []TokenSummary
	for rows.Next() {
		var ts TokenSummary
		if err := rows.Scan(&ts.Model, &ts.InputTokens, &ts.OutputTokens, &ts.CacheReadTokens, &ts.CacheCreationTokens); err != nil {
			return nil, fmt.Errorf("failed to scan token summary: %w", err)
		}
		summaries = append(summaries, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating token summaries: %w", err)
	}
	return summaries, nil
}

// AgentMergeRequestSummary holds aggregate merge stats for an agent's writs.
type AgentMergeRequestSummary struct {
	TotalMRs     int
	MergedMRs    int
	FailedMRs    int
	FirstPassMRs int // merged with attempts == 1
}

// MergeStatsForAgent returns aggregate merge request statistics for all work
// items the given agent has worked on (via cast history entries).
func (s *Store) MergeStatsForAgent(agentName string) (AgentMergeRequestSummary, error) {
	var summary AgentMergeRequestSummary
	err := s.db.QueryRow(
		`SELECT
			COALESCE(COUNT(*), 0),
			COALESCE(SUM(CASE WHEN mr.phase = 'merged' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN mr.phase = 'failed' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN mr.phase = 'merged' AND mr.attempts = 1 THEN 1 ELSE 0 END), 0)
		 FROM merge_requests mr
		 WHERE mr.writ_id IN (
			SELECT DISTINCT ah.writ_id
			FROM agent_history ah
			WHERE ah.agent_name = ? AND ah.action = 'cast' AND ah.writ_id IS NOT NULL
		 )
		 AND mr.phase IN ('merged', 'failed')`,
		agentName,
	).Scan(&summary.TotalMRs, &summary.MergedMRs, &summary.FailedMRs, &summary.FirstPassMRs)
	if err != nil {
		return summary, fmt.Errorf("failed to get merge stats for agent %q: %w", agentName, err)
	}
	return summary, nil
}
