package events

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Event represents a single system event.
type Event struct {
	Timestamp  time.Time `json:"ts"`
	Source     string    `json:"source"`     // "sol", agent ID, or component name
	Type      string    `json:"type"`       // event type (see constants)
	Actor     string    `json:"actor"`      // who triggered the event
	Visibility string   `json:"visibility"` // "feed", "audit", or "both"
	Payload   any       `json:"payload"`    // event-specific data
}

// Event type constants.
const (
	EventCast         = "cast"          // work dispatched to agent
	EventResolve      = "resolve"       // agent completed work
	EventMergeQueued  = "merge_queued"  // merge request created (emitted by forge CLI toolbox)
	EventMergeClaimed = "merge_claimed" // forge claimed MR (emitted by forge CLI toolbox)
	EventMerged       = "merged"        // merge successful (emitted by forge CLI toolbox)
	EventMergeFailed  = "merge_failed"  // merge failed (emitted by forge CLI toolbox)
	EventSessionStart = "session_start" // tmux session started
	EventSessionStop  = "session_stop"  // tmux session stopped
	EventRespawn      = "respawn"       // prefect respawned agent
	EventMassDeath    = "mass_death"    // mass death detected
	EventDegraded     = "degraded"      // entered degraded mode
	EventRecovered    = "recovered"     // exited degraded mode
	EventPatrol       = "patrol"        // sentinel patrol completed
	EventStalled      = "stalled"       // agent detected as stalled
	EventAssess       = "assess"        // AI assessment performed
	EventNudge        = "nudge"         // nudge injected into agent session
	EventMailSent     = "mail_sent"     // message sent (reserved for Loop 5 Consul)

	// Loop 4 events.
	EventWorkflowInstantiate = "workflow_instantiate" // workflow instantiated for agent
	EventWorkflowAdvance     = "workflow_advance"     // workflow step advanced
	EventWorkflowComplete    = "workflow_complete"    // workflow completed all steps
	EventWorkflowFail        = "workflow_fail"        // workflow failed due to step failure
	EventCaravanCreated       = "caravan_created"       // caravan created
	EventCaravanLaunched      = "caravan_launched"      // caravan items dispatched
	EventCaravanClosed        = "caravan_closed"        // caravan auto-closed

	// Loop 5 events.
	EventEscalationCreated  = "escalation_created"  // escalation created
	EventEscalationAcked    = "escalation_acked"    // escalation acknowledged
	EventEscalationResolved = "escalation_resolved" // escalation resolved
	EventHandoff            = "handoff"              // agent handed off session
	EventConsulPatrol       = "consul_patrol"        // consul patrol completed
	EventConsulStaleTether  = "consul_stale_tether"  // stale tether recovered
	EventConsulCaravanFeed     = "consul_caravan_feed"     // consul auto-dispatched caravan items
	EventConsulCaravanDispatch = "consul_caravan_dispatch" // individual item dispatched by consul
	EventConsulEscalationAlert = "consul_escalation_alert" // escalation buildup alert fired
	EventConsulEscRenotified   = "consul_esc_renotified"   // aging escalation re-notified

	// Tether events.
	EventTether       = "tether"        // agent tethered to writ
	EventUntether     = "untether"      // agent untethered from writ
	EventWritActivate = "writ_activate" // active writ switched for persistent agent

	// Sentinel lifecycle events.
	EventReap           = "reap"            // idle agent reaped
	EventOrphanCleanup  = "orphan_cleanup"  // orphaned resource cleaned up
	EventRecast         = "recast"          // failed MR auto-recast by sentinel

	// Quota events.
	EventQuotaScan   = "quota_scan"   // sentinel scanned sessions for rate limits
	EventQuotaRotate = "quota_rotate" // credential rotated to a different account
	EventQuotaPause  = "quota_pause"  // agent paused due to no available accounts

	// Forge events.
	EventForgePatrol = "forge_patrol" // forge patrol cycle completed
	EventForgeRebase = "forge_rebase" // forge auto-rebased a branch before merge

	// Broker events.
	EventBrokerRefresh      = "broker_refresh"      // broker refreshed an account's OAuth token
	EventBrokerPatrol       = "broker_patrol"       // broker patrol completed
	EventBrokerHealthChange = "broker_health_change" // provider health state changed

	// Ledger events.
	EventLedgerStart  = "ledger_start"  // ledger OTLP receiver started
	EventLedgerStop   = "ledger_stop"   // ledger OTLP receiver stopped gracefully
	EventLedgerError  = "ledger_error"  // ledger processing error
	EventLedgerIngest = "ledger_ingest" // periodic ingestion summary

	// Chronicle events.
	EventChronicleStart  = "chronicle_start"  // chronicle process started
	EventChronicleStop   = "chronicle_stop"   // chronicle process stopped gracefully
	EventChroniclePatrol = "chronicle_patrol"  // chronicle periodic processing summary
)

// Logger handles event logging to the JSONL event feed.
type Logger struct {
	path string // path to the events JSONL file
}

// NewLogger creates an event logger.
// The events file is at $SOL_HOME/.events.jsonl.
// Creates the file if it doesn't exist.
func NewLogger(solHome string) *Logger {
	return &Logger{
		path: filepath.Join(solHome, ".events.jsonl"),
	}
}

// Log writes an event to the JSONL file.
// Uses cross-process flock for safe concurrent appending.
// This is best-effort — errors are silently ignored (DEGRADE principle).
// Events must never block primary operations.
func (l *Logger) Log(event Event) {
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return // best-effort, silent failure
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	event.Timestamp = time.Now().UTC()
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "events: failed to write event: %v\n", err)
	}
}

// Emit is a convenience method for logging common events.
// Creates the Event struct and calls Log.
func (l *Logger) Emit(eventType, source, actor, visibility string, payload any) {
	l.Log(Event{
		Source:     source,
		Type:       eventType,
		Actor:      actor,
		Visibility: visibility,
		Payload:    payload,
	})
}
