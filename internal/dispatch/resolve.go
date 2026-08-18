package dispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/runtime"
	"github.com/nevinsm/sol/internal/runtime/loader"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/flock"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/resolutionreport"
	"github.com/nevinsm/sol/internal/softfail"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// resolveCleanupMarkerPath returns the path to the marker file written
// before mgr.Stop during outpost cleanup. It mirrors the marker-before-cycle
// pattern in handoff.Exec (handoff.go:636-642): the marker lands on disk
// BEFORE the destructive op so a fallback reaper can observe that resolve
// cleanup is in flight if the calling process gets killed mid-cleanup.
//
// The marker is a sibling of the resolve lock files (.resolve_in_progress,
// .resolve_in_progress.<writ>) under the agent directory. Consul currently
// reaps stale agent dirs wholesale, so a leftover marker is harmless.
func resolveCleanupMarkerPath(world, agentName, role string) string {
	return filepath.Join(config.AgentDir(world, agentName, role), ".resolve_cleanup_in_progress")
}

// runResolveAddCommit performs the add+commit step of resolve. It distinguishes
// "nothing to commit" (clean tree, benign no-op) from real commit failures
// (hook rejection, lock contention, malformed config) — the previous
// CombinedOutput()-and-discard pattern masked both alike (L-L4).
//
// Behavior:
//   - git add -A — failure returns an error wrapping the output.
//   - git diff --cached --quiet — exit 0 means no staged changes; we skip
//     commit silently. Any other exit means the index has staged changes
//     (or the diff command itself errored) and we proceed to commit.
//   - git commit — any non-zero exit is a hard failure. The error is
//     emitted as a structured soft_failure event for cross-domain
//     observability and returned to the caller so the writ stays tethered
//     instead of flipping to done with a missing commit.
func runResolveAddCommit(ctx context.Context, worktreeDir, commitMsg, authorName, authorEmail string,
	logger *events.Logger, eventPayload map[string]string) error {
	// git add -A — surface failures (permission denied, repo corrupt, etc.).
	addCtx, addCancel := context.WithTimeout(ctx, GitLocalOpTimeout)
	defer addCancel()
	addCmd := exec.CommandContext(addCtx, "git", "-C", worktreeDir, "add", "-A")
	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// git diff --cached --quiet: exit 0 = no staged changes (no-op); skip commit.
	// Any non-zero exit (including 1 = staged changes, or other = diff error)
	// proceeds to commit so a malformed index is surfaced by `git commit` itself
	// rather than swallowed here.
	diffCtx, diffCancel := context.WithTimeout(ctx, GitLocalOpTimeout)
	defer diffCancel()
	diffCmd := exec.CommandContext(diffCtx, "git", "-C", worktreeDir, "diff", "--cached", "--quiet")
	if err := diffCmd.Run(); err == nil {
		// Clean tree — nothing to commit. Succeed silently.
		return nil
	}

	// Staged changes present — commit them. Any error here is a real failure.
	commitCtx, commitCancel := context.WithTimeout(ctx, GitLocalOpTimeout)
	defer commitCancel()
	commitCmd := exec.CommandContext(commitCtx, "git", "-C", worktreeDir, "commit", "-m", commitMsg)
	commitCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME="+authorName,
		"GIT_AUTHOR_EMAIL="+authorEmail,
	)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		commitErr := fmt.Errorf("git commit failed: %s: %w", strings.TrimSpace(string(out)), err)
		slog.Warn("resolve: git commit failed", "error", commitErr)
		if logger != nil {
			payload := make(map[string]string, len(eventPayload)+2)
			maps.Copy(payload, eventPayload)
			payload["op"] = "dispatch.resolve.git_commit"
			payload["error"] = commitErr.Error()
			logger.Emit(events.EventSoftFailure, "dispatch", "resolve", "audit", payload)
		}
		return commitErr
	}
	return nil
}

// cleanupOutpostConfigDir invokes runtime.CleanupConfigDir for an outpost agent.
// Best-effort: logs warnings but never fails resolve.
//
// We resolve the runtime via the world config (the agent record has no Runtime
// field). If the configured runtime is unknown, we fall back to cleaning up
// via every known runtime — CleanupConfigDir is idempotent so the fallback is safe.
func cleanupOutpostConfigDir(world, role, agentName string) {
	worldDir := config.WorldDir(world)
	worldCfg, err := config.LoadWorldConfig(world)
	if err == nil {
		runtimeName := worldCfg.ResolveRuntime(role)
		if r, getErr := loader.Get(runtimeName); getErr == nil {
			if cleanupErr := runtime.CleanupConfigDir(r.Descriptor(), worldDir, role, agentName); cleanupErr != nil {
				slog.Warn("resolve: failed to clean up runtime config dir",
					"agent", agentName, "runtime", runtimeName, "error", cleanupErr)
			}
			return
		}
	}
	// Fallback: clean up via every known runtime (idempotent).
	for name, r := range loader.All() {
		if cleanupErr := runtime.CleanupConfigDir(r.Descriptor(), worldDir, role, agentName); cleanupErr != nil {
			slog.Warn("resolve: failed to clean up runtime config dir (fallback)",
				"agent", agentName, "runtime", name, "error", cleanupErr)
		}
	}
}

// cleanupWorktree removes a git worktree and prunes stale references.
// Best-effort: logs what was cleaned up but does not fail.
// Uses its own background context since it may run in a goroutine after
// the parent context has been cancelled.
func cleanupWorktree(world, worktreeDir string) {
	repoPath := config.RepoPath(world)

	rmCtx, rmCancel := context.WithTimeout(context.Background(), GitWorktreeRemoveTimeout)
	defer rmCancel()
	rmCmd := exec.CommandContext(rmCtx, "git", "-C", repoPath, "worktree", "remove", "--force", worktreeDir)
	if out, err := rmCmd.CombinedOutput(); err != nil {
		slog.Warn("resolve: worktree remove failed", "output", strings.TrimSpace(string(out)), "error", err)
		// Fallback: remove directory directly (matches cast cleanup pattern).
		if removeErr := os.RemoveAll(worktreeDir); removeErr != nil {
			slog.Warn("resolve: failed to remove worktree dir", "dir", worktreeDir, "error", removeErr)
			return
		}
	}
	slog.Debug("resolve: cleaned up worktree", "dir", worktreeDir)

	pruneCtx, pruneCancel := context.WithTimeout(context.Background(), GitLocalOpTimeout)
	defer pruneCancel()
	pruneCmd := exec.CommandContext(pruneCtx, "git", "-C", repoPath, "worktree", "prune")
	if out, err := pruneCmd.CombinedOutput(); err != nil {
		slog.Warn("resolve: worktree prune failed", "output", strings.TrimSpace(string(out)), "error", err)
	}
}

// resolutionReportFilename is the convention-defined name an agent writes at
// the worktree root before resolving: a structured report of deviations,
// assumptions, and surprises that the diff and commit message alone don't
// capture (see docs/conventions — resolution report capture path).
const resolutionReportFilename = ".resolution.md"

// isTrackedByGit reports whether relPath is tracked by git within
// worktreeDir, via `git ls-files --error-unmatch`. Exit 0 means tracked;
// exit 1 means "did not match any tracked file" (the expected common case
// for an agent-authored report — the worktree exclude list keeps
// .resolution.md out of git entirely). Any other failure (git missing,
// worktreeDir not a repo, timeout) is returned as an error so the caller
// can log it distinctly rather than silently treating "couldn't tell" the
// same as "confirmed untracked".
func isTrackedByGit(ctx context.Context, worktreeDir, relPath string) (bool, error) {
	lsCtx, cancel := context.WithTimeout(ctx, GitLocalOpTimeout)
	defer cancel()
	cmd := exec.CommandContext(lsCtx, "git", "-C", worktreeDir, "ls-files", "--error-unmatch", "--", relPath)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git ls-files --error-unmatch failed: %s: %w", strings.TrimSpace(string(out)), err)
}

// captureResolutionReport moves an agent-authored .resolution.md from the
// worktree root to the writ's persistent output directory (as
// resolution.md), if present and not git-tracked. Returns true if a report
// was captured.
//
// A missing file is NOT an error — most writs won't have one. Any failure
// moving the file (permission error, mkdir failure, cross-device rename,
// etc.) is a soft failure: log + continue, never block resolve. Resolve is
// the durability boundary for the actual work (commit + push already landed
// by the time this runs); losing a report the agent forgot to write, or
// failed to move, must not re-tether a writ that is otherwise done.
//
// A .resolution.md that IS git-tracked is, by definition, never an
// agent-authored report: the worktree exclude list keeps real reports
// untracked, so a tracked one can only be a leaked artifact inherited from
// an earlier writ (e.g. accidentally committed to main and picked up at
// worktree creation — see sol-e39cce0c027506af). Capturing it would
// mis-attribute another writ's report to this one, so it is skipped
// entirely rather than moved.
//
// Runs for both code and non-code writs — both flow through worktreeDir.
//
// logger may be nil (matches Resolve's optional logger parameter). Guard
// nil explicitly before handing it to softfail.Emit: a nil *events.Logger
// boxed into the softfail.EventEmitter interface is a non-nil interface
// value, so softfail.Emit's own nil check would not catch it and the
// subsequent Logger method call would panic on the nil receiver.
func captureResolutionReport(ctx context.Context, world, writID, worktreeDir string, logger *events.Logger) bool {
	srcPath := filepath.Join(worktreeDir, resolutionReportFilename)

	if _, err := os.Stat(srcPath); err != nil {
		if !os.IsNotExist(err) {
			// Not "never written" — something else went wrong reading the
			// worktree (permission denied, etc). Soft-fail, don't block resolve.
			emitResolutionReportFailure(logger, "dispatch.capture_resolution_report_stat", err, writID)
		}
		return false
	}

	tracked, err := isTrackedByGit(ctx, worktreeDir, resolutionReportFilename)
	if err != nil {
		// Could not determine tracked status (git missing, worktree not a
		// repo, timeout). Log distinctly and fall through to capture — the
		// common case (git present, worktree initialized) never reaches
		// here, and refusing to capture on an unrelated git error would
		// silently drop legitimate reports.
		emitResolutionReportFailure(logger, "dispatch.capture_resolution_report_track_check", err, writID)
	} else if tracked {
		emitLeakedArtifactSkipped(logger, worktreeDir, writID)
		return false
	}

	outDir := config.WritOutputDir(world, writID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		emitResolutionReportFailure(logger, "dispatch.capture_resolution_report_mkdir",
			fmt.Errorf("failed to create writ output dir %q: %w", outDir, err), writID)
		return false
	}

	destPath := filepath.Join(outDir, "resolution.md")
	if err := os.Rename(srcPath, destPath); err != nil {
		emitResolutionReportFailure(logger, "dispatch.capture_resolution_report_move",
			fmt.Errorf("failed to move resolution report to %q: %w", destPath, err), writID)
		return false
	}
	return true
}

// emitLeakedArtifactSkipped logs and emits a soft_failure event for a
// .resolution.md found tracked by git in the worktree — see
// captureResolutionReport for why this is always a leaked artifact from an
// earlier writ rather than a real report. Uses the reason
// "leaked_artifact_skipped" (distinct from the generic capture-failure ops
// emitted by emitResolutionReportFailure) so consumers can tell "we chose
// not to capture this" apart from "we tried and failed to capture this".
func emitLeakedArtifactSkipped(logger *events.Logger, worktreeDir, writID string) {
	err := fmt.Errorf("%s is tracked by git in %q; treating as a leaked artifact from an earlier writ and skipping capture",
		resolutionReportFilename, worktreeDir)
	payload := map[string]any{"writ_id": writID, "reason": "leaked_artifact_skipped"}
	if logger != nil {
		softfail.Emit(nil, logger, "dispatch.capture_resolution_report_leaked_artifact", err, payload)
		return
	}
	softfail.Log(nil, "dispatch.capture_resolution_report_leaked_artifact", err)
}

// emitResolutionReportFailure logs a resolution-report capture soft failure,
// emitting a structured soft_failure event when a non-nil logger is
// available. See captureResolutionReport for why the nil check happens here
// rather than being delegated to softfail.Emit.
func emitResolutionReportFailure(logger *events.Logger, op string, err error, writID string) {
	if logger != nil {
		softfail.Emit(nil, logger, op, err, map[string]any{"writ_id": writID})
		return
	}
	softfail.Log(nil, op, err)
}

// routeDurableLessons reads a writ's just-captured resolution report (from
// the writ's output directory, NOT the worktree — must run after
// captureResolutionReport has moved it there) and, if the Durable lessons
// section has real content, mails it to the owner of the writ's caravan (or
// the autarch if the writ is in no caravan).
//
// Best-effort throughout: any failure (report unreadable, caravan lookup
// error, mail send error) is logged via emitResolutionReportFailure and
// never propagates — a missing or unroutable durable lesson must not
// re-tether an otherwise-complete writ.
func routeDurableLessons(sphereStore SphereStore, world, writID, writTitle, senderID string, logger *events.Logger) {
	report, err := resolutionreport.Load(world, writID)
	if err != nil {
		emitResolutionReportFailure(logger, "dispatch.durable_lessons_load", err, writID)
		return
	}
	if report == nil {
		return
	}

	lessons := resolutionreport.Section(report.Content, resolutionreport.SectionDurableLessons)
	if !resolutionreport.HasContent(lessons) {
		return
	}

	recipient, err := resolveDurableLessonRecipient(sphereStore, writID)
	if err != nil {
		emitResolutionReportFailure(logger, "dispatch.durable_lessons_recipient", err, writID)
		return
	}

	subject := fmt.Sprintf("Durable lesson from %s", writID)
	body := fmt.Sprintf("%s\n\nWrit: %s (%s)\nReport: %s", lessons, writTitle, writID, report.Path)

	if _, err := sphereStore.SendMessage(senderID, recipient, subject, body, 2, "notification"); err != nil {
		emitResolutionReportFailure(logger, "dispatch.durable_lessons_send", err, writID)
	}
}

// resolveDurableLessonRecipient returns the mail recipient for a writ's
// durable-lessons routing: the owner of the writ's caravan, or the autarch
// if the writ belongs to no caravan (or its caravan somehow has no owner —
// defensive; CreateCaravan always defaults owner to autarch).
func resolveDurableLessonRecipient(sphereStore SphereStore, writID string) (string, error) {
	items, err := sphereStore.GetCaravanItemsForWrit(writID)
	if err != nil {
		return "", fmt.Errorf("failed to look up caravan membership for writ %q: %w", writID, err)
	}
	if len(items) == 0 {
		return config.Autarch, nil
	}

	caravan, err := sphereStore.GetCaravan(items[0].CaravanID)
	if err != nil {
		return "", fmt.Errorf("failed to look up caravan %q: %w", items[0].CaravanID, err)
	}
	if caravan.Owner == "" {
		return config.Autarch, nil
	}
	return caravan.Owner, nil
}

// ResolveResult holds the output of a resolve operation.
type ResolveResult struct {
	WritID         string
	Title          string
	AgentName      string
	BranchName     string
	MergeRequestID string
	SessionKept    bool // true if session was not killed (envoy resolve)
	PushFailed     bool // true if git push failed; writ left tethered for retry (conflict-resolution writs only)

	// ReportChecked is true when the resolve flow actually ran the
	// resolution-report capture step (captureResolutionReport). It is false
	// for paths that never attempt capture — push-failure early returns and
	// conflict-resolution resolves — so callers can distinguish "checked,
	// no report" from "not applicable" instead of treating every zero-value
	// ReportCaptured as a missing-report warning.
	ReportChecked bool
	// ReportCaptured is true when an agent-authored .resolution.md was
	// found, untracked, and moved to the writ's output directory. Only
	// meaningful when ReportChecked is true.
	ReportCaptured bool
}

// ResolveOpts holds the inputs for a resolve operation.
type ResolveOpts struct {
	World     string
	AgentName string
	WritID    string // Optional: specific writ to resolve (persistent agents only; ignored for outpost agents)
}

// IsResolveInProgress returns true if any resolve lock file exists for this agent.
// Checks both the shared lock file (outpost agents) and per-writ lock files (persistent agents).
func IsResolveInProgress(world, agentName, role string) bool {
	if _, err := os.Stat(flock.ResolveLockPath(world, agentName, role)); err == nil {
		return true
	}
	agentDir := config.AgentDir(world, agentName, role)
	matches, err := filepath.Glob(filepath.Join(agentDir, ".resolve_in_progress.*"))
	return err == nil && len(matches) > 0
}

// ClearResolveLocksForAgent removes all resolve lock files for an agent (shared and per-writ).
func ClearResolveLocksForAgent(world, agentName, role string) {
	os.Remove(flock.ResolveLockPath(world, agentName, role))
	agentDir := config.AgentDir(world, agentName, role)
	if matches, err := filepath.Glob(filepath.Join(agentDir, ".resolve_in_progress.*")); err == nil {
		for _, f := range matches {
			os.Remove(f)
		}
	}
}

// agentTeardownState is the shared teardown sequence used by both Resolve and
// resolveConflictResolution: clear tether, update agent state, cleanup and
// stop session.
//
// agent is the agent record captured before the teardown sequence begins. Since
// the dispatch lock is held throughout, the captured value is authoritative —
// no re-fetch is required or performed here.
//
// Returns sessionKept=true if the session was preserved (envoy/forge roles).
func agentTeardownState(
	opts ResolveOpts,
	agent *store.Agent,
	writID, worktreeDir, sessName string,
	sphereStore SphereStore,
	mgr SessionManager,
) (sessionKept bool) {
	role := agent.Role
	agentID := opts.World + "/" + opts.AgentName

	// Clear tether BEFORE updating agent state.
	// If tether clear fails after work is already done, don't roll back the
	// writ — the work is complete and only cleanup failed. Log the error and
	// let consul handle orphaned tethers.
	if role == "outpost" {
		// Outpost: clear entire tether directory.
		if err := tether.Clear(opts.World, opts.AgentName, role); err != nil {
			slog.Warn("resolve: failed to clear tether (work complete, consul will clean up)",
				"agent", opts.AgentName, "writ", writID, "error", err)
		}
	} else {
		// Persistent: remove only the resolved writ's tether file.
		if err := tether.ClearOne(opts.World, opts.AgentName, writID, role); err != nil {
			slog.Warn("resolve: failed to clear tether (work complete, consul will clean up)",
				"agent", opts.AgentName, "writ", writID, "error", err)
		}
	}

	// Update agent state.
	// Outpost agents are ephemeral — delete the record to reclaim the name.
	// Persistent roles (envoy) keep their record and update state based on
	// remaining tethers.
	// Note: At this point the writ is already done/closed and the tether clear
	// was attempted. Agent state failures are logged but don't roll back the
	// writ — the work is complete.
	if role == "outpost" {
		// Re-read agent to check if already deleted (idempotent re-run).
		if _, getErr := sphereStore.GetAgent(agentID); getErr == nil {
			if err := sphereStore.DeleteAgent(agentID); err != nil {
				slog.Warn("resolve: failed to delete agent (work complete)",
					"agent", agentID, "error", err)
			}
		}
	} else {
		// Persistent agent: determine remaining tethers after this resolve.
		// Tether for this writ was already cleared above, so List returns only remaining ones.
		currentTethers, listErr := tether.List(opts.World, opts.AgentName, role)
		if listErr != nil {
			slog.Warn("resolve: failed to list tethers (work complete)",
				"agent", opts.AgentName, "error", listErr)
		} else if len(currentTethers) > 0 {
			// More tethers remain: stay working.
			if agent.ActiveWrit == writID {
				// Resolving the active writ: promote a remaining tether to active_writ
				// so consul's stale-tether recovery can find this agent if the session
				// crashes. Setting active_writ to "" would cause consul to skip recovery.
				if err := sphereStore.UpdateAgentState(agentID, "working", currentTethers[0]); err != nil {
					slog.Warn("resolve: failed to update agent state (work complete)",
						"agent", agentID, "error", err)
				}
			}
			// If resolving a non-active writ, no state update needed.
		} else {
			// No remaining tethers: set to idle, clear active_writ.
			if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
				slog.Warn("resolve: failed to update agent state (work complete)",
					"agent", agentID, "error", err)
			}
		}
	}

	// Cleanup, then stop session.
	// Envoys keep their session alive — they are human-supervised and persistent.
	//
	// Cleanup must run BEFORE mgr.Stop. mgr.Stop (force=true) issues
	// `tmux kill-session`, which kills every process in the session — including
	// this resolve invocation when the agent is the caller (the common case
	// since `sol resolve` is run from inside the agent's tmux session). Any
	// cleanup ordered after Stop loses that race against SIGKILL, which leaves
	// runtime config dirs (.codex-home with auth.json containing
	// credentials) on disk indefinitely — neither consul nor sentinel reaps
	// them after a successful resolve, since the agent record is deleted.
	//
	// Mirror the marker-before-cycle invariant in handoff.Exec: write a
	// synchronization marker BEFORE the destructive op, so a fallback reaper
	// observing the agent dir can tell that resolve cleanup is in flight if
	// our process gets killed mid-cleanup. Consul reaps stale markers along
	// with the agent dir, so a leftover marker on the success path is harmless.
	sessionKept = false
	if role != "envoy" && role != "forge" {
		if role == "outpost" {
			markerPath := resolveCleanupMarkerPath(opts.World, opts.AgentName, role)
			if err := os.WriteFile(markerPath, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
				slog.Warn("resolve: failed to write cleanup marker",
					"agent", opts.AgentName, "error", err)
			}
			// Remove the runtime's config dir for the terminated
			// outpost. Closes the lifecycle opened by EnsureConfigDir; without
			// this, every dispatch leaks .claude-config or .codex-home (the
			// latter contains auth.json with credentials).
			cleanupOutpostConfigDir(opts.World, role, opts.AgentName)
			// Remove the worktree synchronously. Consul remains the backstop
			// for cases where this resolve crashes mid-cleanup.
			cleanupWorktree(opts.World, worktreeDir)
			// Best-effort marker removal on the success path.
			_ = os.Remove(markerPath)
		}
		// Brief delay to allow final agent output to flush before killing
		// the session. Stop is the destructive op — anything after it may
		// not execute when the agent is the caller.
		time.Sleep(1 * time.Second)
		if err := mgr.Stop(sessName, true); err != nil {
			slog.Warn("resolve: failed to stop session", "session", sessName, "error", err)
		}
	} else {
		sessionKept = true
	}

	return sessionKept
}

// Resolve signals work completion: git operations, state updates, tether clear.
// The logger parameter is optional — if nil, no events are emitted.
func Resolve(ctx context.Context, opts ResolveOpts, worldStore WorldStore, sphereStore SphereStore, mgr SessionManager, logger *events.Logger) (*ResolveResult, error) {
	agentID := opts.World + "/" + opts.AgentName
	sessName := config.SessionName(opts.World, opts.AgentName)

	// Look up agent first to determine role (needed for role-aware tether path).
	agent, err := sphereStore.GetAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", agentID, err)
	}

	// 1. Determine which writ to resolve.
	var writID string
	if agent.Role == "outpost" {
		// Outpost: read tether (single writ, unchanged behavior).
		writID, err = tether.Read(opts.World, opts.AgentName, agent.Role)
		if err != nil {
			return nil, fmt.Errorf("failed to read tether: %w", err)
		}
		if writID == "" {
			return nil, fmt.Errorf("no work tethered for agent %q in world %q", opts.AgentName, opts.World)
		}
	} else {
		// Persistent agent: use explicit WritID, fall back to active_writ from DB.
		if opts.WritID != "" {
			writID = opts.WritID
			// Validate the writ is actually tethered.
			if !tether.IsTetheredTo(opts.World, opts.AgentName, writID, agent.Role) {
				return nil, fmt.Errorf("writ %q is not tethered to agent %q", writID, opts.AgentName)
			}
		} else {
			writID = agent.ActiveWrit
			if writID == "" {
				return nil, fmt.Errorf("no active writ for agent %q in world %q", opts.AgentName, opts.World)
			}
		}
	}

	// Create resolve lock to prevent handoff from interrupting.
	// Persistent agents use a per-writ lock file so concurrent resolves don't
	// interfere: when Resolve-A finishes and removes its lock, Resolve-B's lock
	// remains visible and the handoff guard still sees a resolve in progress.
	var lockPath string
	if agent.Role == "outpost" {
		lockPath = flock.ResolveLockPath(opts.World, opts.AgentName, agent.Role)
	} else {
		lockPath = flock.ResolveWritLockPath(opts.World, opts.AgentName, agent.Role, writID)
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}
	// Write resolve lock with writ ID (enables crash recovery detection).
	if err := os.WriteFile(lockPath, []byte(writID), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write resolve lock: %w", err)
	}
	defer os.Remove(lockPath)

	// Acquire locks: writ first, then agent (consistent ordering with Cast).
	lock, err := flock.AcquireWritLock(writID)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	agentLock, err := flock.AcquireAgentLock(agentID)
	if err != nil {
		return nil, err
	}
	defer agentLock.Release()

	// Re-validate tether inside locked region to prevent TOCTOU race:
	// Untether may have completed between the pre-lock check (above) and now.
	// This mirrors the check in dispatch.Untether, which validates inside the lock.
	// Only applies to persistent agents — outpost agents use tether.Read (different semantics).
	if agent.Role != "outpost" {
		if !tether.IsTetheredTo(opts.World, opts.AgentName, writID, agent.Role) {
			return nil, fmt.Errorf("writ %q was untethered before lock acquisition", writID)
		}
	}

	// Compute worktree path and branch name based on role.
	var worktreeDir string
	var branchName string
	switch agent.Role {
	case "envoy":
		worktreeDir = config.EnvoyWorktreePath(opts.World, opts.AgentName)
		branchName = fmt.Sprintf("envoy/%s/%s/%s", opts.World, opts.AgentName, writID)
	case "forge":
		worktreeDir = filepath.Join(config.Home(), opts.World, "forge", "worktree")
		branchName = "forge/" + opts.World
	default:
		worktreeDir = WorktreePath(opts.World, opts.AgentName)
		branchName = fmt.Sprintf("outpost/%s/%s", opts.AgentName, writID)
	}

	// Get the writ for output and conflict-resolution detection.
	item, err := worldStore.GetWrit(writID)
	if err != nil {
		return nil, fmt.Errorf("failed to get writ %q: %w", writID, err)
	}

	// Reject resolve on already-closed writs — no git ops, no MR creation.
	if item.Status == "closed" {
		return nil, fmt.Errorf("writ %s is already closed — cannot resolve", writID)
	}

	// Detect conflict-resolution tasks and handle separately.
	if item.HasLabel("conflict-resolution") {
		return resolveConflictResolution(ctx, opts, item, branchName, worktreeDir,
			sessName, agent, worldStore, sphereStore, mgr, logger)
	}

	// Determine if this is a code writ. Non-code writs (analysis, etc.) skip
	// git operations, MR creation, and forge nudges entirely.
	isCodeWrit := item.Kind == "" || item.Kind == "code"

	var mrID string
	var pushFailed bool

	if isCodeWrit {
		// 2. Git operations in the worktree (code writs only).
		// Add + commit. Distinguishes "nothing to commit" (clean tree —
		// no-op) from real commit failures (hook rejection, lock
		// contention) instead of silently swallowing both alike.
		commitMsg := fmt.Sprintf("sol resolve: %s", item.Title)
		commitEventPayload := map[string]string{
			"writ_id": writID,
			"agent":   opts.AgentName,
		}
		if err := runResolveAddCommit(ctx, worktreeDir, commitMsg,
			agent.Name,
			strings.ToLower(agent.Role+"."+agent.Name)+"@sol.local",
			logger, commitEventPayload); err != nil {
			return nil, err
		}

		// git push: envoy pushes HEAD to a per-writ remote ref via refspec;
		// other roles push HEAD (which tracks the pre-created branch).
		pushCtx, pushCancel := context.WithTimeout(ctx, GitPushTimeout)
		defer pushCancel()
		var pushCmd *exec.Cmd
		if agent.Role == "envoy" {
			// Push HEAD to the per-writ remote ref without creating a local branch.
			// (A local branch cannot coexist with the persistent envoy branch because
			// git stores refs as a filesystem hierarchy and the envoy branch name is a
			// prefix of the per-writ branch name.)
			pushCmd = exec.CommandContext(pushCtx, "git", "-C", worktreeDir,
				"push", "origin", "HEAD:refs/heads/"+branchName)
		} else {
			pushCmd = exec.CommandContext(pushCtx, "git", "-C", worktreeDir, "push", "origin", "HEAD")
		}
		if out, err := pushCmd.CombinedOutput(); err != nil {
			slog.Warn("resolve: git push failed", "output", strings.TrimSpace(string(out)))
			pushFailed = true
		}
	}

	// If push failed, leave the writ tethered so the operator can retry after
	// fixing the push issue (network error, auth failure, force-push rejection).
	// Do NOT update the writ status, clear the tether, or stop the session.
	if pushFailed {
		return &ResolveResult{
			WritID:     writID,
			Title:      item.Title,
			AgentName:  opts.AgentName,
			BranchName: branchName,
			PushFailed: true,
		}, nil
	}

	// Track what has been done so we can undo on failure.
	var writUpdated bool

	rollback := func() {
		if writUpdated {
			if err := worldStore.UpdateWrit(writID, store.WritUpdates{Status: "tethered"}); err != nil {
				slog.Warn("resolve rollback: failed to reset writ", "writ", writID, "error", err)
			}
		}
	}

	// 3. Update writ status.
	if isCodeWrit {
		// Code writs: status → done (idempotent — skip if already done).
		if item.Status != "done" {
			if err := worldStore.UpdateWrit(writID, store.WritUpdates{Status: "done", Assignee: "-"}); err != nil {
				return nil, fmt.Errorf("failed to update writ status: %w", err)
			}
			writUpdated = true
		}
	} else {
		// Non-code writs: close directly with close_reason "completed".
		if item.Status != "closed" {
			if _, err := worldStore.CloseWrit(writID, "completed"); err != nil {
				return nil, fmt.Errorf("failed to close non-code writ: %w", err)
			}
			writUpdated = true
		}
	}

	if isCodeWrit {
		// 4. Create merge request (idempotent — skip if one already exists for this writ).
		// Filter out failed MRs so a new resolve after a failed MR creates a fresh
		// MR with the current branch instead of reusing the stale failed one.
		existingMRs, err := worldStore.ListMergeRequestsByWrit(writID, "")
		if err != nil {
			rollback()
			return nil, fmt.Errorf("failed to check existing merge requests: %w", err)
		}
		var activeMRs []store.MergeRequest
		for _, mr := range existingMRs {
			if store.IsActiveMRPhase(mr.Phase) {
				activeMRs = append(activeMRs, mr)
			}
		}
		if len(activeMRs) > 0 {
			mrID = activeMRs[0].ID
		} else if !pushFailed {
			// Only create an MR when the branch was successfully pushed.
			// If push failed, the remote branch doesn't exist yet, so creating
			// an MR would let forge attempt to merge a non-existent branch —
			// causing an infinite recast loop. The writ stays in "done" state;
			// the next resolve (after a successful push) will create the MR.
			//
			// Supersede any old failed MRs before creating the new one so that
			// the history stays clean and sentinel does not treat stale failed
			// MRs as blocking a future recast.
			if _, serr := worldStore.SupersedeFailedMRsForWrit(writID); serr != nil {
				rollback()
				return nil, fmt.Errorf("failed to supersede old MRs for %q: %w", writID, serr)
			}
			mrID, err = worldStore.CreateMergeRequest(writID, branchName, item.Priority)
			if err != nil {
				rollback()
				return nil, fmt.Errorf("failed to create merge request for %q: %w", writID, err)
			}
		}
	}

	// Auto-resolve writ-linked escalations (best-effort).
	escalations, escErr := sphereStore.ListEscalationsBySourceRef("writ:" + writID)
	if escErr != nil {
		slog.Warn("resolve: failed to check escalations", "error", escErr)
	} else {
		for _, esc := range escalations {
			if err := sphereStore.ResolveEscalation(esc.ID); err != nil {
				slog.Warn("resolve: failed to auto-resolve escalation", "escalation", esc.ID, "error", err)
			}
		}
	}

	// Capture the agent's resolution report (best-effort). Must run BEFORE
	// agentTeardownState: outpost teardown removes the worktree
	// (cleanupWorktree), taking .resolution.md with it.
	reportCaptured := captureResolutionReport(ctx, opts.World, writID, worktreeDir, logger)

	// Route durable lessons from the just-captured report (best-effort).
	// Must run AFTER captureResolutionReport — it reads from the writ's
	// output directory, not the worktree.
	routeDurableLessons(sphereStore, opts.World, writID, item.Title, agentID, logger)

	// Surface report-less resolves: a missing report (file never written)
	// and a skipped-as-tracked report (leaked artifact, logged separately
	// by captureResolutionReport) both land here as "no report captured".
	// This is visibility, not enforcement — DEGRADE applies, resolve still
	// succeeds. See captureResolutionReport for why a tracked file is never
	// treated as a real report.
	if !reportCaptured {
		if logger != nil {
			logger.Emit(events.EventNoResolutionReport, "dispatch", opts.AgentName, "both", map[string]string{
				"writ_id": writID,
				"agent":   opts.AgentName,
			})
		}
	}

	// 5–6b. Clear tether, update agent state, cleanup and stop session.
	sessionKept := agentTeardownState(opts, agent, writID, worktreeDir, sessName, sphereStore, mgr)

	// 8. Emit event and nudge downstream agents (code writs only for nudges).
	if isCodeWrit {
		if logger != nil {
			logger.Emit(events.EventResolve, "sol", opts.AgentName, "both", map[string]string{
				"writ_id":       writID,
				"agent":         opts.AgentName,
				"branch":        branchName,
				"merge_request": mrID,
			})
		}

		// Nudge forge that a new MR is ready (best-effort, smart delivery).
		// Only send when an MR was actually created — an empty mrID means push
		// failed and no MR exists yet, so waking forge would be a no-op.
		if mrID != "" {
			forgeSession := config.SessionName(opts.World, "forge")
			if err := nudge.Deliver(forgeSession, nudge.Message{
				Sender:   opts.AgentName,
				Type:     "MR_READY",
				Subject:  fmt.Sprintf("MR %s ready for merge", mrID),
				Body:     fmt.Sprintf(`{"writ_id":%q,"merge_request_id":%q,"branch":%q,"title":%q}`, writID, mrID, branchName, item.Title),
				Priority: "normal",
			}); err != nil {
				slog.Warn("resolve: failed to nudge forge", "error", err)
			}
		}
	} else {
		// Non-code writs: emit event without branch/MR fields.
		if logger != nil {
			logger.Emit(events.EventResolve, "sol", opts.AgentName, "both", map[string]string{
				"writ_id": writID,
				"agent":   opts.AgentName,
				"kind":    item.Kind,
			})
		}

	}

	// 9. Close history record for cycle-time tracking.
	if _, err := worldStore.EndHistory(writID); err != nil {
		slog.Warn("resolve: failed to end history", "writ", writID, "error", err)
	}

	// For non-code writs, BranchName and MergeRequestID are empty strings.
	resultBranch := ""
	if isCodeWrit {
		resultBranch = branchName
	}

	return &ResolveResult{
		WritID:         writID,
		Title:          item.Title,
		AgentName:      opts.AgentName,
		BranchName:     resultBranch,
		MergeRequestID: mrID,
		SessionKept:    sessionKept,
		ReportChecked:  true,
		ReportCaptured: reportCaptured,
	}, nil
}

// resolveConflictResolution handles the resolve flow for conflict-resolution tasks.
// Differences from normal resolve:
// 1. Uses --force-with-lease for push (branch was rebased)
// 2. Does NOT create a new merge request (original MR already exists)
// 3. Unblocks the original MR
// 4. Closes the resolution writ
func resolveConflictResolution(ctx context.Context, opts ResolveOpts, item *store.Writ, branchName, worktreeDir,
	sessName string, agent *store.Agent, worldStore WorldStore, sphereStore SphereStore, mgr SessionManager, logger *events.Logger) (*ResolveResult, error) {
	role := agent.Role

	// 1. Git operations: add, commit, force-push (branch was rebased).
	// Same add+commit discipline as Resolve — distinguish "nothing to commit"
	// from real failures.
	commitMsg := fmt.Sprintf("sol resolve: %s", item.Title)
	commitEventPayload := map[string]string{
		"writ_id":             item.ID,
		"agent":               opts.AgentName,
		"conflict_resolution": "true",
	}
	if err := runResolveAddCommit(ctx, worktreeDir, commitMsg,
		opts.AgentName,
		strings.ToLower(role+"."+opts.AgentName)+"@sol.local",
		logger, commitEventPayload); err != nil {
		return nil, err
	}

	// Force push with lease — branch was rebased, needs force push.
	pushCtx, pushCancel := context.WithTimeout(ctx, GitPushTimeout)
	defer pushCancel()
	pushCmd := exec.CommandContext(pushCtx, "git", "-C", worktreeDir, "push", "--force-with-lease", "origin", "HEAD")
	pushFailed := false
	if out, err := pushCmd.CombinedOutput(); err != nil {
		slog.Warn("resolve: git push --force-with-lease failed", "output", strings.TrimSpace(string(out)))
		pushFailed = true
	}

	// 2. Reset the parent's MR for retry (only if push succeeded).
	// Two complementary strategies:
	//   a) Find MR blocked by this resolution task and reset it.
	//   b) Look up parent writ's MR by parent_id — covers the case where
	//      the MR ended up in 'failed' phase independently.
	if !pushFailed {
		resetMRs := map[string]bool{} // track already-reset MR IDs

		// 2a. Find MR blocked by this resolution task.
		blockedMR, err := worldStore.FindMergeRequestByBlocker(item.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to find blocked MR for %q: %w", item.ID, err)
		}
		if blockedMR != nil {
			if err := worldStore.ResetMergeRequestForRetry(blockedMR.ID); err != nil {
				return nil, fmt.Errorf("failed to reset MR %q for retry: %w", blockedMR.ID, err)
			}
			resetMRs[blockedMR.ID] = true
		}

		// 2b. Check parent writ's MRs for any stuck in 'failed' phase.
		if item.ParentID != "" {
			parentMRs, err := worldStore.ListMergeRequestsByWrit(item.ParentID, "failed")
			if err != nil {
				slog.Warn("resolve: failed to list parent MRs", "parent", item.ParentID, "error", err)
			} else {
				for _, mr := range parentMRs {
					if resetMRs[mr.ID] {
						continue
					}
					if err := worldStore.ResetMergeRequestForRetry(mr.ID); err != nil {
						slog.Warn("resolve: failed to reset parent MR", "mr", mr.ID, "error", err)
					}
				}
			}
		}
	}

	// If push failed, leave the writ tethered so the operator can retry
	// after fixing the push issue (lease violation, connectivity error, etc.).
	// Do NOT close the resolution writ and do NOT reset the parent MR.
	// The standard Resolve function uses the same pattern.
	if pushFailed {
		return &ResolveResult{
			WritID:     item.ID,
			Title:      item.Title,
			AgentName:  opts.AgentName,
			BranchName: branchName,
			PushFailed: true,
		}, nil
	}

	// 3. Close the resolution writ.
	if _, err := worldStore.CloseWrit(item.ID); err != nil {
		return nil, fmt.Errorf("failed to close resolution writ: %w", err)
	}

	// 4–5b. Clear tether, update agent state, cleanup and stop session.
	sessionKept := agentTeardownState(opts, agent, item.ID, worktreeDir, sessName, sphereStore, mgr)

	// 7. Close history record for cycle-time tracking.
	if _, err := worldStore.EndHistory(item.ID); err != nil {
		slog.Warn("resolve: failed to end history", "writ", item.ID, "error", err)
	}

	if logger != nil {
		logger.Emit(events.EventResolve, "sol", opts.AgentName, "both", map[string]string{
			"writ_id": item.ID,
			"agent":   opts.AgentName,
			"branch":  branchName,
		})
	}

	return &ResolveResult{
		WritID:         item.ID,
		Title:          item.Title,
		AgentName:      opts.AgentName,
		BranchName:     branchName,
		MergeRequestID: "", // No new MR for conflict resolution.
		SessionKept:    sessionKept,
	}, nil
}
