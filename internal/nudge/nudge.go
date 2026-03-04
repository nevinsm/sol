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

	ts := msg.CreatedAt.UnixMilli()
	filename := fmt.Sprintf("%d.json", ts)
	path := filepath.Join(dir, filename)

	// Avoid collisions — append a counter if file exists.
	for i := 1; fileExists(path); i++ {
		filename = fmt.Sprintf("%d_%d.json", ts, i)
		path = filepath.Join(dir, filename)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal nudge message for %q: %w", session, err)
	}

	// Atomic write: temp file → sync → rename.
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("failed to write nudge for %q: %w", session, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("failed to write nudge for %q: %w", session, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("failed to sync nudge for %q: %w", session, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to close nudge for %q: %w", session, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to commit nudge for %q: %w", session, err)
	}
	return nil
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

// Poke injects a short message into the target session to trigger a turn boundary,
// causing the UserPromptSubmit hook to fire and drain any pending nudge messages.
// Best-effort: returns nil if session doesn't exist or inject fails.
func Poke(sessionName string) error {
	mgr := session.New()
	if !mgr.Exists(sessionName) {
		return nil
	}
	return mgr.Inject(sessionName, "check nudge queue", true)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
