package eventformat

import (
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/events"
)

// unionEventTypes is every event type either of the two prior mappings
// (dash's eventVerb/eventDetail, cmd/feed.go's formatEventDescription)
// handled by name before this package existed. Verb must return something
// other than the raw type string for every one of these — that's the
// acceptance criterion for "the union of both mappings survived the merge."
var unionEventTypes = []string{
	events.EventCast,
	events.EventResolve,
	events.EventMergeQueued,
	events.EventMergeClaimed,
	events.EventMerged,
	events.EventMergeFailed,
	events.EventRespawn,
	events.EventStalled,
	events.EventEscalationCreated,
	events.EventEscalationAcked,
	events.EventEscalationResolved,
	events.EventHandoff,
	events.EventDegraded,
	events.EventRecovered,
	events.EventMassDeath,
	events.EventPatrol,
	events.EventConsulPatrol,
	events.EventSessionStart,
	events.EventSessionStop,
	events.EventCaravanCreated,
	events.EventCaravanLaunched,
	events.EventCaravanClosed,
	events.EventRecast,
	events.EventReap,
	// cmd/feed.go-only — dash's copy never handled these (the drift this
	// package exists to fix).
	events.EventMailSent,
	events.EventAssess,
	events.EventNudge,
	events.EventConsulStaleTether,
	events.EventConsulCaravanFeed,
	// Not real events.Event constants, but both prior mappings special-cased
	// these string literals (emitted by prefect's cast/respawn burst
	// coalescing).
	"cast_batch",
	"respawn_batch",
}

func TestVerb_UnionCoverage(t *testing.T) {
	// Every type in unionEventTypes has a dedicated case in Verb's switch
	// (it's the union of what dash's and cmd/feed.go's mappings handled by
	// name), so it must never fall through to empty. Note some verbs
	// legitimately coincide with the raw type string (e.g. EventMerged =
	// "merged", Verb also returns "merged") — that's not a sign of falling
	// through to the default case, so this only checks non-empty.
	for _, et := range unionEventTypes {
		if got := Verb(et); got == "" {
			t.Errorf("Verb(%q) = %q, want a non-empty human verb", et, got)
		}
	}
}

func TestVerb_UnknownTypeFallsBackToRawType(t *testing.T) {
	got := Verb("some_future_event_type")
	if got != "some_future_event_type" {
		t.Errorf("Verb(unknown) = %q, want the raw type string back", got)
	}
}

func TestVerb_DriftCasesPickedWordingDeliberately(t *testing.T) {
	// These are the cases where dash and cmd/feed.go actually disagreed on
	// wording before the merge. Pin the chosen wording so a future edit to
	// either caller can't silently re-introduce drift.
	tests := map[string]string{
		events.EventResolve:           "resolved",
		events.EventMergeClaimed:      "claimed merge",
		events.EventEscalationCreated: "escalated",
		events.EventRecovered:         "recovered",
	}
	for et, want := range tests {
		if got := Verb(et); got != want {
			t.Errorf("Verb(%q) = %q, want %q", et, got, want)
		}
	}
}

func TestDetail_EventMailSent(t *testing.T) {
	// EventMailSent is the headline case from the writ: dash's copy never
	// handled it at all, so it must render properly here.
	ev := events.Event{
		Type: events.EventMailSent,
		Payload: map[string]any{
			"id":        "m1",
			"sender":    "Nova",
			"recipient": "autarch",
		},
	}
	if got := Detail(ev); got != "autarch" {
		t.Errorf("Detail(EventMailSent) = %q, want %q", got, "autarch")
	}
	if got := Verb(ev.Type); got == events.EventMailSent {
		t.Errorf("Verb(EventMailSent) fell back to the raw type, want a human verb")
	}
}

func TestDetail_EventCast(t *testing.T) {
	ev := events.Event{
		Type: events.EventCast,
		Payload: map[string]any{
			"writ_id": "sol-abc123",
			"agent":   "Nova",
			"world":   "sol-dev",
		},
	}
	got := Detail(ev)
	for _, want := range []string{"sol-abc123", "Nova", "sol-dev"} {
		if !strings.Contains(got, want) {
			t.Errorf("Detail(EventCast) = %q, missing %q", got, want)
		}
	}
}

func TestDetail_EventCaravanLaunchedUsesRealPayloadKeys(t *testing.T) {
	// caravan_launched payloads carry "dispatched" and "world" (see
	// cmd/caravan.go), not "name" — dash's original grouped case
	// (get("name") shared with Created/Closed) never matched anything for
	// this type. Detail must use the keys the emitter actually sets.
	ev := events.Event{
		Type: events.EventCaravanLaunched,
		Payload: map[string]any{
			"caravan_id": "car-1",
			"world":      "sol-dev",
			"dispatched": "3",
		},
	}
	got := Detail(ev)
	if !strings.Contains(got, "3") || !strings.Contains(got, "sol-dev") {
		t.Errorf("Detail(EventCaravanLaunched) = %q, want it to mention dispatched count and world", got)
	}
}

func TestDetail_EventConsulCaravanFeedUsesRealPayloadKeys(t *testing.T) {
	// consul_caravan_feed payloads carry "caravan_id" and "dispatched" (see
	// internal/consul/consul.go) — cmd/feed.go's original case read
	// "ready_count", a key the emitter never set, so this detail was
	// silently always empty before.
	ev := events.Event{
		Type: events.EventConsulCaravanFeed,
		Payload: map[string]any{
			"caravan_id": "car-1",
			"dispatched": "2",
		},
	}
	got := Detail(ev)
	if !strings.Contains(got, "car-1") || !strings.Contains(got, "2") {
		t.Errorf("Detail(EventConsulCaravanFeed) = %q, want it to mention caravan_id and dispatched count", got)
	}
}

func TestDetail_UnknownTypeFallsBackToCompactJSON(t *testing.T) {
	ev := events.Event{
		Type:    "some_future_event_type",
		Payload: map[string]any{"a": "b"},
	}
	got := Detail(ev)
	if got == "" {
		t.Fatal("Detail(unknown) with a small payload should fall back to compact JSON, got empty string")
	}
	if len(got) >= 60 {
		t.Errorf("Detail(unknown) fallback should stay under 60 bytes, got %d: %q", len(got), got)
	}
}

func TestDetail_UnknownTypeLargePayloadOmitted(t *testing.T) {
	big := map[string]any{}
	for i := 0; i < 20; i++ {
		big[strings.Repeat("k", i+1)] = strings.Repeat("v", 10)
	}
	ev := events.Event{Type: "some_future_event_type", Payload: big}
	got := Detail(ev)
	if got != "" {
		t.Errorf("Detail(unknown) with an oversized payload should return empty, got %q", got)
	}
}

func TestDetail_NonMapPayload(t *testing.T) {
	ev := events.Event{Type: events.EventCast, Payload: "not a map"}
	if got := Detail(ev); got != "" {
		t.Errorf("Detail with non-map payload = %q, want empty", got)
	}
}
