package forge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// gitCommandTimeout is the timeout for git operations that may involve network access.
const gitCommandTimeout = 60 * time.Second

// errBranchMissing is returned by the containment helpers when the remote
// branch ref does not exist (already deleted, never pushed). This is a
// non-fatal condition for callers — there is nothing to delete.
var errBranchMissing = errors.New("remote branch does not exist")

// isWritLandedOnTarget verifies that the configured target branch contains a
// commit whose message references writID. The forge merge instructions
// (injection.go) tag every squash commit with "<title> (<writID>)", so a
// writ-id grep against the target branch is the authoritative signal that the
// writ's work has landed and the source branch is safe to delete. This is the
// same signal verifyPush already trusts and is resilient to squash merges and
// post-conflict rebases.
//
// Returns:
//   - (true, nil) if a commit on target references writID — safe to delete
//   - (false, nil) if no commit on target references writID — refuse to delete
//   - (false, errBranchMissing) if the source branch ref does not exist
//   - (false, err) for any other failure (fetch error, missing target ref, git log failure)
//
// The function performs a `git fetch origin` first to ensure the grep runs
// against current refs. Operations run against sourceRepo (the managed clone)
// rather than the forge worktree — sourceRepo has the full history and remote
// config and is never touched by merge sessions, so it remains a stable,
// authoritative view of origin even if the forge worktree is structurally broken.
func (r *Forge) isWritLandedOnTarget(branch, writID string) (bool, error) {
	if r.cfg.TargetBranch == "" {
		return false, fmt.Errorf("forge target branch is not configured")
	}
	if writID == "" {
		return false, fmt.Errorf("writ ID is required for containment check")
	}
	runner := r.cmd
	if runner == nil {
		runner = &realCmdRunner{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel()

	// Refresh remote refs so the grep reflects current origin state.
	if out, err := runner.Run(ctx, r.sourceRepo, "git", "fetch", "origin"); err != nil {
		return false, fmt.Errorf("git fetch origin failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	branchRef := "refs/remotes/origin/" + branch
	targetRef := "refs/remotes/origin/" + r.cfg.TargetBranch

	// Verify the remote branch ref still exists. If not, there is nothing
	// to delete and the caller should treat this as a no-op.
	if _, err := runner.Run(ctx, r.sourceRepo, "git", "rev-parse", "--verify", "--quiet", branchRef); err != nil {
		return false, errBranchMissing
	}

	// Verify the target ref exists — without it the grep is meaningless.
	if _, err := runner.Run(ctx, r.sourceRepo, "git", "rev-parse", "--verify", "--quiet", targetRef); err != nil {
		return false, fmt.Errorf("target ref %s not found in source repo", targetRef)
	}

	// Grep target's commit messages for writID. Writ IDs are "sol-" + 16 hex
	// chars — no regex metacharacters — so a plain pattern is safe. We match
	// the bare writID (not "(<writID>)") to align with verifyPush's signal
	// and to remain resilient to commit-message reformatting.
	out, err := runner.Run(ctx, r.sourceRepo, "git", "log", targetRef,
		"--grep="+writID, "-n", "1", "--format=%H")
	if err != nil {
		return false, fmt.Errorf("git log --grep failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// isBranchAncestorOfTarget reports whether refs/remotes/origin/<branch> is
// fully reachable from refs/remotes/origin/<targetBranch>. This is the
// pre-merge sanity check used by the orchestrator's no-op-claim path: a
// legitimate no-op merge means the agent never produced a new commit, so the
// outpost branch tip should still be an ancestor of the current target. The
// post-merge cleanup path uses isWritLandedOnTarget instead because squash
// merges break ancestry but preserve the writ-id tag in the merge commit.
//
// Returns:
//   - (true, nil) if branch is an ancestor of target
//   - (false, nil) if branch is NOT an ancestor
//   - (false, errBranchMissing) if the remote branch does not exist
//   - (false, err) for any other failure
func (r *Forge) isBranchAncestorOfTarget(branch string) (bool, error) {
	if r.cfg.TargetBranch == "" {
		return false, fmt.Errorf("forge target branch is not configured")
	}
	runner := r.cmd
	if runner == nil {
		runner = &realCmdRunner{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel()

	if out, err := runner.Run(ctx, r.sourceRepo, "git", "fetch", "origin"); err != nil {
		return false, fmt.Errorf("git fetch origin failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	branchRef := "refs/remotes/origin/" + branch
	targetRef := "refs/remotes/origin/" + r.cfg.TargetBranch

	if _, err := runner.Run(ctx, r.sourceRepo, "git", "rev-parse", "--verify", "--quiet", branchRef); err != nil {
		return false, errBranchMissing
	}
	if _, err := runner.Run(ctx, r.sourceRepo, "git", "rev-parse", "--verify", "--quiet", targetRef); err != nil {
		return false, fmt.Errorf("target ref %s not found in source repo", targetRef)
	}

	_, err := runner.Run(ctx, r.sourceRepo, "git", "merge-base", "--is-ancestor", branchRef, targetRef)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor failed: %w", err)
}

// deleteBranchIfContained deletes the remote and local copies of branch only
// after verifying that a commit on the target branch references writID. The
// forge tags every squash merge commit with the writ ID (injection.go), so
// the writ-id grep is the authoritative signal that a writ's work has landed.
// If the signal is missing, the delete is skipped and a high-severity
// escalation is created so an operator can investigate. This is the only safe
// path for the deterministic Go branch-cleanup code: blindly running
// `git push origin --delete` after a session reports merged would destroy
// the only copy of unmerged work in the false-claim scenario.
//
// sourceRef is the escalation source_ref tag, e.g. "mr:<mrID>".
func (r *Forge) deleteBranchIfContained(mrID, branch, writID, sourceRef string) {
	landed, err := r.isWritLandedOnTarget(branch, writID)
	switch {
	case errors.Is(err, errBranchMissing):
		// Remote branch already gone — nothing to delete. Still try the
		// local branch cleanup since a stale local ref may linger.
		r.logger.Info("remote branch already absent, skipping remote delete",
			"mr", mrID, "branch", branch)
	case err != nil:
		r.logger.Error("writ-id grep errored, refusing to delete branch",
			"mr", mrID, "branch", branch, "writ", writID, "target", r.cfg.TargetBranch, "error", err)
		if r.sphereStore != nil {
			desc := fmt.Sprintf("Refusing to delete branch %s for MR %s: writ-id grep on target %s failed: %v. "+
				"Manual investigation required — the branch may contain unmerged work.",
				branch, mrID, r.cfg.TargetBranch, err)
			if _, escErr := r.sphereStore.CreateEscalation("high", r.agentID, desc, sourceRef); escErr != nil {
				r.logger.Error("failed to create writ-id grep escalation",
					"mr", mrID, "branch", branch, "error", escErr)
			}
		}
		return
	case !landed:
		r.logger.Error("no commit on target references writ, refusing to delete branch",
			"mr", mrID, "branch", branch, "writ", writID, "target", r.cfg.TargetBranch)
		if r.sphereStore != nil {
			desc := fmt.Sprintf("Refusing to delete branch %s for MR %s: no commit on target %s references writ %s. "+
				"The merge session reported success but the writ ID is not present in the target's commit history — possible false claim or partial merge. "+
				"Manual investigation required.",
				branch, mrID, r.cfg.TargetBranch, writID)
			if _, escErr := r.sphereStore.CreateEscalation("high", r.agentID, desc, sourceRef); escErr != nil {
				r.logger.Error("failed to create writ-id grep escalation",
					"mr", mrID, "branch", branch, "error", escErr)
			}
		}
		return
	}

	// Writ landed on target (or remote branch already gone): proceed with delete.
	if !errors.Is(err, errBranchMissing) {
		pushCtx, pushCancel := context.WithTimeout(context.Background(), gitCommandTimeout)
		defer pushCancel()
		if perr := exec.CommandContext(pushCtx, "git", "-C", r.worktree, "push", "origin", "--delete", branch).Run(); perr != nil {
			r.logger.Warn("failed to delete remote branch", "mr", mrID, "branch", branch, "error", perr)
		}
	}

	// Clean up local branch (best-effort).
	branchCtx, branchCancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer branchCancel()
	if berr := exec.CommandContext(branchCtx, "git", "-C", r.worktree, "branch", "-D", branch).Run(); berr != nil {
		r.logger.Warn("failed to delete local branch", "mr", mrID, "branch", branch, "error", berr)
	}
}

// bestEffortDeleteBranch deletes the remote and local copies of branch
// without any containment check. It is used by the no-op MarkMerged path,
// where the agent has reported that the writ's work is already on target
// (so no new commits exist on the source branch under this writ ID, making
// writ-id grep the wrong signal) and there is no unique work to lose.
// Failures are logged at Warn level and never block the merge.
func (r *Forge) bestEffortDeleteBranch(mrID, branch string) {
	pushCtx, pushCancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer pushCancel()
	if perr := exec.CommandContext(pushCtx, "git", "-C", r.worktree, "push", "origin", "--delete", branch).Run(); perr != nil {
		r.logger.Warn("failed to delete remote branch (no-op cleanup)", "mr", mrID, "branch", branch, "error", perr)
	}

	branchCtx, branchCancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer branchCancel()
	if berr := exec.CommandContext(branchCtx, "git", "-C", r.worktree, "branch", "-D", branch).Run(); berr != nil {
		r.logger.Warn("failed to delete local branch (no-op cleanup)", "mr", mrID, "branch", branch, "error", berr)
	}
}

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
	if err != nil {
		return fmt.Errorf("failed to clear forge pause flag: %w", err)
	}
	return nil
}

// ListReady returns MRs with phase=ready that are not blocked.
// This is a pure read operation — it does not modify any state.
func (r *Forge) ListReady() ([]store.MergeRequest, error) {
	all, err := r.worldStore.ListMergeRequests("ready")
	if err != nil {
		return nil, fmt.Errorf("failed to list ready merge requests: %w", err)
	}
	var ready []store.MergeRequest
	for _, mr := range all {
		if mr.BlockedBy != "" {
			continue
		}
		ready = append(ready, mr)
	}
	return ready, nil
}

// EnforceCaravanBlocks checks ready MRs for caravan-level dependencies and
// sets the caravan-blocked sentinel on any that are blocked. Returns the
// number of MRs blocked.
func (r *Forge) EnforceCaravanBlocks() (int, error) {
	all, err := r.worldStore.ListMergeRequests("ready")
	if err != nil {
		return 0, fmt.Errorf("failed to list ready merge requests: %w", err)
	}
	blocked := 0
	for _, mr := range all {
		if mr.BlockedBy != "" {
			continue // already blocked
		}
		isBlocked, _, err := r.sphereStore.IsWritBlockedByCaravanDeps(mr.WritID)
		if err != nil {
			r.logger.Warn("failed to check caravan deps", "writ", mr.WritID, "error", err)
			continue
		}
		if isBlocked {
			if err := r.worldStore.BlockMergeRequest(mr.ID, store.CaravanBlockedSentinel); err != nil {
				r.logger.Error("failed to set caravan-blocked sentinel", "mr", mr.ID, "error", err)
			} else {
				blocked++
			}
		}
	}
	return blocked, nil
}

// ListBlocked returns MRs with blocked_by IS NOT NULL.
func (r *Forge) ListBlocked() ([]store.MergeRequest, error) {
	all, err := r.worldStore.ListMergeRequests("")
	if err != nil {
		return nil, fmt.Errorf("failed to list merge requests: %w", err)
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
	return r.worldStore.ClaimMergeRequest(r.agentID, r.cfg.MaxAttempts)
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

	return false, nil
}

// MarkMerged sets MR phase to merged, closes writ, deletes remote branch,
// and supersedes any prior failed MRs for the same writ. Branch deletion is
// gated by a writ-id grep against the target branch.
func (r *Forge) MarkMerged(mrID string) error {
	return r.markMergedImpl(mrID, false)
}

// MarkMergedNoOp is the no-op variant of MarkMerged. The agent has reported
// that the writ's work is already on target (typically because another writ
// landed it), so a writ-id grep against target will not match — the writ ID
// is not on target under its own commit. Branch deletion uses an unconditional
// best-effort path because the source branch contains no unique unmerged
// work in the legitimate no-op case. The orchestrator validates the no-op
// claim with isBranchAncestorOfTarget before reaching this method.
func (r *Forge) MarkMergedNoOp(mrID string) error {
	return r.markMergedImpl(mrID, true)
}

func (r *Forge) markMergedImpl(mrID string, noOp bool) error {
	mr, err := r.worldStore.GetMergeRequest(mrID)
	if err != nil {
		return err
	}

	// CRASH SAFETY: Close writ FIRST — this is the critical state transition.
	// If we crash after closing the writ but before updating the MR phase,
	// forge patrol will detect the closed writ and can retry the MR phase
	// update. The reverse (MR "merged" but writ still "working") would leave
	// the tether orphaned and the agent permanently assigned.
	superseded, err := r.worldStore.CloseWrit(mr.WritID)
	if err != nil {
		return fmt.Errorf("failed to close writ %s: %w", mr.WritID, err)
	}
	if len(superseded) > 0 {
		r.logger.Info("superseded failed MRs on writ close", "writ", mr.WritID, "count", len(superseded))
		// For each superseded MR: resolve its escalations and clean up its
		// remote+local branches (best-effort). CloseWrit transitioned these
		// MRs from "failed" to "superseded" inside its transaction, so they
		// will not be re-cleaned by any later pass.
		for _, sid := range superseded {
			r.resolveEscalationsForMR(sid)
			r.cleanupSupersededBranch(sid, mrID)
		}
	}

	if err := r.worldStore.UpdateMergeRequestPhase(mrID, "merged"); err != nil {
		r.logger.Error("failed to mark MR as merged after closing writ",
			"mr", mrID, "writ", mr.WritID, "error", err)
		// Writ is closed (correct) but MR phase is stale. The next forge patrol
		// cycle will detect the claimed/closed inconsistency via
		// RecoverOrphanedMerged and update the MR phase to "merged" directly.
		// Create a low-severity escalation for operator visibility.
		desc := fmt.Sprintf("MR %s not marked merged after writ %s closed: %v. "+
			"The next forge patrol cycle will recover this automatically.", mrID, mr.WritID, err)
		if _, escErr := r.sphereStore.CreateEscalation("low", r.agentID, desc, "mr:"+mrID); escErr != nil {
			r.logger.Error("failed to create escalation for unmarked MR",
				"writ", mr.WritID, "mr", mrID, "error", escErr)
		}
	}

	// Clean up remote and local branches. For non-no-op merges, gate the
	// delete behind a writ-id grep against the target branch — the forge
	// tags every squash commit with the writ ID, so a missing tag means the
	// session reported merged but the work is not actually on target. For
	// no-op merges (the agent verified the work is already there under
	// another writ), no commit on target references this writ ID, so we
	// fall back to a best-effort delete.
	if noOp {
		r.bestEffortDeleteBranch(mrID, mr.Branch)
	} else {
		r.deleteBranchIfContained(mrID, mr.Branch, mr.WritID, "mr:"+mrID)
	}

	// Auto-resolve writ-linked escalations (best-effort).
	r.resolveEscalationsForWrit(mr.WritID)

	r.logger.Info("merged", "mr", mrID, "writ", mr.WritID, "branch", mr.Branch)

	return nil
}

// cleanupSupersededBranch deletes the remote and local branches for an MR
// that was just superseded by CloseWrit. Best-effort: failures are logged at
// Warn level and never block the merge.
func (r *Forge) cleanupSupersededBranch(supersededMRID, mergedByMRID string) {
	mr, err := r.worldStore.GetMergeRequest(supersededMRID)
	if err != nil {
		r.logger.Warn("failed to look up superseded MR for branch cleanup",
			"mr", supersededMRID, "superseded_by", mergedByMRID, "error", err)
		return
	}

	// Delete remote and local branches via the gated helper, which verifies
	// containment before destroying any refs. A superseded MR is one whose
	// writ has just been closed by a sibling MR's merge — but the superseded
	// branch itself may or may not be in the target. The same rule applies:
	// never delete a branch unless it is provably contained in target.
	r.deleteBranchIfContained(mr.ID, mr.Branch, mr.WritID, "mr:"+mr.ID)

	r.logger.Info("superseded", "mr", mr.ID, "branch", mr.Branch, "superseded_by", mergedByMRID)
}

// resolveEscalationsForMR resolves open/acknowledged escalations whose
// source_ref matches "mr:<mrID>".
func (r *Forge) resolveEscalationsForMR(mrID string) {
	if r.sphereStore == nil {
		return
	}
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

// resolveEscalationsForWrit resolves open/acknowledged escalations whose
// source_ref matches "writ:<writID>".
func (r *Forge) resolveEscalationsForWrit(writID string) {
	if r.sphereStore == nil {
		return
	}
	escalations, err := r.sphereStore.ListEscalationsBySourceRef("writ:" + writID)
	if err != nil {
		r.logger.Error("failed to list escalations for writ resolution", "writ", writID, "error", err)
		return
	}

	for _, esc := range escalations {
		if err := r.sphereStore.ResolveEscalation(esc.ID); err != nil {
			r.logger.Error("failed to resolve escalation", "escalation", esc.ID, "writ", writID, "error", err)
		}
	}
}

// MarkFailed sets MR phase to failed, reopens the writ for re-dispatch,
// and creates an escalation for visibility.
func (r *Forge) MarkFailed(mrID string) error {
	mr, err := r.worldStore.GetMergeRequest(mrID)
	if err != nil {
		return err
	}

	// CRASH SAFETY: Reopen writ FIRST — this is the critical state transition.
	// If we crash after reopening the writ but before marking the MR as failed,
	// the writ is "open" and can be re-dispatched (safe), and the MR remains
	// "claimed" — sentinel's releaseStaleClaims will eventually release it.
	// The reverse (MR "failed" but writ still "working") would leave the writ
	// permanently stuck: no agent is assigned (tether already cleared by
	// resolve) and no new MR will be created because the writ isn't "open".
	//
	// Conditional update: only reopen if the writ is still in "done" state
	// (the expected state after resolve). If the writ status has already
	// changed (e.g., re-resolved to "done" with a new MR, or closed by a
	// concurrent MarkMerged), the transition enforcement in UpdateWrit
	// will reject the update. We treat ErrInvalidTransition as non-fatal
	// since the writ is already in a valid state managed by another path.
	reopenErr := r.worldStore.UpdateWrit(mr.WritID, store.WritUpdates{
		Status:   "open",
		Assignee: "-",
	})
	writReopened := true
	if reopenErr != nil {
		if errors.Is(reopenErr, store.ErrInvalidTransition) {
			r.logger.Info("skipped writ reopen — status already changed",
				"writ", mr.WritID, "error", reopenErr)
			reopenErr = nil // not a failure — writ is in a valid state
			writReopened = false
		} else {
			r.logger.Error("failed to reopen writ before marking MR as failed",
				"writ", mr.WritID, "error", reopenErr)
			writReopened = false
		}
	}

	if err := r.worldStore.UpdateMergeRequestPhase(mrID, "failed"); err != nil {
		return fmt.Errorf("failed to mark MR as failed: %w", err)
	}

	// Create escalation for visibility (best-effort).
	desc := fmt.Sprintf("Merge failed for MR %s (branch %s, writ %s).", mrID, mr.Branch, mr.WritID)
	if reopenErr != nil {
		desc += fmt.Sprintf(" Writ reopen also failed: %v", reopenErr)
	} else if writReopened {
		desc += " Writ reopened for re-dispatch."
	} else {
		desc += " Writ status already changed (not reopened)."
	}
	desc += fmt.Sprintf(" Failed branch %s still exists on remote for inspection.", mr.Branch)
	if escID, err := r.sphereStore.CreateEscalation("high", r.agentID, desc, "mr:"+mrID); err != nil {
		r.logger.Error("failed to create escalation for merge failure",
			"mr", mrID, "error", err)
	} else {
		// Record initial notification time for aging checks.
		if err := r.sphereStore.UpdateEscalationLastNotified(escID); err != nil {
			r.logger.Error("failed to set last_notified_at for escalation",
				"escalation", escID, "error", err)
		}
	}

	// Best-effort: reset agent state to idle so it doesn't show "working (dead!)".
	// Parse agent name from branch conventions:
	//   outpost/{agentName}/{writID}
	//   envoy/{world}/{agentName}/...
	var agentID string
	if parts := strings.SplitN(mr.Branch, "/", 4); len(parts) >= 2 {
		switch parts[0] {
		case "outpost":
			agentID = r.world + "/" + parts[1]
		case "envoy":
			if len(parts) >= 3 {
				agentID = parts[1] + "/" + parts[2]
			}
		}
	}
	if agentID != "" {
		if err := r.sphereStore.UpdateAgentState(agentID, store.AgentIdle, ""); err != nil && !errors.Is(err, store.ErrNotFound) {
			r.logger.Error("failed to reset agent state to idle",
				"agent", agentID, "mr", mrID, "error", err)
		}
	}

	// NOTE: We intentionally do NOT delete the remote branch on failure.
	// The branch contains the only copy of the agent's work — if the outpost
	// worktree has already been cleaned up, deleting the remote branch loses
	// that work permanently. Stale branches from superseded failures are
	// cleaned up in MarkMerged (cleanupSupersededBranch) when a subsequent
	// MR for the same writ is merged.

	r.logger.Info("marked failed and reopened", "mr", mrID,
		"writ", mr.WritID, "branch", mr.Branch)

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
	revCtx, revCancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer revCancel()
	out, err := exec.CommandContext(revCtx, "git", "-C", r.worktree, "rev-parse",
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

	return taskID, nil
}

// RecoverOrphanedMerged finds MRs in "claimed" phase whose associated writ
// is "closed" — the state left by a partial MarkMerged failure where the writ
// was closed but UpdateMergeRequestPhase did not complete. For each such MR,
// it calls UpdateMergeRequestPhase directly to set the phase to "merged"; no
// new merge session is needed because the code is already on main.
// Returns the number of MRs recovered.
func (r *Forge) RecoverOrphanedMerged() (int, error) {
	claimed, err := r.worldStore.ListMergeRequests(store.MRClaimed)
	if err != nil {
		return 0, fmt.Errorf("failed to list claimed merge requests: %w", err)
	}

	recovered := 0
	for _, mr := range claimed {
		writ, err := r.worldStore.GetWrit(mr.WritID)
		if err != nil {
			r.logger.Warn("failed to get writ for claimed MR during orphan recovery",
				"mr", mr.ID, "writ", mr.WritID, "error", err)
			continue
		}
		if writ.Status != store.WritClosed {
			continue
		}
		// Writ is closed but MR is still claimed — partial MarkMerged failure.
		if err := r.worldStore.UpdateMergeRequestPhase(mr.ID, store.MRMerged); err != nil {
			r.logger.Error("failed to recover orphaned MR to merged",
				"mr", mr.ID, "writ", mr.WritID, "error", err)
			continue
		}
		// Resolve any escalations created for this MR (best-effort).
		r.resolveEscalationsForMR(mr.ID)
		r.logger.Info("recovered orphaned claimed MR to merged",
			"mr", mr.ID, "writ", mr.WritID)
		recovered++
	}
	return recovered, nil
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

// writTitle fetches the title of a writ, returning "" on error.
func (r *Forge) writTitle(writID string) string {
	item, err := r.worldStore.GetWrit(writID)
	if err != nil {
		return ""
	}
	return item.Title
}
