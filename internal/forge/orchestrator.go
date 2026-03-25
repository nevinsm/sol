package forge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
	sessionCrashed                         // session died unexpectedly
	sessionCancelled                       // context was cancelled
)

// monitorCaptureLines is how many tmux lines to capture per check.
const monitorCaptureLines = 80

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
func (s *patrolState) runMergeSession(ctx context.Context, mr *store.MergeRequest) (*ForgeResult, error) {
	if s.forge.sessions == nil {
		return nil, fmt.Errorf("session manager not configured")
	}

	sessionName := mergeSessionName(s.forge.world)
	worktree := s.forge.worktree

	// 1. Stop any leftover session from a prior crash.
	if s.forge.sessions.Exists(sessionName) {
		s.fl.Log("CLEANUP", fmt.Sprintf("stopping leftover merge session %s", sessionName))
		s.forge.sessions.Stop(sessionName, true)
	}

	// Capture the remote target branch HEAD before launching the merge session.
	// The session will push to origin, and verifyPush uses this as the lower
	// bound of a ref-range search (preMergeRef..origin/{targetBranch}) so that
	// push verification is clock-independent and window-independent.
	{
		targetRef := fmt.Sprintf("origin/%s", s.forge.cfg.TargetBranch)
		if out, err := s.cmd.Run(ctx, worktree, "git", "rev-parse", targetRef); err == nil {
			s.preMergeRef = strings.TrimSpace(string(out))
		} else {
			s.forge.logger.Warn("failed to capture pre-merge ref, push verification will search full history", "error", err)
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
	}
	injection := BuildInjection(mr, writ, injectionCfg)

	// 3. Write injection file for PrimeBuilder and PreCompact hook.
	if err := WriteInjectionFile(worktree, injection); err != nil {
		return nil, fmt.Errorf("failed to write injection file: %w", err)
	}

	// 4. Clean stale result file.
	CleanForgeResult(worktree)

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
	outcome := s.monitorSession(ctx, sessionName, mr)

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

	case sessionCrashed:
		// Try to read result in case it was written before crash.
		result, err := ReadResult(s.forge.worktree)
		if err != nil {
			return nil, fmt.Errorf("session crashed and no result file: %w", err)
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
func (s *patrolState) monitorSession(ctx context.Context, sessionName string, mr *store.MergeRequest) sessionOutcome {
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

		// Write heartbeat to show we're still alive.
		s.writeHeartbeatWithMR("working", 0, mr)

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

// cleanupSession stops the merge session, resets the worktree to a clean state,
// removes result and injection files, and updates the agent record to idle.
func (s *patrolState) cleanupSession() {
	sessionName := mergeSessionName(s.forge.world)

	// Stop the session if it's still running.
	if s.forge.sessions != nil && s.forge.sessions.Exists(sessionName) {
		s.forge.sessions.Stop(sessionName, true)
	}

	// Reset worktree to clean state. A crashed merge session may leave
	// conflict markers, staged changes, or untracked build artifacts.
	// Without this, the next merge session launches into a dirty worktree.
	if s.forge.worktree != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := s.cmd.Run(ctx, s.forge.worktree, "git", "reset", "--hard"); err != nil {
			s.forge.logger.Warn("cleanup: git reset --hard failed", "error", err)
		}
		if _, err := s.cmd.Run(ctx, s.forge.worktree, "git", "clean", "-fd"); err != nil {
			s.forge.logger.Warn("cleanup: git clean -fd failed", "error", err)
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
		}

		// Verify push landed by checking remote HEAD — skip for no-op merges
		// where there was no commit and no push.
		if !result.NoOp {
			if err := s.verifyPush(ctx, mr); err != nil {
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
				s.fl.Log("ERROR", fmt.Sprintf("push verification failed for %s: %s", mr.Branch, truncate(err.Error(), 200)))
				s.lastError = truncate(fmt.Sprintf("push verification failed: %s", err.Error()), 200)
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

		if err := s.forge.MarkMerged(mr.ID); err != nil {
			s.forge.logger.Error("mark-merged failed", "mr", mr.ID, "error", err)
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
		// Update managed repo so subsequent casts branch from current main.
		s.updateSourceRepo(ctx)
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
		if _, err := s.forge.CreateResolutionTask(mr); err != nil {
			// Resolution task creation failed — do NOT release back to "ready".
			// Without a blocker task the MR would be immediately re-claimed and
			// hit the same conflict again, burning MaxAttempts in a tight loop.
			// Mark it failed so an operator can investigate.
			s.forge.logger.Error("create-resolution failed, marking MR failed", "mr", mr.ID, "error", err)
			if markErr := s.forge.MarkFailed(mr.ID); markErr != nil {
				s.forge.logger.Error("mark-failed after resolution task failure failed", "mr", mr.ID, "error", markErr)
			}
		} else {
			// Resolution task created and MR is now blocked. Release from
			// "claimed" back to "ready" so it can be re-attempted once the
			// blocker task is resolved.
			if err := s.forge.worldStore.UpdateMergeRequestPhase(mr.ID, "ready"); err != nil {
				s.forge.logger.Error("release-conflict-claim failed", "mr", mr.ID, "error", err)
			}
		}
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

// verifyPush checks that the merge actually landed on the remote target branch
// by searching for the writ ID in recent commits on the target. This works
// regardless of merge strategy (squash, real merge, rebase) because forge
// always includes the writ ID in the commit message format: {title} ({writ-id}).
//
// Retries up to 3 times with backoff to handle transient network failures
// in the window between push and verification fetch.
func (s *patrolState) verifyPush(ctx context.Context, mr *store.MergeRequest) error {
	const maxAttempts = 3
	const defaultRetryDelay = 5 * time.Second

	retryDelay := s.verifyRetryDelay
	if retryDelay == 0 {
		retryDelay = defaultRetryDelay
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastErr = s.tryVerifyPush(ctx, mr)
		if lastErr == nil {
			return nil
		}
		if attempt < maxAttempts {
			s.forge.logger.Warn("push verification failed, retrying",
				"mr", mr.ID, "attempt", attempt, "error", lastErr)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryDelay):
			}
		}
	}
	return lastErr
}

// tryVerifyPush performs a single fetch+grep attempt to verify the merge landed.
func (s *patrolState) tryVerifyPush(ctx context.Context, mr *store.MergeRequest) error {
	worktree := s.forge.worktree
	targetRef := fmt.Sprintf("origin/%s", s.forge.cfg.TargetBranch)

	// Fetch to get latest remote state.
	if _, err := s.cmd.Run(ctx, worktree, "git", "fetch", "origin"); err != nil {
		return fmt.Errorf("git fetch failed during push verification: %w", err)
	}

	// Search for the writ ID in commits that appeared after the pre-merge ref.
	// Using a ref range (preMergeRef..origin/{targetBranch}) is clock-independent —
	// it finds exactly the commits introduced since we captured the baseline before
	// the merge session started. If preMergeRef is empty (capture failed), fall back
	// to searching all commits on the target branch.
	var out []byte
	var err error
	if s.preMergeRef != "" {
		refRange := fmt.Sprintf("%s..%s", s.preMergeRef, targetRef)
		out, err = s.cmd.Run(ctx, worktree, "git", "log", refRange, "--oneline", "--grep", mr.WritID)
	} else {
		// No pre-merge ref available — limit search to the last 50 commits to
		// avoid false positives from historical writ mentions in older commits.
		// 50 is generous for a single merge push while narrow enough to exclude
		// ancient history where the same writ ID may appear in unrelated commits.
		out, err = s.cmd.Run(ctx, worktree, "git", "log", targetRef, "-50", "--oneline", "--grep", mr.WritID)
	}
	if err != nil {
		return fmt.Errorf("git log grep check failed: %w", err)
	}

	if len(strings.TrimSpace(string(out))) == 0 {
		return fmt.Errorf("writ %s not found in commits on %s", mr.WritID, targetRef)
	}

	return nil
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
