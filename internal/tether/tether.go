package tether

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/fileutil"
)

// TetherDir returns the path to an agent's tether directory.
// Uses role-aware directory: outposts/{name}/.tether/ for agents, envoys/{name}/.tether/ for envoys, etc.
func TetherDir(world, agentName, role string) string {
	return filepath.Join(config.AgentDir(world, agentName, role), ".tether")
}

// Write writes a writ ID to the tether directory.
// Creates the directory if needed. Each writ gets its own file.
// Uses fsync before rename to guarantee durability across power failures.
func Write(world, agentName, writID, role string) error {
	dir := TetherDir(world, agentName, role)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create tether directory for agent %q in world %q: %w", agentName, world, err)
	}

	path := filepath.Join(dir, writID)
	if err := fileutil.AtomicWrite(path, []byte(writID), 0o644); err != nil {
		return fmt.Errorf("failed to write tether for agent %q in world %q: %w", agentName, world, err)
	}

	// Verify write succeeded — defensive check against silent filesystem failures.
	content, err := os.ReadFile(path)
	if err != nil || string(content) != writID {
		return fmt.Errorf("tether write verification failed for agent %q in world %q: wrote %q but read back %q (err: %v)", agentName, world, writID, string(content), err)
	}

	slog.Debug("tether: wrote", "writID", writID, "agent", agentName, "world", world)
	return nil
}

// Read reads the first (only) tether file and returns the writ ID.
// Returns ("", nil) if no tether files exist (agent is idle).
// For backward compatibility with outpost agents that expect a single tether.
func Read(world, agentName, role string) (string, error) {
	ids, err := List(world, agentName, role)
	if err != nil {
		return "", fmt.Errorf("failed to list tethers: %w", err)
	}
	if len(ids) == 0 {
		return "", nil
	}
	return ids[0], nil
}

// List returns all writ IDs from the tether directory.
// Returns nil if the directory doesn't exist (agent is idle).
func List(world, agentName, role string) ([]string, error) {
	dir := TetherDir(world, agentName, role)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list tethers for agent %q in world %q: %w", agentName, world, err)
	}

	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Skip temp files from atomic writes.
		if strings.HasSuffix(name, ".tmp") {
			continue
		}
		ids = append(ids, name)
	}
	sort.Strings(ids)
	return ids, nil
}

// Clear removes ALL tether files from the directory. No-op if directory doesn't exist.
func Clear(world, agentName, role string) error {
	dir := TetherDir(world, agentName, role)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to clear tethers for agent %q in world %q: %w", agentName, world, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to clear tether file %q for agent %q in world %q: %w", e.Name(), agentName, world, err)
		}
	}
	return nil
}

// ClearOne removes a single tether file. No-op if it doesn't exist.
func ClearOne(world, agentName, writID, role string) error {
	path := filepath.Join(TetherDir(world, agentName, role), writID)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear tether %q for agent %q in world %q: %w", writID, agentName, world, err)
	}
	return nil
}

// IsTethered returns true if the tether directory has any files.
func IsTethered(world, agentName, role string) bool {
	ids, err := List(world, agentName, role)
	return err == nil && len(ids) > 0
}

// IsTetheredTo returns true if a specific writ is tethered to this agent.
func IsTetheredTo(world, agentName, writID, role string) bool {
	path := filepath.Join(TetherDir(world, agentName, role), writID)
	_, err := os.Stat(path)
	return err == nil
}

// Migrate detects a legacy .tether file (not directory) and migrates it
// to the new .tether/{writID} directory format. This is a one-time migration
// that should be called on startup for determinism.
func Migrate(world, agentName, role string) error {
	agentDir := config.AgentDir(world, agentName, role)
	tetherPath := filepath.Join(agentDir, ".tether")

	info, err := os.Stat(tetherPath)
	if err != nil {
		return nil // no tether file at all — nothing to migrate
	}
	if info.IsDir() {
		return nil // already a directory — no migration needed
	}

	// Read the old tether file content (writ ID).
	data, err := os.ReadFile(tetherPath)
	if err != nil {
		return fmt.Errorf("tether migration: failed to read legacy tether for agent %q: %w", agentName, err)
	}
	writID := strings.TrimSpace(string(data))
	if writID == "" {
		// Empty tether file — just remove it.
		os.Remove(tetherPath)
		return nil
	}

	// Remove the old file.
	if err := os.Remove(tetherPath); err != nil {
		return fmt.Errorf("tether migration: failed to remove legacy tether for agent %q: %w", agentName, err)
	}

	// Write using the new directory model.
	if err := Write(world, agentName, writID, role); err != nil {
		return fmt.Errorf("tether migration: failed to write new tether for agent %q: %w", agentName, err)
	}

	fmt.Fprintf(os.Stderr, "tether: migrated legacy .tether file to directory for agent %q (writ %s)\n", agentName, writID)
	return nil
}
