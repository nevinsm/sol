package quota

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
)

// RotateOpts holds options for a quota rotation.
type RotateOpts struct {
	World  string
	DryRun bool
}

// RotationAction describes a single credential swap planned or executed.
type RotationAction struct {
	AgentID     string `json:"agent_id"`
	AgentName   string `json:"agent_name"`
	FromAccount string `json:"from_account"`
	ToAccount   string `json:"to_account"`
	Paused      bool   `json:"paused,omitempty"` // true if no account was available
}

// RotateResult summarizes the rotation outcome.
type RotateResult struct {
	Actions []RotationAction
	Expired []string // accounts whose limits expired during this run
}

// Rotate performs quota rotation: finds agents on limited accounts, swaps
// their credential symlinks to available accounts, and respawns their
// sessions with --continue for context preservation.
//
// The entire operation is protected by a flock on quota.json.
func Rotate(opts RotateOpts, sphereStore *store.Store, mgr *session.Manager, logger *events.Logger) (*RotateResult, error) {
	lock, state, err := AcquireLock()
	if err != nil {
		return nil, fmt.Errorf("failed to acquire quota lock: %w", err)
	}
	defer lock.Release()

	result := &RotateResult{}

	// Expire any limits whose resets_at has passed.
	result.Expired = state.ExpireLimits()

	// Find limited accounts.
	limitedAccounts := state.LimitedAccounts()
	if len(limitedAccounts) == 0 {
		// No limited accounts — check if there are paused sessions to restart.
		if len(result.Expired) > 0 {
			if err := restartPausedSessions(state, opts, sphereStore, mgr, logger, result); err != nil {
				return result, err
			}
			if err := Save(state); err != nil {
				return result, fmt.Errorf("failed to save quota state: %w", err)
			}
		}
		return result, nil
	}

	// Build set of limited account handles for fast lookup.
	limitedSet := make(map[string]bool, len(limitedAccounts))
	for _, h := range limitedAccounts {
		limitedSet[h] = true
	}

	// Find working agents in this world.
	agents, err := sphereStore.ListAgents(opts.World, "working")
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	// Filter to outpost agents (not governor, senate, envoy, forge).
	var affectedAgents []store.Agent
	for _, a := range agents {
		if a.Role != "agent" {
			continue
		}
		// Resolve which account this agent is currently using.
		acctHandle := resolveCurrentAccount(opts.World, a.Name, a.Role)
		if limitedSet[acctHandle] {
			affectedAgents = append(affectedAgents, a)
		}
	}

	if len(affectedAgents) == 0 {
		if err := Save(state); err != nil {
			return result, fmt.Errorf("failed to save quota state: %w", err)
		}
		return result, nil
	}

	// Get available accounts sorted by LRU.
	availableAccounts := state.AvailableAccountsLRU()
	availIdx := 0

	for _, agent := range affectedAgents {
		fromAccount := resolveCurrentAccount(opts.World, agent.Name, agent.Role)

		if availIdx >= len(availableAccounts) {
			// No more available accounts — pause the agent.
			action := RotationAction{
				AgentID:     agent.ID,
				AgentName:   agent.Name,
				FromAccount: fromAccount,
				Paused:      true,
			}

			if !opts.DryRun {
				if err := pauseAgent(state, agent, fromAccount, opts, sphereStore, mgr, logger); err != nil {
					fmt.Fprintf(os.Stderr, "quota: failed to pause agent %s: %v\n", agent.Name, err)
				}
			}

			result.Actions = append(result.Actions, action)
			continue
		}

		toAccount := availableAccounts[availIdx]
		availIdx++

		action := RotationAction{
			AgentID:     agent.ID,
			AgentName:   agent.Name,
			FromAccount: fromAccount,
			ToAccount:   toAccount,
		}

		if !opts.DryRun {
			if err := swapAndRespawn(state, agent, toAccount, opts, mgr, logger); err != nil {
				fmt.Fprintf(os.Stderr, "quota: failed to rotate agent %s: %v\n", agent.Name, err)
				continue
			}
		}

		result.Actions = append(result.Actions, action)
	}

	// Also restart any paused sessions if accounts became available.
	if err := restartPausedSessions(state, opts, sphereStore, mgr, logger, result); err != nil {
		fmt.Fprintf(os.Stderr, "quota: failed to restart paused sessions: %v\n", err)
	}

	if !opts.DryRun {
		if err := Save(state); err != nil {
			return result, fmt.Errorf("failed to save quota state: %w", err)
		}
	}

	return result, nil
}

// resolveCurrentAccount reads the .credentials.json symlink in an agent's
// CLAUDE_CONFIG_DIR to determine which account it's currently using.
// Returns the account handle, or "" if it can't be resolved.
func resolveCurrentAccount(world, agentName, role string) string {
	worldDir := config.WorldDir(world)
	configDir := config.ClaudeConfigDir(worldDir, role, agentName)
	credLink := filepath.Join(configDir, ".credentials.json")

	target, err := os.Readlink(credLink)
	if err != nil {
		return ""
	}

	// Target is like $SOL_HOME/.accounts/{handle}/.credentials.json
	// Extract the handle from the path.
	accountsDir := config.AccountsDir()
	rel, err := filepath.Rel(accountsDir, target)
	if err != nil {
		return ""
	}

	// rel is like "{handle}/.credentials.json"
	parts := strings.SplitN(rel, string(filepath.Separator), 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// swapAndRespawn replaces the credential symlink and respawns the session.
func swapAndRespawn(state *State, agent store.Agent, toAccount string, opts RotateOpts, mgr *session.Manager, logger *events.Logger) error {
	worldDir := config.WorldDir(opts.World)

	// Swap the credential symlink.
	configDir := config.ClaudeConfigDir(worldDir, agent.Role, agent.Name)
	credLink := filepath.Join(configDir, ".credentials.json")
	newTarget := filepath.Join(config.AccountDir(toAccount), ".credentials.json")

	// Verify the new target exists.
	if _, err := os.Stat(newTarget); err != nil {
		return fmt.Errorf("account %q credentials not found at %s: %w", toAccount, newTarget, err)
	}

	// Remove old symlink and create new one.
	if err := os.Remove(credLink); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove old credential symlink: %w", err)
	}
	if err := os.Symlink(newTarget, credLink); err != nil {
		return fmt.Errorf("failed to create credential symlink: %w", err)
	}

	// Update quota state: mark new account's last_used.
	state.MarkLastUsed(toAccount)

	// Respawn the session with --continue.
	sessionName := config.SessionName(opts.World, agent.Name)
	if !mgr.Exists(sessionName) {
		return fmt.Errorf("session %q not found", sessionName)
	}

	workdir := config.WorktreePath(opts.World, agent.Name)
	settingsPath := config.SettingsPath(workdir)

	// Build the --continue command. Prime will be injected by the hook.
	cmd := config.BuildSessionCommandContinue(settingsPath,
		"Your credentials have been rotated due to rate limiting. Continue working on your current task.")

	env := map[string]string{
		"SOL_HOME":          config.Home(),
		"SOL_WORLD":         opts.World,
		"SOL_AGENT":         agent.Name,
		"CLAUDE_CONFIG_DIR": configDir,
	}

	if err := mgr.Cycle(sessionName, workdir, cmd, env, agent.Role, opts.World); err != nil {
		return fmt.Errorf("failed to respawn session %s: %w", sessionName, err)
	}

	if logger != nil {
		logger.Emit(events.EventQuotaRotate, "quota", agent.ID, "both",
			map[string]any{
				"agent":        agent.ID,
				"from_account": resolveCurrentAccount(opts.World, agent.Name, agent.Role),
				"to_account":   toAccount,
				"world":        opts.World,
			})
	}

	return nil
}

// pauseAgent stops an agent session cleanly because no accounts are available.
func pauseAgent(state *State, agent store.Agent, previousAccount string, opts RotateOpts, sphereStore *store.Store, mgr *session.Manager, logger *events.Logger) error {
	sessionName := config.SessionName(opts.World, agent.Name)

	// Stop the session cleanly.
	if mgr.Exists(sessionName) {
		if err := mgr.Stop(sessionName, false); err != nil {
			fmt.Fprintf(os.Stderr, "quota: failed to stop session %s: %v\n", sessionName, err)
		}
	}

	// Record the paused session in quota state.
	state.PausedSessions[agent.ID] = PausedSession{
		PausedAt:        time.Now().UTC(),
		PreviousAccount: previousAccount,
		WorkItem:        agent.TetherItem,
		World:           opts.World,
		AgentName:       agent.Name,
		Role:            agent.Role,
	}

	if logger != nil {
		logger.Emit(events.EventQuotaPause, "quota", agent.ID, "both",
			map[string]any{
				"agent":     agent.ID,
				"world":     opts.World,
				"work_item": agent.TetherItem,
				"reason":    "no available accounts for rotation",
			})
	}

	return nil
}

// restartPausedSessions checks for paused sessions in the given world and
// restarts them if available accounts exist. Called when limits expire or
// after rotation frees up accounts.
func restartPausedSessions(state *State, opts RotateOpts, sphereStore *store.Store, mgr *session.Manager, logger *events.Logger, result *RotateResult) error {
	available := state.AvailableAccountsLRU()
	if len(available) == 0 {
		return nil
	}

	availIdx := 0

	for agentID, paused := range state.PausedSessions {
		if paused.World != opts.World {
			continue
		}
		if availIdx >= len(available) {
			break
		}

		toAccount := available[availIdx]

		if !opts.DryRun {
			// Swap credentials and start session.
			worldDir := config.WorldDir(opts.World)
			configDir := config.ClaudeConfigDir(worldDir, paused.Role, paused.AgentName)
			credLink := filepath.Join(configDir, ".credentials.json")
			newTarget := filepath.Join(config.AccountDir(toAccount), ".credentials.json")

			if _, err := os.Stat(newTarget); err != nil {
				fmt.Fprintf(os.Stderr, "quota: account %q credentials not found, skipping restart of %s\n", toAccount, agentID)
				continue
			}

			if err := os.Remove(credLink); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "quota: failed to remove old symlink for %s: %v\n", agentID, err)
				continue
			}
			if err := os.Symlink(newTarget, credLink); err != nil {
				fmt.Fprintf(os.Stderr, "quota: failed to create symlink for %s: %v\n", agentID, err)
				continue
			}

			// Start session (not cycle — session was stopped).
			sessionName := config.SessionName(opts.World, paused.AgentName)
			workdir := config.WorktreePath(opts.World, paused.AgentName)
			settingsPath := config.SettingsPath(workdir)

			cmd := config.BuildSessionCommandContinue(settingsPath,
				"Your session was paused due to rate limiting. An account is now available. Continue working on your current task.")

			env := map[string]string{
				"SOL_HOME":          config.Home(),
				"SOL_WORLD":         opts.World,
				"SOL_AGENT":         paused.AgentName,
				"CLAUDE_CONFIG_DIR": configDir,
			}

			if err := mgr.Start(sessionName, workdir, cmd, env, paused.Role, opts.World); err != nil {
				fmt.Fprintf(os.Stderr, "quota: failed to restart session %s: %v\n", sessionName, err)
				continue
			}

			state.MarkLastUsed(toAccount)
			delete(state.PausedSessions, agentID)

			if logger != nil {
				logger.Emit(events.EventQuotaRotate, "quota", agentID, "both",
					map[string]any{
						"agent":      agentID,
						"to_account": toAccount,
						"world":      opts.World,
						"resumed":    true,
					})
			}
		}

		result.Actions = append(result.Actions, RotationAction{
			AgentID:     agentID,
			AgentName:   paused.AgentName,
			FromAccount: paused.PreviousAccount,
			ToAccount:   toAccount,
		})

		availIdx++
	}

	return nil
}
