package cost

// WritCost represents cost data for a single writ within an agent breakdown.
type WritCost struct {
	WritID       string   `json:"writ_id"`
	Kind         string   `json:"kind"`
	Status       string   `json:"status"`
	InputTokens  int64    `json:"input_tokens"`
	OutputTokens int64    `json:"output_tokens"`
	CacheTokens  int64    `json:"cache_tokens"`
	Cost         *float64 `json:"cost"`
}

// AgentCostResponse is the CLI API representation of per-writ cost breakdown
// for an agent (sol cost --agent --world --json).
type AgentCostResponse struct {
	World     string     `json:"world"`
	Agent     string     `json:"agent"`
	Writs     []WritCost `json:"writs"`
	TotalCost *float64   `json:"total_cost"`
	Period    string     `json:"period"`
}
