package forge

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/nevinsm/sol/internal/store"
)

// SweepEntry describes the disposition of a single branch in a sweep report.
type SweepEntry struct {
	Branch string `json:"branch"`
	WritID string `json:"writ_id,omitempty"`
	Reason string `json:"reason"`
}

// SweepReport summarises the results of a SweepBranches call.
type SweepReport struct {
	Inspected int          `json:"inspected"`
	Deleted   []SweepEntry `json:"deleted"`
	Preserved []SweepEntry `json:"preserved"`
	Errors    []SweepEntry `json:"errors"`
	DryRun    bool         `json:"dry_run"`
}

// SweepBranches iterates all outpost/*/sol-* and envoy/*/*/sol-* branches in
// the managed repo (remote and local) and deletes branches whose work has been
// reconciled. This complements the per-MR cleanup in deleteBranchIfContained
// by catching branches that were not cleaned at merge time due to network
// failures, crashes, or pre-fix stranded refs.
//
// Deletion logic for each candidate (writ ID parsed from branch name):
//   - Skip if the branch has no writ-ID suffix (envoy persistent branches, etc.).
//   - Skip if the branch is checked out in any worktree (never delete live work).
//   - Delete if the writ ID appears in the target branch's commit history
//     (writ's work is on target — safe to delete).
//   - Delete if includeClosedOrphans is true AND the writ is closed in the
//     world DB (writ was reconciled without a merge commit, e.g. after a
//     force-reset of the target branch).
//   - Preserve otherwise.
//
// If dryRun is true, the report is populated with what would be deleted but no
// actual deletions are performed.
//
// A single git fetch is performed at the start so all subsequent checks reflect
// the current remote state. Remote and local deletes are best-effort: failures
// are logged at Warn level and do not abort the sweep.
func (r *Forge) SweepBranches(ctx context.Context, includeClosedOrphans, dryRun bool) (SweepReport, error) {
	report := SweepReport{
		Deleted:   []SweepEntry{},
		Preserved: []SweepEntry{},
		Errors:    []SweepEntry{},
		DryRun:    dryRun,
	}

	if r.cfg.TargetBranch == "" {
		return report, fmt.Errorf("forge target branch is not configured")
	}

	runner := r.cmd
	if runner == nil {
		runner = &realCmdRunner{}
	}

	// Single fetch to get current remote state.
	fetchCtx, fetchCancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer fetchCancel()
	if out, err := runner.Run(fetchCtx, r.sourceRepo, "git", "fetch", "origin"); err != nil {
		return report, fmt.Errorf("git fetch origin failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// Verify the target ref exists before proceeding.
	targetRef := "refs/remotes/origin/" + r.cfg.TargetBranch
	verifyCtx, verifyCancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer verifyCancel()
	if _, err := runner.Run(verifyCtx, r.sourceRepo, "git", "rev-parse", "--verify", "--quiet", targetRef); err != nil {
		return report, fmt.Errorf("target ref %s not found in source repo", targetRef)
	}

	// Collect all candidate branches from remote tracking refs and local refs.
	candidates, err := r.listSweepCandidates(ctx, runner)
	if err != nil {
		return report, fmt.Errorf("failed to list sweep candidates: %w", err)
	}

	// Determine which branches are currently checked out in any worktree.
	checkedOut, err := r.listCheckedOutBranches(ctx, runner)
	if err != nil {
		return report, fmt.Errorf("failed to list checked-out branches: %w", err)
	}

	report.Inspected = len(candidates)

	for _, branch := range candidates {
		// Parse the writ ID from the branch name (last segment, "sol-" prefix).
		writID := parseWritID(branch)
		if writID == "" {
			// No writ-ID suffix — envoy persistent branch or unrecognised format.
			report.Preserved = append(report.Preserved, SweepEntry{
				Branch: branch,
				Reason: "no-writ-id",
			})
			continue
		}

		// Never delete a branch that is checked out in a worktree.
		if checkedOut[branch] {
			report.Preserved = append(report.Preserved, SweepEntry{
				Branch: branch,
				WritID: writID,
				Reason: "worktree-checked-out",
			})
			continue
		}

		// Check if the writ's work has landed on the target branch.
		landed, err := r.sweepWritOnTarget(ctx, runner, writID)
		if err != nil {
			r.logger.Warn("sweep: writ-on-target check failed",
				"branch", branch, "writ", writID, "error", err)
			report.Errors = append(report.Errors, SweepEntry{
				Branch: branch,
				WritID: writID,
				Reason: err.Error(),
			})
			continue
		}

		if landed {
			if !dryRun {
				r.sweepDeleteBranch(branch)
			}
			report.Deleted = append(report.Deleted, SweepEntry{
				Branch: branch,
				WritID: writID,
				Reason: "merged",
			})
			continue
		}

		// Optionally, also delete branches whose writ is closed in the DB but
		// whose writ ID is not on target (e.g. work written out by force-reset).
		if includeClosedOrphans {
			writ, err := r.worldStore.GetWrit(writID)
			if err == nil && writ.Status == store.WritClosed {
				if !dryRun {
					r.sweepDeleteBranch(branch)
				}
				report.Deleted = append(report.Deleted, SweepEntry{
					Branch: branch,
					WritID: writID,
					Reason: "closed-orphan",
				})
				continue
			}
		}

		report.Preserved = append(report.Preserved, SweepEntry{
			Branch: branch,
			WritID: writID,
			Reason: "not-merged",
		})
	}

	return report, nil
}

// listSweepCandidates returns all unique branch names matching
// outpost/*/sol-* and envoy/*/*/sol-* from both remote tracking refs and
// local refs in the managed repo. Branches are returned sorted for
// deterministic output.
func (r *Forge) listSweepCandidates(ctx context.Context, runner cmdRunner) ([]string, error) {
	seen := map[string]bool{}

	// Remote branches: strip the "origin/" prefix from each line.
	for _, pattern := range []string{"origin/outpost/*/sol-*", "origin/envoy/*/*/sol-*"} {
		listCtx, listCancel := context.WithTimeout(ctx, gitCommandTimeout)
		out, err := runner.Run(listCtx, r.sourceRepo, "git", "branch", "-r", "--list", pattern)
		listCancel()
		if err != nil {
			return nil, fmt.Errorf("git branch -r --list %q: %w", pattern, err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Remote ref lines are like "  origin/outpost/Toast/sol-aaa11111".
			branch := strings.TrimPrefix(line, "origin/")
			if branch != line { // only add when prefix was present
				seen[branch] = true
			}
		}
	}

	// Local branches.
	for _, pattern := range []string{"outpost/*/sol-*", "envoy/*/*/sol-*"} {
		listCtx, listCancel := context.WithTimeout(ctx, gitCommandTimeout)
		out, err := runner.Run(listCtx, r.sourceRepo, "git", "branch", "--list", pattern)
		listCancel()
		if err != nil {
			return nil, fmt.Errorf("git branch --list %q: %w", pattern, err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			// "git branch" output may carry a "* " prefix for the current branch.
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, "* ")
			if line == "" {
				continue
			}
			seen[line] = true
		}
	}

	result := make([]string, 0, len(seen))
	for branch := range seen {
		result = append(result, branch)
	}
	sort.Strings(result)
	return result, nil
}

// listCheckedOutBranches returns the set of branch names currently checked out
// in any git worktree of the managed repo (including the main worktree).
func (r *Forge) listCheckedOutBranches(ctx context.Context, runner cmdRunner) (map[string]bool, error) {
	listCtx, listCancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer listCancel()

	out, err := runner.Run(listCtx, r.sourceRepo, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git worktree list --porcelain: %w", err)
	}

	checkedOut := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "branch ") {
			// Porcelain format: "branch refs/heads/{branchname}"
			ref := strings.TrimPrefix(line, "branch ")
			branch := strings.TrimPrefix(ref, "refs/heads/")
			checkedOut[branch] = true
		}
	}
	return checkedOut, nil
}

// sweepWritOnTarget checks whether writID appears in the target branch's
// commit history. It does NOT perform a git fetch — callers must fetch first.
func (r *Forge) sweepWritOnTarget(ctx context.Context, runner cmdRunner, writID string) (bool, error) {
	targetRef := "refs/remotes/origin/" + r.cfg.TargetBranch
	checkCtx, checkCancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer checkCancel()

	out, err := runner.Run(checkCtx, r.sourceRepo, "git", "log", targetRef,
		"--grep="+writID, "-n", "1", "--format=%H")
	if err != nil {
		return false, fmt.Errorf("git log --grep=%s: %s: %w",
			writID, strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// sweepDeleteBranch deletes both the remote and local copies of branch.
// Both operations are best-effort: failures are logged at Warn level and
// never abort the sweep.
func (r *Forge) sweepDeleteBranch(branch string) {
	// Delete remote (best-effort).
	pushCtx, pushCancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer pushCancel()
	if perr := exec.CommandContext(pushCtx, "git", "-C", r.sourceRepo,
		"push", "origin", "--delete", branch).Run(); perr != nil {
		r.logger.Warn("sweep: failed to delete remote branch",
			"branch", branch, "error", perr)
	}

	// Delete local (best-effort).
	branchCtx, branchCancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer branchCancel()
	if berr := exec.CommandContext(branchCtx, "git", "-C", r.sourceRepo,
		"branch", "-D", branch).Run(); berr != nil {
		r.logger.Warn("sweep: failed to delete local branch",
			"branch", branch, "error", berr)
	}
}

// parseWritID extracts the writ ID from a branch name. A writ ID is the last
// slash-separated segment if it starts with "sol-" (i.e. it is a writ-id
// suffix, not an agent name or world name). Returns "" if no writ ID is found.
//
// Examples:
//
//	outpost/Toast/sol-aaa11111          → "sol-aaa11111"
//	envoy/world/Envoy/sol-bbb22222      → "sol-bbb22222"
//	envoy/world/Envoy                   → "" (persistent envoy branch)
func parseWritID(branch string) string {
	parts := strings.Split(branch, "/")
	last := parts[len(parts)-1]
	if strings.HasPrefix(last, "sol-") && len(last) > 4 {
		return last
	}
	return ""
}
