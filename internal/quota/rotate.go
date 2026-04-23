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
	"github.com/nevinsm/sol/internal/startup"
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

// Rotate performs quota rotation: finds agents on limited accounts, writes
// new account credentials to their config dirs, and respawns their
// sessions with --continue for context preservation.
//
// The entire operation is protected by a flock on quota.json.
func Rotate(opts RotateOpts, sphereStore *store.SphereStore, mgr *session.Manager, logger *events.Logger) (*RotateResult, error) {
	lock, state, err := AcquireLock()
	if err != nil {
		return nil, fmt.Errorf("failed to acquire quota lock: %w", err)
	}
	defer lock.Release()

	result := &RotateResult{}

	// Expire any limits whose resets_at has passed.
	// ExpireLimits mutates state in-memory — only persist below if not dry-run.
	result.Expired = state.ExpireLimits()

	// Find limited accounts.
	limitedAccounts := state.LimitedAccounts()
	if len(limitedAccounts) == 0 {
		// No limited accounts — check if there are paused sessions to restart.
		if len(result.Expired) > 0 {
			if err := restartPausedSessions(state, opts, sphereStore, mgr, logger, result); err != nil {
				return result, err
			}
			if !opts.DryRun {
				if err := Save(state); err != nil {
					return result, fmt.Errorf("failed to save quota state: %w", err)
				}
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

	// Filter to outpost agents (not envoy, forge).
	var affectedAgents []store.Agent
	for _, a := range agents {
		if a.Role != "outpost" {
			continue
		}
		// Resolve which account this agent is currently using.
		acctHandle := ResolveCurrentAccount(opts.World, a.Name, a.Role)
		if limitedSet[acctHandle] {
			affectedAgents = append(affectedAgents, a)
		}
	}

	if len(affectedAgents) == 0 {
		if !opts.DryRun {
			if err := Save(state); err != nil {
				return result, fmt.Errorf("failed to save quota state: %w", err)
			}
		}
		return result, nil
	}

	// Get available accounts sorted by LRU.
	availableAccounts := state.AvailableAccountsLRU()
	availIdx := 0

	for _, agent := range affectedAgents {
		fromAccount := ResolveCurrentAccount(opts.World, agent.Name, agent.Role)

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

// ResolveCurrentAccount determines which account an agent is currently using.
// First checks the .account metadata file (broker-managed), then falls back
// to reading the .credentials.json symlink (legacy).
// Returns the account handle, or "" if it can't be resolved.
func ResolveCurrentAccount(world, agentName, role string) string {
	worldDir := config.WorldDir(world)
	configDir := config.ClaudeConfigDir(worldDir, role, agentName)

	// Prefer .account file (broker-managed).
	if handle := readAccountFile(configDir); handle != "" {
		return handle
	}

	// Fallback: read symlink target (legacy).
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

	// rel should be "{handle}/.credentials.json" — verify the trailing
	// component matches so we don't accept arbitrary symlink targets that
	// happen to live anywhere under accountsDir.
	parts := strings.SplitN(rel, string(filepath.Separator), 2)
	if len(parts) != 2 || parts[1] != ".credentials.json" {
		return ""
	}

	// Validate the handle doesn't look like a path traversal.
	handle := parts[0]
	if handle == "" || handle == "." || handle == ".." || strings.Contains(handle, "/") {
		return ""
	}
	return handle
}

// readAccountFile reads the .account metadata file from an agent config dir.
func readAccountFile(configDir string) string {
	data, err := os.ReadFile(filepath.Join(configDir, ".account"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// swapAndRespawn writes new account credentials and respawns the session.
func swapAndRespawn(state *State, agent store.Agent, toAccount string, opts RotateOpts, mgr *session.Manager, logger *events.Logger) error {
	// Mark the new account as assigned to this agent, preventing it from
	// appearing in AvailableAccountsLRU for subsequent rotation calls.
	agentKey := opts.World + "/" + agent.Name
	state.MarkAssigned(toAccount, agentKey)

	// Roll back the assignment if any subsequent step fails, so the account
	// is not permanently stuck in Assigned state.
	var succeeded bool
	defer func() {
		if !succeeded {
			state.ReleaseAccount(toAccount)
		}
	}()

	// Respawn the session with --continue.
	sessionName := config.SessionName(opts.World, agent.Name)
	if !mgr.Exists(sessionName) {
		return fmt.Errorf("session %q not found", sessionName)
	}

	fromAccount := ResolveCurrentAccount(opts.World, agent.Name, agent.Role)

	// Use startup.Resume for registered roles to get system prompt flags,
	// persona, hooks, and workflow re-instantiation.
	cfg := startup.ConfigFor(agent.Role)
	if cfg == nil {
		return fmt.Errorf("quota rotate: no startup config registered for role %q", agent.Role)
	}

	cycleOp := func(name, workdir, cmd string, env map[string]string, role, world string) error {
		if err := mgr.Cycle(name, workdir, cmd, env, role, world); err != nil {
			fmt.Fprintf(os.Stderr, "quota: cycle failed, falling back to stop+start: %v\n", err)
			if stopErr := mgr.Stop(name, true); stopErr != nil {
				fmt.Fprintf(os.Stderr, "quota: stop also failed: %v\n", stopErr)
			}
			return mgr.Start(name, workdir, cmd, env, role, world)
		}
		return nil
	}

	resumeState := startup.ResumeState{
		Reason:          "quota_rotate",
		ClaimedResource: agent.ActiveWrit,
	}

	launchOpts := startup.LaunchOpts{
		Account:   toAccount,
		SessionOp: cycleOp,
	}

	if _, err := startup.Resume(*cfg, opts.World, agent.Name, resumeState, launchOpts); err != nil {
		return fmt.Errorf("failed to respawn session %s: %w", sessionName, err)
	}

	if logger != nil {
		logger.Emit(events.EventQuotaRotate, "quota", agent.ID, "both",
			map[string]any{
				"agent":        agent.ID,
				"from_account": fromAccount,
				"to_account":   toAccount,
				"world":        opts.World,
			})
	}

	succeeded = true
	return nil
}

// pauseAgent stops an agent session cleanly because no accounts are available.
func pauseAgent(state *State, agent store.Agent, previousAccount string, opts RotateOpts, sphereStore *store.SphereStore, mgr *session.Manager, logger *events.Logger) error {
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
		Writ:        agent.ActiveWrit,
		World:           opts.World,
		AgentName:       agent.Name,
		Role:            agent.Role,
	}

	if logger != nil {
		logger.Emit(events.EventQuotaPause, "quota", agent.ID, "both",
			map[string]any{
				"agent":     agent.ID,
				"world":     opts.World,
				"writ": agent.ActiveWrit,
				"reason":    "no available accounts for rotation",
			})
	}

	return nil
}

// restartPausedSessions checks for paused sessions in the given world and
// restarts them if available accounts exist. Called when limits expire or
// after rotation frees up accounts.
func restartPausedSessions(state *State, opts RotateOpts, sphereStore *store.SphereStore, mgr *session.Manager, logger *events.Logger, result *RotateResult) error {
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
			// Use startup.Resume for registered roles to get system prompt
			// flags, persona, hooks, and workflow re-instantiation.
			cfg := startup.ConfigFor(paused.Role)
			if cfg == nil {
				fmt.Fprintf(os.Stderr, "quota: no startup config registered for role %q (agent %s), skipping\n", paused.Role, agentID)
				continue
			}

			resumeState := startup.ResumeState{
				Reason:          "quota_rotate",
				ClaimedResource: paused.Writ,
			}

			launchOpts := startup.LaunchOpts{
				Account:  toAccount,
				Sessions: mgr,
			}

			if _, err := startup.Resume(*cfg, opts.World, paused.AgentName, resumeState, launchOpts); err != nil {
				fmt.Fprintf(os.Stderr, "quota: failed to restart session %s via startup: %v\n", agentID, err)
				continue
			}

			agentKey := paused.World + "/" + paused.AgentName
			state.MarkAssigned(toAccount, agentKey)
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
