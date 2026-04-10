package cost

// WritCostResponse is the CLI API representation of per-model cost breakdown
// for a writ (sol cost --writ --world --json).
type WritCostResponse struct {
	WritID    string      `json:"writ_id"`
	Title     string      `json:"title,omitempty"`
	Kind      string      `json:"kind,omitempty"`
	Status    string      `json:"status,omitempty"`
	Models    []ModelCost `json:"models"`
	TotalCost *float64    `json:"total_cost"`
	Period    string      `json:"period"`
}
