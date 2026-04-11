package events

import (
	"encoding/json"
	"testing"
	"time"

	ievents "github.com/nevinsm/sol/internal/events"
)

func TestFromEvent(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	ie := ievents.Event{
		Timestamp:  now,
		Source:     "sol",
		Type:       ievents.EventCast,
		Actor:      "autarch",
		Visibility: "feed",
		Payload: map[string]any{
			"writ_id": "sol-a1b2c3d4e5f6a7b8",
			"agent":   "Nova",
			"world":   "sol-dev",
		},
	}

	e := FromEvent(ie)

	if !e.Timestamp.Equal(now) {
		t.Errorf("Timestamp = %v, want %v", e.Timestamp, now)
	}
	if e.Source != "sol" {
		t.Errorf("Source = %q, want %q", e.Source, "sol")
	}
	if e.Type != "cast" {
		t.Errorf("Type = %q, want %q", e.Type, "cast")
	}
	if e.Actor != "autarch" {
		t.Errorf("Actor = %q, want %q", e.Actor, "autarch")
	}
	if e.Visibility != "feed" {
		t.Errorf("Visibility = %q, want %q", e.Visibility, "feed")
	}
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		t.Fatalf("Payload type = %T, want map[string]any", e.Payload)
	}
	if payload["writ_id"] != "sol-a1b2c3d4e5f6a7b8" {
		t.Errorf("Payload[writ_id] = %v, want %q", payload["writ_id"], "sol-a1b2c3d4e5f6a7b8")
	}
}

func TestFromEventNilPayload(t *testing.T) {
	ie := ievents.Event{
		Timestamp:  time.Now().UTC(),
		Source:     "sentinel",
		Type:       ievents.EventPatrol,
		Actor:      "sentinel",
		Visibility: "audit",
		Payload:    nil,
	}

	e := FromEvent(ie)

	if e.Payload != nil {
		t.Errorf("Payload = %v, want nil", e.Payload)
	}
}

func TestFromEventJSONEquivalence(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)

	ie := ievents.Event{
		Timestamp:  now,
		Source:     "sol",
		Type:       ievents.EventResolve,
		Actor:      "Nova",
		Visibility: "both",
		Payload: map[string]any{
			"writ_id": "sol-deadbeef12345678",
		},
	}

	// Marshal the cliapi event and verify normalized key names.
	cliapiJSON, err := json.Marshal(FromEvent(ie))
	if err != nil {
		t.Fatalf("marshal cliapi event: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(cliapiJSON, &got); err != nil {
		t.Fatalf("unmarshal cliapi event: %v", err)
	}

	// cliapi must use "occurred_at" (not "ts").
	if _, ok := got["occurred_at"]; !ok {
		t.Error("cliapi JSON missing key \"occurred_at\"")
	}
	if _, ok := got["ts"]; ok {
		t.Error("cliapi JSON must not contain old key \"ts\"; expected \"occurred_at\"")
	}
	if got["occurred_at"] != "2026-04-10T12:00:00Z" {
		t.Errorf("occurred_at = %v, want 2026-04-10T12:00:00Z", got["occurred_at"])
	}
	// Other fields must still be present.
	for _, key := range []string{"source", "type", "actor", "visibility", "payload"} {
		if _, ok := got[key]; !ok {
			t.Errorf("cliapi JSON missing key %q", key)
		}
	}
}
