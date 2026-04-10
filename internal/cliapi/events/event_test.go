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

	// Marshal the internal event directly.
	internalJSON, err := json.Marshal(ie)
	if err != nil {
		t.Fatalf("marshal internal event: %v", err)
	}

	// Marshal the cliapi event.
	cliapiJSON, err := json.Marshal(FromEvent(ie))
	if err != nil {
		t.Fatalf("marshal cliapi event: %v", err)
	}

	if string(internalJSON) != string(cliapiJSON) {
		t.Errorf("JSON mismatch:\n  internal: %s\n  cliapi:   %s", internalJSON, cliapiJSON)
	}
}
