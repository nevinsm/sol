package governor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/brief"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// --- Directory helpers ---

// GovernorDir returns the root directory for a world's governor.
// $SOL_HOME/{world}/governor/
func GovernorDir(world string) string {
	return filepath.Join(config.Home(), world, "governor")
}

// BriefDir returns the brief directory for the governor.
// $SOL_HOME/{world}/governor/.brief/
func BriefDir(world string) string {
	return filepath.Join(config.Home(), world, "governor", ".brief")
}

// BriefPath returns the governor's memory file path.
// $SOL_HOME/{world}/governor/.brief/memory.md
func BriefPath(world string) string {
	return filepath.Join(config.Home(), world, "governor", ".brief", "memory.md")
}

// WorldSummaryPath returns the governor's world summary file path.
// $SOL_HOME/{world}/governor/.brief/world-summary.md
func WorldSummaryPath(world string) string {
	return filepath.Join(config.Home(), world, "governor", ".brief", "world-summary.md")
}

// --- Interfaces ---

// StopStore abstracts sphere store operations for Stop.
type StopStore interface {
	GetAgent(id string) (*store.Agent, error)
	UpdateAgentState(id, state, activeWrit string) error
}

// StopManager abstracts session operations for Stop.
type StopManager interface {
	brief.GracefulStopManager
}

// --- Stop ---

// Stop terminates a governor session. Injects a brief-update prompt and waits
// for output stability before killing the session. Does NOT remove the
// governor directory, mirror, or brief.
func Stop(world string, sphereStore StopStore, mgr StopManager) error {
	sessName := config.SessionName(world, "governor")
	agentID := world + "/governor"

	// Read existing agent record to preserve active_writ across stop/start cycles.
	existing, err := sphereStore.GetAgent(agentID)
	if err != nil {
		return fmt.Errorf("failed to stop governor for world %q: %w", world, err)
	}

	// 1. Graceful stop: inject brief update prompt, wait for stability, then kill.
	//    Falls back to immediate kill if no .brief/ directory exists.
	if mgr.Exists(sessName) {
		if err := brief.GracefulStop(sessName, BriefDir(world), mgr); err != nil {
			// Best-effort: update agent state to idle even when stop fails.
			// The session may already be dead; keeping state="working" triggers spurious Prefect respawns.
			if stateErr := sphereStore.UpdateAgentState(agentID, store.AgentIdle, existing.ActiveWrit); stateErr != nil {
				fmt.Fprintf(os.Stderr, "governor stop: failed to update agent state after stop error: %v\n", stateErr)
			}
			return fmt.Errorf("failed to stop governor for world %q: %w", world, err)
		}
	}

	// 2. Update agent state to idle, preserving active_writ so restart context is retained.
	if err := sphereStore.UpdateAgentState(agentID, store.AgentIdle, existing.ActiveWrit); err != nil {
		return fmt.Errorf("failed to stop governor for world %q: %w", world, err)
	}

	return nil
}
