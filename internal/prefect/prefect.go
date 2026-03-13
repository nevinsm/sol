package prefect

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/chronicle"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/consul"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/ledger"
	"github.com/nevinsm/sol/internal/logutil"
	"github.com/nevinsm/sol/internal/processutil"
	"github.com/nevinsm/sol/internal/sentinel"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/startup"
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
	store.AgentReader
	store.AgentWriter
	store.WorldRegistry
}

// Config holds prefect configuration.
type Config struct {
	HeartbeatInterval  time.Duration // default: 3 minutes
	MassDeathThreshold int           // default: 3 deaths in 30 seconds
	MassDeathWindow    time.Duration // default: 30 seconds
	DegradedCooldown   time.Duration // default: 5 minutes

	Worlds []string // if non-empty, only supervise these worlds (sleeping ones still skipped)

	ConsulEnabled      bool          // whether to monitor the consul (default: false)
	ConsulHeartbeatMax time.Duration // max heartbeat age before restart (default: 15 minutes)
	ConsulCommand      string        // command to start consul (default: "sol consul run")
	ConsulSourceRepo   string        // source repo path for consul config

	ForgeHeartbeatMax  time.Duration // max forge heartbeat age before restart (default: 5 minutes)
	LedgerHeartbeatMax time.Duration // max ledger heartbeat age before restart (default: 5 minutes)

	ChronicleHeartbeatMax time.Duration // max chronicle heartbeat age before restart (default: 5 minutes)
	SentinelHeartbeatMax  time.Duration // max sentinel heartbeat age before restart (default: 15 minutes)

	SolBinary string // path to sol binary for starting world services. If empty, infrastructure check is skipped.
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

		ForgeHeartbeatMax:     5 * time.Minute,
		LedgerHeartbeatMax:    5 * time.Minute,
		ChronicleHeartbeatMax: 5 * time.Minute,
		SentinelHeartbeatMax:  15 * time.Minute,
	}
}

// worldServices are the per-world infrastructure services the prefect manages
// via tmux session existence check. Forge and sentinel are monitored separately
// via heartbeat.
var worldServices []string

// sphereDaemonSpec describes a sphere-level daemon supervised via PID check.
type sphereDaemonSpec struct {
	Name     string   // daemon name (matches PID file: {name}.pid)
	Session  string   // tmux session name to check (empty if not tmux-managed)
	Args     []string // args for sol binary restart command
	Detached bool     // true = start as detached process, false = simple runCommand
}

// supervisedSphereDaemons are sphere-level daemons the prefect monitors via PID/session check.
// Consul and chronicle are supervised separately via heartbeat file staleness.
var supervisedSphereDaemons = []sphereDaemonSpec{
	{Name: "ledger", Args: []string{"ledger", "run"}, Detached: true},
	{Name: "broker", Args: []string{"broker", "run"}, Detached: true},
}

// Prefect monitors agent sessions and restarts crashed ones.
// It is sphere-level: one prefect watches all worlds.
type Prefect struct {
	sphereStore SphereStore
	sessions  SessionManager
	logger    *slog.Logger
	eventLog  *events.Logger // optional event feed logger
	cfg       Config

	// runCommand executes an external command. Defaults to exec.Command(...).Run().
	// Override in tests to avoid real process execution.
	runCommand func(name string, args ...string) error

	// startDaemonProcess starts a daemon as a detached background process.
	// Takes the daemon name (for PID/log file paths) and the binary path + args.
	// Override in tests to avoid real process execution.
	startDaemonProcess func(daemon string, binPath string, args ...string) error

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
		runCommand: func(name string, args ...string) error {
			return exec.Command(name, args...).Run()
		},
		startDaemonProcess: defaultStartDaemonProcess,
		backoff:            make(map[string]int),
		lastStalled:        make(map[string]time.Time),
	}
}

// SetStartDaemonProcess overrides the daemon process starter for testing.
// The function is called with (daemon, binPath, args...).
func (s *Prefect) SetStartDaemonProcess(fn func(daemon string, binPath string, args ...string) error) {
	s.startDaemonProcess = fn
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

	if len(s.cfg.Worlds) > 0 {
		s.logger.Info("prefect started", "pid", pidSelf(), "heartbeat_interval", s.cfg.HeartbeatInterval, "worlds", s.cfg.Worlds)
	} else {
		s.logger.Info("prefect started", "pid", pidSelf(), "heartbeat_interval", s.cfg.HeartbeatInterval)
	}

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

	// Build set of sleeping worlds.
	sleepingWorlds := s.getSleepingWorlds(workingAgents)

	deadCount := 0
	for _, agent := range workingAgents {
		// Skip human-supervised roles — envoys and governors are not auto-respawned.
		if agent.Role == "envoy" || agent.Role == "governor" {
			continue
		}

		// Skip sentinel — it's managed as a direct process via heartbeat, not tmux session.
		if agent.Role == "sentinel" {
			continue
		}

		// Skip consul — managed as a direct process via heartbeat, supervised by checkConsul().
		if agent.Role == "consul" {
			continue
		}

		// Skip sleeping worlds — their services should not be respawned.
		if sleepingWorlds[agent.World] {
			continue
		}

		// Skip worlds outside the configured scope.
		if !s.worldAllowed(agent.World) {
			continue
		}

		sessName := config.SessionName(agent.World, agent.Name)
		if !s.sessions.Exists(sessName) {
			deadCount++
			s.recordDeath()

			// Agents in sentineled worlds are the sentinel's responsibility.
			if agent.Role == "outpost" && sentineledWorlds[agent.World] {
				continue
			}

			if s.degraded {
				s.logger.Warn("session dead but degraded, setting stalled",
					"agent", agent.Name, "world", agent.World)
				if err := s.sphereStore.UpdateAgentState(agent.ID, store.AgentStalled, agent.ActiveWrit); err != nil {
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

	// Check world infrastructure (sentinel/forge) on first heartbeat and every 3rd cycle.
	if s.heartbeatCount == 1 || s.heartbeatCount%3 == 0 {
		s.checkWorldInfrastructure()
	}

	// Check sphere daemons (ledger/broker) on first heartbeat and every 3rd cycle.
	if s.heartbeatCount == 1 || s.heartbeatCount%3 == 0 {
		s.checkSphereDaemons()
	}

	// Check chronicle health via heartbeat (on first heartbeat and every 3rd cycle).
	if s.heartbeatCount == 1 || s.heartbeatCount%3 == 0 {
		s.checkChronicleHealth()
	}

	s.logger.Info("heartbeat", "working_agents", len(workingAgents), "dead_sessions", deadCount)

	// Best-effort log rotation — don't let rotation failure affect supervision.
	logutil.TruncateIfNeeded(filepath.Join(config.RuntimeDir(), "prefect.log"), logutil.DefaultMaxLogSize)
}

// worktreeForAgent returns the worktree path for an agent based on its role.
// Roles with registered startup configs are resolved via their WorktreeDir function.
func worktreeForAgent(agent store.Agent) string {
	if cfg := startup.ConfigFor(agent.Role); cfg != nil && cfg.WorktreeDir != nil {
		return cfg.WorktreeDir(agent.World, agent.Name)
	}
	switch agent.Role {
	case "forge":
		// Fallback for tests where forge config is not registered.
		return forge.WorktreePath(agent.World)
	case "sentinel":
		// Sentinel is a Go process, not a worktree-based agent.
		return config.Home()
	default:
		return dispatch.WorktreePath(agent.World, agent.Name)
	}
}

// respawn restarts a crashed agent session with backoff.
func (s *Prefect) respawn(agent store.Agent) {
	agentID := agent.ID
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
			if err := s.sphereStore.UpdateAgentState(agentID, store.AgentStalled, agent.ActiveWrit); err != nil {
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
		if err := s.sphereStore.UpdateAgentState(agentID, store.AgentIdle, ""); err != nil {
			s.logger.Error("failed to set agent idle", "agent", agent.Name, "error", err)
		}
		tether.Clear(agent.World, agent.Name, agent.Role)
		delete(s.backoff, agentID)
		delete(s.lastStalled, agentID)
		return
	}

	// Use startup.Respawn for roles with registered configs.
	if cfg := startup.ConfigFor(agent.Role); cfg == nil {
		s.logger.Error("no startup config registered for role, cannot respawn",
			"agent", agent.Name, "world", agent.World, "role", agent.Role)
		return
	}
	_, err := startup.Respawn(agent.Role, agent.World, agent.Name, startup.LaunchOpts{
		Sessions: s.sessions,
	})
	if err != nil {
		s.logger.Error("failed to respawn session via startup",
			"agent", agent.Name, "world", agent.World, "error", err)
		return
	}

	s.backoff[agentID] = restartCount
	delete(s.lastStalled, agentID)

	s.logger.Info("respawned session",
		"agent", agent.Name, "world", agent.World,
		"writ", agent.ActiveWrit, "restart", restartCount)

	if s.eventLog != nil {
		s.eventLog.Emit(events.EventRespawn, "prefect", agent.Name, "both", map[string]any{
			"agent":     agent.Name,
			"world":     agent.World,
			"writ": agent.ActiveWrit,
			"restart":   restartCount,
		})
	}
}

// checkConsul reads the consul heartbeat and restarts if stale.
// The consul is exempt from degraded mode — it is infrastructure, not a worker.
func (s *Prefect) checkConsul() error {
	hb, err := consul.ReadHeartbeat(config.Home())
	if err != nil {
		return fmt.Errorf("failed to read consul heartbeat: %w", err)
	}

	if hb == nil {
		// No heartbeat exists — check if process is alive via PID.
		pid := ReadDaemonPID("consul")
		if pid > 0 && IsRunning(pid) {
			// Process alive but no heartbeat yet — give it time.
			return nil
		}
		// No heartbeat and no process — start the consul.
		s.logger.Info("no consul heartbeat found, starting consul")
		return s.startConsul()
	}

	if hb.Status == "stopping" {
		// Consul shut down gracefully but the heartbeat is still fresh.
		// Treat as dead — check PID and restart if needed.
		pid := ReadDaemonPID("consul")
		if pid > 0 && IsRunning(pid) {
			return nil // process somehow still alive, give it time
		}
		s.logger.Info("consul heartbeat shows stopping, restarting")
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

	// Stop existing process if running (might be hung).
	pid := ReadDaemonPID("consul")
	if pid > 0 && IsRunning(pid) {
		if err := processutil.GracefulKill(pid, 5*time.Second); err != nil {
			s.logger.Error("failed to stop stale consul process", "pid", pid, "error", err)
		}
	}

	return s.startConsul()
}

// startConsul starts the consul as a detached background process.
func (s *Prefect) startConsul() error {
	if s.cfg.SolBinary == "" {
		return fmt.Errorf("sol binary path not configured")
	}

	return s.startDaemonProcess("consul", s.cfg.SolBinary, "consul", "run")
}

// checkWorldInfrastructure ensures sentinel and forge sessions exist for
// each non-sleeping, allowed world. Missing services are started by shelling
// out to "sol sentinel start" / "sol forge start", which are idempotent.
// Requires cfg.SolBinary to be set — skips silently if empty.
// Must be called with s.mu held.
func (s *Prefect) checkWorldInfrastructure() {
	if s.cfg.SolBinary == "" {
		return
	}

	worlds, err := s.sphereStore.ListWorlds()
	if err != nil {
		s.logger.Error("failed to list worlds for infrastructure check", "error", err)
		return
	}

	for _, world := range worlds {
		if config.IsSleeping(world.Name) {
			continue
		}
		if !s.worldAllowed(world.Name) {
			continue
		}

		for _, svc := range worldServices {
			sessName := config.SessionName(world.Name, svc)
			if s.sessions.Exists(sessName) {
				continue
			}

			s.logger.Info("starting world service", "service", svc, "world", world.Name)

			if err := s.runCommand(s.cfg.SolBinary, svc, "start", "--world="+world.Name); err != nil {
				s.logger.Error("failed to start world service",
					"service", svc, "world", world.Name, "error", err)
				continue
			}

			if s.eventLog != nil {
				s.eventLog.Emit(events.EventRespawn, "prefect", svc, "both", map[string]any{
					"service": svc,
					"world":   world.Name,
				})
			}
		}

		// Check forge via heartbeat staleness (forge is a Go process, not a session-based service).
		s.checkForgeHealth(world.Name)

		// Check sentinel via heartbeat staleness (sentinel is a direct Go process).
		s.checkSentinelHealth(world.Name)
	}
}

// serviceHeartbeat carries the minimal heartbeat data needed for health checking.
type serviceHeartbeat struct {
	Timestamp time.Time
	isStale   func(maxAge time.Duration) bool
}

// IsStale delegates to the underlying heartbeat implementation.
func (h *serviceHeartbeat) IsStale(maxAge time.Duration) bool {
	return h.isStale(maxAge)
}

// serviceSpec describes a supervised service for checkServiceHealth.
type serviceSpec struct {
	// Name is used in log messages and event payloads.
	Name string

	// ReadPID returns the current PID (0 if not set).
	ReadPID func() int

	// ReadHeartbeat returns the most recent heartbeat, or (nil, nil) if none yet.
	ReadHeartbeat func() (*serviceHeartbeat, error)

	// MaxStaleAge is the heartbeat age threshold that triggers a restart.
	MaxStaleAge time.Duration

	// StopStale terminates the running process when the heartbeat is stale.
	// Receives the current PID.
	StopStale func(pid int) error

	// StartDead starts the service when no process is running.
	// If nil, dead-process restart is skipped (handled externally).
	StartDead func() error

	// StartStale starts the service after stopping a stale one.
	StartStale func() error

	// DeadEventData, if non-nil, is emitted as EventRespawn on dead-process restart.
	DeadEventData map[string]any

	// StaleEventData is emitted as EventRespawn on stale-process restart.
	StaleEventData map[string]any

	// SkipDeadRestart, if non-nil, is called when the process is dead.
	// If it returns true, the restart is skipped for this cycle.
	SkipDeadRestart func() bool
}

// checkServiceHealth implements the shared health check pattern used by forge,
// sentinel, ledger, and chronicle:
//  1. Read PID → check if alive
//  2. If not alive → restart (unless StartDead is nil or SkipDeadRestart returns true)
//  3. If alive → read heartbeat
//  4. If no heartbeat → return (give it time)
//  5. If heartbeat stale → stop + restart + emit EventRespawn
func (s *Prefect) checkServiceHealth(spec serviceSpec) {
	if s.cfg.SolBinary == "" {
		return
	}

	pid := spec.ReadPID()
	processAlive := pid > 0 && IsRunning(pid)

	if !processAlive {
		// Optional: skip restart for this cycle (e.g., sentinel recovery window).
		if spec.SkipDeadRestart != nil && spec.SkipDeadRestart() {
			return
		}
		// If no dead-restart handler, skip (handled externally).
		if spec.StartDead == nil {
			return
		}

		s.logger.Info(spec.Name+" process not running, starting")
		if err := spec.StartDead(); err != nil {
			s.logger.Error("failed to start "+spec.Name, "error", err)
		} else {
			s.logger.Info(spec.Name + " started")
			if s.eventLog != nil && spec.DeadEventData != nil {
				s.eventLog.Emit(events.EventRespawn, "prefect", spec.Name, "both", spec.DeadEventData)
			}
		}
		return
	}

	// Process alive — check heartbeat staleness.
	hb, err := spec.ReadHeartbeat()
	if err != nil {
		s.logger.Warn("failed to read "+spec.Name+" heartbeat", "error", err)
		return
	}

	if hb == nil {
		// No heartbeat yet (service just started) — give it time.
		return
	}

	if !hb.IsStale(spec.MaxStaleAge) {
		return // heartbeat is fresh
	}

	// Heartbeat is stale — stop and restart.
	s.logger.Warn(spec.Name+" heartbeat is stale, restarting",
		"last_heartbeat", hb.Timestamp,
		"max_age", spec.MaxStaleAge)

	if err := spec.StopStale(pid); err != nil {
		s.logger.Error("failed to stop stale "+spec.Name+" process", "error", err)
	}

	if err := spec.StartStale(); err != nil {
		s.logger.Error("failed to restart "+spec.Name, "error", err)
	} else {
		s.logger.Info(spec.Name + " restarted via heartbeat staleness")
		if s.eventLog != nil {
			s.eventLog.Emit(events.EventRespawn, "prefect", spec.Name, "both", spec.StaleEventData)
		}
	}
}

// checkForgeHealth reads the forge heartbeat and restarts if stale.
// The forge runs as a direct background process (not in tmux).
func (s *Prefect) checkForgeHealth(world string) {
	start := func() error {
		return s.runCommand(s.cfg.SolBinary, "forge", "start", "--world="+world)
	}
	s.checkServiceHealth(serviceSpec{
		Name:    "forge",
		ReadPID: func() int { return forge.ReadPID(world) },
		ReadHeartbeat: func() (*serviceHeartbeat, error) {
			hb, err := forge.ReadHeartbeat(world)
			if err != nil || hb == nil {
				return nil, err
			}
			return &serviceHeartbeat{Timestamp: hb.Timestamp, isStale: hb.IsStale}, nil
		},
		MaxStaleAge: s.cfg.ForgeHeartbeatMax,
		StopStale:   func(_ int) error { return forge.StopProcess(world, 10*time.Second) },
		StartDead: func() error {
			forge.ClearPID(world)
			return start()
		},
		StartStale: start,
		// No DeadEventData — forge dead-restart does not emit an event.
		StaleEventData: map[string]any{
			"service": "forge",
			"world":   world,
			"reason":  "heartbeat_stale",
		},
	})
}

// checkSentinelHealth reads the sentinel heartbeat and restarts if stale.
// The sentinel runs as a direct Go process (not a tmux session).
func (s *Prefect) checkSentinelHealth(world string) {
	start := func() error {
		return s.runCommand(s.cfg.SolBinary, "sentinel", "start", "--world="+world)
	}
	s.checkServiceHealth(serviceSpec{
		Name:    "sentinel",
		ReadPID: func() int { return sentinel.ReadPID(world) },
		ReadHeartbeat: func() (*serviceHeartbeat, error) {
			hb, err := sentinel.ReadHeartbeat(world)
			if err != nil || hb == nil {
				return nil, err
			}
			return &serviceHeartbeat{Timestamp: hb.Timestamp, isStale: hb.IsStale}, nil
		},
		MaxStaleAge: s.cfg.SentinelHeartbeatMax,
		StopStale: func(pid int) error {
			if proc, err := os.FindProcess(pid); err == nil {
				_ = proc.Signal(syscall.SIGTERM)
				time.Sleep(500 * time.Millisecond)
				if IsRunning(pid) {
					_ = proc.Signal(syscall.SIGKILL)
				}
			}
			sentinel.ClearPID(world)
			return nil
		},
		StartDead:  start,
		StartStale: start,
		// Skip dead restart if the heartbeat is still fresh (recovery window).
		SkipDeadRestart: func() bool {
			hb, _ := sentinel.ReadHeartbeat(world)
			return hb != nil && !hb.IsStale(s.cfg.SentinelHeartbeatMax)
		},
		DeadEventData: map[string]any{
			"service": "sentinel",
			"world":   world,
			"reason":  "not_running",
		},
		StaleEventData: map[string]any{
			"service": "sentinel",
			"world":   world,
			"reason":  "heartbeat_stale",
		},
	})
}

// checkSphereDaemons checks whether supervised sphere daemons (ledger,
// broker) are alive and restarts any that are dead. Uses PID files and tmux
// session presence for liveness detection. Additionally checks ledger heartbeat
// staleness (like forge).
// Chronicle is supervised separately via checkChronicleHealth (heartbeat-based).
// Requires cfg.SolBinary to be set — skips silently if empty.
// Must be called with s.mu held.
func (s *Prefect) checkSphereDaemons() {
	if s.cfg.SolBinary == "" {
		return
	}

	for _, d := range supervisedSphereDaemons {
		// Check if daemon is alive via PID file.
		pid := ReadDaemonPID(d.Name)
		if pid > 0 && IsRunning(pid) {
			continue
		}

		// For daemons with tmux sessions, also check session presence.
		if d.Session != "" && s.sessions.Exists(d.Session) {
			continue
		}

		s.logger.Warn("sphere daemon dead", "daemon", d.Name)

		var err error
		if d.Detached {
			err = s.startDaemonProcess(d.Name, s.cfg.SolBinary, d.Args...)
		} else {
			err = s.runCommand(s.cfg.SolBinary, d.Args...)
		}

		if err != nil {
			s.logger.Error("failed to restart sphere daemon",
				"daemon", d.Name, "error", err)
			continue
		}

		s.logger.Info("restarted sphere daemon", "daemon", d.Name)

		if s.eventLog != nil {
			s.eventLog.Emit(events.EventRespawn, "prefect", d.Name, "both", map[string]any{
				"daemon": d.Name,
				"type":   "sphere",
			})
		}
	}

	// Check ledger heartbeat staleness (ledger is a detached process).
	s.checkLedgerHealth()
}

// checkLedgerHealth reads the ledger heartbeat and restarts if stale.
// The ledger runs as a detached Go process (not a tmux session).
// Dead-process restart is handled by checkSphereDaemons; this only handles stale heartbeats.
func (s *Prefect) checkLedgerHealth() {
	s.checkServiceHealth(serviceSpec{
		Name:    "ledger",
		ReadPID: func() int { return ReadDaemonPID("ledger") },
		ReadHeartbeat: func() (*serviceHeartbeat, error) {
			hb, err := ledger.ReadHeartbeat()
			if err != nil || hb == nil {
				return nil, err
			}
			return &serviceHeartbeat{Timestamp: hb.Timestamp, isStale: hb.IsStale}, nil
		},
		MaxStaleAge: s.cfg.LedgerHeartbeatMax,
		StopStale:   func(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) },
		// StartDead is nil — checkSphereDaemons handles dead ledger restarts.
		StartStale: func() error {
			return s.startDaemonProcess("ledger", s.cfg.SolBinary, "ledger", "run")
		},
		// No DeadEventData — dead ledger restart is handled (and emitted) elsewhere.
		StaleEventData: map[string]any{
			"daemon": "ledger",
			"type":   "sphere",
			"reason": "heartbeat_stale",
		},
	})
}

// checkChronicleHealth reads the chronicle heartbeat and restarts if stale or missing.
// Chronicle runs as a direct background process supervised via heartbeat file.
// Must be called with s.mu held.
func (s *Prefect) checkChronicleHealth() {
	start := func() error {
		return s.startDaemonProcess("chronicle", s.cfg.SolBinary, "chronicle", "run")
	}
	s.checkServiceHealth(serviceSpec{
		Name:    "chronicle",
		ReadPID: func() int { return ReadDaemonPID("chronicle") },
		ReadHeartbeat: func() (*serviceHeartbeat, error) {
			hb, err := chronicle.ReadHeartbeat()
			if err != nil || hb == nil {
				return nil, err
			}
			return &serviceHeartbeat{Timestamp: hb.Timestamp, isStale: hb.IsStale}, nil
		},
		MaxStaleAge: s.cfg.ChronicleHeartbeatMax,
		StopStale:   func(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) },
		StartDead:   start,
		StartStale:  start,
		DeadEventData: map[string]any{
			"daemon": "chronicle",
			"type":   "sphere",
			"reason": "process_dead",
		},
		StaleEventData: map[string]any{
			"daemon": "chronicle",
			"type":   "sphere",
			"reason": "heartbeat_stale",
		},
	})
}

// defaultStartDaemonProcess starts a daemon as a detached background process,
// matching the approach used by `sol up`. Delegates to processutil.StartDaemon
// for the shared launch implementation.
func defaultStartDaemonProcess(daemon string, binPath string, args ...string) error {
	logPath := filepath.Join(config.RuntimeDir(), daemon+".log")
	_, err := processutil.StartDaemon(logPath, append(os.Environ(), "SOL_HOME="+config.Home()), binPath, args...)
	return err
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
// A world is sentineled when its sentinel process is alive (PID check)
// and its heartbeat is fresh. When the sentinel is actively assessing
// agents (heartbeat status = "assessing"), it is still considered active
// but the prefect should skip agent respawning for that world.
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
		// Check if sentinel process is alive via PID file.
		pid := sentinel.ReadPID(w.World)
		if pid > 0 && IsRunning(pid) {
			worlds[w.World] = true
		}
	}
	return worlds
}

// getSleepingWorlds returns the set of worlds marked as sleeping in their config.
// Only checks worlds that have active agents, to avoid unnecessary config reads.
func (s *Prefect) getSleepingWorlds(agents []store.Agent) map[string]bool {
	// Collect distinct worlds.
	worldSet := make(map[string]bool)
	for _, a := range agents {
		worldSet[a.World] = false
	}

	result := make(map[string]bool)
	for world := range worldSet {
		if config.IsSleeping(world) {
			result[world] = true
		}
	}
	return result
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

		// Skip worlds outside the configured scope.
		if !s.worldAllowed(agent.World) {
			continue
		}
		sessName := config.SessionName(agent.World, agent.Name)
		if s.sessions.Exists(sessName) {
			if err := s.sessions.Stop(sessName, false); err != nil {
				s.logger.Error("failed to stop session during shutdown",
					"agent", agent.Name, "world", agent.World, "error", err)
			} else {
				stopped++
			}
		}
		// Set agent to stalled (hooks persist for recovery).
		if err := s.sphereStore.UpdateAgentState(agent.ID, store.AgentStalled, agent.ActiveWrit); err != nil {
			s.logger.Error("failed to set agent stalled during shutdown",
				"agent", agent.Name, "error", err)
		}
	}

	// Stop consul process if enabled.
	if s.cfg.ConsulEnabled {
		pid := ReadDaemonPID("consul")
		if pid > 0 && IsRunning(pid) {
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
				s.logger.Error("failed to stop consul process during shutdown", "pid", pid, "error", err)
			} else {
				stopped++
				s.logger.Info("consul process stopped during shutdown", "pid", pid)
			}
		}
	}

	// Stop world infrastructure services (sentinel, forge).
	worlds, err := s.sphereStore.ListWorlds()
	if err != nil {
		s.logger.Error("failed to list worlds during shutdown", "error", err)
	} else {
		for _, world := range worlds {
			if config.IsSleeping(world.Name) {
				continue
			}
			if !s.worldAllowed(world.Name) {
				continue
			}
			// Stop session-based services.
			for _, svc := range worldServices {
				sessName := config.SessionName(world.Name, svc)
				if s.sessions.Exists(sessName) {
					if err := s.sessions.Stop(sessName, false); err != nil {
						s.logger.Error("failed to stop world service during shutdown",
							"service", svc, "world", world.Name, "error", err)
					} else {
						stopped++
						s.logger.Info("world service stopped during shutdown",
							"service", svc, "world", world.Name)
					}
				}
			}
			// Stop sentinel (runs as direct Go process with PID file).
			if pid := sentinel.ReadPID(world.Name); pid > 0 && IsRunning(pid) {
				if proc, err := os.FindProcess(pid); err == nil {
					if err := proc.Signal(syscall.SIGTERM); err == nil {
						stopped++
						s.logger.Info("sentinel stopped during shutdown", "world", world.Name)
					} else {
						s.logger.Error("failed to stop sentinel during shutdown",
							"world", world.Name, "error", err)
					}
				}
			}
			// Stop forge (runs as direct background process, not in tmux).
			forgePID := forge.ReadPID(world.Name)
			if forgePID > 0 {
				if forge.IsRunning(forgePID) {
					if err := forge.StopProcess(world.Name, 10*time.Second); err != nil {
						s.logger.Error("failed to stop forge during shutdown",
							"world", world.Name, "error", err)
					} else {
						stopped++
						s.logger.Info("forge stopped during shutdown", "world", world.Name)
					}
				} else {
					// Process already dead — clean up stale PID file.
					forge.ClearPID(world.Name)
				}
			}
			// Also stop any active merge session.
			mergeSessName := config.SessionName(world.Name, "forge-merge")
			if s.sessions.Exists(mergeSessName) {
				if err := s.sessions.Stop(mergeSessName, true); err != nil {
					s.logger.Error("failed to stop forge merge session during shutdown",
						"world", world.Name, "error", err)
				}
			}
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

// worldAllowed returns true if the given world is in the configured Worlds
// list, or if no world filter is configured (empty list = all worlds).
func (s *Prefect) worldAllowed(world string) bool {
	if len(s.cfg.Worlds) == 0 {
		return true
	}
	for _, w := range s.cfg.Worlds {
		if w == world {
			return true
		}
	}
	return false
}

// dirExists checks if a directory exists.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
