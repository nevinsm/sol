package consul

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/escalation"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// Config holds consul patrol configuration.
type Config struct {
	PatrolInterval      time.Duration // time between patrols (default: 5 minutes)
	StaleTetherTimeout  time.Duration // how long a tether can be stale (default: 15 minutes)
	HeartbeatDir        string        // path to heartbeat directory (default: $SOL_HOME/consul)
	SolHome             string        // $SOL_HOME path
	EscalationWebhook   string        // webhook URL for escalation routing (optional)
	EscalationThreshold int           // buildup alert threshold (default: 5)
	EscalationConfig    config.EscalationSection // aging thresholds from sol.toml
}

// DefaultConfig returns a Config with default values.
func DefaultConfig() Config {
	return Config{
		PatrolInterval:      5 * time.Minute,
		StaleTetherTimeout:  15 * time.Minute,
		EscalationThreshold: 5,
		EscalationConfig:    config.DefaultEscalationConfig(),
	}
}

// SphereStore is the subset of store.Store used by the consul.
type SphereStore interface {
	// Agents
	ListAgents(world string, state string) ([]store.Agent, error)
	UpdateAgentState(id, state, activeWrit string) error
	GetAgent(id string) (*store.Agent, error)
	FindIdleAgent(world string) (*store.Agent, error)
	CreateAgent(name, world, role string) (string, error)
	EnsureAgent(name, world, role string) error
	DeleteAgent(id string) error

	// Caravans
	ListCaravans(status string) ([]store.Caravan, error)
	GetCaravan(id string) (*store.Caravan, error)
	CheckCaravanReadiness(caravanID string, worldOpener func(string) (*store.Store, error)) ([]store.CaravanItemStatus, error)
	TryCloseCaravan(caravanID string, worldOpener func(string) (*store.Store, error)) (bool, error)

	// Worlds
	ListWorlds() ([]store.World, error)

	// Escalations
	CreateEscalation(severity, source, description string, sourceRef ...string) (string, error)
	ListEscalationsBySourceRef(sourceRef string) ([]store.Escalation, error)
	ResolveEscalation(id string) error
	CountOpen() (int, error)
	ListOpenEscalations() ([]store.Escalation, error)
	UpdateEscalationLastNotified(id string) error

	// Messages
	PendingProtocol(recipient, protoType string) ([]store.Message, error)
	AckMessage(id string) error
	SendMessage(sender, recipient, subject, body string, priority int, msgType string) (string, error)
	SendProtocolMessage(sender, recipient, protoType string, payload any) (string, error)

	// Close
	Close() error
}

// SessionManager is the session operations used by the consul.
// Embeds the canonical session.SessionManager and adds List for session enumeration.
type SessionManager interface {
	session.SessionManager
	List() ([]session.SessionInfo, error)
}

// WorldOpener opens a world store by name.
type WorldOpener func(world string) (*store.Store, error)

// DispatchFunc dispatches a single writ. Returns agent name and session name.
// Default implementation uses dispatch.Cast.
type DispatchFunc func(ctx context.Context, opts dispatch.CastOpts, worldStore dispatch.WorldStore, sphereStore dispatch.SphereStore, mgr dispatch.SessionManager, logger *events.Logger) (*dispatch.CastResult, error)

// orphanEntry tracks a candidate orphaned session across patrols.
type orphanEntry struct {
	firstSeen time.Time // when the session was first detected as orphaned
	count     int       // consecutive patrol detections
}

// orphanGracePeriod is the minimum time a session must be detected as orphaned
// before it can be stopped. Prevents killing sessions during startup races.
const orphanGracePeriod = 30 * time.Minute

// orphanConsecutiveThreshold is how many consecutive patrols a session must be
// detected as orphaned before it is stopped.
const orphanConsecutiveThreshold = 2

// infrastructureSessions are sphere-level tmux sessions not tracked as agents.
// Note: consul is a PID-managed process (not a tmux session) and is excluded.
var infrastructureSessions = []string{
	"sol-chronicle",
	"sol-broker",
}

// Consul is the sphere-level patrol process.
type Consul struct {
	config       Config
	sphereStore  SphereStore
	sessions     SessionManager
	logger       *events.Logger
	router       *escalation.Router
	worldOpener  WorldOpener
	dispatchFunc DispatchFunc

	patrolCount         int
	orphanedSessions    map[string]*orphanEntry
	lastEscalationAlert time.Time // debounce buildup alerts (30 min cooldown)
}

// New creates a new Consul.
func New(cfg Config, sphereStore SphereStore, sessions SessionManager,
	router *escalation.Router, logger *events.Logger) *Consul {
	return &Consul{
		config:           cfg,
		sphereStore:      sphereStore,
		sessions:         sessions,
		logger:           logger,
		router:           router,
		worldOpener:      store.OpenWorld,
		dispatchFunc:     dispatch.Cast,
		orphanedSessions: make(map[string]*orphanEntry),
	}
}

// SetWorldOpener sets a custom world opener for testing.
func (d *Consul) SetWorldOpener(opener WorldOpener) {
	d.worldOpener = opener
}

// SetDispatchFunc sets a custom dispatch function for testing.
func (d *Consul) SetDispatchFunc(fn DispatchFunc) {
	d.dispatchFunc = fn
}

// logInfo emits a structured event log if a logger is configured.
func (d *Consul) logInfo(eventType string, meta map[string]any) {
	if d.logger != nil {
		d.logger.Emit(eventType, "sphere/consul", "sphere/consul", "feed", meta)
	}
}

// Register creates or updates the consul's agent record.
// Agent ID: "sphere/consul", role: "consul", state: "working".
func (d *Consul) Register() error {
	return d.sphereStore.EnsureAgent("consul", "sphere", "consul")
}

// Run starts the consul patrol loop. Blocks until ctx is cancelled.
// 1. Register as agent (role="consul", world="sphere")
// 2. Write initial heartbeat
// 3. Loop: patrol -> sleep -> repeat
//
// On context cancellation:
// - Write final heartbeat with status="stopping"
// - Set agent state to "idle"
func (d *Consul) Run(ctx context.Context) error {
	if err := d.Register(); err != nil {
		return fmt.Errorf("failed to register consul: %w", err)
	}

	if err := d.sphereStore.UpdateAgentState("sphere/consul", "working", ""); err != nil {
		return fmt.Errorf("failed to set consul working: %w", err)
	}

	// shutdown writes the final heartbeat and sets agent state to idle.
	shutdown := func() {
		openEsc, _ := d.sphereStore.CountOpen()
		_ = WriteHeartbeat(d.config.SolHome, &Heartbeat{
			Timestamp:   time.Now().UTC(),
			PatrolCount: d.patrolCount,
			Status:      "stopping",
			Escalations: openEsc,
		})
		_ = d.sphereStore.UpdateAgentState("sphere/consul", "idle", "")
	}

	// Patrol immediately.
	if errors.Is(d.Patrol(ctx), errShutdown) {
		shutdown()
		return nil
	}

	ticker := time.NewTicker(d.config.PatrolInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			shutdown()
			return nil

		case <-ticker.C:
			if errors.Is(d.Patrol(ctx), errShutdown) {
				shutdown()
				return nil
			}
		}
	}
}

// Patrol runs a single patrol cycle:
// 1. Recover stale tethers
// 2. Feed stranded caravans
// 3. Process lifecycle requests
// 4. Detect orphaned sessions
// 5. Check aging escalations + buildup alerting + stale source-ref resolution
// 6. Count open escalations
// 7. Write heartbeat
// 8. Emit patrol event
//
// Errors in individual patrol steps are logged but do not stop the
// patrol cycle. The consul continues to the next step (DEGRADE).
func (d *Consul) Patrol(ctx context.Context) error {
	d.patrolCount++

	var staleTethers, caravanFeeds, orphansStopped int
	var shutdown bool
	var escRenotified int
	var escalationAlert bool

	// 1. Recover stale tethers.
	recovered, err := d.recoverStaleTethers(ctx)
	if err != nil {
		d.logInfo("consul_error", map[string]any{"action": "stale_tether_recovery", "error": err.Error()})
	}
	staleTethers = recovered

	if ctx.Err() != nil {
		return ctx.Err()
	}

	// 2. Feed stranded caravans.
	fed, err := d.feedStrandedCaravans(ctx)
	if err != nil {
		d.logInfo("consul_error", map[string]any{"action": "caravan_feeding", "error": err.Error()})
	}
	caravanFeeds = fed

	if ctx.Err() != nil {
		return ctx.Err()
	}

	// 3. Process lifecycle requests.
	shutdown, err = d.processLifecycleRequests(ctx)
	if err != nil {
		d.logInfo("consul_error", map[string]any{"action": "lifecycle_requests", "error": err.Error()})
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	// 4. Detect orphaned sessions.
	stopped, err := d.detectOrphanedSessions(ctx)
	if err != nil {
		d.logInfo("consul_error", map[string]any{"action": "detect_orphaned_sessions", "error": err.Error()})
	}
	orphansStopped = stopped

	if ctx.Err() != nil {
		return ctx.Err()
	}

	// 5. Check aging escalations, buildup alerting, and stale source-ref resolution.
	renotified, err := d.checkAgingEscalations(ctx)
	if err != nil {
		d.logInfo("consul_error", map[string]any{"action": "aging_escalations", "error": err.Error()})
	}
	escRenotified = renotified
	escalationAlert = d.checkEscalationBuildup(ctx)
	d.resolveStaleSourceRefs(ctx)

	if ctx.Err() != nil {
		return ctx.Err()
	}

	// 6. Count open escalations.
	openEsc, err := d.sphereStore.CountOpen()
	if err != nil {
		d.logInfo("consul_error", map[string]any{"action": "count_escalations", "error": err.Error()})
	}

	// 7. Write heartbeat.
	status := "running"
	if shutdown {
		status = "stopping"
	}
	if err := WriteHeartbeat(d.config.SolHome, &Heartbeat{
		Timestamp:        time.Now().UTC(),
		PatrolCount:      d.patrolCount,
		Status:           status,
		StaleTethers:     staleTethers,
		CaravanFeeds:     caravanFeeds,
		Escalations:      openEsc,
		OrphanedSessions: orphansStopped,
		EscRenotified:    escRenotified,
		EscalationAlert:  escalationAlert,
	}); err != nil {
		d.logInfo("consul_error", map[string]any{"action": "write_heartbeat", "error": err.Error()})
	}

	// 8. Emit patrol event.
	if d.logger != nil {
		d.logger.Emit(events.EventConsulPatrol, "sphere/consul", "sphere/consul", "feed",
			map[string]any{
				"patrol_count":      d.patrolCount,
				"stale_tethers":     staleTethers,
				"caravan_feeds":     caravanFeeds,
				"escalations":       openEsc,
				"orphaned_sessions": orphansStopped,
				"esc_renotified":    escRenotified,
				"escalation_alert":  escalationAlert,
			})
	}

	// 9. Log patrol summary.
	d.logInfo("consul_patrol", map[string]any{
		"patrol_count":      d.patrolCount,
		"stale_tethers":     staleTethers,
		"caravan_feeds":     caravanFeeds,
		"escalations":       openEsc,
		"orphaned_sessions": orphansStopped,
		"esc_renotified":    escRenotified,
		"escalation_alert":  escalationAlert,
	})

	// If shutdown was requested, return a sentinel error that Run will detect.
	if shutdown {
		return errShutdown
	}

	return nil
}

var errShutdown = fmt.Errorf("shutdown requested")

// recoverStaleTethers finds and recovers stale tethers across all worlds.
// For each stale tether:
// 1. Log the recovery
// 2. Clear the tether file
// 3. Update writ status -> "open", clear assignee
// 4. Update agent state -> "idle", clear active_writ
// 5. Emit event
//
// Returns the number of tethers recovered.
func (d *Consul) recoverStaleTethers(ctx context.Context) (int, error) {
	// List all agents with state "working".
	agents, err := d.sphereStore.ListAgents("", "working")
	if err != nil {
		return 0, fmt.Errorf("failed to list working agents: %w", err)
	}

	var recovered int
	for _, agent := range agents {
		if ctx.Err() != nil {
			return recovered, ctx.Err()
		}

		// Skip infrastructure roles (don't recover sentinel/forge/consul).
		// Recover agents, envoys, and governors.
		if agent.Role != "outpost" && agent.Role != "envoy" && agent.Role != "governor" {
			continue
		}

		// Skip agents without tethered work.
		if agent.ActiveWrit == "" {
			continue
		}

		// Check if session is alive.
		sessionName := config.SessionName(agent.World, agent.Name)
		if d.sessions.Exists(sessionName) {
			continue // session alive, not stale
		}

		// Check if the agent's updated_at is older than StaleTetherTimeout.
		if time.Since(agent.UpdatedAt) < d.config.StaleTetherTimeout {
			continue // too recent, might still be starting
		}

		// This tether is stale — recover it.
		if err := d.recoverOneTether(agent); err != nil {
			d.logInfo("consul_error", map[string]any{"action": "recover_stale_tether", "agent_id": agent.ID, "error": err.Error()})
			continue // DEGRADE: skip this agent, try the next
		}
		recovered++
	}

	return recovered, nil
}

// recoverOneTether recovers a single stale tether.
//
// CRASH SAFETY: The operation ordering ensures idempotent crash recovery.
// Consul detects stale tethers by finding "working" agents with no live
// session. The agent state update is done LAST — while the agent is still
// "working", a crash at any point causes consul to retry on the next patrol.
// Steps 1-3 are idempotent (updating an already-open writ is a no-op,
// clearing an already-cleared tether is a no-op), so retries are safe.
func (d *Consul) recoverOneTether(agent store.Agent) error {
	d.logInfo("consul_recover_tether", map[string]any{"agent_id": agent.ID, "writ_id": agent.ActiveWrit})

	// 1. Open the world store to update the writ.
	worldStore, err := d.worldOpener(agent.World)
	if err != nil {
		return fmt.Errorf("failed to open world %q: %w", agent.World, err)
	}
	defer worldStore.Close()

	// 2. Update writ: status -> "open", clear assignee.
	// Done first so work becomes available for reassignment immediately.
	if err := worldStore.UpdateWrit(agent.ActiveWrit, store.WritUpdates{
		Status:   "open",
		Assignee: "-", // "-" clears assignee
	}); err != nil {
		return fmt.Errorf("failed to update writ %q: %w", agent.ActiveWrit, err)
	}

	// 3. Clear the tether file.
	// Done before agent state update — if we crash here, the agent is still
	// "working" so consul will retry on next patrol.
	if err := tether.Clear(agent.World, agent.Name, agent.Role); err != nil {
		return fmt.Errorf("failed to clear tether for %q: %w", agent.ID, err)
	}

	// 4. Update agent state -> "idle", clear active_writ.
	// Done LAST — this is the signal that recovery is complete. While the
	// agent is still "working", consul's next patrol will re-detect and
	// retry (all prior steps are idempotent).
	if err := d.sphereStore.UpdateAgentState(agent.ID, "idle", ""); err != nil {
		return fmt.Errorf("failed to update agent %q state: %w", agent.ID, err)
	}

	// 5. Emit event.
	if d.logger != nil {
		d.logger.Emit(events.EventConsulStaleTether, "sphere/consul", "sphere/consul", "both",
			map[string]any{
				"agent_id":     agent.ID,
				"writ_id": agent.ActiveWrit,
				"world":        agent.World,
			})
	}

	return nil
}

// feedStrandedCaravans checks all open caravans for ready, undispatched items
// and dispatches them directly using dispatch.Cast.
//
// For each open caravan:
// 1. Check readiness of all items
// 2. Group ready items by world
// 3. Dispatch each ready item via dispatch.Cast
// 4. Attempt auto-close after dispatch
// 5. Emit events
//
// Returns the number of items dispatched.
func (d *Consul) feedStrandedCaravans(ctx context.Context) (int, error) {
	caravans, err := d.sphereStore.ListCaravans("open")
	if err != nil {
		return 0, fmt.Errorf("failed to list open caravans: %w", err)
	}

	var totalDispatched int
	for _, caravan := range caravans {
		if ctx.Err() != nil {
			return totalDispatched, ctx.Err()
		}

		statuses, err := d.sphereStore.CheckCaravanReadiness(caravan.ID, func(world string) (*store.Store, error) {
			return d.worldOpener(world)
		})
		if err != nil {
			d.logInfo("consul_error", map[string]any{"action": "check_caravan_readiness", "caravan_id": caravan.ID, "error": err.Error()})
			continue // DEGRADE
		}

		// Group ready items by world.
		readyByWorld := map[string][]store.CaravanItemStatus{}
		for _, st := range statuses {
			if st.Ready && st.WritStatus == "open" {
				readyByWorld[st.World] = append(readyByWorld[st.World], st)
			}
		}

		if len(readyByWorld) > 0 {
			// Dispatch ready items per world.
			caravanDispatched := 0
			for world, items := range readyByWorld {
				dispatched, dispatchErr := d.dispatchWorldItems(ctx, caravan.ID, world, items)
				caravanDispatched += dispatched
				if dispatchErr != nil {
					d.logInfo("consul_error", map[string]any{
						"action":     "dispatch_world_items",
						"caravan_id": caravan.ID,
						"world":      world,
						"error":      dispatchErr.Error(),
					})
					// DEGRADE: continue with other worlds
				}
			}

			totalDispatched += caravanDispatched

			if caravanDispatched > 0 {
				// Emit caravan-level feed event.
				if d.logger != nil {
					d.logger.Emit(events.EventConsulCaravanFeed, "sphere/consul", "sphere/consul", "both",
						map[string]any{
							"caravan_id": caravan.ID,
							"dispatched": caravanDispatched,
						})
				}
			}
		}

		// Try to auto-close the caravan (unconditional — items may have
		// been merged since the last patrol).
		closed, closeErr := d.sphereStore.TryCloseCaravan(caravan.ID, func(world string) (*store.Store, error) {
			return d.worldOpener(world)
		})
		if closeErr != nil {
			d.logInfo("consul_error", map[string]any{
				"action":     "try_close_caravan",
				"caravan_id": caravan.ID,
				"error":      closeErr.Error(),
			})
		} else if closed {
			if d.logger != nil {
				d.logger.Emit(events.EventCaravanClosed, "sphere/consul", "sphere/consul", "both",
					map[string]any{
						"caravan_id": caravan.ID,
						"name":       caravan.Name,
					})
			}
		}
	}

	return totalDispatched, nil
}

// dispatchWorldItems dispatches ready caravan items within a single world.
// Returns the number of items dispatched and any error from setup (not per-item).
func (d *Consul) dispatchWorldItems(ctx context.Context, caravanID, world string, items []store.CaravanItemStatus) (int, error) {
	worldCfg, err := config.LoadWorldConfig(world)
	if err != nil {
		return 0, fmt.Errorf("failed to load world config for %q: %w", world, err)
	}

	// Skip sleeping worlds — performance optimization to avoid per-item processing.
	// Cast's gate is the correctness boundary.
	if worldCfg.World.Sleeping {
		return 0, nil
	}

	sourceRepo, err := dispatch.ResolveSourceRepo(world, worldCfg)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve source repo for %q: %w", world, err)
	}

	worldStore, err := d.worldOpener(world)
	if err != nil {
		return 0, fmt.Errorf("failed to open world store %q: %w", world, err)
	}
	defer worldStore.Close()

	dispatched := 0
	for _, st := range items {
		if ctx.Err() != nil {
			return dispatched, ctx.Err()
		}

		// Don't re-dispatch items that have been worked on before.
		// Existing MRs (any phase) mean the writ was dispatched, worked,
		// and resolved at least once. Recovery is sentinel's domain.
		mrs, mrsErr := worldStore.ListMergeRequestsByWrit(st.WritID, "")
		if mrsErr == nil && len(mrs) > 0 {
			if d.logger != nil {
				d.logger.Emit("consul_skip_redispatch", "sphere/consul", "sphere/consul", "audit",
					map[string]any{"writ": st.WritID, "reason": "existing_mrs"})
			}
			continue
		}

		castOpts := dispatch.CastOpts{
			WritID:  st.WritID,
			World:       world,
			SourceRepo:  sourceRepo,
			WorldConfig: &worldCfg,
		}
		result, err := d.dispatchFunc(ctx, castOpts, worldStore, d.sphereStore, d.sessions, d.logger)
		if err != nil {
			// Capacity exhausted — no point trying more items in this world.
			if errors.Is(err, dispatch.ErrCapacityExhausted) {
				d.logInfo("consul_capacity_full", map[string]any{
					"caravan_id": caravanID,
					"world":      world,
					"error":      err.Error(),
				})
				break
			}
			d.logInfo("consul_error", map[string]any{
				"action":       "dispatch_item",
				"caravan_id":   caravanID,
				"writ_id": st.WritID,
				"world":        world,
				"error":        err.Error(),
			})
			continue // DEGRADE: skip this item, try the next
		}

		if d.logger != nil {
			d.logger.Emit(events.EventConsulCaravanDispatch, "sphere/consul", "sphere/consul", "both",
				map[string]any{
					"caravan_id":   caravanID,
					"writ_id": st.WritID,
					"agent":        result.AgentName,
					"session":      result.SessionName,
					"world":        world,
				})
		}

		dispatched++
	}

	return dispatched, nil
}

// processLifecycleRequests reads and processes operator messages.
// Recognized commands (in message subject):
// - "CYCLE": force immediate patrol after current one
// - "SHUTDOWN": set a flag to stop after current patrol
//
// Unrecognized messages are acknowledged but ignored.
//
// Returns true if a shutdown was requested.
func (d *Consul) processLifecycleRequests(ctx context.Context) (shutdown bool, err error) {
	msgs, err := d.sphereStore.PendingProtocol("sphere/consul", "")
	if err != nil {
		return false, fmt.Errorf("failed to read lifecycle messages: %w", err)
	}

	for _, msg := range msgs {
		if ctx.Err() != nil {
			return shutdown, ctx.Err()
		}

		switch msg.Subject {
		case "SHUTDOWN":
			shutdown = true
			d.logInfo("consul_lifecycle", map[string]any{"action": "shutdown_requested", "message_id": msg.ID})
		case "CYCLE":
			d.logInfo("consul_lifecycle", map[string]any{"action": "cycle_requested", "message_id": msg.ID})
			// No action needed — the patrol just happened.
		default:
			// Unknown message — ack and ignore.
		}

		// Acknowledge the message.
		if err := d.sphereStore.AckMessage(msg.ID); err != nil {
			d.logInfo("consul_error", map[string]any{"action": "ack_message", "message_id": msg.ID, "error": err.Error()})
		}
	}

	return shutdown, nil
}

// detectOrphanedSessions finds tmux sessions matching sol-* that have no
// corresponding agent record or known infrastructure role.
//
// Detection logic:
// 1. List all tmux sessions, filter to sol-* prefix
// 2. Build a "known sessions" set from agents + infrastructure
// 3. Track candidate orphans across patrols
// 4. Stop sessions that exceed grace period + consecutive threshold
// 5. Prune tracking map for sessions that disappeared
//
// Returns the number of sessions stopped.
func (d *Consul) detectOrphanedSessions(ctx context.Context) (int, error) {
	// 1. List all tmux sessions.
	allSessions, err := d.sessions.List()
	if err != nil {
		// DEGRADE: skip orphan detection, don't stop patrol.
		d.logInfo("consul_error", map[string]any{
			"action": "detect_orphaned_sessions",
			"error":  err.Error(),
		})
		return 0, nil
	}

	// 2. Filter to sol-* prefix sessions.
	var solSessions []session.SessionInfo
	for _, s := range allSessions {
		if strings.HasPrefix(s.Name, "sol-") {
			solSessions = append(solSessions, s)
		}
	}

	if len(solSessions) == 0 {
		// No sol-* sessions at all — clear the tracking map.
		d.orphanedSessions = make(map[string]*orphanEntry)
		return 0, nil
	}

	// 3. Build the "known sessions" set.
	known := make(map[string]bool)

	// 3a. Infrastructure sessions (not tracked as agents).
	for _, name := range infrastructureSessions {
		known[name] = true
	}

	// 3b. All agents (any state) from sphere store → session names.
	agents, err := d.sphereStore.ListAgents("", "")
	if err != nil {
		return 0, fmt.Errorf("failed to list agents for orphan detection: %w", err)
	}
	for _, agent := range agents {
		known[config.SessionName(agent.World, agent.Name)] = true
	}

	// 3c. Per-world infrastructure: sentinel, forge, governor.
	worlds, err := d.sphereStore.ListWorlds()
	if err != nil {
		return 0, fmt.Errorf("failed to list worlds for orphan detection: %w", err)
	}
	for _, w := range worlds {
		// Sentinel is a direct Go process (no tmux session), so no session to mark as known.
		known[fmt.Sprintf("sol-%s-forge", w.Name)] = true
		known[fmt.Sprintf("sol-%s-governor", w.Name)] = true
	}

	// 4. Detect orphans and apply grace period + consecutive threshold.
	now := time.Now()
	currentSessions := make(map[string]bool)
	var stopped int

	for _, s := range solSessions {
		currentSessions[s.Name] = true

		if known[s.Name] {
			continue // known session, not orphaned
		}

		entry, exists := d.orphanedSessions[s.Name]
		if !exists {
			// First detection — start tracking.
			d.orphanedSessions[s.Name] = &orphanEntry{
				firstSeen: now,
				count:     1,
			}
			continue
		}

		entry.count++

		// Grace period: skip if first seen less than 30 minutes ago.
		if now.Sub(entry.firstSeen) < orphanGracePeriod {
			continue
		}

		// Consecutive threshold: need at least 2 consecutive detections.
		if entry.count < orphanConsecutiveThreshold {
			continue
		}

		// Stop the orphaned session.
		if err := d.sessions.Stop(s.Name, false); err != nil {
			d.logInfo("consul_error", map[string]any{
				"action":  "stop_orphaned_session",
				"session": s.Name,
				"error":   err.Error(),
			})
			continue
		}

		d.logInfo("consul_orphan_stopped", map[string]any{
			"session":      s.Name,
			"patrol_count": entry.count,
			"age_minutes":  int(now.Sub(entry.firstSeen).Minutes()),
		})

		if d.logger != nil {
			d.logger.Emit(events.EventConsulPatrol, "sphere/consul", "sphere/consul", "both",
				map[string]any{
					"action":  "orphan_stopped",
					"session": s.Name,
				})
		}

		stopped++
		delete(d.orphanedSessions, s.Name)
	}

	// 5. Prune map entries for sessions no longer present.
	for name := range d.orphanedSessions {
		if !currentSessions[name] {
			delete(d.orphanedSessions, name)
		}
	}

	return stopped, nil
}

