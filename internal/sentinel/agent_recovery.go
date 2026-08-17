package sentinel

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/flock"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// reconcileRespawnCounts seeds in-memory respawn counts from the event log.
//
// CRASH SAFETY: Respawn counts are in-memory and lost on sentinel restart.
// Without reconciliation, a restarted sentinel would reset all counts to 0,
// potentially causing infinite respawn loops for persistently failing agents
// (each restart resets the count, never reaching MaxRespawns). This method
// reads recent respawn events from the event log and reconstructs the counts,
// ensuring crash-restart doesn't bypass the MaxRespawns limit.
func (w *Sentinel) reconcileRespawnCounts() {
	if w.eventReader == nil {
		return // no event reader configured (e.g., in tests)
	}

	// Look back 24 hours — respawn counts are per-writ, and any older
	// respawns are for work that has likely been resolved or reassigned.
	evts, err := w.eventReader.Read(events.ReadOpts{
		Type:   events.EventRespawn,
		Source: w.agentID(),
		Since:  w.now().Add(-24 * time.Hour),
	})
	if err != nil {
		// SAFETY: If the event log is unreadable (locked, corrupted), we must
		// NOT start with zero counts — that would grant extra respawns beyond
		// MaxRespawns. Instead, mark reconciliation as failed so handleStalled
		// uses MaxRespawns as the conservative default for unknown agents.
		w.reconcileFailed = true
		if w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit", map[string]any{
				"error":   err.Error(),
				"context": "reconcileRespawnCounts: event log unreadable, using conservative respawn limits",
			})
		}
		return
	}

	for _, ev := range evts {
		payload, ok := ev.Payload.(map[string]any)
		if !ok {
			continue
		}
		agentID, _ := payload["agent"].(string)
		writID, _ := payload["writ"].(string)
		if agentID == "" {
			continue
		}
		key := respawnKey{AgentID: agentID, WritID: writID}
		w.respawnCounts[key]++
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

// checkClosedWritTethers iterates all agents' tether directories and handles
// closed writs. For outpost agents, a closed writ triggers a full reap. For
// persistent agents, only the closed tether file is removed.
// Returns a set of agent IDs that were reaped.
func (w *Sentinel) checkClosedWritTethers(agents []store.Agent, reapedCount *int, actionsTaken *[]string) map[string]bool {
	reaped := make(map[string]bool)
	if w.worldStore == nil {
		return reaped
	}

	for _, agent := range agents {
		tetheredWrits, err := tether.List(w.config.World, agent.Name, agent.Role)
		if err != nil {
			if w.logger != nil {
				w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
					"agent":  agent.ID,
					"action": "list_tethered_writs",
					"error":  err.Error(),
				})
			}
			continue
		}
		if len(tetheredWrits) == 0 {
			continue
		}

		for _, writID := range tetheredWrits {
			writ, err := w.worldStore.GetWrit(writID)
			if err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
						"agent":  agent.ID,
						"writ":   writID,
						"action": "get_writ_for_closed_check",
						"error":  err.Error(),
					})
				}
				continue
			}
			if writ.Status != "closed" {
				continue
			}

			// Contract with dispatch: dispatch.Resolve closes the writ BEFORE
			// clearing the tether and stopping the session. If a resolve is in
			// progress, the closed-writ + tether-file combination is a transient
			// state — not an orphan. Skip this agent for this patrol cycle and
			// let dispatch.Resolve finish; the next patrol will see the final
			// state. See internal/dispatch/resolve.go (IsResolveInProgress).
			if dispatch.IsResolveInProgress(w.config.World, agent.Name, agent.Role) {
				if w.logger != nil {
					w.logger.Emit("sentinel_action", w.agentID(), agent.ID, "audit",
						map[string]any{
							"agent":  agent.ID,
							"writ":   writID,
							"action": "skip_reap_resolve_in_progress",
						})
				}
				break
			}

			if agent.Role == "outpost" {
				// Outpost agent with closed writ: full reap.
				sessionName := config.SessionName(w.config.World, agent.Name)
				*reapedCount++
				if err := w.reapClosedWritAgent(agent, sessionName, writ.CloseReason); err != nil {
					if w.logger != nil {
						w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
							"agent": agent.ID, "action": "reap_closed_writ", "error": err.Error(),
						})
					}
				}
				*actionsTaken = append(*actionsTaken, "reaped_closed_writ:"+agent.Name)
				reaped[agent.ID] = true
				break // agent is deleted, no point checking more writs
			}

			// Persistent agent: remove just this tether, keep agent alive.
			if err := tether.ClearOne(w.config.World, agent.Name, writID, agent.Role); err != nil {
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), agent.ID, "audit", map[string]any{
						"agent": agent.ID, "writ": writID, "action": "clear_tether_failed", "error": err.Error(),
					})
				}
			}
			if agent.ActiveWrit == writID {
				_ = w.sphereStore.UpdateAgentState(agent.ID, agent.State, "")
				agent.ActiveWrit = "" // update local copy
			}
			if w.logger != nil {
				w.logger.Emit("sentinel_action", w.agentID(), agent.ID, "feed",
					map[string]any{
						"agent":        agent.ID,
						"writ":         writID,
						"close_reason": writ.CloseReason,
						"action":       "cleared_closed_tether",
					})
			}
		}
	}

	return reaped
}

// handleStalled handles an agent whose session died while work was tethered.
//
// CRASH SAFETY: The in-memory respawnCounts map is incremented BEFORE the
// persistent state change in respawnAgent (UpdateAgentState). If sentinel
// crashes after incrementing but before persisting, the in-memory count is
// lost (harmless — reconcileRespawnCounts recovers from event history on
// restart). If sentinel crashes after respawnAgent emits the respawn event,
// the count is recoverable from the event log.
func (w *Sentinel) handleStalled(agent store.Agent) error {
	key := respawnKey{AgentID: agent.ID, WritID: agent.ActiveWrit}
	attempts := w.respawnCounts[key]

	// If event log reconciliation failed and we have no recorded attempts
	// for this agent, use MaxRespawns as a conservative default to prevent
	// granting extra respawns beyond the configured limit.
	if w.reconcileFailed && attempts == 0 {
		attempts = w.config.MaxRespawns
	}

	if attempts >= w.config.MaxRespawns {
		return w.returnWorkToOpen(agent)
	}

	w.respawnCounts[key]++
	return w.respawnAgent(agent)
}

// respawnAgent restarts a crashed agent's tmux session using the startup
// registry. The tether file is durable, and the Claude Code SessionStart
// hook fires sol prime automatically (GUPP).
func (w *Sentinel) respawnAgent(agent store.Agent) error {
	// Ensure agent state is working before respawn.
	if err := w.sphereStore.UpdateAgentState(agent.ID, "working", agent.ActiveWrit); err != nil {
		return fmt.Errorf("failed to set agent %s working: %w", agent.ID, err)
	}

	writExists := func(id string) bool {
		if id == "" {
			return true
		}
		if w.worldStore == nil {
			return true
		}
		_, err := w.worldStore.GetWrit(id)
		if errors.Is(err, store.ErrNotFound) {
			return false
		}
		return true
	}
	_, err := startup.Respawn(agent.Role, w.config.World, agent.Name, startup.LaunchOpts{
		Sessions:   w.sessions,
		WritExists: writExists,
	})
	if err != nil {
		return fmt.Errorf("failed to respawn session for %s: %w", agent.Name, err)
	}

	key := respawnKey{AgentID: agent.ID, WritID: agent.ActiveWrit}
	attempts := w.respawnCounts[key]

	if w.logger != nil {
		w.logger.Emit(events.EventRespawn, w.agentID(), agent.ID, "both",
			map[string]any{
				"agent":     agent.ID,
				"writ": agent.ActiveWrit,
				"attempt":   attempts,
			})
	}

	// Send informational protocol message.
	if _, err := w.sphereStore.SendProtocolMessage(
		w.agentID(), config.Autarch,
		store.ProtoRecoveryNeeded,
		store.RecoveryNeededPayload{
			AgentID:    agent.ID,
			WritID: agent.ActiveWrit,
			Reason:     fmt.Sprintf("respawned (attempt %d)", attempts),
			Attempts:   attempts,
		},
	); err != nil && w.logger != nil {
		w.logger.Emit("mail_error", w.agentID(), agent.ID, "audit",
			map[string]any{"error": err.Error()})
	}

	return nil
}

// returnWorkToOpen returns a stalled agent's writ to the open pool
// after exceeding max respawn attempts.
func (w *Sentinel) returnWorkToOpen(agent store.Agent) error {
	// CRASH SAFETY: Update writ to 'open' FIRST, then set agent to 'idle'.
	// Consul's stale-tether recovery queries agents with state = 'working'.
	// If we crash after step 1 but before step 2: the agent is still 'working'
	// — visible to consul — and consul will complete the recovery on its next
	// patrol (updating an already-open writ is idempotent). If instead we set
	// the agent idle first and then crash, the agent becomes 'idle' and
	// invisible to consul, leaving the writ permanently stuck.

	// 1. Attempt to return writ to open. SafelyReopenWrit only updates if the
	// writ is still in tethered or working status, guarding against the case
	// where dispatch.Resolve already flipped it to 'done' before the session
	// died. Done/closed writs must not be silently reverted to open.
	//
	// Crash safety ordering is preserved: we attempt the writ update FIRST. If
	// we crash here the agent is still 'working' and consul can complete recovery.
	// If the writ turns out to be terminal (done/closed) we skip the writ update
	// but still clean up the agent record below.
	if agent.ActiveWrit != "" {
		reopened, err := w.worldStore.SafelyReopenWrit(
			agent.ActiveWrit,
			[]string{store.WritTethered, store.WritWorking},
		)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("failed to return writ %s to open: %w", agent.ActiveWrit, err)
		}
		if !reopened && err == nil && w.logger != nil {
			// Writ is in a terminal or non-matching status (done/closed/open) —
			// do not flip it back. Emit an audit event for observability.
			w.logger.Emit("sentinel_skip_reopen_terminal", w.agentID(), agent.ID, "audit",
				map[string]any{
					"agent": agent.ID,
					"writ":  agent.ActiveWrit,
				})
		}
	}

	// 2. Set agent state → idle, clear active_writ.
	// Done after writ update — now safe since writ is already open.
	if err := w.sphereStore.UpdateAgentState(agent.ID, "idle", ""); err != nil {
		return fmt.Errorf("failed to set agent %s idle: %w", agent.ID, err)
	}

	// 3. Clean up all agent resources (worktree, session metadata, tether, etc.).
	w.cleanupAgentResources(agent.Name, agent.Role)

	// 4. Clear respawn count.
	key := respawnKey{AgentID: agent.ID, WritID: agent.ActiveWrit}
	delete(w.respawnCounts, key)

	// 5. Emit stalled event with recovered: false.
	if w.logger != nil {
		w.logger.Emit(events.EventStalled, w.agentID(), agent.ID, "both",
			map[string]any{
				"agent":     agent.ID,
				"writ": agent.ActiveWrit,
				"recovered": false,
			})
	}

	// 6. Send RECOVERY_NEEDED protocol message to autarch.
	if _, err := w.sphereStore.SendProtocolMessage(
		w.agentID(), config.Autarch,
		store.ProtoRecoveryNeeded,
		store.RecoveryNeededPayload{
			AgentID:    agent.ID,
			WritID: agent.ActiveWrit,
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
// Stops the session and cleans up tether files and worktree so the agent
// can be reaped promptly rather than waiting for the idle reap cycle.
func (w *Sentinel) handleZombie(agent store.Agent) error {
	sessionName := config.SessionName(w.config.World, agent.Name)
	if err := w.sessions.Stop(sessionName, false); err != nil && !errors.Is(err, session.ErrNotFound) {
		return fmt.Errorf("failed to stop zombie session %s: %w", sessionName, err)
	}

	// Clean up tether files so the agent transitions to idle promptly.
	if err := tether.Clear(w.config.World, agent.Name, agent.Role); err != nil {
		if w.logger != nil {
			w.logger.Emit("sentinel_warn", w.agentID(), agent.ID, "audit", map[string]any{
				"action": "zombie_tether_clear",
				"agent":  agent.Name,
				"error":  err.Error(),
			})
		}
	}

	return nil
}

// handleOrphanedWorking handles an agent that is marked "working" with a dead
// session and no tether file on disk. This state occurs when cast crashes before
// writing the tether, consul's stale-tether recovery clears the tether while agent
// DB state is still "working", or a persistent agent's tethers are cleared externally.
//
// For outpost agents: full cleanup and delete. If the active writ is still
// "tethered", return it to "open" for recast. If "done", leave it for the MR pipeline.
// For persistent agents: set idle and clear active_writ.
// Always cleans up the .resolve_in_progress lock file if present.
func (w *Sentinel) handleOrphanedWorking(agent store.Agent) error {
	resolveWasInProgress := dispatch.IsResolveInProgress(w.config.World, agent.Name, agent.Role)

	if agent.Role == "outpost" {
		// Outpost: clean up entirely.
		// If resolve was in progress, the work was being submitted — MR may exist.
		// If not, tether was lost. Either way, agent is stuck and useless.
		//
		// Order matters for crash safety: update writ FIRST so we never
		// delete the agent record while the writ still references it
		// (which would create an unrecoverable "ghost tether").

		// Step 1: If active writ exists and is still assigned to this agent, return it to open.
		if agent.ActiveWrit != "" && w.worldStore != nil {
			// Acquire the dispatch WritLock (non-blocking) to avoid racing with
			// concurrent dispatch operations (Cast/Resolve). If the lock is held, a
			// dispatch operation is actively modifying this writ — skip recovery for
			// now; sentinel will retry on the next patrol cycle.
			writLock, lockErr := flock.AcquireWritLock(agent.ActiveWrit)
			if lockErr != nil {
				slog.Info("sentinel: writ lock held, skipping orphaned writ recovery",
					"agent", agent.ID, "writ", agent.ActiveWrit)
				return nil
			}
			defer writLock.Release()

			// Re-read the writ under the lock to verify it is still assigned to this
			// agent. If the assignee differs, the writ was re-dispatched to another
			// agent while this one was orphaned — clobbering that assignment would
			// break the new agent's resolve.
			item, getErr := w.worldStore.GetWrit(agent.ActiveWrit)
			if getErr == nil {
				if item.Assignee != agent.ID {
					slog.Info("sentinel: orphaned writ reassigned, skipping reopen",
						"agent", agent.ID, "writ", agent.ActiveWrit, "current_assignee", item.Assignee)
				} else {
					// Writ is still ours — safely reopen it. SafelyReopenWrit is a
					// conditional UPDATE that guards against reopening writs already in
					// terminal status (done/closed).
					if _, reopenErr := w.worldStore.SafelyReopenWrit(
						agent.ActiveWrit,
						[]string{store.WritTethered, store.WritWorking},
					); reopenErr != nil {
						// Agent record still exists — safe to return error and retry next patrol.
						return fmt.Errorf("failed to return orphaned writ %s to open: %w", agent.ActiveWrit, reopenErr)
					}
				}
			}
			// If getErr != nil (writ not found or DB error), skip the reopen and proceed
			// with agent cleanup — the writ state cannot be determined safely.
		}

		// Step 2: Clean up agent resources (tether files, worktree).
		w.cleanupAgentResources(agent.Name, agent.Role)

		// Step 3: Delete agent record last — writ is already freed.
		if err := w.sphereStore.DeleteAgent(agent.ID); err != nil {
			return fmt.Errorf("failed to delete orphaned agent %s: %w", agent.ID, err)
		}
	} else {
		// Persistent agent: set idle, clear active_writ.
		if err := w.sphereStore.UpdateAgentState(agent.ID, "idle", ""); err != nil {
			return fmt.Errorf("failed to set orphaned agent %s idle: %w", agent.ID, err)
		}
	}

	// Clean up resolve lock(s) if present (shared or per-writ).
	if resolveWasInProgress {
		dispatch.ClearResolveLocksForAgent(w.config.World, agent.Name, agent.Role)
	}

	// Emit event for observability.
	if w.logger != nil {
		w.logger.Emit(events.EventStalled, w.agentID(), agent.ID, "both", map[string]any{
			"agent":               agent.ID,
			"writ":                agent.ActiveWrit,
			"recovered":           true,
			"reason":              "orphaned_working_no_tether",
			"resolve_in_progress": resolveWasInProgress,
		})
	}

	return nil
}

// reapIdleAgent deletes an idle agent record that has exceeded the reap timeout.
// Cleans up any lingering worktree, session metadata, tether, and workflow files.
func (w *Sentinel) reapIdleAgent(agent store.Agent) error {
	// 1. Clean up all agent resources on disk.
	w.cleanupAgentResources(agent.Name, agent.Role)

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

// reapClosedWritAgent reaps an outpost agent whose tethered writ has been closed
// (cancelled, superseded, etc.). Stops the session, clears the tether, and deletes
// the agent record — same reap path as idle agent cleanup.
//
// Crash-safety ordering: cleanup and deletion happen while the agent is still
// "working". If sentinel crashes mid-cleanup the agent remains visible to
// consul/sentinel recovery. The idle transition is omitted entirely because
// the agent record is deleted immediately after cleanup.
func (w *Sentinel) reapClosedWritAgent(agent store.Agent, sessionName, closeReason string) error {
	if w.logger != nil {
		w.logger.Emit(events.EventReap, w.agentID(), agent.ID, "both",
			map[string]any{
				"agent":        agent.ID,
				"writ":         agent.ActiveWrit,
				"close_reason": closeReason,
				"reason":       "writ closed",
			})
	}

	// 1. Clean up all agent resources (session, worktree, tether, etc.)
	// while the agent is still "working" — crash here leaves the agent
	// visible to consul/sentinel recovery.
	w.cleanupAgentResources(agent.Name, agent.Role)

	// 2. Delete the agent record to free the name pool slot.
	// No idle transition needed — we are deleting the record outright.
	if err := w.sphereStore.DeleteAgent(agent.ID); err != nil {
		return fmt.Errorf("failed to delete agent %s: %w", agent.ID, err)
	}

	return nil
}

// checkHandoffFrequency checks if any working agent has handed off too frequently.
// 3+ handoffs in 30 minutes signals a possible handoff loop — burning tokens
// without making meaningful progress.
func (w *Sentinel) checkHandoffFrequency(agents []store.Agent) int {
	if w.eventReader == nil {
		return 0
	}

	window := 30 * time.Minute
	threshold := 3
	var escalated int

	for _, agent := range agents {
		if agent.State != "working" {
			continue
		}

		handoffs, err := w.eventReader.Read(events.ReadOpts{
			Type:  events.EventHandoff,
			Actor: agent.Name,
			Since: w.now().Add(-window),
		})
		if err != nil {
			continue
		}

		if len(handoffs) >= threshold {
			escalated++

			if w.logger != nil {
				w.logger.Emit("handoff_loop", w.agentID(), agent.ID, "both",
					map[string]any{
						"agent":    agent.ID,
						"handoffs": len(handoffs),
						"window":   window.String(),
					})
			}

			if _, err := w.sphereStore.SendProtocolMessage(
				w.agentID(), config.Autarch,
				store.ProtoRecoveryNeeded,
				store.RecoveryNeededPayload{
					AgentID:    agent.ID,
					WritID: agent.ActiveWrit,
					Reason:     fmt.Sprintf("handoff loop: %d handoffs in %s", len(handoffs), window),
				},
			); err != nil && w.logger != nil {
				w.logger.Emit("mail_error", w.agentID(), agent.ID, "audit",
					map[string]any{"error": err.Error()})
			}
		}
	}

	return escalated
}
