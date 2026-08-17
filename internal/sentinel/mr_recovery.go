package sentinel

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// recastCountFromMetadata reads the persistent recast count from writ metadata.
func recastCountFromMetadata(item *store.Writ) int {
	if item.Metadata == nil {
		return 0
	}
	v, ok := item.Metadata["recast-count"]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

// resolutionDispatchCountFromMetadata reads the persistent resolution dispatch
// count from writ metadata, matching the recast-count pattern.
func resolutionDispatchCountFromMetadata(item *store.Writ) int {
	if item.Metadata == nil {
		return 0
	}
	v, ok := item.Metadata["resolution-dispatch-count"]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

// lastRecastTimeFromMetadata reads the persistent last recast timestamp from writ metadata.
func lastRecastTimeFromMetadata(item *store.Writ) time.Time {
	if item.Metadata == nil {
		return time.Time{}
	}
	v, ok := item.Metadata["recast-last"]
	if !ok {
		return time.Time{}
	}
	s, ok := v.(string)
	if !ok {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// recoverWrits runs all writ-recovery operations before agent monitoring.
// Covers MR recasting, conflict-resolution dispatch, stale claim release,
// orphaned-done recovery, and orphaned-tethered recovery.
// Returns counts for each operation for patrol event telemetry.
func (w *Sentinel) recoverWrits() (recastCount, resolutionDispatched, releasedCount, doneRecovered, tetheredRecovered int) {
	// Recast failed MRs before agent checks (so newly cast agents appear healthy).
	recastCount = w.recastFailedMRs()
	// Dispatch orphaned conflict-resolution writs blocking MRs.
	resolutionDispatched = w.dispatchOrphanedResolutions()
	// Release stale MR claims (forge crash recovery).
	releasedCount = w.releaseStaleClaims()
	// Recover writs stuck in "done" with no active MR (resolve crash recovery).
	doneRecovered = w.recoverOrphanedDoneWrits()
	// Recover writs stuck in "tethered" with orphaned assignees (agent died and was cleaned up).
	tetheredRecovered = w.recoverOrphanedTetheredWrits()
	return
}

// releaseStaleClaims releases MR claims older than ClaimTTL (forge crash recovery).
// Returns the number of released claims.
func (w *Sentinel) releaseStaleClaims() int {
	if w.worldStore == nil {
		return 0
	}
	released, err := w.worldStore.ReleaseStaleClaims(w.config.ClaimTTL, w.config.ForgeMaxAttempts)
	if err != nil {
		if w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
				map[string]any{"action": "release_stale_claims", "error": err.Error()})
		}
		return 0
	}
	if released > 0 && w.logger != nil {
		w.logger.Emit("sentinel_action", w.agentID(), w.agentID(), "feed",
			map[string]any{"action": "released_stale_claims", "count": released})
	}
	return released
}

// recastFailedMRs checks for merge requests in "failed" phase with open work
// items and re-casts them. Returns the number of writs re-cast.
// Uses exponential backoff (10m, 30m, 60m) between recast attempts.
// Caps retries at MaxRecastAttempts; after that, escalates to the autarch.
// Recast count is persisted in writ metadata to survive sentinel restarts.
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

	now := w.now()
	var recastCount int
	seen := make(map[string]bool) // deduplicate by writ

	for _, mr := range failedMRs {
		if seen[mr.WritID] {
			continue
		}
		seen[mr.WritID] = true

		// Dedup guard: skip if writ was recently cast (within 2× patrol interval).
		// Prevents race where sentinel sees writ as "open" between cast and tether.
		if t, ok := w.lastCastTime[mr.WritID]; ok {
			if now.Sub(t) < 2*w.config.PatrolInterval {
				continue
			}
		}

		item, err := w.worldStore.GetWrit(mr.WritID)
		if err != nil {
			continue
		}

		// Determine if this writ is eligible for recast based on status.
		switch item.Status {
		case "open":
			// Fall through to recast logic.
		case "done":
			// A "done" writ with a failed MR and no assigned agent is orphaned.
			// Transition it to "open" so it can be recast.
			if item.Assignee == "" || item.Assignee == "-" {
				if err := w.worldStore.UpdateWrit(mr.WritID, store.WritUpdates{
					Status:   "open",
					Assignee: "-",
				}); err != nil {
					continue
				}
				// Fall through to recast logic.
			} else {
				// Assignee field is non-empty — check if agent still exists.
				// If the agent was reaped, the assignee field is stale and the
				// writ should be recast rather than permanently skipped.
				_, agentErr := w.sphereStore.GetAgent(item.Assignee)
				if agentErr == nil {
					// Agent still exists — let them handle it.
					continue
				}
				// Agent doesn't exist (reaped) — treat as no assignee, recast.
				if err := w.worldStore.UpdateWrit(mr.WritID, store.WritUpdates{
					Status:   "open",
					Assignee: "-",
				}); err != nil {
					continue
				}
				// Fall through to recast logic.
			}
		default:
			// "tethered" or any other status — skip and prune dedup guard.
			// Tethered writs have an agent working on them (orphaned-working
			// fix handles dead agents separately).
			delete(w.lastCastTime, mr.WritID)
			continue
		}

		// Read persistent recast state from writ metadata.
		attempts := recastCountFromMetadata(item)
		lastRecastTime := lastRecastTimeFromMetadata(item)

		if attempts >= maxAttempts {
			if attempts == maxAttempts {
				// First time hitting max — escalate once.
				w.escalateFailedRecast(mr, item, attempts)
				// Mark escalated in metadata to prevent re-escalation.
				_ = w.worldStore.SetWritMetadata(mr.WritID, map[string]any{
					"recast-count": float64(maxAttempts + 1),
				})
			}
			continue
		}

		// Backoff check: ensure enough time has elapsed before next recast.
		// For the first recast, wait after MR failure (mr.UpdatedAt).
		// For subsequent recasts, wait after the last recast (from metadata).
		var referenceTime time.Time
		if attempts == 0 {
			referenceTime = mr.UpdatedAt
		} else {
			referenceTime = lastRecastTime
		}
		backoffIdx := attempts
		if backoffIdx >= len(recastBackoffIntervals) {
			backoffIdx = len(recastBackoffIntervals) - 1
		}
		if now.Sub(referenceTime) < recastBackoffIntervals[backoffIdx] {
			continue
		}

		// Check for existing active MRs to avoid creating duplicates.
		// "superseded" is terminal (not active), so only "ready", "claimed",
		// and "merged" block recast.
		existingMRs, err := w.worldStore.ListMergeRequestsByWrit(mr.WritID, "")
		if err == nil {
			hasActiveMR := false
			for _, emr := range existingMRs {
				if store.IsActiveMRPhase(emr.Phase) {
					hasActiveMR = true
					break
				}
			}
			if hasActiveMR {
				continue // Active MR exists, skip recast.
			}
		}

		result, err := w.castFn(mr.WritID)
		if err != nil {
			if w.logger != nil {
				w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
					map[string]any{
						"action": "recast",
						"mr":     mr.ID,
						"writ":   mr.WritID,
						"error":  err.Error(),
					})
			}
			continue
		}

		// Persist recast state in writ metadata (survives sentinel restart).
		_ = w.worldStore.SetWritMetadata(mr.WritID, map[string]any{
			"recast-count": float64(attempts + 1),
			"recast-last":  now.UTC().Format(time.RFC3339),
		})

		// Update dedup guard.
		w.lastCastTime[mr.WritID] = now
		recastCount++

		if w.logger != nil {
			w.logger.Emit(events.EventRecast, w.agentID(), w.agentID(), "both",
				map[string]any{
					"mr":      mr.ID,
					"writ":    mr.WritID,
					"agent":   result.AgentName,
					"attempt": attempts + 1,
				})
		}
	}

	return recastCount
}

// escalateFailedRecast sends a RECOVERY_NEEDED protocol message when a work
// item has exceeded the maximum recast attempts, and creates a formal
// escalation for durable tracking.
func (w *Sentinel) escalateFailedRecast(mr store.MergeRequest, item *store.Writ, attempts int) {
	// Check for existing open escalation to avoid duplicates (e.g. after restart).
	sourceRef := "writ:" + mr.WritID
	if existing, err := w.sphereStore.ListEscalationsBySourceRef(sourceRef); err == nil && len(existing) > 0 {
		// Open escalation already exists — skip creation.
	} else {
		// Create formal escalation for durable tracking.
		escDesc := fmt.Sprintf("Merge failed %d times for writ %s (%s), recast limit reached", attempts, mr.WritID, item.Title)
		if _, err := w.sphereStore.CreateEscalation("high", w.config.World+"/sentinel", escDesc, sourceRef); err != nil && w.logger != nil {
			w.logger.Emit("escalation_error", w.agentID(), w.agentID(), "audit",
				map[string]any{"error": err.Error()})
		}
	}

	// Send RECOVERY_NEEDED protocol message to autarch (live nudge).
	if _, err := w.sphereStore.SendProtocolMessage(
		w.agentID(), config.Autarch,
		store.ProtoRecoveryNeeded,
		store.RecoveryNeededPayload{
			WritID: mr.WritID,
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
				"writ":  mr.WritID,
				"mr":         mr.ID,
				"attempts":   attempts,
				"escalated":  true,
				"reason":     "max recast attempts exceeded",
			})
	}
}

// dispatchOrphanedResolutions finds blocked MRs whose resolution writs are
// open, unassigned, and older than 5 minutes, then dispatches them via castFn.
// This catches conflict-resolution writs that were not dispatched.
// Returns the number of writs dispatched.
func (w *Sentinel) dispatchOrphanedResolutions() int {
	if w.castFn == nil {
		return 0
	}

	blockedMRs, err := w.worldStore.ListBlockedMergeRequests()
	if err != nil {
		if w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
				map[string]any{"action": "list_blocked_mrs", "error": err.Error()})
		}
		return 0
	}

	maxAttempts := w.config.MaxRecastAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	const gracePeriod = 5 * time.Minute
	var dispatched int

	for _, mr := range blockedMRs {
		blockerID := mr.BlockedBy

		writ, err := w.worldStore.GetWrit(blockerID)
		if err != nil {
			// Writ not found or error — skip.
			continue
		}

		// Only dispatch if writ is open (not already handled or closed).
		if writ.Status != "open" {
			delete(w.resolutionDispatchCounts, blockerID)
			continue
		}

		// Skip if already assigned (agent is working on it).
		if writ.Assignee != "" {
			continue
		}

		// Grace period: let other processes handle the nudge first.
		if w.now().Sub(writ.CreatedAt) < gracePeriod {
			continue
		}

		// Read persistent dispatch count from writ metadata (survives restart).
		attempts := resolutionDispatchCountFromMetadata(writ)
		// Also check in-memory count and take the higher value to handle
		// the current patrol cycle's increments.
		if memCount := w.resolutionDispatchCounts[blockerID]; memCount > attempts {
			attempts = memCount
		}

		if attempts >= maxAttempts {
			if attempts == maxAttempts {
				// First time hitting max — escalate once.
				w.escalateOrphanedResolution(mr, writ, attempts)
				w.resolutionDispatchCounts[blockerID] = maxAttempts + 1
				// Persist escalated state in writ metadata.
				_ = w.worldStore.SetWritMetadata(blockerID, map[string]any{
					"resolution-dispatch-count": float64(maxAttempts + 1),
				})
			}
			continue
		}

		result, err := w.castFn(blockerID)
		if err != nil {
			if w.logger != nil {
				w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
					map[string]any{
						"action": "dispatch_resolution",
						"mr":     mr.ID,
						"writ":   blockerID,
						"error":  err.Error(),
					})
			}
			continue
		}

		w.resolutionDispatchCounts[blockerID] = attempts + 1
		// Persist dispatch count in writ metadata (survives sentinel restart).
		_ = w.worldStore.SetWritMetadata(blockerID, map[string]any{
			"resolution-dispatch-count": float64(attempts + 1),
		})
		dispatched++

		if w.logger != nil {
			w.logger.Emit("sentinel_dispatch_resolution", w.agentID(), w.agentID(), "both",
				map[string]any{
					"mr":      mr.ID,
					"writ":    blockerID,
					"agent":   result.AgentName,
					"attempt": attempts + 1,
					"message": fmt.Sprintf("auto-dispatching orphaned conflict-resolution writ %s blocking MR %s", blockerID, mr.ID),
				})
		}
	}

	return dispatched
}

// escalateOrphanedResolution sends a RECOVERY_NEEDED protocol message when a
// conflict-resolution writ has exceeded the maximum dispatch attempts, and
// creates a formal escalation for durable tracking.
func (w *Sentinel) escalateOrphanedResolution(mr store.MergeRequest, writ *store.Writ, attempts int) {
	// Check for existing open escalation to avoid duplicates (e.g. after restart).
	sourceRef := "writ:" + writ.ID
	if existing, err := w.sphereStore.ListEscalationsBySourceRef(sourceRef); err == nil && len(existing) > 0 {
		// Open escalation already exists — skip creation.
	} else {
		// Create formal escalation for durable tracking.
		escDesc := fmt.Sprintf("Orphaned conflict-resolution writ %s (%s) blocking MR %s, dispatch limit reached after %d attempts", writ.ID, writ.Title, mr.ID, attempts)
		if _, err := w.sphereStore.CreateEscalation("medium", w.config.World+"/sentinel", escDesc, sourceRef); err != nil && w.logger != nil {
			w.logger.Emit("escalation_error", w.agentID(), w.agentID(), "audit",
				map[string]any{"error": err.Error()})
		}
	}

	// Send RECOVERY_NEEDED protocol message to autarch (live nudge).
	if _, err := w.sphereStore.SendProtocolMessage(
		w.agentID(), config.Autarch,
		store.ProtoRecoveryNeeded,
		store.RecoveryNeededPayload{
			WritID: writ.ID,
			Reason:     fmt.Sprintf("orphaned conflict-resolution writ %q blocking MR %s, dispatch limit reached after %d attempts", writ.Title, mr.ID, attempts),
			Attempts:   attempts,
		},
	); err != nil && w.logger != nil {
		w.logger.Emit("mail_error", w.agentID(), w.agentID(), "audit",
			map[string]any{"error": err.Error()})
	}

	if w.logger != nil {
		w.logger.Emit(events.EventStalled, w.agentID(), w.agentID(), "both",
			map[string]any{
				"writ":     writ.ID,
				"mr":       mr.ID,
				"attempts": attempts,
				"escalated": true,
				"reason":   "max resolution dispatch attempts exceeded",
			})
	}
}

// recoverOrphanedDoneWrits detects writs stuck in "done" status with no active
// merge request. This state arises when the resolve process crashes between
// updating the writ status to "done" and creating the MR. Without recovery the
// writ stays in "done" forever — forge never sees it because there is no MR,
// and no agent picks it up because it is no longer "open".
//
// Recovery: reopen the writ to "open" so the normal dispatch flow can re-cast
// and re-resolve it. Only reopens writs that have no active MRs (ready/claimed)
// and no tethered agent — writs with active MRs are being handled by forge.
// Returns the number of writs recovered.
func (w *Sentinel) recoverOrphanedDoneWrits() int {
	if w.worldStore == nil {
		return 0
	}

	doneWrits, err := w.worldStore.ListWrits(store.ListFilters{Status: "done"})
	if err != nil {
		if w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
				map[string]any{"action": "list_done_writs", "error": err.Error()})
		}
		return 0
	}

	var recovered int
	for _, writ := range doneWrits {
		// Check if the writ has any active (non-failed, non-superseded) MRs.
		mrs, err := w.worldStore.ListMergeRequestsByWrit(writ.ID, "")
		if err != nil {
			continue
		}
		hasActiveMR := false
		for _, mr := range mrs {
			if store.IsActiveMRPhase(mr.Phase) {
				hasActiveMR = true
				break
			}
		}
		if hasActiveMR {
			continue // MR exists — forge will handle it
		}

		// Check if any agent is tethered to this writ (resolve may still be in progress).
		if writ.Assignee != "" {
			// Look up agent — if agent exists and is working, skip.
			agent, err := w.sphereStore.GetAgent(writ.Assignee)
			if err == nil && agent.State == "working" {
				continue // Healthy — agent is actively working.
			}
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				// Transient DB error — do not treat as "agent gone". Skip this
				// iteration; the writ will be reconsidered on the next patrol.
				if w.logger != nil {
					w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
						map[string]any{"action": "get_agent_for_done_writ", "writ": writ.ID, "error": err.Error()})
				}
				continue
			}
			// err is ErrNotFound or agent is not working — fall through to recovery.
		}

		// Grace period: only recover writs that have been stuck for at least 5 minutes.
		if w.now().Sub(writ.UpdatedAt) < 5*time.Minute {
			continue
		}

		// Reopen the writ so dispatch can re-cast it. SafelyReopenWrit is used
		// here to guard against a race where the writ moved to 'closed' between
		// the ListWrits call and now (e.g., closed by the operator).
		reopened, err := w.worldStore.SafelyReopenWrit(writ.ID, []string{store.WritDone})
		if err != nil {
			if w.logger != nil {
				w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
					map[string]any{"action": "reopen_orphaned_done_writ", "writ": writ.ID, "error": err.Error()})
			}
			continue
		}
		if !reopened {
			// Writ status changed since list (e.g., closed) — skip.
			if w.logger != nil {
				w.logger.Emit("sentinel_skip_reopen_terminal", w.agentID(), w.agentID(), "audit",
					map[string]any{"action": "skip_reopen_orphaned_done_writ", "writ": writ.ID})
			}
			continue
		}

		recovered++
		if w.logger != nil {
			w.logger.Emit("sentinel_recovery", w.agentID(), w.agentID(), "both",
				map[string]any{
					"action":  "reopened_orphaned_done_writ",
					"writ":    writ.ID,
					"title":   writ.Title,
					"message": fmt.Sprintf("reopened writ %s stuck in done with no active MR", writ.ID),
				})
		}
	}

	return recovered
}

// recoverOrphanedTetheredWrits detects writs stuck in "tethered" status whose
// assignee agent no longer exists in the sphere store (or is no longer working
// with no tether on disk), and reopens them. This covers the case where an
// outpost agent dies and sentinel cleans up the agent record + outpost directory,
// but the writ remains tethered with a dangling assignee.
//
// Recovery: reopen the writ to "open" so the normal dispatch flow can re-cast it.
// Returns the number of writs recovered.
func (w *Sentinel) recoverOrphanedTetheredWrits() int {
	if w.worldStore == nil {
		return 0
	}

	tetheredWrits, err := w.worldStore.ListWrits(store.ListFilters{Status: "tethered"})
	if err != nil {
		if w.logger != nil {
			w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
				map[string]any{"action": "list_tethered_writs", "error": err.Error()})
		}
		return 0
	}

	var recovered int
	for _, writ := range tetheredWrits {
		if writ.Assignee == "" {
			continue
		}

		// Look up the agent.
		agent, err := w.sphereStore.GetAgent(writ.Assignee)
		if err == nil {
			// Agent exists.
			if agent.State == "working" {
				continue // Healthy — agent is actively working.
			}
			// Agent exists but is not working — check if it still has tether files on disk.
			// Extract agent name from the assignee ID (format: "world/name").
			parts := strings.SplitN(writ.Assignee, "/", 2)
			if len(parts) == 2 && tether.IsTethered(w.config.World, parts[1], agent.Role) {
				continue // Tether files exist on disk — binding is still active.
			}
			// Agent is not working and has no tether files — stale binding, fall through to recovery.
		} else if !errors.Is(err, store.ErrNotFound) {
			// Transient DB error — do not treat as "agent gone". Skip this
			// iteration; the writ will be reconsidered on the next patrol.
			if w.logger != nil {
				w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
					map[string]any{"action": "get_agent_for_tethered_writ", "writ": writ.ID, "error": err.Error()})
			}
			continue
		}
		// If err is ErrNotFound: agent record is gone — fall through to recovery.

		// Grace period: only recover writs that have been stuck for at least 5 minutes.
		if w.now().Sub(writ.UpdatedAt) < 5*time.Minute {
			continue
		}

		// Reopen the writ so dispatch can re-cast it. SafelyReopenWrit guards
		// against a race where the writ status changed between list and update.
		reopened, err := w.worldStore.SafelyReopenWrit(writ.ID, []string{store.WritTethered})
		if err != nil {
			if w.logger != nil {
				w.logger.Emit("sentinel_error", w.agentID(), w.agentID(), "audit",
					map[string]any{"action": "reopen_orphaned_tethered_writ", "writ": writ.ID, "error": err.Error()})
			}
			continue
		}
		if !reopened {
			// Writ status changed since list (e.g., now working/done/closed) — skip.
			if w.logger != nil {
				w.logger.Emit("sentinel_skip_reopen_terminal", w.agentID(), w.agentID(), "audit",
					map[string]any{"action": "skip_reopen_orphaned_tethered_writ", "writ": writ.ID})
			}
			continue
		}

		recovered++
		if w.logger != nil {
			w.logger.Emit("sentinel_recovery", w.agentID(), w.agentID(), "both",
				map[string]any{
					"action":  "reopened_orphaned_tethered_writ",
					"writ":    writ.ID,
					"title":   writ.Title,
					"message": fmt.Sprintf("reopened writ %s stuck in tethered with orphaned assignee", writ.ID),
				})
		}
	}

	return recovered
}
