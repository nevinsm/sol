package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/hook"
	"github.com/nevinsm/gt/internal/protocol"
	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/store"
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
	Close() error
}

// TownStore defines the town store operations used by dispatch.
type TownStore interface {
	GetAgent(id string) (*store.Agent, error)
	FindIdleAgent(rig string) (*store.Agent, error)
	UpdateAgentState(id, state, hookItem string) error
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
}

// SlingOpts holds the inputs for a sling operation.
type SlingOpts struct {
	WorkItemID string
	Rig        string
	AgentName  string // optional: if empty, find an idle agent
	SourceRepo string // path to the source git repo
}

// Sling assigns a work item to a polecat agent and starts its session.
// Supports re-sling (crash recovery): if the item is already hooked to the
// same agent, Sling recreates the worktree and session without error.
func Sling(opts SlingOpts, rigStore RigStore, townStore TownStore, mgr SessionManager) (*SlingResult, error) {
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
			return nil, fmt.Errorf("no idle agents available for rig %q", opts.Rig)
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

	return &SlingResult{
		WorkItemID:  opts.WorkItemID,
		AgentName:   agent.Name,
		SessionName: sessName,
		WorktreeDir: worktreeDir,
	}, nil
}

// PrimeResult holds the output of a prime operation.
type PrimeResult struct {
	Output string
}

// Prime assembles execution context from durable state and returns it.
func Prime(rig, agentName string, rigStore RigStore) (*PrimeResult, error) {
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

// DoneResult holds the output of a done operation.
type DoneResult struct {
	WorkItemID string
	Title      string
	AgentName  string
	BranchName string
}

// DoneOpts holds the inputs for a done operation.
type DoneOpts struct {
	Rig       string
	AgentName string
}

// Done signals work completion: git operations, state updates, hook clear.
func Done(opts DoneOpts, rigStore RigStore, townStore TownStore, mgr SessionManager) (*DoneResult, error) {
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

	// Get the work item title for output.
	item, err := rigStore.GetWorkItem(workItemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work item %q: %w", workItemID, err)
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

	// 3. Update work item: status → done.
	if err := rigStore.UpdateWorkItem(workItemID, store.WorkItemUpdates{Status: "done"}); err != nil {
		return nil, fmt.Errorf("failed to update work item status: %w", err)
	}

	// 4. Update agent: state → idle, hook_item → clear.
	if err := townStore.UpdateAgentState(agentID, "idle", ""); err != nil {
		return nil, fmt.Errorf("failed to update agent state: %w", err)
	}

	// 5. Clear hook file.
	if err := hook.Clear(opts.Rig, opts.AgentName); err != nil {
		return nil, fmt.Errorf("failed to clear hook: %w", err)
	}

	// 6. Stop session — use a brief delay then stop in background.
	go func() {
		time.Sleep(1 * time.Second)
		mgr.Stop(sessName, true)
	}()

	return &DoneResult{
		WorkItemID: workItemID,
		Title:      item.Title,
		AgentName:  opts.AgentName,
		BranchName: branchName,
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
