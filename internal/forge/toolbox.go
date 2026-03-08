package forge

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/store"
)

// pauseFlagPath returns the path to the forge pause flag file for a world.
func pauseFlagPath(world string) string {
	return filepath.Join(config.RuntimeDir(), world+"-forge-paused")
}

// IsForgePaused returns true if the forge is paused for the given world.
func IsForgePaused(world string) bool {
	_, err := os.Stat(pauseFlagPath(world))
	return err == nil
}

// SetForgePaused creates the pause flag file, pausing the forge.
func SetForgePaused(world string) error {
	if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
		return fmt.Errorf("failed to create runtime dir: %w", err)
	}
	return os.WriteFile(pauseFlagPath(world), []byte("paused\n"), 0o644)
}

// ClearForgePaused removes the pause flag file, resuming the forge.
func ClearForgePaused(world string) error {
	err := os.Remove(pauseFlagPath(world))
	if os.IsNotExist(err) {
		return nil // already unpaused
	}
	return err
}

// ListReady returns MRs with phase=ready AND blocked_by IS NULL AND not
// blocked by caravan-level dependencies. Caravan-blocked MRs are marked
// with the "caravan-blocked" sentinel so ClaimMergeRequest's SQL naturally
// excludes them.
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
		blocked, _, err := r.sphereStore.IsWritBlockedByCaravanDeps(mr.WritID)
		if err != nil {
			r.logger.Warn("failed to check caravan deps", "writ", mr.WritID, "error", err)
			// On error, include the MR (fail open to avoid blocking the pipeline).
			ready = append(ready, mr)
			continue
		}
		if blocked {
			// System-enforce: set blocked_by sentinel so claim SQL excludes it.
			if err := r.worldStore.BlockMergeRequest(mr.ID, store.CaravanBlockedSentinel); err != nil {
				r.logger.Error("failed to set caravan-blocked sentinel", "mr", mr.ID, "error", err)
			}
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
			continue // already blocked by writ, shown in ListBlocked
		}
		isBlocked, _, err := r.sphereStore.IsWritBlockedByCaravanDeps(mr.WritID)
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
		fmt.Sprintf(`{"writ_id":%q,"merge_request_id":%q,"branch":%q,"attempts":%d,"max_attempts":%d}`,
			mr.WritID, mrID, mr.Branch, mr.Attempts, r.cfg.MaxAttempts))

	return false, nil
}

// MarkMerged sets MR phase to merged, closes writ, deletes remote branch,
// and supersedes any prior failed MRs for the same writ.
func (r *Forge) MarkMerged(mrID string) error {
	mr, err := r.worldStore.GetMergeRequest(mrID)
	if err != nil {
		return err
	}

	// CRASH SAFETY: Close writ FIRST — this is the critical state transition.
	// If we crash after closing the writ but before updating the MR phase,
	// forge patrol will detect the closed writ and can retry the MR phase
	// update. The reverse (MR "merged" but writ still "working") would leave
	// the tether orphaned and the agent permanently assigned.
	if err := r.worldStore.CloseWrit(mr.WritID); err != nil {
		return fmt.Errorf("failed to close writ %s: %w", mr.WritID, err)
	}

	if err := r.worldStore.UpdateMergeRequestPhase(mrID, "merged"); err != nil {
		r.logger.Error("failed to mark MR as merged after closing writ",
			"mr", mrID, "writ", mr.WritID, "error", err)
		// Writ is closed (correct) but MR phase is stale. Forge patrol will
		// detect this inconsistency and retry. Create an escalation for visibility.
		desc := fmt.Sprintf("MR %s not marked merged after writ %s closed: %v", mrID, mr.WritID, err)
		if _, escErr := r.sphereStore.CreateEscalation("low", r.agentID, desc, "mr:"+mrID); escErr != nil {
			r.logger.Error("failed to create escalation for unmarked MR",
				"writ", mr.WritID, "mr", mrID, "error", escErr)
		}
	}

	// Clean up remote branch (best-effort).
	exec.Command("git", "-C", r.worktree, "push", "origin", "--delete", mr.Branch).Run()

	// Clean up local branch (best-effort).
	exec.Command("git", "-C", r.worktree, "branch", "-D", mr.Branch).Run()

	// Supersede prior failed MRs for the same writ (best-effort).
	r.supersedeFailed(mrID, mr.WritID)

	r.logger.Info("merged", "mr", mrID, "writ", mr.WritID, "branch", mr.Branch)

	// Nudge governor that MR was merged (best-effort).
	r.nudgeGovernor("MERGED",
		fmt.Sprintf("MR %s merged", mrID),
		fmt.Sprintf(`{"writ_id":%q,"merge_request_id":%q,"branch":%q,"title":%q}`,
			mr.WritID, mrID, mr.Branch, r.writTitle(mr.WritID)))

	return nil
}

// supersedeFailed transitions failed MRs for the same writ to "superseded",
// deletes their remote branches, and resolves their escalations.
func (r *Forge) supersedeFailed(mergedMRID, writID string) {
	failed, err := r.worldStore.ListMergeRequestsByWrit(writID, "failed")
	if err != nil {
		r.logger.Error("failed to list failed MRs for superseding", "writ", writID, "error", err)
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
// source_ref matches "mr:<mrID>".
func (r *Forge) resolveEscalationsForMR(mrID string) {
	escalations, err := r.sphereStore.ListEscalationsBySourceRef("mr:" + mrID)
	if err != nil {
		r.logger.Error("failed to list escalations for resolution", "mr", mrID, "error", err)
		return
	}

	for _, esc := range escalations {
		if err := r.sphereStore.ResolveEscalation(esc.ID); err != nil {
			r.logger.Error("failed to resolve escalation", "escalation", esc.ID, "mr", mrID, "error", err)
		}
	}
}

// MarkFailed sets MR phase to failed, reopens the writ for re-dispatch,
// and creates an escalation so the governor knows work needs attention.
func (r *Forge) MarkFailed(mrID string) error {
	mr, err := r.worldStore.GetMergeRequest(mrID)
	if err != nil {
		return err
	}

	if err := r.worldStore.UpdateMergeRequestPhase(mrID, "failed"); err != nil {
		return fmt.Errorf("failed to mark MR as failed: %w", err)
	}

	// Reopen writ so it can be re-dispatched (best-effort).
	reopenErr := r.worldStore.UpdateWrit(mr.WritID, store.WritUpdates{
		Status:   "open",
		Assignee: "-",
	})
	if reopenErr != nil {
		r.logger.Error("failed to reopen writ after merge failure",
			"writ", mr.WritID, "error", reopenErr)
	}

	// Create escalation so the governor knows about the failure (best-effort).
	desc := fmt.Sprintf("Merge failed for MR %s (branch %s, writ %s).", mrID, mr.Branch, mr.WritID)
	if reopenErr != nil {
		desc += fmt.Sprintf(" Writ reopen also failed: %v", reopenErr)
	} else {
		desc += " Writ reopened for re-dispatch."
	}
	if _, err := r.sphereStore.CreateEscalation("high", r.agentID, desc, "mr:"+mrID); err != nil {
		r.logger.Error("failed to create escalation for merge failure",
			"mr", mrID, "error", err)
	}

	r.logger.Info("marked failed and reopened", "mr", mrID,
		"writ", mr.WritID, "branch", mr.Branch)

	// Nudge governor that merge failed (best-effort).
	r.nudgeGovernor("MERGE_FAILED",
		fmt.Sprintf("MR %s merge failed", mrID),
		fmt.Sprintf(`{"writ_id":%q,"merge_request_id":%q,"branch":%q,"reason":"merge failed, writ reopened"}`,
			mr.WritID, mrID, mr.Branch))

	return nil
}

// GetMergeRequest returns a merge request by ID (convenience accessor).
func (r *Forge) GetMergeRequest(mrID string) (*store.MergeRequest, error) {
	return r.worldStore.GetMergeRequest(mrID)
}

// CreateResolutionTask creates a conflict resolution writ, blocks the MR,
// and returns the new task ID.
func (r *Forge) CreateResolutionTask(mr *store.MergeRequest) (string, error) {
	// Get original writ for title.
	item, err := r.worldStore.GetWrit(mr.WritID)
	if err != nil {
		return "", fmt.Errorf("failed to get writ %q: %w", mr.WritID, err)
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
Original writ: %s — %s

WARNING: Do NOT just verify existing code and resolve. You MUST rebase and force-push so the branch merges cleanly.

Instructions (follow every step in order):
1. git fetch origin
2. git rebase origin/%s  (resolve any conflicts during the rebase)
3. make build && make test  (ensure the rebased code compiles and passes)
4. Verify merge-base matches target HEAD:
   git merge-base origin/%s HEAD
   The output MUST equal the current origin/%s SHA. If it does not, the rebase did not work — try again.
5. git push --force-with-lease origin %s
6. ONLY AFTER the force-push succeeds, run 'sol resolve'`,
		mr.Branch, r.cfg.TargetBranch, targetSHA, mr.ID,
		item.ID, item.Title,
		r.cfg.TargetBranch,
		r.cfg.TargetBranch, r.cfg.TargetBranch,
		mr.Branch)

	taskID, err := r.worldStore.CreateWritWithOpts(store.CreateWritOpts{
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
		fmt.Sprintf(`{"writ_id":%q,"merge_request_id":%q,"branch":%q,"resolution_task_id":%q}`,
			mr.WritID, mr.ID, mr.Branch, taskID))

	return taskID, nil
}

// CheckUnblocked finds blocked MRs whose resolution tasks are closed (merged)
// or whose caravan dependencies are satisfied, unblocks them, and returns
// the list of unblocked MR IDs.
// Note: "done" (code complete, awaiting merge) is NOT sufficient — the
// blocker's code must be merged to the target branch first.
func (r *Forge) CheckUnblocked() ([]string, error) {
	blocked, err := r.ListBlocked()
	if err != nil {
		return nil, err
	}

	var unblocked []string
	for _, mr := range blocked {
		// Caravan-blocked MRs: re-check caravan deps.
		if mr.BlockedBy == store.CaravanBlockedSentinel {
			stillBlocked, _, err := r.sphereStore.IsWritBlockedByCaravanDeps(mr.WritID)
			if err != nil {
				r.logger.Warn("failed to re-check caravan deps", "mr", mr.ID, "writ", mr.WritID, "error", err)
				continue
			}
			if !stillBlocked {
				if err := r.worldStore.UnblockMergeRequest(mr.ID); err != nil {
					r.logger.Error("failed to unblock caravan-blocked MR", "mr", mr.ID, "error", err)
					continue
				}
				unblocked = append(unblocked, mr.ID)
				r.logger.Info("unblocked caravan-blocked MR", "mr", mr.ID, "writ", mr.WritID)
			}
			continue
		}

		// Writ-blocked MRs: check if blocker writ is closed.
		item, err := r.worldStore.GetWrit(mr.BlockedBy)
		if err != nil {
			r.logger.Warn("failed to get blocker writ", "blocker", mr.BlockedBy, "error", err)
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

// GetWrit returns a writ by ID (convenience accessor).
func (r *Forge) GetWrit(id string) (*store.Writ, error) {
	return r.worldStore.GetWrit(id)
}

// nudgeGovernor enqueues a message to the governor's nudge queue.
// Best-effort: logs and swallows errors, skips silently if no governor is configured.
func (r *Forge) nudgeGovernor(msgType, subject, body string) {
	govDir := config.AgentDir(r.world, "governor", "governor")
	if _, err := os.Stat(govDir); err != nil {
		return // no governor configured — skip silently
	}
	govSession := config.SessionName(r.world, "governor")
	if err := nudge.Deliver(govSession, nudge.Message{
		Sender:   "forge",
		Type:     msgType,
		Subject:  subject,
		Body:     body,
		Priority: "normal",
	}); err != nil {
		r.logger.Warn("failed to nudge governor", "type", msgType, "error", err)
	}
}

// writTitle fetches the title of a writ, returning "" on error.
func (r *Forge) writTitle(writID string) string {
	item, err := r.worldStore.GetWrit(writID)
	if err != nil {
		return ""
	}
	return item.Title
}
