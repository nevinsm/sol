package consul

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
)

// checkAgingEscalations re-notifies unacknowledged escalations that have aged
// past their severity threshold. Low severity escalations are never re-notified.
// Returns the number of escalations re-notified.
func (d *Consul) checkAgingEscalations(ctx context.Context) (int, error) {
	escs, err := d.sphereStore.ListOpenEscalations()
	if err != nil {
		return 0, fmt.Errorf("failed to list open escalations: %w", err)
	}

	var renotified int
	for _, esc := range escs {
		if ctx.Err() != nil {
			return renotified, ctx.Err()
		}

		// Skip acknowledged escalations — acknowledgment stops re-notification.
		if esc.Acknowledged {
			continue
		}

		// Look up aging threshold by severity.
		threshold, err := d.config.EscalationConfig.AgingThreshold(esc.Severity)
		if err != nil {
			d.logInfo("consul_error", map[string]any{
				"action":       "aging_threshold",
				"escalation_id": esc.ID,
				"error":        err.Error(),
			})
			continue
		}

		// Low severity (threshold == 0): skip, never re-notified.
		if threshold == 0 {
			continue
		}

		// Determine effective last notification time.
		lastNotified := esc.CreatedAt
		if esc.LastNotifiedAt != nil {
			lastNotified = *esc.LastNotifiedAt
		}

		// Check if enough time has elapsed since last notification.
		if time.Since(lastNotified) < threshold {
			continue
		}

		// Re-notify via Router.
		if d.router != nil {
			if routeErr := d.router.Route(ctx, esc); routeErr != nil {
				d.logInfo("consul_error", map[string]any{
					"action":       "aging_renotify",
					"escalation_id": esc.ID,
					"error":        routeErr.Error(),
				})
				// Do NOT update last_notified_at — failed delivery should
				// not delay the next retry. On-call must receive the alert.
				continue
			}
		}

		// Update last_notified_at in the database only after successful routing.
		if updateErr := d.sphereStore.UpdateEscalationLastNotified(esc.ID); updateErr != nil {
			d.logInfo("consul_error", map[string]any{
				"action":       "update_last_notified",
				"escalation_id": esc.ID,
				"error":        updateErr.Error(),
			})
			continue // DEGRADE: skip this escalation
		}

		renotified++

		if d.logger != nil {
			d.logger.Emit(events.EventConsulEscRenotified, "sphere/consul", "sphere/consul", "feed",
				map[string]any{
					"escalation_id": esc.ID,
					"severity":      esc.Severity,
				})
		}
	}

	return renotified, nil
}

// buildupPayload is the JSON body sent for escalation buildup alerts.
type buildupPayload struct {
	Type      string `json:"type"`
	Count     int    `json:"count"`
	Threshold int    `json:"threshold"`
}

// checkEscalationBuildup fires a webhook if open escalation count exceeds the
// configured threshold. Debounced to 30 minutes between alerts.
// Returns true if a buildup alert was fired.
func (d *Consul) checkEscalationBuildup(ctx context.Context) bool {
	if d.config.EscalationWebhook == "" {
		return false
	}

	threshold := d.config.EscalationThreshold
	if threshold <= 0 {
		threshold = 5
	}

	count, err := d.sphereStore.CountOpen()
	if err != nil {
		d.logInfo("consul_error", map[string]any{"action": "buildup_count", "error": err.Error()})
		return false
	}

	if count < threshold {
		return false
	}

	// Debounce: 30 minutes between buildup alerts.
	if !d.lastEscalationAlert.IsZero() && time.Since(d.lastEscalationAlert) < 30*time.Minute {
		return false
	}

	// Fire webhook directly (NOT through the Router).
	payload := buildupPayload{
		Type:      "escalation_buildup",
		Count:     count,
		Threshold: threshold,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		d.logInfo("consul_error", map[string]any{"action": "buildup_marshal", "error": err.Error()})
		return false
	}

	webhookCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(webhookCtx, http.MethodPost, d.config.EscalationWebhook, bytes.NewReader(body))
	if err != nil {
		d.logInfo("consul_error", map[string]any{"action": "buildup_request", "error": err.Error()})
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "sol-escalation/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		d.logInfo("consul_error", map[string]any{"action": "buildup_webhook", "error": err.Error()})
		return false
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		d.logInfo("consul_error", map[string]any{
			"action":      "buildup_webhook",
			"status_code": resp.StatusCode,
		})
		return false
	}

	d.lastEscalationAlert = time.Now()

	// Emit event.
	if d.logger != nil {
		d.logger.Emit(events.EventConsulEscalationAlert, "sphere/consul", "sphere/consul", "both",
			map[string]any{
				"count":     count,
				"threshold": threshold,
			})
	}

	return true
}

// resolveStaleSourceRefs auto-resolves escalations whose source_ref points to
// a closed writ or merged/superseded MR. This is the consul fallback for crash
// recovery — catches escalations missed due to crashes or races.
func (d *Consul) resolveStaleSourceRefs(ctx context.Context) {
	escs, err := d.sphereStore.ListOpenEscalations()
	if err != nil {
		d.logInfo("consul_error", map[string]any{"action": "stale_source_ref_list", "error": err.Error()})
		return
	}

	for _, esc := range escs {
		if ctx.Err() != nil {
			return
		}

		if esc.SourceRef == "" {
			continue
		}

		if strings.HasPrefix(esc.SourceRef, "writ:") {
			d.resolveStaleWrit(ctx, esc)
		} else if strings.HasPrefix(esc.SourceRef, "mr:") {
			d.resolveStaleMR(ctx, esc)
		}
	}
}

// resolveStaleWrit checks if an escalation's linked writ is closed and
// auto-resolves the escalation if so.
func (d *Consul) resolveStaleWrit(ctx context.Context, esc store.Escalation) {
	writID := strings.TrimPrefix(esc.SourceRef, "writ:")
	if writID == "" {
		return
	}

	// Scan world DBs to find the writ.
	worlds, err := d.sphereStore.ListWorlds()
	if err != nil {
		d.logInfo("consul_error", map[string]any{
			"action":       "stale_writ_list_worlds",
			"escalation_id": esc.ID,
			"error":        err.Error(),
		})
		return
	}

	for _, world := range worlds {
		if ctx.Err() != nil {
			return
		}

		worldStore, err := d.worldOpener(world.Name)
		if err != nil {
			// DEGRADE: skip this world, try next.
			d.logInfo("consul_error", map[string]any{
				"action":       "stale_writ_open_world",
				"world":        world.Name,
				"escalation_id": esc.ID,
				"error":        err.Error(),
			})
			continue
		}

		writ, err := worldStore.GetWrit(writID)
		worldStore.Close()

		if err != nil {
			// Writ not found in this world — try next.
			continue
		}

		if writ.Status == "closed" {
			if resolveErr := d.sphereStore.ResolveEscalation(esc.ID); resolveErr != nil {
				d.logInfo("consul_error", map[string]any{
					"action":       "stale_writ_resolve",
					"escalation_id": esc.ID,
					"error":        resolveErr.Error(),
				})
			} else {
				d.logInfo("consul_stale_resolve", map[string]any{
					"escalation_id": esc.ID,
					"source_ref":    esc.SourceRef,
					"reason":        "writ_closed",
				})
			}
		}
		return // Found the writ's world, done searching.
	}
}

// resolveStaleMR checks if an escalation's linked MR is merged or superseded
// and auto-resolves the escalation if so.
func (d *Consul) resolveStaleMR(ctx context.Context, esc store.Escalation) {
	mrID := strings.TrimPrefix(esc.SourceRef, "mr:")
	if mrID == "" {
		return
	}

	// Scan world DBs to find the MR.
	worlds, err := d.sphereStore.ListWorlds()
	if err != nil {
		d.logInfo("consul_error", map[string]any{
			"action":       "stale_mr_list_worlds",
			"escalation_id": esc.ID,
			"error":        err.Error(),
		})
		return
	}

	for _, world := range worlds {
		if ctx.Err() != nil {
			return
		}

		worldStore, err := d.worldOpener(world.Name)
		if err != nil {
			// DEGRADE: skip this world, try next.
			d.logInfo("consul_error", map[string]any{
				"action":       "stale_mr_open_world",
				"world":        world.Name,
				"escalation_id": esc.ID,
				"error":        err.Error(),
			})
			continue
		}

		mr, err := worldStore.GetMergeRequest(mrID)
		worldStore.Close()

		if err != nil {
			// MR not found in this world — try next.
			continue
		}

		if mr.Phase == "merged" || mr.Phase == "superseded" {
			if resolveErr := d.sphereStore.ResolveEscalation(esc.ID); resolveErr != nil {
				d.logInfo("consul_error", map[string]any{
					"action":       "stale_mr_resolve",
					"escalation_id": esc.ID,
					"error":        resolveErr.Error(),
				})
			} else {
				d.logInfo("consul_stale_resolve", map[string]any{
					"escalation_id": esc.ID,
					"source_ref":    esc.SourceRef,
					"reason":        "mr_" + mr.Phase,
				})
			}
		}
		return // Found the MR's world, done searching.
	}
}
