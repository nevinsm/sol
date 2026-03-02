package governor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/config"
)

// --- Directory helpers ---

// GovernorDir returns the root directory for a world's governor.
// $SOL_HOME/{world}/governor/
func GovernorDir(world string) string {
	return filepath.Join(config.Home(), world, "governor")
}

// MirrorPath returns the read-only mirror path.
// $SOL_HOME/{world}/governor/mirror/
func MirrorPath(world string) string {
	return filepath.Join(config.Home(), world, "governor", "mirror")
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
	Exists(name string) bool
	Stop(name string, force bool) error
}

// --- Options ---

// StartOpts holds inputs for starting a governor.
type StartOpts struct {
	World      string
	SourceRepo string // from world config or flag
}

// --- Hook config ---

type hookConfig struct {
	Hooks map[string][]hookEntry `json:"hooks"`
}

type hookEntry struct {
	Type    string `json:"type"`
	Matcher string `json:"matcher,omitempty"`
	Command string `json:"command"`
}

// --- Mirror ---

// SetupMirror clones or updates the read-only mirror of the source repo.
// If mirror doesn't exist, clones. If it exists, pulls latest.
func SetupMirror(world, sourceRepo string) error {
	mirrorPath := MirrorPath(world)

	if _, err := os.Stat(mirrorPath); os.IsNotExist(err) {
		// Clone the source repo.
		cmd := exec.Command("git", "clone", sourceRepo, mirrorPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to setup governor mirror for world %q: %s: %w",
				world, strings.TrimSpace(string(out)), err)
		}
		return nil
	}

	// Pull latest (best-effort — warn on failure, don't error).
	cmd := exec.Command("git", "-C", mirrorPath, "pull", "--ff-only")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "governor: mirror pull failed for world %q (best-effort): %s\n",
			world, strings.TrimSpace(string(out)))
	}
	_ = cmd // silence vet if needed

	return nil
}

// RefreshMirror pulls latest changes in the mirror.
func RefreshMirror(world string) error {
	mirrorPath := MirrorPath(world)

	if _, err := os.Stat(mirrorPath); os.IsNotExist(err) {
		return fmt.Errorf("mirror not found — run governor start first")
	}

	// Checkout main branch.
	cmd := exec.Command("git", "-C", mirrorPath, "checkout", "main")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to refresh governor mirror for world %q: %s: %w",
			world, strings.TrimSpace(string(out)), err)
	}

	// Pull latest.
	cmd = exec.Command("git", "-C", mirrorPath, "pull", "--ff-only")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to refresh governor mirror for world %q: %s: %w",
			world, strings.TrimSpace(string(out)), err)
	}

	return nil
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

	// 4. Setup mirror (warn on failure — governor still works without it).
	if err := SetupMirror(opts.World, opts.SourceRepo); err != nil {
		fmt.Fprintf(os.Stderr, "governor: mirror setup failed for world %q: %v\n", opts.World, err)
	}

	// 5. Install placeholder CLAUDE.md (prompt 06 replaces with protocol generator).
	claudeMDPath := filepath.Join(govDir, "CLAUDE.md")
	if err := os.WriteFile(claudeMDPath, []byte("# Governor\n\nPlaceholder — replaced by protocol generator.\n"), 0o644); err != nil {
		return fmt.Errorf("failed to start governor for world %q: %w", opts.World, err)
	}

	// 6. Install hooks in GovernorDir.
	if err := installHooks(govDir, opts.World); err != nil {
		return fmt.Errorf("failed to start governor for world %q: %w", opts.World, err)
	}

	// 7. Start tmux session.
	if err := mgr.Start(sessName, govDir, "claude --dangerously-skip-permissions", nil, "governor", opts.World); err != nil {
		return fmt.Errorf("failed to start governor for world %q: %w", opts.World, err)
	}

	// 8. Update agent state to "idle".
	agentID := opts.World + "/governor"
	if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
		return fmt.Errorf("failed to start governor for world %q: %w", opts.World, err)
	}

	return nil
}

// installHooks writes .claude/settings.local.json with brief and mirror hooks.
func installHooks(govDir, world string) error {
	claudeDir := filepath.Join(govDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	cfg := hookConfig{
		Hooks: map[string][]hookEntry{
			"SessionStart": {
				{
					Type:    "command",
					Matcher: "startup|resume",
					Command: fmt.Sprintf("sol brief inject --path=.brief/memory.md --max-lines=200 && sol governor refresh-mirror --world=%s", world),
				},
				{
					Type:    "command",
					Matcher: "compact",
					Command: "sol brief inject --path=.brief/memory.md --max-lines=200",
				},
			},
			"Stop": {
				{
					Type:    "command",
					Command: "sol brief check-save .brief/memory.md",
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

// Stop terminates a governor session. Does NOT remove the governor directory,
// mirror, or brief.
func Stop(world string, sphereStore SphereStore, mgr StopManager) error {
	sessName := config.SessionName(world, "governor")
	agentID := world + "/governor"

	// 1. Check session exists. If so, stop it.
	if mgr.Exists(sessName) {
		if err := mgr.Stop(sessName, true); err != nil {
			return fmt.Errorf("failed to stop governor for world %q: %w", world, err)
		}
	}

	// 2. Update agent state to "idle".
	if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
		return fmt.Errorf("failed to stop governor for world %q: %w", world, err)
	}

	return nil
}
