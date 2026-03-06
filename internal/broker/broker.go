package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/account"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
)

// DefaultRefreshMargin is how far before token expiry the broker should
// proactively refresh. Default: 30 minutes before expiresAt.
const DefaultRefreshMargin = 30 * time.Minute

// DefaultPatrolInterval is how often the broker checks token expiry.
const DefaultPatrolInterval = 5 * time.Minute

// Config holds broker configuration.
type Config struct {
	RefreshMargin  time.Duration
	PatrolInterval time.Duration
}

// Heartbeat records the broker's last patrol status.
type Heartbeat struct {
	Timestamp    time.Time `json:"timestamp"`
	PatrolCount  int       `json:"patrol_count"`
	Status       string    `json:"status"` // "running", "stopping"
	Accounts     int       `json:"accounts"`
	AgentDirs    int       `json:"agent_dirs"`
	Refreshed    int       `json:"refreshed"`
	Errors       int       `json:"errors"`
	LastRefresh  string    `json:"last_refresh,omitempty"` // account handle
}

// Broker manages OAuth token refresh for all accounts.
type Broker struct {
	cfg       Config
	logger    *events.Logger
	refreshFn RefreshFn

	patrolCount int
}

// New creates a new Broker.
func New(cfg Config, logger *events.Logger) *Broker {
	if cfg.RefreshMargin == 0 {
		cfg.RefreshMargin = DefaultRefreshMargin
	}
	if cfg.PatrolInterval == 0 {
		cfg.PatrolInterval = DefaultPatrolInterval
	}

	return &Broker{
		cfg:       cfg,
		logger:    logger,
		refreshFn: RefreshOAuthToken,
	}
}

// SetRefreshFn overrides the token refresh function (for testing).
func (b *Broker) SetRefreshFn(fn RefreshFn) {
	b.refreshFn = fn
}

// Run starts the broker loop. Blocks until context is cancelled.
func (b *Broker) Run(ctx context.Context) error {
	fmt.Fprintf(os.Stderr, "Token broker starting (patrol every %s, refresh margin %s)\n",
		b.cfg.PatrolInterval, b.cfg.RefreshMargin)

	// Initial patrol immediately.
	b.patrol()

	ticker := time.NewTicker(b.cfg.PatrolInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			b.writeHeartbeat("stopping", 0, 0, 0, 0, "")
			fmt.Fprintln(os.Stderr, "Token broker stopping")
			return nil
		case <-ticker.C:
			b.patrol()
		}
	}
}

// patrol performs one refresh cycle:
// 1. Load all account credentials
// 2. Check which need refreshing (approaching expiry)
// 3. Refresh tokens
// 4. Write updated access-token-only creds to all agent config dirs
func (b *Broker) patrol() {
	b.patrolCount++

	registry, err := account.LoadRegistry()
	if err != nil {
		fmt.Fprintf(os.Stderr, "broker: failed to load account registry: %v\n", err)
		b.writeHeartbeat("running", 0, 0, 0, 1, "")
		return
	}

	if len(registry.Accounts) == 0 {
		b.writeHeartbeat("running", 0, 0, 0, 0, "")
		return
	}

	var totalDirs, refreshed, errors int
	var lastRefresh string

	for handle := range registry.Accounts {
		credsPath := filepath.Join(config.AccountDir(handle), ".credentials.json")
		creds, err := ReadCredentials(credsPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "broker: failed to read credentials for account %q: %v\n", handle, err)
			errors++
			continue
		}

		if creds.ClaudeAIOAuth == nil {
			continue
		}

		// Check if token needs refreshing.
		timeUntilExpiry := creds.ClaudeAIOAuth.TimeUntilExpiry()
		needsRefresh := timeUntilExpiry < b.cfg.RefreshMargin

		if needsRefresh && creds.ClaudeAIOAuth.RefreshToken != "" {
			fmt.Fprintf(os.Stderr, "broker: refreshing token for account %q (expires in %s)\n",
				handle, timeUntilExpiry.Round(time.Second))

			resp, err := b.refreshFn(creds.ClaudeAIOAuth.RefreshToken)
			if err != nil {
				fmt.Fprintf(os.Stderr, "broker: failed to refresh token for account %q: %v\n", handle, err)
				errors++
				if b.logger != nil {
					b.logger.Emit(events.EventBrokerRefresh, "broker", handle, "audit",
						map[string]any{"account": handle, "error": err.Error()})
				}
				continue
			}

			// Apply refresh response to source credentials.
			ApplyRefreshResponse(creds, resp)

			// Write updated source credentials (with new refresh token).
			if err := WriteCredentials(credsPath, creds); err != nil {
				fmt.Fprintf(os.Stderr, "broker: failed to write source credentials for account %q: %v\n", handle, err)
				errors++
				continue
			}

			refreshed++
			lastRefresh = handle

			if b.logger != nil {
				b.logger.Emit(events.EventBrokerRefresh, "broker", handle, "audit",
					map[string]any{
						"account":    handle,
						"expires_at": creds.ClaudeAIOAuth.ExpiresAtTime().Format(time.RFC3339),
					})
			}
		}

		// Write access-token-only credentials to all agent config dirs
		// that use this account. Do this on every patrol (not just after refresh)
		// to pick up new agents and account rotations.
		dirs, err := b.discoverAgentDirs(handle)
		if err != nil {
			fmt.Fprintf(os.Stderr, "broker: failed to discover agent dirs for account %q: %v\n", handle, err)
			errors++
			continue
		}

		totalDirs += len(dirs)
		accessOnly := creds.AccessTokenOnly()

		for _, dir := range dirs {
			destPath := filepath.Join(dir, ".credentials.json")
			if err := WriteCredentials(destPath, accessOnly); err != nil {
				fmt.Fprintf(os.Stderr, "broker: failed to write access token to %s: %v\n", dir, err)
				errors++
			}
		}
	}

	b.writeHeartbeat("running", len(registry.Accounts), totalDirs, refreshed, errors, lastRefresh)

	if b.logger != nil {
		b.logger.Emit(events.EventBrokerPatrol, "broker", "broker", "audit",
			map[string]any{
				"patrol_count": b.patrolCount,
				"accounts":     len(registry.Accounts),
				"agent_dirs":   totalDirs,
				"refreshed":    refreshed,
				"errors":       errors,
			})
	}
}

// discoverAgentDirs finds all agent config directories that use the given
// account handle. It scans all worlds' .claude-config/ directories for
// .account files matching the handle.
func (b *Broker) discoverAgentDirs(handle string) ([]string, error) {
	home := config.Home()
	var dirs []string

	// Walk top-level entries in SOL_HOME to find worlds.
	entries, err := os.ReadDir(home)
	if err != nil {
		return nil, fmt.Errorf("failed to read SOL_HOME: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		worldDir := filepath.Join(home, entry.Name())
		claudeConfigDir := filepath.Join(worldDir, ".claude-config")

		worldDirs, err := scanClaudeConfigDir(claudeConfigDir, handle)
		if err != nil {
			continue // world may not have .claude-config
		}
		dirs = append(dirs, worldDirs...)
	}

	// Also check sphere-scoped config dirs (senate).
	sphereConfigDir := filepath.Join(home, ".claude-config")
	sphereDirs, err := scanClaudeConfigDir(sphereConfigDir, handle)
	if err == nil {
		dirs = append(dirs, sphereDirs...)
	}

	return dirs, nil
}

// scanClaudeConfigDir scans a .claude-config directory tree for agent dirs
// whose .account file matches the given handle.
func scanClaudeConfigDir(baseDir, handle string) ([]string, error) {
	var dirs []string

	roleDirs, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, err
	}

	for _, roleEntry := range roleDirs {
		if !roleEntry.IsDir() {
			continue
		}

		rolePath := filepath.Join(baseDir, roleEntry.Name())
		agentDirs, err := os.ReadDir(rolePath)
		if err != nil {
			continue
		}

		for _, agentEntry := range agentDirs {
			if !agentEntry.IsDir() {
				continue
			}

			agentPath := filepath.Join(rolePath, agentEntry.Name())
			acctHandle := ReadAccountFile(agentPath)
			if acctHandle == handle {
				dirs = append(dirs, agentPath)
			}
		}
	}

	return dirs, nil
}

// ReadAccountFile reads the .account file in an agent config dir.
// Returns the account handle, or "" if not found.
func ReadAccountFile(configDir string) string {
	data, err := os.ReadFile(filepath.Join(configDir, ".account"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// WriteAccountFile writes the .account metadata file to an agent config dir.
func WriteAccountFile(configDir, handle string) error {
	path := filepath.Join(configDir, ".account")
	return os.WriteFile(path, []byte(handle+"\n"), 0o644)
}

// heartbeatPath returns the path to the broker heartbeat file.
func heartbeatPath() string {
	return filepath.Join(config.Home(), ".runtime", "broker-heartbeat.json")
}

func (b *Broker) writeHeartbeat(status string, accounts, agentDirs, refreshed, errors int, lastRefresh string) {
	hb := Heartbeat{
		Timestamp:   time.Now().UTC(),
		PatrolCount: b.patrolCount,
		Status:      status,
		Accounts:    accounts,
		AgentDirs:   agentDirs,
		Refreshed:   refreshed,
		Errors:      errors,
		LastRefresh: lastRefresh,
	}

	data, err := json.MarshalIndent(hb, "", "  ")
	if err != nil {
		return
	}

	dir := filepath.Dir(heartbeatPath())
	os.MkdirAll(dir, 0o755)

	tmp := heartbeatPath() + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return
	}
	os.Rename(tmp, heartbeatPath())
}

// ReadHeartbeat reads the broker's heartbeat file.
// Returns nil if not found.
func ReadHeartbeat() (*Heartbeat, error) {
	data, err := os.ReadFile(heartbeatPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var hb Heartbeat
	if err := json.Unmarshal(data, &hb); err != nil {
		return nil, err
	}
	return &hb, nil
}

// IsStale returns true if the heartbeat is older than the given max age.
func (h *Heartbeat) IsStale(maxAge time.Duration) bool {
	return time.Since(h.Timestamp) > maxAge
}
