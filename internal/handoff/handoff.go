package handoff

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/fileutil"
	"github.com/nevinsm/sol/internal/flock"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/sessionsave"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// State captures an agent's context at the moment of handoff.
type State struct {
	WritID          string    `json:"writ_id"`
	ActiveWritID    string    `json:"active_writ_id,omitempty"`
	AgentName       string    `json:"agent_name"`
	World           string    `json:"world"`
	Role            string    `json:"role,omitempty"`
	PreviousSession string    `json:"previous_session"`
	Summary         string    `json:"summary"`
	RecentOutput    string    `json:"recent_output"`
	RecentCommits   []string  `json:"recent_commits"`
	HandedOffAt     time.Time `json:"handed_off_at"`
	Consumed        bool      `json:"consumed,omitempty"`
	GitStatus       string    `json:"git_status,omitempty"`
	GitStash        string    `json:"git_stash,omitempty"`
	DiffStat        string    `json:"diff_stat,omitempty"`
	WorkflowStep    string    `json:"workflow_step,omitempty"` // current workflow step from .workflow/state.json
}

// SessionManager is the canonical session manager interface.
type SessionManager = session.SessionManager

// SphereStore is the subset of store.Store used by handoff.
type SphereStore interface {
	SendMessage(sender, recipient, subject, body string, priority int, msgType string) (string, error)
	GetAgent(id string) (*store.Agent, error)
}

// HandoffPath returns the path to an agent's handoff state file.
// Uses role-aware directory: outposts/{name}/ for agents, envoys/{name}/ for envoys, etc.
func HandoffPath(world, agentName, role string) string {
	return filepath.Join(config.AgentDir(world, agentName, role), ".handoff.json")
}

// isResolveInProgress returns true if any resolve lock file exists for the agent.
// Checks both the shared .resolve_in_progress file (outpost agents) and per-writ
// .resolve_in_progress.{writID} files (persistent agents with concurrent resolves).
// Uses flock.ResolveLockPath as the canonical path helper (mirrors dispatch.IsResolveInProgress).
func isResolveInProgress(world, agentName, role string) bool {
	if _, err := os.Stat(flock.ResolveLockPath(world, agentName, role)); err == nil {
		return true
	}
	agentDir := config.AgentDir(world, agentName, role)
	matches, err := filepath.Glob(filepath.Join(agentDir, ".resolve_in_progress.*"))
	return err == nil && len(matches) > 0
}

// HasHandoff returns true if an unconsumed handoff file exists for this agent.
func HasHandoff(world, agentName, role string) bool {
	state, err := Read(world, agentName, role)
	return err == nil && state != nil && !state.Consumed
}

// MarkConsumed sets the consumed flag on the handoff file without deleting it.
// The file remains on disk so it can be re-read if the new session crashes.
// The next Write() call will overwrite it with fresh state.
func MarkConsumed(world, agentName, role string) error {
	state, err := Read(world, agentName, role)
	if err != nil {
		return fmt.Errorf("failed to read handoff state: %w", err)
	}
	if state == nil {
		return nil
	}
	state.Consumed = true
	return Write(state)
}

// CaptureOpts configures what to capture during handoff.
type CaptureOpts struct {
	World        string
	AgentName    string
	Role         string      // agent role (default: "outpost")
	Summary      string      // agent-provided summary (optional)
	CaptureLines int         // lines of tmux output to capture (default: 100)
	CommitCount  int         // recent commits to include (default: 10)
	WorktreeDir  string      // explicit worktree path (uses config.WorktreePath if empty)
	Sphere       SphereStore // optional sphere store for reading active writ from DB
	ActiveWrit   string      // pre-read active writ ID; when set, skips DB re-read in Capture
}

// Capture gathers the current state of an agent's session.
// When a SphereStore is provided in opts, reads active_writ from DB to
// determine the primary writ context. Falls back to tether.Read() when
// no sphere store is available (backward compat). If neither active writ
// nor tethers exist, captures general session state without writ-specific fields.
func Capture(opts CaptureOpts, sessionCapture func(string, int) (string, error),
	gitLog func(string, int) ([]string, error)) (*State, error) {

	if opts.CaptureLines <= 0 {
		opts.CaptureLines = 100
	}
	if opts.CommitCount <= 0 {
		opts.CommitCount = 10
	}

	role := opts.Role
	if role == "" {
		role = "outpost"
	}

	// 1. Determine active writ from DB (preferred) or tether (fallback).
	var activeWritID string
	var writID string

	if opts.ActiveWrit != "" {
		// Use pre-read active writ ID (avoids stale re-read from DB).
		activeWritID = opts.ActiveWrit
		writID = activeWritID
	} else if opts.Sphere != nil {
		agentID := opts.World + "/" + opts.AgentName
		agent, err := opts.Sphere.GetAgent(agentID)
		if err == nil && agent != nil && agent.ActiveWrit != "" {
			activeWritID = agent.ActiveWrit
			writID = activeWritID
		}
	}

	// Fallback to tether if no active writ from DB.
	// Use ReadSingle: for outposts (single tether) this is equivalent to Read.
	// For persistent agents with multiple tethers, it errors rather than
	// silently picking the alphabetically-first writ (CD-4 fix).
	if writID == "" {
		tetherID, err := tether.ReadSingle(opts.World, opts.AgentName, role)
		if err != nil {
			return nil, fmt.Errorf("failed to read tether: %w", err)
		}
		writID = tetherID
	}

	// No active writ and no tether — capture general session state.
	hasWrit := writID != ""

	// 2. Session name.
	sessionName := config.SessionName(opts.World, opts.AgentName)

	// 3. Capture tmux output (always, regardless of writ).
	recentOutput := ""
	if sessionCapture != nil {
		output, err := sessionCapture(sessionName, opts.CaptureLines)
		if err == nil {
			recentOutput = output
		}
	}

	// 4-5: Writ-specific context (git) only when a writ is active.
	var recentCommits []string
	var gitStatus, gitStash, diffStat string
	var workflowStep string

	if hasWrit {
		// 4. Capture recent git commits from worktree.
		worktreeDir := opts.WorktreeDir
		if worktreeDir == "" {
			worktreeDir = config.WorktreePath(opts.World, opts.AgentName)
		}
		if gitLog != nil {
			commits, err := gitLog(worktreeDir, opts.CommitCount)
			if err == nil {
				recentCommits = commits
			}
		}

		// 5. Capture git status, stash, and diff stat from worktree.
		gitStatus = gitShort(worktreeDir, "status", "--short")
		gitStash = gitShort(worktreeDir, "stash", "list")
		diffStat = gitShort(worktreeDir, "diff", "--stat")

		// 6. Read workflow state if present (best-effort — missing or malformed is not an error).
		wfStatePath := filepath.Join(config.AgentDir(opts.World, opts.AgentName, role), ".workflow", "state.json")
		if wfData, wfErr := os.ReadFile(wfStatePath); wfErr == nil {
			var wfState struct {
				CurrentStep string `json:"current_step"`
			}
			if json.Unmarshal(wfData, &wfState) == nil {
				workflowStep = wfState.CurrentStep
			}
		}
	}

	if recentCommits == nil {
		recentCommits = []string{}
	}

	// 7. Auto-generate summary if not provided.
	summary := opts.Summary
	if summary == "" {
		if hasWrit {
			summary = fmt.Sprintf("Session handoff for %s. Working on %s.", opts.AgentName, writID)
		} else {
			summary = fmt.Sprintf("Session handoff for %s. No active writ.", opts.AgentName)
		}
		if workflowStep != "" {
			summary += fmt.Sprintf(" Workflow step: %s.", workflowStep)
		}
		if len(recentCommits) > 0 {
			summary += fmt.Sprintf(" Last commit: %s", recentCommits[0])
		}
	}

	return &State{
		WritID:          writID,
		ActiveWritID:    activeWritID,
		AgentName:       opts.AgentName,
		World:           opts.World,
		Role:            role,
		PreviousSession: sessionName,
		Summary:         summary,
		RecentOutput:    recentOutput,
		RecentCommits:   recentCommits,
		HandedOffAt:     time.Now().UTC(),
		GitStatus:       gitStatus,
		GitStash:        gitStash,
		DiffStat:        diffStat,
		WorkflowStep:    workflowStep,
	}, nil
}

// Write serializes the handoff state to the agent's handoff file.
// Creates parent directories if needed.
func Write(state *State) error {
	role := state.Role
	if role == "" {
		role = "outpost"
	}
	path := HandoffPath(state.World, state.AgentName, role)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create handoff directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal handoff state: %w", err)
	}

	return fileutil.AtomicWrite(path, data, 0o644)
}

// Read deserializes the handoff state from the agent's handoff file.
// Returns nil, nil if no handoff file exists.
// Logs a warning if the file is older than 1 hour (potential staleness).
func Read(world, agentName, role string) (*State, error) {
	p := HandoffPath(world, agentName, role)

	info, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to stat handoff file: %w", err)
	}

	if age := time.Since(info.ModTime()); age > time.Hour {
		slog.Warn("handoff file is stale",
			"path", p,
			"age", age.Round(time.Second).String(),
			"world", world,
			"agent", agentName,
		)
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			// Race: file removed between Stat and ReadFile.
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read handoff file: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal handoff state: %w", err)
	}
	return &state, nil
}

// Remove deletes the handoff file. No-op if it doesn't exist.
func Remove(world, agentName, role string) error {
	err := os.Remove(HandoffPath(world, agentName, role))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove handoff file: %w", err)
	}
	return nil
}

// GitLog returns the last N commit summaries from a git worktree.
// Returns empty slice if the directory has no commits or doesn't exist.
func GitLog(worktreeDir string, count int) ([]string, error) {
	if _, err := os.Stat(worktreeDir); os.IsNotExist(err) {
		return []string{}, nil
	}

	cmd := exec.Command("git", "-C", worktreeDir, "log", "--oneline", fmt.Sprintf("-%d", count))
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "handoff: git log failed in %s: %v\n", worktreeDir, err)
		return []string{}, nil
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []string{}, nil
	}
	return lines, nil
}

// gitShort runs a git command in the worktree and returns trimmed output.
// Returns empty string if the command fails or directory doesn't exist.
//
// Errors from the git command are logged at WARN level (mirroring sibling
// GitLog) so operators debugging "why is the resume prompt missing the SHA"
// have a signal. The non-existent-directory case is silent because that is
// the normal "agent never had a worktree" path.
func gitShort(worktreeDir string, args ...string) string {
	if _, err := os.Stat(worktreeDir); os.IsNotExist(err) {
		return ""
	}
	fullArgs := append([]string{"-C", worktreeDir}, args...)
	cmd := exec.Command("git", fullArgs...)
	// Use CombinedOutput so that git's stderr (e.g. "not a git repository") is
	// captured and surfaced in the warning log rather than silently discarded
	// (V19 — cmd.Output() previously stripped stderr, making "why is this empty"
	// hard to diagnose from the warning alone).
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warn("handoff: gitShort command failed",
			"worktree", worktreeDir,
			"args", args,
			"output", strings.TrimSpace(string(out)),
			"error", err,
		)
		return ""
	}
	return strings.TrimSpace(string(out))
}

// MinHandoffCooldown is the minimum time between handoff cycles.
// Prevents restart storms from pathological cases (e.g., gate dumping 100k output).
const MinHandoffCooldown = 2 * time.Minute

// MarkerPath returns the path to the handoff marker file for an agent.
func MarkerPath(world, agentName, role string) string {
	return filepath.Join(config.AgentDir(world, agentName, role), ".handoff_marker")
}

// WriteMarker writes a handoff marker file with the current timestamp and reason.
func WriteMarker(world, agentName, role, reason string) error {
	path := MarkerPath(world, agentName, role)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create marker directory: %w", err)
	}
	content := fmt.Sprintf("%s\n%s\n", time.Now().UTC().Format(time.RFC3339), reason)
	return fileutil.AtomicWrite(path, []byte(content), 0o644)
}

// ReadMarker reads the handoff marker file. Returns the timestamp and reason.
// Returns zero time and empty string if the marker doesn't exist.
func ReadMarker(world, agentName, role string) (time.Time, string, error) {
	data, err := os.ReadFile(MarkerPath(world, agentName, role))
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, "", nil
		}
		return time.Time{}, "", fmt.Errorf("failed to read marker: %w", err)
	}
	lines := strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)
	if len(lines) == 0 {
		return time.Time{}, "", nil
	}
	ts, err := time.Parse(time.RFC3339, lines[0])
	if err != nil {
		return time.Time{}, "", nil
	}
	reason := ""
	if len(lines) > 1 {
		reason = lines[1]
	}
	return ts, reason, nil
}

// RemoveMarker deletes the handoff marker file. No-op if it doesn't exist.
func RemoveMarker(world, agentName, role string) error {
	err := os.Remove(MarkerPath(world, agentName, role))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove marker: %w", err)
	}
	return nil
}

// LastHandoffPath returns the path to the durable last-handoff timestamp
// file for an agent.
//
// This is deliberately a separate file from the handoff marker
// (.handoff_marker). The marker serves prime.go as a one-shot "fresh
// session" flag — prime reads it and removes it within seconds of session
// start (internal/dispatch/prime.go). Exec also used to read that same
// marker for the restart-storm cooldown guard and session_age telemetry,
// but by the time any subsequent handoff ran, prime had already consumed
// (removed) it — the cooldown only ever covered the pre-prime window, and
// session_age always read "0s" (2026-08-19 handoff audit).
//
// This file has exactly one writer (WriteLastHandoff, called from Exec) and
// nothing removes it during normal operation, so it survives prime and
// gives Exec an accurate signal across the whole session lifetime. It is
// plain text (a single RFC3339 timestamp) rather than reusing the marker's
// consumed-flag/rewrite approach, so an operator can `cat` it directly to
// see when an agent last handed off (GLASS).
func LastHandoffPath(world, agentName, role string) string {
	return filepath.Join(config.AgentDir(world, agentName, role), ".last_handoff")
}

// WriteLastHandoff records the current time as the durable last-handoff
// timestamp.
func WriteLastHandoff(world, agentName, role string) error {
	path := LastHandoffPath(world, agentName, role)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create last-handoff directory: %w", err)
	}
	content := time.Now().UTC().Format(time.RFC3339) + "\n"
	return fileutil.AtomicWrite(path, []byte(content), 0o644)
}

// ReadLastHandoff reads the durable last-handoff timestamp. Returns the
// zero time (with a nil error) if the file doesn't exist yet — e.g. an
// agent's first handoff — or if its contents don't parse as RFC3339,
// mirroring ReadMarker's leniency toward a malformed timestamp line.
func ReadLastHandoff(world, agentName, role string) (time.Time, error) {
	data, err := os.ReadFile(LastHandoffPath(world, agentName, role))
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("failed to read last-handoff timestamp: %w", err)
	}
	ts, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return time.Time{}, nil
	}
	return ts, nil
}

// RemoveLastHandoff deletes the last-handoff timestamp file. No-op if it
// doesn't exist. Used only to clean up after a failed handoff attempt (see
// the startupErr handling in Exec) so a half-completed cycle doesn't leave
// behind a timestamp for a handoff that never actually happened.
func RemoveLastHandoff(world, agentName, role string) error {
	err := os.Remove(LastHandoffPath(world, agentName, role))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove last-handoff timestamp: %w", err)
	}
	return nil
}

// BuildResumeState extracts a startup.ResumeState from a captured handoff State.
// RecentCommits and GitStatus are carried forward so the successor session has
// immediate git context in its prime prompt without running additional git commands
// (V18 — previously these fields were truncated because ResumeState lacked them).
func (s *State) BuildResumeState(reason string) startup.ResumeState {
	rs := startup.ResumeState{
		ClaimedResource: s.WritID,
		Reason:          reason,
		Summary:         s.Summary,
		RecentCommits:   s.RecentCommits,
		GitStatus:       s.GitStatus,
	}
	if s.ActiveWritID != "" {
		rs.NewActiveWrit = s.ActiveWritID
	}
	return rs
}

// CaptureResumeState reads durable state from disk and returns a ResumeState
// suitable for startup.Resume(). Reads active writ (from DB when sphere is
// provided, falling back to tether) to determine the agent's current position.
func CaptureResumeState(world, agent, role, reason string, sphere SphereStore) startup.ResumeState {
	state := startup.ResumeState{Reason: reason}

	// Read active writ from DB (preferred) or tether (fallback).
	if sphere != nil {
		agentID := world + "/" + agent
		ag, err := sphere.GetAgent(agentID)
		if err == nil && ag != nil && ag.ActiveWrit != "" {
			state.NewActiveWrit = ag.ActiveWrit
			state.ClaimedResource = ag.ActiveWrit
			return state
		}
	}

	// Fallback: read claimed work from tether (single-tether agents only).
	// ReadSingle errors for persistent agents with multiple concurrent tethers
	// instead of silently returning the alphabetically-first writ (CD-4 fix).
	writID, err := tether.ReadSingle(world, agent, role)
	if err != nil {
		if errors.Is(err, tether.ErrMultipleTethers) {
			slog.Warn("handoff: cannot determine claimed resource for multi-tether agent without active writ",
				"agent", agent, "world", world, "role", role)
		} else {
			slog.Warn("handoff: failed to read tether for resume state",
				"agent", agent, "world", world, "role", role, "error", err)
		}
		return state
	}
	if writID != "" {
		state.ClaimedResource = writID
	}

	return state
}

// ExecOpts configures the handoff execution.
type ExecOpts struct {
	World       string
	AgentName   string
	Summary     string // optional agent-provided summary
	Role        string // agent role: "outpost", "envoy", "forge" (default: "outpost")
	WorktreeDir string // explicit worktree path (required for non-outpost roles)
	Reason      string // handoff reason: "compact", "manual", "health-check" (default: "unknown")

	// SelfInvoked indicates the process executing Exec IS the target session
	// (the agent ran `sol handoff` on itself), as opposed to an operator or
	// another process (sentinel, autarch) triggering handoff against a
	// live, otherwise-busy session from the outside. The caller determines
	// this (typically by comparing SOL_WORLD/SOL_AGENT of its own
	// environment against World/AgentName) since Exec itself has no way to
	// distinguish "who is calling me". See the envoy save-prompt gate below
	// for why this distinction matters.
	SelfInvoked bool

	// StartupSphere is an optional sphere store for the startup.Resume/Launch
	// path. When nil, startup opens its own. Exposed for testing.
	StartupSphere startup.SphereStore
}

// Exec performs the full handoff sequence:
// 1. Capture current state (if tethered work exists) and write handoff file
// 2. Run the abort gates (cooldown, envoy save-prompt, WriteResumeState,
//    startup config lookup)
// 3. Emit the handoff event and send handoff mail to self (audit trail) —
//    only once every gate above has passed, so the event/mail always
//    correspond to a cycle that actually happens (Defect 2, 2026-08-19
//    handoff audit)
// 4. Cycle the tmux session atomically (respawn-pane -k)
//
// Step 4 uses Cycle for atomic process replacement, which is safe for
// self-handoff — the calling process is killed by respawn-pane -k, but the
// new session starts reliably because tmux handles the transition server-side.
// Falls back to Stop+Start if Cycle fails.
//
// For non-outpost agents (envoy, forge) without tethered work,
// steps 1-3 are skipped — the session is simply cycled, and the existing
// SessionStart hook re-injects context from durable state.
func Exec(opts ExecOpts, sessionMgr SessionManager, sphereStore SphereStore,
	logger *events.Logger) error {

	role := opts.Role
	if role == "" {
		role = "outpost"
	}

	reason := opts.Reason
	if reason == "" {
		reason = "unknown"
	}

	// Early skip: if a concurrent dispatch.Resolve is in progress, do nothing.
	// Resolve will tear down the session shortly anyway, and proceeding here
	// would persist a stale handoff state file + audit mail row that outlive
	// the agent (CF-L1 / CD-6). The check is a cheap local FS stat.
	// Checks both the shared lock (outpost) and any per-writ lock (persistent agents).
	if isResolveInProgress(opts.World, opts.AgentName, role) {
		fmt.Fprintf(os.Stderr, "handoff: resolve in progress, deferring to compaction\n")
		return nil
	}

	// Determine worktree directory.
	worktreeDir := opts.WorktreeDir
	if worktreeDir == "" {
		worktreeDir = config.WorktreePath(opts.World, opts.AgentName)
	}

	// Calculate session age from the durable last-handoff timestamp (time
	// since last handoff/start). Deliberately NOT the marker: prime.go
	// consumes (removes) the marker within seconds of session start, so by
	// the time any subsequent handoff ran here, it would always read as
	// zero/"0s" (2026-08-19 handoff audit). See LastHandoffPath for the full
	// rationale.
	var sessionAge time.Duration
	lastHandoffTS, err := ReadLastHandoff(opts.World, opts.AgentName, role)
	if err != nil {
		slog.Warn("handoff: failed to read last-handoff timestamp, treating as absent",
			"error", err, "agent", opts.AgentName, "world", opts.World)
	}
	if !lastHandoffTS.IsZero() {
		sessionAge = time.Since(lastHandoffTS)
	}

	// Try to capture state from active work (DB active_writ or tether fallback).
	hasTether := tether.IsTethered(opts.World, opts.AgentName, role)

	// Read agent snapshot ONCE to avoid stale re-reads. Subsequent operations
	// (Capture, resume state building) use this value consistently — a
	// concurrent resolve or writ-activate between reads would otherwise
	// produce an internally inconsistent handoff state.
	var activeWritID string
	if sphereStore != nil {
		agentID := opts.World + "/" + opts.AgentName
		if agent, err := sphereStore.GetAgent(agentID); err == nil && agent != nil {
			activeWritID = agent.ActiveWrit
		}
	}
	hasActiveWrit := activeWritID != ""

	hasWork := hasTether || hasActiveWrit

	// Acquire the writ flock as the primary serializer against dispatch.Resolve
	// (CD-4). Resolve writes its marker AND acquires this same flock at
	// internal/dispatch/resolve.go:179 before doing destructive work
	// (git push, writ status update, tether clear). Without this lock,
	// the earlier os.Stat marker check is a TOCTOU window: a resolve can
	// write its marker between handoff's stat and handoff's tmux respawn,
	// and handoff would then kill the session mid-resolve.
	//
	// We hold the lock through the cycle. respawn-pane -k kills the calling
	// process; the kernel releases the flock when our FD closes. The new
	// session does not need this lock.
	//
	// The earlier marker stat check is intentionally retained as a
	// secondary signal — it lets handoff defer cheaply in the common case
	// without spinning up the lock dir, and it surfaces crash-recovery
	// debug context (a stale marker from a dead resolve).
	//
	// Lock target: prefer the DB-resolved active writ; fall back to
	// tether.ReadSingle for outposts that pre-date the active_writ field.
	// ReadSingle (not Read) is used so that persistent agents with multiple
	// tethers surface an error rather than silently locking the wrong writ
	// (CD-4 fix). This matches dispatch.Resolve's own choice so both paths
	// target the same lock file.
	writIDForLock := activeWritID
	if writIDForLock == "" && hasTether {
		tetherID, err := tether.ReadSingle(opts.World, opts.AgentName, role)
		switch {
		case err == nil:
			writIDForLock = tetherID
		case errors.Is(err, tether.ErrMultipleTethers):
			// Persistent agent with multiple tethers and no DB-resolved active
			// writ. We cannot safely pick a lock target — defer rather than
			// risk locking the wrong writ and allowing a concurrent Resolve to
			// proceed without serialization.
			fmt.Fprintf(os.Stderr, "handoff: multiple tethers with no active writ from DB; deferring to compaction\n")
			return nil
		default:
			// Real I/O error reading tether — skip lock acquisition (best effort).
			slog.Warn("handoff: failed to read tether for writ lock", "error", err,
				"agent", opts.AgentName, "world", opts.World)
		}
	}
	if writIDForLock != "" {
		held, err := flock.TryAcquireWritLock(writIDForLock)
		if err != nil {
			// Real I/O failure (lock dir unwritable, etc.). Defer rather
			// than risk stomping on a concurrent resolve in unknown state.
			fmt.Fprintf(os.Stderr, "handoff: failed to try-acquire writ lock for %q (%v), deferring to compaction\n", writIDForLock, err)
			return nil
		}
		if held == nil {
			// Lock held by dispatch.Resolve (or another handoff). Defer.
			fmt.Fprintf(os.Stderr, "handoff: writ %q lock held, deferring to compaction\n", writIDForLock)
			return nil
		}
		defer held.Release()
	}

	var resumeState startup.ResumeState
	// handoffState carries the captured State through to emitHandoffAudit,
	// which fires only after every abort gate below has passed (Defect 2,
	// 2026-08-19 handoff audit).
	var handoffState *State
	if hasWork {
		// Full capture + handoff file + notification for agents with active work.
		state, err := Capture(CaptureOpts{
			World:       opts.World,
			AgentName:   opts.AgentName,
			Role:        role,
			Summary:     opts.Summary,
			WorktreeDir: worktreeDir,
			Sphere:      sphereStore,
			ActiveWrit:  activeWritID, // pass pre-read snapshot to avoid DB re-read
		}, func(name string, lines int) (string, error) {
			return sessionMgr.Capture(name, lines)
		}, GitLog)
		if err != nil {
			return fmt.Errorf("failed to capture handoff state: %w", err)
		}

		if err := Write(state); err != nil {
			return fmt.Errorf("failed to write handoff file: %w", err)
		}

		// Event emission and audit mail are deferred until every abort gate
		// below (envoy save-prompt delivery, WriteResumeState, missing
		// startup config) has passed — see the emitHandoffAudit call near
		// WriteMarker. Emitting here left a phantom "handoff" event/mail for
		// cycles that never happened (Defect 2, 2026-08-19 handoff audit).
		handoffState = state

		// Derive resume state from already-captured handoff state
		// to avoid redundant disk reads.
		resumeState = state.BuildResumeState(reason)
	} else {
		// No tether and no active writ — the agent is untethered, which is
		// the common state for envoys. Capture + write + notify anyway,
		// mirroring the hasWork branch above, so an operator-provided
		// --summary is never silently discarded (Defect 1, 2026-08-19
		// handoff audit): cmd/handoff.go requires --summary and the
		// /handoff skill promises it reaches the successor, but previously
		// nothing was written to disk or mailed for untethered agents.
		// Writ-specific fields (WritID, ActiveWritID, git state) are left
		// empty since there is no writ context to attach them to.
		summary := opts.Summary
		if summary == "" {
			summary = fmt.Sprintf("Session handoff for %s. No active writ.", opts.AgentName)
		}

		recentOutput := ""
		if output, err := sessionMgr.Capture(config.SessionName(opts.World, opts.AgentName), 100); err == nil {
			recentOutput = output
		}

		state := &State{
			AgentName:       opts.AgentName,
			World:           opts.World,
			Role:            role,
			PreviousSession: config.SessionName(opts.World, opts.AgentName),
			Summary:         summary,
			RecentOutput:    recentOutput,
			RecentCommits:   []string{},
			HandedOffAt:     time.Now().UTC(),
		}

		if err := Write(state); err != nil {
			return fmt.Errorf("failed to write handoff file: %w", err)
		}

		// Event emission and audit mail are deferred, mirroring the hasWork
		// branch — see the emitHandoffAudit call near WriteMarker.
		handoffState = state

		// Derive resume state from the state just captured (same as the
		// hasWork branch) so opts.Summary threads through to
		// BuildResumePrime for compact-reason handoffs of untethered agents.
		resumeState = state.BuildResumeState(reason)
	}

	// Cooldown: check the durable last-handoff timestamp to prevent restart
	// storms. Forge is exempt — it may need rapid cycling during active
	// merge processing. Reuse the lastHandoffTS read earlier — it hasn't
	// changed since that read and a second disk round-trip is wasteful.
	// Unlike the old marker-based guard, this survives prime's marker
	// consumption, so it actually covers restart storms that cycle through
	// prime each iteration (2026-08-19 handoff audit) rather than only the
	// pre-prime window.
	if role != "forge" {
		if !lastHandoffTS.IsZero() {
			elapsed := time.Since(lastHandoffTS)
			if elapsed < MinHandoffCooldown {
				remaining := MinHandoffCooldown - elapsed
				fmt.Fprintf(os.Stderr, "handoff: cooldown active (%s remaining), waiting...\n", remaining.Round(time.Second))
				time.Sleep(remaining)
			}
		}
	}

	// Envoy handoff: prompt the agent to save MEMORY.md before cycling.
	//
	// Persistent memory lives OUTSIDE the worktree at <envoyDir>/memory/ via
	// Claude Code's native auto-memory, and the directory survives the cycle
	// automatically — but operators observed that the auto-memory shutdown
	// flow alone produces noticeably worse memory than an explicit "you are
	// about to be cycled, write MEMORY.md now" prompt. The retired brief
	// system had this dance and it proved valuable, so it is back as a
	// sessionsave call.
	//
	// Role gate: only envoys benefit. Outposts are about to resolve or die
	// and have no MEMORY.md; forge and sentinel do not have meaningful
	// agent-authored memory to flush.
	//
	// Delivery is no longer best-effort (2026-08-19 incident): NudgeSession
	// now verifies via pane capture that the save prompt actually left the
	// input area rather than trusting a successful tmux SendKeys call, so an
	// error here is either a confirmed staged-but-unsent prompt or a hard
	// delivery failure (session gone, lock timeout) — never the old silent
	// "swallowed but reported success" case. Proceeding to cycle the session
	// anyway would risk killing the agent's turn with no save warning ever
	// delivered, exactly the incident this writ exists to close. Abort
	// instead: the operator gets a clear error and the session is left
	// running so they can retry or intervene manually.
	//
	// Self-invoked exception: when the agent invoked `sol handoff` on
	// itself, the target session is — by definition — busy executing the
	// very command that would cycle it. Nudging into a session mid-command
	// is inherently racy (this is exactly the swallow scenario NudgeSession
	// now guards against), and the /handoff skill already mandates the
	// agent write MEMORY.md BEFORE invoking handoff, making the extra
	// prompt redundant in this path. Skip it rather than aborting on a
	// failure mode the operator can't do anything about — no operator is
	// present to retry a self-invoked handoff.
	if role == "envoy" {
		if opts.SelfInvoked {
			fmt.Fprintf(os.Stderr, "handoff: self-invoked, skipping save prompt (the /handoff skill requires saving MEMORY.md before invoking)\n")
		} else {
			sessionName := config.SessionName(opts.World, opts.AgentName)
			if err := sessionsave.Prompt(sessionMgr, sessionName, sessionsave.HandoffCyclePrompt, sessionsave.Options{}); err != nil {
				return fmt.Errorf("handoff: save prompt not delivered, aborting cycle to avoid killing the session with unsaved state: %w", err)
			}
		}
	}

	// Write resume state for crash recovery. If the newly cycled session
	// dies before completing, the prefect can use this to call
	// startup.Resume() instead of a bare startup.Launch(), preserving
	// workflow position and claimed resources.
	//
	// L-M3: enforce the "marker BEFORE cycle" recovery invariant. If
	// WriteResumeState fails, do NOT proceed with the cycle — a session
	// that gets cycled and crashes immediately afterward would have no
	// recovery context, and the operator would have no signal that the
	// invariant was violated. The cycleOp uses respawn-pane -k which kills
	// the calling process, so silently continuing would persist a bad state
	// across the cycle. Returning the error lets the caller retry.
	if err := startup.WriteResumeState(opts.World, opts.AgentName, role, resumeState); err != nil {
		return fmt.Errorf("handoff: failed to write resume state (cycle aborted to preserve crash-recovery invariant): %w", err)
	}

	// Build a session operation that uses Cycle (respawn-pane -k) for atomic
	// process replacement, with Stop+Start as fallback. This is safe for
	// self-handoff — respawn-pane -k kills the old process and starts the
	// new one server-side, so the calling process being killed is expected.
	cycleOp := func(name, workdir, cmd string, env map[string]string, role, world string) error {
		if err := sessionMgr.Cycle(name, workdir, cmd, env, role, world); err != nil {
			fmt.Fprintf(os.Stderr, "handoff: cycle failed, falling back to stop+start: %v\n", err)
			if stopErr := sessionMgr.Stop(name, true); stopErr != nil && !errors.Is(stopErr, session.ErrNotFound) {
				fmt.Fprintf(os.Stderr, "handoff: stop also failed: %v\n", stopErr)
			}
			return sessionMgr.Start(name, workdir, cmd, env, role, world)
		}
		return nil
	}

	// Use startup.Resume/Launch for registered roles. This ensures the new
	// session gets system prompt flags, persona, hooks, workflow
	// re-instantiation, and role-specific prime context.
	cfg := startup.ConfigFor(role)
	if cfg == nil {
		return fmt.Errorf("handoff: no startup config registered for role %q", role)
	}

	launchOpts := startup.LaunchOpts{
		SessionOp: cycleOp,
		Sphere:    opts.StartupSphere,
	}

	// Write the durable last-handoff timestamp BEFORE the cycle operation,
	// same "must be on disk before we risk process death" reasoning as
	// WriteResumeState above — and, per the same L-M3 invariant, a failure
	// here must abort rather than warn-and-continue. This file (not the
	// marker) is now what the restart-storm cooldown guard and session_age
	// telemetry depend on; silently losing the write would leave the storm
	// guard disabled for the entire life of the new session, which is
	// exactly the defect this decoupling exists to fix. This intentionally
	// diverges from the marker write just below, which stays warn-and-
	// continue: losing the marker only costs the successor session a
	// cosmetic "fresh session" note, not a safety guard.
	if err := WriteLastHandoff(opts.World, opts.AgentName, role); err != nil {
		return fmt.Errorf("handoff: failed to write last-handoff timestamp (cycle aborted to preserve restart-storm guard invariant): %w", err)
	}

	// Emit the handoff event and send the audit mail now. Every abort gate
	// above (envoy save-prompt delivery, WriteResumeState, the last-handoff
	// timestamp write, missing startup config) has passed, so the cycle is
	// actually happening from here on — this keeps the feed truthful: a
	// "handoff" event means a cycle happened, not that one was merely
	// attempted (Defect 2, 2026-08-19 handoff audit). This must still run
	// before WriteMarker/cycleOp: respawn-pane -k kills the calling process
	// for self-invoked handoffs, so anything placed after the session op is
	// dead on the success path — pushing this later would silently drop it
	// in that path.
	emitHandoffAudit(opts, role, reason, handoffState, sessionAge, sphereStore, logger)

	// Write marker for loop prevention BEFORE the cycle operation.
	// cycleOp uses respawn-pane -k which kills the calling process —
	// any code after the startup call is dead on the success path.
	// The marker must be on disk before we risk process death.
	if err := WriteMarker(opts.World, opts.AgentName, role, reason); err != nil {
		fmt.Fprintf(os.Stderr, "handoff: failed to write marker: %v\n", err)
	}

	var startupErr error
	if reason == "compact" {
		// Compact handoff: fresh conversation with resume context prepended
		// to the role's prime. Do NOT use startup.Resume (which sets
		// --continue) — reloading the conversation that triggered compaction
		// causes an immediate re-compaction loop. The resume prime +
		// auto-memory injection + persona reinstall provide all necessary continuity.
		modifiedCfg := *cfg
		origPrime := modifiedCfg.PrimeBuilder
		modifiedCfg.PrimeBuilder = func(w, a string) string {
			base := ""
			if origPrime != nil {
				base = origPrime(w, a)
			}
			return startup.BuildResumePrime(base, resumeState)
		}
		_, startupErr = startup.Launch(modifiedCfg, opts.World, opts.AgentName, launchOpts)
	} else {
		// Non-compact handoff: use Launch for fresh conversation with
		// role-specific setup (persona, hooks, system prompt, workflow).
		_, startupErr = startup.Launch(*cfg, opts.World, opts.AgentName, launchOpts)
	}
	if startupErr != nil {
		// Clear resume state so the next Respawn doesn't waste a Resume
		// attempt on a dead conversation that never actually started.
		if clearErr := startup.ClearResumeState(opts.World, opts.AgentName, role); clearErr != nil {
			slog.Warn("handoff: failed to clear resume state after startup failure", "error", clearErr)
		}
		// Remove stale handoff artifacts so a future respawn doesn't
		// inject outdated context or trigger loop-prevention guards.
		// Both functions are idempotent on missing files.
		if removeErr := Remove(opts.World, opts.AgentName, role); removeErr != nil {
			slog.Warn("handoff: failed to remove handoff file after startup failure", "error", removeErr)
		}
		if removeErr := RemoveMarker(opts.World, opts.AgentName, role); removeErr != nil {
			slog.Warn("handoff: failed to remove marker after startup failure", "error", removeErr)
		}
		if removeErr := RemoveLastHandoff(opts.World, opts.AgentName, role); removeErr != nil {
			slog.Warn("handoff: failed to remove last-handoff timestamp after startup failure", "error", removeErr)
		}
		return fmt.Errorf("handoff: startup failed: %w", startupErr)
	}

	return nil
}

// emitHandoffAudit emits the handoff event and sends the self-audit mail.
// Exec calls this exactly once, right before WriteMarker/the cycle call —
// after every abort gate that precedes it (envoy save-prompt delivery,
// WriteResumeState, missing startup config) has already succeeded. Placing
// it here rather than at capture time keeps the event feed and mailbox
// truthful: a "handoff" event now means the cycle is actually happening,
// not that a cycle was attempted and then possibly aborted (Defect 2,
// 2026-08-19 handoff audit — the abort gates could previously fire after
// the event/mail were already emitted, leaving a phantom record of a
// handoff that never occurred).
//
// state is the captured/built handoff State (never nil when called from
// Exec); a nil guard is kept here defensively since this is not exported.
func emitHandoffAudit(opts ExecOpts, role, reason string, state *State, sessionAge time.Duration,
	sphereStore SphereStore, logger *events.Logger) {
	if state == nil {
		return
	}

	if logger != nil {
		payload := map[string]string{
			"agent":       opts.AgentName,
			"world":       opts.World,
			"role":        role,
			"reason":      reason,
			"session_age": sessionAge.Round(time.Second).String(),
		}
		if state.WritID != "" {
			payload["writ_id"] = state.WritID
		}
		logger.Emit(events.EventHandoff, "sol", opts.AgentName, "both", payload)
	}

	if sphereStore != nil {
		agentID := fmt.Sprintf("%s/%s", opts.World, opts.AgentName)
		subject := "HANDOFF: (no writ)"
		if state.WritID != "" {
			subject = fmt.Sprintf("HANDOFF: %s", state.WritID)
		}
		body := state.Summary
		if len(state.RecentCommits) > 0 {
			body += "\n\nRecent commits:\n" + strings.Join(state.RecentCommits, "\n")
		}
		if _, err := sphereStore.SendMessage(agentID, agentID, subject, body, 2, "notification"); err != nil {
			fmt.Fprintf(os.Stderr, "handoff: failed to send self-notification: %v\n", err)
		}
	}
}
