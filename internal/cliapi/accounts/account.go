// Package accounts provides the CLI API type for account entities.
package accounts

import (
	"github.com/nevinsm/sol/internal/account"
)

// Account is the CLI API representation of a registered account.
type Account struct {
	Handle  string `json:"handle"`
	Type    string `json:"type"`
	Status  string `json:"status"`
	Default bool   `json:"default"`
}

// FromStoreAccount converts an account.Account from the registry to the CLI API Account type.
// The handle, accountType, status, and isDefault parameters supply context from the registry.
func FromStoreAccount(handle string, a account.Account, accountType, status string, isDefault bool) Account {
	return Account{
		Handle:  handle,
		Type:    accountType,
		Status:  status,
		Default: isDefault,
	}
}
