package envoy

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/adapter"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/persona"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/sessionsave"
	"github.com/nevinsm/sol/internal/softfail"
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

// PersonaPath returns the path to the envoy's optional persona file.
// $SOL_HOME/{world}/envoys/{name}/persona.md
func PersonaPath(world, name string) string {
	return filepath.Join(config.Home(), world, "envoys", name, "persona.md")
}

// cleanupEnvoyConfigDir invokes the runtime adapter's CleanupConfigDir for an
// envoy being permanently terminated. Best-effort: logs warnings via slog but
// never fails the caller.
//
// We resolve the runtime adapter via the world config (the agent record has no
// Runtime field). If the configured runtime is unknown or world config cannot
// be loaded, we fall back to invoking every registered adapter — this catches
// the case where an envoy was created under a previous runtime that has since
// been swapped. CleanupConfigDir is idempotent so the fallback is safe.
//
// Mirrors dispatch.cleanupOutpostConfigDir; the contract is identical except
// that this is only invoked on permanent envoy termination (Delete), never on
// Stop / handoff / resolve. Envoys retain their adapter config (memory,
// auth.json) across normal lifecycle events.
func cleanupEnvoyConfigDir(world, agentName string) {
	worldDir := config.WorldDir(world)
	worldCfg, err := config.LoadWorldConfig(world)
	if err == nil {
		runtime := worldCfg.ResolveRuntime("envoy")
		if a, ok := adapter.Get(runtime); ok {
			if cleanupErr := a.CleanupConfigDir(worldDir, "envoy", agentName); cleanupErr != nil {
				slog.Warn("envoy delete: failed to clean up adapter config dir",
					"agent", agentName, "runtime", runtime, "error", cleanupErr)
			}
			return
		}
	}
	// Fallback: clean up via every registered adapter (idempotent).
	for name, a := range adapter.All() {
		if cleanupErr := a.CleanupConfigDir(worldDir, "envoy", agentName); cleanupErr != nil {
			slog.Warn("envoy delete: failed to clean up adapter config dir (fallback)",
				"agent", agentName, "runtime", name, "error", cleanupErr)
		}
	}
}

// --- Interfaces ---

// SphereStore abstracts sphere store operations for Create.
type SphereStore interface {
	CreateAgent(name, world, role string) (string, error)
	DeleteAgent(id string) error
}

// StopStore abstracts sphere store operations for Stop.
type StopStore interface {
	GetAgent(id string) (*store.Agent, error)
	UpdateAgentState(id, state, activeWrit string) error
}

// ListStore abstracts sphere store operations for List.
type ListStore interface {
	ListAgents(world string, state string) ([]store.Agent, error)
}

// StopManager abstracts session operations for Stop.
//
// Inject and Capture are required so Stop can run sessionsave.Prompt
// (best-effort "save MEMORY.md before I kill you" dance) before tearing
// down the session. *session.Manager satisfies this interface; tests use
// a stub that implements all four methods.
type StopManager interface {
	Exists(name string) bool
	Stop(name string, force bool) error
	Inject(name string, text string, submit bool) error
	Capture(name string, lines int) (string, error)
}

// DeleteStore abstracts sphere store operations for Delete.
type DeleteStore interface {
	GetAgent(id string) (*store.Agent, error)
	DeleteAgent(id string) error
	// UpdateAgentState sets the agent's state and active_writ. Used after killing
	// the session so partial failures leave the agent in a recoverable "idle" state
	// instead of "working" with no session.
	UpdateAgentState(id string, state store.AgentState, activeWrit string) error
	// CreateEscalation records an operator-visible escalation. Used by force-delete
	// when tether enumeration fails so the operator notices the refused action.
	CreateEscalation(severity, source, description string, sourceRef ...string) (string, error)
}

// WritReopener abstracts world store operations needed to reopen orphaned writs.
type WritReopener interface {
	UpdateWrit(id string, updates store.WritUpdates) error
}

// --- Options ---

// CreateOpts holds inputs for creating an envoy.
type CreateOpts struct {
	World      string
	Name       string
	SourceRepo string // path to git repo for worktree
	Persona    string // optional persona template name (resolved via three-tier lookup)
	// EventLogger is optional; when non-nil, rollback failures emit a
	// soft_failure event so chronicle/feed can observe rollback gaps. When
	// nil, soft failures still log to slog but do not surface as events.
	EventLogger softfail.EventEmitter
}

// DeleteOpts holds inputs for deleting an envoy.
type DeleteOpts struct {
	World      string
	Name       string
	SourceRepo string // path to managed repo for git operations
	Force      bool   // override active session / tether checks
	WorldStore WritReopener // optional; used to reopen tethered writs on force-delete
}

// --- Create ---

// Create provisions a new envoy: agent record, directory, and worktree.
// If any step fails after the first, all previous steps are rolled back.
func Create(opts CreateOpts, sphereStore SphereStore) error {
	envoyDir := EnvoyDir(opts.World, opts.Name)
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
		// Remove entire envoy directory (covers worktree dir and memory dir).
		os.RemoveAll(envoyDir)
		// Prune stale worktree references (best-effort).
		pruneCmd := exec.Command("git", "-C", opts.SourceRepo, "worktree", "prune")
		if out, err := pruneCmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "rollback: worktree prune: %s\n", strings.TrimSpace(string(out)))
		}
		// Delete the git branch (best-effort).
		branch := fmt.Sprintf("envoy/%s/%s", opts.World, opts.Name)
		branchCmd := exec.Command("git", "-C", opts.SourceRepo, "branch", "-D", branch)
		if out, err := branchCmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "rollback: branch delete: %s\n", strings.TrimSpace(string(out)))
		}
		// Delete agent record. If this fails, we have an orphaned agent record
		// that will block future creates with the same name and is invisible
		// outside stderr — emit a soft_failure event so chronicle/feed can
		// surface the gap. Stderr is preserved for direct operator visibility.
		if err := sphereStore.DeleteAgent(agentID); err != nil {
			fmt.Fprintf(os.Stderr, "rollback: failed to delete agent record: %v\n", err)
			softfail.Emit(nil, opts.EventLogger, "envoy.create_rollback_delete_agent", err, map[string]any{
				"agent": agentID,
				"world": opts.World,
				"name":  opts.Name,
			})
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

	// 4. Resolve and write persona template (optional).
	if opts.Persona != "" {
		res, err := persona.Resolve(opts.Persona, opts.SourceRepo)
		if err != nil {
			rollback()
			return fmt.Errorf("failed to resolve persona %q: %w", opts.Persona, err)
		}
		personaFile := PersonaPath(opts.World, opts.Name)
		if err := os.WriteFile(personaFile, res.Content, 0o644); err != nil {
			rollback()
			return fmt.Errorf("failed to write persona file: %w", err)
		}
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

// sessionSavePrompt is a package-level indirection for sessionsave.Prompt so
// tests can substitute a fast no-op without waiting on the real 30-second
// default timeout. Production code never overrides it.
var sessionSavePrompt = sessionsave.Prompt

// --- Stop ---

// Stop terminates an envoy session. Does NOT remove the worktree or directory.
//
// Before tearing down the tmux session, Stop runs sessionsave.Prompt: it
// injects a "you are about to be killed, write MEMORY.md now" message into
// the pane and polls until the agent's output goes idle (or a timeout fires).
// Empirically this produces noticeably better memory content than relying on
// Claude Code's native auto-memory shutdown alone — the brief retirement
// (commit c29ca97) removed the dance and operators noticed memory quality
// regressed, so it is back as a best-effort step.
//
// The sessionsave call is intentionally best-effort: if injecting the prompt
// fails (session vanished, tmux glitch, etc.) we log via softfail and proceed
// with the kill anyway. Stop must always be able to make progress.
//
// Memory persists across stop because it lives OUTSIDE the worktree in the
// adapter-managed <envoyDir>/memory/ directory via Claude Code's native
// auto-memory, so the on-disk MEMORY.md the prompt asks the agent to write
// will still be there for the next session that boots in this envoy.
func Stop(world, name string, sphereStore StopStore, mgr StopManager) error {
	agentID := world + "/" + name
	sessName := config.SessionName(world, name)

	// 1. Verify agent exists and is an envoy.
	agent, err := sphereStore.GetAgent(agentID)
	if err != nil {
		return fmt.Errorf("envoy %q not found in world %q: %w", name, world, err)
	}
	if agent.Role != "envoy" {
		return fmt.Errorf("agent %q has role %q, expected \"envoy\"", agentID, agent.Role)
	}

	// 2. Stop the session directly if it exists.
	if mgr.Exists(sessName) {
		// Best-effort: prompt the envoy to flush MEMORY.md before kill.
		// Failure here must not block the stop — log and continue.
		if err := sessionSavePrompt(mgr, sessName, sessionsave.EnvoyStopPrompt, sessionsave.Options{}); err != nil {
			softfail.Log(nil, "envoy stop: sessionsave prompt", err)
		}

		if err := mgr.Stop(sessName, true); err != nil {
			// Session already gone — treat as success and proceed to state update.
			if !errors.Is(err, session.ErrNotFound) {
				// Best-effort: update agent state to idle even when stop fails.
				// The session may already be dead; keeping state="working" triggers spurious Prefect respawns.
				stopErr := fmt.Errorf("failed to stop envoy %q in world %q: %w", name, world, err)
				if stateErr := sphereStore.UpdateAgentState(agentID, store.AgentIdle, agent.ActiveWrit); stateErr != nil {
					return errors.Join(stopErr, fmt.Errorf("failed to update agent state: %w", stateErr))
				}
				return stopErr
			}
		}
	}

	// 3. Update agent state to idle, preserving active_writ so restart context is retained.
	if err := sphereStore.UpdateAgentState(agentID, store.AgentIdle, agent.ActiveWrit); err != nil {
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
// deletes git branch, removes the agent record, and finally invokes the
// runtime adapter's CleanupConfigDir to reap per-agent .claude-config or
// .codex-home state. The adapter cleanup is scoped to permanent termination —
// envoy.Stop, handoff, and resolve do NOT run it because envoy memory and
// auth.json must persist across normal lifecycle events.
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

	// 2. Enumerate tethers BEFORE killing the session.
	//
	// tether.List is a read-only filesystem operation that does not require the
	// session to be dead. Enumerating first ensures that if the enumeration fails
	// (NFS glitch, permission issue, corrupted path) we bail out without having
	// killed the session — avoiding a broken state where the session is dead but
	// the agent record persists as "working" with unknown tether state.
	//
	// If we cannot enumerate the tethered writs, we MUST refuse the delete
	// (even with --force) because the force path's contract is "reopen the
	// orphaned writs before clearing the tether" — which is impossible if we
	// cannot list them. The operator must fix the underlying FS error and retry.
	writIDs, listErr := tether.List(opts.World, opts.Name, "envoy")
	if listErr != nil {
		if opts.Force {
			descr := fmt.Sprintf(
				"envoy %q in world %q: cannot enumerate tethered writs to reopen them; refused force-delete to avoid orphaning. Underlying error: %v",
				opts.Name, opts.World, listErr)
			if _, escErr := sphereStore.CreateEscalation("high", "envoy.delete", descr, "envoy:"+agentID); escErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to create escalation for tether enumeration failure: %v\n", escErr)
			}
		}
		return fmt.Errorf("cannot enumerate tether for envoy %q in world %q (fix the underlying error and retry): %w",
			opts.Name, opts.World, listErr)
	}

	// 3. Check for active session.
	if mgr.Exists(sessName) {
		if !opts.Force {
			return fmt.Errorf("envoy %q has an active session — stop it first or use --force", opts.Name)
		}
		fmt.Fprintf(os.Stderr, "Stopping active session for envoy %q\n", opts.Name)
		if err := mgr.Stop(sessName, true); err != nil {
			// Session vanished between Exists and Stop (TOCTOU race) — treat as
			// success and proceed with cleanup.
			if !errors.Is(err, session.ErrNotFound) {
				return fmt.Errorf("failed to stop envoy session: %w", err)
			}
		}
		// Update agent state to idle after killing the session. This matches
		// envoy.Stop behavior and ensures partial failures (e.g. during tether
		// clearing or worktree removal below) leave the agent in a recoverable
		// "idle" state instead of "working" with no session.
		if err := sphereStore.UpdateAgentState(agentID, store.AgentIdle, agent.ActiveWrit); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to update agent state to idle after session kill: %v\n", err)
		}
	}
	// 4. Handle tethered writs (enumeration already done in step 2).
	if len(writIDs) > 0 {
		if !opts.Force {
			return fmt.Errorf("envoy %q is tethered to %s — clear tether first or use --force", opts.Name, strings.Join(writIDs, ", "))
		}

		// Reopen tethered writs before clearing the tether so they don't get orphaned.
		// If no WorldStore was provided, open one ourselves so force-delete
		// never silently orphans writs.
		ws := opts.WorldStore
		if ws == nil {
			opened, openErr := store.OpenWorld(opts.World)
			if openErr != nil {
				return fmt.Errorf("cannot reopen tethered writs for envoy %q (world store failed to open): %w", opts.Name, openErr)
			}
			defer opened.Close()
			ws = opened
		}
		for _, writID := range writIDs {
			if err := ws.UpdateWrit(writID, store.WritUpdates{
				Status:   "open",
				Assignee: "-",
			}); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to reopen writ %q: %v\n", writID, err)
			} else {
				fmt.Fprintf(os.Stderr, "Reopened tethered writ %q\n", writID)
			}
		}

		fmt.Fprintf(os.Stderr, "Clearing tether for envoy %q\n", opts.Name)
		if err := tether.Clear(opts.World, opts.Name, "envoy"); err != nil {
			return fmt.Errorf("failed to clear tether: %w", err)
		}
	}

	// 5. Remove git worktree (before DB deletion so record survives if cleanup fails).
	worktreeDir := WorktreePath(opts.World, opts.Name)
	if _, err := os.Stat(worktreeDir); err == nil {
		rmCmd := exec.Command("git", "-C", opts.SourceRepo, "worktree", "remove", "--force", worktreeDir)
		if out, err := rmCmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: worktree remove failed: %s\n", strings.TrimSpace(string(out)))
			// Fallback: remove directory directly.
			if removeErr := os.RemoveAll(worktreeDir); removeErr != nil {
				return fmt.Errorf("failed to remove worktree dir %q (manual cleanup required): %w", worktreeDir, removeErr)
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
		return fmt.Errorf("failed to remove envoy directory %q (manual cleanup required): %w", envoyDir, err)
	}

	// 7. Delete the git branch (best-effort).
	branch := fmt.Sprintf("envoy/%s/%s", opts.World, opts.Name)
	branchCmd := exec.Command("git", "-C", opts.SourceRepo, "branch", "-D", branch)
	if out, err := branchCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: branch delete failed: %s\n", strings.TrimSpace(string(out)))
	}

	// 8. Delete the agent record AFTER filesystem cleanup succeeds.
	if err := sphereStore.DeleteAgent(agentID); err != nil {
		return fmt.Errorf("failed to delete agent record: %w", err)
	}

	// 9. Remove runtime adapter config dirs for the terminated envoy.
	// Closes the lifecycle opened by EnsureConfigDir; without this, the
	// .claude-config or .codex-home tree (the latter contains auth.json with
	// credentials) lingers on disk and a future envoy create with the same
	// name would inherit the previous tenant's credentials.
	//
	// Best-effort: any failure is logged via slog inside the helper. Done
	// AFTER the agent record is deleted to keep the lifecycle ordering
	// consistent with the rest of Delete (filesystem cleanup precedes DB
	// deletion; adapter cleanup is the inverse of EnsureConfigDir, which
	// runs at session start AFTER the agent record exists).
	cleanupEnvoyConfigDir(opts.World, opts.Name)

	return nil
}
