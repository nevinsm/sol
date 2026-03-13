package governor

import (
	"fmt"
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


// StopManager abstracts session operations for Stop.
type StopManager interface {
	brief.GracefulStopManager
}

// --- Stop ---

// Stop terminates a governor session. Injects a brief-update prompt and waits
// for output stability before killing the session. Does NOT remove the
// governor directory, mirror, or brief.
func Stop(world string, sphereStore store.AgentWriter, mgr StopManager) error {
	sessName := config.SessionName(world, "governor")
	agentID := world + "/governor"

	// 1. Graceful stop: inject brief update prompt, wait for stability, then kill.
	//    Falls back to immediate kill if no .brief/ directory exists.
	if mgr.Exists(sessName) {
		if err := brief.GracefulStop(sessName, BriefDir(world), mgr); err != nil {
			return fmt.Errorf("failed to stop governor for world %q: %w", world, err)
		}
	}

	// 2. Update agent state to "idle".
	if err := sphereStore.UpdateAgentState(agentID, store.AgentIdle, ""); err != nil {
		return fmt.Errorf("failed to stop governor for world %q: %w", world, err)
	}

	return nil
}
