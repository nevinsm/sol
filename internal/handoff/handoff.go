package handoff

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/nevinsm/sol/internal/workflow"
)

// State captures an agent's context at the moment of handoff.
type State struct {
	WorkItemID       string    `json:"work_item_id"`
	AgentName        string    `json:"agent_name"`
	World            string    `json:"world"`
	Role             string    `json:"role,omitempty"`
	PreviousSession  string    `json:"previous_session"`
	Summary          string    `json:"summary"`
	RecentOutput     string    `json:"recent_output"`
	RecentCommits    []string  `json:"recent_commits"`
	WorkflowStep     string    `json:"workflow_step"`
	WorkflowProgress string    `json:"workflow_progress"`
	HandedOffAt      time.Time `json:"handed_off_at"`
	Consumed         bool      `json:"consumed,omitempty"`
	GitStatus        string    `json:"git_status,omitempty"`
	GitStash         string    `json:"git_stash,omitempty"`
	DiffStat         string    `json:"diff_stat,omitempty"`
	StepDescription  string    `json:"step_description,omitempty"`
}

// SessionManager is the subset of session.Manager used by handoff.
type SessionManager interface {
	Exists(name string) bool
	Inject(name string, text string, submit bool) error
	Capture(name string, lines int) (string, error)
	Stop(name string, force bool) error
	Start(name, workdir, cmd string, env map[string]string, role, world string) error
	Cycle(name, workdir, cmd string, env map[string]string, role, world string) error
}

// SphereStore is the subset of store.Store used by handoff.
type SphereStore interface {
	SendMessage(sender, recipient, subject, body string, priority int, msgType string) (string, error)
}

// HandoffPath returns the path to an agent's handoff state file.
// Uses role-aware directory: outposts/{name}/ for agents, envoys/{name}/ for envoys, etc.
func HandoffPath(world, agentName, role string) string {
	return filepath.Join(config.AgentDir(world, agentName, role), ".handoff.json")
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
		return err
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
	Role         string // agent role (default: "agent")
	Summary      string // agent-provided summary (optional)
	CaptureLines int    // lines of tmux output to capture (default: 100)
	CommitCount  int    // recent commits to include (default: 10)
	WorktreeDir  string // explicit worktree path (uses config.WorktreePath if empty)
}

// Capture gathers the current state of an agent's session.
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
		role = "agent"
	}

	// 1. Read tether file to get work item ID.
	workItemID, err := tether.Read(opts.World, opts.AgentName, role)
	if err != nil {
		return nil, fmt.Errorf("failed to read tether: %w", err)
	}
	if workItemID == "" {
		return nil, fmt.Errorf("no work tethered for agent %q in world %q", opts.AgentName, opts.World)
	}

	// 2. Session name.
	sessionName := config.SessionName(opts.World, opts.AgentName)

	// 3. Capture tmux output.
	recentOutput := ""
	if sessionCapture != nil {
		output, err := sessionCapture(sessionName, opts.CaptureLines)
		if err == nil {
			recentOutput = output
		}
	}

	// 4. Capture recent git commits from worktree.
	worktreeDir := opts.WorktreeDir
	if worktreeDir == "" {
		worktreeDir = config.WorktreePath(opts.World, opts.AgentName)
	}
	var recentCommits []string
	if gitLog != nil {
		commits, err := gitLog(worktreeDir, opts.CommitCount)
		if err == nil {
			recentCommits = commits
		}
	}
	if recentCommits == nil {
		recentCommits = []string{}
	}

	// 5. Capture git status, stash, and diff stat from worktree.
	gitStatus := gitShort(worktreeDir, "status", "--short")
	gitStash := gitShort(worktreeDir, "stash", "list")
	diffStat := gitShort(worktreeDir, "diff", "--stat")

	// 6. Read workflow state (if present).
	workflowStep := ""
	workflowProgress := ""
	stepDescription := ""
	wfState, err := workflow.ReadState(opts.World, opts.AgentName, role)
	if err == nil && wfState != nil && wfState.Status == "running" {
		workflowStep = wfState.CurrentStep
		completed := len(wfState.Completed)
		// Try to get total steps from instance.
		instance, _ := workflow.ReadInstance(opts.World, opts.AgentName, role)
		if instance != nil {
			steps, _ := workflow.ListSteps(opts.World, opts.AgentName, role)
			if steps != nil {
				workflowProgress = fmt.Sprintf("%d/%d complete", completed, len(steps))
			}
		}
		if workflowProgress == "" {
			workflowProgress = fmt.Sprintf("%d steps complete", completed)
		}
		// Capture step title/description for richer context.
		currentStep, _ := workflow.ReadCurrentStep(opts.World, opts.AgentName, role)
		if currentStep != nil {
			stepDescription = currentStep.Title
		}
	}

	// 7. Auto-generate summary if not provided.
	summary := opts.Summary
	if summary == "" {
		summary = fmt.Sprintf("Session handoff for %s. Working on %s.", opts.AgentName, workItemID)
		if len(recentCommits) > 0 {
			summary += fmt.Sprintf(" Last commit: %s", recentCommits[0])
		}
	}

	return &State{
		WorkItemID:       workItemID,
		AgentName:        opts.AgentName,
		World:            opts.World,
		Role:             role,
		PreviousSession:  sessionName,
		Summary:          summary,
		RecentOutput:     recentOutput,
		RecentCommits:    recentCommits,
		WorkflowStep:     workflowStep,
		WorkflowProgress: workflowProgress,
		HandedOffAt:      time.Now().UTC(),
		GitStatus:        gitStatus,
		GitStash:         gitStash,
		DiffStat:         diffStat,
		StepDescription:  stepDescription,
	}, nil
}

// Write serializes the handoff state to the agent's handoff file.
// Creates parent directories if needed.
func Write(state *State) error {
	role := state.Role
	if role == "" {
		role = "agent"
	}
	path := HandoffPath(state.World, state.AgentName, role)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create handoff directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal handoff state: %w", err)
	}

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("failed to write handoff file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("failed to write handoff file: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("failed to sync handoff file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to close handoff file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to commit handoff file: %w", err)
	}
	return nil
}

// Read deserializes the handoff state from the agent's handoff file.
// Returns nil, nil if no handoff file exists.
func Read(world, agentName, role string) (*State, error) {
	data, err := os.ReadFile(HandoffPath(world, agentName, role))
	if err != nil {
		if os.IsNotExist(err) {
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
func gitShort(worktreeDir string, args ...string) string {
	if _, err := os.Stat(worktreeDir); os.IsNotExist(err) {
		return ""
	}
	fullArgs := append([]string{"-C", worktreeDir}, args...)
	cmd := exec.Command("git", fullArgs...)
	out, err := cmd.Output()
	if err != nil {
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
	return os.WriteFile(path, []byte(content), 0o644)
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

// BriefSavePrompt is the message injected into a session before handoff cycling
// for roles that use briefs (envoy, governor).
const BriefSavePrompt = "[sol] Session cycling due to context pressure. " +
	"Update .brief/memory.md NOW with current state, decisions, and next steps."

const briefSaveCaptureLines = 50

// briefSave injects a brief-save prompt and waits for the agent to respond.
// Uses shorter timeouts than GracefulStop since we only need the brief update.
// Returns nil even on timeout — a stale brief is better than a stuck handoff.
func briefSave(sessionName string, mgr SessionManager,
	pollInterval time.Duration, stableThreshold int, maxTimeout time.Duration) {

	// Inject the brief save prompt.
	fmt.Fprintf(os.Stderr, "handoff: requesting brief save before cycling...\n")
	if err := mgr.Inject(sessionName, BriefSavePrompt, true); err != nil {
		fmt.Fprintf(os.Stderr, "handoff: failed to inject brief save prompt: %v\n", err)
		return
	}

	// Poll for output stability.
	deadline := time.Now().Add(maxTimeout)
	var lastHash string
	unchangedCount := 0

	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)

		if !mgr.Exists(sessionName) {
			return // Session exited on its own.
		}

		content, err := mgr.Capture(sessionName, briefSaveCaptureLines)
		if err != nil {
			continue
		}

		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))

		if lastHash != "" && hash == lastHash {
			unchangedCount++
		} else {
			unchangedCount = 0
		}
		lastHash = hash

		if unchangedCount >= stableThreshold {
			fmt.Fprintf(os.Stderr, "handoff: brief save complete (output stable)\n")
			return
		}
	}

	fmt.Fprintf(os.Stderr, "handoff: brief save timed out after %s, proceeding with handoff\n", maxTimeout)
}

// roleUsesBrief returns true if the role maintains a brief (.brief/memory.md).
func roleUsesBrief(role string) bool {
	return role == "envoy" || role == "governor"
}

// ExecOpts configures the handoff execution.
type ExecOpts struct {
	World       string
	AgentName   string
	Summary     string // optional agent-provided summary
	Role        string // agent role: "agent", "envoy", "governor", "forge" (default: "agent")
	WorktreeDir string // explicit worktree path (required for non-outpost roles)
	Reason      string // handoff reason: "compact", "manual", "health-check" (default: "unknown")
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
// For non-outpost agents (envoy, governor, forge) without tethered work,
// steps 1-3 are skipped — the session is simply cycled, and the existing
// SessionStart hook re-injects context from durable state.
func Exec(opts ExecOpts, sessionMgr SessionManager, sphereStore SphereStore,
	logger *events.Logger) error {

	role := opts.Role
	if role == "" {
		role = "agent"
	}

	reason := opts.Reason
	if reason == "" {
		reason = "unknown"
	}

	sessionName := config.SessionName(opts.World, opts.AgentName)

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

	// Try to capture state from tethered work (outposts and envoys with active work).
	workItemID, _ := tether.Read(opts.World, opts.AgentName, role)
	hasTether := workItemID != ""

	if hasTether {
		// Full capture + handoff file + notification for tethered agents.
		state, err := Capture(CaptureOpts{
			World:       opts.World,
			AgentName:   opts.AgentName,
			Role:        role,
			Summary:     opts.Summary,
			WorktreeDir: worktreeDir,
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
				"work_item_id": state.WorkItemID,
				"agent":        opts.AgentName,
				"world":        opts.World,
				"role":         role,
				"reason":       reason,
				"session_age":  sessionAge.Round(time.Second).String(),
			})
		}

		// Send handoff mail to self for audit trail.
		if sphereStore != nil {
			agentID := fmt.Sprintf("%s/%s", opts.World, opts.AgentName)
			body := state.Summary
			if len(state.RecentCommits) > 0 {
				body += "\n\nRecent commits:\n" + strings.Join(state.RecentCommits, "\n")
			}
			if state.WorkflowProgress != "" {
				body += "\n\nWorkflow: " + state.WorkflowProgress
			}
			subject := fmt.Sprintf("HANDOFF: %s", state.WorkItemID)
			if _, err := sphereStore.SendMessage(agentID, agentID, subject, body, 2, "notification"); err != nil {
				fmt.Fprintf(os.Stderr, "handoff: failed to send self-notification: %v\n", err)
			}
		}
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
	}

	// Check for resolve lock — if resolve is in progress, skip the handoff.
	// Resolve is about to kill the session anyway; we just need the context
	// to survive long enough to finish the resolve sequence.
	resolveLock := filepath.Join(config.AgentDir(opts.World, opts.AgentName, role), ".resolve_in_progress")
	if _, err := os.Stat(resolveLock); err == nil {
		fmt.Fprintf(os.Stderr, "handoff: resolve in progress, deferring to compaction\n")
		return nil
	}

	// Cooldown: check marker timestamp to prevent restart storms.
	// Forge and governor are exempt — they may need rapid cycling during active merge processing.
	if role != "forge" && role != "governor" {
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

	// For roles that use briefs, prompt the agent to save its brief before cycling.
	// This ensures the successor session gets fresh context. Uses shorter timeouts
	// than GracefulStop since we only need the brief update, not a full wrap-up.
	if roleUsesBrief(role) {
		briefDir := filepath.Join(config.AgentDir(opts.World, opts.AgentName, role), ".brief")
		if _, err := os.Stat(briefDir); err == nil {
			if os.Getenv("SOL_SESSION_COMMAND") != "" {
				// Test stub sessions: aggressive timeouts.
				briefSave(sessionName, sessionMgr, 50*time.Millisecond, 2, 200*time.Millisecond)
			} else {
				briefSave(sessionName, sessionMgr, 5*time.Second, 3, 30*time.Second)
			}
		}
	}

	// Cycle the session atomically using respawn-pane. This is safe for
	// self-handoff — respawn-pane -k kills the old process and starts the
	// new one server-side, so the calling process being killed is expected.
	claudeConfigDir, err := config.EnsureClaudeConfigDir(config.WorldDir(opts.World), role, opts.AgentName)
	if err != nil {
		return err
	}
	env := map[string]string{
		"SOL_HOME":         config.Home(),
		"SOL_WORLD":        opts.World,
		"SOL_AGENT":        opts.AgentName,
		"CLAUDE_CONFIG_DIR": claudeConfigDir,
	}
	prompt := fmt.Sprintf("Agent %s, world %s (handoff). If no context appears, run: sol prime --world=%s --agent=%s",
		opts.AgentName, opts.World, opts.World, opts.AgentName)
	settingsPath := config.SettingsPath(worktreeDir)

	// Use --continue for compact-triggered handoffs to resume with compressed
	// context from the predecessor session. See BuildSessionCommandContinue
	// for detailed findings on --continue behavior.
	var sessionCmd string
	if opts.Reason == "compact" {
		sessionCmd = config.BuildSessionCommandContinue(settingsPath, prompt)
	} else {
		sessionCmd = config.BuildSessionCommand(settingsPath, prompt)
	}
	if err := sessionMgr.Cycle(sessionName, worktreeDir, sessionCmd, env, role, opts.World); err != nil {
		// Cycle failed — fall back to Stop+Start.
		fmt.Fprintf(os.Stderr, "handoff: cycle failed, falling back to stop+start: %v\n", err)
		if stopErr := sessionMgr.Stop(sessionName, true); stopErr != nil {
			fmt.Fprintf(os.Stderr, "handoff: stop also failed: %v\n", stopErr)
		}
		if startErr := sessionMgr.Start(sessionName, worktreeDir, sessionCmd, env, role, opts.World); startErr != nil {
			return fmt.Errorf("handoff: fallback start also failed: %w", startErr)
		}
	}

	// Write marker for loop prevention. The new session's prime will detect
	// this and warn the agent not to re-trigger handoff immediately.
	if err := WriteMarker(opts.World, opts.AgentName, role, reason); err != nil {
		fmt.Fprintf(os.Stderr, "handoff: failed to write marker: %v\n", err)
	}

	return nil
}
