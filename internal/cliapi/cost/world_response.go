package cost

// WorldCostResponse is the CLI API representation of per-agent cost breakdown
// within a world (sol cost --world --json).
type WorldCostResponse struct {
	World     string      `json:"world"`
	Agents    []AgentCost `json:"agents"`
	TotalCost *float64    `json:"total_cost"`
	Period    string      `json:"period"`
}
