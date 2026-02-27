package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
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
	UpdateAgentState(id, state, hookItem string) error
	ListAgents(world string, state string) ([]store.Agent, error)
	CreateAgent(name, world, role string) (string, error)
	Close() error
}

// SessionName returns the tmux session name for an agent.
func SessionName(world, agentName string) string {
	return fmt.Sprintf("sol-%s-%s", world, agentName)
}

// WorktreePath returns the worktree directory for an agent.
func WorktreePath(world, agentName string) string {
	return filepath.Join(config.Home(), world, "outposts", agentName, "worktree")
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
	WorkItemID string
	World      string
	AgentName  string            // optional: if empty, find an idle agent
	SourceRepo string            // path to the source git repo
	Formula    string            // optional: formula name for workflow
	Variables  map[string]string // optional: workflow variables
}

// Cast assigns a work item to an outpost agent and starts its session.
// Supports re-cast (crash recovery): if the item is already tethered to the
// same agent, Cast recreates the worktree and session without error.
// The logger parameter is optional — if nil, no events are emitted.
func Cast(opts CastOpts, worldStore WorldStore, sphereStore SphereStore, mgr SessionManager, logger *events.Logger) (*CastResult, error) {
	// 0. Acquire per-work-item advisory lock to prevent double dispatch.
	lock, err := AcquireWorkItemLock(opts.WorkItemID)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	// 1. Get work item.
	item, err := worldStore.GetWorkItem(opts.WorkItemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work item %q: %w", opts.WorkItemID, err)
	}

	// 2. Find the agent.
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
			agent, err = autoProvision(opts.World, sphereStore)
			if err != nil {
				return nil, err
			}
		}
	}

	agentID := opts.World + "/" + agent.Name

	// 3. Determine if this is a re-cast (crash recovery).
	reCast := item.Status == "hooked" && item.Assignee == agentID &&
		agent.State == "working" && agent.HookItem == opts.WorkItemID

	// 4. Validate state.
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
			return nil, fmt.Errorf("failed to create worktree: %s: %w", strings.TrimSpace(string(out2)), err)
		}
		_ = out // suppress unused
	}

	// From here on, rollback on failure.
	rollback := func() {
		tether.Clear(opts.World, agent.Name)
		worldStore.UpdateWorkItem(opts.WorkItemID, store.WorkItemUpdates{Status: "open", Assignee: "-"})
		sphereStore.UpdateAgentState(agent.ID, "idle", "")
		rmCmd := exec.Command("git", "-C", opts.SourceRepo, "worktree", "remove", "--force", worktreeDir)
		rmCmd.Run()
	}

	// 4. Write tether file.
	if err := tether.Write(opts.World, agent.Name, opts.WorkItemID); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to write tether: %w", err)
	}

	// 5. Update work item: status → hooked, assignee → agent ID.
	if err := worldStore.UpdateWorkItem(opts.WorkItemID, store.WorkItemUpdates{
		Status:   "hooked",
		Assignee: agent.ID,
	}); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to update work item: %w", err)
	}

	// 6. Update agent: state → working, hook_item → work item ID.
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
		logger.Emit(events.EventSling, "sol", "operator", "both", castPayload)
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
func autoProvision(world string, sphereStore SphereStore) (*store.Agent, error) {
	overridePath := filepath.Join(config.Home(), world, "names.txt")
	pool, err := namepool.Load(overridePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load name pool: %w", err)
	}

	agents, err := sphereStore.ListAgents(world, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list agents for world %q: %w", world, err)
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
	// Refinery gets a special prime context.
	if agentName == "refinery" {
		return primeRefinery(world)
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
		handoff.Remove(world, agentName)
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

// primeRefinery returns refinery-specific context for the prime command.
func primeRefinery(world string) (*PrimeResult, error) {
	output := fmt.Sprintf(`=== REFINERY CONTEXT ===
World: %s
Role: refinery (merge queue processor)

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
	worktreeDir := WorktreePath(opts.World, opts.AgentName)

	// 1. Read tether — get work item ID.
	workItemID, err := tether.Read(opts.World, opts.AgentName)
	if err != nil {
		return nil, fmt.Errorf("failed to read tether: %w", err)
	}
	if workItemID == "" {
		return nil, fmt.Errorf("no work tethered for agent %q in world %q", opts.AgentName, opts.World)
	}

	branchName := fmt.Sprintf("outpost/%s/%s", opts.AgentName, workItemID)

	// Get the work item for output and conflict-resolution detection.
	item, err := worldStore.GetWorkItem(workItemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work item %q: %w", workItemID, err)
	}

	// Detect conflict-resolution tasks and handle separately.
	if item.HasLabel("conflict-resolution") {
		return resolveConflictResolution(opts, item, branchName, worktreeDir,
			agentID, sessName, worldStore, sphereStore, mgr, logger)
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

	// git push origin HEAD (warn but don't fail)
	pushCmd := exec.Command("git", "-C", worktreeDir, "push", "origin", "HEAD")
	if out, err := pushCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: git push failed: %s\n", strings.TrimSpace(string(out)))
	}

	// 3. Create merge request for the forge to process.
	mrID, err := worldStore.CreateMergeRequest(workItemID, branchName, item.Priority)
	if err != nil {
		return nil, fmt.Errorf("failed to create merge request for %q: %w", workItemID, err)
	}

	// 4. Update work item: status → done.
	if err := worldStore.UpdateWorkItem(workItemID, store.WorkItemUpdates{Status: "done"}); err != nil {
		return nil, fmt.Errorf("failed to update work item status: %w", err)
	}

	// 5. Update agent: state → idle, hook_item → clear.
	if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
		return nil, fmt.Errorf("failed to update agent state: %w", err)
	}

	// 6. Clear tether file.
	if err := tether.Clear(opts.World, opts.AgentName); err != nil {
		return nil, fmt.Errorf("failed to clear tether: %w", err)
	}

	// 6b. Clean up workflow if present.
	if _, err := workflow.ReadState(opts.World, opts.AgentName); err == nil {
		workflow.Remove(opts.World, opts.AgentName) // best-effort cleanup
	}

	// 7. Stop session — use a brief delay then stop in background.
	go func() {
		time.Sleep(1 * time.Second)
		mgr.Stop(sessName, true)
	}()

	if logger != nil {
		logger.Emit(events.EventDone, "sol", opts.AgentName, "both", map[string]string{
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
	}, nil
}

// resolveConflictResolution handles the resolve flow for conflict-resolution tasks.
// Differences from normal resolve:
// 1. Uses --force-with-lease for push (branch was rebased)
// 2. Does NOT create a new merge request (original MR already exists)
// 3. Unblocks the original MR
// 4. Closes the resolution work item
func resolveConflictResolution(opts ResolveOpts, item *store.WorkItem, branchName, worktreeDir,
	agentID, sessName string, worldStore WorldStore, sphereStore SphereStore, mgr SessionManager, logger *events.Logger) (*ResolveResult, error) {

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
	if out, err := pushCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: git push --force-with-lease failed: %s\n",
			strings.TrimSpace(string(out)))
	}

	// 2. Find and unblock the original MR.
	blockedMR, err := worldStore.FindMergeRequestByBlocker(item.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to find blocked MR for %q: %w", item.ID, err)
	}
	if blockedMR != nil {
		if err := worldStore.UnblockMergeRequest(blockedMR.ID); err != nil {
			return nil, fmt.Errorf("failed to unblock MR %q: %w", blockedMR.ID, err)
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

	// 6. Stop session.
	go func() {
		time.Sleep(1 * time.Second)
		mgr.Stop(sessName, true)
	}()

	if logger != nil {
		logger.Emit(events.EventDone, "sol", opts.AgentName, "both", map[string]string{
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
