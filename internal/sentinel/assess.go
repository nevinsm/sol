package sentinel

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
)

// captureErrorSentinel is stored in lastCaptures when a session capture fails.
// It is not a valid sha256 hex string, so any subsequent successful capture
// will always differ from it — ensuring we establish a fresh baseline after
// a failure window rather than comparing against a potentially stale pre-failure hash.
const captureErrorSentinel = "capture_error"

// checkProgress checks whether a working agent with a live session is making progress.
// If the tmux output hasn't changed since the last patrol, triggers AI assessment.
func (w *Sentinel) checkProgress(ctx context.Context, agent store.Agent, sessionName string) error {
	output, err := w.sessions.Capture(sessionName, w.config.CaptureLines)
	if err != nil {
		if w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit",
				map[string]any{"error": err.Error(), "action": "capture_failed", "session": sessionName})
		}
		// Record the failure so the next successful capture compares against the
		// sentinel (not a stale pre-failure hash), avoiding a false negative where
		// output that changed just before a stall would mask the stall.
		w.lastCaptures[agent.ID] = captureErrorSentinel
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

	// Update heartbeat to "assessing" status so prefect knows not to respawn agents.
	w.writeHeartbeat("assessing", w.patrolCount, 0, 0, 0, "")

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
	prompt := buildAssessmentPrompt(agent, capturedOutput, w.config.CaptureLines, w.config.PatrolInterval)

	assessTimeout := w.config.AssessTimeout
	if assessTimeout == 0 {
		assessTimeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, assessTimeout)
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

func buildAssessmentPrompt(agent store.Agent, capturedOutput string, captureLines int, patrolInterval time.Duration) string {
	staleWindow := fmt.Sprintf("%d minutes ago", int(patrolInterval.Minutes()))
	return fmt.Sprintf(`You are a sentinel agent monitoring AI coding agents in a multi-agent
orchestration system. An agent's tmux session output has not changed
since the last patrol cycle (%s). Analyze the session output
below and determine the agent's status.

Agent: %s (ID: %s)
Writ: %s
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
(e.g., repeated failures, auth issues, infrastructure problems).`, staleWindow, agent.Name, agent.ID, agent.ActiveWrit, captureLines, capturedOutput)
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

		// Send informational mail to autarch.
		if _, err := w.sphereStore.SendProtocolMessage(
			w.agentID(), config.Autarch,
			store.ProtoRecoveryNeeded,
			store.RecoveryNeededPayload{
				AgentID:    agent.ID,
				WritID: agent.ActiveWrit,
				Reason:     fmt.Sprintf("nudged: %s", result.Reason),
			},
		); err != nil && w.logger != nil {
			w.logger.Emit("mail_error", w.agentID(), agent.ID, "audit",
				map[string]any{"error": err.Error()})
		}

	case "escalate":
		// Create formal escalation for durable tracking, with dedup to avoid
		// duplicates when agent output is unchanged across patrols.
		escDesc := fmt.Sprintf("Agent %s needs recovery: %s", agent.Name, result.Reason)
		var sourceRef string
		if agent.ActiveWrit != "" {
			sourceRef = "writ:" + agent.ActiveWrit
		}
		if sourceRef != "" {
			if existing, err := w.sphereStore.ListEscalationsBySourceRef(sourceRef); err == nil && len(existing) > 0 {
				// Open escalation already exists — skip creation.
			} else {
				if _, err := w.sphereStore.CreateEscalation("high", w.config.World+"/sentinel", escDesc, sourceRef); err != nil && w.logger != nil {
					w.logger.Emit("escalation_error", w.agentID(), agent.ID, "audit",
						map[string]any{"error": err.Error()})
				}
			}
		} else {
			// No source ref (no active writ) — create without dedup.
			if _, err := w.sphereStore.CreateEscalation("high", w.config.World+"/sentinel", escDesc, sourceRef); err != nil && w.logger != nil {
				w.logger.Emit("escalation_error", w.agentID(), agent.ID, "audit",
					map[string]any{"error": err.Error()})
			}
		}

		// Send RECOVERY_NEEDED protocol message to autarch (live nudge).
		if _, err := w.sphereStore.SendProtocolMessage(
			w.agentID(), config.Autarch,
			store.ProtoRecoveryNeeded,
			store.RecoveryNeededPayload{
				AgentID:    agent.ID,
				WritID: agent.ActiveWrit,
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
