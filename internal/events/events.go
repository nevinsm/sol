package events

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Event represents a single system event.
type Event struct {
	Timestamp  time.Time `json:"ts"`
	Source     string    `json:"source"`     // "gt", agent ID, or component name
	Type      string    `json:"type"`       // event type (see constants)
	Actor     string    `json:"actor"`      // who triggered the event
	Visibility string   `json:"visibility"` // "feed", "audit", or "both"
	Payload   any       `json:"payload"`    // event-specific data
}

// Event type constants.
const (
	EventSling        = "sling"         // work dispatched to agent
	EventDone         = "done"          // agent completed work
	EventMergeQueued  = "merge_queued"  // merge request created
	EventMergeClaimed = "merge_claimed" // refinery claimed MR
	EventMerged       = "merged"        // merge successful
	EventMergeFailed  = "merge_failed"  // merge failed
	EventSessionStart = "session_start" // tmux session started
	EventSessionStop  = "session_stop"  // tmux session stopped
	EventRespawn      = "respawn"       // supervisor respawned agent
	EventMassDeath    = "mass_death"    // mass death detected
	EventDegraded     = "degraded"      // entered degraded mode
	EventRecovered    = "recovered"     // exited degraded mode
	EventPatrol       = "patrol"        // witness patrol completed
	EventStalled      = "stalled"       // agent detected as stalled
	EventMailSent     = "mail_sent"     // message sent
)

// Logger handles event logging to the JSONL event feed.
type Logger struct {
	path string // path to the events JSONL file
}

// NewLogger creates an event logger.
// The events file is at $GT_HOME/.events.jsonl.
// Creates the file if it doesn't exist.
func NewLogger(gtHome string) *Logger {
	return &Logger{
		path: filepath.Join(gtHome, ".events.jsonl"),
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
	f.Write(append(data, '\n'))
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
