package brief

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Inject reads the brief file at path and returns its contents framed in
// <brief>...</brief> tags. If the file doesn't exist or is empty, returns
// empty string and nil error. If content exceeds maxLines, truncates and
// appends a notice.
func Inject(path string, maxLines int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read brief %q: %w", path, err)
	}

	content := string(data)
	if strings.TrimSpace(content) == "" {
		return "", nil
	}

	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	truncated := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}

	var b strings.Builder
	b.WriteString("<brief>\n")
	b.WriteString(strings.Join(lines, "\n"))
	if truncated {
		b.WriteString("\n---\n")
		b.WriteString(fmt.Sprintf("TRUNCATED: Brief exceeded %d lines. Read the full file at %s and consolidate.", maxLines, path))
	}
	b.WriteString("\n</brief>")

	return b.String(), nil
}

// WriteSessionStart writes the current time (RFC3339 UTC) to
// {briefDir}/.session_start. Creates the file if it doesn't exist,
// overwrites if it does.
func WriteSessionStart(briefDir string) error {
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		return fmt.Errorf("failed to create brief directory %q: %w", briefDir, err)
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	path := filepath.Join(briefDir, ".session_start")
	if err := os.WriteFile(path, []byte(ts), 0o644); err != nil {
		return fmt.Errorf("failed to write session start %q: %w", path, err)
	}
	return nil
}

// CheckSave compares the mtime of the brief file against the .session_start
// timestamp in the same directory. Returns (true, nil) if the brief was
// modified after session start. Returns (false, nil) if not modified or
// the brief doesn't exist. If .session_start doesn't exist, returns
// (true, nil) — no session start recorded means we can't enforce the check.
func CheckSave(briefPath string) (bool, error) {
	briefDir := filepath.Dir(briefPath)
	sessionStartPath := filepath.Join(briefDir, ".session_start")

	// Read session start timestamp.
	data, err := os.ReadFile(sessionStartPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil // No session start — can't enforce.
		}
		return false, fmt.Errorf("failed to read session start %q: %w", sessionStartPath, err)
	}

	sessionStart, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return false, fmt.Errorf("failed to parse session start timestamp: %w", err)
	}

	// Check brief file mtime.
	info, err := os.Stat(briefPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // No brief file — not updated.
		}
		return false, fmt.Errorf("failed to stat brief %q: %w", briefPath, err)
	}

	return info.ModTime().After(sessionStart), nil
}
