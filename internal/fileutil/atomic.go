// Package fileutil provides file system helpers.
package fileutil

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AtomicWrite writes data to path atomically via a temp file, fsync, and rename.
// The temp file is created with a unique name in the same directory as path so
// that concurrent callers each operate on their own temp file and the final
// rename is still atomic. On any failure the temp file is removed before
// returning the error. This ensures readers never see a partially-written file.
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("failed to sync %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to close %s: %w", path, err)
	}
	// Apply requested permissions after data is written and synced;
	// CreateTemp uses 0600, so the file stays restrictive until this point.
	if err := os.Chmod(tmp, perm); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to commit %s: %w", path, err)
	}
	return nil
}

// AtomicWriteJSON marshals v as indented JSON and writes it atomically to path.
func AtomicWriteJSON(path string, v any, perm os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON for %s: %w", path, err)
	}
	return AtomicWrite(path, data, perm)
}
