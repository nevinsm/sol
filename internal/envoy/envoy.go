package envoy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/brief"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
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

// PersonaPath returns the path to the envoy's optional persona file.
// $SOL_HOME/{world}/envoys/{name}/persona.md
func PersonaPath(world, name string) string {
	return filepath.Join(config.Home(), world, "envoys", name, "persona.md")
}

// --- Interfaces ---

// SphereStore abstracts sphere store operations for Create.
type SphereStore interface {
	CreateAgent(name, world, role string) (string, error)
	DeleteAgent(id string) error
}

// StopStore abstracts sphere store operations for Stop.
type StopStore interface {
	UpdateAgentState(id string, state store.AgentState, activeWrit string) error
}

// ListStore abstracts sphere store operations for List.
type ListStore interface {
	ListAgents(world string, state store.AgentState) ([]store.Agent, error)
}

// StopManager abstracts session operations for Stop.
type StopManager interface {
	brief.GracefulStopManager
}

// DeleteStore abstracts sphere store operations for Delete.
type DeleteStore interface {
	GetAgent(id string) (*store.Agent, error)
	DeleteAgent(id string) error
}

// --- Options ---

// CreateOpts holds inputs for creating an envoy.
type CreateOpts struct {
	World      string
	Name       string
	SourceRepo string // path to git repo for worktree
}

// DeleteOpts holds inputs for deleting an envoy.
type DeleteOpts struct {
	World      string
	Name       string
	SourceRepo string // path to managed repo for git operations
	Force      bool   // override active session / tether checks
}

// --- Create ---

// Create provisions a new envoy: agent record, directory, worktree, and brief.
// If any step fails after the first, all previous steps are rolled back.
func Create(opts CreateOpts, sphereStore SphereStore) error {
	envoyDir := EnvoyDir(opts.World, opts.Name)
	briefDir := BriefDir(opts.World, opts.Name)
	worktree := WorktreePath(opts.World, opts.Name)

	// 1. Register agent (most likely to fail on name conflicts — fail fast).
	agentID, err := sphereStore.CreateAgent(opts.Name, opts.World, "envoy")
	if err != nil {
		return fmt.Errorf("failed to create envoy %q in world %q: %w", opts.Name, opts.World, err)
	}

	// From here on, roll back all completed steps on failure.
	rollback := func() {
		// Remove worktree from git tracking (best-effort).
		if _, statErr := os.Stat(worktree); statErr == nil {
			rmCmd := exec.Command("git", "-C", opts.SourceRepo, "worktree", "remove", "--force", worktree)
			if out, err := rmCmd.CombinedOutput(); err != nil {
				fmt.Fprintf(os.Stderr, "rollback: worktree remove: %s\n", strings.TrimSpace(string(out)))
			}
		}
		// Remove entire envoy directory (covers worktree dir, brief dir).
		os.RemoveAll(envoyDir)
		// Delete agent record.
		if err := sphereStore.DeleteAgent(agentID); err != nil {
			fmt.Fprintf(os.Stderr, "rollback: failed to delete agent record: %v\n", err)
		}
	}

	// 2. Create envoy directory.
	if err := os.MkdirAll(envoyDir, 0o755); err != nil {
		rollback()
		return fmt.Errorf("failed to create envoy %q in world %q: %w", opts.Name, opts.World, err)
	}

	// 3. Create persistent worktree (idempotent).
	if err := ensureWorktree(opts.SourceRepo, opts.World, opts.Name, worktree); err != nil {
		rollback()
		return fmt.Errorf("failed to create envoy %q in world %q: %w", opts.Name, opts.World, err)
	}

	// 4. Create brief directory.
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		rollback()
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

// --- Stop ---

// Stop terminates an envoy session. Injects a brief-update prompt and waits
// for output stability before killing the session. Does NOT remove the
// worktree or directory.
func Stop(world, name string, sphereStore StopStore, mgr StopManager) error {
	agentID := world + "/" + name
	sessName := config.SessionName(world, name)

	// 1. Graceful stop: inject brief update prompt, wait for stability, then kill.
	//    Falls back to immediate kill if no .brief/ directory exists.
	if mgr.Exists(sessName) {
		if err := brief.GracefulStop(sessName, BriefDir(world, name), mgr); err != nil {
			return fmt.Errorf("failed to stop envoy %q in world %q: %w", name, world, err)
		}
	}

	// 2. Update agent state to "idle".
	if err := sphereStore.UpdateAgentState(agentID, store.AgentIdle, ""); err != nil {
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

// --- Delete ---

// Delete removes an envoy: stops session, removes worktree, deletes directory,
// deletes git branch, and removes the agent record.
func Delete(opts DeleteOpts, sphereStore DeleteStore, mgr StopManager) error {
	agentID := opts.World + "/" + opts.Name
	sessName := config.SessionName(opts.World, opts.Name)

	// 1. Verify agent exists and is an envoy.
	agent, err := sphereStore.GetAgent(agentID)
	if err != nil {
		return fmt.Errorf("envoy %q not found in world %q: %w", opts.Name, opts.World, err)
	}
	if agent.Role != "envoy" {
		return fmt.Errorf("agent %q has role %q, expected \"envoy\"", agentID, agent.Role)
	}

	// 2. Check for active session.
	if mgr.Exists(sessName) {
		if !opts.Force {
			return fmt.Errorf("envoy %q has an active session — stop it first or use --force", opts.Name)
		}
		fmt.Fprintf(os.Stderr, "Stopping active session for envoy %q\n", opts.Name)
		if err := mgr.Stop(sessName, true); err != nil {
			return fmt.Errorf("failed to stop envoy session: %w", err)
		}
	}

	// 3. Check for tether.
	if tether.IsTethered(opts.World, opts.Name, "envoy") {
		if !opts.Force {
			writ, _ := tether.Read(opts.World, opts.Name, "envoy")
			return fmt.Errorf("envoy %q is tethered to %q — clear tether first or use --force", opts.Name, writ)
		}
		fmt.Fprintf(os.Stderr, "Clearing tether for envoy %q\n", opts.Name)
		if err := tether.Clear(opts.World, opts.Name, "envoy"); err != nil {
			return fmt.Errorf("failed to clear tether: %w", err)
		}
	}

	// 4. Warn about non-empty brief (informational only).
	briefPath := BriefPath(opts.World, opts.Name)
	if info, err := os.Stat(briefPath); err == nil && info.Size() > 0 {
		fmt.Fprintf(os.Stderr, "Note: envoy %q has a brief — it will be deleted (use 'sol envoy debrief' first to archive)\n", opts.Name)
	}

	// 5. Remove git worktree.
	worktreeDir := WorktreePath(opts.World, opts.Name)
	if _, err := os.Stat(worktreeDir); err == nil {
		rmCmd := exec.Command("git", "-C", opts.SourceRepo, "worktree", "remove", "--force", worktreeDir)
		if out, err := rmCmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: worktree remove failed: %s\n", strings.TrimSpace(string(out)))
			// Fallback: remove directory directly.
			if removeErr := os.RemoveAll(worktreeDir); removeErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to remove worktree dir: %v\n", removeErr)
			}
		}
		// Prune stale worktree references.
		pruneCmd := exec.Command("git", "-C", opts.SourceRepo, "worktree", "prune")
		if out, err := pruneCmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: worktree prune failed: %s\n", strings.TrimSpace(string(out)))
		}
	}

	// 6. Delete the envoy directory.
	envoyDir := EnvoyDir(opts.World, opts.Name)
	if err := os.RemoveAll(envoyDir); err != nil {
		return fmt.Errorf("failed to remove envoy directory %q: %w", envoyDir, err)
	}

	// 7. Delete the git branch (best-effort).
	branch := fmt.Sprintf("envoy/%s/%s", opts.World, opts.Name)
	branchCmd := exec.Command("git", "-C", opts.SourceRepo, "branch", "-D", branch)
	if out, err := branchCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: branch delete failed: %s\n", strings.TrimSpace(string(out)))
	}

	// 8. Delete the agent record.
	if err := sphereStore.DeleteAgent(agentID); err != nil {
		return fmt.Errorf("failed to delete agent record: %w", err)
	}

	return nil
}
