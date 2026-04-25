package account

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

	// Clear stale PreviousAccount references in paused_sessions.
	if pausedRaw, exists := state["paused_sessions"]; exists {
		var paused map[string]json.RawMessage
		if err := json.Unmarshal(pausedRaw, &paused); err == nil {
			changed := false
			for key, entry := range paused {
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(entry, &fields); err != nil {
					continue
				}
				prevRaw, exists := fields["previous_account"]
				if !exists {
					continue
				}
				var prev string
				if err := json.Unmarshal(prevRaw, &prev); err != nil {
					continue
				}
				if prev == handle {
					fields["previous_account"] = json.RawMessage(`""`)
					if updated, err := json.Marshal(fields); err == nil {
						paused[key] = json.RawMessage(updated)
						changed = true
					}
				}
			}
			if changed {
				if updatedPaused, err := json.Marshal(paused); err == nil {
					state["paused_sessions"] = json.RawMessage(updatedPaused)
				}
			}
		}
	}

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

// RemoveOpts controls the behavior of Registry.Remove.
type RemoveOpts struct {
	// Force, if true, allows removal even when live bindings are detected.
	// Bindings are still computed and returned to the caller so it can warn.
	Force bool
}

// Binding describes a place where an account is currently in use. Returned
// from FindBindings and Registry.Remove. Each binding is a single live
// reference (quota state entry, world default_account, agent claude-config).
type Binding struct {
	// Kind is one of: "quota_state", "world_default", "agent_config".
	Kind string
	// World is the world name for world-scoped bindings; empty for sphere
	// bindings (e.g. quota_state).
	World string
	// Detail is a short human-readable description of the binding location.
	Detail string
}

// String renders a binding as a single human-readable line.
func (b Binding) String() string {
	if b.World == "" {
		return fmt.Sprintf("%s: %s", b.Kind, b.Detail)
	}
	return fmt.Sprintf("%s (world %q): %s", b.Kind, b.World, b.Detail)
}

// FormatBindings renders a slice of bindings as a bullet list, one per line.
func FormatBindings(bindings []Binding) string {
	var sb strings.Builder
	for i, b := range bindings {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("  - ")
		sb.WriteString(b.String())
	}
	return sb.String()
}

// FindBindings scans for live references to an account handle. The scan
// covers (in order):
//
//  1. quota.json — does the runtime quota state track the handle?
//  2. World default_account — does any world's world.toml name the handle as
//     default_account?
//  3. Agent claude-config — does any agent's .claude-config metadata file
//     (.account, with .credentials.json symlink fallback) point at the
//     handle?
//
// Returns nil if the account is not bound anywhere. Errors are returned only
// for I/O failures that are likely to indicate a corrupted SOL_HOME; missing
// files (e.g. no quota.json yet, no worlds yet) are treated as "no binding".
func FindBindings(handle string) ([]Binding, error) {
	var bindings []Binding

	// 1. Quota state.
	if inQuota, err := accountInQuotaState(handle); err == nil && inQuota {
		bindings = append(bindings, Binding{
			Kind:   "quota_state",
			Detail: "tracked in quota.json (run `sol quota status` to inspect)",
		})
	}

	// 2 & 3. Walk worlds.
	home := config.Home()
	entries, err := os.ReadDir(home)
	if err != nil {
		if os.IsNotExist(err) {
			return bindings, nil
		}
		return bindings, fmt.Errorf("failed to scan SOL_HOME for world bindings: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Skip dotfiles (.runtime, .store, .accounts, .claude-defaults).
		if strings.HasPrefix(name, ".") {
			continue
		}
		worldDir := filepath.Join(home, name)
		// A directory is a world only if it contains world.toml.
		if _, err := os.Stat(filepath.Join(worldDir, "world.toml")); err != nil {
			continue
		}
		world := name

		// World default_account.
		if cfg, err := config.LoadWorldConfig(world); err == nil {
			if cfg.World.DefaultAccount == handle {
				bindings = append(bindings, Binding{
					Kind:   "world_default",
					World:  world,
					Detail: "set as default_account in world.toml",
				})
			}
		}

		// Agent claude-config bindings.
		bindings = append(bindings, walkClaudeConfigForAccount(worldDir, world, handle)...)
	}

	return bindings, nil
}

// accountInQuotaState reports whether the handle is tracked in quota.json.
// Reads the file directly to avoid an account → quota import cycle (quota
// imports broker which imports account).
func accountInQuotaState(handle string) (bool, error) {
	statePath := filepath.Join(config.RuntimeDir(), "quota.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var state struct {
		Accounts map[string]json.RawMessage `json:"accounts"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return false, err
	}
	_, ok := state.Accounts[handle]
	return ok, nil
}

// walkClaudeConfigForAccount returns bindings for any agent claude-config
// directory under worldDir whose .account metadata (or .credentials.json
// symlink) points at handle. Walks $worldDir/.claude-config/<roleDir>/<agent>/.
func walkClaudeConfigForAccount(worldDir, world, handle string) []Binding {
	var bindings []Binding
	base := filepath.Join(worldDir, ".claude-config")
	roleDirs, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	for _, rd := range roleDirs {
		if !rd.IsDir() {
			continue
		}
		rolePath := filepath.Join(base, rd.Name())
		agents, err := os.ReadDir(rolePath)
		if err != nil {
			continue
		}
		for _, a := range agents {
			if !a.IsDir() {
				continue
			}
			agentDir := filepath.Join(rolePath, a.Name())
			if h := readAgentAccount(agentDir); h == handle {
				bindings = append(bindings, Binding{
					Kind:   "agent_config",
					World:  world,
					Detail: fmt.Sprintf("%s/%s", rd.Name(), a.Name()),
				})
			}
		}
	}
	return bindings
}

// readAgentAccount reads the account handle bound to an agent's claude-config
// directory. Prefers the .account metadata file (broker-managed); falls back
// to extracting the handle from the .credentials.json symlink target.
// Returns "" if no binding can be determined.
func readAgentAccount(configDir string) string {
	if data, err := os.ReadFile(filepath.Join(configDir, ".account")); err == nil {
		return strings.TrimSpace(string(data))
	}
	target, err := os.Readlink(filepath.Join(configDir, ".credentials.json"))
	if err != nil {
		return ""
	}
	accountsDir := config.AccountsDir()
	rel, err := filepath.Rel(accountsDir, target)
	if err != nil {
		return ""
	}
	parts := strings.SplitN(rel, string(filepath.Separator), 2)
	if len(parts) != 2 || parts[1] != ".credentials.json" {
		return ""
	}
	h := parts[0]
	if h == "" || h == "." || h == ".." || strings.Contains(h, "/") {
		return ""
	}
	return h
}

// Remove removes an account from the registry and deletes its config
// directory.
//
// Refuses to remove the default account unless it's the last one. Refuses to
// remove an account with live bindings (quota state, world default_account,
// or agent claude-config) unless opts.Force is set. The discovered bindings
// are returned alongside both the success and refusal paths so the caller can
// log warnings.
func (r *Registry) Remove(handle string, opts RemoveOpts) ([]Binding, error) {
	if _, exists := r.Accounts[handle]; !exists {
		return nil, fmt.Errorf("account %q not found", handle)
	}

	if r.Default == handle && len(r.Accounts) > 1 {
		return nil, fmt.Errorf("cannot remove default account %q — set a different default first, or remove all other accounts", handle)
	}

	bindings, err := FindBindings(handle)
	if err != nil {
		// Surface the I/O error but don't proceed with deletion: a partial
		// scan could miss bindings and leave dangling references.
		return nil, fmt.Errorf("failed to scan for live bindings: %w", err)
	}

	if len(bindings) > 0 && !opts.Force {
		return bindings, fmt.Errorf(
			"account %q has %d live binding(s); refusing to remove without --force:\n%s",
			handle, len(bindings), FormatBindings(bindings))
	}

	configDir := config.AccountDir(handle)
	if err := os.RemoveAll(configDir); err != nil {
		return bindings, fmt.Errorf("failed to remove account directory: %w", err)
	}

	delete(r.Accounts, handle)

	if r.Default == handle {
		r.Default = ""
	}

	// Remove the account from quota state so it is not returned as available.
	removeFromQuotaState(handle)

	return bindings, nil
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
	if err != nil {
		fmt.Fprintf(os.Stderr, "account: failed to load registry for account resolution: %v\n", err)
		return ""
	}
	if reg.Default == "" {
		return ""
	}
	return reg.Default
}
