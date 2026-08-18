// Package stamp provides shared helpers for content-hash "stamp" sidecars:
// small JSON files that record the sha256 hash of tracked content at the
// time sol last wrote it, so a later three-way comparison (current content
// vs. current embedded content vs. the recorded stamp) can distinguish
// "untouched since extraction" (safe to auto-refresh transparently) from
// "operator customized" (never overwrite — autarch data is sacred) without
// relying on mtime or any other unreliable signal.
//
// This pattern first shipped in internal/guidelines (a single sphere-wide
// sidecar keyed by extract name) and generalizes directly to
// internal/workflow (one sidecar per auto-extracted workflow directory,
// keyed by file path within it) — both are "sol writes a starter copy of
// embedded content that an operator might later hand-edit" cases. This
// package holds the parts that generalize: the hash function and the atomic
// load/save of a flat `key -> hex sha256` JSON map. Each caller owns its own
// sidecar path and key scheme.
package stamp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/fileutil"
)

// Hash returns the hex-encoded sha256 of data.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Load reads a stamp sidecar JSON file (key -> hex sha256) at path. A
// missing file is not an error — it returns an empty map, which covers
// fresh installs and content that predates stamping.
func Load(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("failed to read stamp file %q: %w", path, err)
	}
	stamps := map[string]string{}
	if err := json.Unmarshal(data, &stamps); err != nil {
		return nil, fmt.Errorf("failed to parse stamp file %q: %w", path, err)
	}
	return stamps, nil
}

// Save writes a stamp sidecar JSON file atomically (temp file + rename, per
// fileutil conventions), creating its parent directory if needed.
func Save(path string, stamps map[string]string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", dir, err)
	}
	if err := fileutil.AtomicWriteJSON(path, stamps, 0o644); err != nil {
		return fmt.Errorf("failed to write stamp file %q: %w", path, err)
	}
	return nil
}
