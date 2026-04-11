package agents

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/events"
)

func TestFromEvent(t *testing.T) {
	ts := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	payload := map[string]any{
		"world":       "sol-dev",
		"agent":       "Polaris",
		"reason":      "context_exhaustion",
		"session_age": "2h30m",
		"writ_id":     "sol-a1b2c3d4e5f6a7b8",
	}

	ev := events.Event{
		Timestamp:  ts,
		Source:     "sol",
		Type:       events.EventHandoff,
		Actor:      "Polaris",
		Visibility: "feed",
		Payload:    payload,
	}

	got := FromEvent(ev)

	if !got.Timestamp.Equal(ts) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, ts)
	}
	if got.Source != "sol" {
		t.Errorf("Source = %q, want %q", got.Source, "sol")
	}
	if got.Type != "handoff" {
		t.Errorf("Type = %q, want %q", got.Type, "handoff")
	}
	if got.Actor != "Polaris" {
		t.Errorf("Actor = %q, want %q", got.Actor, "Polaris")
	}
	if got.Visibility != "feed" {
		t.Errorf("Visibility = %q, want %q", got.Visibility, "feed")
	}
	if got.Payload == nil {
		t.Fatal("Payload should not be nil")
	}
}

func TestFromEvents(t *testing.T) {
	ts1 := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	ts2 := time.Date(2026, 4, 10, 13, 0, 0, 0, time.UTC)

	evs := []events.Event{
		{Timestamp: ts1, Source: "sol", Type: "handoff", Actor: "Polaris", Visibility: "feed", Payload: map[string]any{"world": "sol-dev"}},
		{Timestamp: ts2, Source: "sol", Type: "handoff", Actor: "Nova", Visibility: "feed", Payload: map[string]any{"world": "sol-dev"}},
	}

	got := FromEvents(evs)

	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Actor != "Polaris" {
		t.Errorf("got[0].Actor = %q, want %q", got[0].Actor, "Polaris")
	}
	if got[1].Actor != "Nova" {
		t.Errorf("got[1].Actor = %q, want %q", got[1].Actor, "Nova")
	}
}

func TestFromEventsEmpty(t *testing.T) {
	got := FromEvents([]events.Event{})

	if got == nil {
		t.Fatal("FromEvents should return non-nil slice for empty input")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}

	// Verify JSON encodes as [] not null.
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("JSON = %s, want []", data)
	}
}

func TestHandoffEventJSONShape(t *testing.T) {
	ts := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	payload := map[string]any{"world": "sol-dev", "agent": "Polaris"}

	he := HandoffEvent{
		Timestamp:  ts,
		Source:     "sol",
		Type:       "handoff",
		Actor:      "Polaris",
		Visibility: "feed",
		Payload:    payload,
	}

	data, err := json.Marshal(he)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error unmarshaling: %v", err)
	}

	// HandoffEvent must use normalized "occurred_at" key (not "ts").
	if _, ok := got["occurred_at"]; !ok {
		t.Error("HandoffEvent JSON missing key \"occurred_at\"")
	}
	if _, ok := got["ts"]; ok {
		t.Error("HandoffEvent JSON must not contain old key \"ts\"; expected \"occurred_at\"")
	}
	if got["occurred_at"] != "2026-04-10T12:00:00Z" {
		t.Errorf("occurred_at = %v, want 2026-04-10T12:00:00Z", got["occurred_at"])
	}
	// Other fields must still be present.
	for _, key := range []string{"source", "type", "actor", "visibility", "payload"} {
		if _, ok := got[key]; !ok {
			t.Errorf("HandoffEvent JSON missing key %q", key)
		}
	}
	// Verify the internal events.Event still uses the old "ts" key (different shape).
	ev := events.Event{
		Timestamp:  ts,
		Source:     "sol",
		Type:       "handoff",
		Actor:      "Polaris",
		Visibility: "feed",
		Payload:    payload,
	}
	evData, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("unexpected error marshaling Event: %v", err)
	}
	var evMap map[string]any
	if err := json.Unmarshal(evData, &evMap); err != nil {
		t.Fatalf("unexpected error unmarshaling Event: %v", err)
	}
	if _, ok := evMap["ts"]; !ok {
		t.Error("internal events.Event should still use key \"ts\"")
	}
}
