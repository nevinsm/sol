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
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/store"
)

// GateResult holds the outcome of running a single quality gate.
type GateResult struct {
	Gate    string        `json:"gate"`
	Passed  bool          `json:"passed"`
	Output  string        `json:"output"`
	Elapsed time.Duration `json:"elapsed"`
}

// ListReady returns MRs with phase=ready AND blocked_by IS NULL AND not
// blocked by caravan-level dependencies.
func (r *Forge) ListReady() ([]store.MergeRequest, error) {
	all, err := r.worldStore.ListMergeRequests("ready")
	if err != nil {
		return nil, err
	}
	var ready []store.MergeRequest
	for _, mr := range all {
		if mr.BlockedBy != "" {
			continue
		}
		// Check caravan-level dependencies.
		blocked, _, err := r.sphereStore.IsWorkItemBlockedByCaravanDeps(mr.WorkItemID)
		if err != nil {
			r.logger.Warn("failed to check caravan deps", "work_item", mr.WorkItemID, "error", err)
			// On error, include the MR (fail open to avoid blocking the pipeline).
			ready = append(ready, mr)
			continue
		}
		if blocked {
			continue
		}
		ready = append(ready, mr)
	}
	return ready, nil
}

// ListCaravanBlocked returns MRs that are blocked by caravan-level dependencies.
func (r *Forge) ListCaravanBlocked() ([]store.MergeRequest, error) {
	all, err := r.worldStore.ListMergeRequests("ready")
	if err != nil {
		return nil, err
	}
	var blocked []store.MergeRequest
	for _, mr := range all {
		if mr.BlockedBy != "" {
			continue // already blocked by work item, shown in ListBlocked
		}
		isBlocked, _, err := r.sphereStore.IsWorkItemBlockedByCaravanDeps(mr.WorkItemID)
		if err != nil {
			continue
		}
		if isBlocked {
			blocked = append(blocked, mr)
		}
	}
	return blocked, nil
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

// Release releases a claimed MR back to ready, or marks it failed if
// max attempts have been exceeded. Returns true if the MR was marked failed.
func (r *Forge) Release(mrID string) (failed bool, err error) {
	mr, err := r.worldStore.GetMergeRequest(mrID)
	if err != nil {
		return false, err
	}

	// If max attempts reached, mark failed instead of releasing.
	if r.cfg.MaxAttempts > 0 && mr.Attempts >= r.cfg.MaxAttempts {
		r.logger.Info("max attempts reached, marking failed",
			"mr", mrID, "attempts", mr.Attempts, "max", r.cfg.MaxAttempts)
		return true, r.MarkFailed(mrID)
	}

	if err := r.worldStore.UpdateMergeRequestPhase(mrID, "ready"); err != nil {
		return false, err
	}

	// Nudge governor about push rejection (best-effort).
	r.nudgeGovernor("PUSH_REJECTED",
		fmt.Sprintf("MR %s push rejected (attempt %d/%d)", mrID, mr.Attempts, r.cfg.MaxAttempts),
		fmt.Sprintf(`{"work_item_id":%q,"merge_request_id":%q,"branch":%q,"attempts":%d,"max_attempts":%d}`,
			mr.WorkItemID, mrID, mr.Branch, mr.Attempts, r.cfg.MaxAttempts))

	return false, nil
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

// MarkMerged sets MR phase to merged, closes work item, deletes remote branch,
// and supersedes any prior failed MRs for the same work item.
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

	// Clean up local branch (best-effort).
	exec.Command("git", "-C", r.worktree, "branch", "-D", mr.Branch).Run()

	// Supersede prior failed MRs for the same work item (best-effort).
	r.supersedeFailed(mrID, mr.WorkItemID)

	r.logger.Info("merged", "mr", mrID, "work_item", mr.WorkItemID, "branch", mr.Branch)

	// Nudge governor that MR was merged (best-effort).
	r.nudgeGovernor("MERGED",
		fmt.Sprintf("MR %s merged", mrID),
		fmt.Sprintf(`{"work_item_id":%q,"merge_request_id":%q,"branch":%q,"title":%q}`,
			mr.WorkItemID, mrID, mr.Branch, r.workItemTitle(mr.WorkItemID)))

	return nil
}

// supersedeFailed transitions failed MRs for the same work item to "superseded",
// deletes their remote branches, and resolves their escalations.
func (r *Forge) supersedeFailed(mergedMRID, workItemID string) {
	failed, err := r.worldStore.ListMergeRequestsByWorkItem(workItemID, "failed")
	if err != nil {
		r.logger.Error("failed to list failed MRs for superseding", "work_item", workItemID, "error", err)
		return
	}

	for _, mr := range failed {
		if mr.ID == mergedMRID {
			continue
		}

		if err := r.worldStore.UpdateMergeRequestPhase(mr.ID, "superseded"); err != nil {
			r.logger.Error("failed to supersede MR", "mr", mr.ID, "error", err)
			continue
		}

		// Delete remote branch (best-effort).
		exec.Command("git", "-C", r.worktree, "push", "origin", "--delete", mr.Branch).Run()

		// Delete local branch (best-effort).
		exec.Command("git", "-C", r.worktree, "branch", "-D", mr.Branch).Run()

		// Resolve escalations that reference this MR (best-effort).
		r.resolveEscalationsForMR(mr.ID)

		r.logger.Info("superseded", "mr", mr.ID, "superseded_by", mergedMRID)
	}
}

// resolveEscalationsForMR resolves open/acknowledged escalations whose
// description contains the given MR ID.
func (r *Forge) resolveEscalationsForMR(mrID string) {
	escalations, err := r.sphereStore.ListEscalations("")
	if err != nil {
		r.logger.Error("failed to list escalations for resolution", "mr", mrID, "error", err)
		return
	}

	for _, esc := range escalations {
		if esc.Status == "resolved" {
			continue
		}
		if !strings.Contains(esc.Description, mrID) {
			continue
		}
		if err := r.sphereStore.ResolveEscalation(esc.ID); err != nil {
			r.logger.Error("failed to resolve escalation", "escalation", esc.ID, "mr", mrID, "error", err)
		}
	}
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

	// Nudge governor that merge failed (best-effort).
	r.nudgeGovernor("MERGE_FAILED",
		fmt.Sprintf("MR %s merge failed", mrID),
		fmt.Sprintf(`{"work_item_id":%q,"merge_request_id":%q,"branch":%q,"reason":"merge failed, work item reopened"}`,
			mr.WorkItemID, mrID, mr.Branch))

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

	// Nudge governor that MR is blocked on conflict resolution (best-effort).
	r.nudgeGovernor("CONFLICT_BLOCKED",
		fmt.Sprintf("MR %s blocked on conflict resolution", mr.ID),
		fmt.Sprintf(`{"work_item_id":%q,"merge_request_id":%q,"branch":%q,"resolution_task_id":%q}`,
			mr.WorkItemID, mr.ID, mr.Branch, taskID))

	return taskID, nil
}

// CheckUnblocked finds blocked MRs whose resolution tasks are closed (merged),
// unblocks them, and returns the list of unblocked MR IDs.
// Note: "done" (code complete, awaiting merge) is NOT sufficient — the
// blocker's code must be merged to the target branch first.
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
		if item.Status == "closed" {
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

// nudgeGovernor enqueues a message to the governor's nudge queue.
// Best-effort: logs and swallows errors, skips silently if no governor is configured.
func (r *Forge) nudgeGovernor(msgType, subject, body string) {
	govDir := config.AgentDir(r.world, "governor", "governor")
	if _, err := os.Stat(govDir); err != nil {
		return // no governor configured — skip silently
	}
	govSession := config.SessionName(r.world, "governor")
	if err := nudge.Enqueue(govSession, nudge.Message{
		Sender:   "forge",
		Type:     msgType,
		Subject:  subject,
		Body:     body,
		Priority: "normal",
	}); err != nil {
		r.logger.Warn("failed to nudge governor", "type", msgType, "error", err)
	}
}

// workItemTitle fetches the title of a work item, returning "" on error.
func (r *Forge) workItemTitle(workItemID string) string {
	item, err := r.worldStore.GetWorkItem(workItemID)
	if err != nil {
		return ""
	}
	return item.Title
}
