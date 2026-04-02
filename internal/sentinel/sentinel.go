package sentinel

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/account"
	"github.com/nevinsm/sol/internal/adapter"
	"github.com/nevinsm/sol/internal/budget"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/logutil"
	"github.com/nevinsm/sol/internal/quota"
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
// The AssessCommand is resolved from the world's runtime adapter when possible,
// falling back to "claude -p" if the adapter is not found.
func DefaultConfig(world, sourceRepo, solHome string) Config {
	assessCmd := resolveCalloutCommand(world, "sentinel")
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

// resolveCalloutCommand resolves the default callout command from the world's
// runtime adapter. Falls back to "claude -p" if the adapter is not found.
func resolveCalloutCommand(world, role string) string {
	const fallback = "claude -p"
	worldCfg, err := config.LoadWorldConfig(world)
	if err != nil {
		return fallback
	}
	runtime := worldCfg.ResolveRuntime(role)
	a, ok := adapter.Get(runtime)
	if !ok {
		return fallback
	}
	return a.CalloutCommand()
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
	SetWritMetadata(id string, metadata map[string]any) error
	ListWrits(filters store.ListFilters) ([]store.Writ, error)
	ListMergeRequests(phase string) ([]store.MergeRequest, error)
	ListMergeRequestsByWrit(writID string, phase string) ([]store.MergeRequest, error)
	ListBlockedMergeRequests() ([]store.MergeRequest, error)
	ReleaseStaleClaims(ttl time.Duration, maxAttempts int) (int, error)
	DailySpendByAccount(account string) (float64, error)
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

// recastCountFromMetadata reads the persistent recast count from writ metadata.
func recastCountFromMetadata(item *store.Writ) int {
	if item.Metadata == nil {
		return 0
	}
	v, ok := item.Metadata["recast-count"]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

// lastRecastTimeFromMetadata reads the persistent last recast timestamp from writ metadata.
func lastRecastTimeFromMetadata(item *store.Writ) time.Time {
	if item.Metadata == nil {
		return time.Time{}
	}
	v, ok := item.Metadata["recast-last"]
	if !ok {
		return time.Time{}
	}
	s, ok := v.(string)
	if !ok {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
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

	// Write PID file for process management.
	if err := WritePID(w.config.World, os.Getpid()); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer ClearPID(w.config.World)

	if err := w.sphereStore.UpdateAgentState(w.agentID(), "working", ""); err != nil {
		return fmt.Errorf("failed to set sentinel working: %w", err)
	}

	// Reconcile respawn counts from event history before first patrol.
	// This prevents infinite respawn loops after sentinel crash-restart.
	w.reconcileRespawnCounts()

	// Write initial heartbeat.
	w.writeHeartbeat("running", 0, 0, 0, 0, "")

	// Patrol immediately.
	w.patrol(ctx)

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
			w.patrol(ctx)
		}
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

	// Recast failed MRs before agent checks (so newly cast agents appear healthy).
	recastCount := w.recastFailedMRs()

	// Dispatch orphaned conflict-resolution writs blocking MRs.
	resolutionDispatched := w.dispatchOrphanedResolutions()

	// Release stale MR claims (forge crash recovery).
	releasedCount := w.releaseStaleClaims()

	// Recover writs stuck in "done" with no active MR (resolve crash recovery).
	doneRecovered := w.recoverOrphanedDoneWrits()

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
			time.Since(agent.UpdatedAt) > w.config.IdleReapTimeout:
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

	// Scan sessions for rate limits and rotate/pause as needed.
	quotaScanned, quotaRotated, quotaPaused := w.quotaPatrol(agents)

	// Check if any quota-paused agents can be restarted.
	quotaRestarted := w.checkQuotaPaused()

	// Prune local branches whose remote tracking branch is gone.
	branchesPruned := w.pruneOrphanedBranches()

	// Clean up orphaned resources (worktrees, session metadata, tethers).
	orphansCleaned := w.cleanupOrphanedResources(activeAgents)

	// Prune stale entries for agents no longer in the active set.
	activeIDs := make(map[string]bool, len(activeAgents))
	for _, a := range activeAgents {
		activeIDs[a.ID] = true
	}
	w.pruneCaptures(activeIDs)
	w.pruneRespawnCounts(activeIDs)

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
				"branches_pruned": branchesPruned,
				"orphans_cleaned": orphansCleaned,
				"handoff_loops":   handoffLoops,
				"quota_scanned":   quotaScanned,
				"quota_rotated":   quotaRotated,
				"quota_paused":    quotaPaused,
				"quota_restarted": quotaRestarted,
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

// checkProgress checks whether a working agent with a live session is making progress.
// If the tmux output hasn't changed since the last patrol, triggers AI assessment.
func (w *Sentinel) checkProgress(ctx context.Context, agent store.Agent, sessionName string) error {
	output, err := w.sessions.Capture(sessionName, w.config.CaptureLines)
	if err != nil {
		if w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit",
				map[string]any{"error": err.Error(), "action": "capture_failed", "session": sessionName})
		}
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

	// Check account budget before spawning AI callout.
	worldCfg, cfgErr := config.LoadWorldConfig(w.config.World)
	if cfgErr == nil && len(worldCfg.Budget.Accounts) > 0 {
		assessAccount := account.ResolveAccount("", worldCfg.World.DefaultAccount)
		if assessAccount != "" {
			if err := budget.CheckAccountBudget(w.worldStore, w.sphereStore, assessAccount, worldCfg.Budget); err != nil {
				if w.logger != nil {
					w.logger.Emit("assess_budget_skip", w.agentID(), agent.ID, "audit",
						map[string]any{"account": assessAccount, "reason": err.Error()})
				}
				return nil // skip assessment, don't block patrol
			}
		}
	}

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
	prompt := buildAssessmentPrompt(agent, capturedOutput, w.config.CaptureLines)

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

func buildAssessmentPrompt(agent store.Agent, capturedOutput string, captureLines int) string {
	return fmt.Sprintf(`You are a sentinel agent monitoring AI coding agents in a multi-agent
orchestration system. An agent's tmux session output has not changed
since the last patrol cycle (3 minutes ago). Analyze the session output
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
(e.g., repeated failures, auth issues, infrastructure problems).`, agent.Name, agent.ID, agent.ActiveWrit, captureLines, capturedOutput)
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
		// Create formal escalation for durable tracking.
		escDesc := fmt.Sprintf("Agent %s needs recovery: %s", agent.Name, result.Reason)
		var sourceRef string
		if agent.ActiveWrit != "" {
			sourceRef = "writ:" + agent.ActiveWrit
		}
		if _, err := w.sphereStore.CreateEscalation("high", w.config.World+"/sentinel", escDesc, sourceRef); err != nil && w.logger != nil {
			w.logger.Emit("escalation_error", w.agentID(), agent.ID, "audit",
				map[string]any{"error": err.Error()})
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

	_, err := startup.Respawn(agent.Role, w.config.World, agent.Name, startup.LaunchOpts{
		Sessions: w.sessions,
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

	// 1. Update writ: status → open, clear assignee.
	// Do this first so a crash here leaves the agent 'working' — recoverable by consul.
	if agent.ActiveWrit != "" {
		if err := w.worldStore.UpdateWrit(agent.ActiveWrit, store.WritUpdates{
			Status:   "open",
			Assignee: "-", // "-" clears assignee
		}); err != nil {
			return fmt.Errorf("failed to return writ %s to open: %w", agent.ActiveWrit, err)
		}
	}

	// 2. Set agent state → idle, clear active_writ.
	// Done after writ update — now safe since writ is already open.
	if err := w.sphereStore.UpdateAgentState(agent.ID, "idle", ""); err != nil {
		return fmt.Errorf("failed to set agent %s idle: %w", agent.ID, err)
	}

	// 3. Clean up all agent resources (worktree, session metadata, tether, etc.).
	w.cleanupAgentResources(agent.Name)

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
func (w *Sentinel) handleZombie(agent store.Agent) error {
	sessionName := config.SessionName(w.config.World, agent.Name)
	if err := w.sessions.Stop(sessionName, false); err != nil {
		return fmt.Errorf("failed to stop zombie session %s: %w", sessionName, err)
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

		// Step 1: If active writ exists and is still "tethered", return it to open.
		if agent.ActiveWrit != "" && w.worldStore != nil {
			item, err := w.worldStore.GetWrit(agent.ActiveWrit)
			if err == nil && item.Status == "tethered" {
				if updateErr := w.worldStore.UpdateWrit(agent.ActiveWrit, store.WritUpdates{
					Status:   "open",
					Assignee: "-",
				}); updateErr != nil {
					// Agent record still exists — safe to return error and retry next patrol.
					return fmt.Errorf("failed to return orphaned writ %s to open: %w", agent.ActiveWrit, updateErr)
				}
			}
			// If writ is "done", leave it — MR pipeline will handle it.
		}

		// Step 2: Clean up agent resources (tether files, worktree).
		w.cleanupAgentResources(agent.Name)

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
	w.cleanupAgentResources(agent.Name)

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

	// 1. Set agent idle and clear tether before resource cleanup.
	if err := w.sphereStore.UpdateAgentState(agent.ID, "idle", ""); err != nil {
		return fmt.Errorf("failed to set agent %s idle: %w", agent.ID, err)
	}

	// 2. Clean up all agent resources (session, worktree, tether, etc.).
	w.cleanupAgentResources(agent.Name)

	// 3. Delete the agent record to free the name pool slot.
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

// quotaPatrol scans all live agent sessions for rate limits. If any are
// detected, rotates the entire world to a fresh account (all agents get new
// credentials). If no accounts are available, pauses autonomous agents
// (outpost, forge, envoy).
// Returns (scanned, rotated, paused).
func (w *Sentinel) quotaPatrol(agents []store.Agent) (int, int, int) {
	// Build list of live agents with sessions (skip sentinel itself).
	type liveAgent struct {
		agent   store.Agent
		session string
		account string
	}
	var live []liveAgent
	for _, a := range agents {
		if a.Role == "sentinel" {
			continue
		}
		sessionName := config.SessionName(w.config.World, a.Name)
		if !w.sessions.Exists(sessionName) {
			continue
		}
		acct := quota.ResolveCurrentAccount(w.config.World, a.Name, a.Role)
		live = append(live, liveAgent{agent: a, session: sessionName, account: acct})
	}

	if len(live) == 0 {
		return 0, 0, 0
	}

	// Acquire quota lock for the entire scan+rotate.
	lock, state, err := quota.AcquireLock()
	if err != nil {
		return 0, 0, 0
	}
	defer lock.Release()

	state.ExpireLimits()

	// Seed quota state with all registered accounts so fresh accounts
	// (never used, not yet in state) are discoverable for rotation.
	if reg, err := account.LoadRegistry(); err == nil {
		for handle := range reg.Accounts {
			if state.Accounts[handle] == nil {
				state.MarkAvailable(handle)
			}
		}
	}

	// Scan each live session for rate limit patterns.
	var scanned int
	limitedAccounts := make(map[string]bool)

	for _, la := range live {
		output, err := w.sessions.Capture(la.session, 20)
		if err != nil {
			continue
		}
		scanned++

		limited, resetsAt := quota.DetectRateLimit(output)
		if limited && la.account != "" {
			state.MarkLimited(la.account, resetsAt)
			limitedAccounts[la.account] = true
		}
	}

	if w.logger != nil && scanned > 0 {
		w.logger.Emit(events.EventQuotaScan, w.agentID(), w.agentID(), "audit",
			map[string]any{
				"world":   w.config.World,
				"scanned": scanned,
				"limited": len(limitedAccounts),
			})
	}

	if len(limitedAccounts) == 0 {
		_ = quota.Save(state)
		return scanned, 0, 0
	}

	// Rate limit detected — decide: rotate or pause.
	available := state.AvailableAccountsLRU()

	if len(available) == 0 {
		// No accounts available — pause autonomous agents on limited accounts.
		var paused int
		for _, la := range live {
				if !limitedAccounts[la.account] {
				continue
			}

			if err := w.sessions.Stop(la.session, false); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), la.agent.ID, "audit",
						map[string]any{"action": "quota_pause", "error": err.Error()})
				}
				continue
			}

			state.PausedSessions[la.agent.ID] = quota.PausedSession{
				PausedAt:        w.now().UTC(),
				PreviousAccount: la.account,
				Writ:        la.agent.ActiveWrit,
				World:           w.config.World,
				AgentName:       la.agent.Name,
				Role:            la.agent.Role,
			}
			paused++

			if w.logger != nil {
				w.logger.Emit(events.EventQuotaPause, w.agentID(), la.agent.ID, "both",
					map[string]any{
						"agent":     la.agent.ID,
						"world":     w.config.World,
						"writ": la.agent.ActiveWrit,
						"reason":    "no available accounts for rotation",
					})
			}
		}

		_ = quota.Save(state)
		return scanned, 0, paused
	}

	// Available accounts exist — rotate the ENTIRE world to one account.
	toAccount := available[0]
	var rotated int

	for _, la := range live {
		if la.account == toAccount {
			continue // already on the target account
		}

		// Use startup.Resume for registered roles to get system prompt
		// flags, persona, hooks, and workflow re-instantiation.
		cfg := startup.ConfigFor(la.agent.Role)
		if cfg != nil {
			cycleOp := func(name, workdir, cmd string, env map[string]string, role, world string) error {
				if err := w.sessions.Cycle(name, workdir, cmd, env, role, world); err != nil {
					fmt.Fprintf(os.Stderr, "sentinel: quota cycle failed, falling back to stop+start: %v\n", err)
					if stopErr := w.sessions.Stop(name, true); stopErr != nil {
						fmt.Fprintf(os.Stderr, "sentinel: stop also failed: %v\n", stopErr)
					}
					return w.sessions.Start(name, workdir, cmd, env, role, world)
				}
				return nil
			}

			resumeState := startup.ResumeState{
				Reason:          "quota_rotate",
				ClaimedResource: la.agent.ActiveWrit,
			}

			launchOpts := startup.LaunchOpts{
				Account:   toAccount,
				SessionOp: cycleOp,
				Sessions:  w.sessions,
			}

			if _, err := startup.Resume(*cfg, w.config.World, la.agent.Name, resumeState, launchOpts); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), la.agent.ID, "audit",
						map[string]any{"action": "quota_rotate", "error": err.Error()})
				}
				continue
			}
		} else {
			// No startup config registered — skip this agent.
			if w.logger != nil {
				w.logger.Emit("sentinel_error", w.agentID(), la.agent.ID, "audit",
					map[string]any{"action": "quota_rotate", "error": fmt.Sprintf("no startup config registered for role %q", la.agent.Role)})
			}
			continue
		}

		rotated++

		if w.logger != nil {
			w.logger.Emit(events.EventQuotaRotate, w.agentID(), la.agent.ID, "both",
				map[string]any{
					"agent":        la.agent.ID,
					"from_account": la.account,
					"to_account":   toAccount,
					"world":        w.config.World,
				})
		}
	}

	state.MarkLastUsed(toAccount)
	_ = quota.Save(state)

	return scanned, rotated, 0
}

// agentWorkdir returns the working directory for an agent based on its role.
func agentWorkdir(world string, agent store.Agent) string {
	base := config.AgentDir(world, agent.Name, agent.Role)
	switch agent.Role {
	default:
		return filepath.Join(base, "worktree")
	}
}

// checkQuotaPaused checks if any quota-paused agents for this world can be
// restarted. Loads quota state, expires stale limits, and restarts paused
// sessions when available accounts exist. Returns the number restarted.
func (w *Sentinel) checkQuotaPaused() int {
	lock, state, err := quota.AcquireLock()
	if err != nil {
		return 0
	}
	defer lock.Release()

	// Expire any limits whose resets_at has passed.
	expired := state.ExpireLimits()

	// Check if there are paused sessions for this world.
	hasPaused := false
	for _, paused := range state.PausedSessions {
		if paused.World == w.config.World {
			hasPaused = true
			break
		}
	}

	if !hasPaused {
		if len(expired) > 0 {
			_ = quota.Save(state)
		}
		return 0
	}

	available := state.AvailableAccountsLRU()
	if len(available) == 0 {
		if len(expired) > 0 {
			_ = quota.Save(state)
		}
		return 0
	}

	availIdx := 0
	var restarted int

	for agentID, paused := range state.PausedSessions {
		if paused.World != w.config.World {
			continue
		}
		if availIdx >= len(available) {
			break
		}

		toAccount := available[availIdx]

		// Use startup.Resume for registered roles to get system prompt
		// flags, persona, hooks, and workflow re-instantiation.
		cfg := startup.ConfigFor(paused.Role)
		if cfg != nil {
			resumeState := startup.ResumeState{
				Reason:          "quota_rotate",
				ClaimedResource: paused.Writ,
			}

			launchOpts := startup.LaunchOpts{
				Account:  toAccount,
				Sessions: w.sessions,
			}

			if _, err := startup.Resume(*cfg, w.config.World, paused.AgentName, resumeState, launchOpts); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agentID, "audit",
						map[string]any{"action": "quota_restart", "error": err.Error()})
				}
				continue
			}
		} else {
			// No startup config registered — skip this agent.
			if w.logger != nil {
				w.logger.Emit("sentinel_error", w.agentID(), agentID, "audit",
					map[string]any{"action": "quota_restart", "error": fmt.Sprintf("no startup config registered for role %q", paused.Role)})
			}
			continue
		}

		state.MarkLastUsed(toAccount)
		delete(state.PausedSessions, agentID)
		restarted++
		availIdx++

		if w.logger != nil {
			w.logger.Emit(events.EventQuotaRotate, w.agentID(), agentID, "both",
				map[string]any{
					"agent":      agentID,
					"to_account": toAccount,
					"world":      w.config.World,
					"resumed":    true,
				})
		}
	}

	if restarted > 0 || len(expired) > 0 {
		_ = quota.Save(state)
	}

	return restarted
}

// releaseStaleClaims releases MR claims older than ClaimTTL (forge crash recovery).
// Returns the number of released claims.
func (w *Sentinel) releaseStaleClaims() int {
	if w.worldStore == nil {
		return 0
	}
	released, err := w.worldStore.ReleaseStaleClaims(w.config.ClaimTTL, w.config.ForgeMaxAttempts)
	if err != nil {
		if w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
				map[string]any{"action": "release_stale_claims", "error": err.Error()})
		}
		return 0
	}
	if released > 0 && w.logger != nil {
		w.logger.Emit("sentinel_action", w.agentID(), w.agentID(), "feed",
			map[string]any{"action": "released_stale_claims", "count": released})
	}
	return released
}

// recastFailedMRs checks for merge requests in "failed" phase with open work
// items and re-casts them. Returns the number of writs re-cast.
// Uses exponential backoff (10m, 30m, 60m) between recast attempts.
// Caps retries at MaxRecastAttempts; after that, escalates to the autarch.
// Recast count is persisted in writ metadata to survive sentinel restarts.
func (w *Sentinel) recastFailedMRs() int {
	if w.castFn == nil {
		return 0
	}

	failedMRs, err := w.worldStore.ListMergeRequests("failed")
	if err != nil {
		if w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
				map[string]any{"action": "list_failed_mrs", "error": err.Error()})
		}
		return 0
	}

	maxAttempts := w.config.MaxRecastAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	now := w.now()
	var recastCount int
	seen := make(map[string]bool) // deduplicate by writ

	for _, mr := range failedMRs {
		if seen[mr.WritID] {
			continue
		}
		seen[mr.WritID] = true

		// Dedup guard: skip if writ was recently cast (within 2× patrol interval).
		// Prevents race where sentinel sees writ as "open" between cast and tether.
		if t, ok := w.lastCastTime[mr.WritID]; ok {
			if now.Sub(t) < 2*w.config.PatrolInterval {
				continue
			}
		}

		item, err := w.worldStore.GetWrit(mr.WritID)
		if err != nil {
			continue
		}

		// Determine if this writ is eligible for recast based on status.
		switch item.Status {
		case "open":
			// Fall through to recast logic.
		case "done":
			// A "done" writ with a failed MR and no assigned agent is orphaned.
			// Transition it to "open" so it can be recast.
			if item.Assignee == "" || item.Assignee == "-" {
				if err := w.worldStore.UpdateWrit(mr.WritID, store.WritUpdates{
					Status:   "open",
					Assignee: "-",
				}); err != nil {
					continue
				}
				// Fall through to recast logic.
			} else {
				// Agent is still assigned — let them handle it.
				continue
			}
		default:
			// "tethered" or any other status — skip and prune dedup guard.
			// Tethered writs have an agent working on them (orphaned-working
			// fix handles dead agents separately).
			delete(w.lastCastTime, mr.WritID)
			continue
		}

		// Read persistent recast state from writ metadata.
		attempts := recastCountFromMetadata(item)
		lastRecastTime := lastRecastTimeFromMetadata(item)

		if attempts >= maxAttempts {
			if attempts == maxAttempts {
				// First time hitting max — escalate once.
				w.escalateFailedRecast(mr, item, attempts)
				// Mark escalated in metadata to prevent re-escalation.
				_ = w.worldStore.SetWritMetadata(mr.WritID, map[string]any{
					"recast-count": float64(maxAttempts + 1),
				})
			}
			continue
		}

		// Backoff check: ensure enough time has elapsed before next recast.
		// For the first recast, wait after MR failure (mr.UpdatedAt).
		// For subsequent recasts, wait after the last recast (from metadata).
		var referenceTime time.Time
		if attempts == 0 {
			referenceTime = mr.UpdatedAt
		} else {
			referenceTime = lastRecastTime
		}
		backoffIdx := attempts
		if backoffIdx >= len(recastBackoffIntervals) {
			backoffIdx = len(recastBackoffIntervals) - 1
		}
		if now.Sub(referenceTime) < recastBackoffIntervals[backoffIdx] {
			continue
		}

		// Check for existing non-failed MRs to avoid creating duplicates.
		existingMRs, err := w.worldStore.ListMergeRequestsByWrit(mr.WritID, "")
		if err == nil {
			hasActiveMR := false
			for _, emr := range existingMRs {
				if emr.Phase != "failed" {
					hasActiveMR = true
					break
				}
			}
			if hasActiveMR {
				continue // Active MR exists, skip recast.
			}
		}

		result, err := w.castFn(mr.WritID)
		if err != nil {
			if w.logger != nil {
				w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
					map[string]any{
						"action": "recast",
						"mr":     mr.ID,
						"writ":   mr.WritID,
						"error":  err.Error(),
					})
			}
			continue
		}

		// Persist recast state in writ metadata (survives sentinel restart).
		_ = w.worldStore.SetWritMetadata(mr.WritID, map[string]any{
			"recast-count": float64(attempts + 1),
			"recast-last":  now.UTC().Format(time.RFC3339),
		})

		// Update dedup guard.
		w.lastCastTime[mr.WritID] = now
		recastCount++

		if w.logger != nil {
			w.logger.Emit(events.EventRecast, w.agentID(), w.agentID(), "both",
				map[string]any{
					"mr":      mr.ID,
					"writ":    mr.WritID,
					"agent":   result.AgentName,
					"attempt": attempts + 1,
				})
		}
	}

	return recastCount
}

// escalateFailedRecast sends a RECOVERY_NEEDED protocol message when a work
// item has exceeded the maximum recast attempts, and creates a formal
// escalation for durable tracking.
func (w *Sentinel) escalateFailedRecast(mr store.MergeRequest, item *store.Writ, attempts int) {
	// Create formal escalation for durable tracking.
	escDesc := fmt.Sprintf("Merge failed %d times for writ %s (%s), recast limit reached", attempts, mr.WritID, item.Title)
	if _, err := w.sphereStore.CreateEscalation("high", w.config.World+"/sentinel", escDesc, "writ:"+mr.WritID); err != nil && w.logger != nil {
		w.logger.Emit("escalation_error", w.agentID(), w.agentID(), "audit",
			map[string]any{"error": err.Error()})
	}

	// Send RECOVERY_NEEDED protocol message to autarch (live nudge).
	if _, err := w.sphereStore.SendProtocolMessage(
		w.agentID(), config.Autarch,
		store.ProtoRecoveryNeeded,
		store.RecoveryNeededPayload{
			WritID: mr.WritID,
			Reason:     fmt.Sprintf("merge failed %d times for %q, recast limit reached", attempts, item.Title),
			Attempts:   attempts,
		},
	); err != nil && w.logger != nil {
		w.logger.Emit("mail_error", w.agentID(), w.agentID(), "audit",
			map[string]any{"error": err.Error()})
	}

	if w.logger != nil {
		w.logger.Emit(events.EventStalled, w.agentID(), w.agentID(), "both",
			map[string]any{
				"writ":  mr.WritID,
				"mr":         mr.ID,
				"attempts":   attempts,
				"escalated":  true,
				"reason":     "max recast attempts exceeded",
			})
	}
}

// dispatchOrphanedResolutions finds blocked MRs whose resolution writs are
// open, unassigned, and older than 5 minutes, then dispatches them via castFn.
// This catches conflict-resolution writs that were not dispatched.
// Returns the number of writs dispatched.
func (w *Sentinel) dispatchOrphanedResolutions() int {
	if w.castFn == nil {
		return 0
	}

	blockedMRs, err := w.worldStore.ListBlockedMergeRequests()
	if err != nil {
		if w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
				map[string]any{"action": "list_blocked_mrs", "error": err.Error()})
		}
		return 0
	}

	maxAttempts := w.config.MaxRecastAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	const gracePeriod = 5 * time.Minute
	var dispatched int

	for _, mr := range blockedMRs {
		blockerID := mr.BlockedBy

		writ, err := w.worldStore.GetWrit(blockerID)
		if err != nil {
			// Writ not found or error — skip.
			continue
		}

		// Only dispatch if writ is open (not already handled or closed).
		if writ.Status != "open" {
			delete(w.resolutionDispatchCounts, blockerID)
			continue
		}

		// Skip if already assigned (agent is working on it).
		if writ.Assignee != "" {
			continue
		}

		// Grace period: let other processes handle the nudge first.
		if time.Since(writ.CreatedAt) < gracePeriod {
			continue
		}

		attempts := w.resolutionDispatchCounts[blockerID]

		if attempts >= maxAttempts {
			if attempts == maxAttempts {
				// First time hitting max — escalate once.
				w.escalateOrphanedResolution(mr, writ, attempts)
				w.resolutionDispatchCounts[blockerID] = maxAttempts + 1
			}
			continue
		}

		result, err := w.castFn(blockerID)
		if err != nil {
			if w.logger != nil {
				w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
					map[string]any{
						"action": "dispatch_resolution",
						"mr":     mr.ID,
						"writ":   blockerID,
						"error":  err.Error(),
					})
			}
			continue
		}

		w.resolutionDispatchCounts[blockerID] = attempts + 1
		dispatched++

		if w.logger != nil {
			w.logger.Emit("sentinel_dispatch_resolution", w.agentID(), w.agentID(), "both",
				map[string]any{
					"mr":      mr.ID,
					"writ":    blockerID,
					"agent":   result.AgentName,
					"attempt": attempts + 1,
					"message": fmt.Sprintf("auto-dispatching orphaned conflict-resolution writ %s blocking MR %s", blockerID, mr.ID),
				})
		}
	}

	return dispatched
}

// escalateOrphanedResolution sends a RECOVERY_NEEDED protocol message when a
// conflict-resolution writ has exceeded the maximum dispatch attempts, and
// creates a formal escalation for durable tracking.
func (w *Sentinel) escalateOrphanedResolution(mr store.MergeRequest, writ *store.Writ, attempts int) {
	// Create formal escalation for durable tracking.
	escDesc := fmt.Sprintf("Orphaned conflict-resolution writ %s (%s) blocking MR %s, dispatch limit reached after %d attempts", writ.ID, writ.Title, mr.ID, attempts)
	if _, err := w.sphereStore.CreateEscalation("medium", w.config.World+"/sentinel", escDesc, "writ:"+writ.ID); err != nil && w.logger != nil {
		w.logger.Emit("escalation_error", w.agentID(), w.agentID(), "audit",
			map[string]any{"error": err.Error()})
	}

	// Send RECOVERY_NEEDED protocol message to autarch (live nudge).
	if _, err := w.sphereStore.SendProtocolMessage(
		w.agentID(), config.Autarch,
		store.ProtoRecoveryNeeded,
		store.RecoveryNeededPayload{
			WritID: writ.ID,
			Reason:     fmt.Sprintf("orphaned conflict-resolution writ %q blocking MR %s, dispatch limit reached after %d attempts", writ.Title, mr.ID, attempts),
			Attempts:   attempts,
		},
	); err != nil && w.logger != nil {
		w.logger.Emit("mail_error", w.agentID(), w.agentID(), "audit",
			map[string]any{"error": err.Error()})
	}

	if w.logger != nil {
		w.logger.Emit(events.EventStalled, w.agentID(), w.agentID(), "both",
			map[string]any{
				"writ":     writ.ID,
				"mr":       mr.ID,
				"attempts": attempts,
				"escalated": true,
				"reason":   "max resolution dispatch attempts exceeded",
			})
	}
}

// recoverOrphanedDoneWrits detects writs stuck in "done" status with no active
// merge request. This state arises when the resolve process crashes between
// updating the writ status to "done" and creating the MR. Without recovery the
// writ stays in "done" forever — forge never sees it because there is no MR,
// and no agent picks it up because it is no longer "open".
//
// Recovery: reopen the writ to "open" so the normal dispatch flow can re-cast
// and re-resolve it. Only reopens writs that have no active MRs (ready/claimed)
// and no tethered agent — writs with active MRs are being handled by forge.
// Returns the number of writs recovered.
func (w *Sentinel) recoverOrphanedDoneWrits() int {
	if w.worldStore == nil {
		return 0
	}

	doneWrits, err := w.worldStore.ListWrits(store.ListFilters{Status: "done"})
	if err != nil {
		if w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
				map[string]any{"action": "list_done_writs", "error": err.Error()})
		}
		return 0
	}

	var recovered int
	for _, writ := range doneWrits {
		// Check if the writ has any active (non-failed, non-superseded) MRs.
		mrs, err := w.worldStore.ListMergeRequestsByWrit(writ.ID, "")
		if err != nil {
			continue
		}
		hasActiveMR := false
		for _, mr := range mrs {
			if mr.Phase == "ready" || mr.Phase == "claimed" || mr.Phase == "merged" {
				hasActiveMR = true
				break
			}
		}
		if hasActiveMR {
			continue // MR exists — forge will handle it
		}

		// Check if any agent is tethered to this writ (resolve may still be in progress).
		if writ.Assignee != "" {
			// Look up agent — if agent exists and is working, skip.
			agent, err := w.sphereStore.GetAgent(writ.Assignee)
			if err == nil && agent.State == "working" {
				continue
			}
		}

		// Grace period: only recover writs that have been stuck for at least 5 minutes.
		if time.Since(writ.UpdatedAt) < 5*time.Minute {
			continue
		}

		// Reopen the writ so dispatch can re-cast it.
		if err := w.worldStore.UpdateWrit(writ.ID, store.WritUpdates{Status: "open"}); err != nil {
			if w.logger != nil {
				w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
					map[string]any{"action": "reopen_orphaned_done_writ", "writ": writ.ID, "error": err.Error()})
			}
			continue
		}

		recovered++
		if w.logger != nil {
			w.logger.Emit("sentinel_recovery", w.agentID(), w.agentID(), "both",
				map[string]any{
					"action":  "reopened_orphaned_done_writ",
					"writ":    writ.ID,
					"title":   writ.Title,
					"message": fmt.Sprintf("reopened writ %s stuck in done with no active MR", writ.ID),
				})
		}
	}

	return recovered
}

// cleanupAgentResources removes all disk resources for an agent: worktree,
// session metadata, tether file, handoff file, and workflow directory.
// Best-effort: logs errors but does not fail.
func (w *Sentinel) cleanupAgentResources(agentName string) {
	sessionName := config.SessionName(w.config.World, agentName)

	// Stop session if still alive.
	if w.sessions.Exists(sessionName) {
		if err := w.sessions.Stop(sessionName, true); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel: failed to stop session %s: %v\n", sessionName, err)
		}
	}

	// Remove worktree via git.
	worktreeDir := dispatch.WorktreePath(w.config.World, agentName)
	if _, err := os.Stat(worktreeDir); err == nil {
		repoPath := config.RepoPath(w.config.World)
		rmCmd := exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", worktreeDir)
		if out, err := rmCmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel: worktree remove failed: %s: %v\n",
				strings.TrimSpace(string(out)), err)
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

	// Clear tether file (outpost agents only — this is called from cleanupOrphanedOutpostDirs).
	tether.Clear(w.config.World, agentName, "outpost") // best-effort

	// Remove handoff file.
	handoff.Remove(w.config.World, agentName, "outpost") // best-effort

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
		// since there is no matching agent record in sphere.db.
		w.cleanupAgentResources(name)

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
		tether.Clear(w.config.World, name, "outpost")
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

