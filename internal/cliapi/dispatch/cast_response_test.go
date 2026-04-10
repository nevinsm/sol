package dispatch

import (
	"encoding/json"
	"testing"

	internaldispatch "github.com/nevinsm/sol/internal/dispatch"
)

func TestFromCastResult(t *testing.T) {
	r := &internaldispatch.CastResult{
		WritID:      "sol-a1b2c3d4e5f6a7b8",
		AgentName:   "Nova",
		SessionName: "sol-myworld-Nova",
		WorktreeDir: "/home/user/sol/myworld/outposts/Nova/worktree",
		Guidelines:  "default",
	}

	cr := FromCastResult(r)

	if cr.WritID != r.WritID {
		t.Errorf("WritID = %q, want %q", cr.WritID, r.WritID)
	}
	if cr.AgentName != "Nova" {
		t.Errorf("AgentName = %q, want %q", cr.AgentName, "Nova")
	}
	if cr.WorktreePath != r.WorktreeDir {
		t.Errorf("WorktreePath = %q, want %q", cr.WorktreePath, r.WorktreeDir)
	}
	if cr.SessionName != r.SessionName {
		t.Errorf("SessionName = %q, want %q", cr.SessionName, r.SessionName)
	}
	if cr.Branch != "outpost/Nova/sol-a1b2c3d4e5f6a7b8" {
		t.Errorf("Branch = %q, want %q", cr.Branch, "outpost/Nova/sol-a1b2c3d4e5f6a7b8")
	}
	if cr.Guidelines != "default" {
		t.Errorf("Guidelines = %q, want %q", cr.Guidelines, "default")
	}
}

func TestFromCastResultNoGuidelines(t *testing.T) {
	r := &internaldispatch.CastResult{
		WritID:      "sol-1111222233334444",
		AgentName:   "Altair",
		SessionName: "sol-dev-Altair",
		WorktreeDir: "/tmp/worktree",
	}

	cr := FromCastResult(r)

	if cr.Guidelines != "" {
		t.Errorf("Guidelines = %q, want empty", cr.Guidelines)
	}
	if cr.Branch != "outpost/Altair/sol-1111222233334444" {
		t.Errorf("Branch = %q, want %q", cr.Branch, "outpost/Altair/sol-1111222233334444")
	}
}

func TestCastResultJSON(t *testing.T) {
	cr := CastResult{
		WritID:       "sol-a1b2c3d4e5f6a7b8",
		AgentName:    "Nova",
		WorktreePath: "/home/user/sol/myworld/outposts/Nova/worktree",
		SessionName:  "sol-myworld-Nova",
		Branch:       "outpost/Nova/sol-a1b2c3d4e5f6a7b8",
		Guidelines:   "default",
	}

	data, err := json.Marshal(cr)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Verify snake_case keys.
	for _, key := range []string{"writ_id", "agent_name", "worktree_path", "session_name", "branch", "guidelines"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON output", key)
		}
	}

	// Verify no unexpected keys.
	if len(m) != 6 {
		t.Errorf("expected 6 keys, got %d: %v", len(m), m)
	}
}

func TestCastResultJSONOmitsEmptyGuidelines(t *testing.T) {
	cr := CastResult{
		WritID:       "sol-a1b2c3d4e5f6a7b8",
		AgentName:    "Nova",
		WorktreePath: "/tmp/wt",
		SessionName:  "sol-dev-Nova",
		Branch:       "outpost/Nova/sol-a1b2c3d4e5f6a7b8",
	}

	data, err := json.Marshal(cr)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if _, ok := m["guidelines"]; ok {
		t.Error("guidelines should be omitted when empty")
	}
}
