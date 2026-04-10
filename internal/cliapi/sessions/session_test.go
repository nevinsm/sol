package sessions

import (
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/session"
)

func TestFromSessionInfo(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	activity := now.Add(5 * time.Minute)

	si := session.SessionInfo{
		Name:      "sol-dev-Nova",
		Role:      "outpost",
		World:     "sol-dev",
		Alive:     true,
		StartedAt: now,
	}

	s := FromSessionInfo(si, &activity)

	if s.Name != "sol-dev-Nova" {
		t.Errorf("Name = %q, want %q", s.Name, "sol-dev-Nova")
	}
	if s.Role != "outpost" {
		t.Errorf("Role = %q, want %q", s.Role, "outpost")
	}
	if s.World != "sol-dev" {
		t.Errorf("World = %q, want %q", s.World, "sol-dev")
	}
	if !s.Alive {
		t.Error("Alive = false, want true")
	}
	if !s.StartedAt.Equal(now) {
		t.Errorf("StartedAt = %v, want %v", s.StartedAt, now)
	}
	if s.LastActivityAt == nil || !s.LastActivityAt.Equal(activity) {
		t.Errorf("LastActivityAt = %v, want %v", s.LastActivityAt, activity)
	}
}

func TestFromSessionInfoDead(t *testing.T) {
	si := session.SessionInfo{
		Name:      "sol-test-Agent1",
		Role:      "outpost",
		World:     "test",
		Alive:     false,
		StartedAt: time.Now().UTC(),
	}

	s := FromSessionInfo(si, nil)

	if s.Alive {
		t.Error("Alive = true, want false")
	}
	if s.LastActivityAt != nil {
		t.Errorf("LastActivityAt = %v, want nil", s.LastActivityAt)
	}
}
