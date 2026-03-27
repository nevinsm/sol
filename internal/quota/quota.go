package quota

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/broker"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/fileutil"
)

// Status represents the quota state of an account.
type Status string

const (
	Available Status = "available"
	Limited   Status = "limited"
)

// AccountState tracks the quota state for a single account.
type AccountState struct {
	Status    Status     `json:"status"`
	LimitedAt *time.Time `json:"limited_at,omitempty"`
	ResetsAt  *time.Time `json:"resets_at,omitempty"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
}

// PausedSession records an agent session paused due to no available accounts.
type PausedSession struct {
	PausedAt        time.Time `json:"paused_at"`
	PreviousAccount string    `json:"previous_account"`
	Writ        string    `json:"writ,omitempty"`
	World           string    `json:"world"`
	AgentName       string    `json:"agent_name"`
	Role            string    `json:"role"`
}

// State holds quota state for all accounts.
type State struct {
	Accounts       map[string]*AccountState `json:"accounts"`
	PausedSessions map[string]PausedSession  `json:"paused_sessions,omitempty"` // keyed by agent ID ("world/name")
}

// DetectRateLimit checks pane output for rate limit patterns by delegating to
// the registered provider. Falls back to the "claude" provider if no runtime
// is specified. Returns true if a rate limit is detected, along with any
// parsed reset time.
func DetectRateLimit(output string) (limited bool, resetsAt *time.Time) {
	return DetectRateLimitForRuntime(output, "")
}

// DetectRateLimitForRuntime checks pane output for rate limit patterns using the
// provider registered for the given runtime. If runtime is empty, defaults to "claude".
// Returns true if a rate limit is detected, along with any parsed reset time.
func DetectRateLimitForRuntime(output, runtime string) (limited bool, resetsAt *time.Time) {
	if runtime == "" {
		runtime = "claude"
	}
	provider, ok := broker.GetProvider(runtime)
	if !ok {
		return false, nil
	}

	signal := provider.DetectRateLimit(output)
	if signal == nil {
		return false, nil
	}

	if !signal.ResetsAt.IsZero() {
		t := signal.ResetsAt
		return true, &t
	}
	return true, nil
}

// statePath returns the path to the quota state file.
func statePath() string {
	return filepath.Join(config.RuntimeDir(), "quota.json")
}

// Load reads the quota state from disk.
// Returns an empty state if the file does not exist.
func Load() (*State, error) {
	data, err := os.ReadFile(statePath())
	if os.IsNotExist(err) {
		return &State{
			Accounts:       make(map[string]*AccountState),
			PausedSessions: make(map[string]PausedSession),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read quota state: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse quota state: %w", err)
	}
	if state.Accounts == nil {
		state.Accounts = make(map[string]*AccountState)
	}
	if state.PausedSessions == nil {
		state.PausedSessions = make(map[string]PausedSession)
	}
	return &state, nil
}

// Save writes the quota state to disk. Callers MUST hold AcquireLock() before
// calling Save to ensure mutual exclusion across all quota state mutations.
func Save(state *State) error {
	dir := config.RuntimeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create quota runtime directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal quota state: %w", err)
	}
	if err := fileutil.AtomicWrite(statePath(), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("failed to write quota state: %w", err)
	}
	return nil
}

// MarkLimited marks an account as rate-limited.
func (s *State) MarkLimited(handle string, resetsAt *time.Time) {
	now := time.Now().UTC()
	acct := s.ensureAccount(handle)
	acct.Status = Limited
	acct.LimitedAt = &now
	acct.ResetsAt = resetsAt
}

// MarkAvailable marks an account as available.
func (s *State) MarkAvailable(handle string) {
	acct := s.ensureAccount(handle)
	acct.Status = Available
	acct.LimitedAt = nil
	acct.ResetsAt = nil
}

// TouchLastUsed updates the last_used timestamp for an account.
func (s *State) TouchLastUsed(handle string) {
	now := time.Now().UTC()
	acct := s.ensureAccount(handle)
	acct.LastUsed = &now
}


func (s *State) ensureAccount(handle string) *AccountState {
	if s.Accounts == nil {
		s.Accounts = make(map[string]*AccountState)
	}
	if s.Accounts[handle] == nil {
		s.Accounts[handle] = &AccountState{Status: Available}
	}
	return s.Accounts[handle]
}
