package hook

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/gt/internal/config"
)

// HookPath returns the path to an agent's hook file.
// Path: $GT_HOME/{rig}/polecats/{name}/.hook
func HookPath(rig, agentName string) string {
	return filepath.Join(config.Home(), rig, "polecats", agentName, ".hook")
}

// Read reads the hook file and returns the work item ID.
// Returns ("", nil) if no hook file exists (agent is idle).
func Read(rig, agentName string) (string, error) {
	data, err := os.ReadFile(HookPath(rig, agentName))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read hook for agent %q in rig %q: %w", agentName, rig, err)
	}
	return string(data), nil
}

// Write writes a work item ID to the hook file.
// Creates parent directories if needed.
func Write(rig, agentName, workItemID string) error {
	path := HookPath(rig, agentName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create hook directory for agent %q in rig %q: %w", agentName, rig, err)
	}
	if err := os.WriteFile(path, []byte(workItemID), 0o644); err != nil {
		return fmt.Errorf("failed to write hook for agent %q in rig %q: %w", agentName, rig, err)
	}
	return nil
}

// Clear removes the hook file. No-op if it doesn't exist.
func Clear(rig, agentName string) error {
	err := os.Remove(HookPath(rig, agentName))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear hook for agent %q in rig %q: %w", agentName, rig, err)
	}
	return nil
}

// IsHooked returns true if a hook file exists for the agent.
func IsHooked(rig, agentName string) bool {
	_, err := os.Stat(HookPath(rig, agentName))
	return err == nil
}
