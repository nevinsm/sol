// Package cost provides the CLI API types for cost/token usage output.
package cost

// CostSummary is the top-level cost output for sphere-wide cost queries.
type CostSummary struct {
	Worlds    []WorldCost `json:"worlds"`
	TotalCost *float64    `json:"total_cost"`
	Period    string      `json:"period"`
}

// WorldCost represents cost data aggregated per world.
type WorldCost struct {
	World        string   `json:"world"`
	Agents       int      `json:"agents"`
	Writs        int      `json:"writs"`
	InputTokens  int64    `json:"input_tokens"`
	OutputTokens int64    `json:"output_tokens"`
	CacheTokens  int64    `json:"cache_tokens"`
	Cost         *float64 `json:"cost"`
}

// AgentCost represents cost data aggregated per agent within a world.
type AgentCost struct {
	Agent        string   `json:"agent"`
	Writs        int      `json:"writs"`
	InputTokens  int64    `json:"input_tokens"`
	OutputTokens int64    `json:"output_tokens"`
	CacheTokens  int64    `json:"cache_tokens"`
	Cost         *float64 `json:"cost"`
}

// ModelCost represents cost data aggregated per model within a writ.
type ModelCost struct {
	Model               string   `json:"model"`
	InputTokens         int64    `json:"input_tokens"`
	OutputTokens        int64    `json:"output_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	CacheCreationTokens int64    `json:"cache_creation_tokens"`
	Cost                *float64 `json:"cost"`
}
