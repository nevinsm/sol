package worlds

import (
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

func TestFromStoreWorld(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	sw := store.World{
		Name:       "sol-dev",
		SourceRepo: "https://github.com/nevinsm/sol",
		CreatedAt:  now,
	}

	info := WorldInfo{
		Branch:         "main",
		State:          "active",
		Health:         "healthy",
		AgentsCount:    3,
		QueueCount:     5,
		Sleeping:       false,
		DefaultAccount: "primary",
	}

	w := FromStoreWorld(sw, info)

	if w.Name != "sol-dev" {
		t.Errorf("Name = %q, want %q", w.Name, "sol-dev")
	}
	if w.SourceRepo != sw.SourceRepo {
		t.Errorf("SourceRepo = %q, want %q", w.SourceRepo, sw.SourceRepo)
	}
	if w.Branch != "main" {
		t.Errorf("Branch = %q, want %q", w.Branch, "main")
	}
	if w.Health != "healthy" {
		t.Errorf("Health = %q, want %q", w.Health, "healthy")
	}
	if w.AgentsCount != 3 {
		t.Errorf("AgentsCount = %d, want 3", w.AgentsCount)
	}
	if w.QueueCount != 5 {
		t.Errorf("QueueCount = %d, want 5", w.QueueCount)
	}
	if w.DefaultAccount != "primary" {
		t.Errorf("DefaultAccount = %q, want %q", w.DefaultAccount, "primary")
	}
	if !w.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", w.CreatedAt, now)
	}
}

func TestFromStoreWorldSleeping(t *testing.T) {
	sw := store.World{
		Name:      "dormant",
		CreatedAt: time.Now().UTC(),
	}

	info := WorldInfo{
		Sleeping: true,
		Health:   "sleeping",
	}

	w := FromStoreWorld(sw, info)

	if !w.Sleeping {
		t.Error("Sleeping = false, want true")
	}
	if w.Health != "sleeping" {
		t.Errorf("Health = %q, want %q", w.Health, "sleeping")
	}
}
