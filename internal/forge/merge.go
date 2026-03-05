package forge

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nevinsm/sol/internal/git"
	"github.com/nevinsm/sol/internal/store"
)

// MergeResult holds the structured outcome of a merge attempt.
type MergeResult struct {
	Success       bool         `json:"success"`
	MergeCommit   string       `json:"merge_commit,omitempty"`
	Conflict      bool         `json:"conflict,omitempty"`
	ConflictFiles []string     `json:"conflict_files,omitempty"`
	GatesFailed   bool         `json:"gates_failed,omitempty"`
	GateResults   []GateResult `json:"gate_results,omitempty"`
	PushRejected  bool         `json:"push_rejected,omitempty"`
	Error         string       `json:"error,omitempty"`
}

// Merge performs a deterministic Go-driven squash merge of the given MR.
// All git operations are handled in Go — no Claude-driven rebase.
//
// 9-step flow:
//  1. Fetch origin
//  2. ResetHard to target tip
//  3. CheckConflicts (dry-run)
//  4. SubmoduleChanges + push
//  5. GetBranchCommitMessage
//  6. MergeSquash
//  7. Post-merge file verification (best-effort)
//  8. RunGates
//  9. Push
func (r *Forge) Merge(ctx context.Context, mr *store.MergeRequest) MergeResult {
	g := git.New(r.worktree)
	targetRef := "origin/" + r.cfg.TargetBranch

	reset := func() {
		if err := g.ResetHard(targetRef); err != nil {
			r.logger.Warn("reset after failure failed", "error", err)
		}
	}

	// Step 1: Fetch origin.
	r.logger.Info("merge: fetching origin", "mr", mr.ID)
	if err := g.Fetch("origin"); err != nil {
		reset()
		return MergeResult{Error: fmt.Sprintf("fetch failed: %v", err)}
	}

	// Step 2: Reset forge worktree to target tip.
	if err := g.ResetHard(targetRef); err != nil {
		return MergeResult{Error: fmt.Sprintf("reset to %s failed: %v", targetRef, err)}
	}

	// Step 3: Dry-run conflict check.
	r.logger.Info("merge: checking conflicts", "mr", mr.ID, "branch", mr.Branch)
	conflicts, err := g.CheckConflicts(mr.Branch, targetRef)
	if err != nil {
		reset()
		return MergeResult{Error: fmt.Sprintf("conflict check failed: %v", err)}
	}
	if len(conflicts) > 0 {
		reset()
		return MergeResult{Conflict: true, ConflictFiles: conflicts}
	}

	// Step 4: Submodule changes (non-fatal warnings).
	subChanges, err := g.SubmoduleChanges(targetRef, mr.Branch)
	if err != nil {
		r.logger.Warn("could not check submodule changes", "error", err)
	}
	if len(subChanges) > 0 {
		if initErr := git.InitSubmodules(r.worktree); initErr != nil {
			r.logger.Warn("failed to init submodules", "error", initErr)
		}
		for _, sc := range subChanges {
			if sc.NewSHA == "" {
				continue
			}
			r.logger.Info("pushing submodule commit", "path", sc.Path, "sha", sc.NewSHA[:min(8, len(sc.NewSHA))])
			if pushErr := g.PushSubmoduleCommit(sc.Path, sc.NewSHA, "origin"); pushErr != nil {
				r.logger.Warn("failed to push submodule", "path", sc.Path, "error", pushErr)
			}
		}
	}

	// Step 5: Preserve original commit message.
	commitMsg, err := g.GetBranchCommitMessage(mr.Branch)
	if err != nil {
		commitMsg = fmt.Sprintf("Squash merge %s into %s", mr.Branch, r.cfg.TargetBranch)
		r.logger.Warn("could not get branch commit message, using fallback", "error", err)
	}

	// Step 6: Squash merge.
	r.logger.Info("merge: squash merging", "mr", mr.ID, "branch", mr.Branch)
	if err := g.MergeSquash(mr.Branch, commitMsg); err != nil {
		// Check if this is a conflict during actual merge.
		conflictFiles, conflictErr := g.GetConflictingFiles()
		_ = g.AbortMerge()
		reset()
		if conflictErr == nil && len(conflictFiles) > 0 {
			return MergeResult{Conflict: true, ConflictFiles: conflictFiles}
		}
		return MergeResult{Error: fmt.Sprintf("squash merge failed: %v", err)}
	}

	// Step 7: Post-merge file verification (best-effort warning).
	r.verifyMergedFiles(g, targetRef, mr.Branch)

	// Step 8: Run quality gates.
	r.logger.Info("merge: running gates", "mr", mr.ID)
	gateResults, err := r.RunGates(ctx)
	if err != nil {
		reset()
		return MergeResult{Error: fmt.Sprintf("gate execution error: %v", err)}
	}
	allPassed := true
	for _, gr := range gateResults {
		if !gr.Passed {
			allPassed = false
			break
		}
	}
	if !allPassed {
		reset()
		return MergeResult{GatesFailed: true, GateResults: gateResults}
	}

	// Step 9: Push.
	r.logger.Info("merge: pushing", "mr", mr.ID)
	if err := r.Push(); err != nil {
		reset()
		return MergeResult{PushRejected: true, Error: fmt.Sprintf("push rejected: %v", err)}
	}

	// Step 10: Post-push verification — confirm remote received the commit.
	localSHA, _ := g.Rev("HEAD")
	remoteSHA, err := g.LsRemote("origin", r.cfg.TargetBranch)
	if err != nil || remoteSHA != localSHA {
		r.logger.Error("post-push verification failed",
			"local", localSHA, "remote", remoteSHA, "error", err)
		reset()
		return MergeResult{PushRejected: true, Error: "post-push verification failed: remote SHA mismatch"}
	}

	r.logger.Info("merge: success", "mr", mr.ID, "commit", localSHA)
	return MergeResult{Success: true, MergeCommit: localSHA}
}

// verifyMergedFiles logs a warning if files from the branch are missing in the
// merge result. Best-effort — errors are swallowed.
func (r *Forge) verifyMergedFiles(g *git.Git, targetRef, branch string) {
	mergeBase, err := g.MergeBase(targetRef, branch)
	if err != nil {
		return
	}
	branchFiles, err := g.DiffNameOnly(mergeBase, branch)
	if err != nil {
		return
	}
	resultFiles, err := g.DiffNameOnly(targetRef, "HEAD")
	if err != nil {
		return
	}

	resultSet := make(map[string]bool, len(resultFiles))
	for _, f := range resultFiles {
		resultSet[f] = true
	}

	var missing []string
	for _, f := range branchFiles {
		if !resultSet[f] {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		r.logger.Warn("post-merge verification: branch files missing from result",
			slog.String("branch", branch),
			slog.Any("missing_files", missing))
	}
}
