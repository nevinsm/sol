package account

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"syscall"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/fileutil"
)

// quotaState is a minimal representation of quota.json for account cleanup.
// Using a local type avoids an import cycle (quota → startup → account).
type quotaState struct {
	Accounts map[string]json.RawMessage `json:"accounts"`
}

// removeFromQuotaState removes a handle from quota.json.
// Errors are intentionally ignored — quota state is best-effort.
// Uses map[string]json.RawMessage to preserve all top-level fields during round-trip.
func removeFromQuotaState(handle string) {
	statePath := filepath.Join(config.RuntimeDir(), "quota.json")
	lockPath := filepath.Join(config.RuntimeDir(), "quota.json.lock")

	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) //nolint:errcheck

	data, err := os.ReadFile(statePath)
	if err != nil {
		return // file may not exist yet — nothing to clean up
	}

	// Unmarshal into a generic map to preserve all top-level fields (e.g., paused_sessions).
	var state map[string]json.RawMessage
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}

	accountsRaw, exists := state["accounts"]
	if !exists {
		return // no accounts key — nothing to clean up
	}

	var accounts map[string]json.RawMessage
	if err := json.Unmarshal(accountsRaw, &accounts); err != nil {
		return
	}
	if _, exists := accounts[handle]; !exists {
		return // nothing to remove
	}

	delete(accounts, handle)

	updatedAccounts, err := json.Marshal(accounts)
	if err != nil {
		return
	}
	state["accounts"] = json.RawMessage(updatedAccounts)

	updated, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	_ = fileutil.AtomicWrite(statePath, append(updated, '\n'), 0o644)
}

// Account represents a registered Claude OAuth account.
type Account struct {
	Email       string `json:"email,omitempty"`
	Description string `json:"description,omitempty"`
}

// Registry holds the account registry persisted in accounts.json.
type Registry struct {
	Accounts map[string]Account `json:"accounts"`
	Default  string             `json:"default"`
}

var validHandle = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._-]*$`)

const maxHandleLen = 64

// ValidateHandle checks that an account handle is valid.
func ValidateHandle(handle string) error {
	if handle == "" {
		return fmt.Errorf("account handle must not be empty")
	}
	if len(handle) > maxHandleLen {
		return fmt.Errorf("account handle %q is too long (%d chars, max %d)", handle, len(handle), maxHandleLen)
	}
	if !validHandle.MatchString(handle) {
		return fmt.Errorf("invalid account handle %q: must start with a letter and contain only [a-zA-Z0-9._-]", handle)
	}
	return nil
}

func registryPath() string {
	return filepath.Join(config.AccountsDir(), "accounts.json")
}

// LoadRegistry reads the account registry from disk.
// Returns an empty registry if the file does not exist.
func LoadRegistry() (*Registry, error) {
	path := registryPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Registry{Accounts: make(map[string]Account)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read accounts registry: %w", err)
	}

	var reg Registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("failed to parse accounts registry: %w", err)
	}
	if reg.Accounts == nil {
		reg.Accounts = make(map[string]Account)
	}
	return &reg, nil
}

// Save writes the registry to disk, creating the directory if needed.
func (r *Registry) Save() error {
	dir := config.AccountsDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create accounts directory: %w", err)
	}

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal accounts registry: %w", err)
	}

	if err := fileutil.AtomicWrite(registryPath(), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("failed to write accounts registry: %w", err)
	}
	return nil
}

// LockedRegistryUpdate performs an atomic load-modify-save cycle on the account
// registry under an exclusive file lock. This prevents concurrent operations
// (e.g., two simultaneous `sol account add` calls) from racing on the
// read-modify-write cycle and losing data.
func LockedRegistryUpdate(fn func(*Registry) error) error {
	dir := config.AccountsDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create accounts directory: %w", err)
	}

	lockPath := registryPath() + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open registry lock file: %w", err)
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("failed to acquire registry lock: %w", err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) //nolint:errcheck

	reg, err := LoadRegistry()
	if err != nil {
		return err
	}

	if err := fn(reg); err != nil {
		return err
	}

	return reg.Save()
}

// Add registers a new account. Creates the account config directory.
// If this is the first account, it becomes the default.
func (r *Registry) Add(handle, email, description string) error {
	if err := ValidateHandle(handle); err != nil {
		return fmt.Errorf("failed to validate account handle: %w", err)
	}
	if _, exists := r.Accounts[handle]; exists {
		return fmt.Errorf("account %q already exists", handle)
	}

	configDir := config.AccountDir(handle)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("failed to create account directory: %w", err)
	}

	r.Accounts[handle] = Account{
		Email:       email,
		Description: description,
	}

	if len(r.Accounts) == 1 {
		r.Default = handle
	}

	return nil
}

// Remove removes an account from the registry and deletes its config directory.
// Refuses to remove the default account unless it's the last one.
func (r *Registry) Remove(handle string) error {
	if _, exists := r.Accounts[handle]; !exists {
		return fmt.Errorf("account %q not found", handle)
	}

	if r.Default == handle && len(r.Accounts) > 1 {
		return fmt.Errorf("cannot remove default account %q — set a different default first, or remove all other accounts", handle)
	}

	configDir := config.AccountDir(handle)
	if err := os.RemoveAll(configDir); err != nil {
		return fmt.Errorf("failed to remove account directory: %w", err)
	}

	delete(r.Accounts, handle)

	if r.Default == handle {
		r.Default = ""
	}

	// Remove the account from quota state so it is not returned as available.
	removeFromQuotaState(handle)

	return nil
}

// SetDefault sets the default account.
func (r *Registry) SetDefault(handle string) error {
	if _, exists := r.Accounts[handle]; !exists {
		return fmt.Errorf("account %q not found", handle)
	}
	r.Default = handle
	return nil
}

// ResolveAccount determines the account to use for credential provisioning.
// Resolution priority:
//  1. flagValue — explicit per-dispatch override (e.g., sol cast --account=personal)
//  2. worldDefault — world.toml default_account setting
//  3. Registry default — sphere-level default from sol account default
//  4. "" — no account configured, caller falls back to ~/.claude/.credentials.json
func ResolveAccount(flagValue, worldDefault string) string {
	if flagValue != "" {
		return flagValue
	}
	if worldDefault != "" {
		return worldDefault
	}
	reg, err := LoadRegistry()
	if err != nil || reg.Default == "" {
		return ""
	}
	return reg.Default
}
