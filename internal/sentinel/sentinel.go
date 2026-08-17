// Package sentinel monitors agents in a single world: it respawns stalled
// sessions, recovers stuck writs and merge requests, and reaps orphaned
// disk resources. The package is split by domain:
//
//   - sentinel.go: Config/DefaultConfig, store/session/event interfaces, and
//     patrol coordination (New, Run, Patrol, patrol, writeHeartbeat).
//   - assess.go: the AI assessment pipeline that decides whether a working
//     agent is progressing, stuck, or idle.
//   - agent_recovery.go: per-agent lifecycle recovery (stalled, zombie, and
//     idle-reap handling; respawn count bookkeeping).
//   - mr_recovery.go: merge request and writ recovery (recast, resolution
//     dispatch, stale claim release).
//   - cleanup.go: orphaned resource cleanup (worktrees, session metadata,
//     tethers, branches) and per-patrol map pruning.
//   - heartbeat.go: heartbeat file I/O.
package sentinel

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/logutil"
	"github.com/nevinsm/sol/internal/runtime/loader"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// Config holds sentinel configuration.
type Config struct {
	World              string
	PatrolInterval     time.Duration // default: 3 minutes
	MaxRespawns        int           // default: 2 (per writ)
	MaxRecastAttempts  int           // default: 3 (per failed MR writ)
	CaptureLines       int           // default: 80 (lines of tmux output to capture)
	AssessCommand      string        // default: "claude -p" (AI assessment command)
	AssessTimeout      time.Duration // default: 30 seconds — timeout for AI assessment command
	SourceRepo         string        // path to source git repo
	SolHome            string        // SOL_HOME path
	IdleReapTimeout    time.Duration // default: 10 minutes — reap idle agents older than this
	ClaimTTL           time.Duration // default: 30 minutes — release MR claims older than this
	ForgeMaxAttempts   int           // default: 3 — max forge merge attempts before marking MR failed
}

// DefaultConfig returns a Config with default values.
// The AssessCommand is resolved from the world's runtime when possible,
// falling back to "claude -p" if the runtime is not found.
func DefaultConfig(world, sourceRepo, solHome string) Config {
	assessCmd := loader.ResolveCalloutCommand(world, "sentinel")
	return Config{
		World:             world,
		PatrolInterval:    3 * time.Minute,
		MaxRespawns:       2,
		MaxRecastAttempts: 3,
		CaptureLines:      80,
		AssessCommand:     assessCmd,
		AssessTimeout:     30 * time.Second,
		SourceRepo:        sourceRepo,
		SolHome:           solHome,
		IdleReapTimeout:   10 * time.Minute,
		ClaimTTL:          30 * time.Minute,
		ForgeMaxAttempts:  3,
	}
}

// SphereStore is the subset of sphere store operations the sentinel needs.
type SphereStore interface {
	GetAgent(id string) (*store.Agent, error)
	ListAgents(world string, state string) ([]store.Agent, error)
	UpdateAgentState(id, state, activeWrit string) error
	CreateAgent(name, world, role string) (string, error)
	EnsureAgent(name, world, role string) error
	DeleteAgent(id string) error
	SendProtocolMessage(sender, recipient, protoType string, payload any) (string, error)
	CreateEscalation(severity, source, description string, sourceRef ...string) (string, error)
	ListEscalationsBySourceRef(sourceRef string) ([]store.Escalation, error)
}

// WorldStore is the subset of world store operations the sentinel needs.
type WorldStore interface {
	GetWrit(id string) (*store.Writ, error)
	UpdateWrit(id string, updates store.WritUpdates) error
	SafelyReopenWrit(writID string, allowedFromStatuses []string) (bool, error)
	SetWritMetadata(id string, metadata map[string]any) error
	ListWrits(filters store.ListFilters) ([]store.Writ, error)
	ListMergeRequests(phase string) ([]store.MergeRequest, error)
	ListMergeRequestsByWrit(writID string, phase string) ([]store.MergeRequest, error)
	ListBlockedMergeRequests() ([]store.MergeRequest, error)
	ReleaseStaleClaims(ttl time.Duration, maxAttempts int) (int, error)
}

// SessionChecker abstracts session operations for testability.
type SessionChecker interface {
	Exists(name string) bool
	Capture(name string, lines int) (string, error)
	Start(name, workdir, cmd string, env map[string]string, role, world string) error
	Stop(name string, force bool) error
	Inject(name string, text string, submit bool) error
	Cycle(name, workdir, cmd string, env map[string]string, role, world string) error
}

// AssessmentResult is the structured output from an AI assessment.
type AssessmentResult struct {
	Status          string `json:"status"`           // progressing, stuck, waiting, idle
	Confidence      string `json:"confidence"`       // high, medium, low
	Reason          string `json:"reason"`
	SuggestedAction string `json:"suggested_action"` // none, nudge, escalate
	NudgeMessage    string `json:"nudge_message"`
}

type assessFunc func(agent store.Agent, sessionName, output string) (*AssessmentResult, error)

// CastResult holds the output of a successful cast operation (matches dispatch.CastResult).
type CastResult struct {
	WritID  string
	AgentName   string
	SessionName string
	WorktreeDir string
}

type respawnKey struct {
	AgentID    string
	WritID string
}

// EventReader abstracts reading events for testability.
type EventReader interface {
	Read(opts events.ReadOpts) ([]events.Event, error)
}

// Sentinel monitors agents in a single world.
type Sentinel struct {
	config        Config
	sphereStore   SphereStore
	worldStore    WorldStore
	sessions      SessionChecker
	logger        *events.Logger
	eventReader   EventReader       // reads raw events for frequency checks
	respawnCounts            map[respawnKey]int
	reconcileFailed          bool                 // true if event log was unreadable during reconciliation
	// lastCastTime is intentionally reset on restart — the 6-minute dedup window
	// resets cleanly; no writ is double-cast from an empty map (contrast:
	// resolutionDispatchCounts persists counts in writ metadata and survives restart).
	lastCastTime             map[string]time.Time // dedup guard: writ ID → last cast time
	resolutionDispatchCounts map[string]int       // blocker writ ID → dispatch attempt count
	lastCaptures             map[string]string    // agent ID → hash of last captured output
	assessFn                 assessFunc           // nil = use real AI call
	castFn                   func(writID string) (*CastResult, error) // nil = skip recast
	nowFn                    func() time.Time     // nil = time.Now, for testing

	// Per-patrol counters, reset at start of each patrol.
	patrolAssessed int
	patrolNudged   int

	// Cumulative patrol count for heartbeat.
	patrolCount int
}

// New creates a new Sentinel.
func New(cfg Config, sphere SphereStore, world WorldStore,
	sessions SessionChecker, logger *events.Logger) *Sentinel {
	s := &Sentinel{
		config:        cfg,
		sphereStore:   sphere,
		worldStore:    world,
		sessions:      sessions,
		logger:        logger,
		respawnCounts:            make(map[respawnKey]int),
		lastCastTime:             make(map[string]time.Time),
		resolutionDispatchCounts: make(map[string]int),
		lastCaptures:             make(map[string]string),
	}
	// Create event reader for handoff frequency checks.
	if cfg.SolHome != "" {
		s.eventReader = events.NewReader(cfg.SolHome, false)
	}
	return s
}

// SetAssessFunc sets a custom assessment function for testing.
// When set, this function is called instead of the real AI assessment.
func (w *Sentinel) SetAssessFunc(fn func(agent store.Agent, sessionName, output string) (*AssessmentResult, error)) {
	w.assessFn = fn
}

// SetCastFunc sets the function used to re-cast failed MR writs.
// When nil, the sentinel skips the recast step during patrol.
func (w *Sentinel) SetCastFunc(fn func(writID string) (*CastResult, error)) {
	w.castFn = fn
}

// SetNowFunc sets a custom time function for testing.
// When nil, time.Now is used.
func (w *Sentinel) SetNowFunc(fn func() time.Time) {
	w.nowFn = fn
}

// now returns the current time, using nowFn if set (for testing).
func (w *Sentinel) now() time.Time {
	if w.nowFn != nil {
		return w.nowFn()
	}
	return time.Now()
}

// recastBackoffIntervals defines the minimum wait time before each recast attempt.
// Index maps to attempt number: [0] = 10m after failure, [1] = 30m after 1st, [2] = 60m after 2nd.
var recastBackoffIntervals = []time.Duration{
	10 * time.Minute,
	30 * time.Minute,
	60 * time.Minute,
}

func (w *Sentinel) agentID() string {
	return w.config.World + "/sentinel"
}

// Register registers the sentinel agent in the sphere store.
// Agent ID: "{world}/sentinel", role: "sentinel".
// Creates if not exists, reuses if already registered.
func (w *Sentinel) Register() error {
	return w.sphereStore.EnsureAgent("sentinel", w.config.World, "sentinel")
}

// Run starts the sentinel patrol loop. Blocks until context is cancelled.
// Patrols immediately on start, then on each interval.
func (w *Sentinel) Run(ctx context.Context) error {
	if err := w.Register(); err != nil {
		return fmt.Errorf("failed to register sentinel: %w", err)
	}

	// PID file lifecycle is owned by the cmd-layer daemon.RunBootstrap wrapper
	// (see cmd/sentinel.go sentinelRunCmd). sentinel.Run previously wrote/cleared
	// the pidfile itself; that responsibility moved out in sol-06e76378be1408bf.

	if err := w.sphereStore.UpdateAgentState(w.agentID(), "working", ""); err != nil {
		return fmt.Errorf("failed to set sentinel working: %w", err)
	}

	// Reconcile respawn counts from event history before first patrol.
	// This prevents infinite respawn loops after sentinel crash-restart.
	w.reconcileRespawnCounts()

	// Write initial heartbeat.
	w.writeHeartbeat("running", 0, 0, 0, 0, "")

	// Patrol immediately.
	w.runPatrolLogged(ctx)

	ticker := time.NewTicker(w.config.PatrolInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Write final heartbeat with stopping status.
			w.writeHeartbeat("stopping", w.patrolCount, 0, 0, 0, "")
			_ = w.sphereStore.UpdateAgentState(w.agentID(), "idle", "")
			if w.logger != nil {
				w.logger.Emit(events.EventSessionStop, w.agentID(), w.agentID(), "feed",
					map[string]any{"world": w.config.World, "component": "sentinel"})
			}
			return nil
		case <-ticker.C:
			w.runPatrolLogged(ctx)
		}
	}
}

// runPatrolLogged runs one patrol cycle and surfaces any error via the event
// log and slog. The run loop intentionally continues on error so that transient
// infrastructure failures (sphere DB locked, sleep-status file unreadable) do
// not silently halt session respawn, tether reaping, and stuck-writ recovery —
// the next tick re-runs and will succeed once the failure clears. See CWE-391.
func (w *Sentinel) runPatrolLogged(ctx context.Context) {
	if err := w.patrol(ctx); err != nil {
		if w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit", map[string]any{
				"action": "patrol",
				"error":  err.Error(),
			})
		}
		slog.Error("sentinel: patrol failed", "world", w.config.World, "error", err)
	}
}

// writeHeartbeat writes a heartbeat file with current sentinel state.
func (w *Sentinel) writeHeartbeat(status string, patrolCount, agentsChecked, stalledCount, reapedCount int, lastDuration string) {
	hb := &Heartbeat{
		Timestamp:          w.now(),
		Status:             status,
		PatrolCount:        patrolCount,
		AgentsChecked:      agentsChecked,
		StalledCount:       stalledCount,
		ReapedCount:        reapedCount,
		LastPatrolDuration: lastDuration,
	}
	if err := WriteHeartbeat(w.config.World, hb); err != nil {
		if w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit", map[string]any{
				"action": "write_heartbeat", "error": err.Error(),
			})
		}
	}
}

// Patrol runs one patrol cycle across all agents in the world. Exported for testing.
func (w *Sentinel) Patrol(ctx context.Context) error {
	return w.patrol(ctx)
}

// patrol runs one patrol cycle across all agents in the world.
func (w *Sentinel) patrol(ctx context.Context) error {
	patrolStart := w.now()
	w.patrolCount++

	// If the world is sleeping, write heartbeat but skip all agent work.
	// Prefect stops starting new sentinels for sleeping worlds, but an existing
	// sentinel must keep writing heartbeats so it is not mistaken for dead.
	sleeping, err := config.IsSleeping(w.config.World)
	if err != nil {
		return fmt.Errorf("failed to check sleep status for world %q: %w", w.config.World, err)
	}
	if sleeping {
		w.writeHeartbeat("running", w.patrolCount, 0, 0, 0, "")
		return nil
	}

	agents, err := w.sphereStore.ListAgents(w.config.World, "")
	if err != nil {
		return fmt.Errorf("failed to list agents: %w", err)
	}

	w.patrolAssessed = 0
	w.patrolNudged = 0

	// Run all writ-recovery operations before agent checks.
	recastCount, resolutionDispatched, releasedCount, doneRecovered, tetheredRecovered := w.recoverWrits()

	var healthyCount, stalledCount, zombieCount, reapedCount int
	var actionsTaken []string

	// Check all agents' tether directories for closed writs.
	// This covers both outpost and persistent agents.
	reaped := w.checkClosedWritTethers(agents, &reapedCount, &actionsTaken)

	// Monitor outpost agents — envoys are human-supervised,
	// forge is supervised by prefect via heartbeat (ADR-0027).
	var activeAgents []store.Agent
	for _, a := range agents {
		if a.Role == "outpost" {
			if !reaped[a.ID] {
				activeAgents = append(activeAgents, a)
			}
		}
	}

	for _, agent := range activeAgents {
		sessionName := config.SessionName(w.config.World, agent.Name)
		alive := w.sessions.Exists(sessionName)

		switch {
		case agent.State == "working" && alive:
			// Working agent with live session — check for progress.
			if err := w.checkProgress(ctx, agent, sessionName); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
						"agent": agent.ID, "action": "check_progress", "error": err.Error(),
					})
				}
			}
			healthyCount++

		case agent.State == "working" && !alive && tether.IsTethered(w.config.World, agent.Name, agent.Role):
			// Session died while tether directory is non-empty — stalled.
			stalledCount++
			if err := w.handleStalled(agent); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
						"agent": agent.ID, "action": "handle_stalled", "error": err.Error(),
					})
				}
			}
			actionsTaken = append(actionsTaken, "stalled:"+agent.Name)

		case agent.State == "working" && !alive && !tether.IsTethered(w.config.World, agent.Name, agent.Role):
			// Session dead, no tether — likely partial resolve or lost tether.
			stalledCount++
			if err := w.handleOrphanedWorking(agent); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
						"agent": agent.ID, "action": "handle_orphaned_working", "error": err.Error(),
					})
				}
			}
			actionsTaken = append(actionsTaken, "orphaned:"+agent.Name)

		case agent.State == "idle" && alive && tether.IsTethered(w.config.World, agent.Name, agent.Role):
			// Inconsistent: idle agent with live session and tether files.
			// This state arises if returnWorkToOpen crashed between setting
			// the agent idle and cleaning up its resources. Treat as zombie —
			// stop the session and clean up.
			zombieCount++
			if w.logger != nil {
				w.logger.Emit("sentinel_zombie_idle_tethered", w.agentID(), agent.ID, "audit", map[string]any{
					"agent": agent.ID, "name": agent.Name, "role": agent.Role,
				})
			}
			if err := w.handleZombie(agent); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
						"agent": agent.ID, "action": "handle_zombie", "error": err.Error(),
					})
				}
			}
			actionsTaken = append(actionsTaken, "zombie:"+agent.Name)

		case agent.State == "idle" && alive && !tether.IsTethered(w.config.World, agent.Name, agent.Role):
			// Idle agent with live session and no tether — zombie.
			zombieCount++
			if err := w.handleZombie(agent); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
						"agent": agent.ID, "action": "handle_zombie", "error": err.Error(),
					})
				}
			}
			actionsTaken = append(actionsTaken, "zombie:"+agent.Name)

		case agent.State == "stalled":
			// Already stalled — retry recovery.
			stalledCount++
			if err := w.handleStalled(agent); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
						"agent": agent.ID, "action": "handle_stalled", "error": err.Error(),
					})
				}
			}
			actionsTaken = append(actionsTaken, "stalled:"+agent.Name)

		case agent.State == "idle" && !alive && w.config.IdleReapTimeout > 0 &&
			w.now().Sub(agent.UpdatedAt) > w.config.IdleReapTimeout:
			// Idle agent past reap threshold with no session — reap it.
			reapedCount++
			if err := w.reapIdleAgent(agent); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
						"agent": agent.ID, "action": "reap_idle", "error": err.Error(),
					})
				}
			}
			actionsTaken = append(actionsTaken, "reaped:"+agent.Name)

		default:
			// Healthy idle or no session needed.
			healthyCount++
		}
	}

	// Check for handoff frequency issues (possible handoff loops).
	handoffLoops := w.checkHandoffFrequency(activeAgents)

	// Run branch pruning, orphaned resource cleanup, and map pruning.
	branchesPruned, orphansCleaned := w.cleanupResources(agents, activeAgents)

	if w.logger != nil {
		w.logger.Emit(events.EventPatrol, w.agentID(), w.agentID(), "feed",
			map[string]any{
				"world":           w.config.World,
				"total":           len(activeAgents),
				"healthy":         healthyCount,
				"stalled":         stalledCount,
				"zombies":         zombieCount,
				"reaped":          reapedCount,
				"recast":                  recastCount,
				"resolution_dispatched":    resolutionDispatched,
				"released_claims":          releasedCount,
				"done_recovered":           doneRecovered,
				"tethered_recovered":       tetheredRecovered,
				"branches_pruned": branchesPruned,
				"orphans_cleaned": orphansCleaned,
				"handoff_loops":   handoffLoops,
				"assessed":        w.patrolAssessed,
				"nudged":          w.patrolNudged,
				"actions":         actionsTaken,
			})
	}

	// Write heartbeat with patrol results.
	patrolDuration := w.now().Sub(patrolStart)
	w.writeHeartbeat("running", w.patrolCount, len(activeAgents), stalledCount, reapedCount, patrolDuration.String())

	// Best-effort log rotation.
	logutil.TruncateIfNeeded(filepath.Join(w.config.SolHome, w.config.World, "sentinel.log"), logutil.DefaultMaxLogSize)

	return nil
}
