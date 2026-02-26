package refinery

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/store"
)

// GateResult holds the outcome of running a single quality gate.
type GateResult struct {
	Gate    string        `json:"gate"`
	Passed  bool          `json:"passed"`
	Output  string        `json:"output"`
	Elapsed time.Duration `json:"elapsed"`
}

// ListReady returns MRs with phase=ready AND blocked_by IS NULL.
func (r *Refinery) ListReady() ([]store.MergeRequest, error) {
	all, err := r.rigStore.ListMergeRequests("ready")
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
func (r *Refinery) ListBlocked() ([]store.MergeRequest, error) {
	all, err := r.rigStore.ListMergeRequests("")
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
func (r *Refinery) Claim() (*store.MergeRequest, error) {
	return r.rigStore.ClaimMergeRequest(r.agentID)
}

// Release releases a claimed MR back to ready.
func (r *Refinery) Release(mrID string) error {
	return r.rigStore.UpdateMergeRequestPhase(mrID, "ready")
}

// RunGates runs quality gates in the worktree and returns results.
func (r *Refinery) RunGates() ([]GateResult, error) {
	var results []GateResult
	for _, gate := range r.cfg.QualityGates {
		start := time.Now()
		cmd := exec.Command("sh", "-c", gate)
		cmd.Dir = r.worktree
		cmd.Env = append(os.Environ(),
			"GT_HOME="+config.Home(),
			"GT_RIG="+r.rig,
		)
		output, err := cmd.CombinedOutput()
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
func (r *Refinery) Push() error {
	lock, err := dispatch.AcquireMergeSlotLock(r.rig)
	if err != nil {
		return fmt.Errorf("failed to acquire merge slot: %w", err)
	}
	defer lock.Release()

	pushCmd := exec.Command("git", "-C", r.worktree, "push", "origin",
		"HEAD:"+r.cfg.TargetBranch)
	if out, err := pushCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("push rejected: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// MarkMerged sets MR phase to merged, closes work item, deletes remote branch.
func (r *Refinery) MarkMerged(mrID string) error {
	mr, err := r.rigStore.GetMergeRequest(mrID)
	if err != nil {
		return err
	}

	if err := r.rigStore.UpdateMergeRequestPhase(mrID, "merged"); err != nil {
		return fmt.Errorf("failed to mark MR as merged: %w", err)
	}

	if err := r.rigStore.CloseWorkItem(mr.WorkItemID); err != nil {
		r.logger.Error("failed to close work item", "work_item", mr.WorkItemID, "error", err)
	}

	// Clean up remote branch (best-effort).
	exec.Command("git", "-C", r.worktree, "push", "origin", "--delete", mr.Branch).Run()

	r.logger.Info("merged", "mr", mrID, "work_item", mr.WorkItemID, "branch", mr.Branch)
	return nil
}

// MarkFailed sets MR phase to failed.
func (r *Refinery) MarkFailed(mrID string) error {
	return r.rigStore.UpdateMergeRequestPhase(mrID, "failed")
}

// GetMergeRequest returns a merge request by ID (convenience accessor).
func (r *Refinery) GetMergeRequest(mrID string) (*store.MergeRequest, error) {
	return r.rigStore.GetMergeRequest(mrID)
}

// CreateResolutionTask creates a conflict resolution work item, blocks the MR,
// and returns the new task ID.
func (r *Refinery) CreateResolutionTask(mr *store.MergeRequest) (string, error) {
	// Get original work item for title.
	item, err := r.rigStore.GetWorkItem(mr.WorkItemID)
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
3. Run 'gt done' when conflicts are resolved`,
		mr.Branch, r.cfg.TargetBranch, targetSHA, mr.ID,
		item.ID, item.Title,
		mr.Branch, r.cfg.TargetBranch)

	taskID, err := r.rigStore.CreateWorkItemWithOpts(store.CreateWorkItemOpts{
		Title:       fmt.Sprintf("Resolve merge conflicts: %s", item.Title),
		Description: description,
		CreatedBy:   r.rig + "/refinery",
		Priority:    priority,
		Labels:      []string{"conflict-resolution", "source-mr:" + mr.ID},
		ParentID:    item.ID,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create resolution task: %w", err)
	}

	if err := r.rigStore.BlockMergeRequest(mr.ID, taskID); err != nil {
		return "", fmt.Errorf("failed to block MR %q: %w", mr.ID, err)
	}

	r.logger.Info("created resolution task", "mr", mr.ID, "task", taskID,
		"branch", mr.Branch)

	return taskID, nil
}

// CheckUnblocked finds blocked MRs whose resolution tasks are done/closed,
// unblocks them, and returns the list of unblocked MR IDs.
func (r *Refinery) CheckUnblocked() ([]string, error) {
	blocked, err := r.ListBlocked()
	if err != nil {
		return nil, err
	}

	var unblocked []string
	for _, mr := range blocked {
		item, err := r.rigStore.GetWorkItem(mr.BlockedBy)
		if err != nil {
			r.logger.Warn("failed to get blocker work item", "blocker", mr.BlockedBy, "error", err)
			continue
		}
		if item.Status == "done" || item.Status == "closed" {
			if err := r.rigStore.UnblockMergeRequest(mr.ID); err != nil {
				r.logger.Error("failed to unblock MR", "mr", mr.ID, "error", err)
				continue
			}
			unblocked = append(unblocked, mr.ID)
			r.logger.Info("unblocked MR", "mr", mr.ID, "blocker", mr.BlockedBy)
		}
	}
	return unblocked, nil
}

// Rig returns the rig name (for CLI use).
func (r *Refinery) Rig() string { return r.rig }

// WorktreeDir returns the worktree path (for CLI use).
func (r *Refinery) WorktreeDir() string { return r.worktree }

// TargetBranch returns the configured target branch.
func (r *Refinery) TargetBranch() string { return r.cfg.TargetBranch }

// QualityGates returns the configured quality gate commands.
func (r *Refinery) QualityGates() []string { return r.cfg.QualityGates }

// GetWorkItem returns a work item by ID (convenience accessor).
func (r *Refinery) GetWorkItem(id string) (*store.WorkItem, error) {
	return r.rigStore.GetWorkItem(id)
}
