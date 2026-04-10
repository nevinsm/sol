package agents

import (
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

func TestFromStoreAgent(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	sa := store.Agent{
		ID:         "sol-dev/Nova",
		Name:       "Nova",
		World:      "sol-dev",
		Role:       "outpost",
		State:      "working",
		ActiveWrit: "sol-a1b2c3d4e5f6a7b8",
	}

	a := FromStoreAgent(sa, "opus", "primary", &now)

	if a.ID != sa.ID {
		t.Errorf("ID = %q, want %q", a.ID, sa.ID)
	}
	if a.Name != "Nova" {
		t.Errorf("Name = %q, want %q", a.Name, "Nova")
	}
	if a.World != "sol-dev" {
		t.Errorf("World = %q, want %q", a.World, "sol-dev")
	}
	if a.Role != "outpost" {
		t.Errorf("Role = %q, want %q", a.Role, "outpost")
	}
	if a.State != "working" {
		t.Errorf("State = %q, want %q", a.State, "working")
	}
	if a.Model != "opus" {
		t.Errorf("Model = %q, want %q", a.Model, "opus")
	}
	if a.Account != "primary" {
		t.Errorf("Account = %q, want %q", a.Account, "primary")
	}
	if a.LastSeenAt == nil || !a.LastSeenAt.Equal(now) {
		t.Errorf("LastSeenAt = %v, want %v", a.LastSeenAt, now)
	}
}

func TestFromStoreAgentMinimal(t *testing.T) {
	sa := store.Agent{
		ID:    "test/Agent1",
		Name:  "Agent1",
		World: "test",
		Role:  "outpost",
		State: "idle",
	}

	a := FromStoreAgent(sa, "", "", nil)

	if a.ActiveWrit != "" {
		t.Errorf("ActiveWrit = %q, want empty", a.ActiveWrit)
	}
	if a.Model != "" {
		t.Errorf("Model = %q, want empty", a.Model)
	}
	if a.LastSeenAt != nil {
		t.Errorf("LastSeenAt = %v, want nil", a.LastSeenAt)
	}
}
