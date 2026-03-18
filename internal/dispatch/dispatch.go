package dispatch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/envoy"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/governor"
	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/namepool"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/nevinsm/sol/internal/workflow"
)

// ErrCapacityExhausted is returned when a world has reached its agent capacity
// and no more agents can be provisioned. Use errors.Is to check for this error.
var ErrCapacityExhausted = errors.New("agent capacity exhausted")

// Git operation timeout constants.
const (
	GitPushTimeout           = 60 * time.Second // network-bound
	GitFetchTimeout          = 60 * time.Second // network-bound
	GitWorktreeAddTimeout    = 30 * time.Second // local
	GitWorktreeRemoveTimeout = 30 * time.Second // local
	GitCheckoutTimeout       = 15 * time.Second // local
	GitLocalOpTimeout        = 30 * time.Second // git add, commit, prune, rev-parse
)

// SessionManager is the canonical session manager interface.
// Alias to session.SessionManager for backward compatibility with
// external references (consul, integration tests).
type SessionManager = session.SessionManager

// WorldStore defines the world store operations used by dispatch.
type WorldStore interface {
	GetWrit(id string) (*store.Writ, error)
	UpdateWrit(id string, updates store.WritUpdates) error
	CreateMergeRequest(writID, branch string, priority int) (string, error)
	ListMergeRequestsByWrit(writID, phase string) ([]store.MergeRequest, error)
	UpdateMergeRequestPhase(id, phase string) error
	CreateWritWithOpts(opts store.CreateWritOpts) (string, error)
	FindMergeRequestByBlocker(blockerID string) (*store.MergeRequest, error)
	UnblockMergeRequest(mrID string) error
	ResetMergeRequestForRetry(mrID string) error
	CloseWrit(id string, closeReason ...string) ([]string, error)
	ListChildWrits(parentID string) ([]store.Writ, error)
	ListAgentMemories(agentName string) ([]store.AgentMemory, error)
	WriteHistory(agentName, writID, action, summary string, startedAt time.Time, endedAt *time.Time) (string, error)
	EndHistory(writID string) (string, error)
	GetDependencies(itemID string) ([]string, error)
	Close() error
}

// SphereStore defines the sphere store operations used by dispatch.
type SphereStore interface {
	GetAgent(id string) (*store.Agent, error)
	FindIdleAgent(world string) (*store.Agent, error)
	UpdateAgentState(id, state, activeWrit string) error
	ListAgents(world string, state string) ([]store.Agent, error)
	CreateAgent(name, world, role string) (string, error)
	DeleteAgent(id string) error
	ListEscalationsBySourceRef(sourceRef string) ([]store.Escalation, error)
	ResolveEscalation(id string) error
	Close() error
}

// WorktreePath returns the worktree directory for an agent.
func WorktreePath(world, agentName string) string {
	return config.WorktreePath(world, agentName)
}

// cleanupWorktree removes a git worktree and prunes stale references.
// Best-effort: logs what was cleaned up but does not fail.
// Uses its own background context since it may run in a goroutine after
// the parent context has been cancelled.
func cleanupWorktree(world, worktreeDir string) {
	repoPath := config.RepoPath(world)

	rmCtx, rmCancel := context.WithTimeout(context.Background(), GitWorktreeRemoveTimeout)
	defer rmCancel()
	rmCmd := exec.CommandContext(rmCtx, "git", "-C", repoPath, "worktree", "remove", "--force", worktreeDir)
	if out, err := rmCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "resolve: worktree remove failed: %s: %v\n",
			strings.TrimSpace(string(out)), err)
		// Fallback: remove directory directly (matches cast cleanup pattern).
		if removeErr := os.RemoveAll(worktreeDir); removeErr != nil {
			fmt.Fprintf(os.Stderr, "resolve: failed to remove worktree dir %s: %v\n", worktreeDir, removeErr)
			return
		}
	}
	fmt.Fprintf(os.Stderr, "resolve: cleaned up worktree %s\n", worktreeDir)

	pruneCtx, pruneCancel := context.WithTimeout(context.Background(), GitLocalOpTimeout)
	defer pruneCancel()
	pruneCmd := exec.CommandContext(pruneCtx, "git", "-C", repoPath, "worktree", "prune")
	if out, err := pruneCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "resolve: worktree prune failed: %s: %v\n",
			strings.TrimSpace(string(out)), err)
	}
}

// CastResult holds the output of a successful cast operation.
type CastResult struct {
	WritID  string
	AgentName   string
	SessionName string
	WorktreeDir string
	Workflow    string // empty if no workflow
}

// CastOpts holds the inputs for a cast operation.
type CastOpts struct {
	WritID  string
	World       string
	AgentName   string              // optional: if empty, find an idle agent
	SourceRepo  string              // path to the source git repo
	Workflow    string              // optional: workflow name to instantiate
	Variables   map[string]string   // optional: workflow variables
	WorldConfig *config.WorldConfig // optional: pre-loaded config (avoids double load)
	Account     string              // optional: explicit account override for credential provisioning
}

// Cast assigns a writ to an outpost agent and starts its session.
// Supports re-cast (crash recovery): if the item is already tethered to the
// same agent, Cast recreates the worktree and session without error.
// The logger parameter is optional — if nil, no events are emitted.
func Cast(ctx context.Context, opts CastOpts, worldStore WorldStore, sphereStore SphereStore, mgr SessionManager, logger *events.Logger) (*CastResult, error) {
	// 0. Load world config once for all consumers.
	var worldCfg config.WorldConfig
	if opts.WorldConfig != nil {
		worldCfg = *opts.WorldConfig
	} else {
		var err error
		worldCfg, err = config.LoadWorldConfig(opts.World)
		if err != nil {
			return nil, fmt.Errorf("failed to load world config for %q: %w", opts.World, err)
		}
	}

	// 0b. Reject dispatch to sleeping worlds.
	if worldCfg.World.Sleeping {
		return nil, fmt.Errorf("world %q is sleeping: dispatch blocked", opts.World)
	}

	// 1. Acquire per-writ advisory lock to prevent double dispatch.
	lock, err := AcquireWritLock(opts.WritID)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	// 2. Get writ.
	item, err := worldStore.GetWrit(opts.WritID)
	if err != nil {
		return nil, fmt.Errorf("failed to get writ %q: %w", opts.WritID, err)
	}

	// 3. Find the agent.
	var agent *store.Agent
	if opts.AgentName != "" {
		agentID := opts.World + "/" + opts.AgentName
		agent, err = sphereStore.GetAgent(agentID)
		if err != nil {
			return nil, fmt.Errorf("failed to get agent %q: %w", agentID, err)
		}
		if agent.Role != "outpost" {
			return nil, fmt.Errorf("cannot dispatch to %s agents — sol cast targets outpost agents only (got %s)", agent.Role, agent.Name)
		}
	} else {
		if item.Status != "open" {
			return nil, fmt.Errorf("writ %q has status %q, expected \"open\"", opts.WritID, item.Status)
		}
		agent, err = sphereStore.FindIdleAgent(opts.World)
		if err != nil {
			return nil, fmt.Errorf("failed to find idle agent for world %q: %w", opts.World, err)
		}
		if agent == nil {
			// Auto-provision a new agent from the name pool.
			agent, err = autoProvision(opts.World, sphereStore, worldCfg.Agents.NamePoolPath, worldCfg.Agents.Capacity)
			if err != nil {
				return nil, err
			}
		}
	}

	agentID := opts.World + "/" + agent.Name

	// Acquire per-agent lock to prevent concurrent dispatch to same agent.
	agentLock, err := AcquireAgentLock(agentID)
	if err != nil {
		return nil, err
	}
	defer agentLock.Release()

	// Re-read agent state inside the locked section to avoid a TOCTOU race:
	// FindIdleAgent was called before lock acquisition, so a concurrent Cast
	// may have selected the same idle agent, dispatched it, and updated its
	// state between our FindIdleAgent call and lock acquisition above.
	agent, err = sphereStore.GetAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to re-read agent %q: %w", agentID, err)
	}

	// 4. Determine if this is a re-cast (crash recovery).
	// Full match: all four fields consistent (clean re-cast).
	// Partial match: writ is tethered to this agent but agent state is stale.
	// This handles crashes between writ update and agent state update.
	reCast := false
	if item.Status == "tethered" && item.Assignee == agentID {
		if agent.State == "working" && agent.ActiveWrit == opts.WritID {
			reCast = true // clean re-cast
		} else if agent.State == "idle" && (agent.ActiveWrit == "" || agent.ActiveWrit == opts.WritID) {
			reCast = true // partial failure recovery — agent wasn't updated
		}
	}

	// 5. Validate state.
	if !reCast {
		if item.Status != "open" {
			return nil, fmt.Errorf("writ %q has status %q, expected \"open\"", opts.WritID, item.Status)
		}
		if agent.State != "idle" {
			return nil, fmt.Errorf("agent %q has state %q, expected \"idle\"", agentID, agent.State)
		}
	}

	worktreeDir := WorktreePath(opts.World, agent.Name)
	sessName := config.SessionName(opts.World, agent.Name)
	branchName := fmt.Sprintf("outpost/%s/%s", agent.Name, opts.WritID)

	// Clean up any stale session (race between resolve teardown and next cast,
	// crashed agents, interrupted stops, etc.).
	if mgr.Exists(sessName) {
		if err := mgr.Stop(sessName, true); err != nil {
			fmt.Fprintf(os.Stderr, "cast: warning: failed to stop stale session %q: %v\n", sessName, err)
		}
	}

	// 5. Create worktree directory.
	// Remove existing worktree if present.
	if _, err := os.Stat(worktreeDir); err == nil {
		rmCtx, rmCancel := context.WithTimeout(ctx, GitWorktreeRemoveTimeout)
		rmCmd := exec.CommandContext(rmCtx, "git", "-C", opts.SourceRepo, "worktree", "remove", "--force", worktreeDir)
		rmCmd.Run() // best-effort
		rmCancel()
		os.RemoveAll(worktreeDir)
	}
	// Prune stale worktree references.
	pruneCtx, pruneCancel := context.WithTimeout(ctx, GitLocalOpTimeout)
	pruneCmd := exec.CommandContext(pruneCtx, "git", "-C", opts.SourceRepo, "worktree", "prune")
	pruneCmd.Run()
	pruneCancel()

	// Try creating worktree with new branch; fall back to existing branch (re-cast).
	addCtx, addCancel := context.WithTimeout(ctx, GitWorktreeAddTimeout)
	defer addCancel()
	addCmd := exec.CommandContext(addCtx, "git", "-C", opts.SourceRepo, "worktree", "add", worktreeDir, "-b", branchName, "HEAD")
	if out, err := addCmd.CombinedOutput(); err != nil {
		addCtx2, addCancel2 := context.WithTimeout(ctx, GitWorktreeAddTimeout)
		defer addCancel2()
		addCmd2 := exec.CommandContext(addCtx2, "git", "-C", opts.SourceRepo, "worktree", "add", worktreeDir, branchName)
		if out2, err2 := addCmd2.CombinedOutput(); err2 != nil {
			return nil, fmt.Errorf("failed to create worktree: %s: %w", strings.TrimSpace(string(out2)), err2)
		}
		_ = out // suppress unused
	}

	// From here on, rollback on failure.
	// Undo in reverse order of: (1) agent→working, (2) tether.Write, (3) writ→tethered.
	rollback := func() {
		if err := worldStore.UpdateWrit(opts.WritID, store.WritUpdates{Status: "open", Assignee: "-"}); err != nil {
			fmt.Fprintf(os.Stderr, "rollback: failed to reset writ: %v\n", err)
		}
		if err := tether.Clear(opts.World, agent.Name, "outpost"); err != nil {
			fmt.Fprintf(os.Stderr, "rollback: failed to clear tether: %v\n", err)
		}
		if err := sphereStore.UpdateAgentState(agent.ID, "idle", ""); err != nil {
			fmt.Fprintf(os.Stderr, "rollback: failed to reset agent state: %v\n", err)
		}
		rbCtx, rbCancel := context.WithTimeout(context.Background(), GitWorktreeRemoveTimeout)
		rmCmd := exec.CommandContext(rbCtx, "git", "-C", opts.SourceRepo, "worktree", "remove", "--force", worktreeDir)
		if out, err := rmCmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "rollback: failed to remove worktree: %s\n", strings.TrimSpace(string(out)))
		}
		rbCancel()
		// Clean up workflow if it was instantiated.
		workflow.Remove(opts.World, agent.Name, "outpost") // best-effort
	}

	// 4. Update agent: state → working, active_writ → writ ID.
	// Done BEFORE tether.Write() to prevent a race with sentinel's
	// cleanupOrphanedTethers, which clears tether files for non-working agents.
	// If we wrote the tether first while agent is still "idle", a concurrent
	// sentinel patrol could clear it before we update agent state.
	if err := sphereStore.UpdateAgentState(agent.ID, "working", opts.WritID); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to update agent state: %w", err)
	}

	// 5. Write tether file.
	if err := tether.Write(opts.World, agent.Name, opts.WritID, "outpost"); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to write tether: %w", err)
	}

	// 6. Update writ: status → tethered, assignee → agent ID.
	if err := worldStore.UpdateWrit(opts.WritID, store.WritUpdates{
		Status:   "tethered",
		Assignee: agent.ID,
	}); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to update writ: %w", err)
	}

	// 6b. Create persistent output directory for the writ.
	// Lives in world storage (not the worktree) and survives worktree cleanup.
	outputDir := config.WritOutputDir(opts.World, opts.WritID)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to create writ output directory: %w", err)
	}

	// 7. Instantiate workflow if provided (before Launch so persona
	// can detect the active workflow).
	if opts.Workflow != "" {
		vars := opts.Variables
		if vars == nil {
			vars = map[string]string{}
		}
		// Always set "issue" variable to the writ ID.
		if _, ok := vars["issue"]; !ok {
			vars["issue"] = opts.WritID
		}
		if _, _, err := workflow.Instantiate(opts.World, agent.Name, "outpost", opts.Workflow, vars); err != nil {
			rollback()
			return nil, fmt.Errorf("failed to instantiate workflow %q: %w", opts.Workflow, err)
		}
	}

	// 8. Launch the session via startup.Launch (persona, hooks, config dir,
	// system prompt, prime, command building, session start).
	launchCfg := OutpostRoleConfig()
	launchOpts := startup.LaunchOpts{
		Account:  opts.Account,
		Sessions: mgr,
		Sphere:   sphereStore,
	}
	if _, err := startup.Launch(launchCfg, opts.World, agent.Name, launchOpts); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to launch session: %w", err)
	}

	castPayload := map[string]string{
		"writ_id": opts.WritID,
		"agent":        agent.Name,
		"world":        opts.World,
	}
	if logger != nil {
		logger.Emit(events.EventCast, "sol", config.Autarch, "both", castPayload)
	}

	// Emit workflow instantiation event if workflow was used.
	if opts.Workflow != "" && logger != nil {
		logger.Emit(events.EventWorkflowInstantiate, "sol", config.Autarch, "both", map[string]string{
			"workflow":     opts.Workflow,
			"writ_id": opts.WritID,
			"agent":        agent.Name,
			"world":        opts.World,
		})
	}

	// Write history record for cycle-time tracking.
	if _, err := worldStore.WriteHistory(agent.Name, opts.WritID, "cast", "", time.Now(), nil); err != nil {
		fmt.Fprintf(os.Stderr, "cast: failed to write history: %v\n", err)
	}

	return &CastResult{
		WritID:  opts.WritID,
		AgentName:   agent.Name,
		SessionName: sessName,
		WorktreeDir: worktreeDir,
		Workflow:    opts.Workflow,
	}, nil
}

// persistentRoles are agent roles that can use sol tether/untether.
// Outpost agents must use sol cast instead.
var persistentRoles = map[string]bool{
	"envoy":    true,
	"governor": true,
	"forge":    true,
}

// TetherResult holds the output of a successful tether operation.
type TetherResult struct {
	WritID    string
	AgentName string
	AgentRole string
}

// TetherOpts holds the inputs for a tether operation.
type TetherOpts struct {
	AgentName string
	WritID    string
	World     string
}

// Tether binds a writ to a persistent agent without creating worktrees or sessions.
// Rejects outpost agents (use Cast instead). Supports multiple concurrent tethers
// per agent — only sets active_writ if no current active writ exists.
// The logger parameter is optional — if nil, no events are emitted.
func Tether(opts TetherOpts, worldStore WorldStore, sphereStore SphereStore, logger *events.Logger) (*TetherResult, error) {
	agentID := opts.World + "/" + opts.AgentName

	// 1. Verify writ exists and is open.
	item, err := worldStore.GetWrit(opts.WritID)
	if err != nil {
		return nil, fmt.Errorf("failed to get writ %q: %w", opts.WritID, err)
	}
	if item.Status != "open" {
		return nil, fmt.Errorf("writ %q has status %q, expected \"open\"", opts.WritID, item.Status)
	}

	// 2. Verify agent exists and has a persistent role.
	agent, err := sphereStore.GetAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", agentID, err)
	}
	if !persistentRoles[agent.Role] {
		return nil, fmt.Errorf("agent %q has role %q — only persistent roles (envoy, governor, forge) can use tether; outposts use sol cast", agentID, agent.Role)
	}

	// 3. Acquire per-writ lock, then per-agent lock (consistent ordering).
	lock, err := AcquireWritLock(opts.WritID)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	agentLock, err := AcquireAgentLock(agentID)
	if err != nil {
		return nil, err
	}
	defer agentLock.Release()

	// Re-read agent state inside the locked section to avoid a TOCTOU race:
	// another concurrent Tether may have changed agent state between step 2
	// and the lock acquisition above. The rollback at step 5 uses these
	// values, so they must reflect the state at lock time.
	agent, err = sphereStore.GetAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to re-read agent %q: %w", agentID, err)
	}

	// 4. Update agent: set working (if was idle), set active_writ only if none.
	// Done BEFORE tether.Write() to prevent a race with sentinel's
	// cleanupOrphanedTethers, which skips tether files for known agents.
	prevState := agent.State
	prevActiveWrit := agent.ActiveWrit
	if agent.State == "idle" {
		if err := sphereStore.UpdateAgentState(agentID, "working", opts.WritID); err != nil {
			return nil, fmt.Errorf("failed to update agent state: %w", err)
		}
	} else if agent.ActiveWrit == "" {
		// Already working but no active writ — set this as active.
		if err := sphereStore.UpdateAgentState(agentID, agent.State, opts.WritID); err != nil {
			return nil, fmt.Errorf("failed to update agent active writ: %w", err)
		}
	}
	// If already working with an active_writ, leave it unchanged.

	// 5. Create tether file in agent tether directory.
	if err := tether.Write(opts.World, opts.AgentName, opts.WritID, agent.Role); err != nil {
		// Rollback agent state.
		sphereStore.UpdateAgentState(agentID, prevState, prevActiveWrit)
		return nil, fmt.Errorf("failed to write tether: %w", err)
	}

	// 6. Update writ: status → tethered, assignee → agent ID.
	if err := worldStore.UpdateWrit(opts.WritID, store.WritUpdates{
		Status:   "tethered",
		Assignee: agent.ID,
	}); err != nil {
		// Rollback tether + agent state (reverse order).
		tether.ClearOne(opts.World, opts.AgentName, opts.WritID, agent.Role)
		sphereStore.UpdateAgentState(agentID, prevState, prevActiveWrit)
		return nil, fmt.Errorf("failed to update writ: %w", err)
	}

	// 7. Emit event.
	if logger != nil {
		logger.Emit(events.EventTether, "sol", config.Autarch, "both", map[string]string{
			"writ_id": opts.WritID,
			"agent":   opts.AgentName,
			"world":   opts.World,
			"role":    agent.Role,
		})
	}

	return &TetherResult{
		WritID:    opts.WritID,
		AgentName: opts.AgentName,
		AgentRole: agent.Role,
	}, nil
}

// UntetherResult holds the output of a successful untether operation.
type UntetherResult struct {
	WritID    string
	AgentName string
	AgentRole string
}

// UntetherOpts holds the inputs for an untether operation.
type UntetherOpts struct {
	AgentName string
	WritID    string
	World     string
}

// Untether unbinds a specific writ from an agent without stopping sessions.
// Removes only the specified tether file. If no tethers remain, agent goes idle.
// If the untethered writ was the active_writ, clears it from the DB.
// The logger parameter is optional — if nil, no events are emitted.
func Untether(opts UntetherOpts, worldStore WorldStore, sphereStore SphereStore, logger *events.Logger) (*UntetherResult, error) {
	agentID := opts.World + "/" + opts.AgentName

	// 1. Get agent (needed for role-aware tether path).
	agent, err := sphereStore.GetAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", agentID, err)
	}

	// 2. Acquire locks: writ first, then agent (consistent ordering).
	lock, err := AcquireWritLock(opts.WritID)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	agentLock, err := AcquireAgentLock(agentID)
	if err != nil {
		return nil, err
	}
	defer agentLock.Release()

	// 3. Verify writ is tethered to this agent (inside locked region to avoid
	// a TOCTOU race: a concurrent clear between the check and lock acquisition
	// could cause a spurious error or double-clear).
	if !tether.IsTetheredTo(opts.World, opts.AgentName, opts.WritID, agent.Role) {
		return nil, fmt.Errorf("writ %q is not tethered to agent %q in world %q", opts.WritID, opts.AgentName, opts.World)
	}

	// 4. Remove single tether file.
	if err := tether.ClearOne(opts.World, opts.AgentName, opts.WritID, agent.Role); err != nil {
		return nil, fmt.Errorf("failed to clear tether: %w", err)
	}

	// 5. Update writ: status → open, assignee → clear.
	if err := worldStore.UpdateWrit(opts.WritID, store.WritUpdates{
		Status:   "open",
		Assignee: "-",
	}); err != nil {
		return nil, fmt.Errorf("failed to update writ: %w", err)
	}

	// 6. If this was the active_writ, clear it.
	// If no remaining tethers, set agent to idle.
	remaining, err := tether.List(opts.World, opts.AgentName, agent.Role)
	if err != nil {
		return nil, fmt.Errorf("failed to list remaining tethers: %w", err)
	}

	if len(remaining) == 0 {
		// No more tethers — go idle.
		if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
			return nil, fmt.Errorf("failed to update agent state: %w", err)
		}
	} else if agent.ActiveWrit == opts.WritID {
		// Active writ was untethered — clear it but stay working.
		if err := sphereStore.UpdateAgentState(agentID, "working", ""); err != nil {
			return nil, fmt.Errorf("failed to clear active writ: %w", err)
		}
	}

	// 7. Emit event.
	if logger != nil {
		logger.Emit(events.EventUntether, "sol", config.Autarch, "both", map[string]string{
			"writ_id": opts.WritID,
			"agent":   opts.AgentName,
			"world":   opts.World,
			"role":    agent.Role,
		})
	}

	return &UntetherResult{
		WritID:    opts.WritID,
		AgentName: opts.AgentName,
		AgentRole: agent.Role,
	}, nil
}

// ActivateResult holds the output of a successful activate operation.
type ActivateResult struct {
	WritID        string // newly active writ
	PreviousWrit  string // previously active writ (empty if none)
	AlreadyActive bool   // true if writID was already active (no-op)
}

// ActivateOpts holds the inputs for an activate operation.
type ActivateOpts struct {
	World     string
	AgentName string
	WritID    string // writ to activate
}

// ActivateWrit switches the active writ for a persistent agent.
// The writ must already be tethered to the agent. If the writ is already active,
// this is a no-op (idempotent). Otherwise, updates active_writ in the DB,
// writes a resume state with writ-switch context, and triggers a session
// restart with --continue for conversation continuity.
//
// The logger parameter is optional — if nil, no events are emitted.
func ActivateWrit(opts ActivateOpts, worldStore WorldStore, sphereStore SphereStore, mgr SessionManager, logger *events.Logger) (*ActivateResult, error) {
	agentID := opts.World + "/" + opts.AgentName

	// 1. Look up agent.
	agent, err := sphereStore.GetAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", agentID, err)
	}

	// 2. Validate that the writ is tethered to this agent.
	if !tether.IsTetheredTo(opts.World, opts.AgentName, opts.WritID, agent.Role) {
		return nil, fmt.Errorf("writ %q is not tethered to agent %q in world %q", opts.WritID, opts.AgentName, opts.World)
	}

	// 3. Check if already active (idempotent).
	if agent.ActiveWrit == opts.WritID {
		return &ActivateResult{
			WritID:        opts.WritID,
			PreviousWrit:  opts.WritID,
			AlreadyActive: true,
		}, nil
	}

	previousWrit := agent.ActiveWrit

	// 4. Acquire per-agent lock.
	agentLock, err := AcquireAgentLock(agentID)
	if err != nil {
		return nil, err
	}
	defer agentLock.Release()

	// Re-read agent state inside the locked section to avoid a TOCTOU race:
	// a concurrent state change between the pre-lock GetAgent and lock
	// acquisition above could mean agent.State is stale. UpdateAgentState
	// must use the current state, not the pre-lock snapshot.
	agent, err = sphereStore.GetAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to re-read agent %q: %w", agentID, err)
	}
	previousWrit = agent.ActiveWrit

	// 5. Update active_writ in DB.
	if err := sphereStore.UpdateAgentState(agentID, agent.State, opts.WritID); err != nil {
		return nil, fmt.Errorf("failed to update active writ: %w", err)
	}

	// 6. For persistent roles (envoy, governor), nudge the running session
	// instead of cycling it — cycling destroys the live conversation.
	if persistentRoles[agent.Role] && agent.Role != "forge" {
		sessionName := config.SessionName(opts.World, opts.AgentName)

		// Look up writ title for the nudge message.
		writTitle := opts.WritID // fallback to ID if lookup fails
		if writ, err := worldStore.GetWrit(opts.WritID); err == nil {
			writTitle = writ.Title
		}

		msg := nudge.Message{
			Sender:   "sol",
			Type:     "writ-activate",
			Subject:  fmt.Sprintf("Writ %s activated: %s — commit normally, `sol resolve` handles branch creation. Run `sol prime --world=%s --agent=%s` for full context", opts.WritID, writTitle, opts.World, opts.AgentName),
			Priority: "urgent",
		}
		if err := nudge.Deliver(sessionName, msg); err != nil {
			// Non-fatal: the DB is updated, the nudge queue will deliver later.
			fmt.Fprintf(os.Stderr, "activate: nudge delivery failed: %v\n", err)
		}
	} else {
		// 6b. Outpost/forge: write resume state and cycle session.
		resumeState := startup.ResumeState{
			Reason:             "writ-switch",
			PreviousActiveWrit: previousWrit,
			NewActiveWrit:      opts.WritID,
		}
		if err := startup.WriteResumeState(opts.World, opts.AgentName, agent.Role, resumeState); err != nil {
			return nil, fmt.Errorf("failed to write resume state: %w", err)
		}

		// 7. Trigger session restart via handoff with writ-switch reason.
		cfg := startup.ConfigFor(agent.Role)
		if cfg != nil {
			// Build a cycle operation: respawn-pane for atomic session replacement.
			cycleOp := func(name, workdir, cmd string, env map[string]string, role, world string) error {
				if err := mgr.Cycle(name, workdir, cmd, env, role, world); err != nil {
					// Fallback: stop + start.
					mgr.Stop(name, true)
					return mgr.Start(name, workdir, cmd, env, role, world)
				}
				return nil
			}

			launchOpts := startup.LaunchOpts{
				SessionOp: cycleOp,
			}

			if _, err := startup.Resume(*cfg, opts.World, opts.AgentName, resumeState, launchOpts); err != nil {
				// Non-fatal: the DB is updated, the resume state is on disk.
				// Prefect or next session start will pick it up.
				fmt.Fprintf(os.Stderr, "activate: session restart failed (resume state preserved): %v\n", err)
			} else {
				// Clear resume state only after successful consumption.
				startup.ClearResumeState(opts.World, opts.AgentName, agent.Role)
			}
		}
	}

	// 8. Emit event.
	if logger != nil {
		logger.Emit(events.EventWritActivate, "sol", config.Autarch, "both", map[string]string{
			"writ_id":       opts.WritID,
			"previous_writ": previousWrit,
			"agent":         opts.AgentName,
			"world":         opts.World,
			"role":          agent.Role,
		})
	}

	return &ActivateResult{
		WritID:       opts.WritID,
		PreviousWrit: previousWrit,
	}, nil
}

// autoProvision creates a new agent from the name pool.
func autoProvision(world string, sphereStore SphereStore, namePoolPath string, capacity int) (*store.Agent, error) {
	overridePath := namePoolPath
	if overridePath == "" {
		overridePath = filepath.Join(config.Home(), world, "names.txt")
	}
	pool, err := namepool.Load(overridePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load name pool: %w", err)
	}

	agents, err := sphereStore.ListAgents(world, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list agents for world %q: %w", world, err)
	}

	// Enforce agent capacity.
	if capacity > 0 && len(agents) >= capacity {
		return nil, fmt.Errorf("world %q has reached agent capacity (%d): %w", world, capacity, ErrCapacityExhausted)
	}

	usedNames := make([]string, len(agents))
	for i, a := range agents {
		usedNames[i] = a.Name
	}

	name, err := pool.AllocateName(usedNames)
	if err != nil {
		return nil, err
	}

	id, err := sphereStore.CreateAgent(name, world, "outpost")
	if err != nil {
		return nil, fmt.Errorf("failed to create agent %q: %w", name, err)
	}

	return &store.Agent{
		ID:    id,
		Name:  name,
		World: world,
		Role:  "outpost",
		State: "idle",
	}, nil
}

// PrimeResult holds the output of a prime operation.
type PrimeResult struct {
	Output string
}

// Prime assembles execution context from durable state and returns it.
func Prime(world, agentName, role string, worldStore WorldStore, compact ...bool) (*PrimeResult, error) {
	if role == "" {
		role = "outpost"
	}

	// Compact mode: short focus reminder during native context compaction.
	if len(compact) > 0 && compact[0] {
		return primeCompact(world, agentName, role, worldStore)
	}

	// Forge gets a special prime context.
	if role == "forge" {
		return primeForge(world)
	}

	// Check for stale resolve lock(s) (previous session died mid-resolve).
	if IsResolveInProgress(world, agentName, role) {
		ClearResolveLocksForAgent(world, agentName, role) // clean up stale lock(s)
		fmt.Fprintf(os.Stderr, "prime: detected stale resolve lock — previous session interrupted during resolve\n")
	}

	// Check for handoff marker (loop prevention).
	// If present, the agent was just handed off — prepend a warning and remove the marker.
	// The reason distinguishes compact recovery ("compact") from other handoffs.
	freshSession := false
	compactRecovery := false
	markerTS, markerReason, _ := handoff.ReadMarker(world, agentName, role)
	if !markerTS.IsZero() {
		freshSession = true
		compactRecovery = markerReason == "compact"
		// Remove marker after reading — the message will be in Claude's context.
		handoff.RemoveMarker(world, agentName, role)
	}

	// Read all tethered writs (directory-based).
	allWritIDs, err := tether.List(world, agentName, role)
	if err != nil {
		return nil, fmt.Errorf("failed to list tethers: %w", err)
	}
	if len(allWritIDs) == 0 {
		return &PrimeResult{Output: "No work tethered"}, nil
	}

	// Determine the active writ ID.
	// For outpost agents (role="outpost"): single tether, always active.
	// For persistent agents: read active_writ from sphere store.
	var activeWritID string
	isPersistent := persistentRoles[role]

	if isPersistent {
		activeWritID = readActiveWrit(world, agentName)
		// Validate active writ is actually tethered.
		if activeWritID != "" {
			found := false
			for _, id := range allWritIDs {
				if id == activeWritID {
					found = true
					break
				}
			}
			if !found {
				fmt.Fprintf(os.Stderr, "prime: active_writ %s not in tether list — clearing\n", activeWritID)
				activeWritID = ""
			}
		}
	} else {
		// Outpost: single tether is always the active writ.
		activeWritID = allWritIDs[0]
	}

	// No active writ for persistent agent: summary + wait message.
	if isPersistent && activeWritID == "" {
		return primeNoActiveWrit(world, agentName, allWritIDs, worldStore)
	}

	// Get the active writ.
	item, err := worldStore.GetWrit(activeWritID)
	if err != nil {
		return nil, fmt.Errorf("failed to get writ %q: %w", activeWritID, err)
	}

	// Check for handoff context (session continuity).
	handoffState, err := handoff.Read(world, agentName, role)
	if err != nil {
		return nil, fmt.Errorf("failed to read handoff state: %w", err)
	}

	var result *PrimeResult

	if handoffState != nil && !handoffState.Consumed {
		if compactRecovery {
			// Compact recovery: lightweight prime that trusts compressed context
			// from the predecessor session (via --continue). Omits the full work
			// item description to save tokens.
			result, err = primeCompactRecovery(world, agentName, item, handoffState)
		} else {
			result, err = primeWithHandoff(world, agentName, item, handoffState)
		}
		if err != nil {
			return nil, err
		}
		// Mark handoff as consumed (durable — file remains for crash recovery).
		if markErr := handoff.MarkConsumed(world, agentName, role); markErr != nil {
			fmt.Fprintf(os.Stderr, "prime: failed to mark handoff consumed: %v\n", markErr)
		}
	} else {
		// Check for active workflow.
		state, wfErr := workflow.ReadState(world, agentName, role)
		if wfErr != nil {
			return nil, fmt.Errorf("failed to read workflow state: %w", wfErr)
		}

		if state != nil && state.Status == "running" {
			result, err = primeWithWorkflow(world, agentName, role, item, state)
			if err != nil {
				return nil, err
			}
		} else if item.HasLabel("convoy-synthesis") {
			// Convoy synthesis item — enrich context with leg info.
			result, err = primeConvoySynthesis(world, agentName, item, worldStore)
			if err != nil {
				return nil, err
			}
		} else {
			// No workflow — standard prime (existing behavior).
			output := fmt.Sprintf(`=== WORK CONTEXT ===
Agent: %s (world: %s)
Writ: %s
Title: %s
Status: %s

Description:
%s

Instructions:
Execute this writ. When complete, run: sol resolve
If stuck, run: sol escalate "description"
=== END CONTEXT ===`, agentName, world, item.ID, item.Title, item.Status, item.Description)
			result = &PrimeResult{Output: output}
		}
	}

	// Append background writ summaries for persistent agents with multiple tethers.
	if isPersistent && len(allWritIDs) > 1 && result != nil {
		bgSection := primeBackgroundWrits(activeWritID, allWritIDs, worldStore)
		if bgSection != "" {
			result.Output += bgSection
		}
	}

	// Append agent memories if any exist.
	if result != nil {
		memories, memErr := worldStore.ListAgentMemories(agentName)
		if memErr != nil {
			fmt.Fprintf(os.Stderr, "prime: failed to read agent memories: %v\n", memErr)
		} else if len(memories) > 0 {
			var mb strings.Builder
			mb.WriteString("\n\n## Agent Memories\n")
			for _, m := range memories {
				fmt.Fprintf(&mb, "- %s: %q\n", m.Key, m.Value)
			}
			result.Output += mb.String()
		}
	}

	// Prepend fresh-session warning if this is a non-compact handoff continuation.
	// Compact recovery has its own framing — no need for the generic warning.
	if freshSession && !compactRecovery && result != nil {
		result.Output = "NOTE: You are a fresh session (handoff from predecessor). Continue working — do NOT call sol handoff.\n\n" + result.Output
	}

	return result, nil
}

// primeCompact generates a short focus reminder for context compaction.
// Reads the tether to find the active writ, looks up its title, and checks
// workflow state. Returns a concise message to keep the agent on track.
func primeCompact(world, agentName, role string, worldStore WorldStore) (*PrimeResult, error) {
	// Read tethered writs.
	allWritIDs, err := tether.List(world, agentName, role)
	if err != nil {
		return nil, fmt.Errorf("failed to list tethers: %w", err)
	}
	if len(allWritIDs) == 0 {
		// Persistent roles (envoy/governor) may have no tether during freeform
		// conversation — return a role-appropriate grounding reminder.
		if role == "envoy" {
			return &PrimeResult{Output: fmt.Sprintf(
				"[sol] Context compaction in progress. You are envoy %s in world %s.\nYour brief is at .brief/memory.md — consult it to re-orient.\nContinue the current conversation.",
				agentName, world)}, nil
		}
		if role == "governor" {
			return &PrimeResult{Output: fmt.Sprintf(
				"[sol] Context compaction in progress. You are the governor of world %s.\nYour brief is at .brief/memory.md — consult it to re-orient.\nContinue the current conversation.",
				world)}, nil
		}
		return &PrimeResult{Output: "[sol] Context compaction in progress. No active work tethered."}, nil
	}

	// Determine active writ.
	var activeWritID string
	if persistentRoles[role] {
		activeWritID = readActiveWrit(world, agentName)
	} else {
		activeWritID = allWritIDs[0]
	}
	if activeWritID == "" {
		return &PrimeResult{Output: "[sol] Context compaction in progress. No active writ."}, nil
	}

	// Look up writ title.
	item, err := worldStore.GetWrit(activeWritID)
	if err != nil {
		return nil, fmt.Errorf("failed to get writ %q: %w", activeWritID, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[sol] Context compaction in progress. Stay focused on your current assignment.\n\n")
	fmt.Fprintf(&b, "Writ: %s — %s\n", item.ID, item.Title)

	// Check for active workflow step.
	state, _ := workflow.ReadState(world, agentName, role)
	if state != nil && state.Status == "running" && state.CurrentStep != "" {
		total := len(state.Completed) + 1 // completed + current
		// Try to get the full step count from ListSteps.
		if steps, err := workflow.ListSteps(world, agentName, role); err == nil && len(steps) > 0 {
			total = len(steps)
		}
		current := len(state.Completed) + 1
		fmt.Fprintf(&b, "Step: %s (%d/%d)\n", state.CurrentStep, current, total)
	}

	fmt.Fprintf(&b, "\nContinue where you left off. Do not restart from scratch.")
	return &PrimeResult{Output: b.String()}, nil
}

// readActiveWrit reads the active_writ field for an agent from the sphere store.
// Returns empty string on any error (best-effort).
func readActiveWrit(world, agentName string) string {
	ss, err := store.OpenSphere()
	if err != nil {
		fmt.Fprintf(os.Stderr, "prime: failed to open sphere store: %v\n", err)
		return ""
	}
	defer ss.Close()

	agentID := world + "/" + agentName
	agent, err := ss.GetAgent(agentID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "prime: failed to get agent %q: %v\n", agentID, err)
		return ""
	}
	return agent.ActiveWrit
}

// primeNoActiveWrit generates prime context when a persistent agent has tethered writs
// but none is active. Lists all writs and tells the agent to wait.
func primeNoActiveWrit(world, agentName string, writIDs []string, worldStore WorldStore) (*PrimeResult, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "=== WORK CONTEXT ===\nAgent: %s (world: %s)\n\n", agentName, world)
	fmt.Fprintf(&b, "You have %d tethered writs. Wait for the operator to activate one.\n\n", len(writIDs))

	for _, id := range writIDs {
		writ, err := worldStore.GetWrit(id)
		if err != nil {
			fmt.Fprintf(&b, "- %s — (failed to load)\n", id)
			continue
		}
		kind := writ.Kind
		if kind == "" {
			kind = "code"
		}
		fmt.Fprintf(&b, "- %s — %s (kind: %s, status: %s)\n", id, writ.Title, kind, writ.Status)
	}

	b.WriteString("\n=== END CONTEXT ===")
	return &PrimeResult{Output: b.String()}, nil
}

// primeBackgroundWrits generates the background writs summary section
// appended after the active writ's prime context for persistent agents.
func primeBackgroundWrits(activeWritID string, allWritIDs []string, worldStore WorldStore) string {
	var b strings.Builder
	b.WriteString("\n\n## Background Writs\n")

	hasBackground := false
	for _, id := range allWritIDs {
		if id == activeWritID {
			continue
		}
		writ, err := worldStore.GetWrit(id)
		if err != nil {
			fmt.Fprintf(&b, "- %s — (failed to load)\n", id)
			hasBackground = true
			continue
		}
		kind := writ.Kind
		if kind == "" {
			kind = "code"
		}
		fmt.Fprintf(&b, "- %s — %s (kind: %s, status: %s)\n", id, writ.Title, kind, writ.Status)
		hasBackground = true
	}

	if !hasBackground {
		return ""
	}

	b.WriteString("\nWork only on your active writ. Background writs are listed for awareness.\n")
	return b.String()
}

// primeWithWorkflow returns workflow-aware context for the prime command.
func primeWithWorkflow(world, agentName, role string, item *store.Writ,
	state *workflow.State) (*PrimeResult, error) {

	// Read all steps to show full checklist.
	allSteps, err := workflow.ListSteps(world, agentName, role)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflow steps: %w", err)
	}
	if len(allSteps) == 0 {
		return &PrimeResult{
			Output: fmt.Sprintf("Workflow complete for %s. Run: sol resolve", item.ID),
		}, nil
	}

	instance, _ := workflow.ReadInstance(world, agentName, role)
	wfName := ""
	if instance != nil {
		wfName = instance.Workflow
	}

	// Find the current step index and build checklist.
	totalSteps := len(allSteps)
	currentIdx := -1
	var currentStep *workflow.Step
	var checklist strings.Builder
	for i, s := range allSteps {
		var marker string
		switch {
		case s.ID == state.CurrentStep:
			marker = "[>]"
			currentIdx = i
			currentStep = &allSteps[i]
		case s.Status == "complete":
			marker = "[x]"
		default:
			marker = "[ ]"
		}
		fmt.Fprintf(&checklist, "  %s %d. %s\n", marker, i+1, s.Title)
	}

	if currentStep == nil {
		// All steps complete — no current step.
		return &PrimeResult{
			Output: fmt.Sprintf("Workflow complete for %s. Run: sol resolve", item.ID),
		}, nil
	}

	output := fmt.Sprintf(`=== WORK CONTEXT ===
Agent: %s (world: %s)
Writ: %s
Title: %s

Workflow: %s (step %d/%d: %s)

Steps:
%s
--- CURRENT STEP ---
%s
--- END STEP ---

When step is complete: sol workflow advance --world=%s --agent=%s
After final step: sol resolve
=== END CONTEXT ===`,
		agentName, world, item.ID, item.Title,
		wfName, currentIdx+1, totalSteps, currentStep.Title,
		strings.TrimRight(checklist.String(), "\n"),
		currentStep.Instructions,
		world, agentName)

	return &PrimeResult{Output: output}, nil
}

// primeWithHandoff returns handoff-aware context for the prime command.
func primeWithHandoff(world, agentName string, item *store.Writ,
	state *handoff.State) (*PrimeResult, error) {

	output := fmt.Sprintf(`=== HANDOFF CONTEXT ===
Agent: %s (world: %s)
Writ: %s
Title: %s

This is a continuation of a previous session. The previous session
handed off to preserve context.

--- PREVIOUS SESSION SUMMARY ---
%s
--- END SUMMARY ---

--- RECENT COMMITS ---
%s
--- END COMMITS ---
`, agentName, world, item.ID, item.Title, state.Summary, strings.Join(state.RecentCommits, "\n"))

	// Add git worktree state if captured.
	if state.GitStatus != "" {
		output += fmt.Sprintf("--- GIT STATUS ---\n%s\n--- END GIT STATUS ---\n\n", state.GitStatus)
	}
	if state.DiffStat != "" {
		output += fmt.Sprintf("--- UNCOMMITTED CHANGES ---\n%s\n--- END UNCOMMITTED CHANGES ---\n\n", state.DiffStat)
	}
	if state.GitStash != "" {
		output += fmt.Sprintf("--- STASHED WORK ---\n%s\n--- END STASHED WORK ---\n\n", state.GitStash)
	}

	// Add workflow context if the agent has an active workflow.
	if state.WorkflowStep != "" {
		stepInfo := state.WorkflowStep
		if state.StepDescription != "" {
			stepInfo = fmt.Sprintf("%s — %s", state.WorkflowStep, state.StepDescription)
		}
		output += fmt.Sprintf(`Workflow progress: %s (current step: %s)
Read your current step: sol workflow current --world=%s --agent=%s

`, state.WorkflowProgress, stepInfo, world, agentName)
	}

	output += fmt.Sprintf(`Continue from where the previous session left off.
When complete, run: sol resolve
If you need to hand off again: sol handoff --summary="<what you've done>"
=== END HANDOFF ===`)

	return &PrimeResult{Output: output}, nil
}

// primeCompactRecovery returns a lightweight prime for sessions recovering from
// context compaction. Unlike primeWithHandoff, it omits the full writ
// description because the agent has compressed context from its predecessor
// session (via --continue). This saves tokens and avoids confusing the agent
// about whether it's starting fresh or continuing.
func primeCompactRecovery(world, agentName string, item *store.Writ,
	state *handoff.State) (*PrimeResult, error) {

	var b strings.Builder
	fmt.Fprintf(&b, `=== SESSION RECOVERY ===
Agent: %s (world: %s)
Writ: %s — %s
Reason: Context compaction recovery

You are continuing a previous session. Your prior conversation has been compressed.

`, agentName, world, item.ID, item.Title)

	// Previous session state from handoff.
	fmt.Fprintf(&b, "PREVIOUS SESSION STATE:\n")
	fmt.Fprintf(&b, "Summary: %s\n", state.Summary)
	if len(state.RecentCommits) > 0 {
		fmt.Fprintf(&b, "Recent commits:\n%s\n", strings.Join(state.RecentCommits, "\n"))
	}
	if state.GitStatus != "" {
		fmt.Fprintf(&b, "Git status:\n%s\n", state.GitStatus)
	}
	if state.DiffStat != "" {
		fmt.Fprintf(&b, "Uncommitted changes:\n%s\n", state.DiffStat)
	}
	if state.GitStash != "" {
		fmt.Fprintf(&b, "Stashed work:\n%s\n", state.GitStash)
	}

	// Workflow state if active.
	if state.WorkflowStep != "" {
		stepInfo := state.WorkflowStep
		if state.StepDescription != "" {
			stepInfo = fmt.Sprintf("%s — %s", state.WorkflowStep, state.StepDescription)
		}
		fmt.Fprintf(&b, "\nCURRENT WORKFLOW STATE:\n")
		fmt.Fprintf(&b, "Progress: %s (current step: %s)\n", state.WorkflowProgress, stepInfo)
		fmt.Fprintf(&b, "Read your current step: sol workflow current --world=%s --agent=%s\n", world, agentName)
	}

	fmt.Fprintf(&b, `
Continue from where you left off. Do NOT re-read the writ description
or restart from scratch — pick up where the previous session stopped.

When complete: sol resolve
=== END RECOVERY ===`)

	return &PrimeResult{Output: b.String()}, nil
}

// primeForge returns forge-specific context for the prime command.
func primeForge(world string) (*PrimeResult, error) {
	output := fmt.Sprintf(`=== FORGE CONTEXT ===
World: %s
Role: forge (merge queue processor)

Begin your patrol loop. Run 'sol forge check-unblocked --world=%s' first,
then scan the queue with 'sol forge ready --world=%s --json'.
=== END CONTEXT ===`, world, world, world)

	return &PrimeResult{Output: output}, nil
}

// primeConvoySynthesis returns enriched context for a convoy synthesis writ.
// It lists all sibling leg writs, their titles, and their merge request branches.
func primeConvoySynthesis(world, agentName string, item *store.Writ,
	worldStore WorldStore) (*PrimeResult, error) {

	var legSection strings.Builder
	legSection.WriteString("## Convoy Legs\n")
	legSection.WriteString("The following leg writs have been merged. Their changes are in your worktree.\n\n")

	if item.ParentID != "" {
		siblings, err := worldStore.ListChildWrits(item.ParentID)
		if err != nil {
			return nil, fmt.Errorf("failed to list sibling writs: %w", err)
		}

		for _, sib := range siblings {
			if sib.ID == item.ID {
				continue // skip the synthesis item itself
			}
			if !sib.HasLabel("convoy-leg") {
				continue
			}
			// Look up the merge request to find the branch name.
			branch := "(unknown)"
			mrs, err := worldStore.ListMergeRequestsByWrit(sib.ID, "")
			if err == nil && len(mrs) > 0 {
				branch = mrs[0].Branch
			}
			legSection.WriteString(fmt.Sprintf("- **%s** (%s)\n  Branch: %s | Status: %s\n", sib.Title, sib.ID, branch, sib.Status))
		}
	}

	output := fmt.Sprintf(`=== WORK CONTEXT ===
Agent: %s (world: %s)
Writ: %s
Title: %s
Status: %s

%s
Description:
%s

Instructions:
This is a convoy synthesis step. All parallel legs have completed and their
branches have been merged to the target branch. Your worktree contains all
leg outputs. Synthesize the findings from all legs into a consolidated result.

When complete, run: sol resolve
If stuck, run: sol escalate "description"
=== END CONTEXT ===`, agentName, world, item.ID, item.Title, item.Status,
		legSection.String(), item.Description)

	return &PrimeResult{Output: output}, nil
}

// ResolveResult holds the output of a resolve operation.
type ResolveResult struct {
	WritID     string
	Title          string
	AgentName      string
	BranchName     string
	MergeRequestID string
	SessionKept    bool // true if session was not killed (envoy resolve)
}

// ResolveOpts holds the inputs for a resolve operation.
type ResolveOpts struct {
	World     string
	AgentName string
	WritID    string // Optional: specific writ to resolve (persistent agents only; ignored for outpost agents)
}

// ResolveLockPath returns the path to the shared resolve-in-progress lock file (used for outpost agents).
func ResolveLockPath(world, agentName, role string) string {
	return filepath.Join(config.AgentDir(world, agentName, role), ".resolve_in_progress")
}

// ResolveWritLockPath returns the path to the per-writ resolve-in-progress lock file.
// Used for persistent agents to avoid concurrent resolves sharing the same lock file.
func ResolveWritLockPath(world, agentName, role, writID string) string {
	return filepath.Join(config.AgentDir(world, agentName, role), ".resolve_in_progress."+writID)
}

// IsResolveInProgress returns true if any resolve lock file exists for this agent.
// Checks both the shared lock file (outpost agents) and per-writ lock files (persistent agents).
func IsResolveInProgress(world, agentName, role string) bool {
	if _, err := os.Stat(ResolveLockPath(world, agentName, role)); err == nil {
		return true
	}
	agentDir := config.AgentDir(world, agentName, role)
	matches, err := filepath.Glob(filepath.Join(agentDir, ".resolve_in_progress.*"))
	return err == nil && len(matches) > 0
}

// ClearResolveLocksForAgent removes all resolve lock files for an agent (shared and per-writ).
func ClearResolveLocksForAgent(world, agentName, role string) {
	os.Remove(ResolveLockPath(world, agentName, role))
	agentDir := config.AgentDir(world, agentName, role)
	if matches, err := filepath.Glob(filepath.Join(agentDir, ".resolve_in_progress.*")); err == nil {
		for _, f := range matches {
			os.Remove(f)
		}
	}
}

// Resolve signals work completion: git operations, state updates, tether clear.
// The logger parameter is optional — if nil, no events are emitted.
func Resolve(ctx context.Context, opts ResolveOpts, worldStore WorldStore, sphereStore SphereStore, mgr SessionManager, logger *events.Logger) (*ResolveResult, error) {
	agentID := opts.World + "/" + opts.AgentName
	sessName := config.SessionName(opts.World, opts.AgentName)

	// Look up agent first to determine role (needed for role-aware tether path).
	agent, err := sphereStore.GetAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", agentID, err)
	}

	// 1. Determine which writ to resolve.
	var writID string
	if agent.Role == "outpost" {
		// Outpost: read tether (single writ, unchanged behavior).
		writID, err = tether.Read(opts.World, opts.AgentName, agent.Role)
		if err != nil {
			return nil, fmt.Errorf("failed to read tether: %w", err)
		}
		if writID == "" {
			return nil, fmt.Errorf("no work tethered for agent %q in world %q", opts.AgentName, opts.World)
		}
	} else {
		// Persistent agent: use explicit WritID, fall back to active_writ from DB.
		if opts.WritID != "" {
			writID = opts.WritID
			// Validate the writ is actually tethered.
			if !tether.IsTetheredTo(opts.World, opts.AgentName, writID, agent.Role) {
				return nil, fmt.Errorf("writ %q is not tethered to agent %q", writID, opts.AgentName)
			}
		} else {
			writID = agent.ActiveWrit
			if writID == "" {
				return nil, fmt.Errorf("no active writ for agent %q in world %q", opts.AgentName, opts.World)
			}
		}
	}

	// Create resolve lock to prevent handoff from interrupting.
	// Persistent agents use a per-writ lock file so concurrent resolves don't
	// interfere: when Resolve-A finishes and removes its lock, Resolve-B's lock
	// remains visible and the handoff guard still sees a resolve in progress.
	var lockPath string
	if agent.Role == "outpost" {
		lockPath = ResolveLockPath(opts.World, opts.AgentName, agent.Role)
	} else {
		lockPath = ResolveWritLockPath(opts.World, opts.AgentName, agent.Role, writID)
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}
	// Write resolve lock with writ ID (enables crash recovery detection).
	if err := os.WriteFile(lockPath, []byte(writID), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write resolve lock: %w", err)
	}
	defer os.Remove(lockPath)

	// Acquire locks: writ first, then agent (consistent ordering with Cast).
	lock, err := AcquireWritLock(writID)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	agentLock, err := AcquireAgentLock(agentID)
	if err != nil {
		return nil, err
	}
	defer agentLock.Release()

	// Compute worktree path and branch name based on role.
	var worktreeDir string
	var branchName string
	switch agent.Role {
	case "envoy":
		worktreeDir = envoy.WorktreePath(opts.World, opts.AgentName)
		branchName = fmt.Sprintf("envoy/%s/%s/%s", opts.World, opts.AgentName, writID)
	case "governor":
		worktreeDir = governor.GovernorDir(opts.World)
		branchName = fmt.Sprintf("governor/%s", opts.World)
	case "forge":
		worktreeDir = filepath.Join(config.Home(), opts.World, "forge", "worktree")
		branchName = "forge/" + opts.World
	default:
		worktreeDir = WorktreePath(opts.World, opts.AgentName)
		branchName = fmt.Sprintf("outpost/%s/%s", opts.AgentName, writID)
	}

	// Get the writ for output and conflict-resolution detection.
	item, err := worldStore.GetWrit(writID)
	if err != nil {
		return nil, fmt.Errorf("failed to get writ %q: %w", writID, err)
	}

	// Detect conflict-resolution tasks and handle separately.
	if item.HasLabel("conflict-resolution") {
		return resolveConflictResolution(ctx, opts, item, branchName, worktreeDir,
			agentID, sessName, agent.Role, worldStore, sphereStore, mgr, logger)
	}

	// Determine if this is a code writ. Non-code writs (analysis, etc.) skip
	// git operations, MR creation, and forge/governor nudges entirely.
	isCodeWrit := item.Kind == "" || item.Kind == "code"

	var mrID string
	var pushFailed bool

	if isCodeWrit {
		// 2. Git operations in the worktree (code writs only).
		// git add -A
		addCtx, addCancel := context.WithTimeout(ctx, GitLocalOpTimeout)
		defer addCancel()
		addCmd := exec.CommandContext(addCtx, "git", "-C", worktreeDir, "add", "-A")
		if out, err := addCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git add failed: %s: %w", strings.TrimSpace(string(out)), err)
		}

		// git commit (skip if nothing to commit)
		commitMsg := fmt.Sprintf("sol resolve: %s", item.Title)
		commitCtx, commitCancel := context.WithTimeout(ctx, GitLocalOpTimeout)
		defer commitCancel()
		commitCmd := exec.CommandContext(commitCtx, "git", "-C", worktreeDir, "commit", "-m", commitMsg)
		commitCmd.CombinedOutput() // ignore error — nothing to commit is OK

		// git push: envoy pushes HEAD to a per-writ remote ref via refspec;
		// other roles push HEAD (which tracks the pre-created branch).
		pushCtx, pushCancel := context.WithTimeout(ctx, GitPushTimeout)
		defer pushCancel()
		var pushCmd *exec.Cmd
		if agent.Role == "envoy" {
			// Push HEAD to the per-writ remote ref without creating a local branch.
			// (A local branch cannot coexist with the persistent envoy branch because
			// git stores refs as a filesystem hierarchy and the envoy branch name is a
			// prefix of the per-writ branch name.)
			pushCmd = exec.CommandContext(pushCtx, "git", "-C", worktreeDir,
				"push", "origin", "HEAD:refs/heads/"+branchName)
		} else {
			pushCmd = exec.CommandContext(pushCtx, "git", "-C", worktreeDir, "push", "origin", "HEAD")
		}
		if out, err := pushCmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: git push failed: %s\n", strings.TrimSpace(string(out)))
			pushFailed = true
		}
	}

	// Track what has been done so we can undo on failure.
	var writUpdated bool

	rollback := func() {
		if writUpdated {
			if err := worldStore.UpdateWrit(writID, store.WritUpdates{Status: "tethered"}); err != nil {
				fmt.Fprintf(os.Stderr, "resolve rollback: failed to reset writ %s: %v\n", writID, err)
			}
		}
	}

	// 3. Update writ status.
	if isCodeWrit {
		// Code writs: status → done (idempotent — skip if already done).
		if item.Status != "done" {
			if err := worldStore.UpdateWrit(writID, store.WritUpdates{Status: "done"}); err != nil {
				return nil, fmt.Errorf("failed to update writ status: %w", err)
			}
			writUpdated = true
		}
	} else {
		// Non-code writs: close directly with close_reason "completed".
		if item.Status != "closed" {
			if _, err := worldStore.CloseWrit(writID, "completed"); err != nil {
				return nil, fmt.Errorf("failed to close non-code writ: %w", err)
			}
			writUpdated = true
		}
	}

	if isCodeWrit {
		// 4. Create merge request (idempotent — skip if one already exists for this writ).
		// Filter out failed MRs so a new resolve after a failed MR creates a fresh
		// MR with the current branch instead of reusing the stale failed one.
		existingMRs, err := worldStore.ListMergeRequestsByWrit(writID, "")
		if err != nil {
			rollback()
			return nil, fmt.Errorf("failed to check existing merge requests: %w", err)
		}
		var activeMRs []store.MergeRequest
		for _, mr := range existingMRs {
			if mr.Phase != "failed" {
				activeMRs = append(activeMRs, mr)
			}
		}
		if len(activeMRs) > 0 {
			mrID = activeMRs[0].ID
		} else if !pushFailed {
			// Only create an MR when the branch was successfully pushed.
			// If push failed, the remote branch doesn't exist yet, so creating
			// an MR would let forge attempt to merge a non-existent branch —
			// causing an infinite recast loop. The writ stays in "done" state;
			// the next resolve (after a successful push) will create the MR.
			mrID, err = worldStore.CreateMergeRequest(writID, branchName, item.Priority)
			if err != nil {
				rollback()
				return nil, fmt.Errorf("failed to create merge request for %q: %w", writID, err)
			}
		}
	}

	// Auto-resolve writ-linked escalations (best-effort).
	escalations, escErr := sphereStore.ListEscalationsBySourceRef("writ:" + writID)
	if escErr != nil {
		fmt.Fprintf(os.Stderr, "resolve: failed to check escalations: %v\n", escErr)
	} else {
		for _, esc := range escalations {
			if err := sphereStore.ResolveEscalation(esc.ID); err != nil {
				fmt.Fprintf(os.Stderr, "resolve: failed to auto-resolve escalation %s: %v\n", esc.ID, err)
			}
		}
	}

	// 5. Update agent state (idempotent — check current state first).
	// Outpost agents are ephemeral — delete the record to reclaim the name.
	// Persistent roles (envoy, governor) keep their record and update state
	// based on remaining tethers.
	if agent.Role == "outpost" {
		// Re-read agent to check if already deleted (idempotent re-run).
		if _, getErr := sphereStore.GetAgent(agentID); getErr == nil {
			if err := sphereStore.DeleteAgent(agentID); err != nil {
				rollback()
				return nil, fmt.Errorf("failed to delete agent %q: %w", agentID, err)
			}
		}
	} else {
		// Persistent agent: determine remaining tethers after this resolve.
		currentTethers, listErr := tether.List(opts.World, opts.AgentName, agent.Role)
		remaining := 0
		if listErr == nil {
			for _, id := range currentTethers {
				if id != writID {
					remaining++
				}
			}
		}

		if remaining > 0 {
			// More tethers remain: stay working.
			if agent.ActiveWrit == writID {
				// Resolving the active writ: clear active_writ but stay working.
				if err := sphereStore.UpdateAgentState(agentID, "working", ""); err != nil {
					rollback()
					return nil, fmt.Errorf("failed to update agent state: %w", err)
				}
			}
			// If resolving a non-active writ, no state update needed.
		} else {
			// No remaining tethers: set to idle, clear active_writ.
			if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
				rollback()
				return nil, fmt.Errorf("failed to update agent state: %w", err)
			}
		}
	}

	// 6. Clear tether.
	if agent.Role == "outpost" {
		// Outpost: clear entire tether directory.
		if err := tether.Clear(opts.World, opts.AgentName, agent.Role); err != nil {
			fmt.Fprintf(os.Stderr, "resolve: failed to clear tether (consul will recover): %v\n", err)
		}
	} else {
		// Persistent: remove only the resolved writ's tether file.
		if err := tether.ClearOne(opts.World, opts.AgentName, writID, agent.Role); err != nil {
			fmt.Fprintf(os.Stderr, "resolve: failed to clear tether (consul will recover): %v\n", err)
		}
	}

	// 6b. Clean up workflow if present (envoys and governors don't use workflow system).
	if agent.Role != "envoy" && agent.Role != "governor" {
		if _, err := workflow.ReadState(opts.World, opts.AgentName, agent.Role); err == nil {
			if removeErr := workflow.Remove(opts.World, opts.AgentName, agent.Role); removeErr != nil {
				fmt.Fprintf(os.Stderr, "resolve: failed to clean up workflow: %v\n", removeErr)
			}
		}
	}

	// 7. Stop session after a brief delay to allow final output.
	// Envoys and governors keep their session alive — they are human-supervised and persistent.
	sessionKept := false
	if agent.Role != "envoy" && agent.Role != "governor" && agent.Role != "forge" {
		done := make(chan struct{})
		go func() {
			defer close(done)
			time.Sleep(1 * time.Second)
			if err := mgr.Stop(sessName, true); err != nil {
				fmt.Fprintf(os.Stderr, "resolve: failed to stop session %s: %v\n", sessName, err)
			}
			// 7b. Remove worktree for outpost agents (ephemeral worktrees only).
			if agent.Role == "outpost" {
				cleanupWorktree(opts.World, worktreeDir)
			}
		}()
	} else {
		sessionKept = true
	}

	// 8. Emit event and nudge downstream agents (code writs only for nudges).
	if isCodeWrit {
		if logger != nil {
			logger.Emit(events.EventResolve, "sol", opts.AgentName, "both", map[string]string{
				"writ_id":      writID,
				"agent":        opts.AgentName,
				"branch":       branchName,
				"merge_request": mrID,
			})
		}

		// Nudge governor that work is done (best-effort, smart delivery).
		govSession := config.SessionName(opts.World, "governor")
		govBody := fmt.Sprintf(`{"writ_id":%q,"agent_name":%q,"branch":%q,"title":%q,"merge_request_id":%q}`,
			writID, opts.AgentName, branchName, item.Title, mrID)
		if err := nudge.Deliver(govSession, nudge.Message{
			Sender:   opts.AgentName,
			Type:     "AGENT_DONE",
			Subject:  fmt.Sprintf("Agent %s resolved %s", opts.AgentName, writID),
			Body:     govBody,
			Priority: "normal",
		}); err != nil {
			fmt.Fprintf(os.Stderr, "resolve: failed to nudge governor: %v\n", err)
		}

		// Nudge forge that a new MR is ready (best-effort, smart delivery).
		forgeSession := config.SessionName(opts.World, "forge")
		if err := nudge.Deliver(forgeSession, nudge.Message{
			Sender:   opts.AgentName,
			Type:     "MR_READY",
			Subject:  fmt.Sprintf("MR %s ready for merge", mrID),
			Body:     fmt.Sprintf(`{"writ_id":%q,"merge_request_id":%q,"branch":%q,"title":%q}`, writID, mrID, branchName, item.Title),
			Priority: "normal",
		}); err != nil {
			fmt.Fprintf(os.Stderr, "resolve: failed to nudge forge: %v\n", err)
		}
	} else {
		// Non-code writs: emit event without branch/MR fields.
		if logger != nil {
			logger.Emit(events.EventResolve, "sol", opts.AgentName, "both", map[string]string{
				"writ_id": writID,
				"agent":   opts.AgentName,
				"kind":    item.Kind,
			})
		}

		// Nudge governor that non-code work is done (best-effort, smart delivery).
		govSession := config.SessionName(opts.World, "governor")
		govBody := fmt.Sprintf(`{"writ_id":%q,"agent_name":%q,"kind":%q,"title":%q}`,
			writID, opts.AgentName, item.Kind, item.Title)
		if err := nudge.Deliver(govSession, nudge.Message{
			Sender:   opts.AgentName,
			Type:     "AGENT_DONE",
			Subject:  fmt.Sprintf("Agent %s resolved %s", opts.AgentName, writID),
			Body:     govBody,
			Priority: "normal",
		}); err != nil {
			fmt.Fprintf(os.Stderr, "resolve: failed to nudge governor: %v\n", err)
		}
	}

	// 9. Close history record for cycle-time tracking.
	if _, err := worldStore.EndHistory(writID); err != nil {
		fmt.Fprintf(os.Stderr, "resolve: failed to end history: %v\n", err)
	}

	// For non-code writs, BranchName and MergeRequestID are empty strings.
	resultBranch := ""
	if isCodeWrit {
		resultBranch = branchName
	}

	return &ResolveResult{
		WritID:         writID,
		Title:          item.Title,
		AgentName:      opts.AgentName,
		BranchName:     resultBranch,
		MergeRequestID: mrID,
		SessionKept:    sessionKept,
	}, nil
}

// resolveConflictResolution handles the resolve flow for conflict-resolution tasks.
// Differences from normal resolve:
// 1. Uses --force-with-lease for push (branch was rebased)
// 2. Does NOT create a new merge request (original MR already exists)
// 3. Unblocks the original MR
// 4. Closes the resolution writ
func resolveConflictResolution(ctx context.Context, opts ResolveOpts, item *store.Writ, branchName, worktreeDir,
	agentID, sessName, role string, worldStore WorldStore, sphereStore SphereStore, mgr SessionManager, logger *events.Logger) (*ResolveResult, error) {

	// 1. Git operations: add, commit, force-push (branch was rebased).
	addCtx, addCancel := context.WithTimeout(ctx, GitLocalOpTimeout)
	defer addCancel()
	addCmd := exec.CommandContext(addCtx, "git", "-C", worktreeDir, "add", "-A")
	if out, err := addCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git add failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	commitMsg := fmt.Sprintf("sol resolve: %s", item.Title)
	commitCtx, commitCancel := context.WithTimeout(ctx, GitLocalOpTimeout)
	defer commitCancel()
	commitCmd := exec.CommandContext(commitCtx, "git", "-C", worktreeDir, "commit", "-m", commitMsg)
	commitCmd.CombinedOutput() // ignore error — nothing to commit is OK

	// Force push with lease — branch was rebased, needs force push.
	pushCtx, pushCancel := context.WithTimeout(ctx, GitPushTimeout)
	defer pushCancel()
	pushCmd := exec.CommandContext(pushCtx, "git", "-C", worktreeDir, "push", "--force-with-lease", "origin", "HEAD")
	pushFailed := false
	if out, err := pushCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: git push --force-with-lease failed: %s\n",
			strings.TrimSpace(string(out)))
		pushFailed = true
	}

	// 2. Reset the parent's MR for retry (only if push succeeded).
	// Two complementary strategies:
	//   a) Find MR blocked by this resolution task and reset it.
	//   b) Look up parent writ's MR by parent_id — covers the case where
	//      the MR ended up in 'failed' phase independently.
	if !pushFailed {
		resetMRs := map[string]bool{} // track already-reset MR IDs

		// 2a. Find MR blocked by this resolution task.
		blockedMR, err := worldStore.FindMergeRequestByBlocker(item.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to find blocked MR for %q: %w", item.ID, err)
		}
		if blockedMR != nil {
			if err := worldStore.ResetMergeRequestForRetry(blockedMR.ID); err != nil {
				return nil, fmt.Errorf("failed to reset MR %q for retry: %w", blockedMR.ID, err)
			}
			resetMRs[blockedMR.ID] = true
		}

		// 2b. Check parent writ's MRs for any stuck in 'failed' phase.
		if item.ParentID != "" {
			parentMRs, err := worldStore.ListMergeRequestsByWrit(item.ParentID, "failed")
			if err != nil {
				fmt.Fprintf(os.Stderr, "resolve: failed to list parent MRs: %v\n", err)
			} else {
				for _, mr := range parentMRs {
					if resetMRs[mr.ID] {
						continue
					}
					if err := worldStore.ResetMergeRequestForRetry(mr.ID); err != nil {
						fmt.Fprintf(os.Stderr, "resolve: failed to reset parent MR %s: %v\n", mr.ID, err)
					}
				}
			}
		}
	}

	// 3. Close the resolution writ.
	if _, err := worldStore.CloseWrit(item.ID); err != nil {
		return nil, fmt.Errorf("failed to close resolution writ: %w", err)
	}

	// 4. Update agent state.
	// Outpost agents are ephemeral — delete the record to reclaim the name.
	// Persistent agents update state based on remaining tethers.
	if role == "outpost" {
		if err := sphereStore.DeleteAgent(agentID); err != nil {
			return nil, fmt.Errorf("failed to delete agent %q: %w", agentID, err)
		}
	} else {
		// Persistent agent: determine remaining tethers after this resolve.
		currentAgent, _ := sphereStore.GetAgent(agentID)
		currentTethers, listErr := tether.List(opts.World, opts.AgentName, role)
		remaining := 0
		if listErr == nil {
			for _, id := range currentTethers {
				if id != item.ID {
					remaining++
				}
			}
		}

		if remaining > 0 {
			// More tethers remain: stay working.
			if currentAgent != nil && currentAgent.ActiveWrit == item.ID {
				if err := sphereStore.UpdateAgentState(agentID, "working", ""); err != nil {
					return nil, fmt.Errorf("failed to update agent state: %w", err)
				}
			}
		} else {
			// No remaining tethers: set to idle.
			if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
				return nil, fmt.Errorf("failed to update agent state: %w", err)
			}
		}
	}

	// 5. Clear tether.
	if role == "outpost" {
		// Outpost: clear entire tether directory.
		if err := tether.Clear(opts.World, opts.AgentName, role); err != nil {
			return nil, fmt.Errorf("failed to clear tether: %w", err)
		}
	} else {
		// Persistent: remove only the resolved writ's tether file.
		if err := tether.ClearOne(opts.World, opts.AgentName, item.ID, role); err != nil {
			return nil, fmt.Errorf("failed to clear tether: %w", err)
		}
	}

	// 6. Stop session after a brief delay to allow final output.
	// Envoys and governors keep their session alive — they are human-supervised and persistent.
	sessionKept := false
	if role != "envoy" && role != "governor" && role != "forge" {
		done := make(chan struct{})
		go func() {
			defer close(done)
			time.Sleep(1 * time.Second)
			if err := mgr.Stop(sessName, true); err != nil {
				fmt.Fprintf(os.Stderr, "resolve: failed to stop session %s: %v\n", sessName, err)
			}
			// 6b. Remove worktree for outpost agents (ephemeral worktrees only).
			if role == "outpost" {
				cleanupWorktree(opts.World, worktreeDir)
			}
		}()
	} else {
		sessionKept = true
	}

	if logger != nil {
		logger.Emit(events.EventResolve, "sol", opts.AgentName, "both", map[string]string{
			"writ_id": item.ID,
			"agent":        opts.AgentName,
			"branch":       branchName,
		})
	}

	return &ResolveResult{
		WritID:     item.ID,
		Title:          item.Title,
		AgentName:      opts.AgentName,
		BranchName:     branchName,
		MergeRequestID: "", // No new MR for conflict resolution.
		SessionKept:    sessionKept,
	}, nil
}

// DiscoverSourceRepo finds the git repo root from the current directory.
func DiscoverSourceRepo() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), GitLocalOpTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not in a git repository: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ResolveSourceRepo returns the path to the managed git clone for a world.
// Falls back to the world config source_repo and CWD git discovery for
// worlds that predate the managed clone system.
func ResolveSourceRepo(world string, cfg config.WorldConfig) (string, error) {
	// Prefer managed clone.
	repoPath := config.RepoPath(world)
	if info, err := os.Stat(repoPath); err == nil && info.IsDir() {
		return repoPath, nil
	}

	// Fallback: world config source_repo (legacy worlds without managed clone).
	if cfg.World.SourceRepo != "" {
		return cfg.World.SourceRepo, nil
	}

	// Fallback: discover from CWD (legacy convenience).
	repo, err := DiscoverSourceRepo()
	if err != nil {
		return "", fmt.Errorf("no managed repo at %s, no source_repo in world.toml, and not in a git repo", repoPath)
	}
	return repo, nil
}

// NewSessionManager creates a new session manager. Convenience wrapper.
func NewSessionManager() *session.Manager {
	return session.New()
}
