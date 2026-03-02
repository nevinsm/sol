package prefect

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/consul"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/envoy"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/governor"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// SessionManager abstracts tmux operations for testing.
type SessionManager interface {
	Exists(name string) bool
	Start(name, workdir, cmd string, env map[string]string, role, world string) error
	Stop(name string, force bool) error
	List() ([]session.SessionInfo, error)
}

// SphereStore abstracts sphere database operations for testing.
type SphereStore interface {
	ListAgents(world string, state string) ([]store.Agent, error)
	UpdateAgentState(id, state, tetherItem string) error
}

// Config holds prefect configuration.
type Config struct {
	HeartbeatInterval  time.Duration // default: 3 minutes
	MassDeathThreshold int           // default: 3 deaths in 30 seconds
	MassDeathWindow    time.Duration // default: 30 seconds
	DegradedCooldown   time.Duration // default: 5 minutes

	ConsulEnabled      bool          // whether to monitor the consul (default: false)
	ConsulHeartbeatMax time.Duration // max heartbeat age before restart (default: 15 minutes)
	ConsulCommand      string        // command to start consul (default: "sol consul run")
	ConsulSourceRepo   string        // source repo path for consul config
}

// DefaultConfig returns the default prefect configuration.
func DefaultConfig() Config {
	return Config{
		HeartbeatInterval:  3 * time.Minute,
		MassDeathThreshold: 3,
		MassDeathWindow:    30 * time.Second,
		DegradedCooldown:   5 * time.Minute,

		ConsulHeartbeatMax: 15 * time.Minute,
		ConsulCommand:      "sol consul run",
	}
}

// Prefect monitors agent sessions and restarts crashed ones.
// It is sphere-level: one prefect watches all worlds.
type Prefect struct {
	sphereStore SphereStore
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

	heartbeatCount int // total heartbeat cycles, used for consul check frequency
}

// New creates a new Prefect.
// The eventLog parameter is optional — if nil, no events are emitted.
func New(cfg Config, sphereStore SphereStore, mgr SessionManager, logger *slog.Logger, eventLog ...*events.Logger) *Prefect {
	var el *events.Logger
	if len(eventLog) > 0 {
		el = eventLog[0]
	}
	return &Prefect{
		sphereStore: sphereStore,
		sessions:    mgr,
		logger:      logger,
		eventLog:    el,
		cfg:         cfg,
		backoff:     make(map[string]int),
		lastStalled: make(map[string]time.Time),
	}
}

// Run starts the prefect heartbeat loop. Blocks until ctx is cancelled.
func (s *Prefect) Run(ctx context.Context) error {
	if s.cfg.HeartbeatInterval <= 0 {
		return fmt.Errorf("invalid heartbeat interval: %v", s.cfg.HeartbeatInterval)
	}

	if err := WritePID(); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer ClearPID()

	s.logger.Info("prefect started", "pid", pidSelf(), "heartbeat_interval", s.cfg.HeartbeatInterval)

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

// IsDegraded returns true if the prefect is in degraded mode.
func (s *Prefect) IsDegraded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.degraded
}

// Heartbeat runs one monitoring cycle. Exported for integration tests.
func (s *Prefect) Heartbeat() {
	s.heartbeat()
}

// heartbeat runs one monitoring cycle.
func (s *Prefect) heartbeat() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.heartbeatCount++

	// Check for degraded recovery before processing.
	s.checkDegradedRecovery()

	// List all working agents across all worlds.
	workingAgents, err := s.sphereStore.ListAgents("", "working")
	if err != nil {
		s.logger.Error("failed to list working agents", "error", err)
		return
	}

	// Build set of sentineled worlds (ADR-0006).
	sentineledWorlds := s.getSentineledWorlds()

	deadCount := 0
	for _, agent := range workingAgents {
		// Skip human-supervised roles — envoys and governors are not auto-respawned.
		if agent.Role == "envoy" || agent.Role == "governor" {
			continue
		}

		sessName := dispatch.SessionName(agent.World, agent.Name)
		if !s.sessions.Exists(sessName) {
			deadCount++
			s.recordDeath()

			// Agents in sentineled worlds are the sentinel's responsibility.
			if agent.Role == "agent" && sentineledWorlds[agent.World] {
				continue
			}

			if s.degraded {
				s.logger.Warn("session dead but degraded, setting stalled",
					"agent", agent.Name, "world", agent.World)
				if err := s.sphereStore.UpdateAgentState(agent.ID, "stalled", agent.TetherItem); err != nil {
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

	// Check consul health (only if enabled).
	// Check on first heartbeat (startup) and every other patrol thereafter.
	if s.cfg.ConsulEnabled && (s.heartbeatCount == 1 || s.heartbeatCount%2 == 0) {
		if err := s.checkConsul(); err != nil {
			s.logger.Error("consul health check failed", "error", err)
		}
	}

	s.logger.Info("heartbeat", "working_agents", len(workingAgents), "dead_sessions", deadCount)
}

// respawnCommand returns the startup command for an agent based on its role.
func respawnCommand(agent store.Agent) string {
	switch agent.Role {
	case "sentinel":
		return fmt.Sprintf("sol sentinel run %s", agent.World)
	case "envoy", "governor":
		// Should never reach here — skipped in heartbeat.
		// But if it does, start a Claude session.
		return "claude --dangerously-skip-permissions"
	default:
		// Agents and forge run Claude sessions — the CLAUDE.md and tethers
		// installed in the worktree provide the execution context.
		return "claude --dangerously-skip-permissions"
	}
}

// worktreeForAgent returns the worktree path for an agent based on its role.
func worktreeForAgent(agent store.Agent) string {
	switch agent.Role {
	case "forge":
		return forge.WorktreePath(agent.World)
	case "sentinel":
		// Sentinel is a Go process, not a worktree-based agent.
		return config.Home()
	case "envoy":
		return envoy.WorktreePath(agent.World, agent.Name)
	case "governor":
		return governor.GovernorDir(agent.World)
	default:
		return dispatch.WorktreePath(agent.World, agent.Name)
	}
}

// respawn restarts a crashed agent session with backoff.
func (s *Prefect) respawn(agent store.Agent) {
	agentID := agent.ID
	sessName := dispatch.SessionName(agent.World, agent.Name)
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
				"agent", agent.Name, "world", agent.World,
				"restart", restartCount, "delay", delay)
			if err := s.sphereStore.UpdateAgentState(agentID, "stalled", agent.TetherItem); err != nil {
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
			"agent", agent.Name, "world", agent.World, "worktree", worktreeDir)
		if err := s.sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
			s.logger.Error("failed to set agent idle", "agent", agent.Name, "error", err)
		}
		tether.Clear(agent.World, agent.Name)
		delete(s.backoff, agentID)
		delete(s.lastStalled, agentID)
		return
	}

	// Start the session.
	env := map[string]string{
		"SOL_HOME":  config.Home(),
		"SOL_WORLD":   agent.World,
		"SOL_AGENT": agent.Name,
	}
	if err := s.sessions.Start(sessName, worktreeDir,
		respawnCommand(agent), env, agent.Role, agent.World); err != nil {
		s.logger.Error("failed to respawn session",
			"agent", agent.Name, "world", agent.World, "error", err)
		return
	}

	s.backoff[agentID] = restartCount
	delete(s.lastStalled, agentID)

	// Set agent back to working.
	if err := s.sphereStore.UpdateAgentState(agentID, "working", agent.TetherItem); err != nil {
		s.logger.Error("failed to set agent working after respawn", "agent", agent.Name, "error", err)
	}

	s.logger.Info("respawned session",
		"agent", agent.Name, "world", agent.World,
		"work_item", agent.TetherItem, "restart", restartCount)

	if s.eventLog != nil {
		s.eventLog.Emit(events.EventRespawn, "prefect", agent.Name, "both", map[string]any{
			"agent":     agent.Name,
			"world":     agent.World,
			"work_item": agent.TetherItem,
			"restart":   restartCount,
		})
	}
}

// consulSessionName is the tmux session name for the consul.
const consulSessionName = "sol-sphere-consul"

// checkConsul reads the consul heartbeat and restarts if stale.
// The consul is exempt from degraded mode — it is infrastructure, not a worker.
func (s *Prefect) checkConsul() error {
	hb, err := consul.ReadHeartbeat(config.Home())
	if err != nil {
		return fmt.Errorf("failed to read consul heartbeat: %w", err)
	}

	if hb == nil {
		// No heartbeat exists — start the consul.
		s.logger.Info("no consul heartbeat found, starting consul")
		return s.startConsul()
	}

	if !hb.IsStale(s.cfg.ConsulHeartbeatMax) {
		// Heartbeat is fresh — no action needed.
		return nil
	}

	// Heartbeat is stale — restart the consul.
	s.logger.Warn("consul heartbeat is stale, restarting",
		"last_heartbeat", hb.Timestamp,
		"max_age", s.cfg.ConsulHeartbeatMax)

	// Stop existing session if present (might be hung).
	if s.sessions.Exists(consulSessionName) {
		if err := s.sessions.Stop(consulSessionName, true); err != nil {
			s.logger.Error("failed to stop stale consul session", "error", err)
		}
	}

	return s.startConsul()
}

// startConsul starts the consul in a tmux session.
func (s *Prefect) startConsul() error {
	env := map[string]string{
		"SOL_HOME": config.Home(),
	}
	cmd := s.cfg.ConsulCommand
	if s.cfg.ConsulSourceRepo != "" {
		cmd += " --source-repo=" + s.cfg.ConsulSourceRepo
	}
	if err := s.sessions.Start(consulSessionName, config.Home(), cmd, env, "consul", "sphere"); err != nil {
		return fmt.Errorf("failed to start consul session: %w", err)
	}

	s.logger.Info("consul session started", "session", consulSessionName)
	return nil
}

// recordDeath records a session death timestamp and checks for mass death.
func (s *Prefect) recordDeath() {
	s.deathTimes = append(s.deathTimes, time.Now())
	s.checkMassDeath()
}

// checkMassDeath checks if enough deaths have occurred in the window to trigger degraded mode.
func (s *Prefect) checkMassDeath() bool {
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
			s.eventLog.Emit(events.EventMassDeath, "prefect", "prefect", "both", map[string]any{
				"deaths": recentDeaths,
				"window": s.cfg.MassDeathWindow.String(),
			})
			s.eventLog.Emit(events.EventDegraded, "prefect", "prefect", "both", nil)
		}
		return true
	}
	return false
}

// checkDegradedRecovery checks if enough quiet time has passed to exit degraded mode.
func (s *Prefect) checkDegradedRecovery() {
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
		s.eventLog.Emit(events.EventRecovered, "prefect", "prefect", "both", map[string]string{
			"duration": now.Sub(s.degradedSince).String(),
		})
	}
}

// resetBackoffForIdle resets backoff counters for agents that went idle.
func (s *Prefect) resetBackoffForIdle() {
	if len(s.backoff) == 0 {
		return
	}

	idleAgents, err := s.sphereStore.ListAgents("", "idle")
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
func (s *Prefect) pruneDeathTimes() {
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

// getSentineledWorlds returns the set of worlds with an active sentinel.
// A world is sentineled when its sentinel agent is working AND the
// sentinel tmux session is alive.
func (s *Prefect) getSentineledWorlds() map[string]bool {
	sentinels, err := s.sphereStore.ListAgents("", "working")
	if err != nil {
		return nil
	}
	worlds := make(map[string]bool)
	for _, w := range sentinels {
		if w.Role != "sentinel" {
			continue
		}
		sessName := dispatch.SessionName(w.World, w.Name)
		if s.sessions.Exists(sessName) {
			worlds[w.World] = true
		}
	}
	return worlds
}

// shutdown gracefully stops all working agent sessions.
func (s *Prefect) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()

	workingAgents, err := s.sphereStore.ListAgents("", "working")
	if err != nil {
		s.logger.Error("failed to list working agents during shutdown", "error", err)
		return
	}

	stalledAgents, err := s.sphereStore.ListAgents("", "stalled")
	if err != nil {
		s.logger.Error("failed to list stalled agents during shutdown", "error", err)
		stalledAgents = nil
	}

	allAgents := append(workingAgents, stalledAgents...)
	stopped := 0

	for _, agent := range allAgents {
		// Skip human-supervised roles — envoys and governors are persistent.
		if agent.Role == "envoy" || agent.Role == "governor" {
			continue
		}
		sessName := dispatch.SessionName(agent.World, agent.Name)
		if s.sessions.Exists(sessName) {
			if err := s.sessions.Stop(sessName, false); err != nil {
				s.logger.Error("failed to stop session during shutdown",
					"agent", agent.Name, "world", agent.World, "error", err)
			} else {
				stopped++
			}
		}
		// Set agent to stalled (hooks persist for recovery).
		if err := s.sphereStore.UpdateAgentState(agent.ID, "stalled", agent.TetherItem); err != nil {
			s.logger.Error("failed to set agent stalled during shutdown",
				"agent", agent.Name, "error", err)
		}
	}

	// Stop consul session if enabled.
	if s.cfg.ConsulEnabled && s.sessions.Exists(consulSessionName) {
		if err := s.sessions.Stop(consulSessionName, false); err != nil {
			s.logger.Error("failed to stop consul session during shutdown", "error", err)
		} else {
			stopped++
			s.logger.Info("consul session stopped during shutdown")
		}
	}

	s.logger.Info("prefect shutdown complete", "sessions_stopped", stopped, "agents_stalled", len(allAgents))
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
