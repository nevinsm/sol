package forge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/nevinsm/sol/internal/account"
	"github.com/nevinsm/sol/internal/adapter"
	"github.com/nevinsm/sol/internal/budget"
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
// The AssessCommand is resolved from the world's runtime adapter when possible,
// falling back to "claude -p" if the adapter is not found.
func DefaultPatrolConfig(world string) PatrolConfig {
	assessCmd := resolveCalloutCommand(world, "forge")
	return PatrolConfig{
		WaitTimeout:     30 * time.Second,
		AssessCommand:   assessCmd,
		AssessTimeout:   30 * time.Second,
		MonitorInterval: 3 * time.Minute,
		LogMaxBytes:     10 * 1024 * 1024, // 10MB
		LogMaxRotated:   3,
	}
}

// resolveCalloutCommand resolves the default callout command from the world's
// runtime adapter. Falls back to "claude -p" if the adapter is not found.
func resolveCalloutCommand(world, role string) string {
	const fallback = "claude -p"
	worldCfg, err := config.LoadWorldConfig(world)
	if err != nil {
		return fallback
	}
	runtime := worldCfg.ResolveRuntime(role)
	a, ok := adapter.Get(runtime)
	if !ok {
		return fallback
	}
	return a.CalloutCommand()
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
	// recoverWorktree, when non-nil, replaces s.forge.EnsureWorktree in the
	// cleanupSession broken-worktree recovery path. Test-only seam — production
	// code leaves this nil so the real EnsureWorktree is used.
	recoverWorktree func() error
}

// patrol runs one complete patrol cycle.
func (s *patrolState) patrol(ctx context.Context) {
	s.patrolCount++

	// 0. Recover — fix any MRs in "claimed" phase whose writ is "closed",
	// which is the state left by a partial MarkMerged failure.
	if n, err := s.forge.RecoverOrphanedMerged(); err != nil {
		s.forge.logger.Error("orphaned MR recovery failed", "error", err)
	} else if n > 0 {
		s.fl.Log("RECOVER", fmt.Sprintf("recovered %d orphaned MR(s) to merged phase", n))
	}

	if ctx.Err() != nil {
		return
	}

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

	// 2.6. Check account budget before spawning merge session.
	worldCfg, cfgErr := config.LoadWorldConfig(s.forge.world)
	if cfgErr == nil && len(worldCfg.Budget.Accounts) > 0 {
		forgeAccount := account.ResolveAccount("", worldCfg.World.DefaultAccount)
		if forgeAccount != "" {
			if err := budget.CheckAccountBudget(s.forge.worldStore, s.forge.sphereStore, forgeAccount, worldCfg.Budget); err != nil {
				s.fl.Log("BUDGET", fmt.Sprintf("budget exhausted for account %q, skipping patrol", forgeAccount))
				s.writeHeartbeat("idle", 0)
				s.emitPatrolEvent(0)
				return
			}
		}
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

	// 5. Execute merge.
	s.executeMergeSession(ctx, mr, len(ready))
}

// executeMergeSession runs the merge via an ephemeral Claude session (ADR-0028).
func (s *patrolState) executeMergeSession(ctx context.Context, mr *store.MergeRequest, queueDepth int) {
	defer s.cleanupSession()

	result, err := s.runMergeSession(ctx, mr, queueDepth)
	if err != nil {
		s.forge.logger.Error("merge session failed", "mr", mr.ID, "error", err)
		s.fl.Log("ERROR", fmt.Sprintf("merge session failed: %s", truncate(err.Error(), 200)))
		s.lastError = truncate(fmt.Sprintf("merge session failed: %s", err.Error()), 200)
		if failed, err := s.forge.Release(mr.ID); err != nil {
			s.forge.logger.Error("failed to release MR after session failure", "mr", mr.ID, "error", err)
		} else if failed {
			s.fl.Log("FAILED", fmt.Sprintf("marked failed after max attempts: %s", mr.Branch))
		}
		s.writeHeartbeat("idle", queueDepth-1)
		s.emitPatrolEvent(queueDepth)
		return
	}

	s.actOnResult(ctx, mr, result, queueDepth)
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
