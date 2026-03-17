package governor

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/fileutil"
)

// QueryDir returns the query protocol directory for a world's governor.
// $SOL_HOME/{world}/governor/.query/
func QueryDir(world string) string {
	return filepath.Join(config.Home(), world, "governor", ".query")
}

// PendingPath returns the path to the pending query file.
func PendingPath(world string) string {
	return filepath.Join(QueryDir(world), "pending.md")
}

// ResponsePath returns the path to the query response file.
func ResponsePath(world string) string {
	return filepath.Join(QueryDir(world), "response.md")
}

// LockPath returns the path to the query lock file.
func LockPath(world string) string {
	return filepath.Join(QueryDir(world), ".query.lock")
}

// QueryLock holds an exclusive flock on the world query protocol.
// Only one query may be in flight per world at a time.
type QueryLock struct {
	file *os.File
	path string
}

// AcquireQueryLock takes an exclusive advisory lock on the query protocol
// for the given world. It blocks until the lock is acquired or timeout elapses.
// Lock file: $SOL_HOME/{world}/governor/.query/.query.lock
// Returns an error if the lock cannot be acquired within timeout.
func AcquireQueryLock(world string, timeout time.Duration) (*QueryLock, error) {
	dir := QueryDir(world)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to acquire query lock for world %q: %w", world, err)
	}

	lockPath := LockPath(world)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire query lock for world %q: %w", world, err)
	}

	// Extract the raw fd before launching the goroutine.  f.Fd() must not be
	// called concurrently with f.Close() — doing so is a data race on the
	// os.File internals.  By capturing rawFd once here (single-threaded), the
	// goroutine can reference the int safely while the select timeout branch
	// calls f.Close() without any concurrent access to *os.File.
	rawFd := int(f.Fd())

	type result struct{ err error }
	ch := make(chan result, 1)
	go func() {
		ch <- result{syscall.Flock(rawFd, syscall.LOCK_EX)}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			f.Close()
			return nil, fmt.Errorf("failed to acquire query lock for world %q: %w", world, r.err)
		}
		return &QueryLock{file: f, path: lockPath}, nil
	case <-time.After(timeout):
		// Closing the file descriptor causes the blocked flock goroutine to
		// return EBADF, which is sent to the buffered channel and discarded.
		f.Close()
		return nil, fmt.Errorf("timed out waiting for query lock for world %q (another query may be in progress)", world)
	}
}

// Release releases the advisory lock and removes the lock file.
func (l *QueryLock) Release() {
	if l == nil || l.file == nil {
		return
	}
	syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	l.file.Close()
	os.Remove(l.path)
	l.file = nil
}

// generateNonce returns a random 16-hex-character nonce for a query.
func generateNonce() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if crypto/rand unavailable.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// WritePending writes a question to .query/pending.md using atomic write.
// The file embeds a unique nonce header so the governor can echo it back
// in the response, enabling stale-response detection.
// Returns the nonce that must be passed to ReadResponse.
func WritePending(world, question string) (string, error) {
	dir := QueryDir(world)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create query directory for world %q: %w", world, err)
	}

	nonce := generateNonce()
	content := fmt.Sprintf("QUERY-ID: %s\n\n%s", nonce, question)
	if err := fileutil.AtomicWrite(PendingPath(world), []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("failed to write query for world %q: %w", world, err)
	}
	return nonce, nil
}

// ReadResponse reads the query response from .query/response.md.
// It validates that the response begins with "QUERY-ID: <nonce>" so that
// stale responses from a previous (timed-out) query are ignored.
// Returns the response body and true if a matching response exists,
// or empty string and false if the file is absent or the nonce does not match.
func ReadResponse(world, nonce string) (string, bool, error) {
	data, err := os.ReadFile(ResponsePath(world))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to read query response for world %q: %w", world, err)
	}

	content := string(data)
	header := "QUERY-ID: " + nonce

	// Reject responses that don't carry the expected nonce (stale or unrelated).
	if !strings.HasPrefix(content, header) {
		return "", false, nil
	}

	// Strip the nonce header line and any immediately following blank line.
	body := content[len(header):]
	body = strings.TrimPrefix(body, "\n")
	body = strings.TrimPrefix(body, "\n")
	return body, true, nil
}

// ClearQuery removes both pending.md and response.md from the query directory.
func ClearQuery(world string) {
	os.Remove(PendingPath(world))
	os.Remove(ResponsePath(world))
}
