package sentinel

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/nevinsm/sol/internal/workflow"
)

// Config holds sentinel configuration.
type Config struct {
	World              string
	PatrolInterval     time.Duration // default: 3 minutes
	MaxRespawns        int           // default: 2 (per work item)
	MaxRecastAttempts  int           // default: 3 (per failed MR work item)
	CaptureLines       int           // default: 80 (lines of tmux output to capture)
	AssessCommand      string        // default: "claude -p" (AI assessment command)
	SourceRepo         string        // path to source git repo
	SolHome            string        // SOL_HOME path
	IdleReapTimeout    time.Duration // default: 10 minutes — reap idle agents older than this
}

// DefaultConfig returns a Config with default values.
func DefaultConfig(world, sourceRepo, solHome string) Config {
	return Config{
		World:             world,
		PatrolInterval:    3 * time.Minute,
		MaxRespawns:       2,
		MaxRecastAttempts: 3,
		CaptureLines:      80,
		AssessCommand:     "claude -p",
		SourceRepo:        sourceRepo,
		SolHome:           solHome,
		IdleReapTimeout:   10 * time.Minute,
	}
}

// SphereStore is the subset of sphere store operations the sentinel needs.
type SphereStore interface {
	GetAgent(id string) (*store.Agent, error)
	ListAgents(world string, state string) ([]store.Agent, error)
	UpdateAgentState(id, state, tetherItem string) error
	CreateAgent(name, world, role string) (string, error)
	EnsureAgent(name, world, role string) error
	DeleteAgent(id string) error
	SendProtocolMessage(sender, recipient, protoType string, payload any) (string, error)
}

// WorldStore is the subset of world store operations the sentinel needs.
type WorldStore interface {
	GetWorkItem(id string) (*store.WorkItem, error)
	UpdateWorkItem(id string, updates store.WorkItemUpdates) error
	ListMergeRequests(phase string) ([]store.MergeRequest, error)
}

// SessionChecker abstracts session operations for testability.
type SessionChecker interface {
	Exists(name string) bool
	Capture(name string, lines int) (string, error)
	Start(name, workdir, cmd string, env map[string]string, role, world string) error
	Stop(name string, force bool) error
	Inject(name string, text string, submit bool) error
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

// CastResult holds the output of a successful cast operation (matches dispatch.CastResult).
type CastResult struct {
	WorkItemID  string
	AgentName   string
	SessionName string
	WorktreeDir string
}

type respawnKey struct {
	AgentID    string
	WorkItemID string
}

// Sentinel monitors agents in a single world.
type Sentinel struct {
	config        Config
	sphereStore   SphereStore
	worldStore    WorldStore
	sessions      SessionChecker
	logger        *events.Logger
	respawnCounts map[respawnKey]int
	recastCounts  map[string]int    // work item ID → recast attempt count
	lastCaptures  map[string]string // agent ID → hash of last captured output
	assessFn      assessFunc        // nil = use real AI call
	castFn        func(workItemID string) (*CastResult, error) // nil = skip recast

	// Per-patrol counters, reset at start of each patrol.
	patrolAssessed int
	patrolNudged   int
}

// New creates a new Sentinel.
func New(cfg Config, sphere SphereStore, world WorldStore,
	sessions SessionChecker, logger *events.Logger) *Sentinel {
	return &Sentinel{
		config:        cfg,
		sphereStore:   sphere,
		worldStore:    world,
		sessions:      sessions,
		logger:        logger,
		respawnCounts: make(map[respawnKey]int),
		recastCounts:  make(map[string]int),
		lastCaptures:  make(map[string]string),
	}
}

// SetAssessFunc sets a custom assessment function for testing.
// When set, this function is called instead of the real AI assessment.
func (w *Sentinel) SetAssessFunc(fn func(agent store.Agent, sessionName, output string) (*AssessmentResult, error)) {
	w.assessFn = fn
}

// SetCastFunc sets the function used to re-cast failed MR work items.
// When nil, the sentinel skips the recast step during patrol.
func (w *Sentinel) SetCastFunc(fn func(workItemID string) (*CastResult, error)) {
	w.castFn = fn
}

func (w *Sentinel) agentID() string {
	return w.config.World + "/sentinel"
}

// Register registers the sentinel agent in the sphere store.
// Agent ID: "{world}/sentinel", role: "sentinel".
// Creates if not exists, reuses if already registered.
func (w *Sentinel) Register() error {
	return w.sphereStore.EnsureAgent("sentinel", w.config.World, "sentinel")
}

// Run starts the sentinel patrol loop. Blocks until context is cancelled.
// Patrols immediately on start, then on each interval.
func (w *Sentinel) Run(ctx context.Context) error {
	if err := w.Register(); err != nil {
		return fmt.Errorf("failed to register sentinel: %w", err)
	}

	if err := w.sphereStore.UpdateAgentState(w.agentID(), "working", ""); err != nil {
		return fmt.Errorf("failed to set sentinel working: %w", err)
	}

	// Patrol immediately.
	w.patrol(ctx)

	ticker := time.NewTicker(w.config.PatrolInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = w.sphereStore.UpdateAgentState(w.agentID(), "idle", "")
			if w.logger != nil {
				w.logger.Emit(events.EventSessionStop, w.agentID(), w.agentID(), "feed",
					map[string]any{"world": w.config.World, "component": "sentinel"})
			}
			return nil
		case <-ticker.C:
			w.patrol(ctx)
		}
	}
}

// Patrol runs one patrol cycle across all agents in the world. Exported for testing.
func (w *Sentinel) Patrol(ctx context.Context) error {
	return w.patrol(ctx)
}

// patrol runs one patrol cycle across all agents in the world.
func (w *Sentinel) patrol(ctx context.Context) error {
	agents, err := w.sphereStore.ListAgents(w.config.World, "")
	if err != nil {
		return fmt.Errorf("failed to list agents: %w", err)
	}

	// Monitor outpost agents and forge — envoys and governors are human-supervised.
	var activeAgents []store.Agent
	for _, a := range agents {
		if a.Role == "agent" || a.Role == "forge" {
			activeAgents = append(activeAgents, a)
		}
	}

	w.patrolAssessed = 0
	w.patrolNudged = 0

	// Recast failed MRs before agent checks (so newly cast agents appear healthy).
	recastCount := w.recastFailedMRs()

	var healthyCount, stalledCount, zombieCount, reapedCount int
	var actionsTaken []string

	for _, agent := range activeAgents {
		sessionName := dispatch.SessionName(w.config.World, agent.Name)
		alive := w.sessions.Exists(sessionName)

		switch {
		case agent.State == "working" && alive:
			// Working agent with live session — check for progress.
			if err := w.checkProgress(ctx, agent, sessionName); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
						"agent": agent.ID, "action": "check_progress", "error": err.Error(),
					})
				}
			}
			healthyCount++

		case agent.State == "working" && !alive && agent.Role == "forge":
			// Forge session died — respawn without tether semantics.
			stalledCount++
			if err := w.handleForgeStalled(agent); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
						"agent": agent.ID, "action": "handle_forge_stalled", "error": err.Error(),
					})
				}
			}
			actionsTaken = append(actionsTaken, "stalled:"+agent.Name)

		case agent.State == "working" && !alive && agent.TetherItem != "":
			// Session died while work was tethered — stalled.
			stalledCount++
			if err := w.handleStalled(agent); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
						"agent": agent.ID, "action": "handle_stalled", "error": err.Error(),
					})
				}
			}
			actionsTaken = append(actionsTaken, "stalled:"+agent.Name)

		case agent.State == "idle" && alive && !tether.IsTethered(w.config.World, agent.Name, agent.Role):
			// Idle agent with live session and no tether — zombie.
			zombieCount++
			if err := w.handleZombie(agent); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
						"agent": agent.ID, "action": "handle_zombie", "error": err.Error(),
					})
				}
			}
			actionsTaken = append(actionsTaken, "zombie:"+agent.Name)

		case agent.State == "stalled":
			// Already stalled — retry recovery.
			stalledCount++
			if err := w.handleStalled(agent); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
						"agent": agent.ID, "action": "handle_stalled", "error": err.Error(),
					})
				}
			}
			actionsTaken = append(actionsTaken, "stalled:"+agent.Name)

		case agent.State == "idle" && !alive && w.config.IdleReapTimeout > 0 &&
			time.Since(agent.UpdatedAt) > w.config.IdleReapTimeout:
			// Idle agent past reap threshold with no session — reap it.
			reapedCount++
			if err := w.reapIdleAgent(agent); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
						"agent": agent.ID, "action": "reap_idle", "error": err.Error(),
					})
				}
			}
			actionsTaken = append(actionsTaken, "reaped:"+agent.Name)

		default:
			// Healthy idle or no session needed.
			healthyCount++
		}
	}

	// Clean up orphaned resources (worktrees, session metadata, tethers).
	orphansCleaned := w.cleanupOrphanedResources(activeAgents)

	// Prune stale entries for agents no longer in the active set.
	activeIDs := make(map[string]bool, len(activeAgents))
	for _, a := range activeAgents {
		activeIDs[a.ID] = true
	}
	w.pruneCaptures(activeIDs)
	w.pruneRespawnCounts(activeIDs)

	if w.logger != nil {
		w.logger.Emit(events.EventPatrol, w.agentID(), w.agentID(), "feed",
			map[string]any{
				"world":           w.config.World,
				"total":           len(activeAgents),
				"healthy":         healthyCount,
				"stalled":         stalledCount,
				"zombies":         zombieCount,
				"reaped":          reapedCount,
				"recast":          recastCount,
				"orphans_cleaned": orphansCleaned,
				"assessed":        w.patrolAssessed,
				"nudged":          w.patrolNudged,
				"actions":         actionsTaken,
			})
	}

	return nil
}

// pruneCaptures removes hash entries for agents that are no longer working.
func (w *Sentinel) pruneCaptures(workingAgentIDs map[string]bool) {
	for key := range w.lastCaptures {
		if !workingAgentIDs[key] {
			delete(w.lastCaptures, key)
		}
	}
}

// pruneRespawnCounts removes respawn count entries for agents that are no longer active.
func (w *Sentinel) pruneRespawnCounts(activeAgentIDs map[string]bool) {
	for key := range w.respawnCounts {
		if !activeAgentIDs[key.AgentID] {
			delete(w.respawnCounts, key)
		}
	}
}

// checkProgress checks whether a working agent with a live session is making progress.
// If the tmux output hasn't changed since the last patrol, triggers AI assessment.
func (w *Sentinel) checkProgress(ctx context.Context, agent store.Agent, sessionName string) error {
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
	return w.assessAgent(ctx, agent, sessionName, output)
}

func sha256Hash(s string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(s)))
}

// assessAgent uses an AI model to evaluate a potentially stuck agent.
func (w *Sentinel) assessAgent(ctx context.Context, agent store.Agent, sessionName, capturedOutput string) error {
	w.patrolAssessed++

	var result *AssessmentResult
	var err error

	if w.assessFn != nil {
		result, err = w.assessFn(agent, sessionName, capturedOutput)
	} else {
		result, err = w.runAssessment(ctx, agent, capturedOutput)
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

func (w *Sentinel) runAssessment(ctx context.Context, agent store.Agent, capturedOutput string) (*AssessmentResult, error) {
	prompt := buildAssessmentPrompt(agent, capturedOutput, w.config.CaptureLines)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
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

func buildAssessmentPrompt(agent store.Agent, capturedOutput string, captureLines int) string {
	return fmt.Sprintf(`You are a sentinel agent monitoring AI coding agents in a multi-agent
orchestration system. An agent's tmux session output has not changed
since the last patrol cycle (3 minutes ago). Analyze the session output
below and determine the agent's status.

Agent: %s (ID: %s)
Work item: %s
Session output (last %d lines):
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
  May be a zombie or may have completed work without calling sol resolve.

Only suggest "escalate" if the situation requires human intervention
(e.g., repeated failures, auth issues, infrastructure problems).`, agent.Name, agent.ID, agent.TetherItem, captureLines, capturedOutput)
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
func (w *Sentinel) actOnAssessment(agent store.Agent, sessionName string,
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
		if err := w.sessions.Inject(sessionName, result.NudgeMessage, true); err != nil {
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
		if _, err := w.sphereStore.SendProtocolMessage(
			w.agentID(), "operator",
			store.ProtoRecoveryNeeded,
			store.RecoveryNeededPayload{
				AgentID:    agent.ID,
				WorkItemID: agent.TetherItem,
				Reason:     fmt.Sprintf("nudged: %s", result.Reason),
			},
		); err != nil && w.logger != nil {
			w.logger.Emit("mail_error", w.agentID(), agent.ID, "audit",
				map[string]any{"error": err.Error()})
		}

	case "escalate":
		// Send RECOVERY_NEEDED protocol message to operator.
		if _, err := w.sphereStore.SendProtocolMessage(
			w.agentID(), "operator",
			store.ProtoRecoveryNeeded,
			store.RecoveryNeededPayload{
				AgentID:    agent.ID,
				WorkItemID: agent.TetherItem,
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

// handleStalled handles an agent whose session died while work was tethered.
func (w *Sentinel) handleStalled(agent store.Agent) error {
	key := respawnKey{AgentID: agent.ID, WorkItemID: agent.TetherItem}
	attempts := w.respawnCounts[key]

	if attempts >= w.config.MaxRespawns {
		return w.returnWorkToOpen(agent)
	}

	w.respawnCounts[key]++
	return w.respawnAgent(agent)
}

// handleForgeStalled handles a forge agent whose session died.
// Unlike regular agents, forge has no tethered work item. On max respawns,
// it goes idle without resource cleanup (forge worktree is persistent).
func (w *Sentinel) handleForgeStalled(agent store.Agent) error {
	key := respawnKey{AgentID: agent.ID, WorkItemID: ""}
	attempts := w.respawnCounts[key]

	if attempts >= w.config.MaxRespawns {
		// Set forge idle — operator must restart manually.
		if err := w.sphereStore.UpdateAgentState(agent.ID, "idle", ""); err != nil {
			return fmt.Errorf("failed to set forge idle: %w", err)
		}
		delete(w.respawnCounts, key)

		if w.logger != nil {
			w.logger.Emit(events.EventStalled, w.agentID(), agent.ID, "both",
				map[string]any{
					"agent":     agent.ID,
					"recovered": false,
					"role":      "forge",
				})
		}

		if _, err := w.sphereStore.SendProtocolMessage(
			w.agentID(), "operator",
			store.ProtoRecoveryNeeded,
			store.RecoveryNeededPayload{
				AgentID:  agent.ID,
				Reason:   fmt.Sprintf("forge: max respawns (%d) exceeded, set idle", w.config.MaxRespawns),
				Attempts: w.config.MaxRespawns,
			},
		); err != nil && w.logger != nil {
			w.logger.Emit("mail_error", w.agentID(), agent.ID, "audit",
				map[string]any{"error": err.Error()})
		}

		return nil
	}

	w.respawnCounts[key]++
	return w.respawnAgent(agent)
}

// respawnAgent restarts a crashed agent's tmux session.
// The sentinel does NOT re-cast or re-prime. The tether file is durable,
// and the Claude Code SessionStart hook fires sol prime automatically (GUPP).
func (w *Sentinel) respawnAgent(agent store.Agent) error {
	// Ensure agent state is working.
	if err := w.sphereStore.UpdateAgentState(agent.ID, "working", agent.TetherItem); err != nil {
		return fmt.Errorf("failed to set agent %s working: %w", agent.ID, err)
	}

	sessionName := dispatch.SessionName(w.config.World, agent.Name)
	workdir := dispatch.WorktreePath(w.config.World, agent.Name)
	env := map[string]string{
		"SOL_HOME":  config.Home(),
		"SOL_WORLD":   w.config.World,
		"SOL_AGENT": agent.Name,
	}

	if err := w.sessions.Start(sessionName, workdir,
		config.SessionCommand(), env, agent.Role, w.config.World); err != nil {
		return fmt.Errorf("failed to start session for %s: %w", agent.Name, err)
	}

	key := respawnKey{AgentID: agent.ID, WorkItemID: agent.TetherItem}
	attempts := w.respawnCounts[key]

	if w.logger != nil {
		w.logger.Emit(events.EventRespawn, w.agentID(), agent.ID, "both",
			map[string]any{
				"agent":     agent.ID,
				"work_item": agent.TetherItem,
				"attempt":   attempts,
			})
	}

	// Send informational protocol message.
	if _, err := w.sphereStore.SendProtocolMessage(
		w.agentID(), "operator",
		store.ProtoRecoveryNeeded,
		store.RecoveryNeededPayload{
			AgentID:    agent.ID,
			WorkItemID: agent.TetherItem,
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
func (w *Sentinel) returnWorkToOpen(agent store.Agent) error {
	// 1. Update work item: status → open, clear assignee.
	if agent.TetherItem != "" {
		if err := w.worldStore.UpdateWorkItem(agent.TetherItem, store.WorkItemUpdates{
			Status:   "open",
			Assignee: "-", // "-" clears assignee
		}); err != nil {
			return fmt.Errorf("failed to return work item %s to open: %w", agent.TetherItem, err)
		}
	}

	// 2. Set agent state → idle, clear tether_item.
	// Done before clearing tether so a crash leaves the agent idle with a stale
	// tether (harmless — next dispatch overwrites it) rather than "working" with
	// no tether (would trigger a wasted respawn).
	if err := w.sphereStore.UpdateAgentState(agent.ID, "idle", ""); err != nil {
		return fmt.Errorf("failed to set agent %s idle: %w", agent.ID, err)
	}

	// 3. Clean up all agent resources (worktree, session metadata, tether, etc.).
	w.cleanupAgentResources(agent.Name)

	// 4. Clear respawn count.
	key := respawnKey{AgentID: agent.ID, WorkItemID: agent.TetherItem}
	delete(w.respawnCounts, key)

	// 5. Emit stalled event with recovered: false.
	if w.logger != nil {
		w.logger.Emit(events.EventStalled, w.agentID(), agent.ID, "both",
			map[string]any{
				"agent":     agent.ID,
				"work_item": agent.TetherItem,
				"recovered": false,
			})
	}

	// 6. Send RECOVERY_NEEDED protocol message to operator.
	if _, err := w.sphereStore.SendProtocolMessage(
		w.agentID(), "operator",
		store.ProtoRecoveryNeeded,
		store.RecoveryNeededPayload{
			AgentID:    agent.ID,
			WorkItemID: agent.TetherItem,
			Reason:     fmt.Sprintf("max respawns (%d) exceeded, work returned to open", w.config.MaxRespawns),
			Attempts:   w.config.MaxRespawns,
		},
	); err != nil && w.logger != nil {
		w.logger.Emit("mail_error", w.agentID(), agent.ID, "audit",
			map[string]any{"error": err.Error()})
	}

	return nil
}

// handleZombie handles an agent with a live session but no tethered work.
func (w *Sentinel) handleZombie(agent store.Agent) error {
	sessionName := dispatch.SessionName(w.config.World, agent.Name)
	if err := w.sessions.Stop(sessionName, false); err != nil {
		return fmt.Errorf("failed to stop zombie session %s: %w", sessionName, err)
	}
	return nil
}

// reapIdleAgent deletes an idle agent record that has exceeded the reap timeout.
// Cleans up any lingering worktree, session metadata, tether, and workflow files.
func (w *Sentinel) reapIdleAgent(agent store.Agent) error {
	// 1. Clean up all agent resources on disk.
	w.cleanupAgentResources(agent.Name)

	// 2. Delete the agent record to free the name pool slot.
	if err := w.sphereStore.DeleteAgent(agent.ID); err != nil {
		return fmt.Errorf("failed to delete idle agent %s: %w", agent.ID, err)
	}

	if w.logger != nil {
		w.logger.Emit(events.EventReap, w.agentID(), agent.ID, "both",
			map[string]any{
				"agent":      agent.ID,
				"idle_since": agent.UpdatedAt.Format(time.RFC3339),
			})
	}

	return nil
}

// recastFailedMRs checks for merge requests in "failed" phase with open work
// items and re-casts them. Returns the number of work items re-cast.
// Caps retries at MaxRecastAttempts; after that, escalates to the operator.
func (w *Sentinel) recastFailedMRs() int {
	if w.castFn == nil {
		return 0
	}

	failedMRs, err := w.worldStore.ListMergeRequests("failed")
	if err != nil {
		if w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
				map[string]any{"action": "list_failed_mrs", "error": err.Error()})
		}
		return 0
	}

	maxAttempts := w.config.MaxRecastAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	var recastCount int
	seen := make(map[string]bool) // deduplicate by work item

	for _, mr := range failedMRs {
		if seen[mr.WorkItemID] {
			continue
		}
		seen[mr.WorkItemID] = true

		item, err := w.worldStore.GetWorkItem(mr.WorkItemID)
		if err != nil {
			continue
		}

		// Only re-cast if the work item has been reopened by the forge.
		if item.Status != "open" {
			// Work item already re-dispatched or handled — prune tracking.
			delete(w.recastCounts, mr.WorkItemID)
			continue
		}

		attempts := w.recastCounts[mr.WorkItemID]

		if attempts >= maxAttempts {
			if attempts == maxAttempts {
				// First time hitting max — escalate once.
				w.escalateFailedRecast(mr, item, attempts)
				w.recastCounts[mr.WorkItemID] = maxAttempts + 1
			}
			continue
		}

		result, err := w.castFn(mr.WorkItemID)
		if err != nil {
			if w.logger != nil {
				w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
					map[string]any{
						"action":    "recast",
						"mr":        mr.ID,
						"work_item": mr.WorkItemID,
						"error":     err.Error(),
					})
			}
			continue
		}

		w.recastCounts[mr.WorkItemID] = attempts + 1
		recastCount++

		if w.logger != nil {
			w.logger.Emit(events.EventRecast, w.agentID(), w.agentID(), "both",
				map[string]any{
					"mr":        mr.ID,
					"work_item": mr.WorkItemID,
					"agent":     result.AgentName,
					"attempt":   attempts + 1,
				})
		}
	}

	return recastCount
}

// escalateFailedRecast sends a RECOVERY_NEEDED protocol message when a work
// item has exceeded the maximum recast attempts.
func (w *Sentinel) escalateFailedRecast(mr store.MergeRequest, item *store.WorkItem, attempts int) {
	if _, err := w.sphereStore.SendProtocolMessage(
		w.agentID(), "operator",
		store.ProtoRecoveryNeeded,
		store.RecoveryNeededPayload{
			WorkItemID: mr.WorkItemID,
			Reason:     fmt.Sprintf("merge failed %d times for %q, recast limit reached", attempts, item.Title),
			Attempts:   attempts,
		},
	); err != nil && w.logger != nil {
		w.logger.Emit("mail_error", w.agentID(), w.agentID(), "audit",
			map[string]any{"error": err.Error()})
	}

	if w.logger != nil {
		w.logger.Emit(events.EventStalled, w.agentID(), w.agentID(), "both",
			map[string]any{
				"work_item":  mr.WorkItemID,
				"mr":         mr.ID,
				"attempts":   attempts,
				"escalated":  true,
				"reason":     "max recast attempts exceeded",
			})
	}
}

// cleanupAgentResources removes all disk resources for an agent: worktree,
// session metadata, tether file, handoff file, and workflow directory.
// Best-effort: logs errors but does not fail.
func (w *Sentinel) cleanupAgentResources(agentName string) {
	sessionName := dispatch.SessionName(w.config.World, agentName)

	// Stop session if still alive.
	if w.sessions.Exists(sessionName) {
		if err := w.sessions.Stop(sessionName, true); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel: failed to stop session %s: %v\n", sessionName, err)
		}
	}

	// Remove worktree via git.
	worktreeDir := dispatch.WorktreePath(w.config.World, agentName)
	if _, err := os.Stat(worktreeDir); err == nil {
		repoPath := config.RepoPath(w.config.World)
		rmCmd := exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", worktreeDir)
		if out, err := rmCmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "sentinel: worktree remove failed: %s: %v\n",
				strings.TrimSpace(string(out)), err)
			// Fallback: remove directory directly.
			os.RemoveAll(worktreeDir)
		}
		pruneCmd := exec.Command("git", "-C", repoPath, "worktree", "prune")
		pruneCmd.Run() // best-effort
	}

	// Remove session metadata files.
	metaPath := filepath.Join(config.RuntimeDir(), "sessions", sessionName+".json")
	os.Remove(metaPath) // best-effort
	hashPath := filepath.Join(config.RuntimeDir(), "sessions", sessionName+".last-capture-hash")
	os.Remove(hashPath) // best-effort

	// Clear tether file (outpost agents only — this is called from cleanupOrphanedOutpostDirs).
	tether.Clear(w.config.World, agentName, "agent") // best-effort

	// Remove handoff file.
	handoff.Remove(w.config.World, agentName, "agent") // best-effort

	// Remove workflow directory.
	workflow.Remove(w.config.World, agentName, "agent") // best-effort

	// Remove the outpost directory itself if empty.
	outpostDir := filepath.Join(config.Home(), w.config.World, "outposts", agentName)
	os.Remove(outpostDir) // only succeeds if empty, which is fine
}

// cleanupOrphanedResources scans for resources on disk that have no matching
// agent record and cleans them up. Returns the number of resources cleaned.
func (w *Sentinel) cleanupOrphanedResources(agents []store.Agent) int {
	agentNames := make(map[string]bool, len(agents))
	for _, a := range agents {
		agentNames[a.Name] = true
	}

	// Build set of working agents for tether checks.
	workingAgents := make(map[string]bool)
	for _, a := range agents {
		if a.State == "working" {
			workingAgents[a.Name] = true
		}
	}

	var cleaned int
	cleaned += w.cleanupOrphanedOutpostDirs(agentNames)
	cleaned += w.cleanupOrphanedSessionMeta(agentNames)
	cleaned += w.cleanupOrphanedTethers(agentNames, workingAgents)
	return cleaned
}

// cleanupOrphanedOutpostDirs removes outpost directories that have no matching agent record.
func (w *Sentinel) cleanupOrphanedOutpostDirs(agentNames map[string]bool) int {
	outpostsDir := filepath.Join(config.Home(), w.config.World, "outposts")
	entries, err := os.ReadDir(outpostsDir)
	if err != nil {
		return 0 // directory may not exist
	}

	var cleaned int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if agentNames[name] {
			continue // agent exists, not orphaned
		}

		// Check if a worktree exists in this directory.
		worktreeDir := filepath.Join(outpostsDir, name, "worktree")
		if _, err := os.Stat(worktreeDir); err != nil {
			continue // no worktree, skip
		}

		// Orphaned worktree — clean it up.
		w.cleanupAgentResources(name)
		cleaned++

		if w.logger != nil {
			w.logger.Emit(events.EventOrphanCleanup, w.agentID(), w.agentID(), "audit",
				map[string]any{
					"type":  "worktree",
					"agent": name,
					"world": w.config.World,
				})
		}
	}
	return cleaned
}

// cleanupOrphanedSessionMeta removes session metadata files for dead outpost
// sessions that have no matching agent record.
func (w *Sentinel) cleanupOrphanedSessionMeta(agentNames map[string]bool) int {
	sessDir := filepath.Join(config.RuntimeDir(), "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		return 0
	}

	prefix := "sol-" + w.config.World + "-"
	var cleaned int
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		fileName := entry.Name()
		sessName := strings.TrimSuffix(fileName, ".json")
		if !strings.HasPrefix(sessName, prefix) {
			continue // not for this world
		}

		agentName := strings.TrimPrefix(sessName, prefix)
		if agentNames[agentName] {
			continue // agent exists, not orphaned
		}

		// Skip if session is still alive in tmux.
		if w.sessions.Exists(sessName) {
			continue
		}

		// Orphaned session metadata — remove it.
		os.Remove(filepath.Join(sessDir, fileName))
		hashFile := sessName + ".last-capture-hash"
		os.Remove(filepath.Join(sessDir, hashFile))
		cleaned++

		if w.logger != nil {
			w.logger.Emit(events.EventOrphanCleanup, w.agentID(), w.agentID(), "audit",
				map[string]any{
					"type":    "session_metadata",
					"session": sessName,
					"world":   w.config.World,
				})
		}
	}
	return cleaned
}

// cleanupOrphanedTethers removes tether files for agents that are not working.
func (w *Sentinel) cleanupOrphanedTethers(agentNames, workingAgents map[string]bool) int {
	outpostsDir := filepath.Join(config.Home(), w.config.World, "outposts")
	entries, err := os.ReadDir(outpostsDir)
	if err != nil {
		return 0
	}

	var cleaned int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()

		// If agent exists and is working, the tether is valid.
		if workingAgents[name] {
			continue
		}

		// Check if a tether file exists (outpost agents only — scanning outposts/ dir).
		if !tether.IsTethered(w.config.World, name, "agent") {
			continue
		}

		// Tether exists but agent is not working — orphaned tether.
		tether.Clear(w.config.World, name, "agent")
		cleaned++

		if w.logger != nil {
			w.logger.Emit(events.EventOrphanCleanup, w.agentID(), w.agentID(), "audit",
				map[string]any{
					"type":  "tether",
					"agent": name,
					"world": w.config.World,
				})
		}
	}
	return cleaned
}
