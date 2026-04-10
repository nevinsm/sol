// Package quota provides the CLI API type for account quota status.
package quota

import (
	"time"

	iquota "github.com/nevinsm/sol/internal/quota"
)

// AccountQuota is the CLI API representation of an account's quota state.
type AccountQuota struct {
	Account    string     `json:"account"`
	Status     string     `json:"status"`
	Window     string     `json:"window,omitempty"`
	LimitedAt  *time.Time `json:"limited_at,omitempty"`
	ResetsAt   *time.Time `json:"resets_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	Remaining  *string    `json:"remaining,omitempty"`
}

// FromBrokerQuota converts quota.AccountState to the CLI API AccountQuota type.
// The handle parameter supplies the account identifier.
func FromBrokerQuota(handle string, state iquota.AccountState) AccountQuota {
	return AccountQuota{
		Account:    handle,
		Status:     string(state.Status),
		LimitedAt:  state.LimitedAt,
		ResetsAt:   state.ResetsAt,
		LastUsedAt: state.LastUsed,
	}
}
