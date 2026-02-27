package tether

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/config"
)

// TetherPath returns the path to an agent's tether file.
// Path: $SOL_HOME/{world}/outposts/{name}/.tether
func TetherPath(world, agentName string) string {
	return filepath.Join(config.Home(), world, "outposts", agentName, ".tether")
}

// Read reads the tether file and returns the work item ID.
// Returns ("", nil) if no tether file exists (agent is idle).
func Read(world, agentName string) (string, error) {
	data, err := os.ReadFile(TetherPath(world, agentName))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read tether for agent %q in world %q: %w", agentName, world, err)
	}
	return string(data), nil
}

// Write writes a work item ID to the tether file.
// Creates parent directories if needed.
func Write(world, agentName, workItemID string) error {
	path := TetherPath(world, agentName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create tether directory for agent %q in world %q: %w", agentName, world, err)
	}
	if err := os.WriteFile(path, []byte(workItemID), 0o644); err != nil {
		return fmt.Errorf("failed to write tether for agent %q in world %q: %w", agentName, world, err)
	}
	return nil
}

// Clear removes the tether file. No-op if it doesn't exist.
func Clear(world, agentName string) error {
	err := os.Remove(TetherPath(world, agentName))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear tether for agent %q in world %q: %w", agentName, world, err)
	}
	return nil
}

// IsTethered returns true if a tether file exists for the agent.
func IsTethered(world, agentName string) bool {
	_, err := os.Stat(TetherPath(world, agentName))
	return err == nil
}
