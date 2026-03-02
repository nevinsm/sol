package consul

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/escalation"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// Config holds consul patrol configuration.
type Config struct {
	PatrolInterval    time.Duration // time between patrols (default: 5 minutes)
	StaleTetherTimeout time.Duration // how long a tether can be stale (default: 1 hour)
	HeartbeatDir      string        // path to heartbeat directory (default: $SOL_HOME/consul)
	SolHome           string        // $SOL_HOME path
	EscalationWebhook string        // webhook URL for escalation routing (optional)
}

// DefaultConfig returns a Config with default values.
func DefaultConfig() Config {
	return Config{
		PatrolInterval:   5 * time.Minute,
		StaleTetherTimeout: 1 * time.Hour,
	}
}

// SphereStore is the subset of store.Store used by the consul.
type SphereStore interface {
	// Agents
	ListAgents(world string, state string) ([]store.Agent, error)
	UpdateAgentState(id, state, tetherItem string) error
	GetAgent(id string) (*store.Agent, error)
	CreateAgent(name, world, role string) (string, error)

	// Caravans
	ListCaravans(status string) ([]store.Caravan, error)
	CheckCaravanReadiness(caravanID string, worldOpener func(string) (*store.Store, error)) ([]store.CaravanItemStatus, error)

	// Escalations
	CreateEscalation(severity, source, description string) (string, error)
	CountOpen() (int, error)

	// Messages
	PendingProtocol(recipient, protoType string) ([]store.Message, error)
	AckMessage(id string) error
	SendMessage(sender, recipient, subject, body string, priority int, msgType string) (string, error)
	SendProtocolMessage(sender, recipient, protoType string, payload any) (string, error)
}

// SessionChecker is the subset of session.Manager used by the consul.
type SessionChecker interface {
	Exists(name string) bool
	List() ([]session.SessionInfo, error)
}

// WorldOpener opens a world store by name.
type WorldOpener func(world string) (*store.Store, error)

// Consul is the sphere-level patrol process.
type Consul struct {
	config      Config
	sphereStore SphereStore
	sessions    SessionChecker
	logger      *events.Logger
	router      *escalation.Router
	worldOpener WorldOpener

	patrolCount int
}

// New creates a new Consul.
func New(cfg Config, sphereStore SphereStore, sessions SessionChecker,
	router *escalation.Router, logger *events.Logger) *Consul {
	return &Consul{
		config:      cfg,
		sphereStore: sphereStore,
		sessions:    sessions,
		logger:      logger,
		router:      router,
		worldOpener: store.OpenWorld,
	}
}

// SetWorldOpener sets a custom world opener for testing.
func (d *Consul) SetWorldOpener(opener WorldOpener) {
	d.worldOpener = opener
}

// Register creates or updates the consul's agent record.
// Agent ID: "sphere/consul", role: "consul", state: "working".
func (d *Consul) Register() error {
	agent, err := d.sphereStore.GetAgent("sphere/consul")
	if err == nil && agent != nil {
		return nil // already registered
	}
	// If GetAgent failed (e.g., DB error), fall through to CreateAgent
	// which will fail cleanly if the agent already exists (unique constraint).
	_, createErr := d.sphereStore.CreateAgent("consul", "sphere", "consul")
	return createErr
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
	if errors.Is(d.Patrol(), errShutdown) {
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
			if errors.Is(d.Patrol(), errShutdown) {
				shutdown()
				return nil
			}
		}
	}
}

// Patrol runs a single patrol cycle:
// 1. Write heartbeat
// 2. Recover stale tethers
// 3. Feed stranded caravans
// 4. Process lifecycle requests
// 5. Emit patrol event
//
// Errors in individual patrol steps are logged but do not stop the
// patrol cycle. The consul continues to the next step (DEGRADE).
func (d *Consul) Patrol() error {
	d.patrolCount++

	var staleTethers, caravanFeeds int
	var shutdown bool

	// 1. Recover stale tethers.
	recovered, err := d.recoverStaleTethers()
	if err != nil {
		log.Printf("consul: stale tether recovery error: %v", err)
	}
	staleTethers = recovered

	// 2. Feed stranded caravans.
	fed, err := d.feedStrandedCaravans()
	if err != nil {
		log.Printf("consul: caravan feeding error: %v", err)
	}
	caravanFeeds = fed

	// 3. Process lifecycle requests.
	shutdown, err = d.processLifecycleRequests()
	if err != nil {
		log.Printf("consul: lifecycle request error: %v", err)
	}

	// 4. Count open escalations.
	openEsc, err := d.sphereStore.CountOpen()
	if err != nil {
		log.Printf("consul: count escalations error: %v", err)
	}

	// 5. Write heartbeat.
	status := "running"
	if shutdown {
		status = "stopping"
	}
	_ = WriteHeartbeat(d.config.SolHome, &Heartbeat{
		Timestamp:    time.Now().UTC(),
		PatrolCount:  d.patrolCount,
		Status:       status,
		StaleTethers: staleTethers,
		CaravanFeeds: caravanFeeds,
		Escalations:  openEsc,
	})

	// 6. Emit patrol event.
	if d.logger != nil {
		d.logger.Emit(events.EventConsulPatrol, "sphere/consul", "sphere/consul", "feed",
			map[string]any{
				"patrol_count":  fmt.Sprintf("%d", d.patrolCount),
				"stale_tethers": fmt.Sprintf("%d", staleTethers),
				"caravan_feeds": fmt.Sprintf("%d", caravanFeeds),
				"escalations":   fmt.Sprintf("%d", openEsc),
			})
	}

	// 7. Log patrol summary.
	log.Printf("[%s] Patrol #%d: %d stale tether%s recovered, %d caravan feed%s, %d open escalation%s",
		time.Now().UTC().Format(time.RFC3339),
		d.patrolCount,
		staleTethers, plural(staleTethers),
		caravanFeeds, plural(caravanFeeds),
		openEsc, plural(openEsc))

	// If shutdown was requested, return a sentinel error that Run will detect.
	if shutdown {
		return errShutdown
	}

	return nil
}

var errShutdown = fmt.Errorf("shutdown requested")

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// recoverStaleTethers finds and recovers stale tethers across all worlds.
// For each stale tether:
// 1. Log the recovery
// 2. Clear the tether file
// 3. Update work item status -> "open", clear assignee
// 4. Update agent state -> "idle", clear tether_item
// 5. Emit event
//
// Returns the number of tethers recovered.
func (d *Consul) recoverStaleTethers() (int, error) {
	// List all agents with state "working".
	agents, err := d.sphereStore.ListAgents("", "working")
	if err != nil {
		return 0, fmt.Errorf("failed to list working agents: %w", err)
	}

	var recovered int
	for _, agent := range agents {
		// Skip non-agent agents (don't recover sentinel/forge/consul).
		if agent.Role != "agent" {
			continue
		}

		// Skip agents without tethered work.
		if agent.TetherItem == "" {
			continue
		}

		// Check if session is alive.
		sessionName := dispatch.SessionName(agent.World, agent.Name)
		if d.sessions.Exists(sessionName) {
			continue // session alive, not stale
		}

		// Check if the agent's updated_at is older than StaleTetherTimeout.
		if time.Since(agent.UpdatedAt) < d.config.StaleTetherTimeout {
			continue // too recent, might still be starting
		}

		// This tether is stale — recover it.
		if err := d.recoverOneTether(agent); err != nil {
			log.Printf("consul: failed to recover stale tether for %s: %v", agent.ID, err)
			continue // DEGRADE: skip this agent, try the next
		}
		recovered++
	}

	return recovered, nil
}

// recoverOneTether recovers a single stale tether.
func (d *Consul) recoverOneTether(agent store.Agent) error {
	log.Printf("consul: recovering stale tether for %s (work item: %s)", agent.ID, agent.TetherItem)

	// 1. Open the world store to update the work item.
	worldStore, err := d.worldOpener(agent.World)
	if err != nil {
		return fmt.Errorf("failed to open world %q: %w", agent.World, err)
	}
	defer worldStore.Close()

	// 2. Update work item: status -> "open", clear assignee.
	if err := worldStore.UpdateWorkItem(agent.TetherItem, store.WorkItemUpdates{
		Status:   "open",
		Assignee: "-", // "-" clears assignee
	}); err != nil {
		return fmt.Errorf("failed to update work item %q: %w", agent.TetherItem, err)
	}

	// 3. Update agent state -> "idle", clear tether_item.
	if err := d.sphereStore.UpdateAgentState(agent.ID, "idle", ""); err != nil {
		return fmt.Errorf("failed to update agent %q state: %w", agent.ID, err)
	}

	// 4. Clear the tether file.
	if err := tether.Clear(agent.World, agent.Name); err != nil {
		return fmt.Errorf("failed to clear tether for %q: %w", agent.ID, err)
	}

	// 5. Emit event.
	if d.logger != nil {
		d.logger.Emit(events.EventConsulStaleTether, "sphere/consul", "sphere/consul", "both",
			map[string]any{
				"agent_id":     agent.ID,
				"work_item_id": agent.TetherItem,
				"world":        agent.World,
			})
	}

	return nil
}

// feedStrandedCaravans checks all open caravans for ready, undispatched items.
// For each stranded caravan:
// 1. Check readiness of all items
// 2. For items that are ready and status="open":
//   - Send CARAVAN_NEEDS_FEEDING protocol message to "operator"
//
// 3. Emit event
//
// The consul does NOT dispatch directly — it sends a protocol message
// that the operator (or automation) can act on.
//
// Returns the number of caravans with ready items.
func (d *Consul) feedStrandedCaravans() (int, error) {
	caravans, err := d.sphereStore.ListCaravans("open")
	if err != nil {
		return 0, fmt.Errorf("failed to list open caravans: %w", err)
	}

	var fed int
	for _, caravan := range caravans {
		statuses, err := d.sphereStore.CheckCaravanReadiness(caravan.ID, func(world string) (*store.Store, error) {
			return d.worldOpener(world)
		})
		if err != nil {
			log.Printf("consul: failed to check caravan %s readiness: %v", caravan.ID, err)
			continue // DEGRADE
		}

		// Count items that are ready and status="open" (not yet dispatched).
		var readyCount int
		for _, st := range statuses {
			if st.Ready && st.WorkItemStatus == "open" {
				readyCount++
			}
		}

		if readyCount == 0 {
			continue
		}

		// Check for existing pending CARAVAN_NEEDS_FEEDING message for this caravan
		// to prevent duplicates.
		pending, err := d.sphereStore.PendingProtocol("operator", store.ProtoCaravanNeedsFeeding)
		if err != nil {
			log.Printf("consul: failed to check pending messages: %v", err)
			continue
		}

		alreadyPending := false
		for _, msg := range pending {
			if caravanPayloadContainsID(msg.Body, caravan.ID) {
				alreadyPending = true
				break
			}
		}
		if alreadyPending {
			continue
		}

		// Send CARAVAN_NEEDS_FEEDING protocol message.
		if _, err := d.sphereStore.SendProtocolMessage(
			"sphere/consul", "operator",
			store.ProtoCaravanNeedsFeeding,
			store.CaravanNeedsFeedingPayload{
				CaravanID:  caravan.ID,
				ReadyCount: readyCount,
			},
		); err != nil {
			log.Printf("consul: failed to send caravan feed message for %s: %v", caravan.ID, err)
			continue
		}

		// Emit event.
		if d.logger != nil {
			d.logger.Emit(events.EventConsulCaravanFeed, "sphere/consul", "sphere/consul", "both",
				map[string]any{
					"caravan_id":  caravan.ID,
					"ready_count": fmt.Sprintf("%d", readyCount),
				})
		}

		fed++
	}

	return fed, nil
}

// processLifecycleRequests reads and processes operator messages.
// Recognized commands (in message subject):
// - "CYCLE": force immediate patrol after current one
// - "SHUTDOWN": set a flag to stop after current patrol
//
// Unrecognized messages are acknowledged but ignored.
//
// Returns true if a shutdown was requested.
func (d *Consul) processLifecycleRequests() (shutdown bool, err error) {
	msgs, err := d.sphereStore.PendingProtocol("sphere/consul", "")
	if err != nil {
		return false, fmt.Errorf("failed to read lifecycle messages: %w", err)
	}

	for _, msg := range msgs {
		switch msg.Subject {
		case "SHUTDOWN":
			shutdown = true
			log.Printf("consul: shutdown requested via message %s", msg.ID)
		case "CYCLE":
			log.Printf("consul: cycle requested via message %s", msg.ID)
			// No action needed — the patrol just happened.
		default:
			// Unknown message — ack and ignore.
		}

		// Acknowledge the message.
		if err := d.sphereStore.AckMessage(msg.ID); err != nil {
			log.Printf("consul: failed to ack message %s: %v", msg.ID, err)
		}
	}

	return shutdown, nil
}

// caravanPayloadContainsID checks if a JSON body contains the caravan ID.
func caravanPayloadContainsID(body, caravanID string) bool {
	var payload store.CaravanNeedsFeedingPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return false
	}
	return payload.CaravanID == caravanID
}
