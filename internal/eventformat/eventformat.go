// Package eventformat is the canonical home for sol's event-type-to-text
// presentation: mapping an events.Event's Type to a human-readable
// past-tense verb (Verb) and its Payload to a compact target/context string
// (Detail). Both `sol dash`'s activity feed (internal/dash/feed.go) and
// `sol feed`'s human-readable output (cmd/feed.go) consume these so the two
// views never drift on how an event type reads to a human.
//
// Why a separate package? internal/dash/feed.go and cmd/feed.go each used
// to maintain their own independent event-type-to-text switch statements.
// They had already drifted by the time this was noticed: dash didn't know
// about EventMailSent (and several other event types cmd/feed.go handled),
// and cmd/feed.go was missing caravan/session-lifecycle/recast/reap
// handling dash had. This is the same bug class internal/statusformat was
// created to kill for process details — see that package's doc comment.
//
// This package has NO styling and NO bubbletea/lipgloss dependency — pure
// presentation. Truncation and column/layout formatting remain the
// caller's concern (dash's formatEvent, cmd/feed.go's printEvent).
//
// Coverage: Verb and Detail cover the union of event types either of the
// two prior mappings handled by name. Event types neither prior mapping
// knew about (e.g. broker/ledger/chronicle/quota lifecycle events) are not
// given dedicated cases here either — they fall through to the same
// defaults the prior mappings used (the raw type string for Verb, a
// compact JSON dump of the payload for Detail). Adding a case here is
// still the right move whenever a new event type needs a nicer rendering;
// nothing about this package limits it to the original union.
package eventformat

import (
	"encoding/json"
	"fmt"

	"github.com/nevinsm/sol/internal/events"
)

// Verb maps an event type to a human-readable past-tense verb describing
// what happened. Falls back to the raw event type string for types this
// package has no dedicated case for, so an unrecognized type still renders
// as *something* rather than disappearing.
//
// Where the two prior mappings (dash's eventVerb, cmd/feed.go's
// formatEventDescription) worded the same event type differently, one
// wording was picked deliberately for consistency — see the doc comment on
// each disputed case below.
func Verb(eventType string) string {
	switch eventType {
	case events.EventCast:
		return "dispatched"
	case events.EventResolve:
		// dash said "resolved"; cmd/feed.go said "completed". Picked
		// "resolved" — it matches the event type name (EventResolve).
		return "resolved"
	case events.EventMergeQueued:
		return "queued merge"
	case events.EventMergeClaimed:
		// dash said "claimed merge"; cmd/feed.go said "claimed <mr> for
		// merge". Picked dash's terser wording.
		return "claimed merge"
	case events.EventMerged:
		return "merged"
	case events.EventMergeFailed:
		return "merge failed"
	case events.EventRespawn:
		return "respawned"
	case events.EventStalled:
		return "stalled"
	case events.EventEscalationCreated:
		// dash said "escalated"; cmd/feed.go's sentence had no verb (it led
		// with the noun "Escalation"). Picked dash's verb form to match
		// Verb's past-tense-verb contract.
		return "escalated"
	case events.EventEscalationAcked:
		return "acknowledged escalation"
	case events.EventEscalationResolved:
		return "resolved escalation"
	case events.EventHandoff:
		return "handed off"
	case events.EventDegraded:
		return "entered degraded mode"
	case events.EventRecovered:
		// dash said "recovered"; cmd/feed.go said "exited degraded mode".
		// Picked dash's — a single verb, consistent with EventDegraded's
		// counterpart being a short phrase rather than a full sentence.
		return "recovered"
	case events.EventMassDeath:
		return "detected mass death"
	case events.EventPatrol:
		return "patrolled"
	case events.EventConsulPatrol:
		return "consul patrolled"
	case events.EventSessionStart:
		return "started session"
	case events.EventSessionStop:
		return "stopped session"
	case events.EventCaravanCreated:
		return "created caravan"
	case events.EventCaravanLaunched:
		return "launched caravan"
	case events.EventCaravanClosed:
		return "closed caravan"
	case events.EventRecast:
		return "recast"
	case events.EventReap:
		return "reaped"
	case events.EventMailSent:
		// cmd/feed.go only (dash had no case — the original drift this
		// package exists to fix).
		return "sent mail"
	case events.EventAssess:
		return "assessed"
	case events.EventNudge:
		return "nudged"
	case events.EventConsulStaleTether:
		return "recovered stale tether"
	case events.EventConsulCaravanFeed:
		return "fed caravan"
	case "cast_batch":
		return "dispatched batch"
	case "respawn_batch":
		return "respawned batch"
	default:
		return eventType
	}
}

// Detail extracts a compact target/context string from an event's payload —
// e.g. a writ ID, an MR ID plus world, or a caravan name. Returns "" when
// there is nothing worth showing. For event types with no dedicated case,
// falls back to a compact JSON dump of the payload when it's under 60
// bytes (dash's original fallback — keeps unknown-type output usable
// without risking an unbounded line).
func Detail(ev events.Event) string {
	payload, ok := ev.Payload.(map[string]any)
	if !ok {
		return ""
	}

	get := func(key string) string {
		if v, ok := payload[key]; ok {
			return fmt.Sprintf("%v", v)
		}
		return ""
	}

	switch ev.Type {
	case events.EventCast:
		writID := get("writ_id")
		agent := get("agent")
		world := get("world")
		switch {
		case writID != "" && agent != "" && world != "":
			return fmt.Sprintf("%s → %s (%s)", writID, agent, world)
		case writID != "" && world != "":
			return fmt.Sprintf("%s (%s)", writID, world)
		default:
			return writID
		}
	case events.EventResolve:
		return get("writ_id")
	case events.EventMergeQueued, events.EventMergeClaimed, events.EventMerged, events.EventMergeFailed:
		mrID := get("merge_request_id")
		world := get("world")
		if mrID != "" && world != "" {
			return fmt.Sprintf("MR %s (%s)", mrID, world)
		}
		return mrID
	case events.EventRespawn:
		agent := get("agent")
		world := get("world")
		if agent != "" && world != "" {
			return fmt.Sprintf("%s (%s)", agent, world)
		}
		return agent
	case events.EventStalled:
		return get("agent")
	case events.EventEscalationCreated:
		desc := get("description")
		severity := get("severity")
		if severity != "" && desc != "" {
			return fmt.Sprintf("[%s] %s", severity, desc)
		}
		return desc
	case events.EventEscalationAcked, events.EventEscalationResolved:
		return get("id")
	case events.EventHandoff:
		return get("writ_id")
	case events.EventCaravanCreated:
		name := get("name")
		count := get("count")
		if name != "" && count != "" {
			return fmt.Sprintf("%s (%s items)", name, count)
		}
		return name
	case events.EventCaravanLaunched:
		// caravan_launched payloads carry "dispatched" and "world", not
		// "name" (dash's original case grouped this with Created/Closed
		// under get("name"), which never matched anything for this type).
		dispatched := get("dispatched")
		world := get("world")
		if dispatched != "" && world != "" {
			return fmt.Sprintf("%s dispatched (%s)", dispatched, world)
		}
		return dispatched
	case events.EventCaravanClosed:
		return get("name")
	case events.EventMassDeath:
		deaths := get("deaths")
		window := get("window")
		if deaths != "" && window != "" {
			return fmt.Sprintf("%s deaths in %s", deaths, window)
		}
		return deaths
	case events.EventPatrol:
		return get("world")
	case events.EventConsulPatrol:
		count := get("patrol_count")
		stale := get("stale_tethers")
		feeds := get("caravan_feeds")
		if count != "" {
			return fmt.Sprintf("#%s (%s stale, %s feeds)", count, stale, feeds)
		}
		return ""
	case events.EventMailSent:
		return get("recipient")
	case events.EventAssess:
		agent := get("agent")
		status := get("status")
		confidence := get("confidence")
		if agent != "" && status != "" {
			return fmt.Sprintf("%s: %s (%s confidence)", agent, status, confidence)
		}
		return status
	case events.EventNudge:
		agent := get("agent")
		message := get("message")
		if agent != "" && message != "" {
			return fmt.Sprintf("%s: %s", agent, message)
		}
		return message
	case events.EventConsulStaleTether:
		agentID := get("agent_id")
		writID := get("writ_id")
		if agentID != "" && writID != "" {
			return fmt.Sprintf("%s (%s)", agentID, writID)
		}
		return agentID
	case events.EventConsulCaravanFeed:
		// consul_caravan_feed payloads carry "caravan_id" and "dispatched"
		// — cmd/feed.go's original case read "ready_count", a key the
		// emitter never sets, so this detail was silently always empty.
		caravanID := get("caravan_id")
		dispatched := get("dispatched")
		if caravanID != "" && dispatched != "" {
			return fmt.Sprintf("%s (%s dispatched)", caravanID, dispatched)
		}
		return caravanID
	case "cast_batch":
		return fmt.Sprintf("%s dispatches (%s)", get("count"), get("world"))
	case "respawn_batch":
		return fmt.Sprintf("%s respawns (%s)", get("count"), get("world"))
	default:
		// Fall back to a compact JSON of the payload.
		if len(payload) > 0 {
			data, err := json.Marshal(payload)
			if err == nil && len(data) < 60 {
				return string(data)
			}
		}
		return ""
	}
}
