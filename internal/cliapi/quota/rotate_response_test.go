package quota

import (
	"encoding/json"
	"testing"

	iquota "github.com/nevinsm/sol/internal/quota"
)

func TestNewRotateAction(t *testing.T) {
	a := iquota.RotationAction{
		AgentID:     "agent-001",
		AgentName:   "Toast",
		FromAccount: "acct1",
		ToAccount:   "acct2",
		Paused:      false,
	}

	ra := NewRotateAction(a)

	if ra.Agent != "Toast" {
		t.Errorf("Agent = %q, want %q", ra.Agent, "Toast")
	}
	if ra.FromAccount != "acct1" {
		t.Errorf("FromAccount = %q, want %q", ra.FromAccount, "acct1")
	}
	if ra.ToAccount != "acct2" {
		t.Errorf("ToAccount = %q, want %q", ra.ToAccount, "acct2")
	}
	if ra.Paused {
		t.Error("Paused = true, want false")
	}
}

func TestNewRotateActionPaused(t *testing.T) {
	a := iquota.RotationAction{
		AgentID:     "agent-002",
		AgentName:   "Ember",
		FromAccount: "acct1",
		Paused:      true,
	}

	ra := NewRotateAction(a)

	if ra.Agent != "Ember" {
		t.Errorf("Agent = %q, want %q", ra.Agent, "Ember")
	}
	if !ra.Paused {
		t.Error("Paused = false, want true")
	}
	if ra.ToAccount != "" {
		t.Errorf("ToAccount = %q, want empty", ra.ToAccount)
	}
}

func TestNewRotateResponse(t *testing.T) {
	result := &iquota.RotateResult{
		Actions: []iquota.RotationAction{
			{AgentName: "Toast", FromAccount: "acct1", ToAccount: "acct2"},
			{AgentName: "Ember", FromAccount: "acct1", Paused: true},
		},
		Expired: []string{"acct3"},
	}

	resp := NewRotateResponse(result, true)

	if len(resp.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(resp.Actions))
	}
	if resp.Actions[0].Agent != "Toast" {
		t.Errorf("Actions[0].Agent = %q, want %q", resp.Actions[0].Agent, "Toast")
	}
	if len(resp.Expired) != 1 || resp.Expired[0] != "acct3" {
		t.Errorf("Expired = %v, want [acct3]", resp.Expired)
	}
	if !resp.DryRun {
		t.Error("DryRun = false, want true")
	}
}

func TestNewRotateResponseEmptyArrays(t *testing.T) {
	result := &iquota.RotateResult{}

	resp := NewRotateResponse(result, false)

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Empty arrays should be present, not null.
	if string(raw["actions"]) != "[]" {
		t.Errorf("actions = %s, want []", string(raw["actions"]))
	}
	if string(raw["expired"]) != "[]" {
		t.Errorf("expired = %s, want []", string(raw["expired"]))
	}
}

func TestRotateResponseJSON(t *testing.T) {
	resp := RotateResponse{
		Actions: []RotateAction{
			{Agent: "Toast", FromAccount: "acct1", ToAccount: "acct2"},
		},
		Expired: []string{},
		DryRun:  false,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	for _, key := range []string{"actions", "expired", "dry_run"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected key %q in JSON output", key)
		}
	}

	// Verify action field names.
	var actions []map[string]json.RawMessage
	if err := json.Unmarshal(raw["actions"], &actions); err != nil {
		t.Fatalf("Unmarshal actions error: %v", err)
	}

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}

	for _, key := range []string{"agent", "from_account", "to_account"} {
		if _, ok := actions[0][key]; !ok {
			t.Errorf("expected key %q in action", key)
		}
	}
}
