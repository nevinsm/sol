package account

import (
	"os"
	"testing"

	"github.com/nevinsm/sol/internal/config"
)

func setupTestHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
}

func TestValidateHandle(t *testing.T) {
	tests := []struct {
		handle string
		ok     bool
	}{
		{"work", true},
		{"personal", true},
		{"my-acct", true},
		{"Acct.1", true},
		{"", false},
		{"1bad", false},
		{"-bad", false},
		{"has space", false},
	}
	for _, tt := range tests {
		err := ValidateHandle(tt.handle)
		if tt.ok && err != nil {
			t.Errorf("ValidateHandle(%q) = %v, want nil", tt.handle, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("ValidateHandle(%q) = nil, want error", tt.handle)
		}
	}
}

func TestAddAndList(t *testing.T) {
	setupTestHome(t)

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if err := reg.Add("work", "work@example.com", "work account"); err != nil {
		t.Fatal(err)
	}
	if reg.Default != "work" {
		t.Errorf("default = %q, want %q", reg.Default, "work")
	}

	if err := reg.Add("personal", "me@example.com", ""); err != nil {
		t.Fatal(err)
	}
	if reg.Default != "work" {
		t.Errorf("default should still be %q after second add, got %q", "work", reg.Default)
	}

	if len(reg.Accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(reg.Accounts))
	}

	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}

	// Reload and verify persistence.
	reg2, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg2.Accounts) != 2 {
		t.Fatalf("expected 2 accounts after reload, got %d", len(reg2.Accounts))
	}
	if reg2.Default != "work" {
		t.Errorf("default after reload = %q, want %q", reg2.Default, "work")
	}
}

func TestAddDuplicate(t *testing.T) {
	setupTestHome(t)

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add("work", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add("work", "", ""); err == nil {
		t.Error("expected error adding duplicate handle")
	}
}

func TestRemove(t *testing.T) {
	setupTestHome(t)

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	_ = reg.Add("work", "", "")
	_ = reg.Add("personal", "", "")
	_ = reg.Save()

	// Cannot remove default when other accounts exist.
	if err := reg.Remove("work"); err == nil {
		t.Error("expected error removing default account with others present")
	}

	// Can remove non-default.
	if err := reg.Remove("personal"); err != nil {
		t.Fatal(err)
	}
	if len(reg.Accounts) != 1 {
		t.Fatalf("expected 1 account after remove, got %d", len(reg.Accounts))
	}

	// Can remove the last remaining account (also the default).
	if err := reg.Remove("work"); err != nil {
		t.Fatal(err)
	}
	if len(reg.Accounts) != 0 {
		t.Fatalf("expected 0 accounts, got %d", len(reg.Accounts))
	}
	if reg.Default != "" {
		t.Errorf("default should be empty, got %q", reg.Default)
	}
}

func TestRemoveDeletesDir(t *testing.T) {
	setupTestHome(t)

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	_ = reg.Add("work", "", "")

	dir := config.AccountDir("work")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatalf("account directory should exist after add: %s", dir)
	}

	_ = reg.Remove("work")

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("account directory should be removed after delete: %s", dir)
	}
}

func TestSetDefault(t *testing.T) {
	setupTestHome(t)

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	_ = reg.Add("work", "", "")
	_ = reg.Add("personal", "", "")

	if err := reg.SetDefault("personal"); err != nil {
		t.Fatal(err)
	}
	if reg.Default != "personal" {
		t.Errorf("default = %q, want %q", reg.Default, "personal")
	}

	if err := reg.SetDefault("nonexistent"); err == nil {
		t.Error("expected error setting default to nonexistent account")
	}
}

func TestAddCreatesDir(t *testing.T) {
	setupTestHome(t)

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	_ = reg.Add("work", "", "")

	expected := config.AccountDir("work")
	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Fatalf("directory should exist: %s", expected)
	}
}

func TestLoadEmptyRegistry(t *testing.T) {
	setupTestHome(t)

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Accounts) != 0 {
		t.Fatalf("expected empty registry, got %d accounts", len(reg.Accounts))
	}
	if reg.Default != "" {
		t.Errorf("expected empty default, got %q", reg.Default)
	}
}
