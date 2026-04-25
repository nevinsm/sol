package dispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/account"
	"github.com/nevinsm/sol/internal/budget"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/flock"
	"github.com/nevinsm/sol/internal/guidelines"
	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/namepool"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/session"
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
	DailySpendByAccount(account string) (float64, error)
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

	// 0c. Check account budget before dispatching.
	if len(worldCfg.Budget.Accounts) > 0 {
		castAccount := opts.Account
		if castAccount == "" {
			castAccount = account.ResolveAccount("", worldCfg.World.DefaultAccount)
		}
		if castAccount != "" {
			if err := budget.CheckAccountBudget(worldStore, sphereStore, castAccount, worldCfg.Budget); err != nil {
				return nil, err
			}
		}
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
			// provLocks holds the provision + sphere session locks; they must
			// remain held until after session creation to close the TOCTOU window.
			var provLocks *provisionLocks
			agent, provLocks, err = autoProvision(opts.World, sphereStore, worldCfg.Agents.NamePoolPath, mgr, worldCfg.Agents.MaxActive, sphereCfg.MaxSessions)
			if err != nil {
				return nil, err
			}
			defer provLocks.Release()
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

	// From here on, rollback on failure.
	// Track which steps have completed so rollback only undoes what succeeded.
	// Rollback executes in reverse order of the original operations.
	var (
		agentUpdated  bool
		tetherWritten bool
		writUpdated   bool
	)
	rollback := func() {
		// Stop the tmux session if it was partially created by Launch.
		// ErrNotFound is benign — the session is already gone.
		if rbErr := mgr.Stop(sessName, true); rbErr != nil && !errors.Is(rbErr, session.ErrNotFound) {
			slog.Warn("rollback failed", "op", "Stop", "session", sessName, "error", rbErr)
		}
		// Remove worktree (always — it was created before this closure).
		rbCtx, rbCancel := context.WithTimeout(context.Background(), GitWorktreeRemoveTimeout)
		rmCmd := exec.CommandContext(rbCtx, "git", "-C", opts.SourceRepo, "worktree", "remove", "--force", worktreeDir)
		if out, rbErr := rmCmd.CombinedOutput(); rbErr != nil {
			slog.Warn("rollback failed", "op", "worktree remove", "dir", worktreeDir, "output", strings.TrimSpace(string(out)), "error", rbErr)
		}
		rbCancel()
		// Clean up guidelines file if it was written.
		os.Remove(filepath.Join(worktreeDir, ".guidelines.md")) // best-effort
		// Undo state changes in reverse order: writ → tether → agent.
		if writUpdated {
			if rbErr := worldStore.UpdateWrit(opts.WritID, store.WritUpdates{Status: "open", Assignee: "-"}); rbErr != nil {
				slog.Warn("rollback failed", "op", "UpdateWrit", "writ", opts.WritID, "error", rbErr)
			}
		}
		if tetherWritten {
			if rbErr := tether.Clear(opts.World, agent.Name, "outpost"); rbErr != nil {
				slog.Warn("rollback failed", "op", "Clear tether", "agent", agent.Name, "error", rbErr)
			}
		}
		if agentUpdated {
			if rbErr := sphereStore.UpdateAgentState(agent.ID, "idle", ""); rbErr != nil {
				slog.Warn("rollback failed", "op", "UpdateAgentState", "agent", agent.ID, "error", rbErr)
			}
		}
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
	agentUpdated = true

	// 5. Write tether file.
	if err := tether.Write(opts.World, agent.Name, opts.WritID, "outpost"); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to write tether: %w", err)
	}
	tetherWritten = true

	// 6. Update writ: status → tethered, assignee → agent ID.
	if err := worldStore.UpdateWrit(opts.WritID, store.WritUpdates{
		Status:   "tethered",
		Assignee: agent.ID,
	}); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to update writ: %w", err)
	}
	writUpdated = true

	// 6b. Create persistent output directory for the writ.
	// Lives in world storage (not the worktree) and survives worktree cleanup.
	outputDir := config.WritOutputDir(opts.World, opts.WritID)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to create writ output directory: %w", err)
	}

	// 7. Resolve and write guidelines to the worktree.
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
	if err := os.WriteFile(guidelinesPath, []byte(rendered), 0o644); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to write guidelines file: %w", err)
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

	// 5. Update active_writ in DB.
	if err := sphereStore.UpdateAgentState(agentID, agent.State, opts.WritID); err != nil {
		return nil, fmt.Errorf("failed to update active writ: %w", err)
	}

	// 6. For persistent roles (envoy), nudge the running session
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
// The returned provisionLocks MUST be released by the caller after the tmux
// session has been created. This ensures the sphere-wide session lock spans
// from capacity check through session start, preventing TOCTOU races where
// concurrent Cast calls to different worlds each pass the check and then both
// start sessions, exceeding max_sessions.
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
		// Standard prime — inject writ context and guidelines if present.
		var b strings.Builder
		fmt.Fprintf(&b, "=== WORK CONTEXT ===\n")
		fmt.Fprintf(&b, "Agent: %s (world: %s)\n", agentName, world)
		fmt.Fprintf(&b, "Writ: %s\n", item.ID)
		fmt.Fprintf(&b, "Title: %s\n", item.Title)
		fmt.Fprintf(&b, "Status: %s\n", item.Status)
		fmt.Fprintf(&b, "\nDescription:\n%s\n", item.Description)

		// Inject guidelines if .guidelines.md exists in the worktree.
		worktreeDir := WorktreePath(world, agentName)
		guidelinesPath := filepath.Join(worktreeDir, ".guidelines.md")
		if guidelinesContent, err := os.ReadFile(guidelinesPath); err == nil && len(guidelinesContent) > 0 {
			fmt.Fprintf(&b, "\n--- GUIDELINES ---\n")
			b.Write(guidelinesContent)
			fmt.Fprintf(&b, "\n--- END GUIDELINES ---\n")
		} else {
			fmt.Fprintf(&b, "\nInstructions:\n")
			fmt.Fprintf(&b, "Execute this writ. When complete, run: sol resolve\n")
			fmt.Fprintf(&b, "If stuck, run: sol escalate \"description\"\n")
		}

		fmt.Fprintf(&b, "=== END CONTEXT ===")
		result = &PrimeResult{Output: b.String()}
	}

	// Append background writ summaries for persistent agents with multiple tethers.
	if isPersistent && len(allWritIDs) > 1 && result != nil {
		bgSection := primeBackgroundWrits(activeWritID, allWritIDs, worldStore)
		if bgSection != "" {
			result.Output += bgSection
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
		// Persistent roles (envoy) may have no tether during freeform
		// conversation — return a role-appropriate grounding reminder.
		if role == "envoy" {
			return &PrimeResult{Output: fmt.Sprintf(
				"[sol] Context compaction in progress. You are envoy %s in world %s.\nYour persistent memory is at <envoyDir>/memory/MEMORY.md (Claude Code auto-memory) — use /memory to review.\nContinue the current conversation.",
				agentName, world)}, nil
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
