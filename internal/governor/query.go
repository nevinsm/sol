package governor

import (
	"fmt"
	"os"
	"path/filepath"

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

// WritePending writes a question to .query/pending.md using atomic write.
func WritePending(world, question string) error {
	dir := QueryDir(world)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create query directory for world %q: %w", world, err)
	}

	if err := fileutil.AtomicWrite(PendingPath(world), []byte(question), 0o644); err != nil {
		return fmt.Errorf("failed to write query for world %q: %w", world, err)
	}
	return nil
}

// ReadResponse reads the query response from .query/response.md.
// Returns the content and true if the file exists, or empty string and false if not.
func ReadResponse(world string) (string, bool, error) {
	data, err := os.ReadFile(ResponsePath(world))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to read query response for world %q: %w", world, err)
	}
	return string(data), true, nil
}

// ClearQuery removes both pending.md and response.md from the query directory.
func ClearQuery(world string) {
	os.Remove(PendingPath(world))
	os.Remove(ResponsePath(world))
}
