package handoff

import (
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
}

// SessionManager is the subset of session.Manager used by handoff.
type SessionManager interface {
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

// HasHandoff returns true if a handoff file exists for this agent.
func HasHandoff(world, agentName, role string) bool {
	_, err := os.Stat(HandoffPath(world, agentName, role))
	return err == nil
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

	// 5. Read workflow state (if present).
	workflowStep := ""
	workflowProgress := ""
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
	}

	// 6. Auto-generate summary if not provided.
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

// ExecOpts configures the handoff execution.
type ExecOpts struct {
	World       string
	AgentName   string
	Summary     string // optional agent-provided summary
	Role        string // agent role: "agent", "envoy", "governor", "forge" (default: "agent")
	WorktreeDir string // explicit worktree path (required for non-outpost roles)
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

	sessionName := config.SessionName(opts.World, opts.AgentName)

	// Determine worktree directory.
	worktreeDir := opts.WorktreeDir
	if worktreeDir == "" {
		worktreeDir = config.WorktreePath(opts.World, opts.AgentName)
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
				"agent": opts.AgentName,
				"world": opts.World,
				"role":  role,
			})
		}
	}

	// Cycle the session atomically using respawn-pane. This is safe for
	// self-handoff — respawn-pane -k kills the old process and starts the
	// new one server-side, so the calling process being killed is expected.
	env := map[string]string{
		"SOL_HOME":  config.Home(),
		"SOL_WORLD": opts.World,
		"SOL_AGENT": opts.AgentName,
	}
	sessionCmd := config.BuildSessionCommand(
		config.SettingsPath(worktreeDir),
		fmt.Sprintf("Agent %s, world %s (handoff). If no context appears, run: sol prime --world=%s --agent=%s",
			opts.AgentName, opts.World, opts.World, opts.AgentName),
	)
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

	return nil
}
