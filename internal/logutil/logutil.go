// Package logutil provides shared log maintenance utilities for sol's
// service processes (prefect, sentinel, forge, consul, chronicle, ledger).
package logutil

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// DefaultMaxLogSize is the default threshold for log truncation (10MB).
// This matches the curated feed threshold in chronicle.
const DefaultMaxLogSize int64 = 10 * 1024 * 1024

// TruncateIfNeeded checks if the file at path exceeds maxBytes and, if so,
// rotates it in place, keeping the tail portion. Returns (truncated, tailStart,
// err). tailStart is the byte offset in the original file from which the
// retained content begins (0 if no truncation occurred).
//
// WARNING: This function uses atomic rename, which replaces the file's inode.
// Any process holding an open file descriptor (e.g. via O_APPEND) will
// continue writing to the old, now-unlinked inode — those writes are lost
// until the process reopens the file. Callers that coordinate with O_APPEND
// writers should arrange for the writer to reopen the file after truncation,
// or consider an in-place truncation approach (ftruncate + seek) instead.
//
// The algorithm: read → compute tail → flock → read any bytes appended since
// the initial read → write (tail + new bytes) to temp file → sync → chmod
// temp to match original permissions → rename temp over original → unlock.
// Capturing the new bytes after the flock closes the data-loss window: events
// appended between the initial ReadFile and the flock are preserved in the new
// file. A residual micro-window remains between the post-lock read and the
// rename, which is inherent in append-only files and acceptable in practice.
func TruncateIfNeeded(path string, maxBytes int64) (bool, int64, error) {
	// 1. Stat the file — also captures permissions for later restoration.
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, 0, nil
		}
		return false, 0, err
	}
	originalMode := info.Mode().Perm()

	// If size <= maxBytes, no-op.
	if info.Size() <= maxBytes {
		return false, 0, nil
	}

	// 2. Read the entire file content.
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, 0, nil
		}
		return false, 0, err
	}

	// File could have been truncated between stat and read.
	if int64(len(data)) <= maxBytes {
		return false, 0, nil
	}

	// 3. Compute the tail to keep — last 75% of the file, snapped to the
	// next newline boundary so we don't split a log line.
	tail := computeTail(data)

	// tailStart is the byte offset in the original file where the retained
	// content begins. Callers use this to reposition read cursors after rename.
	tailStart := int64(len(data)) - int64(len(tail))

	// 4. Acquire an advisory flock (LOCK_EX) on the file to serialize
	// concurrent truncation attempts.
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, 0, nil
		}
		return false, 0, err
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return false, 0, err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck

	// Re-check size under the lock — a concurrent caller may have already
	// truncated the file.
	info, err = f.Stat()
	if err != nil {
		return false, 0, err
	}
	if info.Size() <= maxBytes {
		return false, 0, nil
	}

	// 4a. Read any bytes appended to the file after our initial ReadFile call.
	// These bytes would be lost by the rename without this step. Seeking to
	// int64(len(data)) and reading to EOF captures everything written between
	// ReadFile and the flock acquisition, closing the data-loss window.
	if _, err := f.Seek(int64(len(data)), io.SeekStart); err != nil {
		return false, 0, err
	}
	newBytes, err := io.ReadAll(f)
	if err != nil {
		return false, 0, err
	}

	// 5. Build the payload: tail from the original read, plus any new bytes
	// appended during the window.
	payload := tail
	if len(newBytes) > 0 {
		payload = append(tail, newBytes...)
	}

	// 6. Write the payload to a temp file in the same directory, then atomically
	// rename it over the original. This prevents permanent data loss if the
	// process is killed between write and rename — before rename completes the
	// original is untouched; after rename completes the payload is durable.
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".logrotate-*.tmp")
	if err != nil {
		return false, 0, err
	}
	tmpPath := tmp.Name()
	// Clean up the temp file on any failure before the rename commits.
	committed := false
	defer func() {
		if !committed {
			tmp.Close()
			os.Remove(tmpPath) //nolint:errcheck
		}
	}()

	if _, err := tmp.Write(payload); err != nil {
		return false, 0, err
	}
	if err := tmp.Sync(); err != nil {
		return false, 0, err
	}
	if err := tmp.Close(); err != nil {
		return false, 0, err
	}

	// 7. Restore original file permissions on the temp file before rename.
	// os.CreateTemp uses 0600; without this, the rotated file would lose
	// its original permissions (e.g. 0644 → 0600), breaking monitoring
	// tools that depend on read access.
	if err := os.Chmod(tmpPath, originalMode); err != nil {
		return false, 0, err
	}

	// 8. Atomic rename — replace the original with the temp file.
	if err := os.Rename(tmpPath, path); err != nil {
		return false, 0, err
	}
	committed = true

	// 9. Flock released by deferred call (on the now-unlinked original fd).
	// 10. Return true and tailStart — truncation occurred.
	return true, tailStart, nil
}

// computeTail returns the last ~75% of data, snapped forward to the next
// newline boundary so we don't split a log line. If there are no newlines
// (single long line), the entire tail is kept as-is.
func computeTail(data []byte) []byte {
	// The cutoff point is at 25% of the file — everything after is kept.
	cutoff := len(data) / 4

	// Snap forward to the next newline so we don't start mid-line.
	idx := bytes.IndexByte(data[cutoff:], '\n')
	if idx >= 0 {
		// Skip past the newline itself — the tail starts on the next line.
		cutoff += idx + 1
	}
	// If no newline found after cutoff, keep everything from cutoff
	// (single long line / no newlines edge case).

	return data[cutoff:]
}
