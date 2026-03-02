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
}

// WorldStore defines the world store operations used by dispatch.
type WorldStore interface {
	GetWorkItem(id string) (*store.WorkItem, error)
	UpdateWorkItem(id string, updates store.WorkItemUpdates) error
	CreateMergeRequest(workItemID, branch string, priority int) (string, error)
	UpdateMergeRequestPhase(id, phase string) error
	CreateWorkItemWithOpts(opts store.CreateWorkItemOpts) (string, error)
	FindMergeRequestByBlocker(blockerID string) (*store.MergeRequest, error)
	UnblockMergeRequest(mrID string) error
	CloseWorkItem(id string) error
	Close() error
}

// SphereStore defines the sphere store operations used by dispatch.
type SphereStore interface {
	GetAgent(id string) (*store.Agent, error)
	FindIdleAgent(world string) (*store.Agent, error)
	UpdateAgentState(id, state, tetherItem string) error
	ListAgents(world string, state string) ([]store.Agent, error)
	CreateAgent(name, world, role string) (string, error)
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

	// For re-cast, stop existing session if it's still around.
	if reCast && mgr.Exists(sessName) {
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
		if err := tether.Clear(opts.World, agent.Name); err != nil {
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
		workflow.Remove(opts.World, agent.Name) // best-effort
	}

	// 4. Write tether file.
	if err := tether.Write(opts.World, agent.Name, opts.WorkItemID); err != nil {
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

	// 7. Install CLAUDE.md in the worktree.
	ctx := protocol.ClaudeMDContext{
		AgentName:   agent.Name,
		World:       opts.World,
		WorkItemID:  opts.WorkItemID,
		Title:       item.Title,
		Description: item.Description,
		HasWorkflow: opts.Formula != "",
		ModelTier:   worldCfg.Agents.ModelTier,
	}
	if err := protocol.InstallClaudeMD(worktreeDir, ctx); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to install CLAUDE.md: %w", err)
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
		if _, _, err := workflow.Instantiate(opts.World, agent.Name, opts.Formula, vars); err != nil {
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
	if err := mgr.Start(sessName, worktreeDir, "claude --dangerously-skip-permissions", env, "agent", opts.World); err != nil {
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
func Prime(world, agentName string, worldStore WorldStore) (*PrimeResult, error) {
	// Forge gets a special prime context.
	if agentName == "forge" {
		return primeForge(world)
	}

	// Read the tether file.
	workItemID, err := tether.Read(world, agentName)
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
	handoffState, err := handoff.Read(world, agentName)
	if err != nil {
		return nil, fmt.Errorf("failed to read handoff state: %w", err)
	}

	if handoffState != nil {
		result, err := primeWithHandoff(world, agentName, item, handoffState)
		if err != nil {
			return nil, err
		}
		// Clean up handoff file after successful injection.
		if removeErr := handoff.Remove(world, agentName); removeErr != nil {
			fmt.Fprintf(os.Stderr, "prime: failed to remove handoff file: %v\n", removeErr)
		}
		return result, nil
	}

	// Check for active workflow.
	state, err := workflow.ReadState(world, agentName)
	if err != nil {
		return nil, fmt.Errorf("failed to read workflow state: %w", err)
	}

	if state != nil && state.Status == "running" {
		return primeWithWorkflow(world, agentName, item, state)
	}

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

	return &PrimeResult{Output: output}, nil
}

// primeWithWorkflow returns workflow-aware context for the prime command.
func primeWithWorkflow(world, agentName string, item *store.WorkItem,
	state *workflow.State) (*PrimeResult, error) {

	step, err := workflow.ReadCurrentStep(world, agentName)
	if err != nil {
		return nil, fmt.Errorf("failed to read current step: %w", err)
	}
	if step == nil {
		// Workflow exists but no current step — treat as complete.
		return &PrimeResult{
			Output: fmt.Sprintf("Workflow complete for %s. Run: sol resolve", item.ID),
		}, nil
	}

	// Count progress.
	completed := len(state.Completed)
	instance, _ := workflow.ReadInstance(world, agentName)
	formula := ""
	if instance != nil {
		formula = instance.Formula
	}

	output := fmt.Sprintf(`=== WORK CONTEXT ===
Agent: %s (world: %s)
Work Item: %s
Title: %s

Workflow: %s (step %d/%d+%d: %s)

--- CURRENT STEP ---
%s
--- END STEP ---

Propulsion loop:
1. Execute the step above
2. When done: sol workflow advance --world=%s --agent=%s
3. Check progress: sol workflow status --world=%s --agent=%s
4. After final step: sol resolve
=== END CONTEXT ===`,
		agentName, world, item.ID, item.Title,
		formula, completed+1, completed, 1, step.Title,
		step.Instructions,
		world, agentName, world, agentName)

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

	// Add workflow context if the agent has an active workflow.
	if state.WorkflowStep != "" {
		output += fmt.Sprintf(`
Workflow progress: %s (current step: %s)
Read your current step: sol workflow current --world=%s --agent=%s

`, state.WorkflowProgress, state.WorkflowStep, world, agentName)
	}

	output += fmt.Sprintf(`Continue from where the previous session left off.
When complete, run: sol resolve
If you need to hand off again: sol handoff --summary="<what you've done>"
=== END HANDOFF ===`)

	return &PrimeResult{Output: output}, nil
}

// primeForge returns forge-specific context for the prime command.
func primeForge(world string) (*PrimeResult, error) {
	output := fmt.Sprintf(`=== FORGE CONTEXT ===
World: %s
Role: forge (merge queue processor)

Begin your patrol loop. Run 'sol forge check-unblocked %s' first,
then scan the queue with 'sol forge ready %s --json'.
=== END CONTEXT ===`, world, world, world)

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

// Resolve signals work completion: git operations, state updates, tether clear.
// The logger parameter is optional — if nil, no events are emitted.
func Resolve(opts ResolveOpts, worldStore WorldStore, sphereStore SphereStore, mgr SessionManager, logger *events.Logger) (*ResolveResult, error) {
	agentID := opts.World + "/" + opts.AgentName
	sessName := SessionName(opts.World, opts.AgentName)

	// 1. Read tether — get work item ID.
	workItemID, err := tether.Read(opts.World, opts.AgentName)
	if err != nil {
		return nil, fmt.Errorf("failed to read tether: %w", err)
	}
	if workItemID == "" {
		return nil, fmt.Errorf("no work tethered for agent %q in world %q", opts.AgentName, opts.World)
	}

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

	// Look up agent to determine role (needed for envoy resolve behavior).
	agent, err := sphereStore.GetAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", agentID, err)
	}

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

	// 3. Update work item: status -> done.
	if err := worldStore.UpdateWorkItem(workItemID, store.WorkItemUpdates{Status: "done"}); err != nil {
		return nil, fmt.Errorf("failed to update work item status: %w", err)
	}
	workItemUpdated = true

	// 4. Create merge request — always, even if push failed (so it's tracked).
	mrID, err := worldStore.CreateMergeRequest(workItemID, branchName, item.Priority)
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

	// 5. Update agent: state -> idle, tether_item -> clear.
	if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to update agent state: %w", err)
	}

	// 6. Clear tether file.
	if err := tether.Clear(opts.World, opts.AgentName); err != nil {
		// Agent is idle but tether remains — consul will clean this up.
		fmt.Fprintf(os.Stderr, "resolve: failed to clear tether (consul will recover): %v\n", err)
	}

	// 6b. Clean up workflow if present (envoys and governors don't use workflow system).
	if agent.Role != "envoy" && agent.Role != "governor" {
		if _, err := workflow.ReadState(opts.World, opts.AgentName); err == nil {
			if removeErr := workflow.Remove(opts.World, opts.AgentName); removeErr != nil {
				fmt.Fprintf(os.Stderr, "resolve: failed to clean up workflow: %v\n", removeErr)
			}
		}
	}

	// 7. Stop session after a brief delay to allow final output.
	// Envoys and governors keep their session alive — they are human-supervised and persistent.
	sessionKept := false
	if agent.Role != "envoy" && agent.Role != "governor" {
		done := make(chan struct{})
		go func() {
			defer close(done)
			time.Sleep(1 * time.Second)
			if err := mgr.Stop(sessName, true); err != nil {
				fmt.Fprintf(os.Stderr, "resolve: failed to stop session %s: %v\n", sessName, err)
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

	// 4. Update agent: state → idle, clear tether.
	if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
		return nil, fmt.Errorf("failed to update agent state: %w", err)
	}

	// 5. Clear tether file.
	if err := tether.Clear(opts.World, opts.AgentName); err != nil {
		return nil, fmt.Errorf("failed to clear tether: %w", err)
	}

	// 6. Stop session after a brief delay to allow final output.
	// Envoys and governors keep their session alive — they are human-supervised and persistent.
	sessionKept := false
	if role != "envoy" && role != "governor" {
		done := make(chan struct{})
		go func() {
			defer close(done)
			time.Sleep(1 * time.Second)
			if err := mgr.Stop(sessName, true); err != nil {
				fmt.Fprintf(os.Stderr, "resolve: failed to stop session %s: %v\n", sessName, err)
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

// ResolveSourceRepo returns the source repo from config, falling back to
// CWD-based git discovery if the config value is empty.
func ResolveSourceRepo(cfg config.WorldConfig) (string, error) {
	if cfg.World.SourceRepo != "" {
		return cfg.World.SourceRepo, nil
	}
	repo, err := DiscoverSourceRepo()
	if err != nil {
		return "", fmt.Errorf("no source_repo in world.toml and not in a git repo: %w", err)
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
