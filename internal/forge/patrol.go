package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/heartbeat"
	"github.com/nevinsm/sol/internal/logutil"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/store"
)

// PatrolConfig extends the base Config with patrol-specific settings.
type PatrolConfig struct {
	WaitTimeout     time.Duration // max wait between patrols (default: 30s)
	AssessCommand   string        // AI assessment command (default: "claude -p")
	AssessTimeout   time.Duration // AI callout timeout (default: 30s)
	MonitorInterval time.Duration // interval for session output monitoring (default: 3m)
	LogMaxBytes     int64         // max log file size before rotation (default: 10MB)
	LogMaxRotated   int           // max rotated log files to keep (default: 3)
}

// DefaultPatrolConfig returns a PatrolConfig with sensible defaults.
func DefaultPatrolConfig() PatrolConfig {
	return PatrolConfig{
		WaitTimeout:     30 * time.Second,
		AssessCommand:   "claude -p",
		AssessTimeout:   30 * time.Second,
		MonitorInterval: 3 * time.Minute,
		LogMaxBytes:     10 * 1024 * 1024, // 10MB
		LogMaxRotated:   3,
	}
}

// Heartbeat records the forge's liveness state.
type Heartbeat struct {
	Timestamp   time.Time `json:"timestamp"`
	Status      string    `json:"status"`       // "idle", "working", "stopping"
	PatrolCount int       `json:"patrol_count"`
	QueueDepth  int       `json:"queue_depth"`
	CurrentMR   string    `json:"current_mr,omitempty"`
	CurrentWrit string    `json:"current_writ,omitempty"`
	ClaimedAt   string    `json:"claimed_at,omitempty"`
	LastMerge   time.Time `json:"last_merge,omitempty"`
	MergesTotal int       `json:"merges_total"`
	LastError   string    `json:"last_error,omitempty"`
}

// HeartbeatPath returns the path to the forge heartbeat file.
func HeartbeatPath(world string) string {
	return filepath.Join(config.Home(), world, "forge", "heartbeat.json")
}

// WriteHeartbeat writes the heartbeat file atomically.
func WriteHeartbeat(world string, hb *Heartbeat) error {
	dir := filepath.Join(config.Home(), world, "forge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create forge directory: %w", err)
	}
	return heartbeat.Write(HeartbeatPath(world), hb)
}

// ReadHeartbeat reads the current forge heartbeat file.
// Returns nil, nil if no heartbeat file exists.
func ReadHeartbeat(world string) (*Heartbeat, error) {
	var hb Heartbeat
	if err := heartbeat.Read(HeartbeatPath(world), &hb); err != nil {
		if errors.Is(err, heartbeat.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &hb, nil
}

// IsStale returns true if the heartbeat is older than maxAge.
func (hb *Heartbeat) IsStale(maxAge time.Duration) bool {
	return heartbeat.IsStale(hb.Timestamp, maxAge)
}

// --- Structured logging ---

var (
	logGreen  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	logRed    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	logYellow = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	logDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// verbColor maps forge log verbs to their styled representation.
func verbColor(verb string) string {
	switch verb {
	case "MERGED", "PASS":
		return logGreen.Render(verb)
	case "FAILED", "ERROR":
		return logRed.Render(verb)
	case "CONFLICT", "REBASE":
		return logYellow.Render(verb)
	default:
		return verb
	}
}

// forgeLogger manages structured output to stdout (colored) and log file (plain).
type forgeLogger struct {
	mu       sync.Mutex
	logFile  *os.File
	logPath  string
	maxBytes int64
	maxFiles int
}

// newForgeLogger creates a new forge logger, opening the log file for append.
func newForgeLogger(world string, pcfg PatrolConfig) (*forgeLogger, error) {
	logDir := filepath.Join(config.Home(), world, "forge")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create forge log directory: %w", err)
	}
	logPath := filepath.Join(logDir, "forge.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open forge log file: %w", err)
	}
	return &forgeLogger{
		logFile:  f,
		logPath:  logPath,
		maxBytes: pcfg.LogMaxBytes,
		maxFiles: pcfg.LogMaxRotated,
	}, nil
}

// Log writes a structured log entry to both stdout (colored) and log file (plain).
func (fl *forgeLogger) Log(verb, detail string) {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	now := time.Now()
	ts := now.Format("15:04:05")
	plain := fmt.Sprintf("[%s] %-8s %s\n", ts, verb, detail)

	// Write to stdout (colored).
	colored := fmt.Sprintf("[%s] %-8s %s\n", ts, verbColor(verb), detail)
	fmt.Print(colored)

	// Write to log file (plain).
	if fl.logFile != nil {
		fl.logFile.WriteString(plain)
		fl.maybeRotate()
	}
}

// Idle writes a dim idle status line.
func (fl *forgeLogger) Idle(detail string) {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	now := time.Now()
	ts := now.Format("15:04:05")
	separator := strings.Repeat("\u2500", 8)
	plain := fmt.Sprintf("[%s] %s %s\n", ts, separator, detail)

	// Stdout: dim style.
	colored := logDim.Render(plain)
	fmt.Print(colored)

	// Log file: plain.
	if fl.logFile != nil {
		fl.logFile.WriteString(plain)
	}
}

// Close closes the log file.
func (fl *forgeLogger) Close() {
	fl.mu.Lock()
	defer fl.mu.Unlock()
	if fl.logFile != nil {
		fl.logFile.Close()
		fl.logFile = nil
	}
}

// maybeRotate checks log file size and rotates if necessary.
// Must be called with fl.mu held.
func (fl *forgeLogger) maybeRotate() {
	if fl.logFile == nil || fl.maxBytes <= 0 {
		return
	}
	info, err := fl.logFile.Stat()
	if err != nil || info.Size() < fl.maxBytes {
		return
	}

	// Close current file.
	fl.logFile.Close()
	fl.logFile = nil

	// Rotate: forge.log.2 -> forge.log.3, forge.log.1 -> forge.log.2, forge.log -> forge.log.1
	for i := fl.maxFiles; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d", fl.logPath, i)
		if i == fl.maxFiles {
			os.Remove(old) // remove oldest
		}
		if i > 1 {
			prev := fmt.Sprintf("%s.%d", fl.logPath, i-1)
			os.Rename(prev, old)
		} else {
			os.Rename(fl.logPath, old)
		}
	}

	// Reopen log file.
	f, err := os.OpenFile(fl.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[forge] log rotation failed, logging to stderr: %v\n", err)
		fl.logFile = nil // Log() already handles nil logFile for file writes
		return
	}
	fl.logFile = f
}

// LogPath returns the path to the log file.
func LogPath(world string) string {
	return filepath.Join(config.Home(), world, "forge", "forge.log")
}

// --- Command executor (for testing) ---

// cmdRunner abstracts shell command execution for testing.
type cmdRunner interface {
	Run(ctx context.Context, dir string, name string, args ...string) ([]byte, error)
}

// realCmdRunner executes real commands.
type realCmdRunner struct{}

func (r *realCmdRunner) Run(ctx context.Context, dir string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// --- Patrol loop ---

// RunPatrol executes exactly one patrol cycle and returns. Unlike Run, it does
// not block waiting for nudges or run additional cycles. Intended for testing.
func (r *Forge) RunPatrol(ctx context.Context, pcfg PatrolConfig) error {
	fl, err := newForgeLogger(r.world, pcfg)
	if err != nil {
		return fmt.Errorf("failed to initialize forge logger: %w", err)
	}
	defer fl.Close()

	eventLog := events.NewLogger(config.Home())

	state := &patrolState{
		forge:    r,
		pcfg:     pcfg,
		fl:       fl,
		eventLog: eventLog,
		cmd:      &realCmdRunner{},
	}

	state.patrol(ctx)
	return nil
}

// Run starts the forge patrol loop. Blocks until ctx is cancelled.
func (r *Forge) Run(ctx context.Context, pcfg PatrolConfig) error {
	fl, err := newForgeLogger(r.world, pcfg)
	if err != nil {
		return fmt.Errorf("failed to initialize forge logger: %w", err)
	}
	defer fl.Close()

	eventLog := events.NewLogger(config.Home())

	state := &patrolState{
		forge:     r,
		pcfg:      pcfg,
		fl:        fl,
		eventLog:  eventLog,
		cmd:       &realCmdRunner{},
	}

	fl.Log("START", fmt.Sprintf("forge patrol started for world %q, target %s", r.world, r.cfg.TargetBranch))

	// Patrol immediately.
	state.patrol(ctx)
	// Best-effort log rotation after each patrol.
	logutil.TruncateIfNeeded(LogPath(r.world), logutil.DefaultMaxLogSize)

	for {
		select {
		case <-ctx.Done():
			state.writeHeartbeat("stopping", 0)
			fl.Log("STOP", "forge patrol stopping")
			return nil
		default:
		}

		// Wait for nudge or timeout.
		state.wait(ctx)

		if ctx.Err() != nil {
			state.writeHeartbeat("stopping", 0)
			fl.Log("STOP", "forge patrol stopping")
			return nil
		}

		state.patrol(ctx)
		// Best-effort log rotation after each patrol.
		logutil.TruncateIfNeeded(LogPath(r.world), logutil.DefaultMaxLogSize)
	}
}

// patrolState holds mutable state across patrol cycles.
type patrolState struct {
	forge       *Forge
	pcfg        PatrolConfig
	fl          *forgeLogger
	eventLog    *events.Logger
	cmd         cmdRunner

	patrolCount      int
	mergesTotal      int
	lastMerge        time.Time
	lastError        string // most recent error, cleared on successful merge
	verifyRetryDelay time.Duration // delay between verifyPush retries; 0 uses default (5s)
	preMergeRef      string // origin/{targetBranch} HEAD captured before each merge push
}

// patrol runs one complete patrol cycle.
func (s *patrolState) patrol(ctx context.Context) {
	s.patrolCount++

	// 1. Unblock — check if any blocked MRs can be unblocked.
	unblocked, err := s.forge.CheckUnblocked()
	if err != nil {
		s.forge.logger.Error("unblock check failed", "error", err)
	}
	for _, id := range unblocked {
		s.fl.Log("UNBLOCK", id)
	}

	if ctx.Err() != nil {
		return
	}

	// 2. Check pause.
	if IsForgePaused(s.forge.world) {
		s.fl.Log("PAUSED", "forge is paused, skipping patrol")
		s.writeHeartbeat("paused", 0)
		s.emitPatrolEvent(0)
		return
	}

	// 2.5. Enforce caravan blocks.
	if n, err := s.forge.EnforceCaravanBlocks(); err != nil {
		s.forge.logger.Error("caravan block enforcement failed", "error", err)
	} else if n > 0 {
		s.fl.Log("BLOCK", fmt.Sprintf("blocked %d MRs by caravan deps", n))
	}

	// 3. Scan — list ready MRs.
	ready, err := s.forge.ListReady()
	if err != nil {
		s.forge.logger.Error("scan failed", "error", err)
		s.writeHeartbeat("idle", 0)
		s.emitPatrolEvent(0)
		return
	}
	if len(ready) == 0 {
		s.fl.Idle(fmt.Sprintf("idle, 0 ready in queue"))
		s.writeHeartbeat("idle", 0)
		s.emitPatrolEvent(0)
		return
	}

	// 4. Claim — atomically claim next ready MR.
	mr, err := s.forge.Claim()
	if err != nil {
		s.forge.logger.Error("claim failed", "error", err)
		s.writeHeartbeat("idle", len(ready))
		s.emitPatrolEvent(len(ready))
		return
	}
	if mr == nil {
		s.fl.Idle(fmt.Sprintf("idle, %d ready in queue (claim race)", len(ready)))
		s.writeHeartbeat("idle", len(ready))
		s.emitPatrolEvent(len(ready))
		return
	}

	// Emit claim event.
	title := s.forge.writTitle(mr.WritID)
	s.fl.Log("CLAIM", fmt.Sprintf("%s  %q (%s)", mr.ID, title, mr.WritID))
	if s.eventLog != nil {
		s.eventLog.Emit(events.EventMergeClaimed, "forge", "forge", "both", map[string]string{
			"merge_request_id": mr.ID,
			"writ_id":          mr.WritID,
			"branch":           mr.Branch,
		})
	}

	// Write "working" heartbeat with MR context.
	s.writeHeartbeatWithMR("working", len(ready), mr)

	// 5. Execute merge — session-based (ADR-0028) or legacy path.
	if s.forge.sessions != nil {
		s.executeMergeSession(ctx, mr, len(ready))
	} else {
		s.executeLegacyMerge(ctx, mr, len(ready))
	}
}

// executeMergeSession runs the merge via an ephemeral Claude session (ADR-0028).
func (s *patrolState) executeMergeSession(ctx context.Context, mr *store.MergeRequest, queueDepth int) {
	defer s.cleanupSession()

	result, err := s.runMergeSession(ctx, mr)
	if err != nil {
		s.forge.logger.Error("merge session failed", "mr", mr.ID, "error", err)
		s.fl.Log("ERROR", fmt.Sprintf("merge session failed: %s", truncate(err.Error(), 200)))
		s.lastError = truncate(fmt.Sprintf("merge session failed: %s", err.Error()), 200)
		s.forge.Release(mr.ID)
		s.writeHeartbeat("idle", queueDepth-1)
		s.emitPatrolEvent(queueDepth)
		return
	}

	s.actOnResult(ctx, mr, result, queueDepth)
}

// executeLegacyMerge runs the merge using direct Go code (pre-ADR-0028).
// DEPRECATED: replaced by merge session (ADR-0028). Retained for fallback
// when no session manager is configured.
func (s *patrolState) executeLegacyMerge(ctx context.Context, mr *store.MergeRequest, queueDepth int) {
	// 5. Sync worktree.
	if err := s.syncWorktree(ctx); err != nil {
		s.forge.logger.Error("sync failed", "error", err)
		s.fl.Log("ERROR", fmt.Sprintf("sync failed: %s", truncate(err.Error(), 200)))
		s.lastError = truncate(fmt.Sprintf("sync failed: %s", err.Error()), 200)
		s.forge.Release(mr.ID)
		s.writeHeartbeat("idle", queueDepth)
		s.emitPatrolEvent(queueDepth)
		return
	}

	// 6. Merge.
	mergeResult := s.merge(ctx, mr)
	switch mergeResult {
	case mergeClean:
		// Has changes, proceed to gates.
	case mergeEmpty:
		if mr.Attempts > 1 {
			// Before concluding this is a permanent failure, check whether the
			// branch commits are already present in origin/{targetBranch}. This
			// handles the crash-after-push scenario: forge pushed successfully
			// but crashed before MarkMerged, so on retry the diff is empty but
			// the code is already on main.
			targetRef := fmt.Sprintf("origin/%s", s.forge.cfg.TargetBranch)
			out, logErr := s.cmd.Run(ctx, s.forge.worktree, "git", "log", targetRef, "--oneline", "--since=2 hours ago", "--grep", mr.WritID)
			if logErr == nil && len(strings.TrimSpace(string(out))) > 0 {
				// Commits already landed on the target branch — this was a
				// crash-after-push. Mark merged, not failed.
				s.fl.Log("MERGE", fmt.Sprintf("%s  empty diff but writ %s already in %s, marking merged", mr.Branch, mr.WritID, targetRef))
				if err := s.forge.MarkMerged(mr.ID); err != nil {
					s.forge.logger.Error("mark-merged failed", "mr", mr.ID, "error", err)
				} else {
					s.mergesTotal++
					s.lastMerge = time.Now()
					s.lastError = ""
					s.fl.Log("MERGED", mr.ID)
					if s.eventLog != nil {
						s.eventLog.Emit(events.EventMerged, "forge", "forge", "both", map[string]string{
							"merge_request_id": mr.ID,
						})
					}
				}
				s.writeHeartbeat("idle", queueDepth-1)
				s.emitPatrolEvent(queueDepth)
				return
			}
			s.fl.Log("SUSPECT", fmt.Sprintf("%s  empty after %d attempts, marking failed", mr.Branch, mr.Attempts))
			if err := s.forge.MarkFailed(mr.ID); err != nil {
				s.forge.logger.Error("mark-failed failed", "mr", mr.ID, "error", err)
			}
			s.writeHeartbeat("idle", queueDepth-1)
			s.emitPatrolEvent(queueDepth)
			return
		}
		s.fl.Log("MERGE", fmt.Sprintf("%s  empty diff, marking merged", mr.Branch))
		if err := s.forge.MarkMerged(mr.ID); err != nil {
			s.forge.logger.Error("mark-merged failed", "mr", mr.ID, "error", err)
		} else {
			s.mergesTotal++
			s.lastMerge = time.Now()
			s.lastError = ""
			s.fl.Log("MERGED", mr.ID)
			if s.eventLog != nil {
				s.eventLog.Emit(events.EventMerged, "forge", "forge", "both", map[string]string{
					"merge_request_id": mr.ID,
				})
			}
		}
		s.writeHeartbeat("idle", queueDepth-1)
		s.emitPatrolEvent(queueDepth)
		return
	case mergeConflict:
		s.fl.Log("CONFLICT", fmt.Sprintf("%s  conflicts detected, attempting auto-rebase", mr.Branch))
		if s.autoRebase(ctx, mr) {
			s.fl.Log("REBASE", fmt.Sprintf("auto-rebase succeeded for %s, retrying merge", mr.Branch))
			if s.eventLog != nil {
				s.eventLog.Emit(events.EventForgeRebase, "forge", "forge", "both", map[string]string{
					"merge_request_id": mr.ID,
					"writ_id":          mr.WritID,
					"branch":           mr.Branch,
				})
			}
			if _, err := s.cmd.Run(ctx, s.forge.worktree, "git", "fetch", "origin"); err != nil {
				s.forge.logger.Error("post-rebase fetch failed", "error", err)
			}
			retryResult := s.merge(ctx, mr)
			if retryResult == mergeClean || retryResult == mergeEmpty {
				if retryResult == mergeEmpty {
					s.fl.Log("MERGE", fmt.Sprintf("%s  empty diff after rebase, marking merged", mr.Branch))
					if err := s.forge.MarkMerged(mr.ID); err != nil {
						s.forge.logger.Error("mark-merged failed", "mr", mr.ID, "error", err)
					} else {
						s.mergesTotal++
						s.lastMerge = time.Now()
						s.lastError = ""
						s.fl.Log("MERGED", mr.ID)
						if s.eventLog != nil {
							s.eventLog.Emit(events.EventMerged, "forge", "forge", "both", map[string]string{
								"merge_request_id": mr.ID,
							})
						}
					}
					s.writeHeartbeat("idle", queueDepth-1)
					s.emitPatrolEvent(queueDepth)
					return
				}
				// mergeClean — fall through to gates and push below.
				break
			}
			s.fl.Log("REBASE", fmt.Sprintf("merge still conflicts after rebase for %s, creating resolution task", mr.Branch))
		} else {
			s.fl.Log("REBASE", fmt.Sprintf("auto-rebase failed for %s, creating resolution task", mr.Branch))
		}
		s.lastError = truncate(fmt.Sprintf("merge conflict: %s", mr.Branch), 200)
		if _, err := s.forge.CreateResolutionTask(mr); err != nil {
			s.forge.logger.Error("create-resolution failed", "mr", mr.ID, "error", err)
		}
		if err := s.forge.worldStore.UpdateMergeRequestPhase(mr.ID, "ready"); err != nil {
			s.forge.logger.Error("release-conflict-claim failed", "mr", mr.ID, "error", err)
		}
		s.writeHeartbeat("idle", queueDepth-1)
		s.emitPatrolEvent(queueDepth)
		return
	case mergeError:
		s.fl.Log("ERROR", fmt.Sprintf("merge failed for %s", mr.Branch))
		s.lastError = truncate(fmt.Sprintf("merge failed: %s", mr.Branch), 200)
		if failed, err := s.forge.Release(mr.ID); err != nil {
			s.forge.logger.Error("release failed", "mr", mr.ID, "error", err)
		} else if failed {
			s.fl.Log("FAILED", fmt.Sprintf("marked failed after max attempts: %s", mr.Branch))
		}
		s.writeHeartbeat("idle", queueDepth-1)
		s.emitPatrolEvent(queueDepth)
		return
	}

	if ctx.Err() != nil {
		return
	}

	// 7. Quality gates.
	gateResult := s.runGates(ctx, mr)
	switch gateResult {
	case gatePass:
		// Proceed to push.
	case gateFail:
		s.lastError = truncate(fmt.Sprintf("gate failed: %s", mr.Branch), 200)
		s.writeHeartbeat("idle", queueDepth-1)
		s.emitPatrolEvent(queueDepth)
		return
	case gatePreExisting:
		// Pre-existing failure — proceed to push.
	}

	if ctx.Err() != nil {
		return
	}

	// 8. Push.
	if err := s.push(ctx, mr); err != nil {
		s.fl.Log("REJECTED", fmt.Sprintf("push rejected for %s: %s", mr.Branch, truncate(err.Error(), 200)))
		s.lastError = truncate(fmt.Sprintf("push rejected: %s", err.Error()), 200)
		if failed, releaseErr := s.forge.Release(mr.ID); releaseErr != nil {
			s.forge.logger.Error("release failed", "mr", mr.ID, "error", releaseErr)
		} else if failed {
			s.fl.Log("FAILED", fmt.Sprintf("marked failed after max attempts: %s", mr.Branch))
		}
		s.writeHeartbeat("idle", queueDepth-1)
		s.emitPatrolEvent(queueDepth)
		return
	}

	// 9. Mark merged.
	if err := s.forge.MarkMerged(mr.ID); err != nil {
		s.forge.logger.Error("mark-merged failed", "mr", mr.ID, "error", err)
	} else {
		s.mergesTotal++
		s.lastMerge = time.Now()
		s.lastError = ""
		s.fl.Log("MERGED", mr.ID)
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
}

// wait polls for nudges until one arrives or timeout expires.
func (s *patrolState) wait(ctx context.Context) {
	sessName := config.SessionName(s.forge.world, "forge")
	deadline := time.Now().Add(s.pcfg.WaitTimeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msgs, err := nudge.Drain(sessName)
		if err != nil {
			s.forge.logger.Warn("nudge drain failed", "error", err)
		}

		for _, msg := range msgs {
			if msg.Type == "MR_READY" || msg.Type == "FORGE_RESUMED" {
				return // wake up immediately
			}
		}

		// Sleep 1s between polls.
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
		}
	}
}

// --- Merge step ---

type mergeOutcome int

const (
	mergeClean    mergeOutcome = iota // has changes, ready for gates
	mergeEmpty                        // no diff, mark merged
	mergeConflict                     // conflict detected
	mergeError                        // other error
)

// DEPRECATED: replaced by merge session (ADR-0028). Retained for legacy fallback.
// syncWorktree fetches and resets to origin/target branch.
func (s *patrolState) syncWorktree(ctx context.Context) error {
	worktree := s.forge.worktree

	out, err := s.cmd.Run(ctx, worktree, "git", "fetch", "origin")
	if err != nil {
		return fmt.Errorf("git fetch failed: %s: %w", truncate(string(out), 200), err)
	}

	targetRef := "origin/" + s.forge.cfg.TargetBranch
	out, err = s.cmd.Run(ctx, worktree, "git", "reset", "--hard", targetRef)
	if err != nil {
		return fmt.Errorf("git reset failed: %s: %w", truncate(string(out), 200), err)
	}

	// Get the commit SHA for logging.
	sha, _ := s.cmd.Run(ctx, worktree, "git", "rev-parse", "--short", "HEAD")
	s.fl.Log("SYNC", fmt.Sprintf("%s -> %s", targetRef, strings.TrimSpace(string(sha))))

	return nil
}

// DEPRECATED: replaced by merge session (ADR-0028). Retained for legacy fallback.
// merge performs a squash merge of the MR branch.
func (s *patrolState) merge(ctx context.Context, mr *store.MergeRequest) mergeOutcome {
	worktree := s.forge.worktree
	branchRef := "origin/" + mr.Branch

	out, err := s.cmd.Run(ctx, worktree, "git", "merge", "--squash", branchRef)
	if err != nil {
		// Check for conflict markers.
		outStr := string(out)
		if strings.Contains(outStr, "CONFLICT") || strings.Contains(outStr, "Merge conflict") {
			// Abort the merge.
			s.cmd.Run(ctx, worktree, "git", "merge", "--abort")
			return mergeConflict
		}
		s.forge.logger.Error("merge failed", "branch", mr.Branch, "output", truncate(outStr, 500), "error", err)
		// Try to clean up.
		s.cmd.Run(ctx, worktree, "git", "merge", "--abort")
		return mergeError
	}

	// Check if there are staged changes.
	_, err = s.cmd.Run(ctx, worktree, "git", "diff", "--cached", "--quiet")
	if err == nil {
		// Exit 0 = no diff = empty merge.
		return mergeEmpty
	}
	// Exit 1 = has changes.
	s.fl.Log("MERGE", fmt.Sprintf("%s  clean", mr.Branch))
	return mergeClean
}

// --- Auto-rebase ---

// DEPRECATED: replaced by merge session (ADR-0028). Retained for legacy fallback.
// autoRebase attempts to rebase the MR branch onto origin/main.
// Returns true if rebase and force-push succeeded, false otherwise.
// Always leaves the worktree in detached HEAD on origin/main.
func (s *patrolState) autoRebase(ctx context.Context, mr *store.MergeRequest) bool {
	worktree := s.forge.worktree
	targetRef := "origin/" + s.forge.cfg.TargetBranch

	// 1. Check out the source branch.
	if _, err := s.cmd.Run(ctx, worktree, "git", "checkout", mr.Branch); err != nil {
		// Branch doesn't exist or other checkout failure.
		s.cmd.Run(ctx, worktree, "git", "checkout", "--detach", targetRef)
		return false
	}

	// 2. Attempt rebase onto target.
	if _, err := s.cmd.Run(ctx, worktree, "git", "rebase", targetRef); err != nil {
		// Rebase failed — true conflict.
		s.cmd.Run(ctx, worktree, "git", "rebase", "--abort")
		s.cmd.Run(ctx, worktree, "git", "checkout", "--detach", targetRef)
		return false
	}

	// 3. Force-push the rebased branch.
	if _, err := s.cmd.Run(ctx, worktree, "git", "push", "--force-with-lease", "origin", mr.Branch); err != nil {
		s.cmd.Run(ctx, worktree, "git", "checkout", "--detach", targetRef)
		return false
	}

	// 4. Reset worktree back to target for the retry merge.
	s.cmd.Run(ctx, worktree, "git", "checkout", "--detach", targetRef)
	return true
}

// --- Quality gates ---

type gateOutcome int

const (
	gatePass        gateOutcome = iota // gates passed
	gateFail                           // branch caused failure
	gatePreExisting                    // pre-existing failure, proceed
)

// DEPRECATED: replaced by merge session (ADR-0028). Retained for legacy fallback.
// runGates executes quality gate commands and performs the Scotty Test on failure.
func (s *patrolState) runGates(ctx context.Context, mr *store.MergeRequest) gateOutcome {
	worktree := s.forge.worktree
	gateCmd := strings.Join(s.forge.cfg.QualityGates, " && ")

	gateStart := time.Now()
	gateCtx, cancel := context.WithTimeout(ctx, s.forge.cfg.GateTimeout)
	defer cancel()

	out, err := s.cmd.Run(gateCtx, worktree, "sh", "-c", gateCmd)
	elapsed := time.Since(gateStart)

	if err == nil {
		s.fl.Log("GATES", fmt.Sprintf("%s  PASS (%.1fs)", gateCmd, elapsed.Seconds()))
		return gatePass
	}

	s.fl.Log("GATES", fmt.Sprintf("%s  FAIL (%.1fs)", gateCmd, elapsed.Seconds()))

	// Scotty Test: determine if failure is branch-caused or pre-existing.
	return s.scottyTest(ctx, mr, gateCmd, out)
}

// DEPRECATED: replaced by merge session (ADR-0028). Retained for legacy fallback.
// scottyTest determines if a gate failure was caused by the branch or is pre-existing.
func (s *patrolState) scottyTest(ctx context.Context, mr *store.MergeRequest, gateCmd string, branchOutput []byte) gateOutcome {
	worktree := s.forge.worktree

	// a. Stash the changes.
	_, err := s.cmd.Run(ctx, worktree, "git", "stash")
	if err != nil {
		s.forge.logger.Error("scotty test: git stash failed", "error", err)
		// Can't run Scotty Test — assume branch-caused (conservative).
		s.handleBranchFailure(ctx, mr, gateCmd, branchOutput)
		return gateFail
	}

	// b. Run gates on base branch.
	baseCtx, cancel := context.WithTimeout(ctx, s.forge.cfg.GateTimeout)
	defer cancel()
	_, baseErr := s.cmd.Run(baseCtx, worktree, "sh", "-c", gateCmd)

	if baseErr != nil {
		// c. Base also fails → pre-existing. Pop stash and proceed.
		s.fl.Log("SCOTTY", "base also fails, pre-existing failure — proceeding")
		s.cmd.Run(ctx, worktree, "git", "stash", "pop")
		return gatePreExisting
	}

	// d. Base passes → branch caused the failure.
	s.fl.Log("SCOTTY", "base passes, branch caused failure")
	s.cmd.Run(ctx, worktree, "git", "stash", "drop")
	s.handleBranchFailure(ctx, mr, gateCmd, branchOutput)
	return gateFail
}

// DEPRECATED: replaced by merge session (ADR-0028). Retained for legacy fallback.
// handleBranchFailure runs AI callout for failure analysis and marks MR failed.
func (s *patrolState) handleBranchFailure(ctx context.Context, mr *store.MergeRequest, gateCmd string, output []byte) {
	// Run AI callout for enriched failure analysis.
	summary := s.runAssessment(ctx, mr, gateCmd, output)

	s.fl.Log("FAILED", fmt.Sprintf("%s  %s", mr.ID, truncate(summary, 200)))

	// Mark failed with enriched message.
	if err := s.forge.MarkFailed(mr.ID); err != nil {
		s.forge.logger.Error("mark-failed failed", "mr", mr.ID, "error", err)
	}

	if s.eventLog != nil {
		s.eventLog.Emit(events.EventMergeFailed, "forge", "forge", "both", map[string]string{
			"merge_request_id": mr.ID,
			"writ_id":          mr.WritID,
			"reason":           truncate(summary, 500),
		})
	}
}

// runAssessment calls the AI for failure analysis, with fallback on error.
func (s *patrolState) runAssessment(ctx context.Context, mr *store.MergeRequest, gateCmd string, output []byte) string {
	// Truncate output to last 200 lines.
	outputStr := lastNLines(string(output), 200)

	prompt := map[string]string{
		"context":     "merge gate failure",
		"branch":      mr.Branch,
		"writ_id":     mr.WritID,
		"gate_command": gateCmd,
		"gate_output":  outputStr,
	}
	promptJSON, err := json.Marshal(prompt)
	if err != nil {
		return fmt.Sprintf("gate command failed: %s", truncate(outputStr, 200))
	}

	assessCtx, cancel := context.WithTimeout(ctx, s.pcfg.AssessTimeout)
	defer cancel()

	parts := strings.Fields(s.pcfg.AssessCommand)
	if len(parts) == 0 {
		return fmt.Sprintf("gate command failed: %s", truncate(outputStr, 200))
	}

	cmd := exec.CommandContext(assessCtx, parts[0], parts[1:]...)
	cmd.Stdin = bytes.NewReader(promptJSON)
	cmd.Dir = s.forge.worktree

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		// Fallback: use raw output excerpt.
		s.forge.logger.Warn("AI assessment failed, using raw output", "error", err)
		return fmt.Sprintf("gate command failed: %s", truncate(outputStr, 200))
	}

	result := strings.TrimSpace(stdout.String())
	if result == "" {
		return fmt.Sprintf("gate command failed: %s", truncate(outputStr, 200))
	}
	return result
}

// --- Push step ---

// DEPRECATED: replaced by merge session (ADR-0028). Retained for legacy fallback.
// push commits and pushes to the target branch.
func (s *patrolState) push(ctx context.Context, mr *store.MergeRequest) error {
	worktree := s.forge.worktree
	title := s.forge.writTitle(mr.WritID)
	commitMsg := fmt.Sprintf("%s (%s)", title, mr.WritID)

	// Commit.
	out, err := s.cmd.Run(ctx, worktree, "git", "commit", "-m", commitMsg)
	if err != nil {
		return fmt.Errorf("git commit failed: %s: %w", truncate(string(out), 200), err)
	}

	// Get SHAs for logging.
	oldSHA, _ := s.cmd.Run(ctx, worktree, "git", "rev-parse", "--short", "HEAD~1")
	newSHA, _ := s.cmd.Run(ctx, worktree, "git", "rev-parse", "--short", "HEAD")

	// Push.
	pushRef := fmt.Sprintf("HEAD:%s", s.forge.cfg.TargetBranch)
	out, err = s.cmd.Run(ctx, worktree, "git", "push", "origin", pushRef)
	if err != nil {
		return fmt.Errorf("git push failed: %s: %w", truncate(string(out), 200), err)
	}

	s.fl.Log("PUSH", fmt.Sprintf("%s  %s -> %s",
		s.forge.cfg.TargetBranch,
		strings.TrimSpace(string(oldSHA)),
		strings.TrimSpace(string(newSHA))))
	return nil
}

// --- Heartbeat and events ---

// writeHeartbeat writes the heartbeat file.
func (s *patrolState) writeHeartbeat(status string, queueDepth int) {
	s.writeHeartbeatWithMR(status, queueDepth, nil)
}

// writeHeartbeatWithMR writes the heartbeat file with optional merge request context.
func (s *patrolState) writeHeartbeatWithMR(status string, queueDepth int, mr *store.MergeRequest) {
	hb := &Heartbeat{
		Timestamp:   time.Now().UTC(),
		Status:      status,
		PatrolCount: s.patrolCount,
		QueueDepth:  queueDepth,
		MergesTotal: s.mergesTotal,
		LastMerge:   s.lastMerge,
		LastError:   s.lastError,
	}
	if mr != nil {
		hb.CurrentMR = mr.ID
		hb.CurrentWrit = mr.WritID
		hb.ClaimedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := WriteHeartbeat(s.forge.world, hb); err != nil {
		s.forge.logger.Error("failed to write heartbeat", "error", err)
	}
}

// emitPatrolEvent emits the forge_patrol event.
func (s *patrolState) emitPatrolEvent(queueDepth int) {
	if s.eventLog == nil {
		return
	}
	s.eventLog.Emit(events.EventForgePatrol, "forge", "forge", "feed", map[string]any{
		"patrol_count": s.patrolCount,
		"queue_depth":  queueDepth,
		"merges_total": s.mergesTotal,
		"world":        s.forge.world,
	})
}

// --- Helpers ---

// lastNLines returns the last n lines of s.
func lastNLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
