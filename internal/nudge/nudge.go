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
// Uses atomic write (temp file + hard link) for crash safety.
// The temp file is written and fsynced first, then atomically linked to
// the final .json path — no 0-byte or partial file is ever visible to Drain.
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

	// Write content to a unique temp file first. Drain skips .tmp files,
	// so this is invisible to concurrent readers.
	tmpFile, err := os.CreateTemp(dir, ".enqueue-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to write nudge for %q: %w", session, err)
	}
	tmpPath := tmpFile.Name()

	if _, wErr := tmpFile.Write(data); wErr != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write nudge for %q: %w", session, wErr)
	}
	if sErr := tmpFile.Sync(); sErr != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write nudge for %q: %w", session, sErr)
	}
	tmpFile.Close()

	// Atomically place the temp file at a unique .json path using hard link.
	// os.Link fails with EEXIST if the target already exists, providing the
	// same uniqueness guarantee as O_EXCL without creating a 0-byte file.
	ts := msg.CreatedAt.UnixMilli()
	const maxAttempts = 1000
	for i := 0; i < maxAttempts; i++ {
		var filename string
		if i == 0 {
			filename = fmt.Sprintf("%d.json", ts)
		} else {
			filename = fmt.Sprintf("%d_%d.json", ts, i)
		}
		path := filepath.Join(dir, filename)

		if lErr := os.Link(tmpPath, path); lErr != nil {
			if os.IsExist(lErr) {
				continue // slot taken, try next
			}
			os.Remove(tmpPath)
			return fmt.Errorf("failed to write nudge for %q: %w", session, lErr)
		}
		// Link succeeded — remove the temp file, keep the final .json.
		os.Remove(tmpPath)
		return nil
	}
	os.Remove(tmpPath)
	return fmt.Errorf("failed to write nudge for %q: too many timestamp collisions", session)
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

	// Phase 2: read, validate, and delete claimed files.
	var messages []Message
	for _, path := range claimed {
		data, err := os.ReadFile(path)
		if err != nil {
			continue // best-effort
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			fmt.Fprintf(os.Stderr, "nudge: corrupt message %s: %v\n", filepath.Base(path), err)
			os.Remove(path) // remove corrupt file after logging
			continue
		}
		os.Remove(path) // remove after successful parse
		if msg.isExpired(now) {
			fmt.Fprintf(os.Stderr, "nudge: discarding expired message %s (sender=%s, type=%s, age=%s)\n",
				filepath.Base(path), msg.Sender, msg.Type, now.Sub(msg.CreatedAt).Truncate(time.Second))
			continue
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

		// Remove stale .tmp files left by crashed Enqueue calls.
		if strings.HasSuffix(name, ".tmp") {
			info, err := e.Info()
			if err != nil {
				continue
			}
			if now.Sub(info.ModTime()) > claimedOrphanAge {
				os.Remove(path)
			}
			continue
		}

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

// Deliver sends a nudge message to a session using enqueue-first routing.
//
// 1. Always enqueues the message first for durability.
// 2. Waits up to 3 seconds for the session to be idle (WaitForIdle).
// 3. If idle: also injects directly via NudgeSession for immediate delivery.
// 4. If enqueue fails: falls back to NudgeSession anyway (last resort).
//
// The queue serves as the durability layer: even if the session crashes after
// direct injection but before processing the message, it remains in the queue
// for drain at the next turn boundary. This may result in duplicate delivery
// (once via injection, once via drain), which is acceptable for nudge messages
// — they are informational notifications, not transactional operations.
func Deliver(sessionName string, msg Message) error {
	// Ensure message has a timestamp.
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}

	// Always enqueue first — the queue is the durability layer.
	if qErr := Enqueue(sessionName, msg); qErr != nil {
		// Enqueue failed — last resort: try NudgeSession directly.
		mgr := session.New()
		notification := formatNotification(msg)
		return mgr.NudgeSession(sessionName, notification)
	}

	// Best-effort direct injection for immediate delivery if session is idle.
	mgr := session.New()
	if err := mgr.WaitForIdle(sessionName, deliverIdleTimeout); err == nil {
		notification := formatNotification(msg)
		mgr.NudgeSession(sessionName, notification) // best-effort; queue guarantees delivery
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

