package forge

import (
	"context"
	"fmt"
	"strings"

	"github.com/nevinsm/sol/internal/setup"
	"github.com/nevinsm/sol/internal/store"
)

// matchesSolManagedPath reports whether the given repo-relative path matches
// one of the sol-managed patterns. Patterns follow setup.SolManagedPaths()
// convention: a trailing "/" denotes a directory match (the directory itself
// and everything under it); anything else matches a single file by exact
// path.
func matchesSolManagedPath(path string, patterns []string) bool {
	path = strings.TrimPrefix(path, "./")
	for _, p := range patterns {
		if dir, ok := strings.CutSuffix(p, "/"); ok {
			if path == dir || strings.HasPrefix(path, p) {
				return true
			}
			continue
		}
		if path == p {
			return true
		}
	}
	return false
}

// filterSolManagedPaths returns the subset of files that match a sol-managed
// pattern, using setup.SolManagedPaths() as the single source of truth so
// this gate can never drift from the git exclude installer.
func filterSolManagedPaths(files []string) []string {
	patterns := setup.SolManagedPaths()
	var matched []string
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if matchesSolManagedPath(f, patterns) {
			matched = append(matched, f)
		}
	}
	return matched
}

// checkPathGate deterministically checks whether mr's branch touches any
// sol-managed worktree path before a merge session is ever started. This is
// the forge-side guard from the writ: the exclude-list mechanism
// (setup.InstallExcludes) is inherently ordering-dependent — repo-resolved
// guidelines go live at merge, while exclude entries only go live at binary
// install plus a world sync — so it cannot be the only guard. This check
// runs on every claimed MR regardless of deployment ordering.
//
// It computes the file list the squash merge would introduce via a
// triple-dot diff (mr.Branch against its merge-base with the target branch)
// and matches each file against setup.SolManagedPaths(). Returns the
// offending paths in diff order (empty if none). Diff and pattern matching
// are pure Go — no AI callout.
//
// On a git error (fetch/diff failure), returns an error and no offending
// paths — the caller should log and let the merge session proceed rather
// than reject an MR on an inconclusive check.
func (s *patrolState) checkPathGate(ctx context.Context, mr *store.MergeRequest) ([]string, error) {
	sourceRepo := s.forge.sourceRepo
	if sourceRepo == "" {
		return nil, fmt.Errorf("source repo not configured; cannot run path gate")
	}

	// Refresh remote-tracking refs for both the target and source branches.
	// The default clone fetch refspec covers all heads, so a plain
	// `git fetch origin` refreshes origin/{branch} along with
	// origin/{target}.
	if out, err := s.cmd.Run(ctx, sourceRepo, "git", "fetch", "origin"); err != nil {
		return nil, fmt.Errorf("path gate: git fetch origin failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	targetRef := "origin/" + s.forge.cfg.TargetBranch
	sourceRef := "origin/" + mr.Branch
	diffRange := fmt.Sprintf("%s...%s", targetRef, sourceRef)

	out, err := s.cmd.Run(ctx, sourceRepo, "git", "diff", "--name-only", diffRange)
	if err != nil {
		return nil, fmt.Errorf("path gate: git diff --name-only %s failed: %s: %w",
			diffRange, strings.TrimSpace(string(out)), err)
	}

	files := strings.Split(strings.TrimSpace(string(out)), "\n")
	return filterSolManagedPaths(files), nil
}
