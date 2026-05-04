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

// isResolveInProgress returns true if any resolve lock file exists in the agent directory.
// Checks both the shared .resolve_in_progress file (outpost agents) and per-writ
// .resolve_in_progress.{writID} files (persistent agents with concurrent resolves).
func isResolveInProgress(agentDir string) bool {
	if _, err := os.Stat(filepath.Join(agentDir, ".resolve_in_progress")); err == nil {
		return true
	}
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
	if writID == "" {
		tetherID, err := tether.Read(opts.World, opts.AgentName, role)
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
	out, err := cmd.Output()
	if err != nil {
		slog.Warn("handoff: gitShort command failed",
			"worktree", worktreeDir,
			"args", args,
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

// BuildResumeState extracts a startup.ResumeState from a captured handoff State.
func (s *State) BuildResumeState(reason string) startup.ResumeState {
	rs := startup.ResumeState{
		ClaimedResource: s.WritID,
		Reason:          reason,
		Summary:         s.Summary,
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

	// Fallback: read claimed work from tether.
	writID, _ := tether.Read(world, agent, role)
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

	// StartupSphere is an optional sphere store for the startup.Resume/Launch
	// path. When nil, startup opens its own. Exposed for testing.
	StartupSphere startup.SphereStore
}

// Exec performs the full handoff sequence:
// 1. Capture current state (if tethered work exists)
// 2. Write handoff file (if tethered work exists)
// 3. Send handoff mail to self (audit trail, if tethered)
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
	if isResolveInProgress(config.AgentDir(opts.World, opts.AgentName, role)) {
		fmt.Fprintf(os.Stderr, "handoff: resolve in progress, deferring to compaction\n")
		return nil
	}

	// Determine worktree directory.
	worktreeDir := opts.WorktreeDir
	if worktreeDir == "" {
		worktreeDir = config.WorktreePath(opts.World, opts.AgentName)
	}

	// Calculate session age from the last handoff marker (time since last handoff/start).
	var sessionAge time.Duration
	markerTS, _, _ := ReadMarker(opts.World, opts.AgentName, role)
	if !markerTS.IsZero() {
		sessionAge = time.Since(markerTS)
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
	// tether.Read for outposts that pre-date the active_writ field. This
	// matches dispatch.Resolve's own choice so both paths target the same
	// lock file.
	writIDForLock := activeWritID
	if writIDForLock == "" && hasTether {
		if tetherID, err := tether.Read(opts.World, opts.AgentName, role); err == nil {
			writIDForLock = tetherID
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

		// Emit event after writing handoff file (before stopping session).
		if logger != nil {
			logger.Emit(events.EventHandoff, "sol", opts.AgentName, "both", map[string]string{
				"writ_id":     state.WritID,
				"agent":       opts.AgentName,
				"world":       opts.World,
				"role":        role,
				"reason":      reason,
				"session_age": sessionAge.Round(time.Second).String(),
			})
		}

		// Send handoff mail to self for audit trail.
		if sphereStore != nil {
			agentID := fmt.Sprintf("%s/%s", opts.World, opts.AgentName)
			body := state.Summary
			if len(state.RecentCommits) > 0 {
				body += "\n\nRecent commits:\n" + strings.Join(state.RecentCommits, "\n")
			}
			subject := fmt.Sprintf("HANDOFF: %s", state.WritID)
			if _, err := sphereStore.SendMessage(agentID, agentID, subject, body, 2, "notification"); err != nil {
				fmt.Fprintf(os.Stderr, "handoff: failed to send self-notification: %v\n", err)
			}
		}

		// Derive resume state from already-captured handoff state
		// to avoid redundant disk reads.
		resumeState = state.BuildResumeState(reason)
	} else {
		// No tether — emit event only.
		if logger != nil {
			logger.Emit(events.EventHandoff, "sol", opts.AgentName, "both", map[string]string{
				"agent":       opts.AgentName,
				"world":       opts.World,
				"role":        role,
				"reason":      reason,
				"session_age": sessionAge.Round(time.Second).String(),
			})
		}

		// No captured state available — read durable state from disk
		// (workflow state, active writ) for roles that may
		// have workflows without tethers.
		resumeState = CaptureResumeState(opts.World, opts.AgentName, role, reason, sphereStore)
	}

	// Cooldown: check marker timestamp to prevent restart storms.
	// Forge is exempt — it may need rapid cycling during active merge processing.
	if role != "forge" {
		markerTS, _, _ := ReadMarker(opts.World, opts.AgentName, role)
		if !markerTS.IsZero() {
			elapsed := time.Since(markerTS)
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
	// best-effort sessionsave call.
	//
	// Role gate: only envoys benefit. Outposts are about to resolve or die
	// and have no MEMORY.md; forge and sentinel do not have meaningful
	// agent-authored memory to flush.
	if role == "envoy" {
		sessionName := config.SessionName(opts.World, opts.AgentName)
		if err := sessionsave.Prompt(sessionMgr, sessionName, sessionsave.HandoffCyclePrompt, sessionsave.Options{}); err != nil {
			fmt.Fprintf(os.Stderr, "handoff: sessionsave prompt failed: %v\n", err)
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
		return fmt.Errorf("handoff: startup failed: %w", startupErr)
	}

	return nil
}
