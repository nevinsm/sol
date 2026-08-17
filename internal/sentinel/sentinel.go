package sentinel

import (
	"context"
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
	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/logutil"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/softfail"
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

// captureErrorSentinel is stored in lastCaptures when a session capture fails.
// It is not a valid sha256 hex string, so any subsequent successful capture
// will always differ from it — ensuring we establish a fresh baseline after
// a failure window rather than comparing against a potentially stale pre-failure hash.
const captureErrorSentinel = "capture_error"

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

