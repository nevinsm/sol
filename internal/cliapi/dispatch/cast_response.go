package dispatch

import (
	"fmt"

	internaldispatch "github.com/nevinsm/sol/internal/dispatch"
)

// CastResult is the CLI API response for a successful cast operation.
type CastResult struct {
	WritID       string `json:"writ_id"`
	AgentName    string `json:"agent_name"`
	WorktreePath string `json:"worktree_path"`
	SessionName  string `json:"session_name"`
	Branch       string `json:"branch"`
	Guidelines   string `json:"guidelines,omitempty"`
}

// FromCastResult converts an internal dispatch.CastResult to the CLI API type.
func FromCastResult(r *internaldispatch.CastResult) CastResult {
	return CastResult{
		WritID:       r.WritID,
		AgentName:    r.AgentName,
		WorktreePath: r.WorktreeDir,
		SessionName:  r.SessionName,
		Branch:       fmt.Sprintf("outpost/%s/%s", r.AgentName, r.WritID),
		Guidelines:   r.Guidelines,
	}
}
