package writs

// DepListResponse is the CLI API response for `sol writ dep list --json`.
type DepListResponse struct {
	WritID     string   `json:"writ_id"`
	DependsOn  []string `json:"depends_on"`
	DependedBy []string `json:"depended_by"`
}

// NewDepListResponse creates a DepListResponse with empty-array guarantees.
func NewDepListResponse(writID string, dependsOn, dependedBy []string) DepListResponse {
	if dependsOn == nil {
		dependsOn = []string{}
	}
	if dependedBy == nil {
		dependedBy = []string{}
	}
	return DepListResponse{
		WritID:     writID,
		DependsOn:  dependsOn,
		DependedBy: dependedBy,
	}
}
