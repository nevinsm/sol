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
	CostUSD             *float64
	DurationMS          *int64
	Runtime             string
}

// TokenSummary holds aggregated token counts for a single model.
type TokenSummary struct {
	Model               string
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	CostUSD             *float64
	DurationMS          *int64
}

func generateHistoryID() (string, error) {
	return generatePrefixedID("ah-")
}

func generateTokenUsageID() (string, error) {
	return generatePrefixedID("tu-")
}

// WriteHistory inserts an agent_history record and returns its generated ID.
func (s *WorldStore) WriteHistory(agentName, writID, action, summary string, startedAt time.Time, endedAt *time.Time) (string, error) {
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
func (s *WorldStore) GetHistory(id string) (*HistoryEntry, error) {
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
func (s *WorldStore) ListHistory(agentName string) ([]HistoryEntry, error) {
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
func (s *WorldStore) EndHistory(writID string) (string, error) {
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
// costUSD and durationMS are optional — pass nil to omit.
// runtime identifies the source runtime (e.g. "claude-code") — pass "" to omit.
func (s *WorldStore) WriteTokenUsage(historyID, model string, input, output, cacheRead, cacheCreation int64, costUSD *float64, durationMS *int64, runtime string) (string, error) {
	id, err := generateTokenUsageID()
	if err != nil {
		return "", err
	}

	var rt sql.NullString
	if runtime != "" {
		rt = sql.NullString{String: runtime, Valid: true}
	}

	_, err = s.db.Exec(
		`INSERT INTO token_usage (id, history_id, model, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens, cost_usd, duration_ms, runtime)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, historyID, model, input, output, cacheRead, cacheCreation, costUSD, durationMS, rt,
	)
	if err != nil {
		return "", fmt.Errorf("failed to insert token usage: %w", err)
	}
	return id, nil
}

// scanTokenSummary scans a TokenSummary including optional cost_usd and duration_ms.
func scanTokenSummary(scanner interface{ Scan(...interface{}) error }, ts *TokenSummary) error {
	var costUSD sql.NullFloat64
	var durationMS sql.NullInt64
	if err := scanner.Scan(&ts.Model, &ts.InputTokens, &ts.OutputTokens, &ts.CacheReadTokens, &ts.CacheCreationTokens, &costUSD, &durationMS); err != nil {
		return err
	}
	if costUSD.Valid {
		ts.CostUSD = &costUSD.Float64
	}
	if durationMS.Valid {
		ts.DurationMS = &durationMS.Int64
	}
	return nil
}

// tokenSummaryColumns is the standard SELECT clause for aggregated token queries.
const tokenSummaryColumns = `tu.model,
		        SUM(tu.input_tokens),
		        SUM(tu.output_tokens),
		        SUM(tu.cache_read_tokens),
		        SUM(tu.cache_creation_tokens),
		        SUM(tu.cost_usd),
		        SUM(tu.duration_ms)`

// TokensForHistory returns aggregated token totals for a single history entry.
func (s *WorldStore) TokensForHistory(historyID string) (*TokenSummary, error) {
	var ts TokenSummary
	var costUSD sql.NullFloat64
	var durationMS sql.NullInt64
	err := s.db.QueryRow(
		`SELECT COALESCE(SUM(input_tokens),0),
		        COALESCE(SUM(output_tokens),0),
		        COALESCE(SUM(cache_read_tokens),0),
		        COALESCE(SUM(cache_creation_tokens),0),
		        SUM(cost_usd),
		        SUM(duration_ms)
		 FROM token_usage WHERE history_id = ?`, historyID,
	).Scan(&ts.InputTokens, &ts.OutputTokens, &ts.CacheReadTokens, &ts.CacheCreationTokens, &costUSD, &durationMS)
	if err != nil {
		return nil, fmt.Errorf("failed to get tokens for history %q: %w", historyID, err)
	}
	total := ts.InputTokens + ts.OutputTokens + ts.CacheReadTokens + ts.CacheCreationTokens
	if total == 0 {
		return nil, nil
	}
	if costUSD.Valid {
		ts.CostUSD = &costUSD.Float64
	}
	if durationMS.Valid {
		ts.DurationMS = &durationMS.Int64
	}
	return &ts, nil
}

// AggregateTokens sums token usage across all history entries for an agent,
// grouped by model. Returns per-model totals.
func (s *WorldStore) AggregateTokens(agentName string) ([]TokenSummary, error) {
	rows, err := s.db.Query(
		`SELECT `+tokenSummaryColumns+`
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
		if err := scanTokenSummary(rows, &ts); err != nil {
			return nil, fmt.Errorf("failed to scan token summary: %w", err)
		}
		summaries = append(summaries, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating token summaries: %w", err)
	}
	return summaries, nil
}

// HistoryForWrit returns all agent_history entries for a given writ ID,
// ordered by started_at ASC.
func (s *WorldStore) HistoryForWrit(writID string) ([]HistoryEntry, error) {
	rows, err := s.db.Query(
		`SELECT id, agent_name, writ_id, action, started_at, ended_at, summary
		 FROM agent_history WHERE writ_id = ?
		 ORDER BY started_at ASC`, writID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list history for writ %q: %w", writID, err)
	}
	defer rows.Close()

	var entries []HistoryEntry
	for rows.Next() {
		var h HistoryEntry
		var wID, summary, endedAt sql.NullString
		var startedAt string

		if err := rows.Scan(&h.ID, &h.AgentName, &wID, &h.Action, &startedAt, &endedAt, &summary); err != nil {
			return nil, fmt.Errorf("failed to scan agent history for writ %q: %w", writID, err)
		}

		h.WritID = wID.String
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
		return nil, fmt.Errorf("failed iterating history for writ %q: %w", writID, err)
	}
	return entries, nil
}

// TokensForWrit sums token usage across all history entries for a writ,
// grouped by model. Returns per-model totals.
func (s *WorldStore) TokensForWrit(writID string) ([]TokenSummary, error) {
	rows, err := s.db.Query(
		`SELECT `+tokenSummaryColumns+`
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
		if err := scanTokenSummary(rows, &ts); err != nil {
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
func (s *WorldStore) TokensForWorld() ([]TokenSummary, error) {
	rows, err := s.db.Query(
		`SELECT `+tokenSummaryColumns+`
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
		if err := scanTokenSummary(rows, &ts); err != nil {
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
func (s *WorldStore) TokensByWritForAgent(agentName string) (map[string][]TokenSummary, error) {
	rows, err := s.db.Query(
		`SELECT ah.writ_id,
		        `+tokenSummaryColumns+`
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
		var costUSD sql.NullFloat64
		var durationMS sql.NullInt64
		if err := rows.Scan(&writID, &ts.Model, &ts.InputTokens, &ts.OutputTokens, &ts.CacheReadTokens, &ts.CacheCreationTokens, &costUSD, &durationMS); err != nil {
			return nil, fmt.Errorf("failed to scan token summary: %w", err)
		}
		if costUSD.Valid {
			ts.CostUSD = &costUSD.Float64
		}
		if durationMS.Valid {
			ts.DurationMS = &durationMS.Int64
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
func (s *WorldStore) TokensSince(since time.Time) ([]TokenSummary, error) {
	sinceStr := since.UTC().Format(time.RFC3339)
	rows, err := s.db.Query(
		`SELECT `+tokenSummaryColumns+`
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
		if err := scanTokenSummary(rows, &ts); err != nil {
			return nil, fmt.Errorf("failed to scan token summary: %w", err)
		}
		summaries = append(summaries, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating token summaries: %w", err)
	}
	return summaries, nil
}

// AgentTokenSummary holds aggregated token counts for a single agent.
type AgentTokenSummary struct {
	AgentName           string
	WritCount           int
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	CostUSD             *float64
	DurationMS          *int64
}

// scanAgentTokenSummary scans an AgentTokenSummary including optional cost/duration.
func scanAgentTokenSummary(scanner interface{ Scan(...interface{}) error }, ats *AgentTokenSummary) error {
	var costUSD sql.NullFloat64
	var durationMS sql.NullInt64
	if err := scanner.Scan(&ats.AgentName, &ats.WritCount, &ats.InputTokens, &ats.OutputTokens, &ats.CacheReadTokens, &ats.CacheCreationTokens, &costUSD, &durationMS); err != nil {
		return err
	}
	if costUSD.Valid {
		ats.CostUSD = &costUSD.Float64
	}
	if durationMS.Valid {
		ats.DurationMS = &durationMS.Int64
	}
	return nil
}

// agentTokenSummaryColumns is the standard SELECT clause for per-agent aggregated token queries.
const agentTokenSummaryColumns = `ah.agent_name,
		        COUNT(DISTINCT ah.writ_id),
		        SUM(tu.input_tokens),
		        SUM(tu.output_tokens),
		        SUM(tu.cache_read_tokens),
		        SUM(tu.cache_creation_tokens),
		        SUM(tu.cost_usd),
		        SUM(tu.duration_ms)`

// TokensByAgentForWorld returns per-agent token summaries across all models.
// Each entry aggregates all model usage for one agent. WritCount is the number
// of distinct writs the agent has token records for.
func (s *WorldStore) TokensByAgentForWorld() ([]AgentTokenSummary, error) {
	rows, err := s.db.Query(
		`SELECT `+agentTokenSummaryColumns+`
		 FROM token_usage tu
		 JOIN agent_history ah ON tu.history_id = ah.id
		 GROUP BY ah.agent_name
		 ORDER BY ah.agent_name`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate tokens by agent: %w", err)
	}
	defer rows.Close()

	var summaries []AgentTokenSummary
	for rows.Next() {
		var ats AgentTokenSummary
		if err := scanAgentTokenSummary(rows, &ats); err != nil {
			return nil, fmt.Errorf("failed to scan agent token summary: %w", err)
		}
		summaries = append(summaries, ats)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating agent token summaries: %w", err)
	}
	return summaries, nil
}

// TokensByAgentSince returns per-agent token summaries for history entries
// started at or after the given time.
func (s *WorldStore) TokensByAgentSince(since time.Time) ([]AgentTokenSummary, error) {
	sinceStr := since.UTC().Format(time.RFC3339)
	rows, err := s.db.Query(
		`SELECT `+agentTokenSummaryColumns+`
		 FROM token_usage tu
		 JOIN agent_history ah ON tu.history_id = ah.id
		 WHERE ah.started_at >= ?
		 GROUP BY ah.agent_name
		 ORDER BY ah.agent_name`,
		sinceStr,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate tokens by agent since %s: %w", sinceStr, err)
	}
	defer rows.Close()

	var summaries []AgentTokenSummary
	for rows.Next() {
		var ats AgentTokenSummary
		if err := scanAgentTokenSummary(rows, &ats); err != nil {
			return nil, fmt.Errorf("failed to scan agent token summary: %w", err)
		}
		summaries = append(summaries, ats)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating agent token summaries: %w", err)
	}
	return summaries, nil
}

// TokensByWritForAgentSince returns token usage for a specific agent, broken
// down by writ ID and model, filtered to history entries started at or after
// the given time.
func (s *WorldStore) TokensByWritForAgentSince(agentName string, since time.Time) (map[string][]TokenSummary, error) {
	sinceStr := since.UTC().Format(time.RFC3339)
	rows, err := s.db.Query(
		`SELECT ah.writ_id,
		        `+tokenSummaryColumns+`
		 FROM token_usage tu
		 JOIN agent_history ah ON tu.history_id = ah.id
		 WHERE ah.agent_name = ? AND ah.started_at >= ?
		 GROUP BY ah.writ_id, tu.model
		 ORDER BY ah.writ_id, tu.model`,
		agentName, sinceStr,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate tokens by writ for agent %q since %s: %w", agentName, sinceStr, err)
	}
	defer rows.Close()

	result := make(map[string][]TokenSummary)
	for rows.Next() {
		var writID sql.NullString
		var ts TokenSummary
		var costUSD sql.NullFloat64
		var durationMS sql.NullInt64
		if err := rows.Scan(&writID, &ts.Model, &ts.InputTokens, &ts.OutputTokens, &ts.CacheReadTokens, &ts.CacheCreationTokens, &costUSD, &durationMS); err != nil {
			return nil, fmt.Errorf("failed to scan token summary: %w", err)
		}
		if costUSD.Valid {
			ts.CostUSD = &costUSD.Float64
		}
		if durationMS.Valid {
			ts.DurationMS = &durationMS.Int64
		}
		key := writID.String
		result[key] = append(result[key], ts)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating token summaries: %w", err)
	}
	return result, nil
}

// WorldTokenMeta returns the count of distinct agents and writs that have
// token usage records in this world database.
func (s *WorldStore) WorldTokenMeta() (agents int, writs int, err error) {
	err = s.db.QueryRow(
		`SELECT COUNT(DISTINCT ah.agent_name),
		        COUNT(DISTINCT ah.writ_id)
		 FROM agent_history ah
		 WHERE ah.id IN (SELECT DISTINCT history_id FROM token_usage)`,
	).Scan(&agents, &writs)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to count agents/writs with token data: %w", err)
	}
	return agents, writs, nil
}

// WorldTokenMetaSince returns the count of distinct agents and writs that have
// token usage records started at or after the given time.
func (s *WorldStore) WorldTokenMetaSince(since time.Time) (agents int, writs int, err error) {
	sinceStr := since.UTC().Format(time.RFC3339)
	err = s.db.QueryRow(
		`SELECT COUNT(DISTINCT ah.agent_name),
		        COUNT(DISTINCT ah.writ_id)
		 FROM agent_history ah
		 WHERE ah.started_at >= ?
		   AND ah.id IN (SELECT DISTINCT history_id FROM token_usage)`,
		sinceStr,
	).Scan(&agents, &writs)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to count agents/writs with token data since %s: %w", sinceStr, err)
	}
	return agents, writs, nil
}

// TokensForWritSince sums token usage for a writ, filtered to history entries
// started at or after the given time, grouped by model.
func (s *WorldStore) TokensForWritSince(writID string, since time.Time) ([]TokenSummary, error) {
	sinceStr := since.UTC().Format(time.RFC3339)
	rows, err := s.db.Query(
		`SELECT `+tokenSummaryColumns+`
		 FROM token_usage tu
		 JOIN agent_history ah ON tu.history_id = ah.id
		 WHERE ah.writ_id = ? AND ah.started_at >= ?
		 GROUP BY tu.model
		 ORDER BY tu.model`,
		writID, sinceStr,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate tokens for writ %q since %s: %w", writID, sinceStr, err)
	}
	defer rows.Close()

	var summaries []TokenSummary
	for rows.Next() {
		var ts TokenSummary
		if err := scanTokenSummary(rows, &ts); err != nil {
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
func (s *WorldStore) MergeStatsForAgent(agentName string) (AgentMergeRequestSummary, error) {
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
