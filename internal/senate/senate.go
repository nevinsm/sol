package senate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/config"
)

// --- Directory helpers ---

// SenateDir returns the root directory for the senate.
// $SOL_HOME/senate/
func SenateDir() string {
	return filepath.Join(config.Home(), "senate")
}

// BriefDir returns the brief directory for the senate.
// $SOL_HOME/senate/.brief/
func BriefDir() string {
	return filepath.Join(config.Home(), "senate", ".brief")
}

// BriefPath returns the senate's memory file path.
// $SOL_HOME/senate/.brief/memory.md
func BriefPath() string {
	return filepath.Join(config.Home(), "senate", ".brief", "memory.md")
}

// SessionName is the fixed tmux session name for the senate.
const SessionName = "sol-senate"

// --- Interfaces ---

// SessionManager abstracts session operations for Start.
type SessionManager interface {
	Exists(name string) bool
	Start(name, workdir, cmd string, env map[string]string, role, world string) error
}

// StopManager abstracts session operations for Stop.
type StopManager interface {
	Exists(name string) bool
	Stop(name string, force bool) error
}

// --- Start ---

// Start launches the senate session.
func Start(mgr SessionManager) error {
	senateDir := SenateDir()
	briefDir := BriefDir()

	// 1. Create senate and brief directories.
	if err := os.MkdirAll(senateDir, 0o755); err != nil {
		return fmt.Errorf("failed to start senate: %w", err)
	}
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		return fmt.Errorf("failed to start senate: %w", err)
	}

	// 2. Check if session already exists.
	if mgr.Exists(SessionName) {
		return fmt.Errorf("senate session already running")
	}

	// 3. Start tmux session.
	prompt := "Senate session. If no context appears, run: sol brief inject --path=.brief/memory.md --max-lines=200"
	sessionCmd := config.BuildSessionCommand(config.SettingsPath(senateDir), prompt)
	if err := mgr.Start(SessionName, senateDir, sessionCmd, nil, "senate", ""); err != nil {
		return fmt.Errorf("failed to start senate: %w", err)
	}

	return nil
}

// --- Stop ---

// Stop terminates the senate session.
func Stop(mgr StopManager) error {
	if !mgr.Exists(SessionName) {
		return fmt.Errorf("no senate session running")
	}
	if err := mgr.Stop(SessionName, true); err != nil {
		return fmt.Errorf("failed to stop senate: %w", err)
	}
	return nil
}
