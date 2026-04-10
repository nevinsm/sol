package forge

import (
	"encoding/json"
	"testing"

	"github.com/nevinsm/sol/internal/nudge"
)

func TestForgeAwaitResponse_Woke(t *testing.T) {
	resp := ForgeAwaitResponse{
		Woke: true,
		Messages: []nudge.Message{
			{
				Sender:   "autarch",
				Type:     "FORGE_PAUSED",
				Subject:  "Forge paused",
				Priority: "urgent",
			},
		},
		WaitedSeconds: 1.5,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got["woke"] != true {
		t.Errorf("woke = %v, want true", got["woke"])
	}
	if got["waited_seconds"] != 1.5 {
		t.Errorf("waited_seconds = %v, want 1.5", got["waited_seconds"])
	}

	msgs, ok := got["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages: got %v, want 1-element array", got["messages"])
	}
}

func TestForgeAwaitResponse_Timeout(t *testing.T) {
	resp := ForgeAwaitResponse{
		Woke:          false,
		Messages:      []nudge.Message{},
		WaitedSeconds: 120.0,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got["woke"] != false {
		t.Errorf("woke = %v, want false", got["woke"])
	}

	// Empty messages should be present as an array, not null.
	msgs, ok := got["messages"].([]any)
	if !ok {
		t.Fatalf("messages should be an empty array, got %v (%T)", got["messages"], got["messages"])
	}
	if len(msgs) != 0 {
		t.Errorf("messages length = %d, want 0", len(msgs))
	}
}
