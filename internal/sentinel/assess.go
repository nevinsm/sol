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
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
)

// captureErrorSentinel is stored in lastCaptures when a session capture fails.
// It is not a valid sha256 hex string, so any subsequent successful capture
// will always differ from it — ensuring we establish a fresh baseline after
// a failure window rather than comparing against a potentially stale pre-failure hash.
const captureErrorSentinel = "capture_error"

// checkProgress checks whether a working agent with a live session is making progress.
// If the tmux output hasn't changed since the last patrol, triggers AI assessment —
// unless the process tree shows a descendant burning CPU, in which case the agent
// is deterministically progressing and the AI callout is skipped (see
// observeProcessTree and ProcessTreeDelta.progressingReason).
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

	// Observe the process tree every patrol (mirrors the output capture
	// above), regardless of whether the pane text changed — the delta needs
	// a continuous baseline to detect CPU accrual between consecutive
	// patrols.
	tree := w.observeProcessTree(agent, sessionName)

	if !seen {
		return nil // first patrol for this agent, establish baseline
	}
	if hash != lastHash {
		// Output changed — agent is making progress. Any waiting_on_background
		// streak is broken; a fresh streak starts if the agent stalls again.
		// Clear the paired "already escalated/mailed for this streak" markers
		// too, so a new stall after real progress can escalate/mail again.
		delete(w.waitingCounts, agent.ID)
		delete(w.waitEscalated, agent.ID)
		delete(w.nudgeMailed, agent.ID)
		return nil
	}

	// Pane text is unchanged. Before calling the AI assessor, check the one
	// deterministic (harness-neutral) shortcut this system allows: if a
	// descendant process is burning CPU, the agent is progressing no matter
	// what the pane says — e.g. a quiet compile with no new output yet.
	// This can only ever say "progressing", never "stuck" or "detached".
	if reason, progressing := tree.progressingReason(); progressing {
		delete(w.waitingCounts, agent.ID)
		delete(w.waitEscalated, agent.ID)
		delete(w.nudgeMailed, agent.ID)
		if w.logger != nil {
			w.logger.Emit(events.EventAssess, w.agentID(), agent.ID, "both",
				map[string]any{
					"agent":      agent.ID,
					"status":     "progressing",
					"confidence": "high",
					"action":     "none",
					"reason":     reason,
				})
		}
		return nil
	}

	// No change since last patrol, and no descendant CPU activity — assess with AI.
	return w.assessAgent(ctx, agent, sessionName, output, tree)
}

func sha256Hash(s string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(s)))
}

// ProcessTreeDelta is the structured, harness-neutral second observation
// channel handed to the AI assessor alongside the captured pane text: the
// process subtree under the pane, and how it changed since the last patrol.
// Interpretation (progressing / stuck / detached) stays entirely in the
// assessor prompt — the one exception is progressingReason, the single
// deterministic (one-directional, "progressing"-only) shortcut the design
// allows.
type ProcessTreeDelta struct {
	// Tree is the current process tree (pane root + descendants), sorted by PID.
	// Empty when ProcessTree failed or returned no processes.
	Tree []session.ProcessInfo
	// RootPID is the PID of the pane root process (the agent runtime itself).
	// Zero when Tree is empty.
	RootPID int
	// CPUDeltas maps PID → CPU ticks accrued since the last patrol, for
	// processes present in both the current and previous tree.
	CPUDeltas map[int]uint64
	// Appeared holds processes present now but not in the previous patrol's tree.
	Appeared []session.ProcessInfo
	// Vanished holds processes present in the previous patrol's tree but not now.
	Vanished []session.ProcessInfo
	// HasPrior is false on the first observation for this agent (or after a
	// ProcessTree failure), meaning there is nothing to diff against yet.
	HasPrior bool
	// Err is set when ProcessTree failed. The rest of the struct is
	// zero-valued in that case — callers fall back to text-only assessment.
	Err error
}

// progressingReason implements the one deterministic rule this design
// allows: if any descendant process (never the pane root, which is the
// runtime itself and accrues CPU while the model merely thinks) has
// accrued CPU since the last patrol, the agent is progressing. Returns
// ok=false whenever there is no prior tree to diff against or no
// descendant shows CPU accrual.
func (d ProcessTreeDelta) progressingReason() (reason string, ok bool) {
	if !d.HasPrior {
		return "", false
	}
	for _, p := range d.Tree {
		if p.PID == d.RootPID {
			continue // pane root is the runtime itself, not a descendant
		}
		if d.CPUDeltas[p.PID] > 0 {
			return fmt.Sprintf("descendant process %s accrued CPU", p.Command), true
		}
	}
	return "", false
}

// observeProcessTree calls ProcessTree, updates the agent's last-observed
// tree, and computes the delta versus the previous patrol. A ProcessTree
// failure is soft: it is logged via sentinel_error (mirroring Capture's
// failure handling) and the returned delta carries Err with everything else
// zero-valued, so callers proceed with text-only assessment exactly as if
// this observation channel didn't exist.
func (w *Sentinel) observeProcessTree(agent store.Agent, sessionName string) ProcessTreeDelta {
	tree, err := w.sessions.ProcessTree(sessionName)
	if err != nil {
		if w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit",
				map[string]any{"error": err.Error(), "action": "process_tree_failed", "session": sessionName})
		}
		return ProcessTreeDelta{Err: err}
	}

	prev, hasPrior := w.lastTrees[agent.ID]
	w.lastTrees[agent.ID] = tree

	delta := ProcessTreeDelta{
		Tree:      tree,
		RootPID:   processTreeRootPID(tree),
		CPUDeltas: make(map[int]uint64, len(tree)),
		HasPrior:  hasPrior,
	}

	prevByPID := make(map[int]session.ProcessInfo, len(prev))
	for _, p := range prev {
		prevByPID[p.PID] = p
	}
	curPIDs := make(map[int]bool, len(tree))
	for _, p := range tree {
		curPIDs[p.PID] = true
		if old, existed := prevByPID[p.PID]; existed {
			if p.CPUTicks > old.CPUTicks {
				delta.CPUDeltas[p.PID] = p.CPUTicks - old.CPUTicks
			}
		} else if hasPrior {
			delta.Appeared = append(delta.Appeared, p)
		}
	}
	if hasPrior {
		for _, p := range prev {
			if !curPIDs[p.PID] {
				delta.Vanished = append(delta.Vanished, p)
			}
		}
	}

	return delta
}

// processTreeRootPID identifies the pane root within tree: the one process
// whose parent PID is not itself part of the tree (every descendant's
// parent, by construction, is another process inside the same tree).
// Returns 0 for an empty tree.
func processTreeRootPID(tree []session.ProcessInfo) int {
	if len(tree) == 0 {
		return 0
	}
	inTree := make(map[int]bool, len(tree))
	for _, p := range tree {
		inTree[p.PID] = true
	}
	for _, p := range tree {
		if !inTree[p.PPID] {
			return p.PID
		}
	}
	return tree[0].PID // defensive fallback; every well-formed tree has a root
}

// assessAgent uses an AI model to evaluate a potentially stuck agent.
func (w *Sentinel) assessAgent(ctx context.Context, agent store.Agent, sessionName, capturedOutput string, tree ProcessTreeDelta) error {
	w.patrolAssessed++

	// Update heartbeat to "assessing" status so prefect knows not to respawn agents.
	w.writeHeartbeat("assessing", w.patrolCount, 0, 0, 0, "")

	var result *AssessmentResult
	var err error

	if w.assessFn != nil {
		result, err = w.assessFn(agent, sessionName, capturedOutput, tree)
	} else {
		result, err = w.runAssessment(ctx, agent, capturedOutput, tree)
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

func (w *Sentinel) runAssessment(ctx context.Context, agent store.Agent, capturedOutput string, tree ProcessTreeDelta) (*AssessmentResult, error) {
	prompt := buildAssessmentPrompt(agent, capturedOutput, w.config.CaptureLines, w.config.PatrolInterval, tree)

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

func buildAssessmentPrompt(agent store.Agent, capturedOutput string, captureLines int, patrolInterval time.Duration, tree ProcessTreeDelta) string {
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
%s
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

The process tree above is ground truth; the pane text is only the
agent's self-report, rendered through its own TUI, and can be wrong — a
status bar can keep claiming "1 shell still running" long after that
shell has actually exited. If the agent's output claims a running
background task, but no descendant beyond the runtime's own resident helpers
(e.g. a language server, an MCP or plugin server started at session
startup) exists in the tree, or none of those descendants have accrued
any CPU across patrols, the wait is provably detached — set "detached":
true regardless of what the pane text says.

Only suggest "escalate" if the situation requires human intervention
(e.g., repeated failures, auth issues, infrastructure problems).`, staleWindow, agent.Name, agent.ID, agent.ActiveWrit, captureLines, capturedOutput, renderProcessTreeBlock(tree))
}

// renderProcessTreeBlock renders the process-tree observation channel into
// the short block the prompt inserts after the captured pane output. Empty
// (and the surrounding blank line collapses away) when ProcessTree failed —
// the assessor then falls back to text-only judgment exactly as before this
// channel existed.
func renderProcessTreeBlock(tree ProcessTreeDelta) string {
	if tree.Err != nil {
		return ""
	}
	if len(tree.Tree) == 0 {
		return "\nProcess tree under the pane: no processes found (the pane's own process may have exited).\n"
	}

	var b strings.Builder
	b.WriteString("\nProcess tree under the pane (pid comm cpu_delta_since_last_patrol):\n")
	for _, p := range tree.Tree {
		deltaStr := "new"
		if delta, known := tree.CPUDeltas[p.PID]; known {
			deltaStr = fmt.Sprintf("+%d ticks", delta)
		}
		line := fmt.Sprintf("  %d %s   %s", p.PID, p.Command, deltaStr)
		if p.PID == tree.RootPID {
			line += "   (pane root: the agent runtime itself)"
		}
		b.WriteString(line + "\n")
	}
	if tree.HasPrior {
		b.WriteString(describeProcessTreeChanges(tree.Appeared, tree.Vanished) + "\n")
	}
	return b.String()
}

// describeProcessTreeChanges summarizes which processes appeared or
// vanished since the last patrol, for the assessor prompt's process tree block.
func describeProcessTreeChanges(appeared, vanished []session.ProcessInfo) string {
	appearedDesc := "no processes appeared"
	if len(appeared) > 0 {
		names := make([]string, len(appeared))
		for i, p := range appeared {
			names[i] = p.Command
		}
		appearedDesc = "processes appeared: " + strings.Join(names, ", ")
	}
	vanishedDesc := "no processes vanished"
	if len(vanished) > 0 {
		names := make([]string, len(vanished))
		for i, p := range vanished {
			names[i] = p.Command
		}
		vanishedDesc = "processes vanished: " + strings.Join(names, ", ")
	}
	return fmt.Sprintf("Since last patrol: %s; %s.", appearedDesc, vanishedDesc)
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
	// harness-tracked background wait. Clear the paired "already escalated
	// for this streak" marker along with the counter.
	if result.SuggestedAction != "waiting_on_background" {
		delete(w.waitingCounts, agent.ID)
		delete(w.waitEscalated, agent.ID)
	}

	// Any verdict other than nudge breaks the consecutive nudge streak — the
	// next nudge (if any) starts a fresh streak and may mail again.
	if result.SuggestedAction != "nudge" {
		delete(w.nudgeMailed, agent.ID)
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

		// Send informational mail to autarch — once per nudge streak. An
		// agent with unchanged output can be nudged on every patrol (the
		// nudge itself, above, is unconditional by design); without this
		// gate that would also mail the autarch every patrol. The marker
		// clears on output change (checkProgress) or when the verdict
		// switches away from nudge (above), so a new stall mails again.
		if !w.nudgeMailed[agent.ID] {
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
			} else {
				w.nudgeMailed[agent.ID] = true
			}
		}

	case "escalate":
		w.escalateAgent(agent, result.Reason)

	case "waiting_on_background":
		if result.Detached {
			// Provably detached: nohup/disown/setsid, a killed monitor, or a
			// background process that no longer exists — the completion
			// signal will never arrive. This is the one real risk in the
			// waiting_on_background family, so bypass the grace period and
			// escalate immediately. escalateAgent's own dedup (by source ref)
			// keeps this from re-mailing on subsequent patrols while still
			// detached with unchanged output.
			delete(w.waitingCounts, agent.ID)
			delete(w.waitEscalated, agent.ID)
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

		// Grace expired. Escalate once per stall: leave waitingCounts at/above
		// graceLimit (do NOT reset it — resetting would rebuild the streak
		// and re-escalate every graceLimit patrols for as long as the agent
		// stays parked, which is the bug this marker fixes) and gate further
		// escalation attempts on waitEscalated so this branch is a no-op on
		// every subsequent unchanged-output patrol. Both are cleared together
		// when output changes or the verdict leaves waiting_on_background.
		if w.waitEscalated[agent.ID] {
			return nil
		}
		w.waitEscalated[agent.ID] = true

		// Distinguish this from a detached wait in the mail body so the
		// autarch can triage: the signal may still arrive, it has just been
		// a long wait.
		streak := w.waitingCounts[agent.ID]
		w.escalateAgent(agent, fmt.Sprintf(
			"waiting on background task for %d consecutive patrols with no output change (grace expired, signal may still arrive): %s",
			streak, result.Reason))
	}

	return nil
}

// escalateAgent creates a durable escalation (deduped by active writ) and
// sends a RECOVERY_NEEDED protocol message to the autarch — but only when a
// new escalation was actually created. Shared by the "escalate"
// suggested_action and the waiting_on_background paths that bypass or
// exhaust their grace period, both of which can be called repeatedly for the
// same stall (e.g. once per patrol while output stays unchanged); mailing
// unconditionally on every call is what caused the RECOVERY_NEEDED flood
// this dedup fixes. When dedup skips creation because an open escalation
// already exists for this source ref, the autarch was already mailed when
// it was first created, so no mail is sent here.
func (w *Sentinel) escalateAgent(agent store.Agent, reason string) {
	// Create formal escalation for durable tracking, with dedup to avoid
	// duplicates when agent output is unchanged across patrols.
	escDesc := fmt.Sprintf("Agent %s needs recovery: %s", agent.Name, reason)
	var sourceRef string
	if agent.ActiveWrit != "" {
		sourceRef = "writ:" + agent.ActiveWrit
	}

	created := true
	if sourceRef != "" {
		if existing, err := w.sphereStore.ListEscalationsBySourceRef(sourceRef); err == nil && len(existing) > 0 {
			// Open escalation already exists — skip creation, and skip the
			// mail below since the autarch was already notified.
			created = false
		} else {
			if _, err := w.sphereStore.CreateEscalation("high", w.config.World+"/sentinel", escDesc, sourceRef); err != nil {
				created = false
				if w.logger != nil {
					w.logger.Emit("escalation_error", w.agentID(), agent.ID, "audit",
						map[string]any{"error": err.Error()})
				}
			}
		}
	} else {
		// No source ref (no active writ) — create without dedup.
		if _, err := w.sphereStore.CreateEscalation("high", w.config.World+"/sentinel", escDesc, sourceRef); err != nil {
			created = false
			if w.logger != nil {
				w.logger.Emit("escalation_error", w.agentID(), agent.ID, "audit",
					map[string]any{"error": err.Error()})
			}
		}
	}

	if !created {
		return
	}

	// Send RECOVERY_NEEDED protocol message to autarch (live nudge). Only
	// reached when this call actually created a new escalation.
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
