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
	"github.com/nevinsm/sol/internal/nudge"
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
		// Output changed — agent is making progress. Any waiting_on_background
		// streak is broken; a fresh streak starts if the agent stalls again.
		delete(w.waitingCounts, agent.ID)
		return nil
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
    "suggested_action": "none|nudge|escalate|waiting_on_background",
    "nudge_message": "if suggested_action is nudge, the message to send",
    "detached": false
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

Recognizing a harness-tracked background wait (suggested_action
"waiting_on_background"): this is CORRECT, EXPECTED agent behavior, not
a stuck or idle agent. The agent launched a long-running command (e.g.
a race-safe test suite) as a background task and ended its turn to await
the harness's completion notification — it did nothing wrong. Look for:
  - The status bar or output shows a running background shell, a
    task/job ID, or an outstanding Monitor/wait-for-notification state.
  - Recent turns are short, content-free waiting statements ("Waiting
    for the background task to finish", "Will check back when notified")
    rather than confused or repetitive output.
  - No error loops or repeated failed attempts.
  - Work appeared complete or steadily progressing right before the
    wait began.
When you see this pattern, suggest "waiting_on_background" — do NOT
suggest "nudge" or "escalate" for a correctly-parked wait; nudging an
agent that is legitimately waiting on a harness notification wastes a
turn and cannot speed up the background task.

Detecting a DETACHED wait (set "detached": true): a harness-tracked
wait is only safe if the completion signal can actually arrive. Some
waits are provably dead — the agent (intentionally or not) detached the
process from the harness, so no notification will ever come. Check the
captured output for:
  - Use of "nohup", "disown", or "setsid" on the backgrounded command —
    these detach the process from the controlling session/harness.
  - A Monitor, watcher, or wait loop that was killed or errored out
    before the underlying task finished.
  - Evidence that the background process no longer exists (e.g., "no
    such process", a PID check failing) while the agent is still
    waiting on it.
When suggested_action is "waiting_on_background" AND you see one of
these signs, set "detached": true — this is the one case that IS a
real risk and should bypass the normal grace period. Otherwise leave
"detached": false.

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

	// Any verdict other than waiting_on_background breaks the consecutive
	// waiting streak — the agent is no longer (or was never) parked on a
	// harness-tracked background wait.
	if result.SuggestedAction != "waiting_on_background" {
		delete(w.waitingCounts, agent.ID)
	}

	switch result.SuggestedAction {
	case "none":
		// Agent is progressing or we're not confident — do nothing.
		return nil

	case "nudge":
		// Content goes through the durable nudge queue; the session's pane
		// only ever sees the fixed doorbell (see internal/nudge). Enqueue is
		// the critical section — content loss is unacceptable — so a
		// failure there is returned. Ring is best-effort by design: even if
		// the doorbell never lands, the queue is drained at the next turn
		// boundary regardless.
		if err := nudge.Enqueue(sessionName, nudge.Message{
			Sender: "sentinel",
			Type:   "nudge",
			Body:   result.NudgeMessage,
		}); err != nil {
			return fmt.Errorf("failed to enqueue nudge for %s: %w", sessionName, err)
		}
		nudge.Ring(w.sessions, sessionName)
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
		w.escalateAgent(agent, result.Reason)

	case "waiting_on_background":
		if result.Detached {
			// Provably detached: nohup/disown/setsid, a killed monitor, or a
			// background process that no longer exists — the completion
			// signal will never arrive. This is the one real risk in the
			// waiting_on_background family, so bypass the grace period and
			// escalate immediately.
			delete(w.waitingCounts, agent.ID)
			w.escalateAgent(agent, fmt.Sprintf(
				"detached background wait — completion signal will never arrive: %s",
				result.Reason))
			return nil
		}

		// Correctly parked on a harness-tracked background wait: no autarch
		// mail. Record the observation via the existing patrol assessment
		// event (emitted by assessAgent before actOnAssessment runs) and
		// re-check next patrol. Only escalate after WaitGraceCount
		// consecutive waiting patrols with unchanged output (checkProgress
		// only calls into assessment when the captured output is unchanged
		// since the prior patrol, so consecutive waiting_on_background
		// verdicts already imply consecutive unchanged-output patrols).
		w.waitingCounts[agent.ID]++

		graceLimit := w.config.WaitGraceCount
		if graceLimit <= 0 {
			graceLimit = 3
		}
		if w.waitingCounts[agent.ID] < graceLimit {
			return nil
		}

		// Grace expired — escalate, but distinguish this from a detached
		// wait in the mail body so the autarch can triage: the signal may
		// still arrive, it has just been a long wait.
		streak := w.waitingCounts[agent.ID]
		delete(w.waitingCounts, agent.ID) // avoid re-escalating every subsequent patrol
		w.escalateAgent(agent, fmt.Sprintf(
			"waiting on background task for %d consecutive patrols with no output change (grace expired, signal may still arrive): %s",
			streak, result.Reason))
	}

	return nil
}

// escalateAgent creates a durable escalation (deduped by active writ) and
// sends a RECOVERY_NEEDED protocol message to the autarch. Shared by the
// "escalate" suggested_action and the waiting_on_background paths that
// bypass or exhaust their grace period.
func (w *Sentinel) escalateAgent(agent store.Agent, reason string) {
	// Create formal escalation for durable tracking, with dedup to avoid
	// duplicates when agent output is unchanged across patrols.
	escDesc := fmt.Sprintf("Agent %s needs recovery: %s", agent.Name, reason)
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
			AgentID: agent.ID,
			WritID:  agent.ActiveWrit,
			Reason:  reason,
		},
	); err != nil && w.logger != nil {
		w.logger.Emit("mail_error", w.agentID(), agent.ID, "audit",
			map[string]any{"error": err.Error()})
	}

	if w.logger != nil {
		w.logger.Emit(events.EventStalled, w.agentID(), agent.ID, "both",
			map[string]any{
				"agent":     agent.ID,
				"reason":    reason,
				"escalated": true,
			})
	}
}
