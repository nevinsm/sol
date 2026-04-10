package accounts

import (
	"testing"

	"github.com/nevinsm/sol/internal/account"
)

func TestFromStoreAccount(t *testing.T) {
	a := account.Account{
		Email:       "test@example.com",
		Description: "Primary account",
	}

	result := FromStoreAccount("primary", a, "oauth", "active", true)

	if result.Handle != "primary" {
		t.Errorf("Handle = %q, want %q", result.Handle, "primary")
	}
	if result.Type != "oauth" {
		t.Errorf("Type = %q, want %q", result.Type, "oauth")
	}
	if result.Status != "active" {
		t.Errorf("Status = %q, want %q", result.Status, "active")
	}
	if !result.Default {
		t.Error("Default = false, want true")
	}
}

func TestFromStoreAccountNonDefault(t *testing.T) {
	a := account.Account{}

	result := FromStoreAccount("secondary", a, "api-key", "inactive", false)

	if result.Default {
		t.Error("Default = true, want false")
	}
	if result.Type != "api-key" {
		t.Errorf("Type = %q, want %q", result.Type, "api-key")
	}
}
