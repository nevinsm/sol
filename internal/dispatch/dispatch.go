package dispatch

import (
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
	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/nevinsm/sol/internal/workflow"
)

// SessionManager defines the session operations used by the dispatch package.
type SessionManager interface {
	Start(name, workdir, cmd string, env map[string]string, role, world string) error
	Stop(name string, force bool) error
	Exists(name string) bool
	Inject(name string, text string, submit bool) error
}

// WorldStore defines the world store operations used by dispatch.
type WorldStore interface {
	GetWorkItem(id string) (*store.WorkItem, error)
	UpdateWorkItem(id string, updates store.WorkItemUpdates) error
	CreateMergeRequest(workItemID, branch string, priority int) (string, error)
	ListMergeRequestsByWorkItem(workItemID, phase string) ([]store.MergeRequest, error)
	UpdateMergeRequestPhase(id, phase string) error
	CreateWorkItemWithOpts(opts store.CreateWorkItemOpts) (string, error)
	FindMergeRequestByBlocker(blockerID string) (*store.MergeRequest, error)
	UnblockMergeRequest(mrID string) error
	CloseWorkItem(id string) error
	ListChildWorkItems(parentID string) ([]store.WorkItem, error)
	ListAgentMemories(agentName string) ([]store.AgentMemory, error)
	Close() error
}

// SphereStore defines the sphere store operations used by dispatch.
type SphereStore interface {
	GetAgent(id string) (*store.Agent, error)
	FindIdleAgent(world string) (*store.Agent, error)
	UpdateAgentState(id, state, tetherItem string) error
	ListAgents(world string, state string) ([]store.Agent, error)
	CreateAgent(name, world, role string) (string, error)
	DeleteAgent(id string) error
	Close() error
}

// SessionName returns the tmux session name for an agent.
func SessionName(world, agentName string) string {
	return config.SessionName(world, agentName)
}

// WorktreePath returns the worktree directory for an agent.
func WorktreePath(world, agentName string) string {
	return config.WorktreePath(world, agentName)
}

// cleanupWorktree removes a git worktree and prunes stale references.
// Best-effort: logs what was cleaned up but does not fail.
func cleanupWorktree(world, worktreeDir string) {
	repoPath := config.RepoPath(world)

	rmCmd := exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", worktreeDir)
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

	pruneCmd := exec.Command("git", "-C", repoPath, "worktree", "prune")
	if out, err := pruneCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "resolve: worktree prune failed: %s: %v\n",
			strings.TrimSpace(string(out)), err)
	}
}

// CastResult holds the output of a successful cast operation.
type CastResult struct {
	WorkItemID  string
	AgentName   string
	SessionName string
	WorktreeDir string
	Formula     string // empty if no workflow
}

// CastOpts holds the inputs for a cast operation.
type CastOpts struct {
	WorkItemID  string
	World       string
	AgentName   string              // optional: if empty, find an idle agent
	SourceRepo  string              // path to the source git repo
	Formula     string              // optional: formula name for workflow
	Variables   map[string]string   // optional: workflow variables
	WorldConfig *config.WorldConfig // optional: pre-loaded config (avoids double load)
}

// Cast assigns a work item to an outpost agent and starts its session.
// Supports re-cast (crash recovery): if the item is already tethered to the
// same agent, Cast recreates the worktree and session without error.
// The logger parameter is optional — if nil, no events are emitted.
func Cast(opts CastOpts, worldStore WorldStore, sphereStore SphereStore, mgr SessionManager, logger *events.Logger) (*CastResult, error) {
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

	// 1. Acquire per-work-item advisory lock to prevent double dispatch.
	lock, err := AcquireWorkItemLock(opts.WorkItemID)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	// 2. Get work item.
	item, err := worldStore.GetWorkItem(opts.WorkItemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work item %q: %w", opts.WorkItemID, err)
	}

	// 3. Find the agent.
	var agent *store.Agent
	if opts.AgentName != "" {
		agentID := opts.World + "/" + opts.AgentName
		agent, err = sphereStore.GetAgent(agentID)
		if err != nil {
			return nil, fmt.Errorf("failed to get agent %q: %w", agentID, err)
		}
		if agent.Role != "agent" {
			return nil, fmt.Errorf("cannot dispatch to %s agents — sol cast targets outpost agents only (got %s)", agent.Role, agent.Name)
		}
	} else {
		if item.Status != "open" {
			return nil, fmt.Errorf("work item %q has status %q, expected \"open\"", opts.WorkItemID, item.Status)
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

	// 4. Determine if this is a re-cast (crash recovery).
	// Full match: all four fields consistent (clean re-cast).
	// Partial match: work item is tethered to this agent but agent state is stale.
	// This handles crashes between work item update and agent state update.
	reCast := false
	if item.Status == "tethered" && item.Assignee == agentID {
		if agent.State == "working" && agent.TetherItem == opts.WorkItemID {
			reCast = true // clean re-cast
		} else if agent.State == "idle" && (agent.TetherItem == "" || agent.TetherItem == opts.WorkItemID) {
			reCast = true // partial failure recovery — agent wasn't updated
		}
	}

	// 5. Validate state.
	if !reCast {
		if item.Status != "open" {
			return nil, fmt.Errorf("work item %q has status %q, expected \"open\"", opts.WorkItemID, item.Status)
		}
		if agent.State != "idle" {
			return nil, fmt.Errorf("agent %q has state %q, expected \"idle\"", agentID, agent.State)
		}
	}

	worktreeDir := WorktreePath(opts.World, agent.Name)
	sessName := SessionName(opts.World, agent.Name)
	branchName := fmt.Sprintf("outpost/%s/%s", agent.Name, opts.WorkItemID)

	// Clean up any stale session (race between resolve teardown and next cast,
	// crashed agents, interrupted stops, etc.).
	if mgr.Exists(sessName) {
		mgr.Stop(sessName, true)
	}

	// 5. Create worktree directory.
	// Remove existing worktree if present.
	if _, err := os.Stat(worktreeDir); err == nil {
		rmCmd := exec.Command("git", "-C", opts.SourceRepo, "worktree", "remove", "--force", worktreeDir)
		rmCmd.Run() // best-effort
		os.RemoveAll(worktreeDir)
	}
	// Prune stale worktree references.
	pruneCmd := exec.Command("git", "-C", opts.SourceRepo, "worktree", "prune")
	pruneCmd.Run()

	// Try creating worktree with new branch; fall back to existing branch (re-cast).
	addCmd := exec.Command("git", "-C", opts.SourceRepo, "worktree", "add", worktreeDir, "-b", branchName, "HEAD")
	if out, err := addCmd.CombinedOutput(); err != nil {
		addCmd2 := exec.Command("git", "-C", opts.SourceRepo, "worktree", "add", worktreeDir, branchName)
		if out2, err2 := addCmd2.CombinedOutput(); err2 != nil {
			return nil, fmt.Errorf("failed to create worktree: %s: %w", strings.TrimSpace(string(out2)), err2)
		}
		_ = out // suppress unused
	}

	// From here on, rollback on failure.
	rollback := func() {
		if err := tether.Clear(opts.World, agent.Name, "agent"); err != nil {
			fmt.Fprintf(os.Stderr, "rollback: failed to clear tether: %v\n", err)
		}
		if err := worldStore.UpdateWorkItem(opts.WorkItemID, store.WorkItemUpdates{Status: "open", Assignee: "-"}); err != nil {
			fmt.Fprintf(os.Stderr, "rollback: failed to reset work item: %v\n", err)
		}
		if err := sphereStore.UpdateAgentState(agent.ID, "idle", ""); err != nil {
			fmt.Fprintf(os.Stderr, "rollback: failed to reset agent state: %v\n", err)
		}
		rmCmd := exec.Command("git", "-C", opts.SourceRepo, "worktree", "remove", "--force", worktreeDir)
		if out, err := rmCmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "rollback: failed to remove worktree: %s\n", strings.TrimSpace(string(out)))
		}
		// Clean up workflow if it was instantiated.
		workflow.Remove(opts.World, agent.Name, "agent") // best-effort
	}

	// 4. Write tether file.
	if err := tether.Write(opts.World, agent.Name, opts.WorkItemID, "agent"); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to write tether: %w", err)
	}

	// 5. Update work item: status → tethered, assignee → agent ID.
	if err := worldStore.UpdateWorkItem(opts.WorkItemID, store.WorkItemUpdates{
		Status:   "tethered",
		Assignee: agent.ID,
	}); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to update work item: %w", err)
	}

	// 6. Update agent: state → working, tether_item → work item ID.
	if err := sphereStore.UpdateAgentState(agent.ID, "working", opts.WorkItemID); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to update agent state: %w", err)
	}

	// 7. Install CLAUDE.local.md in the worktree (agent persona).
	ctx := protocol.ClaudeMDContext{
		AgentName:    agent.Name,
		World:        opts.World,
		WorkItemID:   opts.WorkItemID,
		Title:        item.Title,
		Description:  item.Description,
		HasWorkflow:  opts.Formula != "",
		ModelTier:    worldCfg.Agents.ModelTier,
		QualityGates: worldCfg.Forge.QualityGates,
	}
	if err := protocol.InstallClaudeMD(worktreeDir, ctx); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to install CLAUDE.local.md: %w", err)
	}

	// 8. Install Claude Code hooks in the worktree.
	if err := protocol.InstallHooks(worktreeDir, opts.World, agent.Name); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to install hooks: %w", err)
	}

	// 8b. Instantiate workflow if formula provided.
	if opts.Formula != "" {
		vars := opts.Variables
		if vars == nil {
			vars = map[string]string{}
		}
		// Always set "issue" variable to the work item ID.
		if _, ok := vars["issue"]; !ok {
			vars["issue"] = opts.WorkItemID
		}
		if _, _, err := workflow.Instantiate(opts.World, agent.Name, "agent", opts.Formula, vars); err != nil {
			rollback()
			return nil, fmt.Errorf("failed to instantiate workflow %q: %w", opts.Formula, err)
		}
	}

	// 9. Start tmux session.
	env := map[string]string{
		"SOL_HOME":  config.Home(),
		"SOL_WORLD": opts.World,
		"SOL_AGENT": agent.Name,
	}
	prompt := fmt.Sprintf("Agent %s, world %s. If no context appears, run: sol prime --world=%s --agent=%s",
		agent.Name, opts.World, opts.World, agent.Name)
	sessionCmd := config.BuildSessionCommand(config.SettingsPath(worktreeDir), prompt)
	if err := mgr.Start(sessName, worktreeDir, sessionCmd, env, "agent", opts.World); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to start session: %w", err)
	}

	castPayload := map[string]string{
		"work_item_id": opts.WorkItemID,
		"agent":        agent.Name,
		"world":        opts.World,
	}
	if logger != nil {
		logger.Emit(events.EventCast, "sol", "operator", "both", castPayload)
	}

	// Emit workflow instantiation event if formula was used.
	if opts.Formula != "" && logger != nil {
		logger.Emit(events.EventWorkflowInstantiate, "sol", "operator", "both", map[string]string{
			"formula":      opts.Formula,
			"work_item_id": opts.WorkItemID,
			"agent":        agent.Name,
			"world":        opts.World,
		})
	}

	return &CastResult{
		WorkItemID:  opts.WorkItemID,
		AgentName:   agent.Name,
		SessionName: sessName,
		WorktreeDir: worktreeDir,
		Formula:     opts.Formula,
	}, nil
}

// TetherResult holds the output of a successful tether operation.
type TetherResult struct {
	WorkItemID string
	AgentName  string
	AgentRole  string
}

// TetherOpts holds the inputs for a tether operation.
type TetherOpts struct {
	AgentName  string
	WorkItemID string
	World      string
}

// Tether binds a work item to an agent without creating worktrees or sessions.
// Works with any agent role. For outpost agents that need worktrees, use Cast instead.
// The logger parameter is optional — if nil, no events are emitted.
func Tether(opts TetherOpts, worldStore WorldStore, sphereStore SphereStore, logger *events.Logger) (*TetherResult, error) {
	agentID := opts.World + "/" + opts.AgentName

	// 1. Acquire per-work-item advisory lock.
	lock, err := AcquireWorkItemLock(opts.WorkItemID)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	// 2. Get agent.
	agent, err := sphereStore.GetAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", agentID, err)
	}

	// 3. Acquire per-agent lock.
	agentLock, err := AcquireAgentLock(agentID)
	if err != nil {
		return nil, err
	}
	defer agentLock.Release()

	// 4. Get work item.
	item, err := worldStore.GetWorkItem(opts.WorkItemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work item %q: %w", opts.WorkItemID, err)
	}

	// 5. Validate state.
	if item.Status != "open" {
		return nil, fmt.Errorf("work item %q has status %q, expected \"open\"", opts.WorkItemID, item.Status)
	}
	if agent.State != "idle" {
		return nil, fmt.Errorf("agent %q has state %q, expected \"idle\"", agentID, agent.State)
	}

	// 6. Write tether file (role-aware path).
	if err := tether.Write(opts.World, opts.AgentName, opts.WorkItemID, agent.Role); err != nil {
		return nil, fmt.Errorf("failed to write tether: %w", err)
	}

	// 7. Update work item: status → tethered, assignee → agent ID.
	if err := worldStore.UpdateWorkItem(opts.WorkItemID, store.WorkItemUpdates{
		Status:   "tethered",
		Assignee: agent.ID,
	}); err != nil {
		// Rollback tether.
		tether.Clear(opts.World, opts.AgentName, agent.Role)
		return nil, fmt.Errorf("failed to update work item: %w", err)
	}

	// 8. Update agent: state → working, tether_item → work item ID.
	if err := sphereStore.UpdateAgentState(agentID, "working", opts.WorkItemID); err != nil {
		// Rollback tether + work item.
		tether.Clear(opts.World, opts.AgentName, agent.Role)
		worldStore.UpdateWorkItem(opts.WorkItemID, store.WorkItemUpdates{Status: "open", Assignee: "-"})
		return nil, fmt.Errorf("failed to update agent state: %w", err)
	}

	// 9. Emit event.
	if logger != nil {
		logger.Emit(events.EventTether, "sol", "operator", "both", map[string]string{
			"work_item_id": opts.WorkItemID,
			"agent":        opts.AgentName,
			"world":        opts.World,
			"role":         agent.Role,
		})
	}

	return &TetherResult{
		WorkItemID: opts.WorkItemID,
		AgentName:  opts.AgentName,
		AgentRole:  agent.Role,
	}, nil
}

// UntetherResult holds the output of a successful untether operation.
type UntetherResult struct {
	WorkItemID string
	AgentName  string
	AgentRole  string
}

// UntetherOpts holds the inputs for an untether operation.
type UntetherOpts struct {
	AgentName string
	World     string
}

// Untether unbinds a work item from an agent without stopping sessions or cleaning worktrees.
// Reverses the state changes made by Tether: clears tether file, resets work item to open,
// and resets agent to idle.
// The logger parameter is optional — if nil, no events are emitted.
func Untether(opts UntetherOpts, worldStore WorldStore, sphereStore SphereStore, logger *events.Logger) (*UntetherResult, error) {
	agentID := opts.World + "/" + opts.AgentName

	// 1. Get agent (needed for role-aware tether path).
	agent, err := sphereStore.GetAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", agentID, err)
	}

	// 2. Read tether to get work item ID.
	workItemID, err := tether.Read(opts.World, opts.AgentName, agent.Role)
	if err != nil {
		return nil, fmt.Errorf("failed to read tether: %w", err)
	}
	if workItemID == "" {
		return nil, fmt.Errorf("no work tethered for agent %q in world %q", opts.AgentName, opts.World)
	}

	// 3. Acquire locks: work item first, then agent (consistent ordering).
	lock, err := AcquireWorkItemLock(workItemID)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	agentLock, err := AcquireAgentLock(agentID)
	if err != nil {
		return nil, err
	}
	defer agentLock.Release()

	// 4. Clear tether file.
	if err := tether.Clear(opts.World, opts.AgentName, agent.Role); err != nil {
		return nil, fmt.Errorf("failed to clear tether: %w", err)
	}

	// 5. Update work item: status → open, assignee → clear.
	if err := worldStore.UpdateWorkItem(workItemID, store.WorkItemUpdates{
		Status:   "open",
		Assignee: "-",
	}); err != nil {
		return nil, fmt.Errorf("failed to update work item: %w", err)
	}

	// 6. Update agent: state → idle, tether_item → clear.
	if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
		return nil, fmt.Errorf("failed to update agent state: %w", err)
	}

	// 7. Emit event.
	if logger != nil {
		logger.Emit(events.EventUntether, "sol", "operator", "both", map[string]string{
			"work_item_id": workItemID,
			"agent":        opts.AgentName,
			"world":        opts.World,
			"role":         agent.Role,
		})
	}

	return &UntetherResult{
		WorkItemID: workItemID,
		AgentName:  opts.AgentName,
		AgentRole:  agent.Role,
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
		return nil, fmt.Errorf("world %q has reached agent capacity (%d)", world, capacity)
	}

	usedNames := make([]string, len(agents))
	for i, a := range agents {
		usedNames[i] = a.Name
	}

	name, err := pool.AllocateName(usedNames)
	if err != nil {
		return nil, err
	}

	id, err := sphereStore.CreateAgent(name, world, "agent")
	if err != nil {
		return nil, fmt.Errorf("failed to create agent %q: %w", name, err)
	}

	return &store.Agent{
		ID:    id,
		Name:  name,
		World: world,
		Role:  "agent",
		State: "idle",
	}, nil
}

// PrimeResult holds the output of a prime operation.
type PrimeResult struct {
	Output string
}

// Prime assembles execution context from durable state and returns it.
func Prime(world, agentName, role string, worldStore WorldStore) (*PrimeResult, error) {
	if role == "" {
		role = "agent"
	}

	// Forge gets a special prime context.
	if role == "forge" {
		return primeForge(world)
	}

	// Check for stale resolve lock (previous session died mid-resolve).
	if IsResolveInProgress(world, agentName, role) {
		lockPath := ResolveLockPath(world, agentName, role)
		os.Remove(lockPath) // clean up stale lock
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

	// Read the tether file.
	workItemID, err := tether.Read(world, agentName, role)
	if err != nil {
		return nil, fmt.Errorf("failed to read tether: %w", err)
	}
	if workItemID == "" {
		return &PrimeResult{Output: "No work tethered"}, nil
	}

	// Get the work item.
	item, err := worldStore.GetWorkItem(workItemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work item %q: %w", workItemID, err)
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
Work Item: %s
Title: %s
Status: %s

Description:
%s

Instructions:
Execute this work item. When complete, run: sol resolve
If stuck, run: sol escalate "description"
=== END CONTEXT ===`, agentName, world, item.ID, item.Title, item.Status, item.Description)
			result = &PrimeResult{Output: output}
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

// primeWithWorkflow returns workflow-aware context for the prime command.
func primeWithWorkflow(world, agentName, role string, item *store.WorkItem,
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
	formula := ""
	if instance != nil {
		formula = instance.Formula
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
Work Item: %s
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
		formula, currentIdx+1, totalSteps, currentStep.Title,
		strings.TrimRight(checklist.String(), "\n"),
		currentStep.Instructions,
		world, agentName)

	return &PrimeResult{Output: output}, nil
}

// primeWithHandoff returns handoff-aware context for the prime command.
func primeWithHandoff(world, agentName string, item *store.WorkItem,
	state *handoff.State) (*PrimeResult, error) {

	output := fmt.Sprintf(`=== HANDOFF CONTEXT ===
Agent: %s (world: %s)
Work Item: %s
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
// context compaction. Unlike primeWithHandoff, it omits the full work item
// description because the agent has compressed context from its predecessor
// session (via --continue). This saves tokens and avoids confusing the agent
// about whether it's starting fresh or continuing.
func primeCompactRecovery(world, agentName string, item *store.WorkItem,
	state *handoff.State) (*PrimeResult, error) {

	var b strings.Builder
	fmt.Fprintf(&b, `=== SESSION RECOVERY ===
Agent: %s (world: %s)
Work Item: %s — %s
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
Continue from where you left off. Do NOT re-read the work item description
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

// primeConvoySynthesis returns enriched context for a convoy synthesis work item.
// It lists all sibling leg work items, their titles, and their merge request branches.
func primeConvoySynthesis(world, agentName string, item *store.WorkItem,
	worldStore WorldStore) (*PrimeResult, error) {

	var legSection strings.Builder
	legSection.WriteString("## Convoy Legs\n")
	legSection.WriteString("The following leg work items have been merged. Their changes are in your worktree.\n\n")

	if item.ParentID != "" {
		siblings, err := worldStore.ListChildWorkItems(item.ParentID)
		if err != nil {
			return nil, fmt.Errorf("failed to list sibling work items: %w", err)
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
			mrs, err := worldStore.ListMergeRequestsByWorkItem(sib.ID, "")
			if err == nil && len(mrs) > 0 {
				branch = mrs[0].Branch
			}
			legSection.WriteString(fmt.Sprintf("- **%s** (%s)\n  Branch: %s | Status: %s\n", sib.Title, sib.ID, branch, sib.Status))
		}
	}

	output := fmt.Sprintf(`=== WORK CONTEXT ===
Agent: %s (world: %s)
Work Item: %s
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
	WorkItemID     string
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
}

// ResolveLockPath returns the path to the resolve-in-progress lock file.
func ResolveLockPath(world, agentName, role string) string {
	return filepath.Join(config.AgentDir(world, agentName, role), ".resolve_in_progress")
}

// IsResolveInProgress returns true if a resolve lock file exists for this agent.
func IsResolveInProgress(world, agentName, role string) bool {
	_, err := os.Stat(ResolveLockPath(world, agentName, role))
	return err == nil
}

// Resolve signals work completion: git operations, state updates, tether clear.
// The logger parameter is optional — if nil, no events are emitted.
func Resolve(opts ResolveOpts, worldStore WorldStore, sphereStore SphereStore, mgr SessionManager, logger *events.Logger) (*ResolveResult, error) {
	agentID := opts.World + "/" + opts.AgentName
	sessName := SessionName(opts.World, opts.AgentName)

	// Look up agent first to determine role (needed for role-aware tether path).
	agent, err := sphereStore.GetAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", agentID, err)
	}

	// Create resolve lock to prevent handoff from interrupting.
	lockPath := ResolveLockPath(opts.World, opts.AgentName, agent.Role)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}

	// 1. Read tether — get work item ID.
	workItemID, err := tether.Read(opts.World, opts.AgentName, agent.Role)
	if err != nil {
		return nil, fmt.Errorf("failed to read tether: %w", err)
	}
	if workItemID == "" {
		return nil, fmt.Errorf("no work tethered for agent %q in world %q", opts.AgentName, opts.World)
	}

	// Write resolve lock with work item ID (enables crash recovery detection).
	if err := os.WriteFile(lockPath, []byte(workItemID), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write resolve lock: %w", err)
	}
	defer os.Remove(lockPath)

	// Acquire locks: work item first, then agent (consistent ordering with Cast).
	lock, err := AcquireWorkItemLock(workItemID)
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
		branchName = fmt.Sprintf("envoy/%s/%s", opts.World, opts.AgentName)
	case "governor":
		worktreeDir = governor.GovernorDir(opts.World)
		branchName = fmt.Sprintf("governor/%s", opts.World)
	case "forge":
		worktreeDir = filepath.Join(config.Home(), opts.World, "forge", "worktree")
		branchName = "forge/" + opts.World
	default:
		worktreeDir = WorktreePath(opts.World, opts.AgentName)
		branchName = fmt.Sprintf("outpost/%s/%s", opts.AgentName, workItemID)
	}

	// Get the work item for output and conflict-resolution detection.
	item, err := worldStore.GetWorkItem(workItemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work item %q: %w", workItemID, err)
	}

	// Detect conflict-resolution tasks and handle separately.
	if item.HasLabel("conflict-resolution") {
		return resolveConflictResolution(opts, item, branchName, worktreeDir,
			agentID, sessName, agent.Role, worldStore, sphereStore, mgr, logger)
	}

	// 2. Git operations in the worktree.
	// git add -A
	addCmd := exec.Command("git", "-C", worktreeDir, "add", "-A")
	if out, err := addCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git add failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// git commit (skip if nothing to commit)
	commitMsg := fmt.Sprintf("sol resolve: %s", item.Title)
	commitCmd := exec.Command("git", "-C", worktreeDir, "commit", "-m", commitMsg)
	commitCmd.CombinedOutput() // ignore error — nothing to commit is OK

	// git push origin HEAD
	pushCmd := exec.Command("git", "-C", worktreeDir, "push", "origin", "HEAD")
	pushFailed := false
	if out, err := pushCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: git push failed: %s\n", strings.TrimSpace(string(out)))
		pushFailed = true
	}

	// Track what has been done so we can undo on failure.
	var workItemUpdated bool

	rollback := func() {
		if workItemUpdated {
			if err := worldStore.UpdateWorkItem(workItemID, store.WorkItemUpdates{Status: "tethered"}); err != nil {
				fmt.Fprintf(os.Stderr, "resolve rollback: failed to reset work item %s: %v\n", workItemID, err)
			}
		}
	}

	// 3. Update work item: status -> done (idempotent — skip if already done).
	if item.Status != "done" {
		if err := worldStore.UpdateWorkItem(workItemID, store.WorkItemUpdates{Status: "done"}); err != nil {
			return nil, fmt.Errorf("failed to update work item status: %w", err)
		}
		workItemUpdated = true
	}

	// 4. Create merge request (idempotent — skip if one already exists for this work item).
	var mrID string
	existingMRs, err := worldStore.ListMergeRequestsByWorkItem(workItemID, "")
	if err != nil {
		rollback()
		return nil, fmt.Errorf("failed to check existing merge requests: %w", err)
	}
	if len(existingMRs) > 0 {
		mrID = existingMRs[0].ID
	} else {
		mrID, err = worldStore.CreateMergeRequest(workItemID, branchName, item.Priority)
		if err != nil {
			rollback()
			return nil, fmt.Errorf("failed to create merge request for %q: %w", workItemID, err)
		}

		// If push failed, immediately mark the MR as failed so forge doesn't try to merge it.
		if pushFailed {
			if err := worldStore.UpdateMergeRequestPhase(mrID, "failed"); err != nil {
				fmt.Fprintf(os.Stderr, "resolve: failed to mark MR as failed after push failure: %v\n", err)
			}
		}
	}

	// 5. Update agent state (idempotent — check current state first).
	// Outpost agents are ephemeral — delete the record to reclaim the name.
	// Persistent roles (envoy, governor) remain idle for reuse.
	if agent.Role == "agent" {
		// Re-read agent to check if already deleted (idempotent re-run).
		if _, getErr := sphereStore.GetAgent(agentID); getErr == nil {
			if err := sphereStore.DeleteAgent(agentID); err != nil {
				rollback()
				return nil, fmt.Errorf("failed to delete agent %q: %w", agentID, err)
			}
		}
	} else {
		if agent.State != "idle" {
			if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
				rollback()
				return nil, fmt.Errorf("failed to update agent state: %w", err)
			}
		}
	}

	// 6. Clear tether file.
	if err := tether.Clear(opts.World, opts.AgentName, agent.Role); err != nil {
		// Agent is idle but tether remains — consul will clean this up.
		fmt.Fprintf(os.Stderr, "resolve: failed to clear tether (consul will recover): %v\n", err)
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
			if agent.Role == "agent" {
				cleanupWorktree(opts.World, worktreeDir)
			}
		}()
	} else {
		sessionKept = true
	}

	if logger != nil {
		logger.Emit(events.EventResolve, "sol", opts.AgentName, "both", map[string]string{
			"work_item_id":  workItemID,
			"agent":         opts.AgentName,
			"branch":        branchName,
			"merge_request": mrID,
		})
	}

	// 8. Nudge governor that work is done (best-effort, silent skip if no governor).
	govSession := config.SessionName(opts.World, "governor")
	if mgr.Exists(govSession) {
		body := fmt.Sprintf(`{"work_item_id":%q,"agent_name":%q,"branch":%q,"title":%q,"merge_request_id":%q}`,
			workItemID, opts.AgentName, branchName, item.Title, mrID)
		if err := nudge.Enqueue(govSession, nudge.Message{
			Sender:   opts.AgentName,
			Type:     "AGENT_DONE",
			Subject:  fmt.Sprintf("Agent %s resolved %s", opts.AgentName, workItemID),
			Body:     body,
			Priority: "normal",
		}); err != nil {
			fmt.Fprintf(os.Stderr, "resolve: failed to nudge governor: %v\n", err)
		}
	}

	// 9. Nudge forge that a new MR is ready (best-effort).
	forgeSession := config.SessionName(opts.World, "forge")
	if mgr.Exists(forgeSession) {
		forgeBody := fmt.Sprintf(`{"work_item_id":%q,"merge_request_id":%q,"branch":%q,"title":%q}`,
			workItemID, mrID, branchName, item.Title)
		if err := nudge.Enqueue(forgeSession, nudge.Message{
			Sender:   opts.AgentName,
			Type:     "MR_READY",
			Subject:  fmt.Sprintf("MR %s ready for merge", mrID),
			Body:     forgeBody,
			Priority: "normal",
		}); err != nil {
			fmt.Fprintf(os.Stderr, "resolve: failed to nudge forge: %v\n", err)
		}
	}

	// 10. Poke forge to trigger turn boundary and drain pending nudges.
	nudge.Poke(forgeSession)


	return &ResolveResult{
		WorkItemID:     workItemID,
		Title:          item.Title,
		AgentName:      opts.AgentName,
		BranchName:     branchName,
		MergeRequestID: mrID,
		SessionKept:    sessionKept,
	}, nil
}

// resolveConflictResolution handles the resolve flow for conflict-resolution tasks.
// Differences from normal resolve:
// 1. Uses --force-with-lease for push (branch was rebased)
// 2. Does NOT create a new merge request (original MR already exists)
// 3. Unblocks the original MR
// 4. Closes the resolution work item
func resolveConflictResolution(opts ResolveOpts, item *store.WorkItem, branchName, worktreeDir,
	agentID, sessName, role string, worldStore WorldStore, sphereStore SphereStore, mgr SessionManager, logger *events.Logger) (*ResolveResult, error) {

	// 1. Git operations: add, commit, force-push (branch was rebased).
	addCmd := exec.Command("git", "-C", worktreeDir, "add", "-A")
	if out, err := addCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git add failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	commitMsg := fmt.Sprintf("sol resolve: %s", item.Title)
	commitCmd := exec.Command("git", "-C", worktreeDir, "commit", "-m", commitMsg)
	commitCmd.CombinedOutput() // ignore error — nothing to commit is OK

	// Force push with lease — branch was rebased, needs force push.
	pushCmd := exec.Command("git", "-C", worktreeDir, "push", "--force-with-lease", "origin", "HEAD")
	pushFailed := false
	if out, err := pushCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: git push --force-with-lease failed: %s\n",
			strings.TrimSpace(string(out)))
		pushFailed = true
	}

	// 2. Find and unblock the original MR (only if push succeeded).
	if !pushFailed {
		blockedMR, err := worldStore.FindMergeRequestByBlocker(item.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to find blocked MR for %q: %w", item.ID, err)
		}
		if blockedMR != nil {
			if err := worldStore.UnblockMergeRequest(blockedMR.ID); err != nil {
				return nil, fmt.Errorf("failed to unblock MR %q: %w", blockedMR.ID, err)
			}
		}
	}

	// 3. Close the resolution work item.
	if err := worldStore.CloseWorkItem(item.ID); err != nil {
		return nil, fmt.Errorf("failed to close resolution work item: %w", err)
	}

	// 4. Update agent state.
	// Outpost agents are ephemeral — delete the record to reclaim the name.
	if role == "agent" {
		if err := sphereStore.DeleteAgent(agentID); err != nil {
			return nil, fmt.Errorf("failed to delete agent %q: %w", agentID, err)
		}
	} else {
		if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
			return nil, fmt.Errorf("failed to update agent state: %w", err)
		}
	}

	// 5. Clear tether file.
	if err := tether.Clear(opts.World, opts.AgentName, role); err != nil {
		return nil, fmt.Errorf("failed to clear tether: %w", err)
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
			if role == "agent" {
				cleanupWorktree(opts.World, worktreeDir)
			}
		}()
	} else {
		sessionKept = true
	}

	if logger != nil {
		logger.Emit(events.EventResolve, "sol", opts.AgentName, "both", map[string]string{
			"work_item_id": item.ID,
			"agent":        opts.AgentName,
			"branch":       branchName,
		})
	}

	return &ResolveResult{
		WorkItemID:     item.ID,
		Title:          item.Title,
		AgentName:      opts.AgentName,
		BranchName:     branchName,
		MergeRequestID: "", // No new MR for conflict resolution.
		SessionKept:    sessionKept,
	}, nil
}

// DiscoverSourceRepo finds the git repo root from the current directory.
func DiscoverSourceRepo() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
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

// OpenWorldStore opens a world store for the given world name. Convenience wrapper.
func OpenWorldStore(world string) (*store.Store, error) {
	return store.OpenWorld(world)
}

// OpenSphereStore opens the sphere store. Convenience wrapper.
func OpenSphereStore() (*store.Store, error) {
	return store.OpenSphere()
}

// NewSessionManager creates a new session manager. Convenience wrapper.
func NewSessionManager() *session.Manager {
	return session.New()
}
