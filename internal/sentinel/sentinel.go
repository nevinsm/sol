package sentinel

import (
	"context"
	"crypto/sha256"
	"errors"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/runtime"
	"github.com/nevinsm/sol/internal/runtime/loader"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/flock"
	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/logutil"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/softfail"
	"github.com/nevinsm/sol/internal/startup"
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

// reconcileRespawnCounts seeds in-memory respawn counts from the event log.
//
// CRASH SAFETY: Respawn counts are in-memory and lost on sentinel restart.
// Without reconciliation, a restarted sentinel would reset all counts to 0,
// potentially causing infinite respawn loops for persistently failing agents
// (each restart resets the count, never reaching MaxRespawns). This method
// reads recent respawn events from the event log and reconstructs the counts,
// ensuring crash-restart doesn't bypass the MaxRespawns limit.
func (w *Sentinel) reconcileRespawnCounts() {
	if w.eventReader == nil {
		return // no event reader configured (e.g., in tests)
	}

	// Look back 24 hours — respawn counts are per-writ, and any older
	// respawns are for work that has likely been resolved or reassigned.
	evts, err := w.eventReader.Read(events.ReadOpts{
		Type:   events.EventRespawn,
		Source: w.agentID(),
		Since:  w.now().Add(-24 * time.Hour),
	})
	if err != nil {
		// SAFETY: If the event log is unreadable (locked, corrupted), we must
		// NOT start with zero counts — that would grant extra respawns beyond
		// MaxRespawns. Instead, mark reconciliation as failed so handleStalled
		// uses MaxRespawns as the conservative default for unknown agents.
		w.reconcileFailed = true
		if w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit", map[string]any{
				"error":   err.Error(),
				"context": "reconcileRespawnCounts: event log unreadable, using conservative respawn limits",
			})
		}
		return
	}

	for _, ev := range evts {
		payload, ok := ev.Payload.(map[string]any)
		if !ok {
			continue
		}
		agentID, _ := payload["agent"].(string)
		writID, _ := payload["writ"].(string)
		if agentID == "" {
			continue
		}
		key := respawnKey{AgentID: agentID, WritID: writID}
		w.respawnCounts[key]++
	}
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

// cleanupResources runs branch pruning, orphaned resource cleanup, and map pruning.
// agents is the full agent list for cleanupOrphanedResources (all roles).
// activeAgents is the active outpost subset (excluding reaped agents) for map pruning.
// Returns counts for each operation for patrol event telemetry.
func (w *Sentinel) cleanupResources(agents []store.Agent, activeAgents []store.Agent) (branchesPruned, orphansCleaned int) {
	// Prune local branches whose remote tracking branch is gone.
	branchesPruned = w.pruneOrphanedBranches()
	// Clean up orphaned resources (worktrees, session metadata, tethers).
	// Uses the full agent list so envoy and forge directory sweeps check agents of every role.
	orphansCleaned = w.cleanupOrphanedResources(agents)
	// Prune stale entries for agents no longer in the active outpost set.
	activeOutpostIDs := make(map[string]bool, len(activeAgents))
	for _, a := range activeAgents {
		activeOutpostIDs[a.ID] = true
	}
	w.pruneCaptures(activeOutpostIDs)
	w.pruneRespawnCounts(activeOutpostIDs)
	return
}

// pruneCaptures removes hash entries for agents that are no longer working.
func (w *Sentinel) pruneCaptures(workingAgentIDs map[string]bool) {
	for key := range w.lastCaptures {
		if !workingAgentIDs[key] {
			delete(w.lastCaptures, key)
		}
	}
}

// pruneRespawnCounts removes respawn count entries for agents that are no longer active.
func (w *Sentinel) pruneRespawnCounts(activeAgentIDs map[string]bool) {
	for key := range w.respawnCounts {
		if !activeAgentIDs[key.AgentID] {
			delete(w.respawnCounts, key)
		}
	}
}

// checkClosedWritTethers iterates all agents' tether directories and handles
// closed writs. For outpost agents, a closed writ triggers a full reap. For
// persistent agents, only the closed tether file is removed.
// Returns a set of agent IDs that were reaped.
func (w *Sentinel) checkClosedWritTethers(agents []store.Agent, reapedCount *int, actionsTaken *[]string) map[string]bool {
	reaped := make(map[string]bool)
	if w.worldStore == nil {
		return reaped
	}

	for _, agent := range agents {
		tetheredWrits, err := tether.List(w.config.World, agent.Name, agent.Role)
		if err != nil {
			if w.logger != nil {
				w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
					"agent":  agent.ID,
					"action": "list_tethered_writs",
					"error":  err.Error(),
				})
			}
			continue
		}
		if len(tetheredWrits) == 0 {
			continue
		}

		for _, writID := range tetheredWrits {
			writ, err := w.worldStore.GetWrit(writID)
			if err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
						"agent":  agent.ID,
						"writ":   writID,
						"action": "get_writ_for_closed_check",
						"error":  err.Error(),
					})
				}
				continue
			}
			if writ.Status != "closed" {
				continue
			}

			// Contract with dispatch: dispatch.Resolve closes the writ BEFORE
			// clearing the tether and stopping the session. If a resolve is in
			// progress, the closed-writ + tether-file combination is a transient
			// state — not an orphan. Skip this agent for this patrol cycle and
			// let dispatch.Resolve finish; the next patrol will see the final
			// state. See internal/dispatch/resolve.go (IsResolveInProgress).
			if dispatch.IsResolveInProgress(w.config.World, agent.Name, agent.Role) {
				if w.logger != nil {
					w.logger.Emit("sentinel_action", w.agentID(), agent.ID, "audit",
						map[string]any{
							"agent":  agent.ID,
							"writ":   writID,
							"action": "skip_reap_resolve_in_progress",
						})
				}
				break
			}

			if agent.Role == "outpost" {
				// Outpost agent with closed writ: full reap.
				sessionName := config.SessionName(w.config.World, agent.Name)
				*reapedCount++
				if err := w.reapClosedWritAgent(agent, sessionName, writ.CloseReason); err != nil {
					if w.logger != nil {
						w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
							"agent": agent.ID, "action": "reap_closed_writ", "error": err.Error(),
						})
					}
				}
				*actionsTaken = append(*actionsTaken, "reaped_closed_writ:"+agent.Name)
				reaped[agent.ID] = true
				break // agent is deleted, no point checking more writs
			}

			// Persistent agent: remove just this tether, keep agent alive.
			if err := tether.ClearOne(w.config.World, agent.Name, writID, agent.Role); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
						"agent": agent.ID, "writ": writID, "action": "clear_tether_failed", "error": err.Error(),
					})
				}
			}
			if agent.ActiveWrit == writID {
				_ = w.sphereStore.UpdateAgentState(agent.ID, agent.State, "")
				agent.ActiveWrit = "" // update local copy
			}
			if w.logger != nil {
				w.logger.Emit("sentinel_action", w.agentID(), agent.ID, "feed",
					map[string]any{
						"agent":        agent.ID,
						"writ":         writID,
						"close_reason": writ.CloseReason,
						"action":       "cleared_closed_tether",
					})
			}
		}
	}

	return reaped
}

// captureErrorSentinel is stored in lastCaptures when a session capture fails.
// It is not a valid sha256 hex string, so any subsequent successful capture
// will always differ from it — ensuring we establish a fresh baseline after
// a failure window rather than comparing against a potentially stale pre-failure hash.
const captureErrorSentinel = "capture_error"

// checkProgress checks whether a working agent with a live session is making progress.
// If the tmux output hasn't changed since the last patrol, triggers AI assessment.
func (w *Sentinel) checkProgress(ctx context.Context, agent store.Agent, sessionName string) error {
	output, err := w.sessions.Capture(sessionName, w.config.CaptureLines)
	if err != nil {
		if w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit",
				map[string]any{"error": err.Error(), "action": "capture_failed", "session": sessionName})
		}
		// Record the failure so the next successful capture compares against the
		// sentinel (not a stale pre-failure hash), avoiding a false negative where
		// output that changed just before a stall would mask the stall.
		w.lastCaptures[agent.ID] = captureErrorSentinel
		return nil // can't capture, skip assessment
	}

	hash := sha256Hash(output)
	lastHash, seen := w.lastCaptures[agent.ID]
	w.lastCaptures[agent.ID] = hash

	if !seen {
		return nil // first patrol for this agent, establish baseline
	}
	if hash != lastHash {
		return nil // output changed, agent is making progress
	}

	// No change since last patrol — assess with AI.
	return w.assessAgent(ctx, agent, sessionName, output)
}

func sha256Hash(s string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(s)))
}

// assessAgent uses an AI model to evaluate a potentially stuck agent.
func (w *Sentinel) assessAgent(ctx context.Context, agent store.Agent, sessionName, capturedOutput string) error {
	w.patrolAssessed++

	// Update heartbeat to "assessing" status so prefect knows not to respawn agents.
	w.writeHeartbeat("assessing", w.patrolCount, 0, 0, 0, "")

	var result *AssessmentResult
	var err error

	if w.assessFn != nil {
		result, err = w.assessFn(agent, sessionName, capturedOutput)
	} else {
		result, err = w.runAssessment(ctx, agent, capturedOutput)
	}
	if err != nil {
		// AI call failed — log and move on, don't block patrol.
		if w.logger != nil {
			w.logger.Emit("assess_error", w.agentID(), agent.ID, "audit",
				map[string]any{"error": err.Error()})
		}
		return nil
	}

	if w.logger != nil {
		w.logger.Emit(events.EventAssess, w.agentID(), agent.ID, "both",
			map[string]any{
				"agent":      agent.ID,
				"status":     result.Status,
				"confidence": result.Confidence,
				"action":     result.SuggestedAction,
				"reason":     result.Reason,
			})
	}

	return w.actOnAssessment(agent, sessionName, *result)
}

func (w *Sentinel) runAssessment(ctx context.Context, agent store.Agent, capturedOutput string) (*AssessmentResult, error) {
	prompt := buildAssessmentPrompt(agent, capturedOutput, w.config.CaptureLines, w.config.PatrolInterval)

	assessTimeout := w.config.AssessTimeout
	if assessTimeout == 0 {
		assessTimeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, assessTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", w.config.AssessCommand)
	cmd.Stdin = strings.NewReader(prompt)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("assessment command failed: %w", err)
	}

	var result AssessmentResult
	if err := json.Unmarshal(out, &result); err != nil {
		// Couldn't parse response — try to extract JSON from output.
		extracted, extractErr := extractJSON(out)
		if extractErr != nil {
			return nil, fmt.Errorf("unparseable assessment output: %w", err)
		}
		return &extracted, nil
	}

	return &result, nil
}

func buildAssessmentPrompt(agent store.Agent, capturedOutput string, captureLines int, patrolInterval time.Duration) string {
	staleWindow := fmt.Sprintf("%d minutes ago", int(patrolInterval.Minutes()))
	return fmt.Sprintf(`You are a sentinel agent monitoring AI coding agents in a multi-agent
orchestration system. An agent's tmux session output has not changed
since the last patrol cycle (%s). Analyze the session output
below and determine the agent's status.

Agent: %s (ID: %s)
Writ: %s
Session output (last %d lines):
---
%s
---

Respond with ONLY a JSON object (no markdown, no explanation):
{
    "status": "progressing|stuck|waiting|idle",
    "confidence": "high|medium|low",
    "reason": "brief explanation of what the agent appears to be doing",
    "suggested_action": "none|nudge|escalate",
    "nudge_message": "if suggested_action is nudge, the message to send"
}

Status meanings:
- "progressing": Agent is actively working (e.g., long compilation,
  large file write, waiting for a tool call to complete). No action
  needed despite unchanged output.
- "stuck": Agent appears confused, looping, or unable to make progress.
  A nudge with guidance may help.
- "waiting": Agent is waiting for external input or a resource. May
  need a nudge to check its mail or retry.
- "idle": Agent appears to have finished or is not doing anything.
  May be a zombie or may have completed work without calling sol resolve.

Only suggest "escalate" if the situation requires human intervention
(e.g., repeated failures, auth issues, infrastructure problems).`, staleWindow, agent.Name, agent.ID, agent.ActiveWrit, captureLines, capturedOutput)
}

func extractJSON(data []byte) (AssessmentResult, error) {
	s := string(data)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < 0 || end <= start {
		return AssessmentResult{}, fmt.Errorf("no JSON object found in output")
	}
	var result AssessmentResult
	if err := json.Unmarshal([]byte(s[start:end+1]), &result); err != nil {
		return AssessmentResult{}, err
	}
	return result, nil
}

// actOnAssessment acts on an AI assessment result.
func (w *Sentinel) actOnAssessment(agent store.Agent, sessionName string,
	result AssessmentResult) error {

	// Low confidence = no action. Better to wait another patrol cycle
	// than to act on uncertain assessment.
	if result.Confidence == "low" {
		return nil
	}

	switch result.SuggestedAction {
	case "none":
		// Agent is progressing or we're not confident — do nothing.
		return nil

	case "nudge":
		// Inject nudge message into the agent's session.
		if err := w.sessions.Inject(sessionName, result.NudgeMessage, true); err != nil {
			return fmt.Errorf("failed to inject nudge into %s: %w", sessionName, err)
		}
		w.patrolNudged++

		if w.logger != nil {
			w.logger.Emit(events.EventNudge, w.agentID(), agent.ID, "both",
				map[string]any{
					"agent":   agent.ID,
					"message": result.NudgeMessage,
					"reason":  result.Reason,
				})
		}

		// Send informational mail to autarch.
		if _, err := w.sphereStore.SendProtocolMessage(
			w.agentID(), config.Autarch,
			store.ProtoRecoveryNeeded,
			store.RecoveryNeededPayload{
				AgentID:    agent.ID,
				WritID: agent.ActiveWrit,
				Reason:     fmt.Sprintf("nudged: %s", result.Reason),
			},
		); err != nil && w.logger != nil {
			w.logger.Emit("mail_error", w.agentID(), agent.ID, "audit",
				map[string]any{"error": err.Error()})
		}

	case "escalate":
		// Create formal escalation for durable tracking, with dedup to avoid
		// duplicates when agent output is unchanged across patrols.
		escDesc := fmt.Sprintf("Agent %s needs recovery: %s", agent.Name, result.Reason)
		var sourceRef string
		if agent.ActiveWrit != "" {
			sourceRef = "writ:" + agent.ActiveWrit
		}
		if sourceRef != "" {
			if existing, err := w.sphereStore.ListEscalationsBySourceRef(sourceRef); err == nil && len(existing) > 0 {
				// Open escalation already exists — skip creation.
			} else {
				if _, err := w.sphereStore.CreateEscalation("high", w.config.World+"/sentinel", escDesc, sourceRef); err != nil && w.logger != nil {
					w.logger.Emit("escalation_error", w.agentID(), agent.ID, "audit",
						map[string]any{"error": err.Error()})
				}
			}
		} else {
			// No source ref (no active writ) — create without dedup.
			if _, err := w.sphereStore.CreateEscalation("high", w.config.World+"/sentinel", escDesc, sourceRef); err != nil && w.logger != nil {
				w.logger.Emit("escalation_error", w.agentID(), agent.ID, "audit",
					map[string]any{"error": err.Error()})
			}
		}

		// Send RECOVERY_NEEDED protocol message to autarch (live nudge).
		if _, err := w.sphereStore.SendProtocolMessage(
			w.agentID(), config.Autarch,
			store.ProtoRecoveryNeeded,
			store.RecoveryNeededPayload{
				AgentID:    agent.ID,
				WritID: agent.ActiveWrit,
				Reason:     result.Reason,
			},
		); err != nil && w.logger != nil {
			w.logger.Emit("mail_error", w.agentID(), agent.ID, "audit",
				map[string]any{"error": err.Error()})
		}

		if w.logger != nil {
			w.logger.Emit(events.EventStalled, w.agentID(), agent.ID, "both",
				map[string]any{
					"agent":     agent.ID,
					"reason":    result.Reason,
					"escalated": true,
				})
		}
	}

	return nil
}

// handleStalled handles an agent whose session died while work was tethered.
//
// CRASH SAFETY: The in-memory respawnCounts map is incremented BEFORE the
// persistent state change in respawnAgent (UpdateAgentState). If sentinel
// crashes after incrementing but before persisting, the in-memory count is
// lost (harmless — reconcileRespawnCounts recovers from event history on
// restart). If sentinel crashes after respawnAgent emits the respawn event,
// the count is recoverable from the event log.
func (w *Sentinel) handleStalled(agent store.Agent) error {
	key := respawnKey{AgentID: agent.ID, WritID: agent.ActiveWrit}
	attempts := w.respawnCounts[key]

	// If event log reconciliation failed and we have no recorded attempts
	// for this agent, use MaxRespawns as a conservative default to prevent
	// granting extra respawns beyond the configured limit.
	if w.reconcileFailed && attempts == 0 {
		attempts = w.config.MaxRespawns
	}

	if attempts >= w.config.MaxRespawns {
		return w.returnWorkToOpen(agent)
	}

	w.respawnCounts[key]++
	return w.respawnAgent(agent)
}

// respawnAgent restarts a crashed agent's tmux session using the startup
// registry. The tether file is durable, and the Claude Code SessionStart
// hook fires sol prime automatically (GUPP).
func (w *Sentinel) respawnAgent(agent store.Agent) error {
	// Ensure agent state is working before respawn.
	if err := w.sphereStore.UpdateAgentState(agent.ID, "working", agent.ActiveWrit); err != nil {
		return fmt.Errorf("failed to set agent %s working: %w", agent.ID, err)
	}

	writExists := func(id string) bool {
		if id == "" {
			return true
		}
		if w.worldStore == nil {
			return true
		}
		_, err := w.worldStore.GetWrit(id)
		if errors.Is(err, store.ErrNotFound) {
			return false
		}
		return true
	}
	_, err := startup.Respawn(agent.Role, w.config.World, agent.Name, startup.LaunchOpts{
		Sessions:   w.sessions,
		WritExists: writExists,
	})
	if err != nil {
		return fmt.Errorf("failed to respawn session for %s: %w", agent.Name, err)
	}

	key := respawnKey{AgentID: agent.ID, WritID: agent.ActiveWrit}
	attempts := w.respawnCounts[key]

	if w.logger != nil {
		w.logger.Emit(events.EventRespawn, w.agentID(), agent.ID, "both",
			map[string]any{
				"agent":     agent.ID,
				"writ": agent.ActiveWrit,
				"attempt":   attempts,
			})
	}

	// Send informational protocol message.
	if _, err := w.sphereStore.SendProtocolMessage(
		w.agentID(), config.Autarch,
		store.ProtoRecoveryNeeded,
		store.RecoveryNeededPayload{
			AgentID:    agent.ID,
			WritID: agent.ActiveWrit,
			Reason:     fmt.Sprintf("respawned (attempt %d)", attempts),
			Attempts:   attempts,
		},
	); err != nil && w.logger != nil {
		w.logger.Emit("mail_error", w.agentID(), agent.ID, "audit",
			map[string]any{"error": err.Error()})
	}

	return nil
}

// returnWorkToOpen returns a stalled agent's writ to the open pool
// after exceeding max respawn attempts.
func (w *Sentinel) returnWorkToOpen(agent store.Agent) error {
	// CRASH SAFETY: Update writ to 'open' FIRST, then set agent to 'idle'.
	// Consul's stale-tether recovery queries agents with state = 'working'.
	// If we crash after step 1 but before step 2: the agent is still 'working'
	// — visible to consul — and consul will complete the recovery on its next
	// patrol (updating an already-open writ is idempotent). If instead we set
	// the agent idle first and then crash, the agent becomes 'idle' and
	// invisible to consul, leaving the writ permanently stuck.

	// 1. Attempt to return writ to open. SafelyReopenWrit only updates if the
	// writ is still in tethered or working status, guarding against the case
	// where dispatch.Resolve already flipped it to 'done' before the session
	// died. Done/closed writs must not be silently reverted to open.
	//
	// Crash safety ordering is preserved: we attempt the writ update FIRST. If
	// we crash here the agent is still 'working' and consul can complete recovery.
	// If the writ turns out to be terminal (done/closed) we skip the writ update
	// but still clean up the agent record below.
	if agent.ActiveWrit != "" {
		reopened, err := w.worldStore.SafelyReopenWrit(
			agent.ActiveWrit,
			[]string{store.WritTethered, store.WritWorking},
		)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("failed to return writ %s to open: %w", agent.ActiveWrit, err)
		}
		if !reopened && err == nil && w.logger != nil {
			// Writ is in a terminal or non-matching status (done/closed/open) —
			// do not flip it back. Emit an audit event for observability.
			w.logger.Emit("sentinel_skip_reopen_terminal", w.agentID(), agent.ID, "audit",
				map[string]any{
					"agent": agent.ID,
					"writ":  agent.ActiveWrit,
				})
		}
	}

	// 2. Set agent state → idle, clear active_writ.
	// Done after writ update — now safe since writ is already open.
	if err := w.sphereStore.UpdateAgentState(agent.ID, "idle", ""); err != nil {
		return fmt.Errorf("failed to set agent %s idle: %w", agent.ID, err)
	}

	// 3. Clean up all agent resources (worktree, session metadata, tether, etc.).
	w.cleanupAgentResources(agent.Name, agent.Role)

	// 4. Clear respawn count.
	key := respawnKey{AgentID: agent.ID, WritID: agent.ActiveWrit}
	delete(w.respawnCounts, key)

	// 5. Emit stalled event with recovered: false.
	if w.logger != nil {
		w.logger.Emit(events.EventStalled, w.agentID(), agent.ID, "both",
			map[string]any{
				"agent":     agent.ID,
				"writ": agent.ActiveWrit,
				"recovered": false,
			})
	}

	// 6. Send RECOVERY_NEEDED protocol message to autarch.
	if _, err := w.sphereStore.SendProtocolMessage(
		w.agentID(), config.Autarch,
		store.ProtoRecoveryNeeded,
		store.RecoveryNeededPayload{
			AgentID:    agent.ID,
			WritID: agent.ActiveWrit,
			Reason:     fmt.Sprintf("max respawns (%d) exceeded, work returned to open", w.config.MaxRespawns),
			Attempts:   w.config.MaxRespawns,
		},
	); err != nil && w.logger != nil {
		w.logger.Emit("mail_error", w.agentID(), agent.ID, "audit",
			map[string]any{"error": err.Error()})
	}

	return nil
}

// handleZombie handles an agent with a live session but no tethered work.
// Stops the session and cleans up tether files and worktree so the agent
// can be reaped promptly rather than waiting for the idle reap cycle.
func (w *Sentinel) handleZombie(agent store.Agent) error {
	sessionName := config.SessionName(w.config.World, agent.Name)
	if err := w.sessions.Stop(sessionName, false); err != nil && !errors.Is(err, session.ErrNotFound) {
		return fmt.Errorf("failed to stop zombie session %s: %w", sessionName, err)
	}

	// Clean up tether files so the agent transitions to idle promptly.
	if err := tether.Clear(w.config.World, agent.Name, agent.Role); err != nil {
		if w.logger != nil {
			w.logger.Emit("sentinel_warn", w.agentID(), agent.ID, "audit", map[string]any{
				"action": "zombie_tether_clear",
				"agent":  agent.Name,
				"error":  err.Error(),
			})
		}
	}

	return nil
}

// handleOrphanedWorking handles an agent that is marked "working" with a dead
// session and no tether file on disk. This state occurs when cast crashes before
// writing the tether, consul's stale-tether recovery clears the tether while agent
// DB state is still "working", or a persistent agent's tethers are cleared externally.
//
// For outpost agents: full cleanup and delete. If the active writ is still
// "tethered", return it to "open" for recast. If "done", leave it for the MR pipeline.
// For persistent agents: set idle and clear active_writ.
// Always cleans up the .resolve_in_progress lock file if present.
func (w *Sentinel) handleOrphanedWorking(agent store.Agent) error {
	resolveWasInProgress := dispatch.IsResolveInProgress(w.config.World, agent.Name, agent.Role)

	if agent.Role == "outpost" {
		// Outpost: clean up entirely.
		// If resolve was in progress, the work was being submitted — MR may exist.
		// If not, tether was lost. Either way, agent is stuck and useless.
		//
		// Order matters for crash safety: update writ FIRST so we never
		// delete the agent record while the writ still references it
		// (which would create an unrecoverable "ghost tether").

		// Step 1: If active writ exists and is still assigned to this agent, return it to open.
		if agent.ActiveWrit != "" && w.worldStore != nil {
			// Acquire the dispatch WritLock (non-blocking) to avoid racing with
			// concurrent dispatch operations (Cast/Resolve). If the lock is held, a
			// dispatch operation is actively modifying this writ — skip recovery for
			// now; sentinel will retry on the next patrol cycle.
			writLock, lockErr := flock.AcquireWritLock(agent.ActiveWrit)
			if lockErr != nil {
				slog.Info("sentinel: writ lock held, skipping orphaned writ recovery",
					"agent", agent.ID, "writ", agent.ActiveWrit)
				return nil
			}
			defer writLock.Release()

			// Re-read the writ under the lock to verify it is still assigned to this
			// agent. If the assignee differs, the writ was re-dispatched to another
			// agent while this one was orphaned — clobbering that assignment would
			// break the new agent's resolve.
			item, getErr := w.worldStore.GetWrit(agent.ActiveWrit)
			if getErr == nil {
				if item.Assignee != agent.ID {
					slog.Info("sentinel: orphaned writ reassigned, skipping reopen",
						"agent", agent.ID, "writ", agent.ActiveWrit, "current_assignee", item.Assignee)
				} else {
					// Writ is still ours — safely reopen it. SafelyReopenWrit is a
					// conditional UPDATE that guards against reopening writs already in
					// terminal status (done/closed).
					if _, reopenErr := w.worldStore.SafelyReopenWrit(
						agent.ActiveWrit,
						[]string{store.WritTethered, store.WritWorking},
					); reopenErr != nil {
						// Agent record still exists — safe to return error and retry next patrol.
						return fmt.Errorf("failed to return orphaned writ %s to open: %w", agent.ActiveWrit, reopenErr)
					}
				}
			}
			// If getErr != nil (writ not found or DB error), skip the reopen and proceed
			// with agent cleanup — the writ state cannot be determined safely.
		}

		// Step 2: Clean up agent resources (tether files, worktree).
		w.cleanupAgentResources(agent.Name, agent.Role)

		// Step 3: Delete agent record last — writ is already freed.
		if err := w.sphereStore.DeleteAgent(agent.ID); err != nil {
			return fmt.Errorf("failed to delete orphaned agent %s: %w", agent.ID, err)
		}
	} else {
		// Persistent agent: set idle, clear active_writ.
		if err := w.sphereStore.UpdateAgentState(agent.ID, "idle", ""); err != nil {
			return fmt.Errorf("failed to set orphaned agent %s idle: %w", agent.ID, err)
		}
	}

	// Clean up resolve lock(s) if present (shared or per-writ).
	if resolveWasInProgress {
		dispatch.ClearResolveLocksForAgent(w.config.World, agent.Name, agent.Role)
	}

	// Emit event for observability.
	if w.logger != nil {
		w.logger.Emit(events.EventStalled, w.agentID(), agent.ID, "both", map[string]any{
			"agent":               agent.ID,
			"writ":                agent.ActiveWrit,
			"recovered":           true,
			"reason":              "orphaned_working_no_tether",
			"resolve_in_progress": resolveWasInProgress,
		})
	}

	return nil
}

// reapIdleAgent deletes an idle agent record that has exceeded the reap timeout.
// Cleans up any lingering worktree, session metadata, tether, and workflow files.
func (w *Sentinel) reapIdleAgent(agent store.Agent) error {
	// 1. Clean up all agent resources on disk.
	w.cleanupAgentResources(agent.Name, agent.Role)

	// 2. Delete the agent record to free the name pool slot.
	if err := w.sphereStore.DeleteAgent(agent.ID); err != nil {
		return fmt.Errorf("failed to delete idle agent %s: %w", agent.ID, err)
	}

	if w.logger != nil {
		w.logger.Emit(events.EventReap, w.agentID(), agent.ID, "both",
			map[string]any{
				"agent":      agent.ID,
				"idle_since": agent.UpdatedAt.Format(time.RFC3339),
			})
	}

	return nil
}

// reapClosedWritAgent reaps an outpost agent whose tethered writ has been closed
// (cancelled, superseded, etc.). Stops the session, clears the tether, and deletes
// the agent record — same reap path as idle agent cleanup.
//
// Crash-safety ordering: cleanup and deletion happen while the agent is still
// "working". If sentinel crashes mid-cleanup the agent remains visible to
// consul/sentinel recovery. The idle transition is omitted entirely because
// the agent record is deleted immediately after cleanup.
func (w *Sentinel) reapClosedWritAgent(agent store.Agent, sessionName, closeReason string) error {
	if w.logger != nil {
		w.logger.Emit(events.EventReap, w.agentID(), agent.ID, "both",
			map[string]any{
				"agent":        agent.ID,
				"writ":         agent.ActiveWrit,
				"close_reason": closeReason,
				"reason":       "writ closed",
			})
	}

	// 1. Clean up all agent resources (session, worktree, tether, etc.)
	// while the agent is still "working" — crash here leaves the agent
	// visible to consul/sentinel recovery.
	w.cleanupAgentResources(agent.Name, agent.Role)

	// 2. Delete the agent record to free the name pool slot.
	// No idle transition needed — we are deleting the record outright.
	if err := w.sphereStore.DeleteAgent(agent.ID); err != nil {
		return fmt.Errorf("failed to delete agent %s: %w", agent.ID, err)
	}

	return nil
}

// checkHandoffFrequency checks if any working agent has handed off too frequently.
// 3+ handoffs in 30 minutes signals a possible handoff loop — burning tokens
// without making meaningful progress.
func (w *Sentinel) checkHandoffFrequency(agents []store.Agent) int {
	if w.eventReader == nil {
		return 0
	}

	window := 30 * time.Minute
	threshold := 3
	var escalated int

	for _, agent := range agents {
		if agent.State != "working" {
			continue
		}

		handoffs, err := w.eventReader.Read(events.ReadOpts{
			Type:  events.EventHandoff,
			Actor: agent.Name,
			Since: w.now().Add(-window),
		})
		if err != nil {
			continue
		}

		if len(handoffs) >= threshold {
			escalated++

			if w.logger != nil {
				w.logger.Emit("handoff_loop", w.agentID(), agent.ID, "both",
					map[string]any{
						"agent":    agent.ID,
						"handoffs": len(handoffs),
						"window":   window.String(),
					})
			}

			if _, err := w.sphereStore.SendProtocolMessage(
				w.agentID(), config.Autarch,
				store.ProtoRecoveryNeeded,
				store.RecoveryNeededPayload{
					AgentID:    agent.ID,
					WritID: agent.ActiveWrit,
					Reason:     fmt.Sprintf("handoff loop: %d handoffs in %s", len(handoffs), window),
				},
			); err != nil && w.logger != nil {
				w.logger.Emit("mail_error", w.agentID(), agent.ID, "audit",
					map[string]any{"error": err.Error()})
			}
		}
	}

	return escalated
}

// cleanupAgentResources removes all disk resources for an agent: worktree,
// session metadata, tether file, handoff file, and workflow directory.
// Best-effort: logs errors but does not fail.
//
// The role parameter selects the role-scoped paths used by tether.Clear,
// handoff.Remove, and runtime.CleanupConfigDir. Passing the wrong role
// silently corrupts state for the other role's tethers/handoffs, so
// callers must pass the agent's actual role rather than hardwiring "outpost".
func (w *Sentinel) cleanupAgentResources(agentName, role string) {
	sessionName := config.SessionName(w.config.World, agentName)

	// Stop session if still alive.
	if w.sessions.Exists(sessionName) {
		if err := w.sessions.Stop(sessionName, true); err != nil {
			if w.logger != nil {
				w.logger.Emit("sentinel_warn", w.agentID(), w.agentID(), "audit", map[string]any{
					"action":  "cleanup_stop_session",
					"session": sessionName,
					"error":   err.Error(),
				})
			} else {
				slog.Warn("sentinel: failed to stop session", "session", sessionName, "error", err)
			}
		}
	}

	// Remove nudge queue directory for the dead session. The agent is not
	// coming back, so there is nothing to requeue — RemoveQueueDir is a
	// wholesale reap of the queue dir. Best-effort: the directory may not
	// exist for agents that never received nudges.
	if err := nudge.RemoveQueueDir(sessionName); err != nil {
		softfail.Log(nil, "sentinel.nudge_queue_remove", err)
	}

	// Remove worktree via git.
	worktreeDir := dispatch.WorktreePath(w.config.World, agentName)
	if _, err := os.Stat(worktreeDir); err == nil {
		repoPath := config.RepoPath(w.config.World)
		rmCmd := exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", worktreeDir)
		if out, err := rmCmd.CombinedOutput(); err != nil {
			if w.logger != nil {
				w.logger.Emit("sentinel_warn", w.agentID(), w.agentID(), "audit", map[string]any{
					"action":  "cleanup_worktree_remove",
					"output":  strings.TrimSpace(string(out)),
					"error":   err.Error(),
				})
			} else {
				slog.Warn("sentinel: worktree remove failed", "output", strings.TrimSpace(string(out)), "error", err)
			}
			// Fallback: remove directory directly.
			os.RemoveAll(worktreeDir)
		}
		pruneCmd := exec.Command("git", "-C", repoPath, "worktree", "prune")
		pruneCmd.Run() // best-effort
	}

	// Remove session metadata files.
	metaPath := filepath.Join(config.RuntimeDir(), "sessions", sessionName+".json")
	os.Remove(metaPath) // best-effort
	hashPath := filepath.Join(config.RuntimeDir(), "sessions", sessionName+".last-capture-hash")
	os.Remove(hashPath) // best-effort

	// Clear tether file. Tether storage is role-scoped — passing the wrong
	// role would leave the real tether in place and silently corrupt state.
	if err := tether.Clear(w.config.World, agentName, role); err != nil {
		slog.Warn("sentinel: failed to clear tether", "agent", agentName, "role", role, "error", err)
	}

	// Remove handoff file (also role-scoped).
	if err := handoff.Remove(w.config.World, agentName, role); err != nil {
		slog.Warn("sentinel: failed to remove handoff", "agent", agentName, "role", role, "error", err)
	}

	// Remove runtime config dirs for the terminated agent. We don't know which
	// runtime owned the agent (the record may already be gone), so we invoke
	// every known runtime — CleanupConfigDir is idempotent.
	worldDir := config.WorldDir(w.config.World)
	for name, r := range loader.All() {
		if err := runtime.CleanupConfigDir(r.Descriptor(), worldDir, role, agentName); err != nil {
			slog.Warn("sentinel: failed to clean up runtime config dir",
				"agent", agentName, "role", role, "runtime", name, "error", err)
		}
	}

	// Remove the outpost directory itself if empty.
	outpostDir := filepath.Join(config.Home(), w.config.World, "outposts", agentName)
	os.Remove(outpostDir) // only succeeds if empty, which is fine
}

// cleanupOrphanedResources scans for resources on disk that have no matching
// agent record and cleans them up. Returns the number of resources cleaned.
func (w *Sentinel) cleanupOrphanedResources(agents []store.Agent) int {
	agentNames := make(map[string]bool, len(agents))
	for _, a := range agents {
		agentNames[a.Name] = true
	}

	// Build set of working agents for tether checks.
	workingAgents := make(map[string]bool)
	for _, a := range agents {
		if a.State == "working" {
			workingAgents[a.Name] = true
		}
	}

	var cleaned int
	cleaned += w.cleanupOrphanedOutpostDirs(agentNames)
	cleaned += w.cleanupOrphanedEnvoyDirs(agentNames)
	cleaned += w.cleanupOrphanedSessionMeta(agentNames)
	cleaned += w.cleanupOrphanedTethers(agentNames, workingAgents)
	return cleaned
}

// cleanupOrphanedOutpostDirs removes outpost directories that have no matching agent record.
func (w *Sentinel) cleanupOrphanedOutpostDirs(agentNames map[string]bool) int {
	outpostsDir := filepath.Join(config.Home(), w.config.World, "outposts")
	entries, err := os.ReadDir(outpostsDir)
	if err != nil {
		return 0 // directory may not exist
	}

	var cleaned int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if agentNames[name] {
			continue // agent exists, not orphaned
		}

		// Orphaned outpost directory — clean it up regardless of contents.
		// The directory may contain a worktree, stale .resume_state.json,
		// empty .tether/ dirs, or other remnants. All are safe to remove
		// since there is no matching agent record in sphere.db. The orphan
		// scanner only walks the outposts/ tree, so the role is "outpost".
		w.cleanupAgentResources(name, "outpost")

		// Force-remove any remaining files. cleanupAgentResources uses
		// os.Remove (empty-only) for the directory, but orphan cleanup
		// needs full removal of stale remnants.
		outpostDir := filepath.Join(outpostsDir, name)
		os.RemoveAll(outpostDir) // best-effort

		cleaned++

		if w.logger != nil {
			w.logger.Emit(events.EventOrphanCleanup, w.agentID(), w.agentID(), "audit",
				map[string]any{
					"type":  "outpost-dir",
					"agent": name,
					"world": w.config.World,
				})
		}
	}
	return cleaned
}

// cleanupOrphanedEnvoyDirs removes envoy directories that have no matching
// agent record in sphere.db. Mirrors cleanupOrphanedOutpostDirs but is scoped
// to $SOL_HOME/<world>/envoys/.
//
// Without this sweep, an envoy.Delete that fails midway (e.g. DB lock during
// DeleteAgent after the worktree was removed) leaves the envoy directory and
// any runtime config state on disk forever — sentinel was previously hard-
// scoped to outposts/ and could not see envoy orphans.
//
// Cleanup steps mirror what envoy.Delete does: stop any live session, remove
// the git worktree (so .git/worktrees doesn't accumulate stale entries),
// clear tether and handoff files (role=envoy), invoke every registered
// runtime's CleanupConfigDir to reap .claude-config/.codex-home leaks, then
// finally os.RemoveAll on the envoy directory itself.
func (w *Sentinel) cleanupOrphanedEnvoyDirs(agentNames map[string]bool) int {
	envoysDir := filepath.Join(config.Home(), w.config.World, "envoys")
	entries, err := os.ReadDir(envoysDir)
	if err != nil {
		return 0 // directory may not exist
	}

	var cleaned int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if agentNames[name] {
			continue // agent exists, not orphaned
		}

		// Orphaned envoy directory — clean up associated state.
		envoyDir := filepath.Join(envoysDir, name)

		// Stop session if still alive.
		sessionName := config.SessionName(w.config.World, name)
		if w.sessions.Exists(sessionName) {
			if err := w.sessions.Stop(sessionName, true); err != nil && w.logger != nil {
				w.logger.Emit("sentinel_warn", w.agentID(), w.agentID(), "audit", map[string]any{
					"action":  "orphan_envoy_stop_session",
					"session": sessionName,
					"error":   err.Error(),
				})
			}
		}

		// Remove envoy worktree via git so the source repo's worktree
		// administrative entry (.git/worktrees/<name>) is dropped too.
		// Path matches envoy.WorktreePath; hardcoded here to avoid an
		// envoy package import (sentinel sits below envoy in the import
		// graph everywhere else).
		worktreeDir := filepath.Join(envoyDir, "worktree")
		if _, err := os.Stat(worktreeDir); err == nil {
			repoPath := config.RepoPath(w.config.World)
			rmCmd := exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", worktreeDir)
			if out, err := rmCmd.CombinedOutput(); err != nil && w.logger != nil {
				w.logger.Emit("sentinel_warn", w.agentID(), w.agentID(), "audit", map[string]any{
					"action": "orphan_envoy_worktree_remove",
					"output": strings.TrimSpace(string(out)),
					"error":  err.Error(),
				})
			}
			pruneCmd := exec.Command("git", "-C", repoPath, "worktree", "prune")
			pruneCmd.Run() // best-effort
		}

		// Remove session metadata.
		metaPath := filepath.Join(config.RuntimeDir(), "sessions", sessionName+".json")
		os.Remove(metaPath) // best-effort
		hashPath := filepath.Join(config.RuntimeDir(), "sessions", sessionName+".last-capture-hash")
		os.Remove(hashPath) // best-effort

		// Clear tether file (role-scoped — must pass "envoy").
		if err := tether.Clear(w.config.World, name, "envoy"); err != nil {
			slog.Warn("sentinel: failed to clear orphan envoy tether", "agent", name, "error", err)
		}

		// Remove handoff file (role-scoped).
		if err := handoff.Remove(w.config.World, name, "envoy"); err != nil {
			slog.Warn("sentinel: failed to remove orphan envoy handoff", "agent", name, "error", err)
		}

		// Remove runtime config dirs. Mirrors cleanupAgentResources:
		// invoke every known runtime so we catch the runtime that
		// EnsureConfigDir was called against, even if the world config has
		// since been swapped. CleanupConfigDir is idempotent.
		worldDir := config.WorldDir(w.config.World)
		for runtimeName, r := range loader.All() {
			if err := runtime.CleanupConfigDir(r.Descriptor(), worldDir, "envoy", name); err != nil {
				slog.Warn("sentinel: failed to clean up orphan envoy runtime config dir",
					"agent", name, "runtime", runtimeName, "error", err)
			}
		}

		// Finally, remove the envoy directory entirely.
		os.RemoveAll(envoyDir) // best-effort

		cleaned++

		if w.logger != nil {
			w.logger.Emit(events.EventOrphanCleanup, w.agentID(), w.agentID(), "audit",
				map[string]any{
					"type":  "envoy-dir",
					"agent": name,
					"world": w.config.World,
				})
		}
	}
	return cleaned
}

// cleanupOrphanedSessionMeta removes session metadata files for dead outpost
// sessions that have no matching agent record.
func (w *Sentinel) cleanupOrphanedSessionMeta(agentNames map[string]bool) int {
	sessDir := filepath.Join(config.RuntimeDir(), "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		return 0
	}

	prefix := "sol-" + w.config.World + "-"
	var cleaned int
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		fileName := entry.Name()
		sessName := strings.TrimSuffix(fileName, ".json")
		if !strings.HasPrefix(sessName, prefix) {
			continue // not for this world
		}

		agentName := strings.TrimPrefix(sessName, prefix)
		if agentNames[agentName] {
			continue // agent exists, not orphaned
		}

		// Skip if session is still alive in tmux.
		if w.sessions.Exists(sessName) {
			continue
		}

		// Orphaned session metadata — remove it.
		os.Remove(filepath.Join(sessDir, fileName))
		hashFile := sessName + ".last-capture-hash"
		os.Remove(filepath.Join(sessDir, hashFile))
		cleaned++

		if w.logger != nil {
			w.logger.Emit(events.EventOrphanCleanup, w.agentID(), w.agentID(), "audit",
				map[string]any{
					"type":    "session_metadata",
					"session": sessName,
					"world":   w.config.World,
				})
		}
	}
	return cleaned
}

// cleanupOrphanedTethers scans tether directories for agents that are not working
// and clears all tether files within.
//
// IMPORTANT: Before clearing, re-reads agent state from DB (not the stale snapshot)
// to avoid a race with Cast(), which writes the tether before updating agent state.
func (w *Sentinel) cleanupOrphanedTethers(agentNames, workingAgents map[string]bool) int {
	outpostsDir := filepath.Join(config.Home(), w.config.World, "outposts")
	entries, err := os.ReadDir(outpostsDir)
	if err != nil {
		return 0
	}

	var cleaned int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()

		// If agent exists in DB at all (any state), skip it.
		// Only clear tethers for agents with NO record in the sphere DB
		// (truly orphaned — the agent was deleted but its tether directory
		// wasn't cleaned up). Idle agents with tethers are handled by
		// consul's stale-tether recovery with proper context.
		if agentNames[name] {
			continue
		}

		// Check if the tether directory has any files.
		if !tether.IsTethered(w.config.World, name, "outpost") {
			continue
		}

		// Tether directory non-empty for agent with no DB record — truly orphaned.
		if err := tether.Clear(w.config.World, name, "outpost"); err != nil && w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit", map[string]any{
				"error": fmt.Sprintf("failed to clear orphaned tether (best-effort): agent=%s: %v", name, err),
			})
		}
		cleaned++

		if w.logger != nil {
			w.logger.Emit(events.EventOrphanCleanup, w.agentID(), w.agentID(), "audit",
				map[string]any{
					"type":  "tether",
					"agent": name,
					"world": w.config.World,
				})
		}
	}
	return cleaned
}

// pruneOrphanedBranches deletes local branches whose remote tracking branch
// has been deleted (i.e., marked as "gone" by git). Active worktree branches
// are protected. Returns the number of branches pruned.
func (w *Sentinel) pruneOrphanedBranches() int {
	repoPath := w.config.SourceRepo
	if repoPath == "" {
		return 0
	}

	// Prune remote tracking refs for deleted remote branches.
	exec.Command("git", "-C", repoPath, "fetch", "--prune").Run()

	// List local branches with their upstream tracking status.
	// Format: %(refname:short) %(upstream:track)
	// Branches whose remote is gone show "[gone]" in the track field.
	out, err := exec.Command("git", "-C", repoPath, "for-each-ref",
		"--format=%(refname:short) %(upstream:track)",
		"refs/heads/").CombinedOutput()
	if err != nil {
		return 0
	}

	// Resolve the world's primary branch from config (default "main").
	worldBranch := "main"
	if worldCfg, cfgErr := config.LoadWorldConfig(w.config.World); cfgErr == nil {
		worldBranch = worldCfg.World.Branch
	}

	// Build set of branches used by active worktrees.
	worktreeBranches := w.listWorktreeBranches(repoPath)

	var pruned int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		if !strings.Contains(line, "[gone]") {
			continue
		}

		branch := strings.Fields(line)[0]

		// Never delete the world's primary branch.
		if branch == worldBranch {
			continue
		}

		// Protect branches that have an active worktree.
		if worktreeBranches[branch] {
			continue
		}

		// Delete the orphaned local branch.
		if err := exec.Command("git", "-C", repoPath, "branch", "-D", branch).Run(); err != nil {
			continue
		}
		pruned++

		if w.logger != nil {
			w.logger.Emit("sentinel_action", w.agentID(), w.agentID(), "audit",
				map[string]any{
					"action": "pruned_branch",
					"branch": branch,
					"world":  w.config.World,
				})
		}
	}
	return pruned
}

// listWorktreeBranches returns a set of branch names currently checked out
// in git worktrees.
func (w *Sentinel) listWorktreeBranches(repoPath string) map[string]bool {
	out, err := exec.Command("git", "-C", repoPath, "worktree", "list",
		"--porcelain").CombinedOutput()
	if err != nil {
		return nil
	}

	branches := make(map[string]bool)
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "branch ") {
			ref := strings.TrimPrefix(line, "branch ")
			// Convert refs/heads/foo to foo.
			branch := strings.TrimPrefix(ref, "refs/heads/")
			branches[branch] = true
		}
	}
	return branches
}

