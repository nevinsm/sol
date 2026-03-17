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
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return sessionCancelled

		case <-ticker.C:
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
}

// assessMergeSession runs AI assessment on a merge session's captured output.
// Returns "progressing", "stuck", or "idle".
func (s *patrolState) assessMergeSession(ctx context.Context, sessionName, output string, mr *store.MergeRequest) string {
	prompt := buildMergeAssessmentPrompt(mr, output)

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
func buildMergeAssessmentPrompt(mr *store.MergeRequest, capturedOutput string) string {
	return fmt.Sprintf(`You are monitoring a forge merge session in a multi-agent orchestration
system. The session is executing a merge of branch %q (writ %s).
The session output has not changed for 3 minutes. Analyze the output and
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
- "idle": Session appears to have finished or is not doing anything.`, mr.Branch, mr.WritID, monitorCaptureLines, capturedOutput)
}

// cleanupSession stops the merge session, removes result and injection files,
// and updates the agent record to idle.
func (s *patrolState) cleanupSession() {
	sessionName := mergeSessionName(s.forge.world)

	// Stop the session if it's still running.
	if s.forge.sessions != nil && s.forge.sessions.Exists(sessionName) {
		s.forge.sessions.Stop(sessionName, true)
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
	agentID := s.forge.world + "/forge-merge"
	sphereStore, err := store.OpenSphere()
	if err != nil {
		s.forge.logger.Warn("cleanup: failed to open sphere store for agent record", "error", err)
		return
	}
	defer sphereStore.Close()

	if err := sphereStore.UpdateAgentState(agentID, store.AgentIdle, ""); err != nil {
		// Not found is fine — record may not exist if Launch was never called.
		s.forge.logger.Debug("cleanup: failed to update agent record", "error", err)
	}
}

// actOnResult maps a ForgeResult to existing toolbox operations.
func (s *patrolState) actOnResult(ctx context.Context, mr *store.MergeRequest, result *ForgeResult, queueDepth int) {
	switch result.Result {
	case "merged":
		// Verify push landed by checking remote HEAD.
		if err := s.verifyPush(ctx, mr); err != nil {
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
			s.forge.logger.Error("create-resolution failed", "mr", mr.ID, "error", err)
		}
		if err := s.forge.worldStore.UpdateMergeRequestPhase(mr.ID, "ready"); err != nil {
			s.forge.logger.Error("release-conflict-claim failed", "mr", mr.ID, "error", err)
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

	// Search for the writ ID in recent commits on the target branch.
	out, err := s.cmd.Run(ctx, worktree, "git", "log", targetRef, "--oneline", "-5", "--grep", mr.WritID)
	if err != nil {
		return fmt.Errorf("git log grep check failed: %w", err)
	}

	if len(strings.TrimSpace(string(out))) == 0 {
		return fmt.Errorf("writ %s not found in recent commits on %s", mr.WritID, targetRef)
	}

	return nil
}

// updateSourceRepo fetches origin in the managed repo so its local main ref
// tracks the remote after a successful merge push. This ensures subsequent
// casts branch from the updated main rather than a stale ref. Best-effort:
// errors are logged but not propagated since the merge itself already succeeded.
func (s *patrolState) updateSourceRepo(ctx context.Context) {
	sourceRepo := s.forge.sourceRepo
	if sourceRepo == "" {
		return
	}
	targetBranch := s.forge.cfg.TargetBranch
	if _, err := s.cmd.Run(ctx, sourceRepo, "git", "fetch", "origin", targetBranch); err != nil {
		s.forge.logger.Warn("failed to update managed repo after merge",
			"repo", sourceRepo, "error", err)
	} else {
		s.fl.Log("SYNC", fmt.Sprintf("updated managed repo %s ref", targetBranch))
	}
}
