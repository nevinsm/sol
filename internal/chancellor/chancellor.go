package chancellor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/account"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/protocol"
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

// SessionName is the fixed tmux session name for the chancellor.
const SessionName = "sol-chancellor"

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

// Start launches the chancellor session.
func Start(mgr SessionManager) error {
	chancellorDir := ChancellorDir()
	briefDir := BriefDir()

	// 1. Create chancellor and brief directories.
	if err := os.MkdirAll(chancellorDir, 0o755); err != nil {
		return fmt.Errorf("failed to start chancellor: %w", err)
	}
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		return fmt.Errorf("failed to start chancellor: %w", err)
	}

	// 2. Install hooks in chancellor directory.
	if err := installHooks(chancellorDir); err != nil {
		return fmt.Errorf("failed to start chancellor: %w", err)
	}

	// 3. Check if session already exists.
	if mgr.Exists(SessionName) {
		return fmt.Errorf("chancellor session already running")
	}

	// 4. Resolve account and start tmux session.
	resolvedAccount := account.ResolveAccount("", "")
	claudeConfigDir, err := config.EnsureClaudeConfigDir(config.Home(), "chancellor", "chancellor", resolvedAccount)
	if err != nil {
		return fmt.Errorf("failed to ensure claude config dir: %w", err)
	}
	prompt := "Chancellor session. If no context appears, run: sol brief inject --path=.brief/memory.md --max-lines=200"
	sessionCmd := config.BuildSessionCommand(config.SettingsPath(chancellorDir), prompt)
	env := map[string]string{
		"SOL_HOME":          config.Home(),
		"SOL_AGENT":         "chancellor",
		"CLAUDE_CONFIG_DIR": claudeConfigDir,
	}
	if err := mgr.Start(SessionName, chancellorDir, sessionCmd, env, "chancellor", ""); err != nil {
		return fmt.Errorf("failed to start chancellor: %w", err)
	}

	return nil
}

// installHooks writes .claude/settings.local.json with PreToolUse hooks.
func installHooks(chancellorDir string) error {
	claudeDir := filepath.Join(chancellorDir, ".claude")
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
			}, protocol.GuardHooks("chancellor")...),
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

// Stop terminates the chancellor session.
func Stop(mgr StopManager) error {
	if !mgr.Exists(SessionName) {
		return fmt.Errorf("no chancellor session running")
	}
	if err := mgr.Stop(SessionName, true); err != nil {
		return fmt.Errorf("failed to stop chancellor: %w", err)
	}
	return nil
}
