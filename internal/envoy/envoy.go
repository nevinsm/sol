package envoy

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/store"
)

// --- Directory helpers ---

// EnvoyDir returns the root directory for an envoy.
// $SOL_HOME/{world}/envoys/{name}/
func EnvoyDir(world, name string) string {
	return filepath.Join(config.Home(), world, "envoys", name)
}

// WorktreePath returns the persistent worktree path for an envoy.
// $SOL_HOME/{world}/envoys/{name}/worktree/
func WorktreePath(world, name string) string {
	return filepath.Join(config.Home(), world, "envoys", name, "worktree")
}

// BriefDir returns the brief directory for an envoy.
// $SOL_HOME/{world}/envoys/{name}/.brief/
func BriefDir(world, name string) string {
	return filepath.Join(config.Home(), world, "envoys", name, ".brief")
}

// BriefPath returns the path to the envoy's memory file.
// $SOL_HOME/{world}/envoys/{name}/.brief/memory.md
func BriefPath(world, name string) string {
	return filepath.Join(config.Home(), world, "envoys", name, ".brief", "memory.md")
}

// SessionName returns the tmux session name for an envoy.
func SessionName(world, name string) string {
	return config.SessionName(world, name)
}

// --- Interfaces ---

// SphereStore abstracts sphere store operations for Create.
type SphereStore interface {
	CreateAgent(name, world, role string) (string, error)
}

// StartStore abstracts sphere store operations for Start and Stop.
type StartStore interface {
	GetAgent(id string) (*store.Agent, error)
	UpdateAgentState(id, state, tetherItem string) error
}

// ListStore abstracts sphere store operations for List.
type ListStore interface {
	ListAgents(world string, state string) ([]store.Agent, error)
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

// CreateOpts holds inputs for creating an envoy.
type CreateOpts struct {
	World      string
	Name       string
	SourceRepo string // path to git repo for worktree
}

// StartOpts holds inputs for starting an envoy session.
type StartOpts struct {
	World string
	Name  string
}

// --- Create ---

// Create provisions a new envoy: directory, brief, worktree, and agent record.
func Create(opts CreateOpts, sphereStore SphereStore) error {
	envoyDir := EnvoyDir(opts.World, opts.Name)
	briefDir := BriefDir(opts.World, opts.Name)
	worktree := WorktreePath(opts.World, opts.Name)

	// 1. Create envoy directory.
	if err := os.MkdirAll(envoyDir, 0o755); err != nil {
		return fmt.Errorf("failed to create envoy %q in world %q: %w", opts.Name, opts.World, err)
	}

	// 2. Create brief directory.
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		return fmt.Errorf("failed to create envoy %q in world %q: %w", opts.Name, opts.World, err)
	}

	// 3. Create persistent worktree (idempotent).
	if err := ensureWorktree(opts.SourceRepo, opts.World, opts.Name, worktree); err != nil {
		return fmt.Errorf("failed to create envoy %q in world %q: %w", opts.Name, opts.World, err)
	}

	// 4. Register agent.
	if _, err := sphereStore.CreateAgent(opts.Name, opts.World, "envoy"); err != nil {
		return fmt.Errorf("failed to create envoy %q in world %q: %w", opts.Name, opts.World, err)
	}

	return nil
}

// ensureWorktree creates a persistent git worktree for an envoy, or verifies
// an existing one is valid.
func ensureWorktree(sourceRepo, world, name, worktree string) error {
	// If worktree already exists and is valid, skip.
	if info, err := os.Stat(worktree); err == nil && info.IsDir() {
		cmd := exec.Command("git", "-C", worktree, "rev-parse", "--is-inside-work-tree")
		if _, err := cmd.CombinedOutput(); err == nil {
			return nil
		}
	}

	branch := fmt.Sprintf("envoy/%s/%s", world, name)

	// Try creating worktree with new branch.
	cmd := exec.Command("git", "-C", sourceRepo, "worktree", "add",
		"-b", branch, worktree, "HEAD")
	out1, err := cmd.CombinedOutput()
	if err != nil {
		// Branch may already exist — try without -b.
		cmd2 := exec.Command("git", "-C", sourceRepo, "worktree", "add",
			worktree, branch)
		if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
			return fmt.Errorf("worktree add failed (attempt 1: %s) (attempt 2: %s): %w",
				strings.TrimSpace(string(out1)),
				strings.TrimSpace(string(out2)), err2)
		}
	}

	return nil
}

// --- Start ---

// Start launches an envoy's tmux session with brief hooks.
// The caller is responsible for installing the CLAUDE.md protocol file
// before calling Start (following the forge pattern).
func Start(opts StartOpts, sphereStore StartStore, mgr SessionManager) error {
	agentID := opts.World + "/" + opts.Name
	sessName := SessionName(opts.World, opts.Name)
	worktree := WorktreePath(opts.World, opts.Name)

	// 1. Get agent record, verify role is "envoy".
	agent, err := sphereStore.GetAgent(agentID)
	if err != nil {
		return fmt.Errorf("failed to start envoy %q in world %q: %w", opts.Name, opts.World, err)
	}
	if agent.Role != "envoy" {
		return fmt.Errorf("agent %q has role %q, expected \"envoy\"", agentID, agent.Role)
	}

	// 2. Check if session already exists.
	if mgr.Exists(sessName) {
		return fmt.Errorf("envoy session %q already running", sessName)
	}

	// 3. Install hooks — .claude/settings.local.json with brief hooks.
	if err := installHooks(worktree); err != nil {
		return fmt.Errorf("failed to start envoy %q in world %q: %w", opts.Name, opts.World, err)
	}

	// 4. Start tmux session.
	prompt := fmt.Sprintf("Envoy %s, world %s. If no context appears, run: sol brief inject --path=.brief/memory.md --max-lines=200",
		opts.Name, opts.World)
	sessionCmd := config.BuildSessionCommand(config.SettingsPath(worktree), prompt)
	if err := mgr.Start(sessName, worktree, sessionCmd, nil, "envoy", opts.World); err != nil {
		return fmt.Errorf("failed to start envoy %q in world %q: %w", opts.Name, opts.World, err)
	}

	// 5. Update agent state to "idle".
	if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
		return fmt.Errorf("failed to start envoy %q in world %q: %w", opts.Name, opts.World, err)
	}

	return nil
}

// installHooks writes .claude/settings.local.json with brief hooks.
func installHooks(worktreeDir string) error {
	claudeDir := filepath.Join(worktreeDir, ".claude")
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
							Command: "sol brief inject --path=.brief/memory.md --max-lines=200",
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

// Stop terminates an envoy session. Does NOT remove the worktree or directory.
func Stop(world, name string, sphereStore StartStore, mgr StopManager) error {
	agentID := world + "/" + name
	sessName := SessionName(world, name)

	// 1. Check session exists. If so, stop it.
	if mgr.Exists(sessName) {
		if err := mgr.Stop(sessName, true); err != nil {
			return fmt.Errorf("failed to stop envoy %q in world %q: %w", name, world, err)
		}
	}

	// 2. Update agent state to "idle".
	if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
		return fmt.Errorf("failed to stop envoy %q in world %q: %w", name, world, err)
	}

	return nil
}

// --- List ---

// List returns envoy agents for a world. If world is empty, returns all envoys.
func List(world string, sphereStore ListStore) ([]store.Agent, error) {
	agents, err := sphereStore.ListAgents(world, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list envoys: %w", err)
	}

	var envoys []store.Agent
	for _, a := range agents {
		if a.Role == "envoy" {
			envoys = append(envoys, a)
		}
	}

	return envoys, nil
}
