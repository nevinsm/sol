package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/account"
	"github.com/nevinsm/sol/internal/cliformat"
)

// setupAccountHome creates a fresh SOL_HOME for an account test, with any
// leftover list flags reset.
func setupAccountHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatalf("create .store: %v", err)
	}
	t.Cleanup(func() {
		accountListJSON = false
		accountDeleteConfirm = false
		accountDeleteForce = false
	})
	return dir
}

// TestAccountTypeAndStatus exercises the TYPE/STATUS cell mapping in
// isolation — the broker/quota state is passed explicitly so the test does
// not depend on a running broker.
func TestAccountTypeAndStatus(t *testing.T) {
	setupAccountHome(t)

	// Register two accounts with different token types.
	if err := account.LockedRegistryUpdate(func(reg *account.Registry) error {
		if err := reg.Add("alice", "", ""); err != nil {
			return err
		}
		return reg.Add("bob", "", "")
	}); err != nil {
		t.Fatalf("register accounts: %v", err)
	}

	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-24 * time.Hour)

	if err := account.WriteToken("alice", &account.Token{
		Type:      "oauth_token",
		Token:     "secret",
		CreatedAt: time.Now(),
		ExpiresAt: &future,
	}); err != nil {
		t.Fatalf("write alice token: %v", err)
	}
	if err := account.WriteToken("bob", &account.Token{
		Type:      "api_key",
		Token:     "key-123",
		CreatedAt: time.Now(),
		ExpiresAt: &past,
	}); err != nil {
		t.Fatalf("write bob token: %v", err)
	}

	now := time.Now()

	tests := []struct {
		name        string
		handle      string
		offline     bool
		limited     map[string]bool
		wantType    string
		wantStatus  string
	}{
		{
			name:       "oauth healthy",
			handle:     "alice",
			wantType:   "oauth",
			wantStatus: "ok",
		},
		{
			name:       "api_key expired token",
			handle:     "bob",
			wantType:   "api_key",
			wantStatus: "expired",
		},
		{
			name:       "oauth limited by quota",
			handle:     "alice",
			limited:    map[string]bool{"alice": true},
			wantType:   "oauth",
			wantStatus: "limited",
		},
		{
			name:       "broker offline renders status as unknown",
			handle:     "alice",
			offline:    true,
			wantType:   "oauth",
			wantStatus: cliformat.EmptyMarker,
		},
		{
			name:       "no token at all",
			handle:     "ghost",
			wantType:   cliformat.EmptyMarker,
			wantStatus: cliformat.EmptyMarker,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			limited := tc.limited
			if limited == nil {
				limited = map[string]bool{}
			}
			gotType, gotStatus := accountTypeAndStatus(tc.handle, now, tc.offline, limited)
			if gotType != tc.wantType {
				t.Errorf("type: want %q, got %q", tc.wantType, gotType)
			}
			if gotStatus != tc.wantStatus {
				t.Errorf("status: want %q, got %q", tc.wantStatus, gotStatus)
			}
		})
	}
}

// TestAccountListColumns verifies the table header and count footer rendered
// by `sol account list` — no EMAIL/DESCRIPTION columns, new HANDLE/TYPE/
// STATUS/DEFAULT layout, and pluralised count footer.
func TestAccountListColumns(t *testing.T) {
	setupAccountHome(t)

	if err := account.LockedRegistryUpdate(func(reg *account.Registry) error {
		if err := reg.Add("alice", "alice@example.com", "default test account"); err != nil {
			return err
		}
		return reg.Add("bob", "", "")
	}); err != nil {
		t.Fatalf("register accounts: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runAccountList(nil, nil); err != nil {
			t.Fatalf("runAccountList: %v", err)
		}
	})

	// Header must be the new layout, with no EMAIL or DESCRIPTION columns.
	if !strings.Contains(out, "HANDLE") || !strings.Contains(out, "TYPE") ||
		!strings.Contains(out, "STATUS") || !strings.Contains(out, "DEFAULT") {
		t.Errorf("expected HANDLE/TYPE/STATUS/DEFAULT header, got:\n%s", out)
	}
	if strings.Contains(out, "EMAIL") {
		t.Errorf("did not expect dead EMAIL column, got:\n%s", out)
	}
	if strings.Contains(out, "DESCRIPTION") {
		t.Errorf("did not expect dead DESCRIPTION column, got:\n%s", out)
	}

	// Both handles appear.
	if !strings.Contains(out, "alice") || !strings.Contains(out, "bob") {
		t.Errorf("expected both handles in output, got:\n%s", out)
	}

	// Count footer uses cliformat.FormatCount pluralisation.
	if !strings.Contains(out, "2 accounts") {
		t.Errorf("expected '2 accounts' footer, got:\n%s", out)
	}
}

// TestAccountListCountFooterSingular verifies the singular form of the
// account count footer.
func TestAccountListCountFooterSingular(t *testing.T) {
	setupAccountHome(t)

	if err := account.LockedRegistryUpdate(func(reg *account.Registry) error {
		return reg.Add("alice", "", "")
	}); err != nil {
		t.Fatalf("register account: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runAccountList(nil, nil); err != nil {
			t.Fatalf("runAccountList: %v", err)
		}
	})

	if !strings.Contains(out, "1 account") || strings.Contains(out, "1 accounts") {
		t.Errorf("expected singular '1 account' footer, got:\n%s", out)
	}
}

// TestAccountListJSONPreservesEmailDescription verifies that --json output
// still carries email/description for any caller that populates them via
// direct DB write.
func TestAccountListJSONPreservesEmailDescription(t *testing.T) {
	setupAccountHome(t)

	if err := account.LockedRegistryUpdate(func(reg *account.Registry) error {
		return reg.Add("alice", "alice@example.com", "some description")
	}); err != nil {
		t.Fatalf("register account: %v", err)
	}

	accountListJSON = true
	out := captureStdout(t, func() {
		if err := runAccountList(nil, nil); err != nil {
			t.Fatalf("runAccountList: %v", err)
		}
	})

	if !json.Valid([]byte(out)) {
		t.Fatalf("expected valid JSON, got: %s", out)
	}

	var entries []accountEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Email != "alice@example.com" {
		t.Errorf("email dropped from --json output: %+v", entries[0])
	}
	if entries[0].Description != "some description" {
		t.Errorf("description dropped from --json output: %+v", entries[0])
	}
}

// TestAccountRemoveCommandIsHiddenAlias verifies the rename: the delete
// command is registered, the remove command is still present but hidden,
// and both share the same flag set so --confirm/--force behave identically.
func TestAccountRemoveCommandIsHiddenAlias(t *testing.T) {
	if accountDeleteCmd.Use != "delete <handle>" {
		t.Errorf("expected delete command Use, got %q", accountDeleteCmd.Use)
	}
	if accountRemoveCmd.Use != "remove <handle>" {
		t.Errorf("expected remove command Use, got %q", accountRemoveCmd.Use)
	}
	if !accountRemoveCmd.Hidden {
		t.Errorf("account remove alias must be hidden")
	}
	if !strings.Contains(strings.ToLower(accountRemoveCmd.Short), "deprecated") {
		t.Errorf("remove alias Short should mention deprecation, got %q", accountRemoveCmd.Short)
	}

	// Both must be registered under `sol account`.
	var sawDelete, sawRemove bool
	for _, c := range accountCmd.Commands() {
		switch c.Name() {
		case "delete":
			sawDelete = true
		case "remove":
			sawRemove = true
		}
	}
	if !sawDelete {
		t.Errorf("'sol account delete' not registered")
	}
	if !sawRemove {
		t.Errorf("'sol account remove' alias not registered")
	}
}

// TestAccountDeleteAndRemoveAliasBothWork verifies that both the new
// `sol account delete` verb and the deprecated `sol account remove` alias
// successfully delete an account via runAccountDelete.
func TestAccountDeleteAndRemoveAliasBothWork(t *testing.T) {
	setupAccountHome(t)

	// Create two accounts so we can exercise both commands.
	if err := account.LockedRegistryUpdate(func(reg *account.Registry) error {
		if err := reg.Add("alice", "", ""); err != nil {
			return err
		}
		return reg.Add("bob", "", "")
	}); err != nil {
		t.Fatalf("register accounts: %v", err)
	}

	// Move default away from alice so it can be removed.
	if err := account.LockedRegistryUpdate(func(reg *account.Registry) error {
		return reg.SetDefault("bob")
	}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	// Delete via the new verb.
	out := captureStdout(t, func() {
		if err := runAccountDelete("alice", true, false); err != nil {
			t.Fatalf("runAccountDelete(alice): %v", err)
		}
	})
	if !strings.Contains(out, `Removed account "alice"`) {
		t.Errorf("expected success message for alice, got:\n%s", out)
	}

	// Delete via the deprecated alias code path.
	out = captureStdout(t, func() {
		if err := runAccountDelete("bob", true, false); err != nil {
			t.Fatalf("runAccountDelete(bob): %v", err)
		}
	})
	if !strings.Contains(out, `Removed account "bob"`) {
		t.Errorf("expected success message for bob, got:\n%s", out)
	}

	// Registry should now be empty.
	reg, err := account.LoadRegistry()
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if len(reg.Accounts) != 0 {
		t.Errorf("expected empty registry after deletes, got %+v", reg.Accounts)
	}
}
