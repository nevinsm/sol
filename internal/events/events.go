package events

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
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
	EventBrokerRefresh      = "broker_refresh"       // broker refreshed an account's OAuth token
	EventBrokerPatrol       = "broker_patrol"        // broker patrol completed
	EventBrokerHealthChange = "broker_health_change" // provider health state changed
	EventBrokerTokenExpiry  = "broker_token_expiry"  // broker detected a token approaching expiry or expired

	// Ledger events.
	EventLedgerStart  = "ledger_start"  // ledger OTLP receiver started
	EventLedgerStop   = "ledger_stop"   // ledger OTLP receiver stopped gracefully
	EventLedgerError  = "ledger_error"  // ledger processing error
	EventLedgerIngest = "ledger_ingest" // periodic ingestion summary

	// Chronicle events.
	EventChronicleStart   = "chronicle_start"   // chronicle process started
	EventChronicleStop    = "chronicle_stop"    // chronicle process stopped gracefully
	EventChroniclePatrol  = "chronicle_patrol"  // chronicle periodic processing summary
	EventChronicleError   = "chronicle_error"   // chronicle processing cycle error
	EventChronicleDropped = "chronicle_dropped" // chronicle dropped events (e.g. raw-feed rotation, oversize line)

	// Cross-domain observability events. See cross-domain.md Pattern 1 and
	// internal/softfail for the helper that emits these.
	EventSoftFailure = "soft_failure" // a non-fatal error was swallowed at a package boundary
)

// Logger handles event logging to the JSONL event feed.
type Logger struct {
	path string // path to the events JSONL file

	// rotationRetries counts how many times Log has detected an inode change
	// between OpenFile and the post-flock SameFile check (i.e. the chronicle
	// rotated the events file under the writer). Exposed for unit tests in
	// the same package via the unexported field — not part of the public API.
	rotationRetries atomic.Int64

	// testHookAfterOpen, if non-nil, is invoked inside Log between OpenFile
	// and Flock. Unit tests use it to deterministically simulate a rotation
	// occurring in that gap (the production race window). Production code
	// never sets this field.
	testHookAfterOpen func()
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
// This is best-effort — most errors are silently ignored (DEGRADE principle):
// events must never block primary operations.
//
// Rotation handling: the chronicle rotator (logutil.TruncateIfNeeded) atomically
// renames a new file over l.path. A writer that opened the old fd before the
// rename and acquired flock after it would write to the unlinked old inode
// and silently lose the event — and chronicle's chronicle_dropped accounting
// cannot detect bytes appended by third processes after a rename. After Flock
// we re-stat l.path and compare the inode to the open fd via os.SameFile. On
// mismatch we release the lock, close the fd, and retry once. If the second
// attempt also misses, we drop the event and emit a single stderr line so the
// loss is observable. This mirrors the discipline in events/reader.go's
// Reader.Follow, which is the canonical implementation of this pattern.
func (l *Logger) Log(event Event) {
	event.Timestamp = time.Now().UTC()
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	line := append(data, '\n')

	// Attempt twice. Two consecutive rotations between OpenFile and the
	// SameFile check is exceptionally unlikely (chronicle rotation cadence
	// is many milliseconds apart), so a single retry covers the realistic
	// case while bounding worst-case work.
	for range 2 {
		written, rotated := l.tryWrite(line)
		if written {
			return
		}
		if !rotated {
			// Some other failure (OpenFile, Flock, Stat, Write) —
			// match the original best-effort behavior and stop.
			return
		}
		l.rotationRetries.Add(1)
	}
	// Both attempts saw the file rotated under us. Drop the event and
	// surface a single stderr line so the loss is observable rather than
	// silent — this is the failure floor the writ explicitly requires.
	fmt.Fprintf(os.Stderr,
		"events: dropped event after rotation-race retries (type=%s source=%s)\n",
		event.Type, event.Source)
}

// tryWrite performs a single open + flock + write attempt. Returns:
//   - written=true on a successful append.
//   - rotated=true if the inode at l.path changed under our open fd
//     (caller may retry once).
//   - both false on any other failure (caller should drop silently to
//     preserve best-effort semantics).
func (l *Logger) tryWrite(line []byte) (written, rotated bool) {
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return false, false
	}
	defer f.Close()

	// Test hook: lets unit tests inject a rename in the gap between
	// OpenFile and Flock — the production race window. Production code
	// never sets this hook.
	if hook := l.testHookAfterOpen; hook != nil {
		hook()
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return false, false
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck

	// Detect inode replacement under our open fd. The chronicle rotator
	// (logutil.TruncateIfNeeded) atomically renames a new file over the
	// path; without this check we'd append to the unlinked old inode and
	// the bytes would vanish on close. See Reader.Follow in reader.go for
	// the canonical implementation of this discipline.
	pathInfo, pathErr := os.Stat(l.path)
	fdInfo, fdErr := f.Stat()
	if pathErr == nil && fdErr == nil && !os.SameFile(pathInfo, fdInfo) {
		return false, true
	}

	if _, err := f.Write(line); err != nil {
		fmt.Fprintf(os.Stderr, "events: failed to write event: %v\n", err)
		return false, false
	}
	return true, false
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
