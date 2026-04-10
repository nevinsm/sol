package caravans

// ListEntry is the CLI API representation of a single caravan in
// caravan list --json output. Preserves the existing JSON shape exactly.
type ListEntry struct {
	ID            string                    `json:"id"`
	Name          string                    `json:"name"`
	Status        string                    `json:"status"`
	Owner         string                    `json:"owner"`
	Items         int                       `json:"items"`
	Merged        int                       `json:"merged"`
	Worlds        []string                  `json:"worlds"`
	PhaseProgress map[string]ListPhaseStats `json:"phase_progress"`
	CreatedAt     string                    `json:"created_at"`
	ClosedAt      *string                   `json:"closed_at,omitempty"`
}

// ListPhaseStats tracks per-phase item counts in caravan list --json output.
type ListPhaseStats struct {
	Total      int `json:"total"`
	Merged     int `json:"merged"`
	InProgress int `json:"in_progress"`
	Ready      int `json:"ready"`
	Blocked    int `json:"blocked"`
}
