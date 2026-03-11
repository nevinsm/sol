// Package logutil provides shared log maintenance utilities for sol's
// service processes (prefect, sentinel, forge, consul, chronicle, ledger).
package logutil

import (
	"bytes"
	"errors"
	"os"
	"syscall"
)

// DefaultMaxLogSize is the default threshold for log truncation (10MB).
// This matches the curated feed threshold in chronicle.
const DefaultMaxLogSize int64 = 10 * 1024 * 1024

// TruncateIfNeeded checks if the file at path exceeds maxBytes and, if so,
// truncates it in place, keeping the tail portion. Returns true if truncation
// occurred. Safe to call on files held open by other processes with O_APPEND.
//
// The algorithm is copytruncate-style: read → compute tail → flock → truncate
// → write back → unlock. All daemon logs are opened with O_APPEND, so after
// truncation the daemon's next write atomically seeks to end-of-file and
// appends. There is a small window where log lines written between read and
// truncate are lost — the standard copytruncate tradeoff, acceptable for
// operational log data.
func TruncateIfNeeded(path string, maxBytes int64) (bool, error) {
	// 1. Stat the file.
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	// If size <= maxBytes, no-op.
	if info.Size() <= maxBytes {
		return false, nil
	}

	// 2. Read the entire file content.
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	// File could have been truncated between stat and read.
	if int64(len(data)) <= maxBytes {
		return false, nil
	}

	// 3. Compute the tail to keep — last 75% of the file, snapped to the
	// next newline boundary so we don't split a log line.
	tail := computeTail(data)

	// 4. Acquire an advisory flock (LOCK_EX) on the file to serialize
	// concurrent truncation attempts.
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return false, err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck

	// Re-check size under the lock — a concurrent caller may have already
	// truncated the file.
	info, err = f.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() <= maxBytes {
		return false, nil
	}

	// 5. Truncate the file to 0.
	if err := f.Truncate(0); err != nil {
		return false, err
	}

	// Seek to beginning so the write lands at offset 0.
	if _, err := f.Seek(0, 0); err != nil {
		return false, err
	}

	// 6. Write the tail back.
	if _, err := f.Write(tail); err != nil {
		return false, err
	}

	// 7. Flock released by deferred call.
	// 8. Return true — truncation occurred.
	return true, nil
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
