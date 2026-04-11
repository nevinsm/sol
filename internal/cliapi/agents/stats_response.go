package agents

import "github.com/nevinsm/sol/internal/store"

// StatsReport holds computed performance metrics for an agent,
// used as the --json output for "sol agent stats [name]".
type StatsReport struct {
	Name             string         `json:"name"`
	TotalCasts       int            `json:"total_casts"`
	CompletedCasts   int            `json:"completed_casts"`
	CycleTimeMedianS *float64      `json:"cycle_time_median_s,omitempty"`
	CycleTimeP90S    *float64      `json:"cycle_time_p90_s,omitempty"`
	FirstPassRate    *float64      `json:"first_pass_rate,omitempty"`
	FirstPassMRs     int           `json:"first_pass_mrs"`
	MergedMRs        int           `json:"merged_mrs"`
	FailedMRs        int           `json:"failed_mrs"`
	ReworkCount      int           `json:"rework_count"`
	Tokens           []TokenSummary `json:"tokens"`
	TotalTokens      int64         `json:"total_tokens"`
	EstimatedCost    *float64      `json:"estimated_cost"`
}

// TokenSummary holds aggregated token counts for a single model.
type TokenSummary struct {
	Model               string   `json:"model"`
	InputTokens         int64    `json:"input_tokens"`
	OutputTokens        int64    `json:"output_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	CacheCreationTokens int64    `json:"cache_creation_tokens"`
	ReasoningTokens     int64    `json:"reasoning_tokens"`
	CostUSD             *float64 `json:"cost_usd,omitempty"`
	DurationMS          *int64   `json:"duration_ms,omitempty"`
}

// FromStoreTokenSummary converts a store.TokenSummary to the CLI API TokenSummary.
func FromStoreTokenSummary(t store.TokenSummary) TokenSummary {
	return TokenSummary{
		Model:               t.Model,
		InputTokens:         t.InputTokens,
		OutputTokens:        t.OutputTokens,
		CacheReadTokens:     t.CacheReadTokens,
		CacheCreationTokens: t.CacheCreationTokens,
		ReasoningTokens:     t.ReasoningTokens,
		CostUSD:             t.CostUSD,
		DurationMS:          t.DurationMS,
	}
}

// FromStoreTokenSummaries converts a slice of store.TokenSummary to CLI API types.
func FromStoreTokenSummaries(ts []store.TokenSummary) []TokenSummary {
	if ts == nil {
		return nil
	}
	out := make([]TokenSummary, len(ts))
	for i, t := range ts {
		out[i] = FromStoreTokenSummary(t)
	}
	return out
}
