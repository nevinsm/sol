package witness

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/events"
	"github.com/nevinsm/gt/internal/hook"
	"github.com/nevinsm/gt/internal/store"
)

// Config holds witness configuration.
type Config struct {
	Rig            string
	PatrolInterval time.Duration // default: 3 minutes
	MaxRespawns    int           // default: 2 (per work item)
	CaptureLines   int           // default: 80 (lines of tmux output to capture)
	AssessCommand  string        // default: "claude -p" (AI assessment command)
	SourceRepo     string        // path to source git repo
	GTHome         string        // GT_HOME path
}

// DefaultConfig returns a Config with default values.
func DefaultConfig(rig, sourceRepo, gtHome string) Config {
	return Config{
		Rig:            rig,
		PatrolInterval: 3 * time.Minute,
		MaxRespawns:    2,
		CaptureLines:   80,
		AssessCommand:  "claude -p",
		SourceRepo:     sourceRepo,
		GTHome:         gtHome,
	}
}

// TownStore is the subset of town store operations the witness needs.
type TownStore interface {
	GetAgent(id string) (*store.Agent, error)
	ListAgents(rig string, state string) ([]store.Agent, error)
	UpdateAgentState(id, state, hookItem string) error
	CreateAgent(name, rig, role string) (string, error)
	SendProtocolMessage(sender, recipient, protoType string, payload any) (string, error)
}

// RigStore is the subset of rig store operations the witness needs.
type RigStore interface {
	GetWorkItem(id string) (*store.WorkItem, error)
	UpdateWorkItem(id string, updates store.WorkItemUpdates) error
}

// SessionChecker abstracts session operations for testability.
type SessionChecker interface {
	Exists(name string) bool
	Capture(name string, lines int) (string, error)
	Start(name, workdir, cmd string, env map[string]string, role, rig string) error
	Stop(name string, force bool) error
	Inject(name string, text string) error
}

// AssessmentResult is the structured output from an AI assessment.
type AssessmentResult struct {
	Status          string `json:"status"`           // progressing, stuck, waiting, idle
	Confidence      string `json:"confidence"`       // high, medium, low
	Reason          string `json:"reason"`
	SuggestedAction string `json:"suggested_action"` // none, nudge, escalate
	NudgeMessage    string `json:"nudge_message"`
}

type assessFunc func(agent store.Agent, sessionName, output string) (*AssessmentResult, error)

type respawnKey struct {
	AgentID    string
	WorkItemID string
}

// Witness monitors polecats in a single rig.
type Witness struct {
	config        Config
	townStore     TownStore
	rigStore      RigStore
	sessions      SessionChecker
	logger        *events.Logger
	respawnCounts map[respawnKey]int
	lastCaptures  map[string]string // agent ID → hash of last captured output
	assessFn      assessFunc        // nil = use real AI call

	// Per-patrol counters, reset at start of each patrol.
	patrolAssessed int
	patrolNudged   int
}

// New creates a new Witness.
func New(cfg Config, town TownStore, rig RigStore,
	sessions SessionChecker, logger *events.Logger) *Witness {
	return &Witness{
		config:        cfg,
		townStore:     town,
		rigStore:      rig,
		sessions:      sessions,
		logger:        logger,
		respawnCounts: make(map[respawnKey]int),
		lastCaptures:  make(map[string]string),
	}
}

// SetAssessFunc sets a custom assessment function for testing.
// When set, this function is called instead of the real AI assessment.
func (w *Witness) SetAssessFunc(fn func(agent store.Agent, sessionName, output string) (*AssessmentResult, error)) {
	w.assessFn = fn
}

func (w *Witness) agentID() string {
	return w.config.Rig + "/witness"
}

// Register registers the witness agent in the town store.
// Agent ID: "{rig}/witness", role: "witness".
// Creates if not exists, reuses if already registered.
func (w *Witness) Register() error {
	_, err := w.townStore.GetAgent(w.agentID())
	if err == nil {
		return nil // already registered
	}
	_, err = w.townStore.CreateAgent("witness", w.config.Rig, "witness")
	return err
}

// Run starts the witness patrol loop. Blocks until context is cancelled.
// Patrols immediately on start, then on each interval.
func (w *Witness) Run(ctx context.Context) error {
	if err := w.Register(); err != nil {
		return fmt.Errorf("failed to register witness: %w", err)
	}

	if err := w.townStore.UpdateAgentState(w.agentID(), "working", ""); err != nil {
		return fmt.Errorf("failed to set witness working: %w", err)
	}

	// Patrol immediately.
	w.patrol()

	ticker := time.NewTicker(w.config.PatrolInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = w.townStore.UpdateAgentState(w.agentID(), "idle", "")
			if w.logger != nil {
				w.logger.Emit(events.EventSessionStop, w.agentID(), w.agentID(), "feed",
					map[string]any{"rig": w.config.Rig, "component": "witness"})
			}
			return nil
		case <-ticker.C:
			w.patrol()
		}
	}
}

// Patrol runs one patrol cycle across all polecats in the rig. Exported for testing.
func (w *Witness) Patrol() error {
	return w.patrol()
}

// patrol runs one patrol cycle across all polecats in the rig.
func (w *Witness) patrol() error {
	agents, err := w.townStore.ListAgents(w.config.Rig, "")
	if err != nil {
		return fmt.Errorf("failed to list agents: %w", err)
	}

	// Filter to polecats only.
	var polecats []store.Agent
	for _, a := range agents {
		if a.Role == "polecat" {
			polecats = append(polecats, a)
		}
	}

	w.patrolAssessed = 0
	w.patrolNudged = 0

	var healthyCount, stalledCount, zombieCount int
	var actionsTaken []string

	for _, agent := range polecats {
		sessionName := dispatch.SessionName(w.config.Rig, agent.Name)
		alive := w.sessions.Exists(sessionName)

		switch {
		case agent.State == "working" && alive:
			// Working agent with live session — check for progress.
			_ = w.checkProgress(agent, sessionName)
			healthyCount++

		case agent.State == "working" && !alive && agent.HookItem != "":
			// Session died while work was hooked — stalled.
			stalledCount++
			_ = w.handleStalled(agent)
			actionsTaken = append(actionsTaken, "stalled:"+agent.Name)

		case agent.State == "idle" && alive && !hook.IsHooked(w.config.Rig, agent.Name):
			// Idle agent with live session and no hook — zombie.
			zombieCount++
			_ = w.handleZombie(agent)
			actionsTaken = append(actionsTaken, "zombie:"+agent.Name)

		case agent.State == "stalled":
			// Already stalled — retry recovery.
			stalledCount++
			_ = w.handleStalled(agent)
			actionsTaken = append(actionsTaken, "stalled:"+agent.Name)

		default:
			// Healthy idle or no session needed.
			healthyCount++
		}
	}

	if w.logger != nil {
		w.logger.Emit(events.EventPatrol, w.agentID(), w.agentID(), "feed",
			map[string]any{
				"rig":      w.config.Rig,
				"total":    len(polecats),
				"healthy":  healthyCount,
				"stalled":  stalledCount,
				"zombies":  zombieCount,
				"assessed": w.patrolAssessed,
				"nudged":   w.patrolNudged,
				"actions":  actionsTaken,
			})
	}

	return nil
}

// checkProgress checks whether a working agent with a live session is making progress.
// If the tmux output hasn't changed since the last patrol, triggers AI assessment.
func (w *Witness) checkProgress(agent store.Agent, sessionName string) error {
	output, err := w.sessions.Capture(sessionName, w.config.CaptureLines)
	if err != nil {
		return nil // can't capture, skip assessment
	}

	hash := sha256Hash(output)
	lastHash, seen := w.lastCaptures[agent.ID]
	w.lastCaptures[agent.ID] = hash

	if !seen {
		return nil // first patrol for this agent, establish baseline
	}
	if hash != lastHash {
		return nil // output changed, agent is making progress
	}

	// No change since last patrol — assess with AI.
	return w.assessAgent(agent, sessionName, output)
}

func sha256Hash(s string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(s)))
}

// assessAgent uses an AI model to evaluate a potentially stuck agent.
func (w *Witness) assessAgent(agent store.Agent, sessionName, capturedOutput string) error {
	w.patrolAssessed++

	var result *AssessmentResult
	var err error

	if w.assessFn != nil {
		result, err = w.assessFn(agent, sessionName, capturedOutput)
	} else {
		result, err = w.runAssessment(agent, capturedOutput)
	}
	if err != nil {
		// AI call failed — log and move on, don't block patrol.
		if w.logger != nil {
			w.logger.Emit("assess_error", w.agentID(), agent.ID, "audit",
				map[string]any{"error": err.Error()})
		}
		return nil
	}

	if w.logger != nil {
		w.logger.Emit(events.EventAssess, w.agentID(), agent.ID, "both",
			map[string]any{
				"agent":      agent.ID,
				"status":     result.Status,
				"confidence": result.Confidence,
				"action":     result.SuggestedAction,
				"reason":     result.Reason,
			})
	}

	return w.actOnAssessment(agent, sessionName, *result)
}

func (w *Witness) runAssessment(agent store.Agent, capturedOutput string) (*AssessmentResult, error) {
	prompt := buildAssessmentPrompt(agent, capturedOutput)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", w.config.AssessCommand)
	cmd.Stdin = strings.NewReader(prompt)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("assessment command failed: %w", err)
	}

	var result AssessmentResult
	if err := json.Unmarshal(out, &result); err != nil {
		// Couldn't parse response — try to extract JSON from output.
		extracted, extractErr := extractJSON(out)
		if extractErr != nil {
			return nil, fmt.Errorf("unparseable assessment output: %w", err)
		}
		return &extracted, nil
	}

	return &result, nil
}

func buildAssessmentPrompt(agent store.Agent, capturedOutput string) string {
	return fmt.Sprintf(`You are a witness agent monitoring AI coding agents in a multi-agent
orchestration system. An agent's tmux session output has not changed
since the last patrol cycle (3 minutes ago). Analyze the session output
below and determine the agent's status.

Agent: %s (ID: %s)
Work item: %s
Session output (last 80 lines):
---
%s
---

Respond with ONLY a JSON object (no markdown, no explanation):
{
    "status": "progressing|stuck|waiting|idle",
    "confidence": "high|medium|low",
    "reason": "brief explanation of what the agent appears to be doing",
    "suggested_action": "none|nudge|escalate",
    "nudge_message": "if suggested_action is nudge, the message to send"
}

Status meanings:
- "progressing": Agent is actively working (e.g., long compilation,
  large file write, waiting for a tool call to complete). No action
  needed despite unchanged output.
- "stuck": Agent appears confused, looping, or unable to make progress.
  A nudge with guidance may help.
- "waiting": Agent is waiting for external input or a resource. May
  need a nudge to check its mail or retry.
- "idle": Agent appears to have finished or is not doing anything.
  May be a zombie or may have completed work without calling gt done.

Only suggest "escalate" if the situation requires human intervention
(e.g., repeated failures, auth issues, infrastructure problems).`, agent.Name, agent.ID, agent.HookItem, capturedOutput)
}

func extractJSON(data []byte) (AssessmentResult, error) {
	s := string(data)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < 0 || end <= start {
		return AssessmentResult{}, fmt.Errorf("no JSON object found in output")
	}
	var result AssessmentResult
	if err := json.Unmarshal([]byte(s[start:end+1]), &result); err != nil {
		return AssessmentResult{}, err
	}
	return result, nil
}

// actOnAssessment acts on an AI assessment result.
func (w *Witness) actOnAssessment(agent store.Agent, sessionName string,
	result AssessmentResult) error {

	// Low confidence = no action. Better to wait another patrol cycle
	// than to act on uncertain assessment.
	if result.Confidence == "low" {
		return nil
	}

	switch result.SuggestedAction {
	case "none":
		// Agent is progressing or we're not confident — do nothing.
		return nil

	case "nudge":
		// Inject nudge message into the agent's session.
		if err := w.sessions.Inject(sessionName, result.NudgeMessage); err != nil {
			return fmt.Errorf("failed to inject nudge into %s: %w", sessionName, err)
		}
		w.patrolNudged++

		if w.logger != nil {
			w.logger.Emit(events.EventNudge, w.agentID(), agent.ID, "both",
				map[string]any{
					"agent":   agent.ID,
					"message": result.NudgeMessage,
					"reason":  result.Reason,
				})
		}

		// Send informational mail to operator.
		if _, err := w.townStore.SendProtocolMessage(
			w.agentID(), "operator",
			store.ProtoRecoveryNeeded,
			store.RecoveryNeededPayload{
				AgentID:    agent.ID,
				WorkItemID: agent.HookItem,
				Reason:     fmt.Sprintf("nudged: %s", result.Reason),
			},
		); err != nil && w.logger != nil {
			w.logger.Emit("mail_error", w.agentID(), agent.ID, "audit",
				map[string]any{"error": err.Error()})
		}

	case "escalate":
		// Send RECOVERY_NEEDED protocol message to operator.
		if _, err := w.townStore.SendProtocolMessage(
			w.agentID(), "operator",
			store.ProtoRecoveryNeeded,
			store.RecoveryNeededPayload{
				AgentID:    agent.ID,
				WorkItemID: agent.HookItem,
				Reason:     result.Reason,
			},
		); err != nil && w.logger != nil {
			w.logger.Emit("mail_error", w.agentID(), agent.ID, "audit",
				map[string]any{"error": err.Error()})
		}

		if w.logger != nil {
			w.logger.Emit(events.EventStalled, w.agentID(), agent.ID, "both",
				map[string]any{
					"agent":     agent.ID,
					"reason":    result.Reason,
					"escalated": true,
				})
		}
	}

	return nil
}

// handleStalled handles a polecat whose session died while work was hooked.
func (w *Witness) handleStalled(agent store.Agent) error {
	key := respawnKey{AgentID: agent.ID, WorkItemID: agent.HookItem}
	attempts := w.respawnCounts[key]

	if attempts >= w.config.MaxRespawns {
		return w.returnWorkToOpen(agent)
	}

	w.respawnCounts[key]++
	return w.respawnAgent(agent)
}

// respawnAgent restarts a crashed agent's tmux session.
// The witness does NOT re-sling or re-prime. The hook file is durable,
// and the Claude Code SessionStart hook fires gt prime automatically (GUPP).
func (w *Witness) respawnAgent(agent store.Agent) error {
	// Ensure agent state is working.
	if err := w.townStore.UpdateAgentState(agent.ID, "working", agent.HookItem); err != nil {
		return fmt.Errorf("failed to set agent %s working: %w", agent.ID, err)
	}

	sessionName := dispatch.SessionName(w.config.Rig, agent.Name)
	workdir := dispatch.WorktreePath(w.config.Rig, agent.Name)
	env := map[string]string{
		"GT_HOME":  config.Home(),
		"GT_RIG":   w.config.Rig,
		"GT_AGENT": agent.Name,
	}

	if err := w.sessions.Start(sessionName, workdir,
		"claude --dangerously-skip-permissions", env, agent.Role, w.config.Rig); err != nil {
		return fmt.Errorf("failed to start session for %s: %w", agent.Name, err)
	}

	key := respawnKey{AgentID: agent.ID, WorkItemID: agent.HookItem}
	attempts := w.respawnCounts[key]

	if w.logger != nil {
		w.logger.Emit(events.EventRespawn, w.agentID(), agent.ID, "both",
			map[string]any{
				"agent":     agent.ID,
				"work_item": agent.HookItem,
				"attempt":   attempts,
			})
	}

	// Send informational protocol message.
	if _, err := w.townStore.SendProtocolMessage(
		w.agentID(), "operator",
		store.ProtoRecoveryNeeded,
		store.RecoveryNeededPayload{
			AgentID:    agent.ID,
			WorkItemID: agent.HookItem,
			Reason:     fmt.Sprintf("respawned (attempt %d)", attempts),
			Attempts:   attempts,
		},
	); err != nil && w.logger != nil {
		w.logger.Emit("mail_error", w.agentID(), agent.ID, "audit",
			map[string]any{"error": err.Error()})
	}

	return nil
}

// returnWorkToOpen returns a stalled agent's work item to the open pool
// after exceeding max respawn attempts.
func (w *Witness) returnWorkToOpen(agent store.Agent) error {
	// 1. Update work item: status → open, clear assignee.
	if agent.HookItem != "" {
		if err := w.rigStore.UpdateWorkItem(agent.HookItem, store.WorkItemUpdates{
			Status:   "open",
			Assignee: "-", // "-" clears assignee
		}); err != nil {
			return fmt.Errorf("failed to return work item %s to open: %w", agent.HookItem, err)
		}
	}

	// 2. Set agent state → idle, clear hook_item.
	// Done before clearing hook so a crash leaves the agent idle with a stale
	// hook (harmless — next dispatch overwrites it) rather than "working" with
	// no hook (would trigger a wasted respawn).
	if err := w.townStore.UpdateAgentState(agent.ID, "idle", ""); err != nil {
		return fmt.Errorf("failed to set agent %s idle: %w", agent.ID, err)
	}

	// 3. Clear the hook file.
	if err := hook.Clear(w.config.Rig, agent.Name); err != nil {
		return fmt.Errorf("failed to clear hook for agent %s: %w", agent.ID, err)
	}

	// 4. Clear respawn count.
	key := respawnKey{AgentID: agent.ID, WorkItemID: agent.HookItem}
	delete(w.respawnCounts, key)

	// 5. Emit stalled event with recovered: false.
	if w.logger != nil {
		w.logger.Emit(events.EventStalled, w.agentID(), agent.ID, "both",
			map[string]any{
				"agent":     agent.ID,
				"work_item": agent.HookItem,
				"recovered": false,
			})
	}

	// 6. Send RECOVERY_NEEDED protocol message to operator.
	if _, err := w.townStore.SendProtocolMessage(
		w.agentID(), "operator",
		store.ProtoRecoveryNeeded,
		store.RecoveryNeededPayload{
			AgentID:    agent.ID,
			WorkItemID: agent.HookItem,
			Reason:     fmt.Sprintf("max respawns (%d) exceeded, work returned to open", w.config.MaxRespawns),
			Attempts:   w.config.MaxRespawns,
		},
	); err != nil && w.logger != nil {
		w.logger.Emit("mail_error", w.agentID(), agent.ID, "audit",
			map[string]any{"error": err.Error()})
	}

	return nil
}

// handleZombie handles a polecat with a live session but no hooked work.
func (w *Witness) handleZombie(agent store.Agent) error {
	sessionName := dispatch.SessionName(w.config.Rig, agent.Name)
	if err := w.sessions.Stop(sessionName, false); err != nil {
		return fmt.Errorf("failed to stop zombie session %s: %w", sessionName, err)
	}
	return nil
}
