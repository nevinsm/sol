package quota

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/fileutil"
)

// Status represents the quota state of an account.
type Status string

const (
	Available Status = "available"
	Limited   Status = "limited"
	Cooldown  Status = "cooldown"
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

// rateLimitPatterns match Claude rate limit error messages in pane output.
var rateLimitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`You've hit your .*limit`),
	regexp.MustCompile(`limit\s*·\s*resets\s+\d+[:\d]*(am|pm)`),
	regexp.MustCompile(`Stop and wait for limit to reset`),
	regexp.MustCompile(`API Error:\s*Rate limit reached`),
	regexp.MustCompile(`OAuth token revoked`),
	regexp.MustCompile(`OAuth token has expired`),
}

// resetTimePattern extracts a reset time from rate limit messages.
var resetTimePattern = regexp.MustCompile(`resets\s+(\d{1,2}(?::\d{2})?\s*(?:am|pm))`)

// DetectRateLimit checks pane output for rate limit patterns.
// Returns true if a rate limit is detected, along with any parsed reset time.
func DetectRateLimit(output string) (limited bool, resetsAt *time.Time) {
	for _, pat := range rateLimitPatterns {
		if pat.MatchString(output) {
			limited = true
			break
		}
	}
	if !limited {
		return false, nil
	}

	// Try to parse reset time.
	if m := resetTimePattern.FindStringSubmatch(output); len(m) > 1 {
		if t, err := parseResetTime(m[1]); err == nil {
			resetsAt = &t
		}
	}

	return limited, resetsAt
}

// parseResetTime parses a time string like "3:45pm" or "4am" into a time.Time
// on the current day (or next day if the time has already passed).
func parseResetTime(s string) (time.Time, error) {
	s = strings.TrimSpace(strings.ToLower(s))

	var hour, minute int
	var ampm string

	// Try "3:45pm" format first, then "4pm" format.
	if n, _ := fmt.Sscanf(s, "%d:%d%s", &hour, &minute, &ampm); n == 3 {
		// parsed
	} else if n, _ := fmt.Sscanf(s, "%d%s", &hour, &ampm); n == 2 {
		minute = 0
	} else {
		return time.Time{}, fmt.Errorf("cannot parse reset time %q", s)
	}

	ampm = strings.TrimSpace(ampm)
	if ampm == "pm" && hour != 12 {
		hour += 12
	} else if ampm == "am" && hour == 12 {
		hour = 0
	}

	now := time.Now()
	reset := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if reset.Before(now) {
		reset = reset.Add(24 * time.Hour)
	}

	return reset.UTC(), nil
}

// statePath returns the path to the quota state file.
func statePath() string {
	return filepath.Join(config.RuntimeDir(), "quota.json")
}

// lockPath returns the path to the quota lock file.
func lockPath() string {
	return filepath.Join(config.RuntimeDir(), "quota.lock")
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

// Save writes the quota state to disk with flock protection.
func Save(state *State) error {
	dir := config.RuntimeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create quota runtime directory: %w", err)
	}

	// Acquire flock.
	lockFile, err := os.OpenFile(lockPath(), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open quota lock: %w", err)
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("failed to acquire quota lock: %w", err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

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

// ExpireCooldowns transitions limited accounts to available if their
// reset time has passed.
func (s *State) ExpireCooldowns() {
	now := time.Now().UTC()
	for _, acct := range s.Accounts {
		if acct.Status == Limited && acct.ResetsAt != nil && now.After(*acct.ResetsAt) {
			acct.Status = Available
			acct.LimitedAt = nil
			acct.ResetsAt = nil
		}
	}
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
