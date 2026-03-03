package forge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/store"
)

// GateResult holds the outcome of running a single quality gate.
type GateResult struct {
	Gate    string        `json:"gate"`
	Passed  bool          `json:"passed"`
	Output  string        `json:"output"`
	Elapsed time.Duration `json:"elapsed"`
}

// ListReady returns MRs with phase=ready AND blocked_by IS NULL.
func (r *Forge) ListReady() ([]store.MergeRequest, error) {
	all, err := r.worldStore.ListMergeRequests("ready")
	if err != nil {
		return nil, err
	}
	var ready []store.MergeRequest
	for _, mr := range all {
		if mr.BlockedBy == "" {
			ready = append(ready, mr)
		}
	}
	return ready, nil
}

// ListBlocked returns MRs with blocked_by IS NOT NULL.
func (r *Forge) ListBlocked() ([]store.MergeRequest, error) {
	all, err := r.worldStore.ListMergeRequests("")
	if err != nil {
		return nil, err
	}
	var blocked []store.MergeRequest
	for _, mr := range all {
		if mr.BlockedBy != "" {
			blocked = append(blocked, mr)
		}
	}
	return blocked, nil
}

// Claim atomically claims the next ready unblocked MR.
func (r *Forge) Claim() (*store.MergeRequest, error) {
	return r.worldStore.ClaimMergeRequest(r.agentID)
}

// Release releases a claimed MR back to ready.
func (r *Forge) Release(mrID string) error {
	return r.worldStore.UpdateMergeRequestPhase(mrID, "ready")
}

// RunGates runs quality gates in the worktree and returns results.
func (r *Forge) RunGates(ctx context.Context) ([]GateResult, error) {
	timeout := r.cfg.GateTimeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	var results []GateResult
	for _, gate := range r.cfg.QualityGates {
		start := time.Now()
		gateCtx, cancel := context.WithTimeout(ctx, timeout)
		cmd := exec.CommandContext(gateCtx, "sh", "-c", gate)
		cmd.WaitDelay = time.Second
		cmd.Dir = r.worktree
		cmd.Env = append(os.Environ(),
			"SOL_HOME="+config.Home(),
			"SOL_WORLD="+r.world,
		)
		output, err := cmd.CombinedOutput()
		cancel()
		elapsed := time.Since(start)

		result := GateResult{
			Gate:    gate,
			Passed:  err == nil,
			Output:  truncate(string(output), 2000),
			Elapsed: elapsed,
		}
		results = append(results, result)

		if err != nil {
			return results, nil // Return results so far; caller sees which failed.
		}
	}
	return results, nil
}

// Push acquires the merge slot, pushes HEAD to target branch, releases slot.
func (r *Forge) Push() error {
	lock, err := dispatch.AcquireMergeSlotLock(r.world)
	if err != nil {
		return fmt.Errorf("failed to acquire merge slot: %w", err)
	}
	defer lock.Release()

	pushCmd := exec.Command("git", "-C", r.worktree, "push", "origin",
		"HEAD:"+r.cfg.TargetBranch)
	if out, err := pushCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("push rejected: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// MarkMerged sets MR phase to merged, closes work item, deletes remote branch.
func (r *Forge) MarkMerged(mrID string) error {
	mr, err := r.worldStore.GetMergeRequest(mrID)
	if err != nil {
		return err
	}

	if err := r.worldStore.UpdateMergeRequestPhase(mrID, "merged"); err != nil {
		return fmt.Errorf("failed to mark MR as merged: %w", err)
	}

	if err := r.worldStore.CloseWorkItem(mr.WorkItemID); err != nil {
		r.logger.Error("failed to close work item", "work_item", mr.WorkItemID, "error", err)
	}

	// Clean up remote branch (best-effort).
	exec.Command("git", "-C", r.worktree, "push", "origin", "--delete", mr.Branch).Run()

	r.logger.Info("merged", "mr", mrID, "work_item", mr.WorkItemID, "branch", mr.Branch)
	return nil
}

// MarkFailed sets MR phase to failed, reopens the work item for re-dispatch,
// and creates an escalation so the governor knows work needs attention.
func (r *Forge) MarkFailed(mrID string) error {
	mr, err := r.worldStore.GetMergeRequest(mrID)
	if err != nil {
		return err
	}

	if err := r.worldStore.UpdateMergeRequestPhase(mrID, "failed"); err != nil {
		return fmt.Errorf("failed to mark MR as failed: %w", err)
	}

	// Reopen work item so it can be re-dispatched (best-effort).
	if err := r.worldStore.UpdateWorkItem(mr.WorkItemID, store.WorkItemUpdates{
		Status:   "open",
		Assignee: "-",
	}); err != nil {
		r.logger.Error("failed to reopen work item after merge failure",
			"work_item", mr.WorkItemID, "error", err)
	}

	// Create escalation so the governor knows about the failure (best-effort).
	desc := fmt.Sprintf("Merge failed for MR %s (branch %s, work item %s). Work item reopened for re-dispatch.",
		mrID, mr.Branch, mr.WorkItemID)
	if _, err := r.sphereStore.CreateEscalation("high", r.agentID, desc); err != nil {
		r.logger.Error("failed to create escalation for merge failure",
			"mr", mrID, "error", err)
	}

	r.logger.Info("marked failed and reopened", "mr", mrID,
		"work_item", mr.WorkItemID, "branch", mr.Branch)
	return nil
}

// GetMergeRequest returns a merge request by ID (convenience accessor).
func (r *Forge) GetMergeRequest(mrID string) (*store.MergeRequest, error) {
	return r.worldStore.GetMergeRequest(mrID)
}

// CreateResolutionTask creates a conflict resolution work item, blocks the MR,
// and returns the new task ID.
func (r *Forge) CreateResolutionTask(mr *store.MergeRequest) (string, error) {
	// Get original work item for title.
	item, err := r.worldStore.GetWorkItem(mr.WorkItemID)
	if err != nil {
		return "", fmt.Errorf("failed to get work item %q: %w", mr.WorkItemID, err)
	}

	// Get current target branch SHA.
	out, err := exec.Command("git", "-C", r.worktree, "rev-parse",
		"origin/"+r.cfg.TargetBranch).CombinedOutput()
	targetSHA := strings.TrimSpace(string(out))
	if err != nil {
		targetSHA = "(unknown)"
	}

	// Boost priority: P2→P1, P1→P0, P0→P0.
	priority := item.Priority - 1
	if priority < 0 {
		priority = 0
	}

	description := fmt.Sprintf(`Resolve merge conflicts for branch %s.

Target branch: %s (SHA: %s)
Original MR: %s
Original work item: %s — %s

Instructions:
1. Rebase branch %s onto origin/%s
2. Resolve all merge conflicts
3. Run 'sol resolve' when conflicts are resolved`,
		mr.Branch, r.cfg.TargetBranch, targetSHA, mr.ID,
		item.ID, item.Title,
		mr.Branch, r.cfg.TargetBranch)

	taskID, err := r.worldStore.CreateWorkItemWithOpts(store.CreateWorkItemOpts{
		Title:       fmt.Sprintf("Resolve merge conflicts: %s", item.Title),
		Description: description,
		CreatedBy:   r.world + "/forge",
		Priority:    priority,
		Labels:      []string{"conflict-resolution", "source-mr:" + mr.ID},
		ParentID:    item.ID,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create resolution task: %w", err)
	}

	if err := r.worldStore.BlockMergeRequest(mr.ID, taskID); err != nil {
		return "", fmt.Errorf("failed to block MR %q: %w", mr.ID, err)
	}

	r.logger.Info("created resolution task", "mr", mr.ID, "task", taskID,
		"branch", mr.Branch)

	return taskID, nil
}

// CheckUnblocked finds blocked MRs whose resolution tasks are done/closed,
// unblocks them, and returns the list of unblocked MR IDs.
func (r *Forge) CheckUnblocked() ([]string, error) {
	blocked, err := r.ListBlocked()
	if err != nil {
		return nil, err
	}

	var unblocked []string
	for _, mr := range blocked {
		item, err := r.worldStore.GetWorkItem(mr.BlockedBy)
		if err != nil {
			r.logger.Warn("failed to get blocker work item", "blocker", mr.BlockedBy, "error", err)
			continue
		}
		if item.Status == "done" || item.Status == "closed" {
			if err := r.worldStore.UnblockMergeRequest(mr.ID); err != nil {
				r.logger.Error("failed to unblock MR", "mr", mr.ID, "error", err)
				continue
			}
			unblocked = append(unblocked, mr.ID)
			r.logger.Info("unblocked MR", "mr", mr.ID, "blocker", mr.BlockedBy)
		}
	}
	return unblocked, nil
}

// World returns the world name (for CLI use).
func (r *Forge) World() string { return r.world }

// WorktreeDir returns the worktree path (for CLI use).
func (r *Forge) WorktreeDir() string { return r.worktree }

// TargetBranch returns the configured target branch.
func (r *Forge) TargetBranch() string { return r.cfg.TargetBranch }

// QualityGates returns the configured quality gate commands.
func (r *Forge) QualityGates() []string { return r.cfg.QualityGates }

// GetWorkItem returns a work item by ID (convenience accessor).
func (r *Forge) GetWorkItem(id string) (*store.WorkItem, error) {
	return r.worldStore.GetWorkItem(id)
}
