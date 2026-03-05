package governor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/brief"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/protocol"
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

// SphereStore abstracts sphere store operations for governor lifecycle.
type SphereStore interface {
	EnsureAgent(name, world, role string) error
	UpdateAgentState(id, state, tetherItem string) error
}

// SessionManager abstracts session operations for Start.
type SessionManager interface {
	Exists(name string) bool
	Start(name, workdir, cmd string, env map[string]string, role, world string) error
}

// StopManager abstracts session operations for Stop.
type StopManager interface {
	brief.GracefulStopManager
}

// --- Options ---

// StartOpts holds inputs for starting a governor.
type StartOpts struct {
	World string
}

// --- Start ---

// Start launches a governor session for the given world.
func Start(opts StartOpts, sphereStore SphereStore, mgr SessionManager) error {
	govDir := GovernorDir(opts.World)
	briefDir := BriefDir(opts.World)

	// 1. Create governor directory and brief directory.
	if err := os.MkdirAll(govDir, 0o755); err != nil {
		return fmt.Errorf("failed to start governor for world %q: %w", opts.World, err)
	}
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		return fmt.Errorf("failed to start governor for world %q: %w", opts.World, err)
	}

	// 2. Register agent (idempotent — governor is a singleton).
	if err := sphereStore.EnsureAgent("governor", opts.World, "governor"); err != nil {
		return fmt.Errorf("failed to start governor for world %q: %w", opts.World, err)
	}

	// 3. Check if session already exists.
	sessName := config.SessionName(opts.World, "governor")
	if mgr.Exists(sessName) {
		return fmt.Errorf("governor session for world %q already running", opts.World)
	}

	// 4. Install hooks in GovernorDir.
	if err := installHooks(govDir, opts.World); err != nil {
		return fmt.Errorf("failed to start governor for world %q: %w", opts.World, err)
	}

	// 5. Start tmux session.
	prompt := fmt.Sprintf("Governor, world %s. If no context appears, run: sol brief inject --path=.brief/memory.md --max-lines=200 && sol world sync --world=%s",
		opts.World, opts.World)
	sessionCmd := config.BuildSessionCommand(config.SettingsPath(govDir), prompt)
	if err := mgr.Start(sessName, govDir, sessionCmd, nil, "governor", opts.World); err != nil {
		return fmt.Errorf("failed to start governor for world %q: %w", opts.World, err)
	}

	// 6. Update agent state to "idle".
	agentID := opts.World + "/governor"
	if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
		return fmt.Errorf("failed to start governor for world %q: %w", opts.World, err)
	}

	return nil
}

// installHooks writes .claude/settings.local.json with brief, sync, and PreCompact hooks.
func installHooks(govDir, world string) error {
	claudeDir := filepath.Join(govDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	cfg := protocol.HookConfig{
		Hooks: map[string][]protocol.HookMatcherGroup{
			"SessionStart": {
				{
					Matcher: "startup|resume",
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol brief inject --path=.brief/memory.md --max-lines=200 && sol world sync --world=%s", world),
						},
					},
				},
				{
					Matcher: "compact",
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: "sol brief inject --path=.brief/memory.md --max-lines=200",
						},
					},
				},
			},
			"PreCompact": {
				{
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol handoff --world=%s --agent=governor", world),
						},
					},
				},
			},
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
			}, protocol.GuardHooks("governor")...),
			"UserPromptSubmit": {
				{
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol nudge drain --world=%s --agent=governor", world),
						},
					},
				},
			},
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

// Stop terminates a governor session. Injects a brief-update prompt and waits
// for output stability before killing the session. Does NOT remove the
// governor directory, mirror, or brief.
func Stop(world string, sphereStore SphereStore, mgr StopManager) error {
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
	if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
		return fmt.Errorf("failed to stop governor for world %q: %w", world, err)
	}

	return nil
}
