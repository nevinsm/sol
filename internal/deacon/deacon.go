package deacon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/escalation"
	"github.com/nevinsm/gt/internal/events"
	"github.com/nevinsm/gt/internal/hook"
	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/store"
)

// Config holds deacon patrol configuration.
type Config struct {
	PatrolInterval    time.Duration // time between patrols (default: 5 minutes)
	StaleHookTimeout  time.Duration // how long a hook can be stale (default: 1 hour)
	HeartbeatDir      string        // path to heartbeat directory (default: $GT_HOME/deacon)
	GTHome            string        // $GT_HOME path
	SourceRepo        string        // path to source git repo (for dispatch)
	EscalationWebhook string        // webhook URL for escalation routing (optional)
}

// DefaultConfig returns a Config with default values.
func DefaultConfig() Config {
	return Config{
		PatrolInterval:   5 * time.Minute,
		StaleHookTimeout: 1 * time.Hour,
	}
}

// TownStore is the subset of store.Store used by the deacon.
type TownStore interface {
	// Agents
	ListAgents(rig string, state string) ([]store.Agent, error)
	UpdateAgentState(id, state, hookItem string) error
	GetAgent(id string) (*store.Agent, error)
	CreateAgent(name, rig, role string) (string, error)

	// Convoys
	ListConvoys(status string) ([]store.Convoy, error)
	CheckConvoyReadiness(convoyID string, rigOpener func(string) (*store.Store, error)) ([]store.ConvoyItemStatus, error)

	// Escalations
	CreateEscalation(severity, source, description string) (string, error)
	CountOpen() (int, error)

	// Messages
	PendingProtocol(recipient, protoType string) ([]store.Message, error)
	AckMessage(id string) error
	SendMessage(sender, recipient, subject, body string, priority int, msgType string) (string, error)
	SendProtocolMessage(sender, recipient, protoType string, payload any) (string, error)
}

// SessionChecker is the subset of session.Manager used by the deacon.
type SessionChecker interface {
	Exists(name string) bool
	List() ([]session.SessionInfo, error)
}

// RigOpener opens a rig store by name.
type RigOpener func(rig string) (*store.Store, error)

// Deacon is the town-level patrol process.
type Deacon struct {
	config     Config
	townStore  TownStore
	sessions   SessionChecker
	logger     *events.Logger
	router     *escalation.Router
	rigOpener  RigOpener

	patrolCount int
}

// New creates a new Deacon.
func New(cfg Config, townStore TownStore, sessions SessionChecker,
	router *escalation.Router, logger *events.Logger) *Deacon {
	return &Deacon{
		config:    cfg,
		townStore: townStore,
		sessions:  sessions,
		logger:    logger,
		router:    router,
		rigOpener: store.OpenRig,
	}
}

// SetRigOpener sets a custom rig opener for testing.
func (d *Deacon) SetRigOpener(opener RigOpener) {
	d.rigOpener = opener
}

// Register creates or updates the deacon's agent record.
// Agent ID: "town/deacon", role: "deacon", state: "working".
func (d *Deacon) Register() error {
	_, err := d.townStore.GetAgent("town/deacon")
	if err == nil {
		return nil // already registered
	}
	_, err = d.townStore.CreateAgent("deacon", "town", "deacon")
	return err
}

// Run starts the deacon patrol loop. Blocks until ctx is cancelled.
// 1. Register as agent (role="deacon", rig="town")
// 2. Write initial heartbeat
// 3. Loop: patrol -> sleep -> repeat
//
// On context cancellation:
// - Write final heartbeat with status="stopping"
// - Set agent state to "idle"
func (d *Deacon) Run(ctx context.Context) error {
	if err := d.Register(); err != nil {
		return fmt.Errorf("failed to register deacon: %w", err)
	}

	if err := d.townStore.UpdateAgentState("town/deacon", "working", ""); err != nil {
		return fmt.Errorf("failed to set deacon working: %w", err)
	}

	// Patrol immediately.
	d.Patrol()

	ticker := time.NewTicker(d.config.PatrolInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Write final heartbeat.
			openEsc, _ := d.townStore.CountOpen()
			_ = WriteHeartbeat(d.config.GTHome, &Heartbeat{
				Timestamp:   time.Now().UTC(),
				PatrolCount: d.patrolCount,
				Status:      "stopping",
				Escalations: openEsc,
			})
			_ = d.townStore.UpdateAgentState("town/deacon", "idle", "")
			return nil

		case <-ticker.C:
			d.Patrol()
		}
	}
}

// Patrol runs a single patrol cycle:
// 1. Write heartbeat
// 2. Recover stale hooks
// 3. Feed stranded convoys
// 4. Process lifecycle requests
// 5. Emit patrol event
//
// Errors in individual patrol steps are logged but do not stop the
// patrol cycle. The deacon continues to the next step (DEGRADE).
func (d *Deacon) Patrol() error {
	d.patrolCount++

	var staleHooks, convoyFeeds int
	var shutdown bool

	// 1. Recover stale hooks.
	recovered, err := d.recoverStaleHooks()
	if err != nil {
		log.Printf("deacon: stale hook recovery error: %v", err)
	}
	staleHooks = recovered

	// 2. Feed stranded convoys.
	fed, err := d.feedStrandedConvoys()
	if err != nil {
		log.Printf("deacon: convoy feeding error: %v", err)
	}
	convoyFeeds = fed

	// 3. Process lifecycle requests.
	shutdown, err = d.processLifecycleRequests()
	if err != nil {
		log.Printf("deacon: lifecycle request error: %v", err)
	}

	// 4. Count open escalations.
	openEsc, err := d.townStore.CountOpen()
	if err != nil {
		log.Printf("deacon: count escalations error: %v", err)
	}

	// 5. Write heartbeat.
	status := "running"
	if shutdown {
		status = "stopping"
	}
	_ = WriteHeartbeat(d.config.GTHome, &Heartbeat{
		Timestamp:   time.Now().UTC(),
		PatrolCount: d.patrolCount,
		Status:      status,
		StaleHooks:  staleHooks,
		ConvoyFeeds: convoyFeeds,
		Escalations: openEsc,
	})

	// 6. Emit patrol event.
	if d.logger != nil {
		d.logger.Emit(events.EventDeaconPatrol, "town/deacon", "town/deacon", "feed",
			map[string]any{
				"patrol_count": fmt.Sprintf("%d", d.patrolCount),
				"stale_hooks":  fmt.Sprintf("%d", staleHooks),
				"convoy_feeds": fmt.Sprintf("%d", convoyFeeds),
				"escalations":  fmt.Sprintf("%d", openEsc),
			})
	}

	// 7. Log patrol summary.
	log.Printf("[%s] Patrol #%d: %d stale hook%s recovered, %d convoy feed%s, %d open escalation%s",
		time.Now().UTC().Format(time.RFC3339),
		d.patrolCount,
		staleHooks, plural(staleHooks),
		convoyFeeds, plural(convoyFeeds),
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

// recoverStaleHooks finds and recovers stale hooks across all rigs.
// For each stale hook:
// 1. Log the recovery
// 2. Clear the hook file
// 3. Update work item status -> "open", clear assignee
// 4. Update agent state -> "idle", clear hook_item
// 5. Emit event
//
// Returns the number of hooks recovered.
func (d *Deacon) recoverStaleHooks() (int, error) {
	// List all agents with state "working".
	agents, err := d.townStore.ListAgents("", "working")
	if err != nil {
		return 0, fmt.Errorf("failed to list working agents: %w", err)
	}

	var recovered int
	for _, agent := range agents {
		// Skip non-polecat agents (don't recover witness/refinery/deacon).
		if agent.Role != "polecat" {
			continue
		}

		// Skip agents without hooked work.
		if agent.HookItem == "" {
			continue
		}

		// Check if session is alive.
		sessionName := dispatch.SessionName(agent.Rig, agent.Name)
		if d.sessions.Exists(sessionName) {
			continue // session alive, not stale
		}

		// Check if the agent's updated_at is older than StaleHookTimeout.
		if time.Since(agent.UpdatedAt) < d.config.StaleHookTimeout {
			continue // too recent, might still be starting
		}

		// This hook is stale — recover it.
		if err := d.recoverOneHook(agent); err != nil {
			log.Printf("deacon: failed to recover stale hook for %s: %v", agent.ID, err)
			continue // DEGRADE: skip this agent, try the next
		}
		recovered++
	}

	return recovered, nil
}

// recoverOneHook recovers a single stale hook.
func (d *Deacon) recoverOneHook(agent store.Agent) error {
	log.Printf("deacon: recovering stale hook for %s (work item: %s)", agent.ID, agent.HookItem)

	// 1. Open the rig store to update the work item.
	rigStore, err := d.rigOpener(agent.Rig)
	if err != nil {
		return fmt.Errorf("failed to open rig %q: %w", agent.Rig, err)
	}
	defer rigStore.Close()

	// 2. Update work item: status -> "open", clear assignee.
	if err := rigStore.UpdateWorkItem(agent.HookItem, store.WorkItemUpdates{
		Status:   "open",
		Assignee: "-", // "-" clears assignee
	}); err != nil {
		return fmt.Errorf("failed to update work item %q: %w", agent.HookItem, err)
	}

	// 3. Update agent state -> "idle", clear hook_item.
	if err := d.townStore.UpdateAgentState(agent.ID, "idle", ""); err != nil {
		return fmt.Errorf("failed to update agent %q state: %w", agent.ID, err)
	}

	// 4. Clear the hook file.
	if err := hook.Clear(agent.Rig, agent.Name); err != nil {
		return fmt.Errorf("failed to clear hook for %q: %w", agent.ID, err)
	}

	// 5. Emit event.
	if d.logger != nil {
		d.logger.Emit(events.EventDeaconStaleHook, "town/deacon", "town/deacon", "both",
			map[string]any{
				"agent_id":     agent.ID,
				"work_item_id": agent.HookItem,
				"rig":          agent.Rig,
			})
	}

	return nil
}

// feedStrandedConvoys checks all open convoys for ready, undispatched items.
// For each stranded convoy:
// 1. Check readiness of all items
// 2. For items that are ready and status="open":
//   - Send CONVOY_NEEDS_FEEDING protocol message to "operator"
//
// 3. Emit event
//
// The deacon does NOT dispatch directly — it sends a protocol message
// that the operator (or automation) can act on.
//
// Returns the number of convoys with ready items.
func (d *Deacon) feedStrandedConvoys() (int, error) {
	convoys, err := d.townStore.ListConvoys("open")
	if err != nil {
		return 0, fmt.Errorf("failed to list open convoys: %w", err)
	}

	var fed int
	for _, convoy := range convoys {
		statuses, err := d.townStore.CheckConvoyReadiness(convoy.ID, func(rig string) (*store.Store, error) {
			return d.rigOpener(rig)
		})
		if err != nil {
			log.Printf("deacon: failed to check convoy %s readiness: %v", convoy.ID, err)
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

		// Check for existing pending CONVOY_NEEDS_FEEDING message for this convoy
		// to prevent duplicates.
		pending, err := d.townStore.PendingProtocol("operator", store.ProtoConvoyNeedsFeeding)
		if err != nil {
			log.Printf("deacon: failed to check pending messages: %v", err)
			continue
		}

		alreadyPending := false
		for _, msg := range pending {
			if strings.Contains(msg.Body, convoy.ID) {
				alreadyPending = true
				break
			}
		}
		if alreadyPending {
			continue
		}

		// Send CONVOY_NEEDS_FEEDING protocol message.
		if _, err := d.townStore.SendProtocolMessage(
			"town/deacon", "operator",
			store.ProtoConvoyNeedsFeeding,
			store.ConvoyNeedsFeedingPayload{
				ConvoyID:   convoy.ID,
				ReadyCount: readyCount,
			},
		); err != nil {
			log.Printf("deacon: failed to send convoy feed message for %s: %v", convoy.ID, err)
			continue
		}

		// Emit event.
		if d.logger != nil {
			d.logger.Emit(events.EventDeaconConvoyFeed, "town/deacon", "town/deacon", "both",
				map[string]any{
					"convoy_id":   convoy.ID,
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
func (d *Deacon) processLifecycleRequests() (shutdown bool, err error) {
	msgs, err := d.townStore.PendingProtocol("town/deacon", "")
	if err != nil {
		return false, fmt.Errorf("failed to read lifecycle messages: %w", err)
	}

	for _, msg := range msgs {
		switch msg.Subject {
		case "SHUTDOWN":
			shutdown = true
			log.Printf("deacon: shutdown requested via message %s", msg.ID)
		case "CYCLE":
			log.Printf("deacon: cycle requested via message %s", msg.ID)
			// No action needed — the patrol just happened.
		default:
			// Unknown message — ack and ignore.
		}

		// Acknowledge the message.
		if err := d.townStore.AckMessage(msg.ID); err != nil {
			log.Printf("deacon: failed to ack message %s: %v", msg.ID, err)
		}
	}

	return shutdown, nil
}

// convoyPayloadContainsID checks if a JSON body contains the convoy ID.
func convoyPayloadContainsID(body, convoyID string) bool {
	var payload store.ConvoyNeedsFeedingPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return false
	}
	return payload.ConvoyID == convoyID
}
