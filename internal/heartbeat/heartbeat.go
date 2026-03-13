// Package heartbeat provides shared I/O helpers for daemon heartbeat files.
//
// Each daemon keeps its own heartbeat struct (fields differ per service).
// This package provides only the shared I/O primitives and staleness check.
//
// Usage:
//
//	// Write (indented JSON, atomic)
//	if err := heartbeat.Write(path, &myStruct); err != nil { ... }
//
//	// Write (compact JSON, atomic) — e.g. for machine-read-only files
//	if err := heartbeat.WriteCompact(path, &myStruct); err != nil { ... }
//
//	// Read
//	var hb MyHeartbeat
//	if err := heartbeat.Read(path, &hb); err != nil {
//	    if errors.Is(err, heartbeat.ErrNotFound) { /* file doesn't exist yet */ }
//	    ...
//	}
//
//	// Staleness check
//	if heartbeat.IsStale(hb.Timestamp, maxAge) { ... }
package heartbeat

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// ErrNotFound is returned by Read when the heartbeat file does not exist.
var ErrNotFound = errors.New("heartbeat file not found")

// Write marshals v as indented JSON and atomically writes it to path via
// a temp-file rename. The destination directory must already exist.
func Write(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat: %w", err)
	}
	return atomicWrite(path, data)
}

// WriteCompact marshals v as compact JSON and atomically writes it to path
// via a temp-file rename. Use this for machine-read-only heartbeat files
// where human readability is not required.
func WriteCompact(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat: %w", err)
	}
	return atomicWrite(path, data)
}

// atomicWrite writes data to path atomically using a temp-file rename.
func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("failed to write heartbeat temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to rename heartbeat file: %w", err)
	}
	return nil
}

// Read reads the heartbeat file at path and unmarshals it into v.
// Returns ErrNotFound if the file does not exist.
func Read(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return fmt.Errorf("failed to read heartbeat: %w", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("failed to parse heartbeat: %w", err)
	}
	return nil
}

// IsStale returns true if the given timestamp is older than maxAge.
func IsStale(timestamp time.Time, maxAge time.Duration) bool {
	return time.Since(timestamp) > maxAge
}
