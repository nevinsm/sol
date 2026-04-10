package forge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/account"
	"github.com/nevinsm/sol/internal/budget"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
)

// ForgeSessionManager is the subset of session.SessionManager used by the forge
// orchestrator. Defined here to avoid a dependency cycle and to keep the forge's
// test surface small.
type ForgeSessionManager interface {
	Start(name, workdir, cmd string, env map[string]string, role, world string) error
	Stop(name string, force bool) error
	Exists(name string) bool
	Inject(name string, text string, submit bool) error
	Capture(name string, lines int) (string, error)
}

// ForgeResult and result file constants are defined in result.go.

// sessionOutcome describes how the monitored session ended.
type sessionOutcome int

const (
	sessionCompleted sessionOutcome = iota // session finished (result file may exist)
	sessionStuck                           // AI assessment determined session is stuck
	sessionCancelled                       // context was cancelled
)

// monitorCaptureLines is how many tmux lines to capture per check.
const monitorCaptureLines = 80

// maxResolutionTasks caps the number of conflict resolution tasks that can be
// created for a single merge request. Without this bound, a persistent conflict
// (e.g., a branch that always conflicts after resolution) creates an unbounded
// cascade of resolution tasks.
const maxResolutionTasks = 3

// mergeSessionName returns the tmux session name for a forge merge session.
func mergeSessionName(world string) string {
	return config.SessionName(world, "forge-merge")
}

// SessionLauncher abstracts startup.Launch for testing. Production code uses
// startup.Launch; tests can inject a mock that skips world config, config dir,
// and sphere store setup.
type SessionLauncher func(cfg startup.RoleConfig, world, agent string, opts startup.LaunchOpts) (string, error)

// runMergeSession starts a Claude session to execute the merge, monitors it,
// reads the result, and returns the ForgeResult. The caller is responsible for
// acting on the result (mark merged/failed/etc.) and cleaning up the session.
//
// queueDepth is the depth of the ready queue at claim time and is plumbed
// through to monitorSession's heartbeat writes so that the high-frequency
// monitor heartbeats record the same depth the patrol-written heartbeat does.
// Without this, monitor heartbeats clobber patrol's queue_depth to 0 mid-merge
// (CF-M11).
func (s *patrolState) runMergeSession(ctx context.Context, mr *store.MergeRequest, queueDepth int) (*ForgeResult, error) {
	if s.forge.sessions == nil {
		return nil, fmt.Errorf("session manager not configured")
	}

	sessionName := mergeSessionName(s.forge.world)
	worktree := s.forge.worktree

	// 1. Stop any leftover session from a prior crash. Call Stop() directly
	// without Exists() guard to avoid a TOCTOU race (session can die between
	// Exists and Stop). Treat "not found" as non-fatal.
	if err := s.forge.sessions.Stop(sessionName, true); err != nil {
		if !strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("failed to stop leftover merge session: %w", err)
		}
	} else {
		s.fl.Log("CLEANUP", fmt.Sprintf("stopped leftover merge session %s", sessionName))
		// Wait for git lock files to clear after killing the session.
		s.waitForGitLock(worktree, 3*time.Second)
	}

	// 1b. Verify worktree is clean before proceeding. If a prior cleanup failed,
	// launching into a dirty worktree causes cascading failures.
	{
		verifyCtx, verifyCancel := context.WithTimeout(ctx, 10*time.Second)
		defer verifyCancel()
		if err := s.verifyCleanWorktree(verifyCtx); err != nil {
			return nil, fmt.Errorf("worktree not clean before merge session: %w", err)
		}
	}

	// Capture the remote target branch HEAD before launching the merge session.
	// The session will push to origin, and verifyPush uses this as the lower
	// bound of a ref-range search (preMergeRef..origin/{targetBranch}) so that
	// push verification is clock-independent and window-independent.
	//
	// Capture against sourceRepo (the managed clone) rather than the forge
	// worktree so the baseline is stable even if the forge worktree is torn
	// apart during the merge session. The forge worktree historically produced
	// false-negative verification failures when it became structurally broken
	// mid-session: exit 128 from git commands in a missing .git was treated as
	// "push didn't land" when in fact the push had succeeded.
	{
		targetRef := fmt.Sprintf("origin/%s", s.forge.cfg.TargetBranch)
		refDir := s.forge.sourceRepo
		if refDir == "" {
			refDir = worktree
		}
		if out, err := s.cmd.Run(ctx, refDir, "git", "rev-parse", targetRef); err == nil {
			s.preMergeRef = strings.TrimSpace(string(out))
		} else {
			s.forge.logger.Warn("failed to capture pre-merge ref, push verification will search full history",
				"dir", refDir, "error", err)
			s.preMergeRef = ""
		}
	}

	// 2. Build injection context using the full builder.
	writ, err := s.forge.worldStore.GetWrit(mr.WritID)
	if err != nil {
		return nil, fmt.Errorf("failed to get writ %s for injection: %w", mr.WritID, err)
	}

	injectionCfg := InjectionConfig{
		MaxAttempts:  s.forge.cfg.MaxAttempts,
		GateCommands: s.forge.cfg.QualityGates,
		WorktreeDir:  worktree,
		TargetBranch: s.forge.cfg.TargetBranch,
		World:        s.forge.world,
	}
	injection := BuildInjection(mr, writ, injectionCfg)

	// 3. Write injection file for PrimeBuilder and PreCompact hook.
	if err := WriteInjectionFile(worktree, injection); err != nil {
		return nil, fmt.Errorf("failed to write injection file: %w", err)
	}

	// 4. Clean stale result file. If removal fails (permission denied, file
	// locked), abort — launching the session would read a stale result.
	if err := CleanForgeResult(worktree); err != nil {
		return nil, fmt.Errorf("failed to clean stale result file: %w", err)
	}

	// 5. Launch session via startup infrastructure.
	launch := s.forge.launcher
	if launch == nil {
		launch = startup.Launch
	}

	cfg := ForgeMergeRoleConfig(s.forge.cfg.TargetBranch)
	opts := startup.LaunchOpts{
		Sessions: s.forge.sessions,
	}
	if _, err := launch(cfg, s.forge.world, "forge-merge", opts); err != nil {
		return nil, fmt.Errorf("failed to launch merge session: %w", err)
	}

	s.fl.Log("SESSION", fmt.Sprintf("started merge session %s for %s", sessionName, mr.Branch))

	// 6. Monitor session progress.
	outcome := s.monitorSession(ctx, sessionName, mr, queueDepth)

	// 7. Read result based on outcome.
	switch outcome {
	case sessionCompleted, sessionStuck:
		// Try to read the result file regardless — the session may have written
		// one even if it appeared stuck/idle.
		result, err := ReadResult(s.forge.worktree)
		if err != nil {
			if outcome == sessionStuck {
				return &ForgeResult{
					Result:  "failed",
					Summary: "session was stuck and no result file found",
				}, nil
			}
			return nil, fmt.Errorf("session completed but result file missing: %w", err)
		}
		return result, nil

	case sessionCancelled:
		return nil, fmt.Errorf("merge session cancelled")

	default:
		return nil, fmt.Errorf("unexpected session outcome: %d", outcome)
	}
}

// monitorSession watches the merge session using output hash comparison,
// replicating the sentinel's checkProgress pattern. Writes heartbeat during
// monitoring to maintain liveness.
//
// queueDepth is the depth of the ready queue at claim time (passed by the
// caller). It is written into every "working" heartbeat so that operators
// viewing `sol forge status` during a long merge see the actual queue depth
// rather than 0 (CF-M11).
func (s *patrolState) monitorSession(ctx context.Context, sessionName string, mr *store.MergeRequest, queueDepth int) sessionOutcome {
	var lastHash string
	interval := s.pcfg.MonitorInterval
	if interval <= 0 {
		interval = 3 * time.Minute
	}

	// Initial fast check: detect sessions that complete quickly (e.g., fast
	// merges, immediate failures) without waiting for the first full tick.
	// Uses a short delay to let the session start up before first poll.
	initialDelay := min(5*time.Second, interval)
	timer := time.NewTimer(initialDelay)
	defer timer.Stop()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	firstCheck := true
	for {
		select {
		case <-ctx.Done():
			return sessionCancelled

		case <-timer.C:
			if !firstCheck {
				continue
			}
			firstCheck = false

		case <-ticker.C:
		}

		// Shared monitoring logic — runs after both timer and ticker fire.

		// Write heartbeat to show we're still alive. Use the queue depth
		// captured at claim time so monitor heartbeats don't clobber the
		// patrol-written value to 0 mid-merge (CF-M11).
		s.writeHeartbeatWithMR("working", queueDepth, mr)

		// Fast path: result file means work is done, regardless of session state.
		resultPath := filepath.Join(s.forge.worktree, resultFileName)
		if _, err := os.Stat(resultPath); err == nil {
			s.fl.Log("SESSION", "result file detected, completing")
			return sessionCompleted
		}

		// Check if session still exists.
		if !s.forge.sessions.Exists(sessionName) {
			s.fl.Log("SESSION", "merge session exited")
			return sessionCompleted
		}

		// Capture output and hash.
		output, err := s.forge.sessions.Capture(sessionName, monitorCaptureLines)
		if err != nil {
			s.forge.logger.Warn("failed to capture merge session output", "error", err)
			continue
		}

		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(output)))

		if lastHash == "" {
			// First capture — establish baseline.
			lastHash = hash
			continue
		}

		if hash != lastHash {
			// Output changed — session is making progress.
			lastHash = hash
			s.fl.Log("SESSION", "merge session progressing")
			continue
		}

		// Output unchanged — assess with AI.
		s.fl.Log("SESSION", "merge session output unchanged, assessing...")
		assessment := s.assessMergeSession(ctx, sessionName, output, mr)

		switch assessment {
		case "progressing":
			s.fl.Log("SESSION", "assessment: progressing, continuing to wait")
			// Reset hash so we don't re-assess on the same output.
			lastHash = hash
		case "stuck":
			s.fl.Log("SESSION", "assessment: stuck, stopping session")
			return sessionStuck
		case "idle":
			s.fl.Log("SESSION", "assessment: idle, checking for result")
			// Check if result file exists.
			resultPath := filepath.Join(s.forge.worktree, resultFileName)
			if _, err := os.Stat(resultPath); err == nil {
				return sessionCompleted
			}
			// No result file and idle — likely stuck.
			return sessionStuck
		default:
			// Unknown assessment — continue waiting.
			s.fl.Log("SESSION", fmt.Sprintf("assessment: %s, continuing to wait", assessment))
			lastHash = hash
		}
	}
}

// assessMergeSession runs AI assessment on a merge session's captured output.
// Returns "progressing", "stuck", or "idle".
func (s *patrolState) assessMergeSession(ctx context.Context, sessionName, output string, mr *store.MergeRequest) string {
	// Check account budget before spawning AI callout.
	worldCfg, cfgErr := config.LoadWorldConfig(s.forge.world)
	if cfgErr == nil && len(worldCfg.Budget.Accounts) > 0 {
		assessAccount := account.ResolveAccount("", worldCfg.World.DefaultAccount)
		if assessAccount != "" {
			if err := budget.CheckAccountBudget(s.forge.worldStore, s.forge.sphereStore, assessAccount, worldCfg.Budget); err != nil {
				s.forge.logger.Warn("forge assessment skipped due to budget", "account", assessAccount, "error", err)
				return "progressing" // assume progressing when budget exhausted
			}
		}
	}

	prompt := buildMergeAssessmentPrompt(mr, output, s.pcfg.MonitorInterval)

	assessCtx, cancel := context.WithTimeout(ctx, s.pcfg.AssessTimeout)
	defer cancel()

	parts := strings.Fields(s.pcfg.AssessCommand)
	if len(parts) == 0 {
		return "progressing" // no assess command configured, assume progressing
	}

	cmd := exec.CommandContext(assessCtx, parts[0], parts[1:]...)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Dir = s.forge.worktree

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		s.forge.logger.Warn("merge session AI assessment failed", "error", err)
		return "progressing" // assume progressing on assessment failure
	}

	result := strings.TrimSpace(stdout.String())

	// Try to parse as JSON first.
	var parsed struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err == nil && parsed.Status != "" {
		return normalizeAssessment(parsed.Status)
	}

	// Fall back to raw string matching.
	return normalizeAssessment(result)
}

// normalizeAssessment normalizes an assessment result string.
func normalizeAssessment(s string) string {
	lower := strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(lower, "stuck"):
		return "stuck"
	case strings.Contains(lower, "idle"):
		return "idle"
	case strings.Contains(lower, "progressing"):
		return "progressing"
	default:
		return "progressing" // conservative default
	}
}

// buildMergeAssessmentPrompt builds the AI assessment prompt for a merge session.
func buildMergeAssessmentPrompt(mr *store.MergeRequest, capturedOutput string, monitorInterval time.Duration) string {
	return fmt.Sprintf(`You are monitoring a forge merge session in a multi-agent orchestration
system. The session is executing a merge of branch %q (writ %s).
The session output has not changed for %s. Analyze the output and
determine the session's status.

Session output (last %d lines):
---
%s
---

Respond with ONLY a JSON object (no markdown, no explanation):
{
    "status": "progressing|stuck|idle",
    "reason": "brief explanation"
}

Status meanings:
- "progressing": Session is actively working (e.g., running tests, compiling,
  resolving conflicts). No action needed despite unchanged output.
- "stuck": Session appears confused, looping, or unable to make progress.
- "idle": Session appears to have finished or is not doing anything.`, mr.Branch, mr.WritID, monitorInterval, monitorCaptureLines, capturedOutput)
}

// waitForGitLock waits until index.lock is released or the timeout expires.
// After Stop() kills a session, git lock files may still exist briefly. This
// prevents subsequent git operations from failing with "Unable to create lock file".
func (s *patrolState) waitForGitLock(worktree string, timeout time.Duration) {
	// The forge worktree is created via `git worktree add`, so .git is a file
	// (containing "gitdir: <path>") not a directory. Resolve the actual gitdir
	// to find the correct index.lock path.
	lockPath := resolveGitLockPath(worktree)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(lockPath); os.IsNotExist(err) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	// Best-effort: remove stale lock file if it still exists after timeout.
	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		s.forge.logger.Warn("cleanup: failed to remove stale git lock file", "path", lockPath, "error", err)
	}
}

// resolveGitLockPath returns the path to index.lock for the given worktree.
// For `git worktree add` worktrees, .git is a file containing "gitdir: <path>"
// pointing to the real git directory. For standalone clones, .git is a directory.
func resolveGitLockPath(worktree string) string {
	dotGit := filepath.Join(worktree, ".git")
	info, err := os.Lstat(dotGit)
	if err != nil {
		// Can't stat .git — fall back to the directory assumption.
		return filepath.Join(dotGit, "index.lock")
	}
	if info.IsDir() {
		// Standalone clone — .git is a real directory.
		return filepath.Join(dotGit, "index.lock")
	}
	// .git is a file — read it to extract the gitdir path.
	data, err := os.ReadFile(dotGit)
	if err != nil {
		return filepath.Join(dotGit, "index.lock")
	}
	content := strings.TrimSpace(string(data))
	if !strings.HasPrefix(content, "gitdir: ") {
		return filepath.Join(dotGit, "index.lock")
	}
	gitdir := strings.TrimPrefix(content, "gitdir: ")
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(worktree, gitdir)
	}
	return filepath.Join(gitdir, "index.lock")
}

// verifyCleanWorktree runs git status --porcelain in the worktree and returns
// an error if the worktree is dirty.
func (s *patrolState) verifyCleanWorktree(ctx context.Context) error {
	out, err := s.cmd.Run(ctx, s.forge.worktree, "git", "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status --porcelain failed: %w", err)
	}
	status := strings.TrimSpace(string(out))
	if status != "" {
		lines := strings.Split(status, "\n")
		preview := status
		if len(lines) > 10 {
			preview = strings.Join(lines[:10], "\n") + fmt.Sprintf("\n... and %d more files", len(lines)-10)
		}
		return fmt.Errorf("worktree is dirty:\n%s", preview)
	}
	return nil
}

// cleanupSession stops the merge session, resets the worktree to a clean state,
// removes result and injection files, and updates the agent record to idle.
func (s *patrolState) cleanupSession() {
	sessionName := mergeSessionName(s.forge.world)

	// Stop the session if it's still running. Best-effort — log errors as
	// warnings since cleanup must continue regardless.
	if s.forge.sessions != nil {
		if err := s.forge.sessions.Stop(sessionName, true); err != nil {
			if !strings.Contains(err.Error(), "not found") {
				s.forge.logger.Warn("cleanup: failed to stop merge session", "session", sessionName, "error", err)
			}
		}
	}

	// Reset worktree to clean state. A crashed merge session may leave
	// conflict markers, staged changes, or untracked build artifacts.
	// Without this, the next merge session launches into a dirty worktree.
	if s.forge.worktree != "" {
		// Wait for git lock files to be released after Stop() kills the session.
		s.waitForGitLock(s.forge.worktree, 3*time.Second)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Probe worktree health BEFORE attempting reset/clean. A structurally
		// broken worktree (missing .git, stale gitdir, etc.) returns exit 128
		// on every git op, which the previous implementation misattributed as
		// "worktree dirty" and escalated — even though the correct response
		// is to recreate the worktree from source repo + origin/{targetBranch}.
		if !s.worktreeStructurallySound(ctx) {
			s.forge.logger.Warn("cleanup: forge worktree structurally broken; attempting recreation",
				"world", s.forge.world, "worktree", s.forge.worktree)
			s.fl.Log("RECOVER", "forge worktree structurally broken; attempting recreation")

			recover := s.recoverWorktree
			if recover == nil {
				recover = s.forge.EnsureWorktree
			}
			if err := recover(); err != nil {
				s.forge.logger.Error("cleanup: failed to recreate broken forge worktree",
					"world", s.forge.world, "error", err)
				s.fl.Log("ERROR", fmt.Sprintf("cleanup: forge worktree structurally broken; recreation failed: %s",
					truncate(err.Error(), 200)))
				s.escalateBrokenWorktree(err)
			} else {
				s.forge.logger.Info("forge worktree recreated after structural failure",
					"world", s.forge.world)
				s.fl.Log("RECOVER", "forge worktree recreated after structural failure")
			}
		} else {
			// Fetch origin so we can advance HEAD to the latest target branch state.
			if _, err := s.cmd.Run(ctx, s.forge.worktree, "git", "fetch", "origin"); err != nil {
				s.forge.logger.Warn("cleanup: git fetch origin failed", "error", err)
			}

			// Reset to origin/{target} to both clean the worktree AND advance HEAD
			// so the next session starts from the latest target branch state.
			targetRef := fmt.Sprintf("origin/%s", s.forge.cfg.TargetBranch)
			if _, err := s.cmd.Run(ctx, s.forge.worktree, "git", "reset", "--hard", targetRef); err != nil {
				s.forge.logger.Warn("cleanup: git reset --hard failed", "error", err)
			}
			if _, err := s.cmd.Run(ctx, s.forge.worktree, "git", "clean", "-fd"); err != nil {
				s.forge.logger.Warn("cleanup: git clean -fd failed", "error", err)
			}

			// Verify worktree is actually clean after reset+clean.
			if err := s.verifyCleanWorktree(ctx); err != nil {
				s.forge.logger.Error("cleanup: worktree still dirty after reset+clean — pausing forge", "error", err)
				s.fl.Log("ERROR", fmt.Sprintf("cleanup: worktree still dirty after reset+clean: %s", truncate(err.Error(), 200)))
				s.escalateDirtyWorktree(err)
			}
		}
	}

	// Remove result file.
	resultPath := filepath.Join(s.forge.worktree, resultFileName)
	os.Remove(resultPath)

	// Remove injection file.
	CleanInjectionFile(s.forge.worktree)

	// Best-effort: update agent record to idle.
	s.cleanupAgentRecord()
}

// escalateDirtyWorktree pauses the forge and creates a high-severity escalation
// when the worktree cannot be cleaned after a session. Without pausing, the
// next runMergeSession pre-flight will fail every queued MR for the same
// reason, burning MaxAttempts on each within minutes (CF-M10). Operators must
// manually reset the worktree and resume the forge before merges proceed.
//
// Best-effort: pause and escalation errors are logged but not propagated, since
// the surrounding cleanupSession is itself a best-effort path.
func (s *patrolState) escalateDirtyWorktree(cause error) {
	// Pause the forge so subsequent patrols skip merge attempts.
	if err := SetForgePaused(s.forge.world); err != nil {
		s.forge.logger.Error("cleanup: failed to pause forge after dirty worktree", "error", err)
	} else {
		s.fl.Log("PAUSED", "forge paused due to dirty worktree; run `sol forge resume` after manual reset")
	}

	if s.forge.sphereStore == nil {
		return
	}

	// Create escalation. Use a stable source_ref so duplicate dirty-worktree
	// scenarios in the same world coalesce around a single open escalation
	// (operators won't see a flood if the issue persists across restarts).
	source := s.forge.world + "/forge"
	sourceRef := "forge:" + s.forge.world + ":dirty-worktree"
	description := fmt.Sprintf(
		"forge worktree is dirty after session cleanup; manual reset required before next merge attempt: %s",
		truncate(cause.Error(), 400),
	)

	// Skip if an unresolved dirty-worktree escalation already exists for this
	// world. ListEscalationsBySourceRef returns only open escalations.
	if existing, err := s.forge.sphereStore.ListEscalationsBySourceRef(sourceRef); err == nil && len(existing) > 0 {
		return
	}

	if _, err := s.forge.sphereStore.CreateEscalation("high", source, description, sourceRef); err != nil {
		s.forge.logger.Error("cleanup: failed to create dirty-worktree escalation", "error", err)
	}
}

// worktreeStructurallySound reports whether the forge worktree is a usable git
// worktree. It checks two things:
//  1. The .git entry (file or directory) exists at the worktree root.
//  2. `git rev-parse --is-inside-work-tree` succeeds in the worktree.
//
// A structurally broken worktree (dir inode replaced, .git deleted, stale
// gitdir pointer, etc.) must not be treated as "dirty" — reset/clean cannot
// recover from it, and the correct response is to recreate the worktree from
// the source repo.
func (s *patrolState) worktreeStructurallySound(ctx context.Context) bool {
	if s.forge.worktree == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(s.forge.worktree, ".git")); err != nil {
		return false
	}
	if _, err := s.cmd.Run(ctx, s.forge.worktree, "git", "rev-parse", "--is-inside-work-tree"); err != nil {
		return false
	}
	return true
}

// escalateBrokenWorktree pauses the forge and raises a high-severity
// escalation when the worktree is structurally broken AND recreation via
// EnsureWorktree failed. The source_ref is intentionally distinct from the
// dirty-worktree escalation so operators can tell the two classes of
// problem apart:
//   - forge:<world>:dirty-worktree  → reset/clean could not restore a valid
//     worktree (permissions, stuck locks, git hooks re-creating files)
//   - forge:<world>:broken-worktree → worktree is not a valid git worktree
//     and recreation from source repo also failed
//
// Best-effort: pause and escalation errors are logged but not propagated.
func (s *patrolState) escalateBrokenWorktree(cause error) {
	if err := SetForgePaused(s.forge.world); err != nil {
		s.forge.logger.Error("cleanup: failed to pause forge after broken worktree", "error", err)
	} else {
		s.fl.Log("PAUSED", "forge paused due to broken worktree; recreation failed")
	}

	if s.forge.sphereStore == nil {
		return
	}

	source := s.forge.world + "/forge"
	sourceRef := "forge:" + s.forge.world + ":broken-worktree"
	description := fmt.Sprintf(
		"forge worktree structurally broken; recreation failed: %s",
		truncate(cause.Error(), 400),
	)

	if existing, err := s.forge.sphereStore.ListEscalationsBySourceRef(sourceRef); err == nil && len(existing) > 0 {
		return
	}

	if _, err := s.forge.sphereStore.CreateEscalation("high", source, description, sourceRef); err != nil {
		s.forge.logger.Error("cleanup: failed to create broken-worktree escalation", "error", err)
	}
}

// cleanupAgentRecord updates the forge-merge agent record to idle state.
// Best-effort — errors are logged but not propagated since cleanup is
// non-critical and supervisors already filter out forge-merge agents.
func (s *patrolState) cleanupAgentRecord() {
	if s.forge.sphereStore == nil {
		return
	}
	agentID := s.forge.world + "/forge-merge"
	if err := s.forge.sphereStore.UpdateAgentState(agentID, store.AgentIdle, ""); err != nil {
		// Not found is fine — record may not exist if Launch was never called.
		s.forge.logger.Debug("cleanup: failed to update agent record", "error", err)
	}
}

// actOnResult maps a ForgeResult to existing toolbox operations.
func (s *patrolState) actOnResult(ctx context.Context, mr *store.MergeRequest, result *ForgeResult, queueDepth int) {
	switch result.Result {
	case "merged":
		if result.NoOp {
			s.fl.Log("NO-OP", fmt.Sprintf("%s  %s", mr.ID, truncate(result.Summary, 200)))

			// Validate no-op claim BEFORE any state mutation. A legitimate
			// no-op merge means the agent never produced a new commit — the
			// outpost branch tip should still be reachable from target. Use
			// the ancestor check (NOT writ-id grep) here because in a no-op
			// the work landed under another writ ID, so the current writ ID
			// will not appear on target. The branch-tip ancestor signal is
			// the only one that survives the legitimate no-op case.
			ancestor, ancestorErr := s.forge.isBranchAncestorOfTarget(mr.Branch)
			switch {
			case errors.Is(ancestorErr, errBranchMissing):
				// Remote branch is gone — consistent with "already merged
				// and cleaned up". Allow MarkMergedNoOp to proceed.
			case ancestorErr != nil:
				s.fl.Log("ERROR", fmt.Sprintf("no-op merged ancestor check errored for %s: %s", mr.Branch, truncate(ancestorErr.Error(), 200)))
				s.lastError = truncate(fmt.Sprintf("no-op merged ancestor check failed: %s", ancestorErr.Error()), 200)
				if err := s.forge.MarkFailed(mr.ID); err != nil {
					s.forge.logger.Error("mark-failed after no-op ancestor check error", "mr", mr.ID, "error", err)
				}
				s.writeHeartbeat("idle", queueDepth-1)
				s.emitPatrolEvent(queueDepth)
				return
			case !ancestor:
				s.fl.Log("ERROR", fmt.Sprintf("no-op merged claim rejected for %s: branch tip not in target %s history", mr.Branch, s.forge.cfg.TargetBranch))
				s.lastError = truncate(fmt.Sprintf("no-op merged claim rejected: branch %s tip not in target history", mr.Branch), 200)
				if err := s.forge.MarkFailed(mr.ID); err != nil {
					s.forge.logger.Error("mark-failed after rejected no-op claim", "mr", mr.ID, "error", err)
				}
				s.writeHeartbeat("idle", queueDepth-1)
				s.emitPatrolEvent(queueDepth)
				return
			}
		}

		// Verify push landed by checking remote HEAD — skip for no-op merges
		// where there was no commit and no push.
		if !result.NoOp {
			vr := s.verifyPush(ctx, mr)
			if vr.landed {
				// Confirmed landed — advance the managed repo's local ref
				// BEFORE any other state mutation. "We pushed" and "we
				// advanced our local ref to match" must be coupled to the
				// same authoritative signal, regardless of which verification
				// path (source/ls-remote) produced it. Advancing before
				// MarkMerged also means a crash between the two leaves the
				// managed repo pointing at the newly-landed commit — stale
				// refs strictly cause fewer surprises than an advanced ref
				// without a corresponding MarkMerged.
				s.updateSourceRepo(ctx)
				if vr.err != nil {
					// Reserved for post-confirmation cleanup errors: the
					// commit is on origin but some cleanup step reported a
					// problem. Log and proceed — the merge itself succeeded.
					s.forge.logger.Warn("push verification landed with non-fatal error",
						"mr", mr.ID, "via", vr.via, "error", vr.err)
				}
			} else {
				// If the context was cancelled (e.g. sol down / SIGTERM), don't mark
				// a potentially-successful merge as failed. Release the MR so the
				// next startup retries verification instead of re-dispatching work.
				if ctx.Err() != nil {
					s.fl.Log("SHUTDOWN", fmt.Sprintf("verification deferred for %s: context cancelled during push verification", mr.Branch))
					if _, err := s.forge.Release(mr.ID); err != nil {
						s.forge.logger.Error("release after cancelled verify failed", "mr", mr.ID, "error", err)
					}
					return
				}
				errStr := "push not confirmed on remote"
				if vr.err != nil {
					errStr = vr.err.Error()
				}
				s.fl.Log("ERROR", fmt.Sprintf("push verification failed for %s: %s", mr.Branch, truncate(errStr, 200)))
				s.lastError = truncate(fmt.Sprintf("push verification failed: %s", errStr), 200)
				// MarkFailed instead of Release — retry destroys the good result
				// (CleanForgeResult runs on session start) and produces an empty diff
				// if the push actually succeeded.
				if err := s.forge.MarkFailed(mr.ID); err != nil {
					s.forge.logger.Error("mark-failed after verify failure", "mr", mr.ID, "error", err)
				}
				s.writeHeartbeat("idle", queueDepth-1)
				s.emitPatrolEvent(queueDepth)
				return
			}
		}

		var markErr error
		if result.NoOp {
			markErr = s.forge.MarkMergedNoOp(mr.ID)
		} else {
			markErr = s.forge.MarkMerged(mr.ID)
		}
		if markErr != nil {
			s.forge.logger.Error("mark-merged failed", "mr", mr.ID, "error", markErr)
		} else {
			s.mergesTotal++
			s.lastMerge = time.Now()
			s.lastError = ""
			s.fl.Log("MERGED", fmt.Sprintf("%s  %s", mr.ID, truncate(result.Summary, 200)))
			if s.eventLog != nil {
				s.eventLog.Emit(events.EventMerged, "forge", "forge", "both", map[string]string{
					"merge_request_id": mr.ID,
				})
			}
		}
		// For no-op merges there was no push to verify and therefore no
		// earlier updateSourceRepo call — refresh the managed repo here so
		// subsequent casts still branch from current target. Normal merges
		// already updated the managed repo immediately after the verified
		// push confirmation, so no second call is needed for that path.
		if result.NoOp {
			s.updateSourceRepo(ctx)
		}
		s.writeHeartbeat("idle", queueDepth-1)
		s.emitPatrolEvent(queueDepth)

	case "failed":
		s.fl.Log("FAILED", fmt.Sprintf("%s  %s", mr.ID, truncate(result.Summary, 200)))
		s.lastError = truncate(fmt.Sprintf("merge failed: %s", result.Summary), 200)
		if err := s.forge.MarkFailed(mr.ID); err != nil {
			s.forge.logger.Error("mark-failed failed", "mr", mr.ID, "error", err)
		}
		if s.eventLog != nil {
			s.eventLog.Emit(events.EventMergeFailed, "forge", "forge", "both", map[string]string{
				"merge_request_id": mr.ID,
				"writ_id":          mr.WritID,
				"reason":           truncate(result.Summary, 500),
			})
		}
		s.writeHeartbeat("idle", queueDepth-1)
		s.emitPatrolEvent(queueDepth)

	case "conflict":
		s.fl.Log("CONFLICT", fmt.Sprintf("%s  %s", mr.Branch, truncate(result.Summary, 200)))
		s.lastError = truncate(fmt.Sprintf("merge conflict: %s", mr.Branch), 200)

		// Bound resolution task cascade: cap at maxResolutionTasks per MR.
		// Without this, each conflict→resolution→conflict cycle creates a new
		// resolution task with no limit.
		if mr.ResolutionCount >= maxResolutionTasks {
			s.forge.logger.Error("max resolution tasks reached, marking MR failed",
				"mr", mr.ID, "resolution_count", mr.ResolutionCount)
			if markErr := s.forge.MarkFailed(mr.ID); markErr != nil {
				s.forge.logger.Error("mark-failed after max resolutions failed", "mr", mr.ID, "error", markErr)
			}
		} else if err := s.forge.worldStore.IncrementMRResolutionCount(mr.ID); err != nil {
			s.forge.logger.Error("increment resolution count failed, marking MR failed", "mr", mr.ID, "error", err)
			if markErr := s.forge.MarkFailed(mr.ID); markErr != nil {
				s.forge.logger.Error("mark-failed after resolution count increment failure", "mr", mr.ID, "error", markErr)
			}
		} else if _, err := s.forge.CreateResolutionTask(mr); err != nil {
			// Resolution task creation failed — do NOT release back to "ready".
			// Without a blocker task the MR would be immediately re-claimed and
			// hit the same conflict again, burning MaxAttempts in a tight loop.
			// Mark it failed so an operator can investigate.
			s.forge.logger.Error("create-resolution failed, marking MR failed", "mr", mr.ID, "error", err)
			if markErr := s.forge.MarkFailed(mr.ID); markErr != nil {
				s.forge.logger.Error("mark-failed after resolution task failure failed", "mr", mr.ID, "error", markErr)
			}
		}
		// Note: CreateResolutionTask calls BlockMergeRequest which already
		// sets phase=ready and clears claimed_by/claimed_at — no additional
		// UpdateMergeRequestPhase call needed.
		s.writeHeartbeat("idle", queueDepth-1)
		s.emitPatrolEvent(queueDepth)

	default:
		// Unknown result — release for retry.
		s.fl.Log("ERROR", fmt.Sprintf("unknown result %q for %s", result.Result, mr.ID))
		s.lastError = truncate(fmt.Sprintf("unknown result: %s", result.Result), 200)
		if failed, err := s.forge.Release(mr.ID); err != nil {
			s.forge.logger.Error("release failed", "mr", mr.ID, "error", err)
		} else if failed {
			s.fl.Log("FAILED", fmt.Sprintf("marked failed after max attempts: %s", mr.Branch))
		}
		s.writeHeartbeat("idle", queueDepth-1)
		s.emitPatrolEvent(queueDepth)
	}
}

// verifyPushResult is the structured outcome of a push verification attempt.
//
// landed reports whether any authoritative path confirmed that the commit
// is present on origin's target branch. It is the single signal the caller
// must couple "we pushed" to "we advanced our local ref to match" — separate
// from err so unrecoverable errors in cleanup, network blips on secondary
// paths, or post-confirmation bookkeeping failures cannot suppress the
// advance of the managed repo's ref when the push is actually on origin.
//
// via identifies which verification path produced the signal. Values:
//   - "source"    primary fetch + log-grep in the managed source repo
//   - "ls-remote" ls-remote + shallow fetch fallback
//   - "worktree"  reserved for a future direct-worktree verification path
//   - ""          no path produced a confirming signal (landed is false)
//
// err carries the last non-confirming error from tryVerifyPush (or ctx.Err()
// on cancellation). When landed is true the err field is reserved for
// post-confirmation problems; the caller must still honour landed and
// advance the managed repo ref before acting on err.
type verifyPushResult struct {
	landed bool
	via    string
	err    error
}

// verifyPush checks that the merge actually landed on the remote target branch
// by searching for the writ ID in recent commits on the target. This works
// regardless of merge strategy (squash, real merge, rebase) because forge
// always includes the writ ID in the commit message format: {title} ({writ-id}).
//
// Retries up to 3 times with backoff to handle transient network failures
// in the window between push and verification fetch.
//
// Returns a structured verifyPushResult so callers can distinguish
// "authoritatively confirmed on origin" from "some verification path
// errored" — the two must not be conflated. See verifyPushResult for the
// full contract.
func (s *patrolState) verifyPush(ctx context.Context, mr *store.MergeRequest) verifyPushResult {
	const maxAttempts = 3
	const defaultRetryDelay = 5 * time.Second

	retryDelay := s.verifyRetryDelay
	if retryDelay == 0 {
		retryDelay = defaultRetryDelay
	}

	var last verifyPushResult
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		last = s.tryVerifyPush(ctx, mr)
		if last.landed {
			return last
		}
		if attempt < maxAttempts {
			s.forge.logger.Warn("push verification failed, retrying",
				"mr", mr.ID, "attempt", attempt, "error", last.err)
			select {
			case <-ctx.Done():
				return verifyPushResult{landed: false, via: last.via, err: ctx.Err()}
			case <-time.After(retryDelay):
			}
		}
	}
	return last
}

// tryVerifyPush performs a single fetch+grep attempt to verify the merge landed.
//
// Verification runs against sourceRepo (the managed clone), not the forge
// worktree. sourceRepo has the full history and remote config and is never
// touched by merge sessions, so it remains a stable, authoritative view of
// origin even if the forge worktree is structurally broken. If the primary
// fetch against sourceRepo fails, a fallback path attempts `git ls-remote`
// + a targeted shallow fetch so a degraded sourceRepo state still yields an
// authoritative answer against the remote instead of a false-negative
// "push didn't land" error.
//
// The `path` slog attribute on warnings lets operators distinguish
// "push didn't land" from "verifier is broken": values are
// "source" (primary), "ls-remote" (fallback), or "source-empty" (misconfig).
func (s *patrolState) tryVerifyPush(ctx context.Context, mr *store.MergeRequest) verifyPushResult {
	targetBranch := s.forge.cfg.TargetBranch
	targetRef := fmt.Sprintf("origin/%s", targetBranch)

	sourceRepo := s.forge.sourceRepo
	if sourceRepo == "" {
		s.forge.logger.Warn("verifyPush: source repo not configured; cannot verify push",
			"path", "source-empty", "mr", mr.ID)
		return verifyPushResult{
			landed: false,
			via:    "",
			err:    fmt.Errorf("source repo not configured; cannot verify push"),
		}
	}

	path := "source"
	searchRef := targetRef

	// Primary: fetch origin in sourceRepo to refresh origin/{targetBranch}.
	if _, err := s.cmd.Run(ctx, sourceRepo, "git", "fetch", "origin"); err != nil {
		s.forge.logger.Warn("verifyPush: git fetch against source repo failed; attempting ls-remote fallback",
			"path", path, "mr", mr.ID, "error", err)

		// Fallback: ls-remote + targeted shallow fetch of the remote HEAD.
		lsOut, lsErr := s.cmd.Run(ctx, sourceRepo, "git", "ls-remote", "origin",
			"refs/heads/"+targetBranch)
		if lsErr != nil {
			s.forge.logger.Warn("verifyPush: ls-remote fallback failed",
				"path", "ls-remote", "mr", mr.ID, "error", lsErr)
			return verifyPushResult{
				landed: false,
				via:    "ls-remote",
				err: fmt.Errorf("git fetch failed during push verification: %w (ls-remote fallback: %v)",
					err, lsErr),
			}
		}
		remoteHead := parseLsRemoteHead(lsOut)
		if remoteHead == "" {
			s.forge.logger.Warn("verifyPush: ls-remote returned empty result",
				"path", "ls-remote", "mr", mr.ID, "branch", targetBranch)
			return verifyPushResult{
				landed: false,
				via:    "ls-remote",
				err:    fmt.Errorf("git fetch failed during push verification and ls-remote returned empty result: %w", err),
			}
		}
		// Shallow fetch the specific commit so we can log-grep it locally.
		if _, fetchErr := s.cmd.Run(ctx, sourceRepo, "git", "fetch", "--depth=200",
			"origin", remoteHead); fetchErr != nil {
			s.forge.logger.Warn("verifyPush: shallow fetch of remote HEAD failed",
				"path", "ls-remote", "mr", mr.ID, "commit", remoteHead, "error", fetchErr)
			return verifyPushResult{
				landed: false,
				via:    "ls-remote",
				err: fmt.Errorf("git fetch failed during push verification and shallow fetch fallback failed: %w (shallow fetch: %v)",
					err, fetchErr),
			}
		}
		// Use the discovered commit as the search tip so the ref-range query
		// below resolves even though origin/{targetBranch} was not refreshed.
		searchRef = remoteHead
		path = "ls-remote"
	}

	// Search for the writ ID in commits that appeared after the pre-merge ref.
	// Using a ref range (preMergeRef..searchRef) is clock-independent — it
	// finds exactly the commits introduced since we captured the baseline
	// before the merge session started. If preMergeRef is empty (capture
	// failed), fall back to searching the last 200 commits on the target.
	var out []byte
	var err error
	if s.preMergeRef != "" {
		refRange := fmt.Sprintf("%s..%s", s.preMergeRef, searchRef)
		out, err = s.cmd.Run(ctx, sourceRepo, "git", "log", refRange, "--oneline", "--grep", mr.WritID)
	} else {
		out, err = s.cmd.Run(ctx, sourceRepo, "git", "log", searchRef, "-200", "--oneline", "--grep", mr.WritID)
	}
	if err != nil {
		s.forge.logger.Warn("verifyPush: git log grep check failed",
			"path", path, "mr", mr.ID, "error", err)
		return verifyPushResult{
			landed: false,
			via:    path,
			err:    fmt.Errorf("git log grep check failed: %w", err),
		}
	}

	if len(strings.TrimSpace(string(out))) == 0 {
		s.forge.logger.Warn("verifyPush: writ not found in target branch commits",
			"path", path, "mr", mr.ID, "writ", mr.WritID, "target", searchRef)
		return verifyPushResult{
			landed: false,
			via:    path,
			err:    fmt.Errorf("writ %s not found in commits on %s", mr.WritID, searchRef),
		}
	}

	return verifyPushResult{landed: true, via: path, err: nil}
}

// parseLsRemoteHead parses the first line of `git ls-remote` output and
// returns the commit hash, or "" if the output is empty or malformed.
// ls-remote lines are of the form "<sha>\t<ref>".
func parseLsRemoteHead(out []byte) string {
	line := strings.TrimSpace(string(out))
	if line == "" {
		return ""
	}
	// Take the first line only in case multiple refs came back.
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	fields := strings.SplitN(line, "\t", 2)
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSpace(fields[0])
}

// updateSourceRepo fetches origin in the managed repo and advances the local
// branch to match origin/{targetBranch} after a successful merge push. This
// ensures subsequent casts branch from the post-merge code rather than a stale
// ref. Best-effort: errors are logged but not propagated since the merge itself
// already succeeded.
//
// git update-ref is used to advance the local branch — it is a pure ref
// operation that does not require a working tree checkout or clean state, which
// makes it safe for the managed repo (a read-only research copy with no local
// state worth preserving).
func (s *patrolState) updateSourceRepo(ctx context.Context) {
	sourceRepo := s.forge.sourceRepo
	if sourceRepo == "" {
		return
	}
	targetBranch := s.forge.cfg.TargetBranch
	if _, err := s.cmd.Run(ctx, sourceRepo, "git", "fetch", "origin", targetBranch); err != nil {
		s.forge.logger.Warn("failed to fetch managed repo after merge",
			"repo", sourceRepo, "error", err)
		return
	}
	// Advance the local branch so HEAD points to the same commit as
	// origin/{targetBranch}. Without this, refs/heads/{targetBranch} stays at
	// whatever commit it was at during the last explicit sol world sync, and
	// subsequent casts would branch from stale code.
	localRef := fmt.Sprintf("refs/heads/%s", targetBranch)
	remoteRef := fmt.Sprintf("origin/%s", targetBranch)
	if _, err := s.cmd.Run(ctx, sourceRepo, "git", "update-ref", localRef, remoteRef); err != nil {
		s.forge.logger.Warn("failed to advance local branch in managed repo after merge",
			"repo", sourceRepo, "branch", targetBranch, "error", err)
		return
	}
	s.fl.Log("SYNC", fmt.Sprintf("updated managed repo %s ref", targetBranch))
}
