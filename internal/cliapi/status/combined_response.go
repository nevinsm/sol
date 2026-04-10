package status

import (
	internstatus "github.com/nevinsm/sol/internal/status"
)

// CombinedStatusResponse is the CLI API response for `sol status` when a world
// is auto-detected from cwd. It embeds the full world status and adds
// sphere-level consul info and escalation summary.
type CombinedStatusResponse struct {
	Consul      ConsulInfo         `json:"consul"`
	Escalations *EscalationSummary `json:"escalations,omitempty"`
	*WorldStatusResponse
}

// FromCombinedStatus converts the internal types used by runCombinedStatus
// into the CLI API response type.
func FromCombinedStatus(
	consul internstatus.ConsulInfo,
	escalations *internstatus.EscalationSummary,
	ws *internstatus.WorldStatus,
) *CombinedStatusResponse {
	return &CombinedStatusResponse{
		Consul:              convertConsulInfo(consul),
		Escalations:         convertEscalationSummary(escalations),
		WorldStatusResponse: FromWorldStatus(ws),
	}
}
