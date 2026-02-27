package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/events"
	"github.com/nevinsm/gt/internal/handoff"
	"github.com/nevinsm/gt/internal/hook"
	"github.com/nevinsm/gt/internal/namepool"
	"github.com/nevinsm/gt/internal/protocol"
	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/store"
	"github.com/nevinsm/gt/internal/workflow"
)

// SessionManager defines the session operations used by the dispatch package.
type SessionManager interface {
	Start(name, workdir, cmd string, env map[string]string, role, rig string) error
	Stop(name string, force bool) error
	Exists(name string) bool
}

// RigStore defines the rig store operations used by dispatch.
type RigStore interface {
	GetWorkItem(id string) (*store.WorkItem, error)
	UpdateWorkItem(id string, updates store.WorkItemUpdates) error
	CreateMergeRequest(workItemID, branch string, priority int) (string, error)
	CreateWorkItemWithOpts(opts store.CreateWorkItemOpts) (string, error)
	FindMergeRequestByBlocker(blockerID string) (*store.MergeRequest, error)
	UnblockMergeRequest(mrID string) error
	CloseWorkItem(id string) error
	Close() error
}

// TownStore defines the town store operations used by dispatch.
type TownStore interface {
	GetAgent(id string) (*store.Agent, error)
	FindIdleAgent(rig string) (*store.Agent, error)
	UpdateAgentState(id, state, hookItem string) error
	ListAgents(rig string, state string) ([]store.Agent, error)
	CreateAgent(name, rig, role string) (string, error)
	Close() error
}

// SessionName returns the tmux session name for an agent.
func SessionName(rig, agentName string) string {
	return fmt.Sprintf("gt-%s-%s", rig, agentName)
}

// WorktreePath returns the worktree directory for an agent.
func WorktreePath(rig, agentName string) string {
	return filepath.Join(config.Home(), rig, "polecats", agentName, "rig")
}

// SlingResult holds the output of a successful sling operation.
type SlingResult struct {
	WorkItemID  string
	AgentName   string
	SessionName string
	WorktreeDir string
	Formula     string // empty if no workflow
}

// SlingOpts holds the inputs for a sling operation.
type SlingOpts struct {
	WorkItemID string
	Rig        string
	AgentName  string            // optional: if empty, find an idle agent
	SourceRepo string            // path to the source git repo
	Formula    string            // optional: formula name for workflow
	Variables  map[string]string // optional: workflow variables
}

// Sling assigns a work item to a polecat agent and starts its session.
// Supports re-sling (crash recovery): if the item is already hooked to the
// same agent, Sling recreates the worktree and session without error.
// The logger parameter is optional — if nil, no events are emitted.
func Sling(opts SlingOpts, rigStore RigStore, townStore TownStore, mgr SessionManager, logger *events.Logger) (*SlingResult, error) {
	// 0. Acquire per-work-item advisory lock to prevent double dispatch.
	lock, err := AcquireWorkItemLock(opts.WorkItemID)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	// 1. Get work item.
	item, err := rigStore.GetWorkItem(opts.WorkItemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work item %q: %w", opts.WorkItemID, err)
	}

	// 2. Find the agent.
	var agent *store.Agent
	if opts.AgentName != "" {
		agentID := opts.Rig + "/" + opts.AgentName
		agent, err = townStore.GetAgent(agentID)
		if err != nil {
			return nil, fmt.Errorf("failed to get agent %q: %w", agentID, err)
		}
	} else {
		if item.Status != "open" {
			return nil, fmt.Errorf("work item %q has status %q, expected \"open\"", opts.WorkItemID, item.Status)
		}
		agent, err = townStore.FindIdleAgent(opts.Rig)
		if err != nil {
			return nil, fmt.Errorf("failed to find idle agent for rig %q: %w", opts.Rig, err)
		}
		if agent == nil {
			// Auto-provision a new agent from the name pool.
			agent, err = autoProvision(opts.Rig, townStore)
			if err != nil {
				return nil, err
			}
		}
	}

	agentID := opts.Rig + "/" + agent.Name

	// 3. Determine if this is a re-sling (crash recovery).
	reSling := item.Status == "hooked" && item.Assignee == agentID &&
		agent.State == "working" && agent.HookItem == opts.WorkItemID

	// 4. Validate state.
	if !reSling {
		if item.Status != "open" {
			return nil, fmt.Errorf("work item %q has status %q, expected \"open\"", opts.WorkItemID, item.Status)
		}
		if agent.State != "idle" {
			return nil, fmt.Errorf("agent %q has state %q, expected \"idle\"", agentID, agent.State)
		}
	}

	worktreeDir := WorktreePath(opts.Rig, agent.Name)
	sessName := SessionName(opts.Rig, agent.Name)
	branchName := fmt.Sprintf("polecat/%s/%s", agent.Name, opts.WorkItemID)

	// For re-sling, stop existing session if it's still around.
	if reSling && mgr.Exists(sessName) {
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

	// Try creating worktree with new branch; fall back to existing branch (re-sling).
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
		hook.Clear(opts.Rig, agent.Name)
		rigStore.UpdateWorkItem(opts.WorkItemID, store.WorkItemUpdates{Status: "open", Assignee: "-"})
		townStore.UpdateAgentState(agent.ID, "idle", "")
		rmCmd := exec.Command("git", "-C", opts.SourceRepo, "worktree", "remove", "--force", worktreeDir)
		rmCmd.Run()
	}

	// 4. Write hook file.
	if err := hook.Write(opts.Rig, agent.Name, opts.WorkItemID); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to write hook: %w", err)
	}

	// 5. Update work item: status → hooked, assignee → agent ID.
	if err := rigStore.UpdateWorkItem(opts.WorkItemID, store.WorkItemUpdates{
		Status:   "hooked",
		Assignee: agent.ID,
	}); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to update work item: %w", err)
	}

	// 6. Update agent: state → working, hook_item → work item ID.
	if err := townStore.UpdateAgentState(agent.ID, "working", opts.WorkItemID); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to update agent state: %w", err)
	}

	// 7. Install CLAUDE.md in the worktree.
	ctx := protocol.ClaudeMDContext{
		AgentName:   agent.Name,
		Rig:         opts.Rig,
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
	if err := protocol.InstallHooks(worktreeDir, opts.Rig, agent.Name); err != nil {
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
		if _, _, err := workflow.Instantiate(opts.Rig, agent.Name, opts.Formula, vars); err != nil {
			rollback()
			return nil, fmt.Errorf("failed to instantiate workflow %q: %w", opts.Formula, err)
		}
	}

	// 9. Start tmux session.
	env := map[string]string{
		"GT_HOME":  config.Home(),
		"GT_RIG":   opts.Rig,
		"GT_AGENT": agent.Name,
	}
	if err := mgr.Start(sessName, worktreeDir, "claude --dangerously-skip-permissions", env, "polecat", opts.Rig); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to start session: %w", err)
	}

	slingPayload := map[string]string{
		"work_item_id": opts.WorkItemID,
		"agent":        agent.Name,
		"rig":          opts.Rig,
	}
	if logger != nil {
		logger.Emit(events.EventSling, "gt", "operator", "both", slingPayload)
	}

	// Emit workflow instantiation event if formula was used.
	if opts.Formula != "" && logger != nil {
		logger.Emit(events.EventWorkflowInstantiate, "gt", "operator", "both", map[string]string{
			"formula":      opts.Formula,
			"work_item_id": opts.WorkItemID,
			"agent":        agent.Name,
			"rig":          opts.Rig,
		})
	}

	return &SlingResult{
		WorkItemID:  opts.WorkItemID,
		AgentName:   agent.Name,
		SessionName: sessName,
		WorktreeDir: worktreeDir,
		Formula:     opts.Formula,
	}, nil
}

// autoProvision creates a new agent from the name pool.
func autoProvision(rig string, townStore TownStore) (*store.Agent, error) {
	overridePath := filepath.Join(config.Home(), rig, "names.txt")
	pool, err := namepool.Load(overridePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load name pool: %w", err)
	}

	agents, err := townStore.ListAgents(rig, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list agents for rig %q: %w", rig, err)
	}

	usedNames := make([]string, len(agents))
	for i, a := range agents {
		usedNames[i] = a.Name
	}

	name, err := pool.AllocateName(usedNames)
	if err != nil {
		return nil, err
	}

	id, err := townStore.CreateAgent(name, rig, "polecat")
	if err != nil {
		return nil, fmt.Errorf("failed to create agent %q: %w", name, err)
	}

	return &store.Agent{
		ID:    id,
		Name:  name,
		Rig:   rig,
		Role:  "polecat",
		State: "idle",
	}, nil
}

// PrimeResult holds the output of a prime operation.
type PrimeResult struct {
	Output string
}

// Prime assembles execution context from durable state and returns it.
func Prime(rig, agentName string, rigStore RigStore) (*PrimeResult, error) {
	// Refinery gets a special prime context.
	if agentName == "refinery" {
		return primeRefinery(rig)
	}

	// Read the hook file.
	workItemID, err := hook.Read(rig, agentName)
	if err != nil {
		return nil, fmt.Errorf("failed to read hook: %w", err)
	}
	if workItemID == "" {
		return &PrimeResult{Output: "No work hooked"}, nil
	}

	// Get the work item.
	item, err := rigStore.GetWorkItem(workItemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work item %q: %w", workItemID, err)
	}

	// Check for handoff context (session continuity).
	handoffState, err := handoff.Read(rig, agentName)
	if err != nil {
		return nil, fmt.Errorf("failed to read handoff state: %w", err)
	}

	if handoffState != nil {
		result, err := primeWithHandoff(rig, agentName, item, handoffState)
		if err != nil {
			return nil, err
		}
		// Clean up handoff file after successful injection.
		handoff.Remove(rig, agentName)
		return result, nil
	}

	// Check for active workflow.
	state, err := workflow.ReadState(rig, agentName)
	if err != nil {
		return nil, fmt.Errorf("failed to read workflow state: %w", err)
	}

	if state != nil && state.Status == "running" {
		return primeWithWorkflow(rig, agentName, item, state)
	}

	// No workflow — standard prime (existing behavior).
	output := fmt.Sprintf(`=== WORK CONTEXT ===
Agent: %s (rig: %s)
Work Item: %s
Title: %s
Status: %s

Description:
%s

Instructions:
Execute this work item. When complete, run: gt done
If stuck, run: gt escalate "description"
=== END CONTEXT ===`, agentName, rig, item.ID, item.Title, item.Status, item.Description)

	return &PrimeResult{Output: output}, nil
}

// primeWithWorkflow returns workflow-aware context for the prime command.
func primeWithWorkflow(rig, agentName string, item *store.WorkItem,
	state *workflow.State) (*PrimeResult, error) {

	step, err := workflow.ReadCurrentStep(rig, agentName)
	if err != nil {
		return nil, fmt.Errorf("failed to read current step: %w", err)
	}
	if step == nil {
		// Workflow exists but no current step — treat as complete.
		return &PrimeResult{
			Output: fmt.Sprintf("Workflow complete for %s. Run: gt done", item.ID),
		}, nil
	}

	// Count progress.
	completed := len(state.Completed)
	instance, _ := workflow.ReadInstance(rig, agentName)
	formula := ""
	if instance != nil {
		formula = instance.Formula
	}

	output := fmt.Sprintf(`=== WORK CONTEXT ===
Agent: %s (rig: %s)
Work Item: %s
Title: %s

Workflow: %s (step %d/%d+%d: %s)

--- CURRENT STEP ---
%s
--- END STEP ---

Propulsion loop:
1. Execute the step above
2. When done: gt workflow advance --rig=%s --agent=%s
3. Check progress: gt workflow status --rig=%s --agent=%s
4. After final step: gt done
=== END CONTEXT ===`,
		agentName, rig, item.ID, item.Title,
		formula, completed+1, completed, 1, step.Title,
		step.Instructions,
		rig, agentName, rig, agentName)

	return &PrimeResult{Output: output}, nil
}

// primeWithHandoff returns handoff-aware context for the prime command.
func primeWithHandoff(rig, agentName string, item *store.WorkItem,
	state *handoff.State) (*PrimeResult, error) {

	output := fmt.Sprintf(`=== HANDOFF CONTEXT ===
Agent: %s (rig: %s)
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
`, agentName, rig, item.ID, item.Title, state.Summary, strings.Join(state.RecentCommits, "\n"))

	// Add workflow context if the agent has an active workflow.
	if state.WorkflowStep != "" {
		output += fmt.Sprintf(`
Workflow progress: %s (current step: %s)
Read your current step: gt workflow current --rig=%s --agent=%s

`, state.WorkflowProgress, state.WorkflowStep, rig, agentName)
	}

	output += fmt.Sprintf(`Continue from where the previous session left off.
When complete, run: gt done
If you need to hand off again: gt handoff --summary="<what you've done>"
=== END HANDOFF ===`)

	return &PrimeResult{Output: output}, nil
}

// primeRefinery returns refinery-specific context for the prime command.
func primeRefinery(rig string) (*PrimeResult, error) {
	output := fmt.Sprintf(`=== REFINERY CONTEXT ===
Rig: %s
Role: refinery (merge queue processor)

Begin your patrol loop. Run 'gt refinery check-unblocked %s' first,
then scan the queue with 'gt refinery ready %s --json'.
=== END CONTEXT ===`, rig, rig, rig)

	return &PrimeResult{Output: output}, nil
}

// DoneResult holds the output of a done operation.
type DoneResult struct {
	WorkItemID     string
	Title          string
	AgentName      string
	BranchName     string
	MergeRequestID string
}

// DoneOpts holds the inputs for a done operation.
type DoneOpts struct {
	Rig       string
	AgentName string
}

// Done signals work completion: git operations, state updates, hook clear.
// The logger parameter is optional — if nil, no events are emitted.
func Done(opts DoneOpts, rigStore RigStore, townStore TownStore, mgr SessionManager, logger *events.Logger) (*DoneResult, error) {
	agentID := opts.Rig + "/" + opts.AgentName
	sessName := SessionName(opts.Rig, opts.AgentName)
	worktreeDir := WorktreePath(opts.Rig, opts.AgentName)

	// 1. Read hook — get work item ID.
	workItemID, err := hook.Read(opts.Rig, opts.AgentName)
	if err != nil {
		return nil, fmt.Errorf("failed to read hook: %w", err)
	}
	if workItemID == "" {
		return nil, fmt.Errorf("no work hooked for agent %q in rig %q", opts.AgentName, opts.Rig)
	}

	branchName := fmt.Sprintf("polecat/%s/%s", opts.AgentName, workItemID)

	// Get the work item for output and conflict-resolution detection.
	item, err := rigStore.GetWorkItem(workItemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work item %q: %w", workItemID, err)
	}

	// Detect conflict-resolution tasks and handle separately.
	if item.HasLabel("conflict-resolution") {
		return doneConflictResolution(opts, item, branchName, worktreeDir,
			agentID, sessName, rigStore, townStore, mgr, logger)
	}

	// 2. Git operations in the worktree.
	// git add -A
	addCmd := exec.Command("git", "-C", worktreeDir, "add", "-A")
	if out, err := addCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git add failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// git commit (skip if nothing to commit)
	commitMsg := fmt.Sprintf("gt done: %s", item.Title)
	commitCmd := exec.Command("git", "-C", worktreeDir, "commit", "-m", commitMsg)
	commitCmd.CombinedOutput() // ignore error — nothing to commit is OK

	// git push origin HEAD (warn but don't fail)
	pushCmd := exec.Command("git", "-C", worktreeDir, "push", "origin", "HEAD")
	if out, err := pushCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: git push failed: %s\n", strings.TrimSpace(string(out)))
	}

	// 3. Create merge request for the refinery to process.
	mrID, err := rigStore.CreateMergeRequest(workItemID, branchName, item.Priority)
	if err != nil {
		return nil, fmt.Errorf("failed to create merge request for %q: %w", workItemID, err)
	}

	// 4. Update work item: status → done.
	if err := rigStore.UpdateWorkItem(workItemID, store.WorkItemUpdates{Status: "done"}); err != nil {
		return nil, fmt.Errorf("failed to update work item status: %w", err)
	}

	// 5. Update agent: state → idle, hook_item → clear.
	if err := townStore.UpdateAgentState(agentID, "idle", ""); err != nil {
		return nil, fmt.Errorf("failed to update agent state: %w", err)
	}

	// 6. Clear hook file.
	if err := hook.Clear(opts.Rig, opts.AgentName); err != nil {
		return nil, fmt.Errorf("failed to clear hook: %w", err)
	}

	// 6b. Clean up workflow if present.
	if _, err := workflow.ReadState(opts.Rig, opts.AgentName); err == nil {
		workflow.Remove(opts.Rig, opts.AgentName) // best-effort cleanup
	}

	// 7. Stop session — use a brief delay then stop in background.
	go func() {
		time.Sleep(1 * time.Second)
		mgr.Stop(sessName, true)
	}()

	if logger != nil {
		logger.Emit(events.EventDone, "gt", opts.AgentName, "both", map[string]string{
			"work_item_id":  workItemID,
			"agent":         opts.AgentName,
			"branch":        branchName,
			"merge_request": mrID,
		})
	}

	return &DoneResult{
		WorkItemID:     workItemID,
		Title:          item.Title,
		AgentName:      opts.AgentName,
		BranchName:     branchName,
		MergeRequestID: mrID,
	}, nil
}

// doneConflictResolution handles the done flow for conflict-resolution tasks.
// Differences from normal done:
// 1. Uses --force-with-lease for push (branch was rebased)
// 2. Does NOT create a new merge request (original MR already exists)
// 3. Unblocks the original MR
// 4. Closes the resolution work item
func doneConflictResolution(opts DoneOpts, item *store.WorkItem, branchName, worktreeDir,
	agentID, sessName string, rigStore RigStore, townStore TownStore, mgr SessionManager, logger *events.Logger) (*DoneResult, error) {

	// 1. Git operations: add, commit, force-push (branch was rebased).
	addCmd := exec.Command("git", "-C", worktreeDir, "add", "-A")
	if out, err := addCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git add failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	commitMsg := fmt.Sprintf("gt done: %s", item.Title)
	commitCmd := exec.Command("git", "-C", worktreeDir, "commit", "-m", commitMsg)
	commitCmd.CombinedOutput() // ignore error — nothing to commit is OK

	// Force push with lease — branch was rebased, needs force push.
	pushCmd := exec.Command("git", "-C", worktreeDir, "push", "--force-with-lease", "origin", "HEAD")
	if out, err := pushCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: git push --force-with-lease failed: %s\n",
			strings.TrimSpace(string(out)))
	}

	// 2. Find and unblock the original MR.
	blockedMR, err := rigStore.FindMergeRequestByBlocker(item.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to find blocked MR for %q: %w", item.ID, err)
	}
	if blockedMR != nil {
		if err := rigStore.UnblockMergeRequest(blockedMR.ID); err != nil {
			return nil, fmt.Errorf("failed to unblock MR %q: %w", blockedMR.ID, err)
		}
	}

	// 3. Close the resolution work item.
	if err := rigStore.CloseWorkItem(item.ID); err != nil {
		return nil, fmt.Errorf("failed to close resolution work item: %w", err)
	}

	// 4. Update agent: state → idle, clear hook.
	if err := townStore.UpdateAgentState(agentID, "idle", ""); err != nil {
		return nil, fmt.Errorf("failed to update agent state: %w", err)
	}

	// 5. Clear hook file.
	if err := hook.Clear(opts.Rig, opts.AgentName); err != nil {
		return nil, fmt.Errorf("failed to clear hook: %w", err)
	}

	// 6. Stop session.
	go func() {
		time.Sleep(1 * time.Second)
		mgr.Stop(sessName, true)
	}()

	if logger != nil {
		logger.Emit(events.EventDone, "gt", opts.AgentName, "both", map[string]string{
			"work_item_id": item.ID,
			"agent":        opts.AgentName,
			"branch":       branchName,
		})
	}

	return &DoneResult{
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

// OpenRigStore opens a rig store for the given rig name. Convenience wrapper.
func OpenRigStore(rig string) (*store.Store, error) {
	return store.OpenRig(rig)
}

// OpenTownStore opens the town store. Convenience wrapper.
func OpenTownStore() (*store.Store, error) {
	return store.OpenTown()
}

// NewSessionManager creates a new session manager. Convenience wrapper.
func NewSessionManager() *session.Manager {
	return session.New()
}
