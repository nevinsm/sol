package chancellor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/brief"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
)

// --- Directory helpers ---

// ChancellorDir returns the root directory for the chancellor.
// $SOL_HOME/chancellor/
func ChancellorDir() string {
	return filepath.Join(config.Home(), "chancellor")
}

// BriefDir returns the brief directory for the chancellor.
// $SOL_HOME/chancellor/.brief/
func BriefDir() string {
	return filepath.Join(config.Home(), "chancellor", ".brief")
}

// BriefPath returns the chancellor's memory file path.
// $SOL_HOME/chancellor/.brief/memory.md
func BriefPath() string {
	return filepath.Join(config.Home(), "chancellor", ".brief", "memory.md")
}

// SessionName is the tmux session name for the chancellor.
// Derived from config.SessionName so it stays in sync if the naming scheme changes.
var SessionName = config.SessionName("", "chancellor")

// --- Interfaces ---

// SessionManager abstracts session operations for Start.
type SessionManager interface {
	Exists(name string) bool
	Start(name, workdir, cmd string, env map[string]string, role, world string) error
}

// StopManager abstracts session operations for Stop.
type StopManager interface {
	brief.GracefulStopManager
}

// StopStore abstracts sphere store operations for Stop.
type StopStore interface {
	GetAgent(id string) (*store.Agent, error)
	UpdateAgentState(id, state, activeWrit string) error
}

// --- Start ---

// Start launches the chancellor session.
func Start(mgr SessionManager) error {
	// Create chancellor and brief directories before Launch checks for worktree.
	if err := os.MkdirAll(BriefDir(), 0o755); err != nil {
		return fmt.Errorf("failed to start chancellor: %w", err)
	}

	if mgr.Exists(SessionName) {
		return fmt.Errorf("chancellor session already running")
	}

	cfg := RoleConfig()
	if _, err := startup.Launch(cfg, "", "chancellor", startup.LaunchOpts{
		Sessions: mgr,
	}); err != nil {
		return fmt.Errorf("failed to start chancellor: %w", err)
	}

	return nil
}

// --- Stop ---

// Stop terminates the chancellor session gracefully and updates the agent
// record to idle. Follows the same pattern as envoy.Stop and governor.Stop.
func Stop(mgr StopManager, sphereStore StopStore) error {
	agentID := "/chancellor"

	// Read existing agent record to preserve active_writ across stop/start cycles.
	existing, err := sphereStore.GetAgent(agentID)
	if err != nil {
		return fmt.Errorf("failed to stop chancellor: %w", err)
	}

	if !mgr.Exists(SessionName) {
		return fmt.Errorf("no chancellor session running")
	}

	if err := brief.GracefulStop(SessionName, BriefDir(), mgr); err != nil {
		// Best-effort: update agent state to idle even when stop fails.
		if stateErr := sphereStore.UpdateAgentState(agentID, store.AgentIdle, existing.ActiveWrit); stateErr != nil {
			fmt.Fprintf(os.Stderr, "chancellor stop: failed to update agent state after stop error: %v\n", stateErr)
		}
		return fmt.Errorf("failed to stop chancellor: %w", err)
	}

	// Update agent state to idle, preserving active_writ so restart context is retained.
	if err := sphereStore.UpdateAgentState(agentID, store.AgentIdle, existing.ActiveWrit); err != nil {
		return fmt.Errorf("failed to update chancellor agent state: %w", err)
	}

	return nil
}
