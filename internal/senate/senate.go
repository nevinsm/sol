package senate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/protocol"
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

	// 2. Install hooks in senate directory.
	if err := installHooks(senateDir); err != nil {
		return fmt.Errorf("failed to start senate: %w", err)
	}

	// 3. Check if session already exists.
	if mgr.Exists(SessionName) {
		return fmt.Errorf("senate session already running")
	}

	// 4. Start tmux session.
	claudeConfigDir, err := config.EnsureClaudeConfigDir(config.Home(), "senate", "senate")
	if err != nil {
		return err
	}
	prompt := "Senate session. If no context appears, run: sol brief inject --path=.brief/memory.md --max-lines=200"
	sessionCmd := config.BuildSessionCommand(config.SettingsPath(senateDir), prompt)
	env := map[string]string{
		"SOL_HOME":         config.Home(),
		"SOL_AGENT":        "senate",
		"CLAUDE_CONFIG_DIR": claudeConfigDir,
	}
	if err := mgr.Start(SessionName, senateDir, sessionCmd, env, "senate", ""); err != nil {
		return fmt.Errorf("failed to start senate: %w", err)
	}

	return nil
}

// installHooks writes .claude/settings.local.json with PreToolUse hooks.
func installHooks(senateDir string) error {
	claudeDir := filepath.Join(senateDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	cfg := protocol.HookConfig{
		Hooks: map[string][]protocol.HookMatcherGroup{
			"PreToolUse": append([]protocol.HookMatcherGroup{
				{
					Matcher: "Write|Edit",
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: `FILE=$(jq -r '.tool_input.file_path // empty'); if echo "$FILE" | grep -q '.claude/projects/.*/memory/'; then echo "BLOCKED: Use .brief/memory.md, not Claude Code auto-memory." >&2; exit 2; fi`,
						},
					},
				},
				{
					Matcher: "EnterPlanMode",
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: `echo "BLOCKED: Plan mode overrides your persona and context. Outline your approach in conversation instead. Your persistent memory is at .brief/memory.md — consult it for your role constraints and accumulated knowledge." >&2; exit 2`,
						},
					},
				},
			}, protocol.GuardHooks("senate")...),
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal hook settings: %w", err)
	}

	settingsPath := filepath.Join(claudeDir, "settings.local.json")
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write settings.local.json: %w", err)
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
