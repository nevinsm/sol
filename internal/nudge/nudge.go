package nudge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/session"
)

// Default TTLs for message priorities.
const (
	NormalTTL = 30 * time.Minute
	UrgentTTL = 2 * time.Hour
)

// claimedOrphanAge is how long a .claimed file can exist before
// being considered orphaned and requeued.
const claimedOrphanAge = 5 * time.Minute

// Message represents a nudge queue message.
type Message struct {
	Sender    string    `json:"sender"`
	Type      string    `json:"type"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	Priority  string    `json:"priority"`
	CreatedAt time.Time `json:"created_at"`
	TTL       string    `json:"ttl"`
}

// ttlDuration parses the TTL field or returns a default based on priority.
func (m Message) ttlDuration() time.Duration {
	if m.TTL != "" {
		if d, err := time.ParseDuration(m.TTL); err == nil {
			return d
		}
	}
	if m.Priority == "urgent" {
		return UrgentTTL
	}
	return NormalTTL
}

// isExpired returns true if the message has exceeded its TTL.
func (m Message) isExpired(now time.Time) bool {
	return now.After(m.CreatedAt.Add(m.ttlDuration()))
}

// Enqueue writes a message to the session's nudge queue.
// Uses atomic write (temp file + rename) for crash safety.
func Enqueue(session string, msg Message) error {
	dir := config.NudgeQueueDir(session)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create nudge queue dir for %q: %w", session, err)
	}

	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal nudge message for %q: %w", session, err)
	}

	ts := msg.CreatedAt.UnixMilli()

	// Use O_CREATE|O_EXCL to atomically claim a unique filename,
	// avoiding the TOCTOU race between fileExists and AtomicWrite.
	const maxAttempts = 1000
	for i := 0; i < maxAttempts; i++ {
		var filename string
		if i == 0 {
			filename = fmt.Sprintf("%d.json", ts)
		} else {
			filename = fmt.Sprintf("%d_%d.json", ts, i)
		}
		path := filepath.Join(dir, filename)

		// Atomically claim the filename with O_EXCL (prevents TOCTOU race).
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			if os.IsExist(err) {
				continue // slot taken, try next
			}
			return fmt.Errorf("failed to write nudge for %q: %w", session, err)
		}
		f.Close()

		// Write content to a temp file, then rename over the claimed path.
		// This ensures Drain never sees a partial JSON file.
		tmpPath := path + ".tmp"
		if wErr := writeAndSync(tmpPath, data); wErr != nil {
			os.Remove(path)    // release claimed slot
			os.Remove(tmpPath) // clean up temp file
			return fmt.Errorf("failed to write nudge for %q: %w", session, wErr)
		}
		if rErr := os.Rename(tmpPath, path); rErr != nil {
			os.Remove(path)    // release claimed slot
			os.Remove(tmpPath) // clean up temp file
			return fmt.Errorf("failed to write nudge for %q: %w", session, rErr)
		}
		return nil
	}
	return fmt.Errorf("failed to write nudge for %q: too many timestamp collisions", session)
}

// writeAndSync writes data to a new file and fsyncs it for durability.
func writeAndSync(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Drain claims all pending messages for a session, reads them,
// deletes the claimed files, and returns messages sorted by timestamp.
// Expired messages are silently discarded.
func Drain(session string) ([]Message, error) {
	dir := config.NudgeQueueDir(session)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read nudge queue for %q: %w", session, err)
	}

	now := time.Now().UTC()
	var claimed []string

	// Phase 1: claim pending files by renaming to .claimed.
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".tmp") {
			continue
		}
		src := filepath.Join(dir, name)
		dst := src + ".claimed"
		if err := os.Rename(src, dst); err != nil {
			continue // another process may have claimed it
		}
		claimed = append(claimed, dst)
	}

	// Phase 2: read and delete claimed files.
	var messages []Message
	for _, path := range claimed {
		data, err := os.ReadFile(path)
		if err != nil {
			continue // best-effort
		}
		os.Remove(path) // delete after reading

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.isExpired(now) {
			continue // discard expired
		}
		messages = append(messages, msg)
	}

	// Sort by timestamp (FIFO).
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].CreatedAt.Before(messages[j].CreatedAt)
	})

	return messages, nil
}

// Peek returns the count of pending messages without claiming them.
func Peek(session string) (int, error) {
	dir := config.NudgeQueueDir(session)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to peek nudge queue for %q: %w", session, err)
	}

	count := 0
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".tmp") {
			count++
		}
	}
	return count, nil
}

// List reads all pending messages without claiming them (non-destructive).
// Messages are returned sorted by timestamp (FIFO). Expired messages are excluded.
func List(session string) ([]Message, error) {
	dir := config.NudgeQueueDir(session)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read nudge queue for %q: %w", session, err)
	}

	now := time.Now().UTC()
	var messages []Message

	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".claimed") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.isExpired(now) {
			continue
		}
		messages = append(messages, msg)
	}

	sort.Slice(messages, func(i, j int) bool {
		return messages[i].CreatedAt.Before(messages[j].CreatedAt)
	})

	return messages, nil
}

// Cleanup requeues orphaned .claimed files older than 5 minutes
// and deletes expired pending messages.
func Cleanup(session string) error {
	dir := config.NudgeQueueDir(session)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to clean nudge queue for %q: %w", session, err)
	}

	now := time.Now().UTC()

	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(dir, name)

		// Requeue orphaned .claimed files.
		if strings.HasSuffix(name, ".json.claimed") {
			info, err := e.Info()
			if err != nil {
				continue
			}
			if now.Sub(info.ModTime()) > claimedOrphanAge {
				// Requeue by removing .claimed suffix.
				dst := strings.TrimSuffix(path, ".claimed")
				os.Rename(path, dst) // best-effort
			}
			continue
		}

		// Delete expired pending messages.
		if strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".tmp") {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var msg Message
			if err := json.Unmarshal(data, &msg); err != nil {
				os.Remove(path) // remove corrupt files
				continue
			}
			if msg.isExpired(now) {
				os.Remove(path)
			}
		}
	}
	return nil
}

// deliverIdleTimeout is how long Deliver waits for a session to become idle
// before falling back to queue-based delivery.
const deliverIdleTimeout = 3 * time.Second

// Deliver sends a nudge message to a session using smart idle-or-queue routing.
//
// 1. Waits up to 3 seconds for the session to be idle (WaitForIdle).
// 2. If idle: formats the message and sends it via NudgeSession.
// 3. If busy or not running: enqueues the message for later drain at turn boundary.
// 4. If enqueue fails: falls back to NudgeSession anyway (last resort).
//
// Messages are always enqueued as a fallback, even if the session doesn't
// currently exist — they will be drained when the session starts.
func Deliver(sessionName string, msg Message) error {
	// Ensure message has a timestamp.
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}

	// Try to wait for idle prompt.
	mgr := session.New()
	err := mgr.WaitForIdle(sessionName, deliverIdleTimeout)
	if err == nil {
		// Session is idle — deliver directly via NudgeSession.
		notification := formatNotification(msg)
		if injectErr := mgr.NudgeSession(sessionName, notification); injectErr != nil {
			// Session died between idle check and nudge — queue for later drain.
			return Enqueue(sessionName, msg)
		}
		return nil
	}

	// Session is busy, not running, or WaitForIdle failed — queue for later drain.
	if qErr := Enqueue(sessionName, msg); qErr != nil {
		// Enqueue failed — last resort: try NudgeSession anyway.
		notification := formatNotification(msg)
		return mgr.NudgeSession(sessionName, notification)
	}

	return nil
}

// formatNotification formats a Message into a human-readable notification string
// suitable for injection into a Claude Code session.
func formatNotification(msg Message) string {
	var header string
	if msg.Subject != "" {
		header = fmt.Sprintf("[%s] %s: %s", msg.Type, msg.Sender, msg.Subject)
	} else {
		header = fmt.Sprintf("[%s] %s", msg.Type, msg.Sender)
	}
	if msg.Body != "" {
		return header + "\n" + msg.Body
	}
	return header
}

