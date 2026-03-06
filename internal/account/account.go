package account

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/nevinsm/sol/internal/config"
)

// Account represents a registered Claude OAuth account.
type Account struct {
	Email       string `json:"email,omitempty"`
	Description string `json:"description,omitempty"`
	ConfigDir   string `json:"config_dir"`
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create accounts directory: %w", err)
	}

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal accounts registry: %w", err)
	}

	path := registryPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("failed to write accounts registry: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to commit accounts registry: %w", err)
	}
	return nil
}

// Add registers a new account. Creates the account config directory.
// If this is the first account, it becomes the default.
func (r *Registry) Add(handle, email, description string) error {
	if err := ValidateHandle(handle); err != nil {
		return err
	}
	if _, exists := r.Accounts[handle]; exists {
		return fmt.Errorf("account %q already exists", handle)
	}

	configDir := config.AccountDir(handle)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("failed to create account directory: %w", err)
	}

	r.Accounts[handle] = Account{
		Email:       email,
		Description: description,
		ConfigDir:   configDir,
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
