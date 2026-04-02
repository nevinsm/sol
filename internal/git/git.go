// Package git provides reusable git operations via subprocess.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitError contains raw output from a failed git command.
type GitError struct {
	Command string   // The git subcommand (e.g., "merge", "push")
	Args    []string // Full argument list
	Stdout  string   // Raw stdout
	Stderr  string   // Raw stderr
	Err     error    // Underlying error
}

func (e *GitError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("git %s: %s", e.Command, e.Stderr)
	}
	return fmt.Sprintf("git %s: %v", e.Command, e.Err)
}

func (e *GitError) Unwrap() error {
	return e.Err
}

// SubmoduleChange represents a submodule pointer change between two refs.
type SubmoduleChange struct {
	Path   string // Submodule path relative to repo root
	OldSHA string // Previous commit SHA (empty for new submodule)
	NewSHA string // New commit SHA (empty for removed submodule)
}

// Git wraps git operations for a working directory.
type Git struct {
	workDir string
}

// New creates a new Git wrapper for the given directory.
func New(workDir string) *Git {
	return &Git{workDir: workDir}
}

// WorkDir returns the working directory.
func (g *Git) WorkDir() string {
	return g.workDir
}

// run executes a git command and returns trimmed stdout.
func (g *Git) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Determine the subcommand name for the error.
		command := ""
		for _, arg := range args {
			if !strings.HasPrefix(arg, "-") {
				command = arg
				break
			}
		}
		if command == "" && len(args) > 0 {
			command = args[0]
		}
		return "", &GitError{
			Command: command,
			Args:    args,
			Stdout:  strings.TrimSpace(stdout.String()),
			Stderr:  strings.TrimSpace(stderr.String()),
			Err:     err,
		}
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Fetch fetches from the given remote.
func (g *Git) Fetch(remote string) error {
	_, err := g.run("fetch", remote)
	return err
}

// Checkout checks out the given ref.
func (g *Git) Checkout(ref string) error {
	_, err := g.run("checkout", ref)
	return err
}

// Pull pulls from the remote branch.
func (g *Git) Pull(remote, branch string) error {
	_, err := g.run("pull", remote, branch)
	return err
}

// Push pushes HEAD to the remote branch.
// If forceWithLease is true, uses --force-with-lease for safe force push.
func (g *Git) Push(remote, refspec string, forceWithLease bool) error {
	args := []string{"push", remote, refspec}
	if forceWithLease {
		args = append(args, "--force-with-lease")
	}
	_, err := g.run(args...)
	return err
}

// Rev returns the commit hash for the given ref (rev-parse).
func (g *Git) Rev(ref string) (string, error) {
	return g.run("rev-parse", ref)
}

// BranchExists checks if a branch exists (local or remote tracking).
func (g *Git) BranchExists(name string) (bool, error) {
	_, err := g.run("show-ref", "--verify", "--quiet", "refs/heads/"+name)
	if err != nil {
		// If the local lookup failed with a non-GitError (e.g. permission denied,
		// git binary missing), surface it immediately rather than falling through
		// to the remote lookup and silently discarding the real error.
		var ge *GitError
		if !isGitError(err, &ge) {
			return false, err
		}
		// Also try remote tracking refs.
		_, err2 := g.run("show-ref", "--verify", "--quiet", "refs/remotes/"+name)
		if err2 != nil {
			// show-ref returns exit 1 when ref doesn't exist — that's not an error.
			var ge2 *GitError
			if isGitError(err2, &ge2) {
				return false, nil
			}
			return false, err2
		}
		return true, nil
	}
	return true, nil
}

// ResetHard resets the working tree and index to the given ref.
func (g *Git) ResetHard(ref string) error {
	_, err := g.run("reset", "--hard", ref)
	return err
}

// MergeSquash performs a squash merge of the given branch and commits with the
// provided message. Two-step: git merge --squash, then git commit -m.
func (g *Git) MergeSquash(branch, message string) error {
	if _, err := g.run("merge", "--squash", branch); err != nil {
		return err
	}
	_, err := g.run("commit", "-m", message)
	return err
}

// VerifyCleanWorktree checks that the working tree has no uncommitted changes,
// staged files, or untracked files. Returns nil if the worktree is clean, or an
// error describing the dirty state.
func (g *Git) VerifyCleanWorktree() error {
	out, err := g.run("status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status --porcelain failed: %w", err)
	}
	if out != "" {
		// Truncate the status output to avoid unbounded error messages.
		lines := strings.Split(out, "\n")
		preview := out
		if len(lines) > 10 {
			preview = strings.Join(lines[:10], "\n") + fmt.Sprintf("\n... and %d more files", len(lines)-10)
		}
		return fmt.Errorf("worktree is dirty:\n%s", preview)
	}
	return nil
}

// CheckConflicts performs a dry-run merge to check if source can be merged into
// the current branch without conflicts. Returns conflicting file names, or nil
// if clean. Always cleans up — no actual changes persist.
func (g *Git) CheckConflicts(source, target string) (conflicts []string, err error) {
	// Guard: refuse to run if worktree is dirty — checkout and merge --no-commit
	// will produce unpredictable results on a dirty worktree.
	if cleanErr := g.VerifyCleanWorktree(); cleanErr != nil {
		return nil, fmt.Errorf("CheckConflicts requires clean worktree: %w", cleanErr)
	}

	// Save current branch/HEAD so we can restore it when done.
	origRef, err := g.run("symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		// Detached HEAD — fall back to commit SHA.
		origRef, err = g.Rev("HEAD")
		if err != nil {
			return nil, fmt.Errorf("saving current HEAD: %w", err)
		}
	} else {
		// Strip refs/heads/ prefix for checkout.
		origRef = strings.TrimPrefix(origRef, "refs/heads/")
	}
	defer func() {
		if restoreErr := g.Checkout(origRef); restoreErr != nil && err == nil {
			err = fmt.Errorf("conflict check succeeded but failed to restore branch %s: %w", origRef, restoreErr)
		}
	}()

	// Checkout the target branch.
	if err := g.Checkout(target); err != nil {
		return nil, fmt.Errorf("checkout target %s: %w", target, err)
	}

	// Attempt test merge with --no-commit --no-ff.
	_, mergeErr := g.run("merge", "--no-commit", "--no-ff", source)

	if mergeErr != nil {
		// Check for unmerged files (conflict indicator).
		conflicts, err := g.GetConflictingFiles()
		if err == nil && len(conflicts) > 0 {
			if abortErr := g.AbortMerge(); abortErr != nil {
				return nil, fmt.Errorf("conflicts detected but merge abort failed (repo may be in MERGE_HEAD state, consider re-cloning): %w", abortErr)
			}
			return conflicts, nil
		}
		// Some other merge error — attempt cleanup.
		if abortErr := g.AbortMerge(); abortErr != nil {
			return nil, fmt.Errorf("merge failed and merge abort failed (repo may be in MERGE_HEAD state, consider re-cloning): %w", abortErr)
		}
		return nil, mergeErr
	}

	// Merge succeeded (no conflicts) — abort to clean up MERGE_HEAD.
	if err := g.AbortMerge(); err != nil {
		return nil, fmt.Errorf("merge cleanup abort failed (MERGE_HEAD may linger, consider re-cloning): %w", err)
	}
	return nil, nil
}

// GetConflictingFiles returns files with unresolved merge conflicts.
func (g *Git) GetConflictingFiles() ([]string, error) {
	out, err := g.run("diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var files []string
	for _, f := range strings.Split(out, "\n") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

// AbortMerge aborts a merge in progress.
func (g *Git) AbortMerge() error {
	_, err := g.run("merge", "--abort")
	return err
}

// GetBranchCommitMessage returns the commit message of the HEAD commit on
// the given branch. Useful for preserving conventional commit messages during
// squash merges.
func (g *Git) GetBranchCommitMessage(branch string) (string, error) {
	return g.run("log", "-1", "--format=%B", branch)
}

// MergeBase returns the best common ancestor of two refs.
func (g *Git) MergeBase(a, b string) (string, error) {
	return g.run("merge-base", a, b)
}

// DiffNameOnly returns file names changed between two refs.
func (g *Git) DiffNameOnly(base, head string) ([]string, error) {
	out, err := g.run("diff", "--name-only", base, head)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var files []string
	for _, f := range strings.Split(out, "\n") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

// SubmoduleChanges detects submodule pointer changes between two refs.
// Returns nil if no submodules changed.
func (g *Git) SubmoduleChanges(base, head string) ([]SubmoduleChange, error) {
	out, err := g.run("diff", "--raw", base, head)
	if err != nil {
		return nil, fmt.Errorf("diffing for submodule changes: %w", err)
	}
	if out == "" {
		return nil, nil
	}

	var changes []SubmoduleChange
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Submodule entries have mode 160000.
		if !strings.Contains(line, "160000") {
			continue
		}
		// Format: :oldmode newmode oldsha newsha status\tpath
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		path := strings.TrimSpace(parts[1])
		fields := strings.Fields(parts[0])
		if len(fields) < 5 {
			continue
		}
		oldSHA := fields[2]
		newSHA := fields[3]
		if strings.Repeat("0", len(oldSHA)) == oldSHA {
			oldSHA = ""
		}
		if strings.Repeat("0", len(newSHA)) == newSHA {
			newSHA = ""
		}
		changes = append(changes, SubmoduleChange{
			Path:   path,
			OldSHA: oldSHA,
			NewSHA: newSHA,
		})
	}
	return changes, nil
}

// PushSubmoduleCommit pushes a specific commit SHA from a submodule to its remote.
func (g *Git) PushSubmoduleCommit(submodulePath, sha, remote string) error {
	absPath := filepath.Join(g.workDir, submodulePath)
	defaultBranch, err := submoduleDefaultBranch(absPath, remote)
	if err != nil {
		return fmt.Errorf("detecting default branch for submodule %s: %w", submodulePath, err)
	}
	cmd := exec.Command("git", "-C", absPath, "push", remote, sha+":refs/heads/"+defaultBranch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pushing submodule %s commit %s: %s: %w",
			submodulePath, sha[:min(8, len(sha))], strings.TrimSpace(stderr.String()), err)
	}
	return nil
}

// InitSubmodules initializes and updates submodules if .gitmodules exists.
// No-op for repos without submodules.
func InitSubmodules(repoPath string) error {
	gitmodules := filepath.Join(repoPath, ".gitmodules")
	if _, err := os.Stat(gitmodules); os.IsNotExist(err) {
		return nil
	}
	cmd := exec.Command("git", "-C", repoPath, "submodule", "update", "--init", "--recursive")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("initializing submodules: %w", err)
	}
	return nil
}

// submoduleDefaultBranch detects the default branch of a submodule's remote.
func submoduleDefaultBranch(submodulePath, remote string) (string, error) {
	// Try local symbolic-ref first (no network).
	symCmd := exec.Command("git", "-C", submodulePath, "symbolic-ref", "refs/remotes/"+remote+"/HEAD")
	if symOut, err := symCmd.Output(); err == nil {
		ref := strings.TrimSpace(string(symOut))
		if parts := strings.Split(ref, "/"); len(parts) > 0 {
			branch := parts[len(parts)-1]
			if branch != "" {
				return branch, nil
			}
		}
	}

	// Try local tracking refs (no network).
	for _, candidate := range []string{"main", "master"} {
		check := exec.Command("git", "-C", submodulePath, "rev-parse", "--verify", "--quiet",
			"refs/remotes/"+remote+"/"+candidate)
		if check.Run() == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("could not determine default branch for remote %s", remote)
}

// LsRemote queries the remote for the SHA of a given ref.
// Returns the full commit hash and nil on success.
func (g *Git) LsRemote(remote, ref string) (string, error) {
	out, err := g.run("ls-remote", remote, ref)
	if err != nil {
		return "", err
	}
	if out == "" {
		return "", fmt.Errorf("ls-remote returned empty for %s %s", remote, ref)
	}
	// Output format: "<sha>\t<refname>"
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", fmt.Errorf("ls-remote returned unexpected output: %s", out)
	}
	return fields[0], nil
}

// isGitError checks if err is a *GitError and populates target.
func isGitError(err error, target **GitError) bool {
	return errors.As(err, target)
}
