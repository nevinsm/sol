// Package dispatch implements cast and resolve orchestration — creating worktrees, tethering writs, starting sessions, and cleaning up on resolve.
package dispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/fileutil"
	"github.com/nevinsm/sol/internal/flock"
	"github.com/nevinsm/sol/internal/guidelines"
	"github.com/nevinsm/sol/internal/namepool"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/softfail"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// ErrCapacityExhausted is returned when a world has reached its per-world
// active session limit (max_active). Use errors.Is to check for this error.
var ErrCapacityExhausted = errors.New("agent capacity exhausted")

// ErrSphereCapacityExhausted is returned when the sphere-wide session limit
// (sphere.max_sessions) has been reached. Use errors.Is to check for this error.
var ErrSphereCapacityExhausted = errors.New("sphere session capacity exhausted")

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
	SupersedeFailedMRsForWrit(writID string) ([]string, error)
	UpdateMergeRequestPhase(id, phase string) error
	CreateWritWithOpts(opts store.CreateWritOpts) (string, error)
	FindMergeRequestByBlocker(blockerID string) (*store.MergeRequest, error)
	UnblockMergeRequest(mrID string) error
	ResetMergeRequestForRetry(mrID string) error
	CloseWrit(id string, closeReason ...string) ([]string, error)
	ListChildWrits(parentID string) ([]store.Writ, error)
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
	CreateEscalation(severity, source, description string, sourceRef ...string) (string, error)
	ListEscalationsBySourceRef(sourceRef string) ([]store.Escalation, error)
	ResolveEscalation(id string) error
	GetCaravanItemsForWrit(writID string) ([]store.CaravanItem, error)
	GetCaravan(id string) (*store.Caravan, error)
	SendMessage(sender, recipient, subject, body string, priority int, msgType string) (string, error)
	Close() error
}

// WorktreePath returns the worktree directory for an agent.
func WorktreePath(world, agentName string) string {
	return config.WorktreePath(world, agentName)
}

// CastResult holds the output of a successful cast operation.
type CastResult struct {
	WritID      string
	AgentName   string
	SessionName string
	WorktreeDir string
	Guidelines  string // guidelines template name, empty if none
}

// CastOpts holds the inputs for a cast operation.
type CastOpts struct {
	WritID      string
	World       string
	AgentName   string              // optional: if empty, find an idle agent
	SourceRepo  string              // path to the source git repo
	Guidelines  string              // optional: explicit guidelines template name
	Variables   map[string]string   // optional: template variables
	WorldConfig *config.WorldConfig // optional: pre-loaded config (avoids double load)
}

// emitRollbackFailure logs a Cast-rollback soft failure and emits a
// structured "dispatch_rollback_failed" event so cross-package consumers
// (sol feed, chronicle, audit) can detect partial-rollback states.
//
// CD-5: each rollback step is its own observability site — UpdateAgentState
// failures during rollback leave the agent in "working" with no live
// resources, and consul/sentinel only catch this after the 15-minute stale
// tether timeout. Direct Emit lets operators see the partial rollback in
// real time via `sol feed --filter=dispatch_rollback_failed`.
func emitRollbackFailure(logger *events.Logger, op string, err error, fields map[string]any) {
	if err == nil {
		return
	}
	// softfail.Emit logs + emits the canonical soft_failure event so it shows
	// up in `sol feed`. We additionally publish a dedicated
	// dispatch_rollback_failed event so operators can filter on the specific
	// failure type without grepping through op strings.
	softfail.Emit(nil, logger, op, err, fields)
	if logger == nil {
		return
	}
	payload := make(map[string]any, len(fields)+2)
	maps.Copy(payload, fields)
	payload["op"] = op
	payload["error"] = err.Error()
	logger.Emit("dispatch_rollback_failed", "dispatch", "dispatch", "audit", payload)
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
	lock, err := flock.AcquireWritLock(opts.WritID)
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
	var provLocks *provisionLocks // non-nil when auto-provisioning created a new agent
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
			// Load sphere config for max_sessions limit.
			sphereCfg, scErr := config.LoadSphereConfig()
			if scErr != nil {
				return nil, fmt.Errorf("failed to load sphere config: %w", scErr)
			}
			// Auto-provision a new agent from the name pool.
			// provLocks holds the provision + sphere session locks; they are
			// released explicitly before the Launch call (step 11) to avoid
			// holding them during the 1.5s session verify sleep.
			agent, provLocks, err = autoProvision(opts.World, sphereStore, worldCfg.Agents.NamePoolPath, mgr, worldCfg.Agents.MaxActive, sphereCfg.MaxSessions)
			if err != nil {
				return nil, err
			}
			defer provLocks.Release() // safety net — explicit release at step 11
		}
	}

	agentID := opts.World + "/" + agent.Name

	// Acquire per-agent lock to prevent concurrent dispatch to same agent.
	agentLock, err := flock.AcquireAgentLock(agentID)
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
		if err := mgr.Stop(sessName, true); err != nil && !errors.Is(err, session.ErrNotFound) {
			fmt.Fprintf(os.Stderr, "cast: warning: failed to stop stale session %q: %v\n", sessName, err)
		}
	}

	// 6. Create worktree directory.
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
		// Branch exists from prior cast — reset it to HEAD so the new agent
		// starts from current main, not wherever the previous agent left it.
		resetCtx, resetCancel := context.WithTimeout(ctx, GitLocalOpTimeout)
		resetCmd := exec.CommandContext(resetCtx, "git", "-C", opts.SourceRepo, "branch", "-f", branchName, "HEAD")
		if resetOut, resetErr := resetCmd.CombinedOutput(); resetErr != nil {
			resetCancel()
			return nil, fmt.Errorf("failed to reset branch %s to HEAD: %s: %w",
				branchName, strings.TrimSpace(string(resetOut)), resetErr)
		}
		resetCancel()

		addCtx2, addCancel2 := context.WithTimeout(ctx, GitWorktreeAddTimeout)
		defer addCancel2()
		addCmd2 := exec.CommandContext(addCtx2, "git", "-C", opts.SourceRepo, "worktree", "add", worktreeDir, branchName)
		if out2, err2 := addCmd2.CombinedOutput(); err2 != nil {
			return nil, fmt.Errorf("failed to create worktree: %s: %w", strings.TrimSpace(string(out2)), err2)
		}
		_ = out // suppress unused
	}

	// Capture pre-call snapshots for capture-and-restore rollback (CD-2 / V9).
	// On re-cast (crash recovery), agent and writ are already in a valid binding
	// state. Snapshots ensure rollback restores the pre-existing binding rather
	// than wiping it with hardcoded "open"/"idle" defaults.
	prevAgentState := agent.State
	prevAgentActiveWrit := agent.ActiveWrit
	prevWritStatus := item.Status
	prevWritAssignee := item.Assignee
	tetherExistsBefore := tether.IsTetheredTo(opts.World, agent.Name, opts.WritID, agent.Role)

	// From here on, rollback on failure.
	// Track which steps have completed so rollback only undoes what succeeded.
	// Rollback executes in reverse order of the original operations.
	var (
		agentUpdated  bool
		tetherWritten bool
		writUpdated   bool
	)
	rollback := func() {
		// Release provision locks early on failure (idempotent; may already be released).
		provLocks.Release()
		// Stop the tmux session if it was partially created by Launch.
		// ErrNotFound is benign — the session is already gone.
		if rbErr := mgr.Stop(sessName, true); rbErr != nil && !errors.Is(rbErr, session.ErrNotFound) {
			emitRollbackFailure(logger, "dispatch.cast_rollback_session_stop", rbErr, map[string]any{
				"writ_id": opts.WritID,
				"agent":   agent.Name,
				"world":   opts.World,
				"session": sessName,
			})
		}
		// Remove worktree (always — it was created before this closure).
		rbCtx, rbCancel := context.WithTimeout(context.Background(), GitWorktreeRemoveTimeout)
		rmCmd := exec.CommandContext(rbCtx, "git", "-C", opts.SourceRepo, "worktree", "remove", "--force", worktreeDir)
		if out, rbErr := rmCmd.CombinedOutput(); rbErr != nil {
			emitRollbackFailure(logger, "dispatch.cast_rollback_worktree_remove", rbErr, map[string]any{
				"writ_id": opts.WritID,
				"agent":   agent.Name,
				"world":   opts.World,
				"dir":     worktreeDir,
				"output":  strings.TrimSpace(string(out)),
			})
		}
		rbCancel()
		// Clean up guidelines file if it was written.
		os.Remove(filepath.Join(worktreeDir, ".guidelines.md")) // best-effort
		// Undo state changes in reverse order: writ → tether → agent.
		// Use captured snapshots so re-cast rollback restores the pre-existing
		// binding instead of overwriting it with hardcoded defaults.
		if writUpdated {
			// Translate empty assignee (NULL in DB) to "-" (explicit clear signal).
			rbAssignee := prevWritAssignee
			if rbAssignee == "" {
				rbAssignee = "-"
			}
			if rbErr := worldStore.UpdateWrit(opts.WritID, store.WritUpdates{Status: prevWritStatus, Assignee: rbAssignee}); rbErr != nil {
				emitRollbackFailure(logger, "dispatch.cast_rollback_writ_update", rbErr, map[string]any{
					"writ_id": opts.WritID,
					"agent":   agent.Name,
					"world":   opts.World,
				})
			}
		}
		// Only clear the tether if we actually created it (tetherExistsBefore=false).
		// On re-cast the file already existed; clearing it would destroy a valid binding.
		if tetherWritten && !tetherExistsBefore {
			if rbErr := tether.ClearOne(opts.World, agent.Name, opts.WritID, agent.Role); rbErr != nil {
				emitRollbackFailure(logger, "dispatch.cast_rollback_tether_clear", rbErr, map[string]any{
					"writ_id": opts.WritID,
					"agent":   agent.Name,
					"world":   opts.World,
				})
			}
		}
		if agentUpdated {
			if rbErr := sphereStore.UpdateAgentState(agent.ID, prevAgentState, prevAgentActiveWrit); rbErr != nil {
				emitRollbackFailure(logger, "dispatch.cast_rollback_agent_state", rbErr, map[string]any{
					"writ_id":  opts.WritID,
					"agent":    agent.Name,
					"agent_id": agent.ID,
					"world":    opts.World,
				})
			}
		}
	}

	// 7. Update agent: state → working, active_writ → writ ID.
	// Done BEFORE tether.Write() to prevent a race with sentinel's
	// cleanupOrphanedTethers, which clears tether files for non-working agents.
	// If we wrote the tether first while agent is still "idle", a concurrent
	// sentinel patrol could clear it before we update agent state.
	if err := sphereStore.UpdateAgentState(agent.ID, "working", opts.WritID); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to update agent state: %w", err)
	}
	agentUpdated = true

	// 8. Write tether file.
	if err := tether.Write(opts.World, agent.Name, opts.WritID, "outpost"); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to write tether: %w", err)
	}
	tetherWritten = true

	// 9. Update writ: status → tethered, assignee → agent ID.
	if err := worldStore.UpdateWrit(opts.WritID, store.WritUpdates{
		Status:   "tethered",
		Assignee: agent.ID,
	}); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to update writ: %w", err)
	}
	writUpdated = true

	// 9b. Create persistent output directory for the writ.
	// Lives in world storage (not the worktree) and survives worktree cleanup.
	outputDir := config.WritOutputDir(opts.World, opts.WritID)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to create writ output directory: %w", err)
	}

	// 10. Resolve and write guidelines to the worktree.
	guidelinesName := guidelines.ResolveTemplateName(
		opts.Guidelines, item.Kind, config.GuidelinesSection(worldCfg.Guidelines),
	)
	res, err := guidelines.Resolve(guidelinesName, opts.SourceRepo)
	if err != nil {
		rollback()
		return nil, fmt.Errorf("failed to resolve guidelines %q: %w", guidelinesName, err)
	}
	// Render with variable substitution.
	vars := opts.Variables
	if vars == nil {
		vars = map[string]string{}
	}
	if _, ok := vars["issue"]; !ok {
		vars["issue"] = opts.WritID
	}
	rendered := guidelines.Render(string(res.Content), vars)
	guidelinesPath := filepath.Join(worktreeDir, ".guidelines.md")
	if err := fileutil.AtomicWrite(guidelinesPath, []byte(rendered), 0o644); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to write guidelines file: %w", err)
	}

	// 11. Launch the session via startup.Launch (persona, hooks, config dir,
	// system prompt, prime, command building, session start).
	//
	// Release provision locks before Launch to avoid holding them during the
	// 1.5s session verify sleep inside session.Start. The provisioning decision
	// (capacity check + agent creation) is complete; the agent is in "working"
	// state in the DB, so concurrent Cast calls won't double-dispatch.
	// provLocks.Release() is nil-safe and idempotent (defer above is the safety net).
	provLocks.Release()
	launchCfg := OutpostRoleConfig()
	launchOpts := startup.LaunchOpts{
		Sessions:    mgr,
		Sphere:      sphereStore,
		WorldConfig: &worldCfg,
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

	// Write history record for cycle-time tracking.
	if _, err := worldStore.WriteHistory(agent.Name, opts.WritID, "cast", "", time.Now(), nil); err != nil {
		fmt.Fprintf(os.Stderr, "cast: failed to write history: %v\n", err)
	}

	return &CastResult{
		WritID:      opts.WritID,
		AgentName:   agent.Name,
		SessionName: sessName,
		WorktreeDir: worktreeDir,
		Guidelines:  guidelinesName,
	}, nil
}

// persistentRoles are agent roles that can use sol tether/untether.
// Outpost agents must use sol cast instead.
var persistentRoles = map[string]bool{
	"envoy":    true,
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
		return nil, fmt.Errorf("agent %q has role %q — only persistent roles (envoy, forge) can use tether; outposts use sol cast", agentID, agent.Role)
	}

	// 3. Acquire per-writ lock, then per-agent lock (consistent ordering).
	lock, err := flock.AcquireWritLock(opts.WritID)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	agentLock, err := flock.AcquireAgentLock(agentID)
	if err != nil {
		return nil, err
	}
	defer agentLock.Release()

	// Re-read writ state inside the locked section to avoid a TOCTOU race:
	// a concurrent Cast may have tethered this writ between the pre-lock read
	// at step 1 and the writ lock acquisition above.
	item, err = worldStore.GetWrit(opts.WritID)
	if err != nil {
		return nil, fmt.Errorf("failed to re-read writ %q: %w", opts.WritID, err)
	}
	if item.Status != "open" {
		return nil, fmt.Errorf("writ %q has status %q, expected \"open\"", opts.WritID, item.Status)
	}

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
		if rbErr := sphereStore.UpdateAgentState(agentID, prevState, prevActiveWrit); rbErr != nil {
			slog.Warn("rollback failed", "op", "UpdateAgentState", "agent", agentID, "error", rbErr)
		}
		return nil, fmt.Errorf("failed to write tether: %w", err)
	}

	// 6. Update writ: status → tethered, assignee → agent ID.
	if err := worldStore.UpdateWrit(opts.WritID, store.WritUpdates{
		Status:   "tethered",
		Assignee: agent.ID,
	}); err != nil {
		// Rollback tether + agent state (reverse order).
		if rbErr := tether.ClearOne(opts.World, opts.AgentName, opts.WritID, agent.Role); rbErr != nil {
			slog.Warn("rollback failed", "op", "ClearOne", "writ", opts.WritID, "agent", opts.AgentName, "error", rbErr)
		}
		if rbErr := sphereStore.UpdateAgentState(agentID, prevState, prevActiveWrit); rbErr != nil {
			slog.Warn("rollback failed", "op", "UpdateAgentState", "agent", agentID, "error", rbErr)
		}
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
	lock, err := flock.AcquireWritLock(opts.WritID)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	agentLock, err := flock.AcquireAgentLock(agentID)
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

	// From here on, rollback on failure.
	// Track which steps have completed so rollback only undoes what succeeded.
	// Rollback executes in reverse order of the original operations.
	var (
		tetherCleared = true
		writUpdated   bool
	)
	rollback := func() {
		// Undo state changes in reverse order: writ → tether.
		if writUpdated {
			if rbErr := worldStore.UpdateWrit(opts.WritID, store.WritUpdates{
				Status:   "tethered",
				Assignee: agent.ID,
			}); rbErr != nil {
				slog.Warn("rollback failed", "op", "UpdateWrit", "writ", opts.WritID, "error", rbErr)
			}
		}
		if tetherCleared {
			if rbErr := tether.Write(opts.World, opts.AgentName, opts.WritID, agent.Role); rbErr != nil {
				slog.Warn("rollback failed", "op", "Write tether", "writ", opts.WritID, "agent", opts.AgentName, "error", rbErr)
			}
		}
	}

	// 5. Update writ: status → open, assignee → clear.
	if err := worldStore.UpdateWrit(opts.WritID, store.WritUpdates{
		Status:   "open",
		Assignee: "-",
	}); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to update writ: %w", err)
	}
	writUpdated = true

	// 6. If this was the active_writ, clear it.
	// If no remaining tethers, set agent to idle.
	remaining, err := tether.List(opts.World, opts.AgentName, agent.Role)
	if err != nil {
		rollback()
		return nil, fmt.Errorf("failed to list remaining tethers: %w", err)
	}

	if len(remaining) == 0 {
		// No more tethers — go idle.
		if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
			rollback()
			return nil, fmt.Errorf("failed to update agent state: %w", err)
		}
	} else if agent.ActiveWrit == opts.WritID {
		// Active writ was untethered — clear it but stay working.
		if err := sphereStore.UpdateAgentState(agentID, "working", ""); err != nil {
			rollback()
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

	// SessionRestartErr is non-nil when the active_writ DB update succeeded
	// but the subsequent session restart (Cycle / Stop+Start, via
	// startup.Resume) failed. The caller should treat the activate as a
	// soft-failure: the DB is updated and the resume state is on disk, but
	// no fresh session is running. CLI callers should report exit code 1.
	SessionRestartErr error
}

// ActivateOpts holds the inputs for an activate operation.
type ActivateOpts struct {
	World     string
	AgentName string
	WritID    string // writ to activate
}

// ActivateWrit switches the active writ for a persistent agent.
// The writ must already be tethered to the agent. If the writ is already
// active, this is a no-op (idempotent). Otherwise, updates active_writ in
// the DB and notifies the agent of the change.
//
// For envoys (and other non-forge persistent roles), the running session is
// kept alive and a "writ-activate" message is appended to the nudge queue
// via nudge.Deliver — the live conversation is preserved. For forge (and
// outpost during dispatch retry), a resume_state.json is written and the
// session is cycled atomically via startup.Resume so the new prime takes
// effect; restart failures are surfaced via the returned error and the
// SessionRestartErr field on the result.
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
	agentLock, err := flock.AcquireAgentLock(agentID)
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

	// L-M1 + CD-8: re-validate tether under the agent lock. A concurrent
	// dispatch.Untether (which holds the same agent lock) may have cleared
	// the tether between the pre-lock check at step 2 and lock acquisition
	// above. Without this re-check, ActivateWrit would leave active_writ
	// pointing at a writ that has no tether file — the next sol prime would
	// self-heal by clearing active_writ, but only after a stale resume
	// state had been written and possibly injected once. One extra os.Stat
	// per ActivateWrit closes the race.
	if !tether.IsTetheredTo(opts.World, opts.AgentName, opts.WritID, agent.Role) {
		return nil, fmt.Errorf("writ %q is not tethered to agent %q in world %q", opts.WritID, opts.AgentName, opts.World)
	}

	// 5. Update active_writ in DB.
	if err := sphereStore.UpdateAgentState(agentID, agent.State, opts.WritID); err != nil {
		return nil, fmt.Errorf("failed to update active writ: %w", err)
	}

	// 6. For persistent roles (envoy), nudge the running session
	// instead of cycling it — cycling destroys the live conversation.
	var sessionRestartErr error
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
			// L-M4: Stop / Start failures are returned so the caller (and the
			// CLI exit code) can reflect that the session never actually
			// restarted. ErrNotFound on Stop is benign — the session is
			// already gone, which is exactly the state Stop is trying to
			// reach.
			cycleOp := func(name, workdir, cmd string, env map[string]string, role, world string) error {
				if err := mgr.Cycle(name, workdir, cmd, env, role, world); err != nil {
					if stopErr := mgr.Stop(name, true); stopErr != nil && !errors.Is(stopErr, session.ErrNotFound) {
						return fmt.Errorf("session stop failed: %w", stopErr)
					}
					if startErr := mgr.Start(name, workdir, cmd, env, role, world); startErr != nil {
						return fmt.Errorf("session start failed: %w", startErr)
					}
					return nil
				}
				return nil
			}

			// CD-6: hand BuildResumePrime a way to detect a phantom writ.
			// If the writ being activated is closed/deleted between this
			// call and the next session's prime read, the resume prime
			// will substitute a degraded notice instead of printing a
			// dangling writ id that `sol prime` would then fail on.
			writExists := func(id string) bool {
				if id == "" {
					return true
				}
				_, err := worldStore.GetWrit(id)
				if err != nil {
					if errors.Is(err, store.ErrNotFound) {
						return false
					}
					// Transient error: prefer "exists" so a flaky read
					// does not strip the active writ from the prime.
					return true
				}
				return true
			}

			launchOpts := startup.LaunchOpts{
				SessionOp:  cycleOp,
				WritExists: writExists,
			}

			if _, err := startup.Resume(*cfg, opts.World, opts.AgentName, resumeState, launchOpts); err != nil {
				// L-M4: surface the failure. The DB is updated and the
				// resume state is on disk (so the next session start, when
				// it eventually happens, will pick it up). But the operator
				// invoked `sol writ activate` and the session never
				// restarted; returning success would hide that until consul
				// or prefect notice. Record on the result and propagate.
				sessionRestartErr = fmt.Errorf("session restart failed (resume state preserved): %w", err)
				fmt.Fprintf(os.Stderr, "activate: %v\n", sessionRestartErr)
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

	result := &ActivateResult{
		WritID:            opts.WritID,
		PreviousWrit:      previousWrit,
		SessionRestartErr: sessionRestartErr,
	}
	if sessionRestartErr != nil {
		return result, sessionRestartErr
	}
	return result, nil
}

// provisionLocks bundles the advisory locks acquired by autoProvision.
// The caller (Cast) must release them after session creation so the locks
// span both the capacity check and the actual tmux session start, closing
// the TOCTOU window.
type provisionLocks struct {
	provision *flock.ProvisionLock
	sphere    *flock.SphereSessionLock
}

// Release releases both locks. It is safe to call on nil fields.
func (pl *provisionLocks) Release() {
	if pl == nil {
		return
	}
	if pl.sphere != nil {
		pl.sphere.Release()
		pl.sphere = nil
	}
	if pl.provision != nil {
		pl.provision.Release()
		pl.provision = nil
	}
}

// autoProvision creates a new agent from the name pool.
// A per-world provision lock is held for the entire capacity-check + CreateAgent
// sequence, so concurrent Cast calls cannot both pass the capacity check and
// both create an agent (which would silently exceed the world limit).
//
// The returned provisionLocks should be released by the caller before the
// session launch to avoid holding the sphere-wide lock during the 1.5s
// session verify sleep. The provisioning decision (capacity check + agent
// creation) is complete at return; the agent is in "idle" state and will be
// set to "working" before launch, which prevents concurrent double-dispatch.
func autoProvision(world string, sphereStore SphereStore, namePoolPath string, mgr SessionManager, maxActive int, maxSessions int) (*store.Agent, *provisionLocks, error) {
	overridePath := namePoolPath
	if overridePath == "" {
		overridePath = filepath.Join(config.Home(), world, "names.txt")
	}
	pool, err := namepool.Load(overridePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load name pool: %w", err)
	}

	locks := &provisionLocks{}

	// Acquire a per-world provision lock before the capacity check.
	// This serializes concurrent autoProvision calls so that only one can
	// proceed through the check-and-create window at a time.
	locks.provision, err = flock.AcquireProvisionLock(world)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to serialize provisioning for world %q: %w", world, err)
	}

	// Enforce per-world active session limit.
	if maxActive > 0 {
		worldPrefix := "sol-" + world + "-"
		count, err := mgr.CountSessions(worldPrefix)
		if err != nil {
			locks.Release()
			return nil, nil, fmt.Errorf("failed to count sessions for world %q: %w", world, err)
		}
		if count >= maxActive {
			locks.Release()
			return nil, nil, fmt.Errorf("world %q has reached active session limit (%d): %w", world, maxActive, ErrCapacityExhausted)
		}
	}

	// Enforce sphere-wide session limit.
	if maxSessions > 0 {
		locks.sphere, err = flock.AcquireSphereSessionLock()
		if err != nil {
			locks.Release()
			return nil, nil, fmt.Errorf("failed to acquire sphere session lock: %w", err)
		}

		count, err := mgr.CountSessions("sol-")
		if err != nil {
			locks.Release()
			return nil, nil, fmt.Errorf("failed to count sphere sessions: %w", err)
		}
		if count >= maxSessions {
			locks.Release()
			return nil, nil, fmt.Errorf("sphere has reached session limit (%d): %w", maxSessions, ErrSphereCapacityExhausted)
		}
	}

	agents, err := sphereStore.ListAgents(world, "")
	if err != nil {
		locks.Release()
		return nil, nil, fmt.Errorf("failed to list agents for world %q: %w", world, err)
	}

	usedNames := make([]string, len(agents))
	for i, a := range agents {
		usedNames[i] = a.Name
	}

	name, err := pool.AllocateName(usedNames)
	if err != nil {
		locks.Release()
		return nil, nil, err
	}

	id, err := sphereStore.CreateAgent(name, world, "outpost")
	if err != nil {
		locks.Release()
		return nil, nil, fmt.Errorf("failed to create agent %q: %w", name, err)
	}

	return &store.Agent{
		ID:    id,
		Name:  name,
		World: world,
		Role:  "outpost",
		State: "idle",
	}, locks, nil
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
