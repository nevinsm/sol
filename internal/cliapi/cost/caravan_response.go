package cost

// CaravanWritCost represents cost data for a single writ within a caravan breakdown.
type CaravanWritCost struct {
	WritID       string   `json:"writ_id"`
	World        string   `json:"world"`
	Phase        int      `json:"phase"`
	Kind         string   `json:"kind"`
	Status       string   `json:"status"`
	InputTokens  int64    `json:"input_tokens"`
	OutputTokens int64    `json:"output_tokens"`
	CacheTokens  int64    `json:"cache_tokens"`
	Cost         *float64 `json:"cost"`
}

// CaravanCostResponse is the CLI API representation of per-writ cost breakdown
// for a caravan (sol cost --caravan --json).
type CaravanCostResponse struct {
	CaravanID   string            `json:"caravan_id"`
	CaravanName string            `json:"caravan_name"`
	Writs       []CaravanWritCost `json:"writs"`
	TotalCost   *float64          `json:"total_cost"`
	Period      string            `json:"period"`
}
