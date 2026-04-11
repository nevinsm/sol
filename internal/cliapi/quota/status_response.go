package quota

import (
	"time"

	iquota "github.com/nevinsm/sol/internal/quota"
)

// StatusAccount is the per-account JSON row emitted by `sol quota status --json`.
// Field names preserve the post-cli-polish shape; renames happen in W2.1.
type StatusAccount struct {
	Handle    string       `json:"handle"`
	Status    iquota.Status `json:"status"`
	LimitedAt *time.Time   `json:"limited_at,omitempty"`
	ResetsAt  *time.Time   `json:"resets_at,omitempty"`
	LastUsed  *time.Time   `json:"last_used_at,omitempty"`
	Window    string       `json:"window,omitempty"`
	Remaining *int         `json:"remaining,omitempty"`
}

// StatusResponse is the top-level JSON output for `sol quota status --json`.
type StatusResponse struct {
	Accounts []StatusAccount `json:"accounts"`
}

// NewStatusAccount converts an internal AccountState to the CLI API StatusAccount type.
func NewStatusAccount(handle string, state iquota.AccountState) StatusAccount {
	return StatusAccount{
		Handle:    handle,
		Status:    state.Status,
		LimitedAt: state.LimitedAt,
		ResetsAt:  state.ResetsAt,
		LastUsed:  state.LastUsed,
		// Window/Remaining intentionally omitted — see cmd/quota.go TODO.
	}
}
