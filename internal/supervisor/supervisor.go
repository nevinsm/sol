package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/events"
	"github.com/nevinsm/gt/internal/hook"
	"github.com/nevinsm/gt/internal/refinery"
	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/store"
)

// SessionManager abstracts tmux operations for testing.
type SessionManager interface {
	Exists(name string) bool
	Start(name, workdir, cmd string, env map[string]string, role, rig string) error
	Stop(name string, force bool) error
	List() ([]session.SessionInfo, error)
}

// TownStore abstracts town database operations for testing.
type TownStore interface {
	ListAgents(rig string, state string) ([]store.Agent, error)
	UpdateAgentState(id, state, hookItem string) error
}

// Config holds supervisor configuration.
type Config struct {
	HeartbeatInterval  time.Duration // default: 3 minutes
	MassDeathThreshold int           // default: 3 deaths in 30 seconds
	MassDeathWindow    time.Duration // default: 30 seconds
	DegradedCooldown   time.Duration // default: 5 minutes
}

// DefaultConfig returns the default supervisor configuration.
func DefaultConfig() Config {
	return Config{
		HeartbeatInterval:  3 * time.Minute,
		MassDeathThreshold: 3,
		MassDeathWindow:    30 * time.Second,
		DegradedCooldown:   5 * time.Minute,
	}
}

// Supervisor monitors agent sessions and restarts crashed ones.
// It is town-level: one supervisor watches all rigs.
type Supervisor struct {
	townStore TownStore
	sessions  SessionManager
	logger    *slog.Logger
	eventLog  *events.Logger // optional event feed logger
	cfg       Config

	mu            sync.Mutex
	degraded      bool
	degradedSince time.Time
	deathTimes    []time.Time    // timestamps of recent session deaths
	backoff       map[string]int // agent ID -> consecutive restart count
	lastStalled   map[string]time.Time // agent ID -> time when stalled (for backoff delay)
}

// New creates a new Supervisor.
// The eventLog parameter is optional — if nil, no events are emitted.
func New(cfg Config, townStore TownStore, mgr SessionManager, logger *slog.Logger, eventLog ...*events.Logger) *Supervisor {
	var el *events.Logger
	if len(eventLog) > 0 {
		el = eventLog[0]
	}
	return &Supervisor{
		townStore:   townStore,
		sessions:    mgr,
		logger:      logger,
		eventLog:    el,
		cfg:         cfg,
		backoff:     make(map[string]int),
		lastStalled: make(map[string]time.Time),
	}
}

// Run starts the supervisor heartbeat loop. Blocks until ctx is cancelled.
func (s *Supervisor) Run(ctx context.Context) error {
	if err := WritePID(); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer ClearPID()

	s.logger.Info("supervisor started", "pid", pidSelf(), "heartbeat_interval", s.cfg.HeartbeatInterval)

	// Run one immediate heartbeat.
	s.heartbeat()

	ticker := time.NewTicker(s.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.shutdown()
			return nil
		case <-ticker.C:
			s.heartbeat()
		}
	}
}

// IsDegraded returns true if the supervisor is in degraded mode.
func (s *Supervisor) IsDegraded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.degraded
}

// heartbeat runs one monitoring cycle.
func (s *Supervisor) heartbeat() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for degraded recovery before processing.
	s.checkDegradedRecovery()

	// List all working agents across all rigs.
	workingAgents, err := s.townStore.ListAgents("", "working")
	if err != nil {
		s.logger.Error("failed to list working agents", "error", err)
		return
	}

	// Build set of witnessed rigs (ADR-0006).
	witnessedRigs := s.getWitnessedRigs()

	deadCount := 0
	for _, agent := range workingAgents {
		sessName := dispatch.SessionName(agent.Rig, agent.Name)
		if !s.sessions.Exists(sessName) {
			deadCount++
			s.recordDeath()

			// Polecats in witnessed rigs are the witness's responsibility.
			if agent.Role == "polecat" && witnessedRigs[agent.Rig] {
				continue
			}

			if s.degraded {
				s.logger.Warn("session dead but degraded, setting stalled",
					"agent", agent.Name, "rig", agent.Rig)
				if err := s.townStore.UpdateAgentState(agent.ID, "stalled", agent.HookItem); err != nil {
					s.logger.Error("failed to set agent stalled", "agent", agent.Name, "error", err)
				}
				continue
			}

			s.respawn(agent)
		}
	}

	// Reset backoff for agents that went idle.
	s.resetBackoffForIdle()

	// Prune old death times.
	s.pruneDeathTimes()

	s.logger.Info("heartbeat", "working_agents", len(workingAgents), "dead_sessions", deadCount)
}

// respawnCommand returns the startup command for an agent based on its role.
func respawnCommand(agent store.Agent) string {
	switch agent.Role {
	case "witness":
		return fmt.Sprintf("gt witness run %s", agent.Rig)
	default:
		// Polecats and refinery run Claude sessions — the CLAUDE.md and hooks
		// installed in the worktree provide the execution context.
		return "claude --dangerously-skip-permissions"
	}
}

// worktreeForAgent returns the worktree path for an agent based on its role.
func worktreeForAgent(agent store.Agent) string {
	switch agent.Role {
	case "refinery":
		return refinery.RefineryWorktreePath(agent.Rig)
	case "witness":
		// Witness is a Go process, not a worktree-based agent.
		return config.Home()
	default:
		return dispatch.WorktreePath(agent.Rig, agent.Name)
	}
}

// respawn restarts a crashed agent session with backoff.
func (s *Supervisor) respawn(agent store.Agent) {
	agentID := agent.ID
	sessName := dispatch.SessionName(agent.Rig, agent.Name)
	worktreeDir := worktreeForAgent(agent)

	restartCount := s.backoff[agentID] + 1
	delay := backoffDuration(restartCount)

	// Check if enough time has passed since we stalled this agent.
	if delay > 0 {
		stalledAt, ok := s.lastStalled[agentID]
		if !ok {
			// First time seeing this agent needs a delayed restart — stall it.
			s.backoff[agentID] = restartCount
			s.lastStalled[agentID] = time.Now()
			s.logger.Info("session dead, deferring respawn",
				"agent", agent.Name, "rig", agent.Rig,
				"restart", restartCount, "delay", delay)
			if err := s.townStore.UpdateAgentState(agentID, "stalled", agent.HookItem); err != nil {
				s.logger.Error("failed to set agent stalled", "agent", agent.Name, "error", err)
			}
			return
		}
		if time.Since(stalledAt) < delay {
			// Not enough time has passed — keep waiting.
			return
		}
	}

	// Check worktree exists.
	if !dirExists(worktreeDir) {
		s.logger.Warn("worktree missing, setting agent idle",
			"agent", agent.Name, "rig", agent.Rig, "worktree", worktreeDir)
		if err := s.townStore.UpdateAgentState(agentID, "idle", ""); err != nil {
			s.logger.Error("failed to set agent idle", "agent", agent.Name, "error", err)
		}
		hook.Clear(agent.Rig, agent.Name)
		delete(s.backoff, agentID)
		delete(s.lastStalled, agentID)
		return
	}

	// Start the session.
	env := map[string]string{
		"GT_HOME":  config.Home(),
		"GT_RIG":   agent.Rig,
		"GT_AGENT": agent.Name,
	}
	if err := s.sessions.Start(sessName, worktreeDir,
		respawnCommand(agent), env, agent.Role, agent.Rig); err != nil {
		s.logger.Error("failed to respawn session",
			"agent", agent.Name, "rig", agent.Rig, "error", err)
		return
	}

	s.backoff[agentID] = restartCount
	delete(s.lastStalled, agentID)

	// Set agent back to working.
	if err := s.townStore.UpdateAgentState(agentID, "working", agent.HookItem); err != nil {
		s.logger.Error("failed to set agent working after respawn", "agent", agent.Name, "error", err)
	}

	s.logger.Info("respawned session",
		"agent", agent.Name, "rig", agent.Rig,
		"work_item", agent.HookItem, "restart", restartCount)

	if s.eventLog != nil {
		s.eventLog.Emit(events.EventRespawn, "supervisor", agent.Name, "both", map[string]any{
			"agent":     agent.Name,
			"rig":       agent.Rig,
			"work_item": agent.HookItem,
			"restart":   restartCount,
		})
	}
}

// recordDeath records a session death timestamp and checks for mass death.
func (s *Supervisor) recordDeath() {
	s.deathTimes = append(s.deathTimes, time.Now())
	s.checkMassDeath()
}

// checkMassDeath checks if enough deaths have occurred in the window to trigger degraded mode.
func (s *Supervisor) checkMassDeath() bool {
	if s.degraded {
		return true
	}

	now := time.Now()
	cutoff := now.Add(-s.cfg.MassDeathWindow)
	recentDeaths := 0
	for _, t := range s.deathTimes {
		if t.After(cutoff) {
			recentDeaths++
		}
	}

	if recentDeaths >= s.cfg.MassDeathThreshold {
		s.degraded = true
		s.degradedSince = now
		s.logger.Error("mass death detected",
			"deaths", recentDeaths, "window", s.cfg.MassDeathWindow)
		if s.eventLog != nil {
			s.eventLog.Emit(events.EventMassDeath, "supervisor", "supervisor", "both", map[string]any{
				"deaths": recentDeaths,
				"window": s.cfg.MassDeathWindow.String(),
			})
			s.eventLog.Emit(events.EventDegraded, "supervisor", "supervisor", "both", nil)
		}
		return true
	}
	return false
}

// checkDegradedRecovery checks if enough quiet time has passed to exit degraded mode.
func (s *Supervisor) checkDegradedRecovery() {
	if !s.degraded {
		return
	}

	now := time.Now()
	cutoff := now.Add(-s.cfg.DegradedCooldown)

	// Check if any deaths occurred in the cooldown period.
	for _, t := range s.deathTimes {
		if t.After(cutoff) {
			return // Still have recent deaths.
		}
	}

	s.degraded = false
	s.logger.Info("exited degraded mode", "duration", now.Sub(s.degradedSince))
	if s.eventLog != nil {
		s.eventLog.Emit(events.EventRecovered, "supervisor", "supervisor", "both", map[string]string{
			"duration": now.Sub(s.degradedSince).String(),
		})
	}
}

// resetBackoffForIdle resets backoff counters for agents that went idle.
func (s *Supervisor) resetBackoffForIdle() {
	if len(s.backoff) == 0 {
		return
	}

	idleAgents, err := s.townStore.ListAgents("", "idle")
	if err != nil {
		s.logger.Error("failed to list idle agents for backoff reset", "error", err)
		return
	}

	for _, agent := range idleAgents {
		if _, ok := s.backoff[agent.ID]; ok {
			delete(s.backoff, agent.ID)
			delete(s.lastStalled, agent.ID)
		}
	}
}

// pruneDeathTimes removes death timestamps older than the mass-death window.
func (s *Supervisor) pruneDeathTimes() {
	cutoff := time.Now().Add(-s.cfg.MassDeathWindow)
	n := 0
	for _, t := range s.deathTimes {
		if t.After(cutoff) {
			s.deathTimes[n] = t
			n++
		}
	}
	s.deathTimes = s.deathTimes[:n]
}

// getWitnessedRigs returns the set of rigs with an active witness.
// A rig is witnessed when its witness agent is working AND the
// witness tmux session is alive.
func (s *Supervisor) getWitnessedRigs() map[string]bool {
	witnesses, err := s.townStore.ListAgents("", "working")
	if err != nil {
		return nil
	}
	rigs := make(map[string]bool)
	for _, w := range witnesses {
		if w.Role != "witness" {
			continue
		}
		sessName := dispatch.SessionName(w.Rig, w.Name)
		if s.sessions.Exists(sessName) {
			rigs[w.Rig] = true
		}
	}
	return rigs
}

// shutdown gracefully stops all working agent sessions.
func (s *Supervisor) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()

	workingAgents, err := s.townStore.ListAgents("", "working")
	if err != nil {
		s.logger.Error("failed to list working agents during shutdown", "error", err)
		return
	}

	stalledAgents, err := s.townStore.ListAgents("", "stalled")
	if err != nil {
		s.logger.Error("failed to list stalled agents during shutdown", "error", err)
		stalledAgents = nil
	}

	allAgents := append(workingAgents, stalledAgents...)
	stopped := 0

	for _, agent := range allAgents {
		sessName := dispatch.SessionName(agent.Rig, agent.Name)
		if s.sessions.Exists(sessName) {
			if err := s.sessions.Stop(sessName, false); err != nil {
				s.logger.Error("failed to stop session during shutdown",
					"agent", agent.Name, "rig", agent.Rig, "error", err)
			} else {
				stopped++
			}
		}
		// Set agent to stalled (hooks persist for recovery).
		if err := s.townStore.UpdateAgentState(agent.ID, "stalled", agent.HookItem); err != nil {
			s.logger.Error("failed to set agent stalled during shutdown",
				"agent", agent.Name, "error", err)
		}
	}

	s.logger.Info("supervisor shutdown complete", "sessions_stopped", stopped, "agents_stalled", len(allAgents))
}

// backoffDuration returns the delay before respawning based on consecutive restart count.
func backoffDuration(consecutiveRestarts int) time.Duration {
	switch {
	case consecutiveRestarts <= 1:
		return 0
	case consecutiveRestarts == 2:
		return 30 * time.Second
	case consecutiveRestarts == 3:
		return 1 * time.Minute
	case consecutiveRestarts == 4:
		return 2 * time.Minute
	default:
		return 5 * time.Minute
	}
}

// dirExists checks if a directory exists.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
